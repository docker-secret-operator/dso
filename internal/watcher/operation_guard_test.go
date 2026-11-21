package watcher

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeGuard is a scriptable OperationGuard.
type fakeGuard struct {
	mu      sync.Mutex
	blocked map[string]string // key -> reason
	calls   atomic.Int32
}

func (g *fakeGuard) RotationBlocked(keys ...string) (string, bool) {
	g.calls.Add(1)
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, k := range keys {
		if r, ok := g.blocked[k]; ok {
			return r, true
		}
	}
	return "", false
}

func (g *fakeGuard) block(key, reason string) {
	g.mu.Lock()
	if g.blocked == nil {
		g.blocked = map[string]string{}
	}
	g.blocked[key] = reason
	g.mu.Unlock()
}

// Every dispatch path funnels into TriggerReload; a target whose service has an
// unresolved operation is refused (never mutated), is reported as blocked AND
// deferred (so the caller keeps retrying later), and an unrelated target is still
// rotated.
func TestTriggerReload_GuardRefusesServiceWithUnresolvedOperation(t *testing.T) {
	kills := &sync.Map{}
	r := newMockController(t, composeInspectHandler(t, map[string][2]string{
		"c-a": {"projA", "db"},
		"c-b": {"projB", "api"},
	}, kills))
	r.Targets.Store("c-a", &TargetContainer{ID: "c-a", Strategy: "signal", Secrets: []string{"s"}})
	r.Targets.Store("c-b", &TargetContainer{ID: "c-b", Strategy: "signal", Secrets: []string{"s"}})
	g := &fakeGuard{}
	g.block("db", "unresolved operation rot-1 (signal, last intent_signal_publish)") // by compose service name
	r.Guard = g

	err := waitOutcome(t, r, "s")
	if !errors.Is(err, ErrRotationBlocked) || !errors.Is(err, ErrRotationDeferred) {
		t.Fatalf("outcome %v, want ErrRotationBlocked and ErrRotationDeferred", err)
	}
	if n := killCount(kills, "c-a"); n != 0 {
		t.Fatalf("blocked service was mutated (%d signals)", n)
	}
	if n := killCount(kills, "c-b"); n != 1 {
		t.Fatalf("unrelated service must still rotate once, got %d", n)
	}
	// The refused target must not leave its in-memory service lock held.
	if _, held := r.rotationLocks.Load("projA/db"); held {
		t.Fatal("blocked target leaked its service lock")
	}
}

// The guard matches on container ID too (journal records name original and
// candidate IDs), so a strategy that journals under a different service identifier
// is still covered.
func TestTriggerReload_GuardMatchesByContainerID(t *testing.T) {
	kills := &sync.Map{}
	r := newMockController(t, composeInspectHandler(t, map[string][2]string{"c-a": {"projA", "db"}}, kills))
	r.Targets.Store("c-a", &TargetContainer{ID: "c-a", Strategy: "signal", Secrets: []string{"s"}})
	g := &fakeGuard{}
	g.block("c-a", "x")
	r.Guard = g
	if err := waitOutcome(t, r, "s"); !errors.Is(err, ErrRotationBlocked) {
		t.Fatalf("outcome %v, want ErrRotationBlocked", err)
	}
	if killCount(kills, "c-a") != 0 {
		t.Fatal("blocked container was signalled")
	}
}

// No guard, or a guard that does not block, leaves behaviour unchanged.
func TestTriggerReload_NoGuardOrClearGuardRotates(t *testing.T) {
	for _, g := range []OperationGuard{nil, &fakeGuard{}} {
		kills := &sync.Map{}
		r := newMockController(t, composeInspectHandler(t, map[string][2]string{"c-a": {"projA", "db"}}, kills))
		r.Targets.Store("c-a", &TargetContainer{ID: "c-a", Strategy: "signal", Secrets: []string{"s"}})
		r.Guard = g
		if err := waitOutcome(t, r, "s"); err != nil {
			t.Fatalf("guard %T: outcome %v, want nil", g, err)
		}
		if killCount(kills, "c-a") != 1 {
			t.Fatalf("guard %T: expected exactly one rotation", g)
		}
	}
}

// Concurrent dispatchers (watcher event, scheduled poll, IPC, REST) racing on one
// service while the FIRST rotation journals an unresolved state (as a partially
// published signal rotation does): once that state exists no further rotation may
// start. Run with -race.
func TestTriggerReload_ConcurrentDispatchersNeverStartASecondRotationOverAnUnresolvedOne(t *testing.T) {
	kills := &sync.Map{}
	r := newMockController(t, composeInspectHandler(t, map[string][2]string{"c-a": {"projA", "db"}}, kills))
	r.Targets.Store("c-a", &TargetContainer{ID: "c-a", Strategy: "signal", Secrets: []string{"s"}})
	g := &fakeGuard{}
	r.Guard = g
	var started atomic.Int32
	r.signalRunHook = func(ctx context.Context, target *TargetContainer, serviceName, secretName string) error {
		started.Add(1)
		// The strategy journals an unresolved operation before doing anything else
		// (under the service lock the dispatcher holds).
		g.block("db", "partially published")
		return errors.New("interrupted")
	}

	const dispatchers = 24
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < dispatchers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			done := make(chan error, 1)
			if err := r.TriggerReload(context.Background(), "s", func(err error) { done <- err }); err != nil {
				t.Errorf("TriggerReload: %v", err)
				return
			}
			<-done
		}()
	}
	close(start)
	wg.Wait()
	if n := started.Load(); n != 1 {
		t.Fatalf("%d rotations started over an unresolved operation, want exactly 1 (the one that created it)", n)
	}
}
