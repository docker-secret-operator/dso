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
	Data      map[string]string
	Hash      string
	ExpiresAt time.Time
}

// ComputeHash generates a secure SHA-256 hash of the input data
func ComputeHash(data map[string]string) string {
	b, _ := json.Marshal(data)
	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:])
}

// SecretCache provides a TTL cache for external secrets (from providers)
type SecretCache struct {
	mu    sync.RWMutex
	items map[string]CacheItem
	ttl   time.Duration
}

func NewSecretCache(ttl time.Duration) *SecretCache {
	return &SecretCache{
		items: make(map[string]CacheItem),
		ttl:   ttl,
	}
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

	return item.Data, true
}

func (c *SecretCache) Set(key string, data map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[key] = CacheItem{
		Data:      data,
		Hash:      ComputeHash(data),
		ExpiresAt: time.Now().Add(c.ttl),
	}
}

func (c *SecretCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
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

// Cache holds the in-memory state of active DSO Native Vault secrets.
// It acts as the secure intermediary between the CLI and the Agent runtime.
type Cache struct {
	secrets  map[string]string // hash → plaintext value
	projects map[string]*resolver.AgentSeed
	mu       sync.RWMutex
}

// NewCache initializes an empty thread-safe cache for Native Vault secrets.
func NewCache() *Cache {
	return &Cache{
		secrets:  make(map[string]string),
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
		c.secrets[hash] = value
	}
}

// Get retrieves a plaintext secret by its pool hash.
func (c *Cache) Get(hash string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	val, ok := c.secrets[hash]
	return val, ok
}

// GetSeed retrieves the current deployment tracking state for a project.
func (c *Cache) GetSeed(project string) (*resolver.AgentSeed, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	seed, ok := c.projects[project]
	return seed, ok
}

// Clear safely removes a project's tracked state from the cache.
func (c *Cache) Clear(project string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.projects, project)
}
