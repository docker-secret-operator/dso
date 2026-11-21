package watcher

// N22 regression tests (confirmed source-level defect, fixed with the N19 workstream):
//
//   - the restart branch skipped file injection SILENTLY when Cache.Get missed and
//     started the replacement without its file secret (rolling had the same shape);
//   - the primary restart branch probed only `Inject.Type == "file"`, never the
//     EFFECTIVE type (a secret that inherits the default inject type was never probed).
//
// These tests use only the HTTP daemon (no delivery seam), so they compile and RUN
// against the pre-fix code, where they fail; they pass with the fix.

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/docker/docker/api/types/container"

	dsoConfig "github.com/docker-secret-operator/dso/pkg/config"
)

type n22Daemon struct {
	mu       sync.Mutex
	log      []string
	execErr  bool
	startErr bool   // the replacement's start fails
	health   string // replacement health ("healthy" when empty)
}

func (d *n22Daemon) calls(sub string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, l := range d.log {
		if strings.Contains(l, sub) {
			n++
		}
	}
	return n
}

func (d *n22Daemon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	d.mu.Lock()
	d.log = append(d.log, r.Method+" "+p)
	d.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && strings.Contains(p, "/containers/orig-1/json"):
		in := minimalInspect("app", "orig-1")
		in.Config.Labels = map[string]string{}
		in.State = &container.State{Running: true}
		withTmpfsAndCmd(&in, "/run/secrets/dso")
		b, _ := json.Marshal(in)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	case r.Method == http.MethodGet && strings.Contains(p, "/containers/new-1/json"):
		in := minimalInspect("app", "new-1")
		h := d.health
		if h == "" {
			h = "healthy"
		}
		in.State = &container.State{Running: true, Health: &container.Health{Status: h}}
		b, _ := json.Marshal(in)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	case d.startErr && strings.HasSuffix(p, "/containers/new-1/start"):
		http.Error(w, `{"message":"start failed"}`, http.StatusInternalServerError)
	case strings.HasSuffix(p, "/containers/create"):
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"Id":"new-1"}`))
	case strings.Contains(p, "/exec") && d.execErr:
		http.Error(w, `{"message":"exec unavailable"}`, http.StatusInternalServerError)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func n22Controller(t *testing.T, d *n22Daemon, strategy string, cache map[string]map[string]string, sec dsoConfig.SecretMapping, defaults dsoConfig.InjectionConfig) *ReloaderController {
	t.Helper()
	r := newMockController(t, d)
	r.Cache = &fakeSecretCache{data: cache}
	r.Config = &dsoConfig.Config{
		Providers: map[string]dsoConfig.ProviderConfig{"vault": {}},
		Secrets:   []dsoConfig.SecretMapping{sec},
	}
	r.Config.Defaults.Inject = defaults
	r.Targets.Store("orig-1", &TargetContainer{ID: "orig-1", Strategy: strategy, Secrets: []string{sec.Name}})
	return r
}

func n22FileSecret() dsoConfig.SecretMapping {
	return dsoConfig.SecretMapping{
		Name: "db", Provider: "vault",
		Inject:   dsoConfig.InjectionConfig{Type: "file", Path: "/run/secrets/dso", UID: 1000, GID: 1000},
		Mappings: map[string]string{"value": "DB"},
	}
}

// A required file secret with no cached value must abort the rotation BEFORE anything
// is changed: no rename, no replacement created or started, no original stopped.
func TestN22_Restart_FileSecretCacheMiss_FailsClosedBeforeAnyChange(t *testing.T) {
	d := &n22Daemon{}
	r := n22Controller(t, d, "restart", map[string]map[string]string{}, n22FileSecret(), dsoConfig.InjectionConfig{})

	err := waitOutcome(t, r, "db")
	if err == nil {
		t.Fatal("rotation reported success although the file secret had no value")
	}
	for _, sub := range []string{"/rename", "/containers/create", "/containers/new-1/start", "/containers/orig-1/stop"} {
		if n := d.calls(sub); n != 0 {
			t.Fatalf("%s was called %d time(s): a replacement must never be started without its file secret (log %v)", sub, n, d.log)
		}
	}
}

func TestN22_Rolling_FileSecretCacheMiss_FailsClosedBeforeAnyChange(t *testing.T) {
	d := &n22Daemon{}
	r := n22Controller(t, d, "rolling", map[string]map[string]string{}, n22FileSecret(), dsoConfig.InjectionConfig{})

	err := waitOutcome(t, r, "db")
	if err == nil {
		t.Fatal("rotation reported success although the file secret had no value")
	}
	for _, sub := range []string{"/rename", "/containers/create", "/containers/new-1/start"} {
		if n := d.calls(sub); n != 0 {
			t.Fatalf("%s was called %d time(s) (log %v)", sub, n, d.log)
		}
	}
}

// A secret that inherits the DEFAULT inject type "file" is a file secret: its
// delivery must be verified. The daemon here cannot run any exec; the old code
// probed only an explicit `Inject.Type == "file"`, so a defaulted file secret was
// never probed and the rotation reported success.
func TestN22_Restart_DefaultInjectTypeFile_IsVerifiedNotSkipped(t *testing.T) {
	d := &n22Daemon{execErr: true}
	sec := n22FileSecret()
	sec.Inject = dsoConfig.InjectionConfig{} // inherits the default
	r := n22Controller(t, d, "restart",
		map[string]map[string]string{"vault:db": {"value": "synthetic-placeholder"}}, sec,
		dsoConfig.InjectionConfig{Type: "file", Path: "/run/secrets/dso", UID: 1000, GID: 1000})

	err := waitOutcome(t, r, "db")
	if err == nil {
		t.Fatal("restart succeeded without ever verifying delivery of a defaulted file secret")
	}
	if d.calls("/containers/orig-1/stop") != 0 && d.calls("DELETE /containers/orig-1") != 0 {
		t.Fatalf("the original was removed although verification failed (log %v)", d.log)
	}
}

// New-code-only: the refusal is typed so callers can tell it from a transient error.
func TestN22_FileSecretUnavailableIsTyped(t *testing.T) {
	d := &n22Daemon{}
	r := n22Controller(t, d, "restart", map[string]map[string]string{}, n22FileSecret(), dsoConfig.InjectionConfig{})
	err := waitOutcome(t, r, "db")
	if !isUnavailable(err) {
		t.Fatalf("err = %v, want ErrFileSecretUnavailable", err)
	}
	// missing inject.path and empty value are refused the same way
	sec := n22FileSecret()
	sec.Inject.Path = ""
	if _, rerr := n22Controller(t, d, "restart", map[string]map[string]string{"vault:db": {"value": "x"}}, sec, dsoConfig.InjectionConfig{}).
		resolveFileSpecs(&TargetContainer{ID: "orig-1", Secrets: []string{"db"}}); !isUnavailable(rerr) {
		t.Fatalf("missing path: %v", rerr)
	}
	if _, rerr := n22Controller(t, d, "restart", map[string]map[string]string{"vault:db": {}}, n22FileSecret(), dsoConfig.InjectionConfig{}).
		resolveFileSpecs(&TargetContainer{ID: "orig-1", Secrets: []string{"db"}}); !isUnavailable(rerr) {
		t.Fatalf("empty value: %v", rerr)
	}
}
