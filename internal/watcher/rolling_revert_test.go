package watcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"go.uber.org/zap"

	dsoProxy "github.com/docker-secret-operator/dso/internal/proxy"
)

// revertRollingHandoff is RollingStrategy.OnRevert for a proxy-owned container: after a
// rolling rotation failed once the handoff had already swapped the proxy to the candidate,
// the original must be routed again. These tests use the real proxy.Manager.

func revertRig(t *testing.T, originalIP string) (*ReloaderController, *dsoProxy.Manager) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/old/json") {
			resp := container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{ID: "old", Name: "/svc"},
				NetworkSettings: &container.NetworkSettings{Networks: map[string]*network.EndpointSettings{
					"bridge": {IPAddress: originalIP},
				}},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	cli, err := client.NewClientWithOpts(client.WithVersion("1.41"), client.WithHost("tcp://"+strings.TrimPrefix(srv.URL, "http://")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	pm := dsoProxy.NewManager(zap.NewNop())
	t.Cleanup(func() { _ = pm.Stop(0) })
	if err := pm.RegisterContainer("old", "10.0.0.1", 18095, 80); err != nil {
		t.Fatal(err)
	}
	if err := pm.SwapBackend("old", "new", "10.0.0.2", 80, 18095); err != nil {
		t.Fatal(err)
	}
	return &ReloaderController{Logger: zap.NewNop(), cli: cli, ProxyManager: pm}, pm
}

func activeBackendIDs(pm *dsoProxy.Manager) map[string]bool {
	active, _ := pm.RoutingSnapshot()
	out := map[string]bool{}
	for _, id := range active {
		out[id] = true
	}
	return out
}

func TestRevertRollingHandoff_PutsTheOriginalBackInService(t *testing.T) {
	r, pm := revertRig(t, "10.0.0.1")
	if a := activeBackendIDs(pm); a["old"] || !a["new"] {
		t.Fatalf("precondition: only the candidate is active after the swap, got %v", a)
	}
	if err := r.revertRollingHandoff(context.Background(), "old", "new", "18095:80"); err != nil {
		t.Fatalf("revertRollingHandoff: %v", err)
	}
	if a := activeBackendIDs(pm); !a["old"] || a["new"] {
		t.Fatalf("after the revert only the original must be routed, got %v", a)
	}
}

func TestRevertRollingHandoff_NothingToRevertWhenNotProxyOwned(t *testing.T) {
	r, pm := revertRig(t, "10.0.0.1")
	before := activeBackendIDs(pm)
	if err := r.revertRollingHandoff(context.Background(), "old", "new", ""); err != nil {
		t.Fatalf("no ports label means OnReady handed nothing off: %v", err)
	}
	r.ProxyManager = nil
	if err := r.revertRollingHandoff(context.Background(), "old", "new", "18095:80"); err != nil {
		t.Fatalf("no proxy manager means nothing to revert: %v", err)
	}
	if after := activeBackendIDs(pm); len(after) != len(before) || after["new"] != before["new"] {
		t.Fatalf("a no-op revert changed the proxy: %v -> %v", before, after)
	}
}

func TestRevertRollingHandoff_ReportsAnOriginalItCannotRestore(t *testing.T) {
	r, pm := revertRig(t, "") // the original has no address to re-register with
	// Simulate the drain window having removed the original from the registry already.
	pm.DeregisterContainer("old")
	if err := r.revertRollingHandoff(context.Background(), "old", "new", "18095:80"); err == nil {
		t.Fatal("a revert that cannot restore the original must fail (the strategy then preserves the candidate)")
	}
	if a := activeBackendIDs(pm); !a["new"] {
		t.Fatalf("the candidate, the only backend, must not be dropped by a failed revert: %v", a)
	}
}
