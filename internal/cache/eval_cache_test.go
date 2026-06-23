package cache

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEntry_FreshGet(t *testing.T) {
	e := NewEntry[string](30 * time.Second)
	e.Set("hello", 5*time.Millisecond)

	v, ok := e.Get()
	if !ok || v != "hello" {
		t.Fatalf("expected (hello, true), got (%q, %v)", v, ok)
	}
}

func TestEntry_StaleAfterTTL(t *testing.T) {
	e := NewEntry[int](10 * time.Millisecond)
	e.Set(42, 0)

	time.Sleep(20 * time.Millisecond)

	_, ok := e.Get()
	if ok {
		t.Fatal("expected stale after TTL expired")
	}
}

func TestEntry_Invalidate(t *testing.T) {
	e := NewEntry[string](30 * time.Second)
	e.Set("value", 0)
	e.Invalidate()

	_, ok := e.Get()
	if ok {
		t.Fatal("expected invalid after Invalidate()")
	}
}

func TestEntry_GetOrCompute_CallsOnce(t *testing.T) {
	e := NewEntry[int](30 * time.Second)
	var calls int32

	fn := func(_ context.Context) int {
		atomic.AddInt32(&calls, 1)
		return 99
	}

	ctx := context.Background()
	v1 := e.GetOrCompute(ctx, fn)
	v2 := e.GetOrCompute(ctx, fn) // should hit cache

	if calls != 1 {
		t.Fatalf("expected fn called once, got %d", calls)
	}
	if v1 != 99 || v2 != 99 {
		t.Fatalf("unexpected values: %d, %d", v1, v2)
	}
}

func TestEntry_GetOrCompute_RecomputesAfterInvalidate(t *testing.T) {
	e := NewEntry[int](30 * time.Second)
	var calls int32

	fn := func(_ context.Context) int {
		atomic.AddInt32(&calls, 1)
		return int(calls)
	}
	ctx := context.Background()

	e.GetOrCompute(ctx, fn)
	e.Invalidate()
	e.GetOrCompute(ctx, fn)

	if calls != 2 {
		t.Fatalf("expected 2 calls after invalidate, got %d", calls)
	}
}

func TestEntry_ConcurrentGetOrCompute(t *testing.T) {
	// Many goroutines racing to compute; fn should be called very few times
	// (ideally once, due to double-checked locking).
	e := NewEntry[string](30 * time.Second)
	var calls int32
	fn := func(_ context.Context) string {
		time.Sleep(5 * time.Millisecond) // simulate evaluation latency
		atomic.AddInt32(&calls, 1)
		return "result"
	}

	const goroutines = 50
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make([]string, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = e.GetOrCompute(ctx, fn)
		}(i)
	}
	wg.Wait()

	for i, r := range results {
		if r != "result" {
			t.Errorf("goroutine %d got wrong result: %q", i, r)
		}
	}
	// Due to double-checked locking, fn should be called at most a small number
	// of times (not 50). In practice it's typically 1 or 2.
	if calls > 5 {
		t.Errorf("fn called %d times under concurrency — expected ≤5", calls)
	}
}

func TestEntry_IsStale(t *testing.T) {
	e := NewEntry[bool](10 * time.Millisecond)
	if !e.IsStale() {
		t.Fatal("expected stale before first Set")
	}
	e.Set(true, 0)
	if e.IsStale() {
		t.Fatal("expected fresh right after Set")
	}
	time.Sleep(20 * time.Millisecond)
	if !e.IsStale() {
		t.Fatal("expected stale after TTL")
	}
}

func TestEntry_LastEvalDuration(t *testing.T) {
	e := NewEntry[int](30 * time.Second)
	e.Set(1, 123*time.Millisecond)
	if e.LastEvalDuration() != 123*time.Millisecond {
		t.Fatalf("unexpected eval duration: %v", e.LastEvalDuration())
	}
}
