package providers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/docker-secret-operator/dso/pkg/api"
	"github.com/docker-secret-operator/dso/pkg/config"
	pkgprovider "github.com/docker-secret-operator/dso/pkg/provider"
	"github.com/hashicorp/go-plugin"
	"go.uber.org/zap"
)

// staleAfter is how long an entry may go without a successful use before
// GetProvider re-initialises it.
const staleAfter = 10 * time.Minute

type StoreEntry struct {
	mu          sync.Mutex
	Provider    api.SecretProvider
	Client      *plugin.Client // nil for native (in-process) backends
	LastHealthy time.Time
	ConsecFails int
	MaxFailures int

	// configDigest identifies the provider configuration this entry was
	// built from (see configDigest). GetProvider never returns an entry whose
	// digest differs from the configuration it was asked for (PE-11). Empty
	// only for entries not created by GetProvider (e.g. seeded in tests).
	configDigest string
}

// isNative reports whether the entry wraps an in-process backend (file, env)
// that has no plugin subprocess. PE-29: such entries carry a nil Client and
// must never have plugin lifecycle methods called on them.
func (e *StoreEntry) isNative() bool {
	return e.Client == nil
}

// alive reports whether the entry's backend is still usable. Native backends
// live in the agent process and are always alive.
func (e *StoreEntry) alive() bool {
	if e.isNative() {
		return true
	}
	return !e.Client.Exited()
}

// kill terminates the plugin subprocess, if any. No-op for native backends.
func (e *StoreEntry) kill() {
	if e.isNative() {
		return
	}
	e.Client.Kill()
}

// initCall is one in-flight provider initialisation shared by every
// concurrent GetProvider caller for the same (name, config digest).
type initCall struct {
	done chan struct{}
	prov api.SecretProvider
	err  error
}

// SecretStoreManager caches provider connections by provider name. store is a
// sync.Map and each StoreEntry carries its own mu guarding its mutable health
// fields (REL-2). initMu guards only the inflight map used to single-flight
// provider initialisation (PE-11: closes the concurrent-rebuild window in
// which the last writer used to win).
type SecretStoreManager struct {
	logger *zap.Logger
	store  sync.Map

	initMu   sync.Mutex
	inflight map[string]*initCall
	nameMu   sync.Map // map[string]*sync.Mutex; serializes replacements for one provider name
}

func NewSecretStoreManager(logger *zap.Logger) *SecretStoreManager {
	return &SecretStoreManager{
		logger:   logger,
		inflight: make(map[string]*initCall),
	}
}

// configDigest returns a stable digest of the parts of a provider
// configuration that determine which backend a client talks to and with what
// credentials. Retry settings only affect how initialisation is attempted,
// not the resulting client, so they are excluded.
func configDigest(pCfg config.ProviderConfig) string {
	canonical := struct {
		Type       string            `json:"type"`
		Region     string            `json:"region"`
		AuthMethod string            `json:"auth_method"`
		AuthParams map[string]string `json:"auth_params"`
		Config     map[string]string `json:"config"`
	}{pCfg.Type, pCfg.Region, pCfg.Auth.Method, pCfg.Auth.Params, pCfg.Config}
	// json.Marshal sorts map keys, so the encoding is canonical. It cannot
	// fail for this struct of strings and string maps.
	b, _ := json.Marshal(canonical)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// GetProvider retrieves an active connection or initializes a new one with
// backoff retry. A cached entry is returned only if it is alive, not stale,
// and was built from exactly pCfg (by digest); otherwise it is evicted and a
// fresh connection is built from pCfg. Concurrent initialisations for the
// same (name, config) are collapsed into one.
func (s *SecretStoreManager) GetProvider(providerName string, pCfg config.ProviderConfig) (api.SecretProvider, error) {
	digest := configDigest(pCfg)
	if prov, ok := s.lookupLive(providerName, digest); ok {
		return prov, nil
	}

	key := providerName + "\x00" + digest
	s.initMu.Lock()
	if call, ok := s.inflight[key]; ok {
		s.initMu.Unlock()
		<-call.done
		return call.prov, call.err
	}
	call := &initCall{done: make(chan struct{})}
	s.inflight[key] = call
	s.initMu.Unlock()

	defer func() {
		s.initMu.Lock()
		delete(s.inflight, key)
		s.initMu.Unlock()
		close(call.done)
	}()

	// Serialize initializations for different configurations of the same
	// provider name. They cannot share a result, but they must not overwrite
	// each other's cache entry without releasing the displaced plugin.
	nameLock := s.providerNameLock(providerName)
	nameLock.Lock()
	defer nameLock.Unlock()

	// Double-check: another caller may have finished initialising this
	// provider between our lookup and claiming the in-flight slot.
	if prov, ok := s.lookupLive(providerName, digest); ok {
		call.prov = prov
		return prov, nil
	}

	call.prov, call.err = s.initProvider(providerName, digest, pCfg)
	return call.prov, call.err
}

func (s *SecretStoreManager) providerNameLock(providerName string) *sync.Mutex {
	lock, _ := s.nameMu.LoadOrStore(providerName, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

// lookupLive returns the cached provider for providerName if it is usable for
// a configuration with the given digest, evicting it otherwise.
func (s *SecretStoreManager) lookupLive(providerName, digest string) (api.SecretProvider, bool) {
	val, ok := s.store.Load(providerName)
	if !ok {
		return nil, false
	}
	entry := val.(*StoreEntry)
	// LastHealthy is mutated under entry.mu by MarkProviderHealthy/
	// MarkProviderFailure, so it must be read under the same lock (REL-2).
	entry.mu.Lock()
	lastHealthy := entry.LastHealthy
	entry.mu.Unlock()

	switch {
	case entry.configDigest != "" && entry.configDigest != digest:
		s.logger.Warn("Provider configuration changed, rebuilding connection",
			zap.String("provider", providerName))
	case time.Since(lastHealthy) > staleAfter:
		s.logger.Warn("Provider connection may be stale, reconnecting",
			zap.String("provider", providerName),
			zap.Duration("lastHealthy", time.Since(lastHealthy)))
	case !entry.alive():
		// Plugin process has crashed; detect and recover automatically
		s.logger.Warn("Provider plugin process crashed, recovering",
			zap.String("provider", providerName))
	default:
		return entry.Provider, true
	}
	s.evict(providerName, entry)
	return nil, false
}

// evict removes entry from the store if it is still the current entry for
// providerName, and kills its plugin subprocess. CompareAndDelete ensures a
// caller holding an outdated entry can never remove a freshly built one.
func (s *SecretStoreManager) evict(providerName string, entry *StoreEntry) {
	if s.store.CompareAndDelete(providerName, entry) {
		entry.kill()
	}
}

func (s *SecretStoreManager) initProvider(providerName, digest string, pCfg config.ProviderConfig) (api.SecretProvider, error) {
	providerLogger := s.logger.With(zap.String("provider", providerName), zap.String("type", pCfg.Type))
	providerLogger.Info("Initializing new persistent provider connection")

	prov, client, err := loadWithRetry(pCfg, providerLogger)
	if err != nil {
		return nil, fmt.Errorf("provider %s initialization %w", providerName, err)
	}

	entry := &StoreEntry{
		Provider:     prov,
		Client:       client,
		LastHealthy:  time.Now(),
		ConsecFails:  0,
		MaxFailures:  5,
		configDigest: digest,
	}
	if previous, loaded := s.store.Swap(providerName, entry); loaded {
		previous.(*StoreEntry).kill()
	}

	providerLogger.Info("Provider connection established successfully")
	return prov, nil
}

// loaderConfig merges the provider-specific config with region/auth settings
// into the flat map pkg/provider.LoadProvider expects.
func loaderConfig(pCfg config.ProviderConfig) map[string]string {
	out := make(map[string]string, len(pCfg.Config)+2)
	for k, v := range pCfg.Config {
		out[k] = v
	}
	if pCfg.Region != "" {
		out["region"] = pCfg.Region
	}
	if pCfg.Auth.Method != "" {
		out["auth_method"] = pCfg.Auth.Method
		for mk, mv := range pCfg.Auth.Params {
			out["auth_param_"+mk] = mv
		}
	}
	return out
}

// loadWithRetry loads a provider, retrying with exponential backoff and
// jitter according to pCfg.Retry (default 3 attempts, 2s base).
func loadWithRetry(pCfg config.ProviderConfig, providerLogger *zap.Logger) (api.SecretProvider, *plugin.Client, error) {
	attempts := 3
	if pCfg.Retry.Attempts > 0 {
		attempts = pCfg.Retry.Attempts
	}

	baseBackoff := 2 * time.Second
	if pCfg.Retry.Backoff != "" {
		if d, err := time.ParseDuration(pCfg.Retry.Backoff); err == nil {
			baseBackoff = d
		}
	}

	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		var prov api.SecretProvider
		var client *plugin.Client
		prov, client, err = pkgprovider.LoadProvider(pCfg.Type, loaderConfig(pCfg))
		if err == nil {
			return prov, client, nil
		}

		if attempt < attempts {
			// Exponential backoff: delay = base * 2^(attempt-1)
			delay := baseBackoff * time.Duration(1<<(attempt-1))
			jitter := time.Duration(secureJitterMillis()) * time.Millisecond // #nosec G115 -- secureJitterMillis returns 0-99.
			totalDelay := delay + jitter

			providerLogger.Warn("Failed to initialize provider, applying backoff",
				zap.Error(err),
				zap.Int("attempt", attempt),
				zap.Duration("retry_in", totalDelay))

			time.Sleep(totalDelay)
		}
	}
	return nil, nil, fmt.Errorf("exhausted %d retries: %w", attempts, err)
}

// CheckConnectivity builds a throw-away provider from pCfg with a single
// attempt and tears it down again. It never reads or writes the shared
// provider cache, so a connectivity check can neither be served by nor
// replace the connection the agent uses for rotation (PE-11).
func (s *SecretStoreManager) CheckConnectivity(pCfg config.ProviderConfig) error {
	pCfg.Retry = config.RetryConfig{Attempts: 1}
	_, client, err := loadWithRetry(pCfg, s.logger.With(zap.String("type", pCfg.Type), zap.Bool("ephemeral", true)))
	if client != nil {
		client.Kill()
	}
	return err
}

// MarkProviderHealthy updates the last successful use timestamp
func (s *SecretStoreManager) MarkProviderHealthy(providerName string) {
	if val, ok := s.store.Load(providerName); ok {
		entry := val.(*StoreEntry)
		entry.mu.Lock()
		entry.LastHealthy = time.Now()
		entry.ConsecFails = 0
		entry.mu.Unlock()
	}
}

// MarkProviderFailure tracks consecutive failures and removes provider if threshold exceeded
func (s *SecretStoreManager) MarkProviderFailure(providerName string) {
	if val, ok := s.store.Load(providerName); ok {
		entry := val.(*StoreEntry)
		entry.mu.Lock()
		entry.ConsecFails++
		exceeded := entry.ConsecFails >= entry.MaxFailures
		failures := entry.ConsecFails
		entry.mu.Unlock()

		if exceeded {
			s.logger.Error("Provider exhausted failure threshold, removing connection",
				zap.String("provider", providerName),
				zap.Int("failures", failures))
			// This request may have observed an older entry while a fresh
			// connection was installed. Remove and stop only the exact entry
			// whose failure count crossed the threshold.
			if s.store.CompareAndDelete(providerName, entry) {
				entry.kill()
			}
		}
	}
}

// Health reports what is currently known about a provider WITHOUT
// triggering a connection attempt (unlike GetProvider). known is false when
// this provider has never actually been used yet — a distinct state from
// unhealthy, since an unused provider isn't necessarily broken.
func (s *SecretStoreManager) Health(providerName string) (healthy bool, known bool) {
	val, ok := s.store.Load(providerName)
	if !ok {
		return false, false
	}
	entry := val.(*StoreEntry)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	healthy = time.Since(entry.LastHealthy) < staleAfter && entry.ConsecFails < entry.MaxFailures
	return healthy, true
}

func secureJitterMillis() uint64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return uint64(time.Now().UnixNano() % 100)
	}
	return binary.LittleEndian.Uint64(b[:]) % 100
}

// Shutdown cleanly kills all active provider plugin children. Native
// backends have no subprocess and are skipped (PE-29).
func (s *SecretStoreManager) Shutdown() {
	s.store.Range(func(key, value interface{}) bool {
		value.(*StoreEntry).kill()
		return true
	})
}
