package proxy

import (
	"math"
	"sync"
	"testing"
)

func TestRouter_Next(t *testing.T) {
	t.Run("errors when no backends registered", func(t *testing.T) {
		r := NewRouter(NewRegistry())
		_, err := r.Next()
		if err == nil {
			t.Fatal("expected error when no backends are registered")
		}
	})

	t.Run("errors when all backends are draining", func(t *testing.T) {
		reg := NewRegistry()
		_ = reg.Add(Backend{ID: "c1", Addr: "10.0.0.1:80"})
		_ = reg.SetDraining("c1")
		r := NewRouter(reg)
		_, err := r.Next()
		if err == nil {
			t.Fatal("expected error when all backends are draining")
		}
	})

	t.Run("returns the single active backend", func(t *testing.T) {
		reg := NewRegistry()
		_ = reg.Add(Backend{ID: "c1", Addr: "10.0.0.1:80"})
		r := NewRouter(reg)
		b, err := r.Next()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if b.ID != "c1" {
			t.Errorf("expected c1, got %s", b.ID)
		}
	})

	t.Run("round-robins across multiple active backends", func(t *testing.T) {
		reg := NewRegistry()
		_ = reg.Add(Backend{ID: "a", Addr: "10.0.0.1:80"})
		_ = reg.Add(Backend{ID: "b", Addr: "10.0.0.2:80"})
		r := NewRouter(reg)

		seen := map[string]int{}
		for i := 0; i < 10; i++ {
			b, err := r.Next()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			seen[b.ID]++
		}
		if seen["a"] != 5 || seen["b"] != 5 {
			t.Errorf("expected even round-robin split (5/5), got a=%d b=%d", seen["a"], seen["b"])
		}
	})

	t.Run("skips draining backends", func(t *testing.T) {
		reg := NewRegistry()
		_ = reg.Add(Backend{ID: "a", Addr: "10.0.0.1:80"})
		_ = reg.Add(Backend{ID: "b", Addr: "10.0.0.2:80"})
		_ = reg.SetDraining("a")
		r := NewRouter(reg)

		for i := 0; i < 5; i++ {
			b, err := r.Next()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if b.ID != "b" {
				t.Errorf("expected only 'b' (non-draining), got %s", b.ID)
			}
		}
	})

	t.Run("increments the chosen backend's request counter", func(t *testing.T) {
		reg := NewRegistry()
		_ = reg.Add(Backend{ID: "c1", Addr: "10.0.0.1:80"})
		r := NewRouter(reg)

		for i := 0; i < 3; i++ {
			if _, err := r.Next(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		}
		b, _ := reg.Get("c1")
		if b.Requests() != 3 {
			t.Errorf("expected 3 requests recorded, got %d", b.Requests())
		}
	})

	t.Run("newly added backend becomes reachable immediately (zero-downtime swap semantics)", func(t *testing.T) {
		reg := NewRegistry()
		_ = reg.Add(Backend{ID: "old", Addr: "10.0.0.1:80"})
		r := NewRouter(reg)

		_ = reg.Add(Backend{ID: "new", Addr: "10.0.0.2:80"})
		_ = reg.SetDraining("old")

		b, err := r.Next()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if b.ID != "new" {
			t.Errorf("expected routing to switch to 'new' immediately after draining 'old', got %s", b.ID)
		}
	})
}

// TestRouter_Next_CounterNearOverflow is a regression test for a gosec
// G115-flagged integer overflow: Next() used to convert the (unbounded,
// ever-growing) uint64 counter to int *before* taking the modulo. Once the
// counter passed math.MaxInt64, that conversion produced a negative int,
// and active[negative] panics. Fixed by taking the modulo in uint64 space
// first, then narrowing only the already-bounded (< len(active)) result.
func TestRouter_Next_CounterNearOverflow(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Add(Backend{ID: "a", Addr: "10.0.0.1:80"})
	_ = reg.Add(Backend{ID: "b", Addr: "10.0.0.2:80"})
	_ = reg.Add(Backend{ID: "c", Addr: "10.0.0.3:80"})
	r := NewRouter(reg)

	// Set the internal counter to just below its uint64 max so the very
	// next Add(1) wraps past math.MaxInt64 -- exactly the value that would
	// have converted to a negative int under the old code.
	r.counter.Store(math.MaxUint64 - 1)

	for i := 0; i < 10; i++ {
		b, err := r.Next()
		if err != nil {
			t.Fatalf("unexpected error near counter overflow: %v", err)
		}
		if b == nil {
			t.Fatal("expected a non-nil backend near counter overflow")
		}
	}
}

// TestRouter_ConcurrentNext verifies the router's lock-free hot path (atomic
// counter + registry snapshot) is safe under concurrent use, and that the
// round-robin distribution remains fair under contention.
func TestRouter_ConcurrentNext(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Add(Backend{ID: "a", Addr: "10.0.0.1:80"})
	_ = reg.Add(Backend{ID: "b", Addr: "10.0.0.2:80"})
	r := NewRouter(reg)

	var wg sync.WaitGroup
	var mu sync.Mutex
	counts := map[string]int{}

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				b, err := r.Next()
				if err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}
				mu.Lock()
				counts[b.ID]++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	total := counts["a"] + counts["b"]
	if total != 1000 {
		t.Fatalf("expected 1000 total selections, got %d", total)
	}
	// Confirm the atomic counter distributed fairly (not all to one backend).
	if counts["a"] == 0 || counts["b"] == 0 {
		t.Errorf("expected both backends to receive traffic under concurrency, got a=%d b=%d", counts["a"], counts["b"])
	}
}
