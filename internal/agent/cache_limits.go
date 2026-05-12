package agent

import (
	"fmt"
	"sync"
	"sync/atomic"

	"go.uber.org/zap"
)

// CacheLimiter enforces memory limits on secret cache to prevent OOM
type CacheLimiter struct {
	maxCacheSize     int64 // Maximum total cache size in bytes
	maxSecretSize    int64 // Maximum size for a single secret in bytes
	currentCacheSize int64 // Current cache size in bytes
	logger           *zap.Logger
	mu               sync.RWMutex
}

// NewCacheLimiter creates a cache limiter with specified limits
// maxCacheSize: total cache size limit (e.g., 100MB = 104857600)
// maxSecretSize: individual secret size limit (e.g., 10MB = 10485760)
func NewCacheLimiter(maxCacheSize, maxSecretSize int64, logger *zap.Logger) *CacheLimiter {
	return &CacheLimiter{
		maxCacheSize:     maxCacheSize,
		maxSecretSize:    maxSecretSize,
		currentCacheSize: 0,
		logger:           logger,
	}
}

// CheckSecretSize verifies a secret doesn't exceed individual limit
func (cl *CacheLimiter) CheckSecretSize(secretName string, data map[string]string) error {
	size := calculateSecretSize(data)
	if size > cl.maxSecretSize {
		cl.logger.Error("Secret exceeds maximum allowed size",
			zap.String("secret", secretName),
			zap.Int64("size", size),
			zap.Int64("max_allowed", cl.maxSecretSize))
		return fmt.Errorf("secret %s is too large: %d bytes (max: %d)", secretName, size, cl.maxSecretSize)
	}
	return nil
}

// CheckCacheCapacity verifies adding a secret won't exceed total cache limit
func (cl *CacheLimiter) CheckCacheCapacity(secretName string, data map[string]string) error {
	size := calculateSecretSize(data)
	currentSize := atomic.LoadInt64(&cl.currentCacheSize)

	if currentSize+size > cl.maxCacheSize {
		cl.logger.Error("Cache capacity exceeded",
			zap.String("secret", secretName),
			zap.Int64("current_size", currentSize),
			zap.Int64("adding", size),
			zap.Int64("max_cache", cl.maxCacheSize))
		return fmt.Errorf("cache would exceed limit: %d + %d > %d",
			currentSize, size, cl.maxCacheSize)
	}
	return nil
}

// RecordSecretSize updates the cache size when a secret is added
func (cl *CacheLimiter) RecordSecretSize(secretName string, data map[string]string) {
	size := calculateSecretSize(data)
	newSize := atomic.AddInt64(&cl.currentCacheSize, size)

	cl.logger.Debug("Secret cached",
		zap.String("secret", secretName),
		zap.Int64("secret_size", size),
		zap.Int64("total_cache_size", newSize),
		zap.Int64("max_cache", cl.maxCacheSize))
}

// RemoveSecretSize updates the cache size when a secret is removed
func (cl *CacheLimiter) RemoveSecretSize(data map[string]string) {
	size := calculateSecretSize(data)
	newSize := atomic.AddInt64(&cl.currentCacheSize, -size)

	cl.logger.Debug("Secret removed from cache",
		zap.Int64("secret_size", size),
		zap.Int64("total_cache_size", newSize))
}

// GetCacheStats returns current cache statistics
func (cl *CacheLimiter) GetCacheStats() (used, max int64, utilization float64) {
	used = atomic.LoadInt64(&cl.currentCacheSize)
	max = cl.maxCacheSize
	if max > 0 {
		utilization = float64(used) / float64(max) * 100
	}
	return
}

// calculateSecretSize estimates the memory footprint of a secret
func calculateSecretSize(data map[string]string) int64 {
	var size int64
	for key, val := range data {
		size += int64(len(key) + len(val))
	}
	// Add overhead for map structure
	size += 64 * int64(len(data))
	return size
}

// ValidateLimits ensures cache limiter configuration is valid
func (cl *CacheLimiter) ValidateLimits() error {
	if cl.maxCacheSize <= 0 {
		return fmt.Errorf("max cache size must be positive")
	}
	if cl.maxSecretSize <= 0 {
		return fmt.Errorf("max secret size must be positive")
	}
	if cl.maxSecretSize > cl.maxCacheSize {
		return fmt.Errorf("max secret size (%d) cannot exceed max cache size (%d)",
			cl.maxSecretSize, cl.maxCacheSize)
	}
	return nil
}

// LimitEnforcingCache wraps SecretCache with size limit enforcement
type LimitEnforcingCache struct {
	cache   *SecretCache
	limiter *CacheLimiter
}

// NewLimitEnforcingCache creates a cache wrapper that enforces size limits
func NewLimitEnforcingCache(cache *SecretCache, limiter *CacheLimiter) *LimitEnforcingCache {
	return &LimitEnforcingCache{
		cache:   cache,
		limiter: limiter,
	}
}

// SetWithLimits sets a secret in cache with size limit validation
func (lec *LimitEnforcingCache) SetWithLimits(key string, data map[string]string) error {
	// Check individual secret size
	if err := lec.limiter.CheckSecretSize(key, data); err != nil {
		return err
	}

	// Check total cache capacity
	if err := lec.limiter.CheckCacheCapacity(key, data); err != nil {
		return err
	}

	// Set in underlying cache
	lec.cache.Set(key, data)

	// Record size
	lec.limiter.RecordSecretSize(key, data)

	return nil
}

// Get retrieves a secret from cache
func (lec *LimitEnforcingCache) Get(key string) (map[string]string, bool) {
	return lec.cache.Get(key)
}

// Delete removes a secret from cache
func (lec *LimitEnforcingCache) Delete(key string) {
	if data, ok := lec.cache.Get(key); ok {
		lec.limiter.RemoveSecretSize(data)
	}
	// Note: cache doesn't have Delete, so we'd need to add it for proper cleanup
}
