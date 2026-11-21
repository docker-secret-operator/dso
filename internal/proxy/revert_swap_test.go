package proxy

import (
	"testing"
	"time"

	"go.uber.org/zap"
)

// RevertSwap undoes a SwapBackend whose caller failed afterwards. Without it, a failed
// rolling rotation left the proxy routing only to a container that was about to be
// removed, with the original draining and then removed from the registry after
// DrainWindow, and nothing ever re-registered it.

func newSwapped(t *testing.T) *Manager {
	t.Helper()
	m := NewManager(zap.NewNop())
	t.Cleanup(func() { _ = m.Stop(0) })
	if err := m.registry.Add(Backend{ID: "old", Addr: "10.0.0.1:80"}); err != nil {
		t.Fatal(err)
	}
	if err := m.SwapBackend("old", "new", "10.0.0.2", 80, 18090); err != nil {
		t.Fatal(err)
	}
	return m
}

func activeIDs(m *Manager) map[string]bool {
	out := map[string]bool{}
	for _, b := range m.registry.Active() {
		out[b.ID] = true
	}
	return out
}

func TestRevertSwap_RestoresTheOriginalAndDropsTheCandidate(t *testing.T) {
	m := newSwapped(t)
	if a := activeIDs(m); !a["new"] || a["old"] {
		t.Fatalf("precondition: after the swap only the candidate is active, got %v", a)
	}
	if err := m.RevertSwap("old", "new", "10.0.0.1", 80); err != nil {
		t.Fatalf("RevertSwap: %v", err)
	}
	if a := activeIDs(m); !a["old"] || a["new"] {
		t.Fatalf("after a revert only the original is active, got %v", a)
	}
	if _, ok := m.registry.Get("new"); ok {
		t.Fatal("the candidate backend must be removed")
	}
}

func TestRevertSwap_SurvivesTheDeferredRemovalOfTheOriginal(t *testing.T) {
	m := newSwapped(t)
	if err := m.RevertSwap("old", "new", "10.0.0.1", 80); err != nil {
		t.Fatal(err)
	}
	// SwapBackend's goroutine removes the old backend after DrainWindow unless the swap was
	// reverted. Wait past it: the restored original must still be there and active.
	time.Sleep(DrainWindow + 700*time.Millisecond)
	if a := activeIDs(m); !a["old"] {
		t.Fatalf("the deferred removal took the restored original out of the proxy: active=%v", a)
	}
}

func TestRevertSwap_ReaddsAnOriginalThatWasAlreadyRemoved(t *testing.T) {
	m := newSwapped(t)
	// Simulate the drain window having elapsed before the caller's failure.
	_ = m.registry.Remove("old")
	if err := m.RevertSwap("old", "new", "10.0.0.1", 80); err != nil {
		t.Fatalf("RevertSwap: %v", err)
	}
	if a := activeIDs(m); !a["old"] || a["new"] {
		t.Fatalf("a removed original must be re-registered from the supplied address, got %v", a)
	}
	b, _ := m.registry.Get("old")
	if b.Addr != "10.0.0.1:80" || b.Draining {
		t.Fatalf("re-registered original has addr %q draining=%v", b.Addr, b.Draining)
	}
}

func TestRevertSwap_FailureKeepsTheCandidateRouted(t *testing.T) {
	m := newSwapped(t)
	_ = m.registry.Remove("old")
	// No usable address for the original: it cannot be restored, so the candidate (the only
	// backend) must NOT be dropped.
	if err := m.RevertSwap("old", "new", "", 80); err == nil {
		t.Fatal("a revert that cannot restore the original must fail")
	}
	if a := activeIDs(m); !a["new"] {
		t.Fatalf("the only remaining backend was dropped by a failed revert: %v", a)
	}
}
