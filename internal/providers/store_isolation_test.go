package providers

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/docker-secret-operator/dso/pkg/config"
	"go.uber.org/zap/zaptest"
)

// PE-11 regression: the cache used to be keyed by provider name only and
// returned whatever entry occupied that name regardless of the configuration
// requested, so whoever initialised a name first (or after it went stale)
// decided which backend every later caller used. GetProvider must never hand
// back an entry built from a different configuration.
func TestGetProvider_NeverReturnsEntryBuiltFromDifferentConfig(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(dirA, "item"), []byte(`{"value":"synthetic-A"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirB, "item"), []byte(`{"value":"synthetic-B"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgA := config.ProviderConfig{Type: "file", Config: map[string]string{"path": dirA}}
	cfgB := config.ProviderConfig{Type: "file", Config: map[string]string{"path": dirB}}

	s := NewSecretStoreManager(zaptest.NewLogger(t))
	// "Attacker" initialises the name first with its own config.
	if _, err := s.GetProvider("prod", cfgB); err != nil {
		t.Fatal(err)
	}
	// The operator-configured lookup must get a client for cfgA.
	prov, err := s.GetProvider("prod", cfgA)
	if err != nil {
		t.Fatal(err)
	}
	data, err := prov.GetSecret("item")
	if err != nil {
		t.Fatal(err)
	}
	if data["value"] != "synthetic-A" {
		t.Fatalf("GetProvider returned an entry built from a different config (got %q)", data["value"])
	}
}

func TestConfigDigest_IgnoresRetryAndIsOrderIndependent(t *testing.T) {
	a := config.ProviderConfig{Type: "vault", Config: map[string]string{"x": "1", "y": "2"}}
	b := config.ProviderConfig{Type: "vault", Config: map[string]string{"y": "2", "x": "1"}, Retry: config.RetryConfig{Attempts: 1}}
	if configDigest(a) != configDigest(b) {
		t.Fatal("digest must ignore retry settings and map ordering")
	}
	c := config.ProviderConfig{Type: "vault", Config: map[string]string{"x": "1", "y": "3"}}
	if configDigest(a) == configDigest(c) {
		t.Fatal("digest must change when config changes")
	}
}

// CheckConnectivity must never populate (or replace) the shared cache.
func TestCheckConnectivity_DoesNotTouchCache(t *testing.T) {
	s := NewSecretStoreManager(zaptest.NewLogger(t))
	if err := s.CheckConnectivity(config.ProviderConfig{Type: "env"}); err != nil {
		t.Fatalf("check: %v", err)
	}
	if _, known := s.Health("env"); known {
		t.Fatal("connectivity check created a cache entry")
	}
	n := 0
	s.store.Range(func(_, _ interface{}) bool { n++; return true })
	if n != 0 {
		t.Fatalf("expected empty cache after connectivity check, found %d entries", n)
	}
	if err := s.CheckConnectivity(config.ProviderConfig{Type: "non-existent-provider"}); err == nil {
		t.Fatal("expected an error for an unknown provider type")
	}
}

// Single-flight: concurrent first lookups for the same name+config share one
// initialisation and therefore one provider instance (run with -race).
func TestGetProvider_ConcurrentFirstLookupsShareOneInit(t *testing.T) {
	s := NewSecretStoreManager(zaptest.NewLogger(t))
	dir := t.TempDir()
	cfg := config.ProviderConfig{Type: "file", Config: map[string]string{"path": dir}}

	const n = 32
	results := make([]interface{}, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p, err := s.GetProvider("shared", cfg)
			if err != nil {
				t.Errorf("lookup: %v", err)
				return
			}
			results[i] = p
		}(i)
	}
	wg.Wait()
	for i := 1; i < n; i++ {
		if results[i] != results[0] {
			t.Fatal("concurrent first lookups produced more than one provider instance")
		}
	}
}
