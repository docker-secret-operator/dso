package rotation

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestLockManager_AcquireRelease_InMemory(t *testing.T) {
	lm, err := NewLockManager("", zap.NewNop())
	if err != nil {
		t.Fatalf("NewLockManager: %v", err)
	}

	if err := lm.AcquireLock("svc-a", time.Second); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	lm.ReleaseLock("svc-a")

	// Re-acquiring after release must succeed.
	if err := lm.AcquireLock("svc-a", time.Second); err != nil {
		t.Fatalf("re-acquire after release failed: %v", err)
	}
	lm.ReleaseLock("svc-a")
}

func TestLockManager_Contention_TimesOut(t *testing.T) {
	lm, err := NewLockManager("", zap.NewNop())
	if err != nil {
		t.Fatalf("NewLockManager: %v", err)
	}

	if err := lm.AcquireLock("svc-b", time.Second); err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	defer lm.ReleaseLock("svc-b")

	// A second acquisition of the same key must time out while the first is held.
	start := time.Now()
	if err := lm.AcquireLock("svc-b", 50*time.Millisecond); err == nil {
		t.Fatal("expected timeout acquiring a held lock, got nil")
	}
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Errorf("timeout returned too early: %v", elapsed)
	}

	// A different key must not be blocked.
	if err := lm.AcquireLock("svc-c", 100*time.Millisecond); err != nil {
		t.Fatalf("independent key should acquire: %v", err)
	}
	lm.ReleaseLock("svc-c")
}

func TestFileLock_AcquireRelease(t *testing.T) {
	dir := t.TempDir()
	lm, err := NewLockManager(dir, zap.NewNop())
	if err != nil {
		t.Fatalf("NewLockManager: %v", err)
	}

	if err := lm.AcquireLock("container1", time.Second); err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	// The lock file must exist while held.
	lockFile := filepath.Join(dir, "container1.lock")
	if _, err := os.Stat(lockFile); err != nil {
		t.Fatalf("expected lock file to exist: %v", err)
	}

	lm.ReleaseLock("container1")

	// Acquire again after release to confirm the flock was dropped.
	if err := lm.AcquireLock("container1", time.Second); err != nil {
		t.Fatalf("re-acquire after release failed: %v", err)
	}
	lm.ReleaseLock("container1")
}

func TestFileLock_CrossHandleContention(t *testing.T) {
	dir := t.TempDir()
	logger := zap.NewNop()

	lm1, err := NewLockManager(dir, logger)
	if err != nil {
		t.Fatalf("NewLockManager: %v", err)
	}
	lm2, err := NewLockManager(dir, logger)
	if err != nil {
		t.Fatalf("NewLockManager: %v", err)
	}

	if err := lm1.AcquireLock("shared", time.Second); err != nil {
		t.Fatalf("lm1 acquire failed: %v", err)
	}

	// A separate file lock over the same directory/key uses a distinct open file
	// description, so flock must keep them mutually exclusive.
	if err := lm2.AcquireLock("shared", 100*time.Millisecond); err == nil {
		lm2.ReleaseLock("shared")
		t.Fatal("expected flock contention to time out, got nil")
	}

	lm1.ReleaseLock("shared")

	if err := lm2.AcquireLock("shared", time.Second); err != nil {
		t.Fatalf("lm2 acquire after lm1 release failed: %v", err)
	}
	lm2.ReleaseLock("shared")
}

// TestLockManager_ConcurrentMutualExclusion proves the lock serializes access
// under contention with no starvation (every goroutine eventually acquires within
// the generous timeout) and no data race (the critical section is exclusive).
// Run with -race to validate the second property.
func TestLockManager_ConcurrentMutualExclusion(t *testing.T) {
	lm, err := NewLockManager(t.TempDir(), zap.NewNop())
	if err != nil {
		t.Fatalf("NewLockManager: %v", err)
	}

	const goroutines = 20
	var (
		wg      sync.WaitGroup
		active  int32
		counter int // mutated only while the lock is held
	)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := lm.AcquireLock("shared-key", 10*time.Second); err != nil {
				t.Errorf("acquire failed (possible starvation): %v", err)
				return
			}
			defer lm.ReleaseLock("shared-key")

			if n := atomic.AddInt32(&active, 1); n != 1 {
				t.Errorf("mutual exclusion violated: %d concurrent holders", n)
			}
			counter++ // safe iff the lock is exclusive; -race catches a violation
			atomic.AddInt32(&active, -1)
		}()
	}
	wg.Wait()

	if counter != goroutines {
		t.Errorf("counter = %d, want %d (lost updates imply broken exclusion)", counter, goroutines)
	}
}

func TestTryLockWithTimeout(t *testing.T) {
	var mu sync.Mutex

	if !tryLockWithTimeout(&mu, time.Second) {
		t.Fatal("expected to lock a free mutex")
	}
	// mu is now held; a second attempt must fail within the timeout.
	if tryLockWithTimeout(&mu, 30*time.Millisecond) {
		t.Fatal("expected tryLock to fail on a held mutex")
	}
	mu.Unlock()
	if !tryLockWithTimeout(&mu, time.Second) {
		t.Fatal("expected to lock after unlock")
	}
	mu.Unlock()
}

func TestSanitizeLockName(t *testing.T) {
	cases := map[string]string{
		"plain":       "plain",
		"with/slash":  "with_slash",
		"a/b/c":       "a_b_c",
		"../escape":   "__escape",
		"back\\slash": "back_slash",
	}
	for in, want := range cases {
		if got := sanitizeLockName(in); got != want {
			t.Errorf("sanitizeLockName(%q) = %q, want %q", in, got, want)
		}
	}
	// The result must never contain a path separator.
	if filepath.Base(sanitizeLockName("a/b/c")) != "a_b_c" {
		t.Error("sanitized name still contains a separator")
	}
}
