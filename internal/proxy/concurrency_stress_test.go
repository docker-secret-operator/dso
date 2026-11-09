package proxy

import (
	"fmt"
	"strconv"
	"sync"
	"testing"

	"go.uber.org/zap/zaptest"
)

// ============================================================================
// Phase 7 / P2-4, Test B: SwapBackend vs DeregisterContainer concurrency
// regression coverage.
//
// The Phase 6 audit did NOT find a confirmed race here -- it found missing
// targeted coverage for exactly this interleaving. This test exercises it
// under `go test -race` and asserts the registry is left in a valid,
// non-panicking, internally-consistent state regardless of ordering. It
// does not weaken any existing locking to make itself pass.
// ============================================================================

// TestConcurrent_SwapBackendVsDeregister fires many concurrent SwapBackend
// and DeregisterContainer calls involving overlapping container IDs and
// verifies the registry never panics or ends up in a corrupted state (e.g.
// a backend entry whose ID doesn't match anything ever registered).
func TestConcurrent_SwapBackendVsDeregister(t *testing.T) {
	m := NewManager(zaptest.NewLogger(t))
	t.Cleanup(func() { _ = m.Stop(0) })

	const numContainers = 6
	const numRounds = 50

	ids := make([]string, numContainers)
	for i := range ids {
		ids[i] = "container-" + strconv.Itoa(i)
	}

	var wg sync.WaitGroup

	// Concurrent SwapBackend calls cycling through container pairs --
	// stresses registry.Add/SetDraining/Remove and containerToBackendID
	// under real contention.
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < numRounds; i++ {
				oldID := ids[i%numContainers]
				newID := ids[(i+1)%numContainers]
				ip := fmt.Sprintf("10.0.%d.%d", workerID, i%255)
				// Errors are expected and fine here (e.g. "already
				// registered" if another goroutine's swap raced ahead) --
				// the invariant under test is "never panics, never
				// corrupts state", not "every call succeeds".
				_ = m.SwapBackend(oldID, newID, ip, 8080, 9000+i%10)
			}
		}(w)
	}

	// Concurrent DeregisterContainer calls against the SAME container IDs
	// the swap goroutines are actively using -- the specific interleaving
	// the Phase 6 audit flagged as untested.
	for w := 0; w < 3; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < numRounds; i++ {
				m.DeregisterContainer(ids[i%numContainers])
			}
		}()
	}

	wg.Wait()

	// The registry must still be in a valid, readable state -- Backends()
	// must not panic, and every entry it reports must be independently
	// retrievable via Get (no torn/partial entries).
	for _, b := range m.registry.Backends() {
		got, ok := m.registry.Get(b.ID)
		if !ok {
			t.Errorf("registry.Backends() listed id %q but Get(%q) reports not found", b.ID, b.ID)
			continue
		}
		if got.Addr == "" {
			t.Errorf("registry entry %q has an empty Addr after concurrent swap/deregister stress", b.ID)
		}
	}
}

// TestConcurrent_SwapBackendSameContainerIDRepeatedly is a narrower variant
// isolating the highest-contention case: many goroutines racing to swap
// backends for the EXACT SAME old/new container ID pair simultaneously
// (e.g. a duplicate Docker event triggering the same rotation twice). Only
// one Add of the new ID can succeed; the rest must fail cleanly (the
// existing, intentional "already registered" behavior), never panic, and
// never leave two entries for the same ID.
func TestConcurrent_SwapBackendSameContainerIDRepeatedly(t *testing.T) {
	m := NewManager(zaptest.NewLogger(t))
	t.Cleanup(func() { _ = m.Stop(0) })

	// Pre-register the "old" backend so every goroutine's swap has
	// something real to drain.
	if err := m.RegisterContainer("old-shared", "10.0.0.1", 9000, 8080); err != nil {
		t.Fatalf("RegisterContainer: %v", err)
	}

	const numGoroutines = 20
	var wg sync.WaitGroup
	var successCount int
	var mu sync.Mutex

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := m.SwapBackend("old-shared", "new-shared", "10.0.0.2", 8080, 9000)
			if err == nil {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if successCount != 1 {
		t.Errorf("expected exactly 1 of %d concurrent identical SwapBackend calls to succeed (registry.Add is the single source of truth for \"already registered\"), got %d", numGoroutines, successCount)
	}

	backends := m.registry.Backends()
	newCount := 0
	for _, b := range backends {
		if b.ID == "new-shared" {
			newCount++
		}
	}
	if newCount != 1 {
		t.Errorf("expected exactly 1 registry entry for \"new-shared\" after concurrent duplicate swaps, found %d", newCount)
	}
}
