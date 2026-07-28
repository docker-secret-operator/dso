package proxy

import (
	"testing"
	"time"
)

func TestNewManager(t *testing.T) {
	m := NewManager(testLogger(t))
	defer func() { _ = m.Stop(time.Second) }()

	if m.registry == nil || m.router == nil || m.server == nil {
		t.Fatal("NewManager must initialize registry, router, and server")
	}
}

func TestManager_EnsurePort(t *testing.T) {
	m := NewManager(testLogger(t))
	defer func() { _ = m.Stop(time.Second) }()

	if err := m.EnsurePort(0, 80); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.server.Bindings()) != 1 {
		t.Fatalf("expected 1 port binding, got %d", len(m.server.Bindings()))
	}

	// Calling again for the same port must be idempotent (delegates to
	// Server.Bind, already tested to be idempotent).
	realPort := m.server.Bindings()[0].ListenPort
	if err := m.EnsurePort(realPort, 80); err != nil {
		t.Fatalf("expected idempotent EnsurePort to succeed, got: %v", err)
	}
}

func TestManager_RegisterContainer(t *testing.T) {
	m := NewManager(testLogger(t))
	defer func() { _ = m.Stop(time.Second) }()

	if err := m.RegisterContainer("container-1", "10.0.0.5", 3306, 3306); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b, ok := m.registry.Get("container-1")
	if !ok {
		t.Fatal("expected backend to be registered")
	}
	if b.Addr != "10.0.0.5:3306" {
		t.Errorf("expected addr 10.0.0.5:3306, got %s", b.Addr)
	}

	// containerToBackendID mapping should resolve back to the same ID.
	if v, ok := m.containerToBackendID.Load("container-1"); !ok || v != "container-1" {
		t.Error("expected containerToBackendID to map container-1 -> container-1")
	}
}

func TestManager_RegisterContainer_DuplicateFails(t *testing.T) {
	m := NewManager(testLogger(t))
	defer func() { _ = m.Stop(time.Second) }()

	if err := m.RegisterContainer("c1", "10.0.0.1", 80, 80); err != nil {
		t.Fatal(err)
	}
	err := m.RegisterContainer("c1", "10.0.0.2", 80, 80)
	if err == nil {
		t.Fatal("expected error registering the same container ID twice")
	}
}

func TestManager_DeregisterContainer(t *testing.T) {
	m := NewManager(testLogger(t))
	defer func() { _ = m.Stop(time.Second) }()

	if err := m.RegisterContainer("c1", "10.0.0.1", 80, 80); err != nil {
		t.Fatal(err)
	}
	m.DeregisterContainer("c1")

	if _, ok := m.registry.Get("c1"); ok {
		t.Error("expected backend to be removed from registry")
	}
	if _, ok := m.containerToBackendID.Load("c1"); ok {
		t.Error("expected containerToBackendID entry to be cleared")
	}
}

func TestManager_DeregisterContainer_UnknownIsSafe(t *testing.T) {
	m := NewManager(testLogger(t))
	defer func() { _ = m.Stop(time.Second) }()
	// Must not panic when deregistering a container that was never registered.
	m.DeregisterContainer("never-registered")
}

func TestManager_Stop(t *testing.T) {
	m := NewManager(testLogger(t))
	if err := m.EnsurePort(0, 80); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(2 * time.Second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.server.Bindings()) != 0 {
		t.Error("expected all bindings closed after Stop")
	}
}

// TestManager_SwapBackend_ImmediateRoutingChange verifies the documented
// zero-downtime swap contract: the new backend is reachable immediately,
// and the old backend is marked draining right away (no new connections),
// even though its full removal is deferred.
//
// NOTE: SwapBackend's deferred removal runs in an un-cancellable
// fire-and-forget goroutine (time.Sleep(5*time.Second) with no context or
// stop channel tied to Manager.Stop). This is the same issue already
// identified as BUG-4 in the original audit and is deliberately NOT
// fixed here -- QUALITY-1's scope is test coverage, not behavior changes.
// This test verifies the swap's immediate, synchronous effects only.
func TestManager_SwapBackend_ImmediateRoutingChange(t *testing.T) {
	m := NewManager(testLogger(t))
	defer func() { _ = m.Stop(time.Second) }()

	if err := m.RegisterContainer("old", "10.0.0.1", 80, 80); err != nil {
		t.Fatal(err)
	}

	if err := m.SwapBackend("old", "new", "10.0.0.2", 80, 8080); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// New backend must be immediately active.
	newBackend, ok := m.registry.Get("new")
	if !ok {
		t.Fatal("expected new backend to be registered immediately")
	}
	if newBackend.Draining {
		t.Error("new backend must not be draining")
	}

	// Old backend must be immediately draining (still present, not yet removed).
	oldBackend, ok := m.registry.Get("old")
	if !ok {
		t.Fatal("expected old backend to still be present immediately after swap (removal is deferred)")
	}
	if !oldBackend.Draining {
		t.Error("expected old backend to be marked draining immediately")
	}

	// Router must route to 'new' only, immediately -- not wait for the
	// deferred removal.
	router := NewRouter(m.registry)
	for i := 0; i < 5; i++ {
		b, err := router.Next()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if b.ID != "new" {
			t.Errorf("expected routing to 'new' immediately after swap, got %s", b.ID)
		}
	}
}

// TestManager_SwapBackend_DeferredRemoval verifies the swap's eventual
// consistency: after the drain window elapses, the old backend is actually
// removed from the registry, not just marked draining forever. Polls rather
// than a blind sleep to stay as fast as possible while remaining
// deterministic (the drain window is a fixed 5s in SwapBackend).
//
// This does not test or change the fire-and-forget goroutine's lifecycle
// (no context/stop channel tied to Manager.Stop) -- that gap is BUG-4 from
// the original audit and is out of scope for this coverage-only change.
func TestManager_SwapBackend_DeferredRemoval(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping >5s deferred-removal test in -short mode")
	}
	m := NewManager(testLogger(t))
	defer func() { _ = m.Stop(time.Second) }()

	if err := m.RegisterContainer("old", "10.0.0.1", 80, 80); err != nil {
		t.Fatal(err)
	}
	if err := m.SwapBackend("old", "new", "10.0.0.2", 80, 8080); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(7 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := m.registry.Get("old"); !ok {
			return // removed, as expected
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("expected old backend to be removed from the registry within 7s of SwapBackend")
}

func TestManager_SwapBackend_OldAlreadyGoneIsTolerated(t *testing.T) {
	m := NewManager(testLogger(t))
	defer func() { _ = m.Stop(time.Second) }()

	// "old" was never registered -- SwapBackend must still succeed for the
	// new backend, per the documented "may already be removed" tolerance.
	if err := m.SwapBackend("never-existed", "new", "10.0.0.2", 80, 8080); err != nil {
		t.Fatalf("expected SwapBackend to tolerate a missing old backend, got: %v", err)
	}
	if _, ok := m.registry.Get("new"); !ok {
		t.Fatal("expected new backend to be registered despite old backend being absent")
	}
}
