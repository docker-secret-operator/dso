package rotation

// A small STATEFUL fake Docker daemon (httptest) for the managed-delivery tests:
// containers keep the configuration they were created with, start/stop/rename/remove
// change their state, and every call is logged in order. No real daemon, no socket.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

type n19Ctr struct {
	id, name string
	cfg      container.Config
	hc       *container.HostConfig
	running  bool
	health   string
	removed  bool
}

type n19Daemon struct {
	t   *testing.T
	rec *n19Rec

	mu    sync.Mutex
	ctrs  map[string]*n19Ctr
	log   []string
	seq   int
	count map[string]int

	startErr     bool   // ContainerStart of the candidate fails
	candHealth   string // health of the started candidate ("healthy" by default)
	candExited   bool   // the started candidate is not running (application failed to start)
	archiveCalls int
}

func newN19Daemon(t *testing.T, rec *n19Rec, orig n19Ctr) (*n19Daemon, *client.Client) {
	t.Helper()
	d := &n19Daemon{t: t, rec: rec, ctrs: map[string]*n19Ctr{}, count: map[string]int{}, candHealth: "healthy"}
	o := orig
	o.running = true
	d.ctrs[o.id] = &o
	srv := httptest.NewServer(http.HandlerFunc(d.serve))
	t.Cleanup(srv.Close)
	cli, err := client.NewClientWithOpts(client.WithHost("tcp://"+strings.TrimPrefix(srv.URL, "http://")), client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	return d, cli
}

func (d *n19Daemon) n(call string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.count[call]
}

func (d *n19Daemon) find(ref string) *n19Ctr {
	if c, ok := d.ctrs[ref]; ok && !c.removed {
		return c
	}
	for _, c := range d.ctrs {
		if !c.removed && c.name == ref {
			return c
		}
	}
	return nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (d *n19Daemon) serve(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	// strip the /vX.YZ prefix
	if strings.HasPrefix(p, "/v") {
		if i := strings.Index(p[1:], "/"); i >= 0 {
			p = p[i+1:]
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	rec := func(s string) {
		d.log = append(d.log, s)
		d.count[s]++
		if d.rec != nil {
			d.rec.mu.Lock()
			d.rec.events = append(d.rec.events, "docker:"+s)
			d.rec.mu.Unlock()
		}
	}
	switch {
	case p == "/_ping":
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPost && p == "/containers/create":
		var cr n19CreateReq
		_ = json.NewDecoder(r.Body).Decode(&cr)
		d.seq++
		id := "cand-" + string(rune('0'+d.seq))
		d.ctrs[id] = &n19Ctr{id: id, name: r.URL.Query().Get("name"), cfg: cr.Config, hc: cr.HostConfig}
		if d.rec != nil {
			b, _ := json.Marshal(cr)
			d.rec.capture(b)
		}
		rec("create")
		writeJSON(w, http.StatusCreated, container.CreateResponse{ID: id})
	case r.Method == http.MethodGet && strings.HasSuffix(p, "/json") && strings.HasPrefix(p, "/containers/"):
		ref := strings.TrimSuffix(strings.TrimPrefix(p, "/containers/"), "/json")
		c := d.find(ref)
		if c == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "No such container: " + ref})
			return
		}
		cfg := c.cfg
		in := container.InspectResponse{
			ContainerJSONBase: &container.ContainerJSONBase{
				ID: c.id, Name: "/" + c.name, Path: "/bin/sh", HostConfig: c.hc,
				State: &container.State{Running: c.running},
			},
			Config:          &cfg,
			NetworkSettings: &container.NetworkSettings{Networks: map[string]*network.EndpointSettings{"bridge": {}}},
		}
		if c.health != "" && c.running {
			in.State.Health = &container.Health{Status: c.health}
		}
		writeJSON(w, http.StatusOK, in)
	case r.Method == http.MethodPost && strings.HasSuffix(p, "/start"):
		ref := strings.TrimSuffix(strings.TrimPrefix(p, "/containers/"), "/start")
		c := d.find(ref)
		rec("start:" + ref)
		if c == nil || (d.startErr && strings.HasPrefix(c.id, "cand-")) {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "start failed"})
			return
		}
		c.running = !d.candExited || !strings.HasPrefix(c.id, "cand-")
		if strings.HasPrefix(c.id, "cand-") {
			c.health = d.candHealth
		}
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && strings.HasSuffix(p, "/stop"):
		ref := strings.TrimSuffix(strings.TrimPrefix(p, "/containers/"), "/stop")
		if c := d.find(ref); c != nil {
			c.running = false
		}
		rec("stop:" + ref)
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && strings.HasSuffix(p, "/rename"):
		ref := strings.TrimSuffix(strings.TrimPrefix(p, "/containers/"), "/rename")
		if c := d.find(ref); c != nil {
			c.name = r.URL.Query().Get("name")
		}
		rec("rename:" + ref + "->" + r.URL.Query().Get("name"))
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodDelete && strings.HasPrefix(p, "/containers/"):
		ref := strings.TrimPrefix(p, "/containers/")
		if c := d.find(ref); c != nil {
			c.removed, c.running = true, false
		}
		rec("remove:" + ref)
		w.WriteHeader(http.StatusNoContent)
	case strings.Contains(p, "/archive"):
		d.archiveCalls++
		rec("archive")
		w.WriteHeader(http.StatusOK)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// memJournal is an in-memory rotation.Journal recording every event; Record can be
// made to fail for one transition (the durable write is then refused).
type memJournal struct {
	mu     sync.Mutex
	events []JournalEvent
	failOn string
}

func (j *memJournal) Record(_ context.Context, e JournalEvent) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.failOn != "" && e.Transition == j.failOn {
		return errN19Sim
	}
	j.events = append(j.events, e)
	return nil
}

func (j *memJournal) transitions() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	var out []string
	for _, e := range j.events {
		out = append(out, e.Transition)
	}
	return out
}

func (j *memJournal) last() string {
	t := j.transitions()
	if len(t) == 0 {
		return ""
	}
	return t[len(t)-1]
}

func (j *memJournal) event(tr string) *JournalEvent {
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
