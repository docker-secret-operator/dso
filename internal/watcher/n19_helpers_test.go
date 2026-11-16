package watcher

// Test support for N19 managed file delivery (docs/design/n19-managed-delivery.md).
// Everything here is Docker-free: the daemon is an httptest handler and the
// in-container operations (hardened exec injector, probe) are the rotation.DeliveryOps
// seam, with the REAL gate arming, tmpfs/identity verification, journal and strategy
// code in between.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"

	"github.com/docker-secret-operator/dso/internal/rotation"
)

type createReq struct {
	container.Config
	HostConfig *container.HostConfig `json:"HostConfig"`
}

// gateRec records container-create bodies (what the candidate was created with) and
// the ordered delivery events, and serves the DeliveryOps seam.
type gateRec struct {
	mu     sync.Mutex
	last   createReq
	bodies [][]byte
	events []string

	injectErr  error // returned by every Inject
	probeErr   error // returned by every Probe
	probeAfter int   // Probe fails only from this call number on (1-based); 0 = always when probeErr set
	releaseErr error
	user       string // overrides the inspected container user

	flagPresent bool // the gate release flag already exists (flag probes succeed)

	probeCalls int
	// onEvent is called (under no lock) at each delivery event, for ordering checks.
	onEvent func(ev string)
}

func (g *gateRec) record(ev string) {
	g.mu.Lock()
	g.events = append(g.events, ev)
	cb := g.onEvent
	g.mu.Unlock()
	if cb != nil {
		cb(ev)
	}
}

func (g *gateRec) eventList() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.events...)
}

func (g *gateRec) has(ev string) bool {
	for _, e := range g.eventList() {
		if e == ev {
			return true
		}
	}
	return false
}

// wrap captures the body of every container create and forwards to next.
func (g *gateRec) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/create") {
			body, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(body))
			var cr createReq
			if json.Unmarshal(body, &cr) == nil {
				g.mu.Lock()
				g.last = cr
				g.bodies = append(g.bodies, body)
				g.mu.Unlock()
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ops returns the delivery seam. The inspected container is the last created one.
func (g *gateRec) ops() *rotation.DeliveryOps {
	return &rotation.DeliveryOps{
		Inspect: func(_ context.Context, id string) (container.InspectResponse, error) {
			g.mu.Lock()
			defer g.mu.Unlock()
			cfg := g.last.Config
			hc := g.last.HostConfig
			if g.user != "" {
				cfg.User = g.user
			}
			in := container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{
					ID: id, Name: "/cand", Path: "/bin/sh",
					State:      &container.State{Running: true},
					HostConfig: hc,
				},
				Config: &cfg,
			}
			if hc != nil {
				for dest := range hc.Tmpfs {
					in.Mounts = append(in.Mounts, container.MountPoint{Type: mount.TypeTmpfs, Destination: dest, RW: true})
				}
			}
			return in, nil
		},
		Inject: func(_ context.Context, id string, spec rotation.FileSecretSpec) error {
			g.record("inject:" + spec.Path)
			return g.injectErr
		},
		Probe: func(_ context.Context, id string, spec rotation.FileSecretSpec) error {
			if _, isFlag := spec.Data[rotation.GateFlagName]; isFlag {
				g.record("flagprobe")
				g.mu.Lock()
				present := g.flagPresent
				g.mu.Unlock()
				if present {
					return nil
				}
				return errSimulated
			}
			g.mu.Lock()
			g.probeCalls++
			n := g.probeCalls
			err := g.probeErr
			after := g.probeAfter
			g.mu.Unlock()
			g.record("probe:" + spec.Path)
			if err != nil && (after == 0 || n >= after) {
				return err
			}
			return nil
		},
		Release: func(_ context.Context, id, dir string, uid, gid int) error {
			g.record("release:" + dir)
			return g.releaseErr
		},
	}
}

var errSimulated = errors.New("simulated delivery failure")

// tmpfsInspect makes inspect describe a container with a declared tmpfs at dir and a
// command (so the gate can wrap it).
func withTmpfsAndCmd(in *container.InspectResponse, dirs ...string) {
	if in.HostConfig == nil {
		in.HostConfig = &container.HostConfig{}
	}
	in.HostConfig.Tmpfs = map[string]string{}
	for _, d := range dirs {
		in.HostConfig.Tmpfs[d] = ""
	}
	in.Config.Entrypoint = []string{"/app/entry"}
	in.Config.Cmd = []string{"--serve", "a b"}
}

func isUnavailable(err error) bool { return errors.Is(err, ErrFileSecretUnavailable) }

// memJ is an in-memory rotation.Journal.
type memJ struct {
	mu     sync.Mutex
	events []rotation.JournalEvent
	failOn string
}

func (j *memJ) Record(_ context.Context, e rotation.JournalEvent) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.failOn != "" && e.Transition == j.failOn {
		return errSimulated
	}
	j.events = append(j.events, e)
	return nil
}

func (j *memJ) transitions() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	var out []string
	for _, e := range j.events {
		out = append(out, e.Transition)
	}
	return out
}

func (j *memJ) has(tr string) bool {
	for _, t := range j.transitions() {
		if t == tr {
			return true
		}
	}
	return false
}

func (j *memJ) last() string {
	t := j.transitions()
	if len(t) == 0 {
		return ""
	}
	return t[len(t)-1]
}

func (j *memJ) event(tr string) *rotation.JournalEvent {
	j.mu.Lock()
	defer j.mu.Unlock()
	for i := range j.events {
		if j.events[i].Transition == tr {
			e := j.events[i]
			return &e
		}
	}
	return nil
}

func indexOf(list []string, want string) int {
	for i, x := range list {
		if x == want {
			return i
		}
	}
	return -1
}
