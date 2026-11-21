package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/docker-secret-operator/dso/internal/providers"
	"github.com/docker-secret-operator/dso/pkg/config"
)

// G013 pinning test: a provider outage (GetSecret returning an error) must
// not be treated as a new, empty or changed secret version. The poll path
// skips ExecuteRotation entirely on error, so the last-known-good hash and
// cache entry stay intact and no rotation is dispatched; when the provider
// recovers with the SAME value, nothing rotates either.
//
// A dispatch would reach t.Reloader, which is nil here, so an erroneous
// dispatch fails loudly (panic in the rotation goroutine) rather than silently.
func TestPollOutageDoesNotDisturbKnownGoodState(t *testing.T) {
	core, logs := observer.New(zapcore.ErrorLevel)
	logger := zap.New(core)

	cache := NewSecretCache(time.Hour)
	t.Cleanup(cache.Close)
	store := providers.NewSecretStoreManager(logger)

	cfg := &config.Config{}
	te, err := NewTriggerEngineWithDirs(cache, store, nil, logger, cfg, nil, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(te.Stop)

	secretsDir := t.TempDir() // empty: every GetSecret fails ("outage")
	pCfg := config.ProviderConfig{
		Type:   "file",
		Config: map[string]string{"path": secretsDir},
		Retry:  config.RetryConfig{Backoff: "10ms"},
	}

	// Known-good state from a previous successful observation.
	good := map[string]string{"password": "known-good-value"}
	key := "local:db"
	te.secretHashes.Store(key, ComputeHash(good))
	cache.Set(key, good)

	if err := te.StartPolling("local", pCfg, config.SecretMapping{Name: "db", Provider: "local"}, 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	waitFor := func(n int) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for logs.FilterMessage("Secret fetch failed, will retry").Len() < n {
			if time.Now().After(deadline) {
				t.Fatalf("poller did not attempt %d failing fetches", n)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	waitFor(3)

	assertIntact := func(phase string) {
		t.Helper()
		h, ok := te.secretHashes.Load(key)
		if !ok || h.(string) != ComputeHash(good) {
			t.Fatalf("%s: last-known-good hash was disturbed (ok=%v)", phase, ok)
		}
		got, ok := cache.Get(key)
		if !ok || got["password"] != "known-good-value" {
			t.Fatalf("%s: cache entry was disturbed (ok=%v got=%v)", phase, ok, got)
		}
		if _, rotated := te.lastRotations.Load(key); rotated {
			t.Fatalf("%s: a rotation was dispatched", phase)
		}
	}
	assertIntact("during outage")

	// Provider recovers with the same value: still no rotation.
	if err := os.WriteFile(filepath.Join(secretsDir, "db.json"), []byte(`{"password":"known-good-value"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	assertIntact("after recovery with unchanged value")
}
