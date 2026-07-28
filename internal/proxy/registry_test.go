package proxy

import (
	"sync"
	"testing"
)

func TestRegistry_Add(t *testing.T) {
	t.Run("adds a valid backend", func(t *testing.T) {
		r := NewRegistry()
		if err := r.Add(Backend{ID: "c1", Addr: "10.0.0.1:80"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if r.Len() != 1 {
			t.Fatalf("expected 1 backend, got %d", r.Len())
		}
	})

	t.Run("rejects empty ID", func(t *testing.T) {
		r := NewRegistry()
		err := r.Add(Backend{ID: "", Addr: "10.0.0.1:80"})
		if err == nil {
			t.Fatal("expected error for empty ID")
		}
	})

	t.Run("rejects empty Addr", func(t *testing.T) {
		r := NewRegistry()
		err := r.Add(Backend{ID: "c1", Addr: ""})
		if err == nil {
			t.Fatal("expected error for empty Addr")
		}
	})

	t.Run("rejects duplicate ID", func(t *testing.T) {
		r := NewRegistry()
		if err := r.Add(Backend{ID: "c1", Addr: "10.0.0.1:80"}); err != nil {
			t.Fatal(err)
		}
		err := r.Add(Backend{ID: "c1", Addr: "10.0.0.2:80"})
		if err == nil {
			t.Fatal("expected error for duplicate ID")
		}
	})

	t.Run("sets AddedAt automatically", func(t *testing.T) {
		r := NewRegistry()
		if err := r.Add(Backend{ID: "c1", Addr: "10.0.0.1:80"}); err != nil {
			t.Fatal(err)
		}
		b, _ := r.Get("c1")
		if b.AddedAt.IsZero() {
			t.Error("expected AddedAt to be set")
		}
	})
}

func TestRegistry_Remove(t *testing.T) {
	t.Run("removes an existing backend", func(t *testing.T) {
		r := NewRegistry()
		_ = r.Add(Backend{ID: "c1", Addr: "10.0.0.1:80"})
		if err := r.Remove("c1"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if r.Len() != 0 {
			t.Fatalf("expected 0 backends after removal, got %d", r.Len())
		}
	})

	t.Run("errors when ID not found", func(t *testing.T) {
		r := NewRegistry()
		err := r.Remove("nonexistent")
		if err == nil {
			t.Fatal("expected error removing nonexistent backend")
		}
	})
}

func TestRegistry_SetDraining(t *testing.T) {
	t.Run("marks a backend draining without removing it", func(t *testing.T) {
		r := NewRegistry()
		_ = r.Add(Backend{ID: "c1", Addr: "10.0.0.1:80"})
		if err := r.SetDraining("c1"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		b, ok := r.Get("c1")
		if !ok {
			t.Fatal("expected backend still present after SetDraining")
		}
		if !b.Draining {
			t.Error("expected Draining to be true")
		}
	})

	t.Run("errors when ID not found", func(t *testing.T) {
		r := NewRegistry()
		err := r.SetDraining("nonexistent")
		if err == nil {
			t.Fatal("expected error for nonexistent backend")
		}
	})
}

func TestRegistry_Get(t *testing.T) {
	t.Run("returns a copy, not a live pointer", func(t *testing.T) {
		r := NewRegistry()
		_ = r.Add(Backend{ID: "c1", Addr: "10.0.0.1:80"})
		b1, _ := r.Get("c1")
		b1.Addr = "mutated"
		b2, _ := r.Get("c1")
		if b2.Addr == "mutated" {
			t.Error("Get should return an independent copy; mutating it must not affect the registry")
		}
	})

	t.Run("ok is false for missing ID", func(t *testing.T) {
		r := NewRegistry()
		_, ok := r.Get("nonexistent")
		if ok {
			t.Error("expected ok=false for missing backend")
		}
	})
}

func TestRegistry_BackendsAndActive(t *testing.T) {
	r := NewRegistry()
	_ = r.Add(Backend{ID: "b", Addr: "10.0.0.2:80"})
	_ = r.Add(Backend{ID: "a", Addr: "10.0.0.1:80"})
	_ = r.Add(Backend{ID: "c", Addr: "10.0.0.3:80"})
	_ = r.SetDraining("b")

	t.Run("Backends includes draining backends, sorted by ID", func(t *testing.T) {
		all := r.Backends()
		if len(all) != 3 {
			t.Fatalf("expected 3 backends, got %d", len(all))
		}
		if all[0].ID != "a" || all[1].ID != "b" || all[2].ID != "c" {
			t.Errorf("expected sorted [a b c], got [%s %s %s]", all[0].ID, all[1].ID, all[2].ID)
		}
	})

	t.Run("Active excludes draining backends, sorted by ID", func(t *testing.T) {
		active := r.Active()
		if len(active) != 2 {
			t.Fatalf("expected 2 active backends, got %d", len(active))
		}
		if active[0].ID != "a" || active[1].ID != "c" {
			t.Errorf("expected sorted [a c], got [%s %s]", active[0].ID, active[1].ID)
		}
	})

	t.Run("Active returns empty slice, not nil, when all draining", func(t *testing.T) {
		r2 := NewRegistry()
		_ = r2.Add(Backend{ID: "x", Addr: "10.0.0.1:80"})
		_ = r2.SetDraining("x")
		active := r2.Active()
		if len(active) != 0 {
			t.Fatalf("expected 0 active backends, got %d", len(active))
		}
	})
}

func TestRegistry_Len(t *testing.T) {
	r := NewRegistry()
	if r.Len() != 0 {
		t.Fatalf("expected 0 for empty registry, got %d", r.Len())
	}
	_ = r.Add(Backend{ID: "a", Addr: "10.0.0.1:80"})
	_ = r.Add(Backend{ID: "b", Addr: "10.0.0.2:80"})
	if r.Len() != 2 {
		t.Fatalf("expected 2, got %d", r.Len())
	}
	// Draining backends still count toward Len (only removed ones don't).
	_ = r.SetDraining("a")
	if r.Len() != 2 {
		t.Fatalf("expected Len to still be 2 after draining (not removing), got %d", r.Len())
	}
}

func TestBackend_RequestCounter(t *testing.T) {
	t.Run("Requests is 0 for a zero-value Backend (nil counter)", func(t *testing.T) {
		var b Backend
		if b.Requests() != 0 {
			t.Errorf("expected 0 for zero-value Backend, got %d", b.Requests())
		}
	})

	t.Run("IncrRequests on a zero-value Backend does not panic", func(t *testing.T) {
		var b Backend
		b.IncrRequests() // must not panic even though requests is nil
	})

	t.Run("registry.incrRequests increments the correct backend's counter", func(t *testing.T) {
		r := NewRegistry()
		_ = r.Add(Backend{ID: "c1", Addr: "10.0.0.1:80"})
		r.incrRequests("c1")
		r.incrRequests("c1")
		b, _ := r.Get("c1")
		if b.Requests() != 2 {
			t.Errorf("expected 2 requests, got %d", b.Requests())
		}
	})

	t.Run("registry.incrRequests on unknown ID is a safe no-op", func(t *testing.T) {
		r := NewRegistry()
		r.incrRequests("nonexistent") // must not panic
	})

	t.Run("MarshalledRequests aliases Requests", func(t *testing.T) {
		r := NewRegistry()
		_ = r.Add(Backend{ID: "c1", Addr: "10.0.0.1:80"})
		r.incrRequests("c1")
		b, _ := r.Get("c1")
		if b.MarshalledRequests() != b.Requests() {
			t.Error("MarshalledRequests should equal Requests")
		}
	})
}

// TestRegistry_ConcurrentAccess verifies the documented invariant: Add/Remove/
// SetDraining are atomic with respect to each other, and snapshot reads
// (Active, Backends, Get) are always consistent under concurrent mutation.
func TestRegistry_ConcurrentAccess(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := string(rune('a' + n%26))
			_ = r.Add(Backend{ID: id, Addr: "10.0.0.1:80"})
			_ = r.SetDraining(id)
			_, _ = r.Get(id)
			_ = r.Active()
			_ = r.Backends()
			r.incrRequests(id)
			_ = r.Remove(id)
		}(i)
	}
	wg.Wait()
}
