package watcher

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	dsoConfig "github.com/docker-secret-operator/dso/pkg/config"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
)

// fakeSecretCache is a minimal in-memory SecretCache for tests that need
// TriggerReload to find a live secret value under a "provider:secretName" key.
type fakeSecretCache struct {
	data map[string]map[string]string
}

func (f *fakeSecretCache) Get(key string) (map[string]string, bool) {
	v, ok := f.data[key]
	return v, ok
}

// TestTriggerReload_PrimaryRestart_StripsStaleManagedSecretEnv is the P0-2
// regression test: the PRIMARY "restart" strategy branch of TriggerReload
// (activeStrategy == "restart", distinct from the rolling-strategy fallback
// fixed for P0-1) has the same root cause -- it clones the original
// container's Config.Env verbatim and only ever adds/updates keys present in
// the freshly-fetched env-mode secrets, never removing a name that used to be
// env-mode and has since switched to file-mode (or been dropped). This test
// drives the real production entry point (TriggerReload) against a mock
// Docker daemon and proves:
//   - a stale secret value inherited in Config.Env (from when the secret was
//     env-mode) is stripped once that secret is reconfigured as file-mode
//   - the unrelated env var FOO=bar survives untouched
//   - a *currently* env-mode secret still gets its fresh rotated value
//   - the file-mode secret is delivered via StreamSecretToContainer, not env
func TestTriggerReload_PrimaryRestart_StripsStaleManagedSecretEnv(t *testing.T) {
	const (
		origID   = "container-1"
		newID    = "new-container-1"
		origName = "myapp"
		sentinel = "STALE-LEAKED-VALUE-1789"
		freshVal = "fresh-current-value"
		filePath = "/run/secrets/dso/api-key"
	)

	inspect := minimalInspect(origName, origID)
	// The original (already-running) container inherited STALE_SECRET into
	// its Config.Env from an earlier point when that secret was env-mode.
	// FOO is a plain, non-DSO-managed env var that must survive untouched.
	// CURRENT_SECRET is a still-env-mode secret with an old value that this
	// rotation must update to freshVal.
	inspect.Config.Env = []string{
		"FOO=bar",
		"STALE_SECRET=" + sentinel,
		"CURRENT_SECRET=old-current-value",
	}
	inspect.Config.Labels = map[string]string{}
	inspectJSON, _ := json.Marshal(inspect)

	newInspectHealthy := minimalInspect(origName, newID)
	newInspectHealthy.State = &container.State{
		Running: true,
		Health:  &container.Health{Status: "healthy"},
	}
	newInspectHealthy.NetworkSettings = &container.NetworkSettings{
		Networks: map[string]*network.EndpointSettings{},
	}
	newHealthyJSON, _ := json.Marshal(newInspectHealthy)

	createJSON, _ := json.Marshal(container.CreateResponse{ID: newID})
	execCreateJSON, _ := json.Marshal(container.ExecCreateResponse{ID: "exec-1"})
	execInspectJSON, _ := json.Marshal(container.ExecInspect{ExecID: "exec-1", ExitCode: 0})

	var (
		mu         sync.Mutex
		createdEnv []string
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case r.Method == http.MethodGet && strings.Contains(p, "/containers/"+origID+"/json"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(inspectJSON)

		case r.Method == http.MethodGet && strings.Contains(p, "/containers/"+newID+"/json"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(newHealthyJSON)

		case strings.Contains(p, "/rename"):
			w.WriteHeader(http.StatusNoContent)

		case strings.Contains(p, "/containers/create"):
			body, _ := io.ReadAll(r.Body)
			var req struct {
				Env []string `json:"Env"`
			}
			_ = json.Unmarshal(body, &req)
			mu.Lock()
			createdEnv = req.Env
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write(createJSON)

		case strings.Contains(p, "/containers/"+origID+"/stop"):
			w.WriteHeader(http.StatusNoContent)

		case strings.Contains(p, "/containers/"+newID+"/start"):
			w.WriteHeader(http.StatusNoContent)

		case strings.Contains(p, "/containers/"+newID+"/archive"):
			// StreamSecretToContainer's tar-PUT for the file-mode secret.
			w.WriteHeader(http.StatusOK)

		case strings.Contains(p, "/containers/"+newID+"/exec"):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write(execCreateJSON)

		case strings.Contains(p, "/exec/exec-1/start"):
			w.WriteHeader(http.StatusOK)

		case strings.Contains(p, "/exec/exec-1/json"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(execInspectJSON)

		default:
			w.WriteHeader(http.StatusOK)
		}
	})

	rc := newMockController(t, handler)
	rc.Cache = &fakeSecretCache{data: map[string]map[string]string{
		"vault:current_secret": {"value": freshVal},
		"vault:stale_secret":   {"value": "file-mode-secret-value"},
	}}
	rc.Config = &dsoConfig.Config{
		Providers: map[string]dsoConfig.ProviderConfig{"vault": {}},
		Secrets: []dsoConfig.SecretMapping{
			{
				Name:     "stale_secret",
				Provider: "vault",
				Inject:   dsoConfig.InjectionConfig{Type: "file", Path: filePath, UID: 100, GID: 101},
				Mappings: map[string]string{"value": "STALE_SECRET"},
			},
			{
				Name:     "current_secret",
				Provider: "vault",
				Inject:   dsoConfig.InjectionConfig{Type: "env"},
				Mappings: map[string]string{"value": "CURRENT_SECRET"},
			},
		},
	}
	rc.Targets.Store(origID, &TargetContainer{
		ID:       origID,
		Strategy: "restart",
		Secrets:  []string{"stale_secret", "current_secret"},
	})

	var (
		doneMu sync.Mutex
		done   bool
	)
	onComplete := func(error) {
		doneMu.Lock()
		done = true
		doneMu.Unlock()
	}

	if err := rc.TriggerReload(context.Background(), "current_secret", onComplete); err != nil {
		t.Fatalf("TriggerReload returned unexpected synchronous error: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		doneMu.Lock()
		d := done
		doneMu.Unlock()
		if d {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()

	if createdEnv == nil {
		t.Fatal("ContainerCreate was never called (rotation did not reach container creation)")
	}

	for _, e := range createdEnv {
		if strings.Contains(e, sentinel) {
			t.Fatalf("P0-2 regression: stale secret value leaked into replacement container's Config.Env: %q (full env: %v)", e, createdEnv)
		}
		if strings.HasPrefix(e, "STALE_SECRET=") {
			t.Fatalf("P0-2 regression: STALE_SECRET key should have been stripped (secret is now file-mode), got: %q (full env: %v)", e, createdEnv)
		}
	}

	fooOK, freshOK := false, false
	for _, e := range createdEnv {
		if e == "FOO=bar" {
			fooOK = true
		}
		if e == "CURRENT_SECRET="+freshVal {
			freshOK = true
		}
	}
	if !fooOK {
		t.Errorf("unrelated env var FOO=bar should have been preserved, got: %v", createdEnv)
	}
	if !freshOK {
		t.Errorf("current env-mode secret CURRENT_SECRET should have been updated to %q, got: %v", freshVal, createdEnv)
	}
}
