package proxy

import (
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"go.uber.org/zap"

	"github.com/docker-secret-operator/dso/internal/rotation"
)

// M0-04 3.2: discovery is not eligibility. A held container is recorded, never
// bound or registered, and is admitted only when a later pass finds it eligible;
// registration is idempotent and a foreign-owned container is neither registered
// nor held.
func TestScan_HeldCandidateIsNotRegisteredUntilEligible(t *testing.T) {
	m := NewManager(zap.NewNop())
	t.Cleanup(func() { _ = m.Stop(100 * time.Millisecond) })
	m.SetOwnerID("owner-A")

	plain := summary("plain", "10.0.0.1", freePort(t), nil)
	cand := summary("cand", "10.0.0.2", freePort(t), map[string]string{rotation.LabelRotationID: "rot-1"})
	foreign := summary("foreign", "10.0.0.3", freePort(t), map[string]string{"dso.owner": "owner-B", rotation.LabelRotationID: "rot-2"})
	list := []container.Summary{plain, cand, foreign}

	rep := m.registerScannedGated(list, DefaultEligibility)
	if _, ok := m.registry.Get("cand"); ok {
		t.Fatal("a rotation candidate was registered at scan")
	}
	if _, ok := m.registry.Get("plain"); !ok {
		t.Fatal("an ordinary container must be registered")
	}
	if !m.IsHeld("cand") || m.IsHeld("plain") || m.IsHeld("foreign") {
		t.Fatalf("held set wrong: %v", m.HeldSnapshot())
	}
	if _, ok := m.registry.Get("foreign"); ok {
		t.Fatal("a foreign-owned container was registered")
	}
	if len(rep.Held) != 1 {
		t.Fatalf("report held = %v", rep.Held)
	}
	if ids, est := m.RoutingSnapshot(); !est || len(ids) != 1 || ids[0] != "plain" {
		t.Fatalf("snapshot %v %v", ids, est)
	}

	// Still ineligible: stays held, still not routed, scanning again is harmless.
	m.registerScannedGated([]container.Summary{cand}, DefaultEligibility)
	if _, ok := m.registry.Get("cand"); ok || !m.IsHeld("cand") {
		t.Fatal("a still-ineligible candidate must stay held")
	}

	// Recovery settled it: now eligible, admitted, no longer held, idempotent.
	allow := func(ScanCandidate) (bool, string) { return true, "" }
	for i := 0; i < 2; i++ {
		m.registerScannedGated([]container.Summary{cand}, allow)
		if _, ok := m.registry.Get("cand"); !ok || m.IsHeld("cand") {
			t.Fatalf("pass %d: settled candidate must be routed and released", i+1)
		}
	}
	if ids, _ := m.RoutingSnapshot(); len(ids) != 2 {
		t.Fatalf("active %v, want plain+cand", ids)
	}
}

// A draining backend is not "receiving traffic" in the snapshot.
func TestRoutingSnapshot_ExcludesDrainingBackends(t *testing.T) {
	m := NewManager(zap.NewNop())
	t.Cleanup(func() { _ = m.Stop(100 * time.Millisecond) })
	m.registerScannedGated([]container.Summary{summary("a", "10.0.0.1", freePort(t), nil)}, nil)
	if err := m.registry.SetDraining("a"); err != nil {
		t.Fatal(err)
	}
	if ids, est := m.RoutingSnapshot(); !est || len(ids) != 0 {
		t.Fatalf("draining backend reported active: %v %v", ids, est)
	}
}
