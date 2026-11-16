package rotation

// Test support for N19 gated file delivery: the in-container operations are the
// DeliveryOps seam; the gate arming, tmpfs/identity verification, journal and
// strategy code between them are the real code. No Docker daemon is involved.

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
)

type n19CreateReq struct {
	container.Config
	HostConfig *container.HostConfig `json:"HostConfig"`
}

type n19Rec struct {
	mu        sync.Mutex
	last      n19CreateReq
	created   [][]byte
	events    []string
	delivered map[string]map[string]string // dir -> files (test observation only)
	injectErr error
	probeErr  error
	relErr    error
}

var errN19Sim = errors.New("simulated delivery failure")

func (g *n19Rec) capture(body []byte) {
	var cr n19CreateReq
	if json.Unmarshal(body, &cr) == nil {
		g.mu.Lock()
		g.last = cr
		g.created = append(g.created, body)
		g.mu.Unlock()
	}
}

func (g *n19Rec) ev(e string) {
	g.mu.Lock()
	g.events = append(g.events, e)
	g.mu.Unlock()
}

func (g *n19Rec) has(e string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, x := range g.events {
		if x == e {
			return true
		}
	}
	return false
}

func (g *n19Rec) ops() *DeliveryOps {
	return &DeliveryOps{
		Inspect: func(_ context.Context, id string) (container.InspectResponse, error) {
			g.mu.Lock()
			defer g.mu.Unlock()
			cfg := g.last.Config
			in := container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{
					ID: id, Name: "/cand", Path: "/bin/sh",
					State: &container.State{Running: true}, HostConfig: g.last.HostConfig,
				},
				Config: &cfg,
			}
			if g.last.HostConfig != nil {
				for d := range g.last.HostConfig.Tmpfs {
					in.Mounts = append(in.Mounts, container.MountPoint{Type: mount.TypeTmpfs, Destination: d, RW: true})
				}
			}
			return in, nil
		},
		Inject: func(_ context.Context, id string, spec FileSecretSpec) error {
			g.ev("inject:" + spec.Path)
			if g.injectErr != nil {
				return g.injectErr
			}
			g.mu.Lock()
			if g.delivered == nil {
				g.delivered = map[string]map[string]string{}
			}
			g.delivered[spec.Path] = spec.Data
			g.mu.Unlock()
			return nil
		},
		Probe: func(_ context.Context, id string, spec FileSecretSpec) error {
			g.ev("probe:" + spec.Path)
			return g.probeErr
		},
		Release: func(_ context.Context, id, dir string, uid, gid int) error {
			g.ev("release:" + dir)
			return g.relErr
		},
	}
}
