package agent

import (
	"context"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/pkg/config"
	"go.uber.org/zap"
)

// ---- helpers ---------------------------------------------------------------

func newTestLEC(t *testing.T, maxCache, maxSecret int64) (*LimitEnforcingCache, *CacheLimiter) {
	t.Helper()
	logger := zap.NewNop()
	sc := NewSecretCache(30 * time.Second)
	t.Cleanup(func() { sc.Close() })
	limiter := NewCacheLimiter(maxCache, maxSecret, logger)
	lec := NewLimitEnforcingCache(sc, limiter)
	return lec, limiter
}

// newTestEngine builds a minimal TriggerEngine that is sufficient for testing
// the cache-write path in ExecuteRotation. The rotation strategy is set to
// "none" so no Docker / Reloader / Config machinery is needed.
func newTestEngine(t *testing.T, sc *SecretCache, lec *LimitEnforcingCache) *TriggerEngine {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	eng := &TriggerEngine{
		Cache:      sc,
		LimitCache: lec,
		Logger:     zap.NewNop(),
		ctx:        ctx,
	}
	return eng
}

// noneMapping returns a SecretMapping with strategy "none" so ExecuteRotation
// skips the reloader goroutine (which requires Config/Docker).
func noneMapping() config.SecretMapping {
	return config.SecretMapping{
		Rotation: config.RotationConfigV2{
			Strategy: "none",
		},
	}
}

// ---- C1: LimitEnforcingCache.Delete ----------------------------------------

// TestLimitEnforcingCache_Delete_RemovesEntry verifies that Delete() actually
// removes the secret from the underlying SecretCache (C1 regression test).
func TestLimitEnforcingCache_Delete_RemovesEntry(t *testing.T) {
	lec, _ := newTestLEC(t, 10*1024*1024, 1*1024*1024)

	data := map[string]string{"password": "hunter2"}
	if err := lec.SetWithLimits("myapp:db", data); err != nil {
		t.Fatalf("SetWithLimits failed: %v", err)
	}

	if _, ok := lec.Get("myapp:db"); !ok {
		t.Fatal("expected secret to be present after set")
	}

	lec.Delete("myapp:db")

	// After Delete the entry must be gone from the underlying cache.
	if _, ok := lec.Get("myapp:db"); ok {
		t.Error("C1 regression: secret still present after LimitEnforcingCache.Delete()")
	}
}

// TestLimitEnforcingCache_Delete_NonExistent verifies deleting a missing key
// is a safe no-op.
func TestLimitEnforcingCache_Delete_NonExistent(t *testing.T) {
	lec, _ := newTestLEC(t, 10*1024*1024, 1*1024*1024)
	// Must not panic.
	lec.Delete("does-not-exist")
}

// TestLimitEnforcingCache_Delete_SizeAccounting verifies that after deletion
// the limiter size counter returns to zero so new secrets can be added.
func TestLimitEnforcingCache_Delete_SizeAccounting(t *testing.T) {
	// Small but sufficient for one tiny secret.
	lec, limiter := newTestLEC(t, 1024, 1024)

	data := map[string]string{"k": "v"}
	if err := lec.SetWithLimits("s1", data); err != nil {
		t.Fatalf("first set should succeed: %v", err)
	}

	used, _, _ := limiter.GetCacheStats()
	if used == 0 {
		t.Fatal("expected non-zero used bytes after set")
	}

	lec.Delete("s1")

	used, _, _ = limiter.GetCacheStats()
	if used != 0 {
		t.Errorf("expected zero used bytes after delete, got %d", used)
	}

	// Should be able to re-add the same secret without hitting the limit.
	if err := lec.SetWithLimits("s1", data); err != nil {
		t.Errorf("re-add after delete should succeed: %v", err)
	}
}

// ---- C3: ExecuteRotation routes through LimitEnforcingCache ----------------

// TestExecuteRotation_CacheLimitEnforced verifies that ExecuteRotation rejects
// an oversized secret when a LimitCache is attached and aborts without writing
// to the underlying SecretCache (C3 regression test).
func TestExecuteRotation_CacheLimitEnforced(t *testing.T) {
	sc := NewSecretCache(30 * time.Second)
	t.Cleanup(func() { sc.Close() })

	// 1-byte limit: any real secret will exceed it.
	limiter := NewCacheLimiter(1, 1, zap.NewNop())
	lec := NewLimitEnforcingCache(sc, limiter)

	eng := newTestEngine(t, sc, lec)

	bigSecret := map[string]string{"password": "super-secret-value"}
	eng.ExecuteRotation("aws", "myapp/db", bigSecret, noneMapping())

	cacheKey := "aws:myapp/db"
	if _, ok := sc.Get(cacheKey); ok {
		t.Error("C3 regression: oversized secret was written to cache despite limit enforcement")
	}
}

// TestExecuteRotation_NormalSecretWithLimitCache verifies that a normal-sized
// secret is cached when a LimitCache is attached.
func TestExecuteRotation_NormalSecretWithLimitCache(t *testing.T) {
	sc := NewSecretCache(30 * time.Second)
	t.Cleanup(func() { sc.Close() })

	limiter := NewCacheLimiter(10*1024*1024, 1*1024*1024, zap.NewNop())
	lec := NewLimitEnforcingCache(sc, limiter)

	eng := newTestEngine(t, sc, lec)

	data := map[string]string{"password": "s3cr3t"}
	eng.ExecuteRotation("aws", "myapp/db", data, noneMapping())

	if _, ok := sc.Get("aws:myapp/db"); !ok {
		t.Error("normal secret should be in cache after ExecuteRotation with LimitCache")
	}
}

// TestExecuteRotation_NoLimitCache verifies backward compatibility: without a
// LimitCache the secret is written directly via SecretCache.Set.
func TestExecuteRotation_NoLimitCache(t *testing.T) {
	sc := NewSecretCache(30 * time.Second)
	t.Cleanup(func() { sc.Close() })

	eng := newTestEngine(t, sc, nil) // nil LimitCache → legacy path

	data := map[string]string{"token": "abc123"}
	eng.ExecuteRotation("local", "app/token", data, noneMapping())

	if _, ok := sc.Get("local:app/token"); !ok {
		t.Error("secret should be cached when no LimitCache is configured")
	}
}
