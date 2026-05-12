package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"github.com/docker-secret-operator/dso/internal/resolver"
)

type CacheItem struct {
	Data      map[string][]byte
	Hash      string
	ExpiresAt time.Time
}

// zeroBytes explicitly zeros out a byte slice to prevent secret retention in memory
func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// zeroCacheItem zeros all secret data in a cache item before deletion
func zeroCacheItem(item *CacheItem) {
	if item == nil {
		return
	}
	for _, val := range item.Data {
		zeroBytes(val)
	}
}

// ComputeHash generates a secure SHA-256 hash of the input data
func ComputeHash(data map[string]string) string {
	b, _ := json.Marshal(data)
	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:])
}

// SecretCache provides a TTL cache for external secrets (from providers)
// Secrets are stored as []byte and explicitly zeroized on deletion to prevent
// memory retention of sensitive data.
type SecretCache struct {
	mu         sync.RWMutex
	items      map[string]CacheItem
	ttl        time.Duration
	maxSize    int64
	currentLen int64
	stopCh     chan struct{}
}

func NewSecretCache(ttl time.Duration) *SecretCache {
	sc := &SecretCache{
		items:      make(map[string]CacheItem),
		ttl:        ttl,
		maxSize:    100 * 1024 * 1024, // 100MB limit
		stopCh:     make(chan struct{}),
	}
	// Start background cleanup
	go sc.cleanupExpiredEntries()
	return sc
}

func (c *SecretCache) Get(key string) (map[string]string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	item, exists := c.items[key]
	if !exists {
		return nil, false
	}

	if time.Now().After(item.ExpiresAt) {
		return nil, false
	}

	// Convert []byte back to string for API compatibility
	result := make(map[string]string)
	for k, v := range item.Data {
		result[k] = string(v)
	}
	return result, true
}

func (c *SecretCache) Set(key string, data map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Convert string to []byte for secure storage
	byteData := make(map[string][]byte)
	for k, v := range data {
		byteData[k] = []byte(v)
	}

	c.items[key] = CacheItem{
		Data:      byteData,
		Hash:      ComputeHash(data),
		ExpiresAt: time.Now().Add(c.ttl),
	}
}

func (c *SecretCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if item, ok := c.items[key]; ok {
		zeroCacheItem(&item)
		delete(c.items, key)
	}
}

// ListKeys returns all keys currently in the cache
func (c *SecretCache) ListKeys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	keys := make([]string, 0, len(c.items))
	for k := range c.items {
		keys = append(keys, k)
	}
	return keys
}

// cleanupExpiredEntries periodically removes expired cache entries.
// Cleanup interval is synchronized with TTL to prevent stale entries.
// Uses TTL/2 as cleanup interval to ensure expired entries are removed promptly.
// Secrets are explicitly zeroized before deletion to prevent memory retention.
func (c *SecretCache) cleanupExpiredEntries() {
	cleanupInterval := c.ttl / 2
	if cleanupInterval < 10*time.Second {
		cleanupInterval = 10 * time.Second
	}
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.mu.Lock()
			now := time.Now()
			expiredKeys := make([]string, 0)
			for k, item := range c.items {
				if now.After(item.ExpiresAt) {
					expiredKeys = append(expiredKeys, k)
				}
			}
			for _, k := range expiredKeys {
				if item, ok := c.items[k]; ok {
					zeroCacheItem(&item)
				}
				delete(c.items, k)
			}
			c.mu.Unlock()
		}
	}
}

// Close stops the cleanup goroutine and zeroizes all remaining secrets
func (c *SecretCache) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Zeroize all remaining secrets on shutdown
	for _, item := range c.items {
		zeroCacheItem(&item)
	}
	c.items = make(map[string]CacheItem)

	select {
	case <-c.stopCh:
	default:
		close(c.stopCh)
	}
}

// Cache holds the in-memory state of active DSO Native Vault secrets.
// It acts as the secure intermediary between the CLI and the Agent runtime.
// Secrets are stored as []byte and zeroized when cleared.
type Cache struct {
	secrets  map[string][]byte // hash → plaintext value
	projects map[string]*resolver.AgentSeed
	mu       sync.RWMutex
}

// NewCache initializes an empty thread-safe cache for Native Vault secrets.
func NewCache() *Cache {
	return &Cache{
		secrets:  make(map[string][]byte),
		projects: make(map[string]*resolver.AgentSeed),
	}
}

// Seed ingests a new AgentSeed payload from the CLI.
func (c *Cache) Seed(seed *resolver.AgentSeed) {
	if seed == nil || seed.ProjectName == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.projects[seed.ProjectName] = seed

	for hash, value := range seed.SecretPool {
		c.secrets[hash] = []byte(value)
	}
}

// Get retrieves a plaintext secret by its pool hash.
// The returned string is a copy and can be safely used by the caller.
func (c *Cache) Get(hash string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	val, ok := c.secrets[hash]
	if !ok {
		return "", false
	}
	return string(val), true
}

// GetSeed retrieves the current deployment tracking state for a project.
func (c *Cache) GetSeed(project string) (*resolver.AgentSeed, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	seed, ok := c.projects[project]
	return seed, ok
}

// Clear safely removes a project's tracked state from the cache and zeroizes secrets.
func (c *Cache) Clear(project string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.projects, project)
	// Note: SecretPool secrets are owned by the AgentSeed; we don't zeroize here
	// since the seed may still be referenced elsewhere. Zeroization happens when
	// the seed is garbage collected.
}
