package watcher

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
)

// waitUntil polls cond with a generous deadline (a failure bound, not a sync point).
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for: %s", what)
		}
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
}

// Lifecycle (1)(2): any number of requests never create more than one worker; the worker
// exits when idle (gateScanBusy released), so nothing accumulates.
func TestN19ReconcileLifecycle_NoWorkerAccumulation(t *testing.T) {
	w := newGateWorld(t)
	w.listGate = make(chan struct{})
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 250; i++ {
				w.r.RequestGateReconcile(t.Context())
			}
		}()
	}
	wg.Wait()
	waitUntil(t, "first pass listing", func() bool { return w.lists() >= 1 })
	// 2000 requests while the pass is blocked: a second worker would list concurrently.
	runtime.Gosched()
	if n := w.lists(); n != 1 {
		t.Fatalf("%d concurrent list calls: more than one worker is running", n)
	}
	close(w.listGate)
	waitUntil(t, "worker idle", func() bool { return !w.r.gateScanBusy.Load() })
	if w.r.gateScanPending.Load() {
		t.Fatal("pending request left unserved by an idle worker")
	}
	if n := w.lists(); n != 2 {
		t.Fatalf("2000 requests during one pass must collapse into exactly one more pass, got %d total", n)
	}
}

// Lifecycle (3): a request arriving while a pass is running is served by a further pass.
func TestN19ReconcileLifecycle_RequestDuringRunningPassIsNotLost(t *testing.T) {
	w := newGateWorld(t)
	w.listGate = make(chan struct{})
	w.r.RequestGateReconcile(t.Context())
	waitUntil(t, "first pass listing", func() bool { return w.lists() == 1 })
	w.r.RequestGateReconcile(t.Context()) // arrives mid-pass
	close(w.listGate)
	waitUntil(t, "second pass", func() bool { return w.lists() == 2 })
	waitUntil(t, "worker idle", func() bool { return !w.r.gateScanBusy.Load() })
	if n := w.lists(); n != 2 {
		t.Fatalf("expected exactly 2 passes, got %d", n)
	}
}

// Lifecycle (3), the finishing-worker race: a request issued as the worker winds down
// must always cause a pass that starts after the request (no lost wakeup).
func TestN19ReconcileLifecycle_NoLostWakeupWhenWorkerFinishes(t *testing.T) {
	w := newGateWorld(t)
	for i := 0; i < 1500; i++ {
		before := w.lists()
		w.r.RequestGateReconcile(t.Context())
		waitUntil(t, "a pass after the request", func() bool { return w.lists() > before })
	}
	waitUntil(t, "worker idle", func() bool { return !w.r.gateScanBusy.Load() })
}

// Lifecycle (4)(5): cancelling the context while a pass is in flight ends the worker, and a
// request on a cancelled context issues no Docker call at all.
func TestN19ReconcileLifecycle_CancelStopsWorkerAndFurtherOperations(t *testing.T) {
	w := newGateWorld(t)
	w.addRunning("app-1", "app")
	w.listGate = make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-w.listGate:
		default:
			close(w.listGate)
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	w.r.RequestGateReconcile(ctx)
	waitUntil(t, "pass in flight", func() bool { return w.lists() == 1 })
	w.r.RequestGateReconcile(ctx) // pending, but the context dies before it can run
	cancel()
	waitUntil(t, "worker exited after cancel", func() bool { return !w.r.gateScanBusy.Load() })
	for i := 0; i < 20; i++ {
		w.r.RequestGateReconcile(ctx)
	}
	waitUntil(t, "worker exited again", func() bool { return !w.r.gateScanBusy.Load() })
	if n := w.lists(); n != 1 {
		t.Fatalf("Docker listed %d times; nothing may be issued after cancellation", n)
	}
	if len(w.lifecycleCalls()) != 0 || w.released() || w.gate.has("inject:/run/secrets/dso") {
		t.Fatalf("operations after cancellation: %v", w.gate.eventList())
	}
}

// Lifecycle (5): cancellation in the middle of a pass stops the per-container loop; the
// remaining containers are neither inspected nor touched.
func TestN19ReconcileLifecycle_CancelMidPassStopsPerContainerWork(t *testing.T) {
	w := newGateWorld(t)
	w.cache.data = map[string]map[string]string{} // would only wait, never deliver
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var mu sync.Mutex
	inspected := map[string]int{}
	for _, id := range []string{"c1", "c2", "c3"} {
		w.addRunning(id, "svc-"+id)
		id := id
		w.override[id] = func(*container.InspectResponse) {
			mu.Lock()
			inspected[id]++
			mu.Unlock()
			if id == "c1" {
				cancel()
			}
		}
	}
	w.r.ReconcileGatedContainers(ctx)
	mu.Lock()
	defer mu.Unlock()
	if inspected["c1"] != 1 || inspected["c2"] != 0 || inspected["c3"] != 0 {
		t.Fatalf("work continued after cancel: %v", inspected)
	}
	if w.released() || len(w.lifecycleCalls()) != 0 {
		t.Fatalf("gate touched after cancel: %v", w.gate.eventList())
	}
}
