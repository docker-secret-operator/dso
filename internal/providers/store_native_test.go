package providers

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/hashicorp/go-plugin"
	"go.uber.org/zap/zaptest"
)

// PE-29 regression: native (in-process) backends are cached with a nil
// *plugin.Client. The cache-hit path used to call entry.Client.Exited() on
// that nil pointer, panicking inside the caller (an RPC handler or a poll
// goroutine), which crashed the agent on the SECOND lookup of any native
// provider.
func TestGetProvider_NativeBackend_RepeatedLookupDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "placeholder"), []byte(`{"value":"synthetic-not-a-secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		cfg  config.ProviderConfig
	}{
		{"env", config.ProviderConfig{Type: "env"}},
		{"file", config.ProviderConfig{Type: "file", Config: map[string]string{"path": dir}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewSecretStoreManager(zaptest.NewLogger(t))
			first, err := s.GetProvider("native-"+tc.name, tc.cfg)
			if err != nil {
				t.Fatalf("first lookup: %v", err)
			}
			for i := 0; i < 3; i++ {
				got, err := s.GetProvider("native-"+tc.name, tc.cfg)
				if err != nil {
					t.Fatalf("lookup %d: %v", i+2, err)
				}
				if got != first {
					t.Fatalf("lookup %d: expected the cached native provider to be reused", i+2)
				}
			}
		})
	}
}

// PE-29: a stale native entry (no successful use for >10 minutes) must be
// re-initialised without dereferencing its nil client.
func TestGetProvider_NativeBackend_StaleEntryDoesNotPanic(t *testing.T) {
	s := NewSecretStoreManager(zaptest.NewLogger(t))
	cfg := config.ProviderConfig{Type: "env"}
	if _, err := s.GetProvider("native-env", cfg); err != nil {
		t.Fatal(err)
	}
	val, _ := s.store.Load("native-env")
	entry := val.(*StoreEntry)
	entry.mu.Lock()
	entry.LastHealthy = time.Now().Add(-30 * time.Minute)
	entry.mu.Unlock()

	if _, err := s.GetProvider("native-env", cfg); err != nil {
		t.Fatalf("stale re-init: %v", err)
	}
}

// PE-29: Shutdown must kill plugin-backed entries and skip native ones
// (Kill on a nil *plugin.Client panics).
func TestShutdown_MixedNativeAndPluginEntries(t *testing.T) {
	s := NewSecretStoreManager(zaptest.NewLogger(t))
	if _, err := s.GetProvider("native-env", config.ProviderConfig{Type: "env"}); err != nil {
		t.Fatal(err)
	}
	// Zero-value plugin.Client: Kill() returns early without a subprocess.
	s.store.Store("plugin-prov", &StoreEntry{Client: &plugin.Client{}, LastHealthy: time.Now(), MaxFailures: 5})

	s.Shutdown()
}

// PE-29 under concurrency (run with -race): many goroutines repeatedly look
// up the same native provider while health marks are applied.
func TestGetProvider_NativeBackend_ConcurrentLookups(t *testing.T) {
	s := NewSecretStoreManager(zaptest.NewLogger(t))
	cfg := config.ProviderConfig{Type: "env"}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := s.GetProvider("native-env", cfg); err != nil {
				t.Errorf("lookup: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			s.MarkProviderHealthy("native-env")
		}()
	}
	wg.Wait()
}
