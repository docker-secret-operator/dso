package proxy

import (
	"strconv"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"go.uber.org/zap"
)

func summary(id, ip string, port int, labels map[string]string) container.Summary {
	l := map[string]string{"dso.host_ports": strconv.Itoa(port) + ":80"}
	for k, v := range labels {
		l[k] = v
	}
	return container.Summary{
		ID:     id,
		Labels: l,
		NetworkSettings: &container.NetworkSettingsSummary{
			Networks: map[string]*network.EndpointSettings{"bridge": {IPAddress: ip}},
		},
	}
}

// ScanAndRegister's ownership gate: a container claimed by another agent is
// never proxied; unowned (backward compatible) and self-owned ones are.
// Rescanning must be idempotent and must not alter any labels.
func TestRegisterScanned_OwnershipGate(t *testing.T) {
	m := NewManager(zap.NewNop())
	t.Cleanup(func() { _ = m.Stop(100 * time.Millisecond) })
	m.SetOwnerID("owner-A")

	foreign := summary("foreign-id", "10.0.0.1", freePort(t), map[string]string{"dso.owner": "owner-B"})
	unowned := summary("unowned-id", "10.0.0.2", freePort(t), nil)
	mine := summary("mine-id", "10.0.0.3", freePort(t), map[string]string{"dso.owner": "owner-A"})
	list := []container.Summary{foreign, unowned, mine}

	for i := 0; i < 2; i++ { // second scan: no overwrite, no claim, no duplicate
		m.registerScanned(list)
		if _, ok := m.registry.Get("foreign-id"); ok {
			t.Fatalf("scan %d: foreign-owned container was registered", i+1)
		}
		for _, id := range []string{"unowned-id", "mine-id"} {
			if _, ok := m.registry.Get(id); !ok {
				t.Fatalf("scan %d: %s must be registered", i+1, id)
			}
		}
		if n := m.registry.Len(); n != 2 {
			t.Fatalf("scan %d: registry has %d backends, want 2", i+1, n)
		}
	}
	if foreign.Labels["dso.owner"] != "owner-B" || unowned.Labels["dso.owner"] != "" {
		t.Fatal("scan must never change ownership labels")
	}
}

func TestOwnerAllowed_NoOwnerConfiguredAllowsAll(t *testing.T) {
	m := NewManager(zap.NewNop())
	t.Cleanup(func() { _ = m.Stop(100 * time.Millisecond) })
	if !m.OwnerAllowed(map[string]string{"dso.owner": "anyone"}) {
		t.Fatal("with no owner id configured nothing is denied (bare/test harness)")
	}
}
