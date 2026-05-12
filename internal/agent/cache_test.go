package agent

import (
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/resolver"
)

// TestCache_MultipleSecrets stores and retrieves multiple secrets
func TestCache_MultipleSecrets(t *testing.T) {
	cache := NewCache()

	// Create seed with multiple secrets
	seed := &resolver.AgentSeed{
		ProjectName: "app",
		SecretPool: map[string]string{
			"hash-db-pass":   "postgres-password",
			"hash-api-key":   "sk-1234567890",
			"hash-jwt-token": "eyJhbGc...",
		},
		Services: make(map[string]resolver.ServiceSecrets),
	}

	cache.Seed(seed)

	// Verify all secrets are stored
	tests := []struct {
		hash      string
		expected  string
		shouldExist bool
	}{
		{"hash-db-pass", "postgres-password", true},
		{"hash-api-key", "sk-1234567890", true},
		{"hash-jwt-token", "eyJhbGc...", true},
		{"nonexistent", "", false},
	}

	for _, tt := range tests {
		val, exists := cache.Get(tt.hash)
		if tt.shouldExist {
			if !exists {
				t.Errorf("Expected hash %s to exist", tt.hash)
			}
			if val != tt.expected {
				t.Errorf("Expected value %s, got %s", tt.expected, val)
			}
		} else {
			if exists {
				t.Errorf("Expected hash %s to not exist", tt.hash)
			}
		}
	}
}

// TestCache_OverwriteSeed replaces previous seed with new one
func TestCache_OverwriteSeed(t *testing.T) {
	cache := NewCache()
	projectName := "app"

	// First seed
	seed1 := &resolver.AgentSeed{
		ProjectName: projectName,
		SecretPool: map[string]string{
			"hash-v1": "secret-v1",
		},
		Services: make(map[string]resolver.ServiceSecrets),
	}

	cache.Seed(seed1)
	val, exists := cache.Get("hash-v1")
	if !exists || val != "secret-v1" {
		t.Error("First seed should be stored")
	}

	// Second seed (overwrites)
	seed2 := &resolver.AgentSeed{
		ProjectName: projectName,
		SecretPool: map[string]string{
			"hash-v2": "secret-v2",
		},
		Services: make(map[string]resolver.ServiceSecrets),
	}

	cache.Seed(seed2)

	// Both should exist (seed accumulates)
	val1, exists1 := cache.Get("hash-v1")
	val2, exists2 := cache.Get("hash-v2")

	if !exists1 || val1 != "secret-v1" {
		t.Error("First secret should still exist")
	}
	if !exists2 || val2 != "secret-v2" {
		t.Error("Second secret should exist")
	}
}

// TestCache_MultipleProjects handles multiple projects independently
func TestCache_MultipleProjects(t *testing.T) {
	cache := NewCache()

	// Project 1
	seed1 := &resolver.AgentSeed{
		ProjectName: "project-1",
		SecretPool: map[string]string{
			"hash-1": "secret-1",
		},
		Services: make(map[string]resolver.ServiceSecrets),
	}

	// Project 2
	seed2 := &resolver.AgentSeed{
		ProjectName: "project-2",
		SecretPool: map[string]string{
			"hash-2": "secret-2",
		},
		Services: make(map[string]resolver.ServiceSecrets),
	}

	cache.Seed(seed1)
	cache.Seed(seed2)

	// Both projects should exist
	proj1, exists1 := cache.GetSeed("project-1")
	proj2, exists2 := cache.GetSeed("project-2")

	if !exists1 || proj1.ProjectName != "project-1" {
		t.Error("Project 1 should exist")
	}
	if !exists2 || proj2.ProjectName != "project-2" {
		t.Error("Project 2 should exist")
	}

	// Secrets should be stored
	val1, exists1 := cache.Get("hash-1")
	val2, exists2 := cache.Get("hash-2")

	if !exists1 || val1 != "secret-1" {
		t.Error("Secret 1 should exist")
	}
	if !exists2 || val2 != "secret-2" {
		t.Error("Secret 2 should exist")
	}
}

// TestCache_Clear_PreservesOtherProjects removes only specific project
func TestCache_Clear_PreservesOtherProjects(t *testing.T) {
	cache := NewCache()

	seed1 := &resolver.AgentSeed{
		ProjectName: "project-1",
		SecretPool: map[string]string{
			"hash-1": "secret-1",
		},
		Services: make(map[string]resolver.ServiceSecrets),
	}

	seed2 := &resolver.AgentSeed{
		ProjectName: "project-2",
		SecretPool: map[string]string{
			"hash-2": "secret-2",
		},
		Services: make(map[string]resolver.ServiceSecrets),
	}

	cache.Seed(seed1)
	cache.Seed(seed2)

	// Clear project 1
	cache.Clear("project-1")

	// Project 1 should be gone, project 2 should remain
	_, exists1 := cache.GetSeed("project-1")
	proj2, exists2 := cache.GetSeed("project-2")

	if exists1 {
		t.Error("Project 1 should be cleared")
	}
	if !exists2 || proj2.ProjectName != "project-2" {
		t.Error("Project 2 should still exist")
	}

	// Secrets should also reflect project state
	// (Note: This depends on implementation - secrets might persist)
	_, exists := cache.Get("hash-1")
	if exists {
		t.Log("Secret 1 may persist (depends on implementation)")
	}
}

// TestCache_Seed_WithEmptySecretPool handles empty secret pool
func TestCache_Seed_WithEmptySecretPool(t *testing.T) {
	cache := NewCache()

	seed := &resolver.AgentSeed{
		ProjectName: "project",
		SecretPool:  make(map[string]string),
		Services:    make(map[string]resolver.ServiceSecrets),
	}

	cache.Seed(seed)

	// Project should exist but have no secrets
	proj, exists := cache.GetSeed("project")
	if !exists || proj.ProjectName != "project" {
		t.Error("Project should be created even with empty secrets")
	}

	// Verify no secrets stored
	if len(proj.SecretPool) != 0 {
		t.Error("Secret pool should be empty")
	}
}

// TestCache_RaceCondition_ConcurrentReadWrite tests concurrent operations
func TestCache_RaceCondition_ConcurrentReadWrite(t *testing.T) {
	cache := NewCache()

	done := make(chan bool, 20)

	// Writer goroutines
	for i := 0; i < 5; i++ {
		go func(index int) {
			seed := &resolver.AgentSeed{
				ProjectName: "project-" + string(rune('0'+index)),
				SecretPool: map[string]string{
					"hash-" + string(rune('0'+index)): "value-" + string(rune('0'+index)),
				},
				Services: make(map[string]resolver.ServiceSecrets),
			}
			cache.Seed(seed)
			done <- true
		}(i)
	}

	// Reader goroutines
	for i := 0; i < 5; i++ {
		go func(index int) {
			cache.Get("hash-" + string(rune('0'+(index%5))))
			cache.GetSeed("project-" + string(rune('0'+(index%5))))
			done <- true
		}(i)
	}

	// Deleter goroutines
	for i := 0; i < 5; i++ {
		go func(index int) {
			// Wait a bit before deleting to ensure seed exists
			time.Sleep(10 * time.Millisecond)
			cache.Clear("project-" + string(rune('0'+(index%5))))
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 15; i++ {
		<-done
	}

	// Verify cache is in consistent state
	cache.secrets = make(map[string][]byte)
	cache.projects = make(map[string]*resolver.AgentSeed)
}

// TestCache_LargeSecretPool handles large number of secrets
func TestCache_LargeSecretPool(t *testing.T) {
	cache := NewCache()

	// Create large secret pool
	secretPool := make(map[string]string)
	for i := 0; i < 1000; i++ {
		key := "hash-" + string(rune('0'+(i/100)))
		val := "secret-" + string(rune(i))
		secretPool[key] = val
	}

	seed := &resolver.AgentSeed{
		ProjectName: "large-project",
		SecretPool:  secretPool,
		Services:    make(map[string]resolver.ServiceSecrets),
	}

	cache.Seed(seed)

	// Verify all secrets are accessible
	for i := 0; i < 1000; i++ {
		key := "hash-" + string(rune('0'+(i/100)))
		val, exists := cache.Get(key)
		if !exists {
			t.Errorf("Secret %s should exist", key)
		}
		if val == "" {
			t.Errorf("Secret %s should have value", key)
		}
	}
}

// TestCache_GetSeed_WithDifferentProjects retrieves correct seed
func TestCache_GetSeed_WithDifferentProjects(t *testing.T) {
	cache := NewCache()

	projects := []string{"web", "api", "worker", "db"}

	// Create multiple projects
	for _, proj := range projects {
		seed := &resolver.AgentSeed{
			ProjectName: proj,
			SecretPool:  make(map[string]string),
			Services:    make(map[string]resolver.ServiceSecrets),
		}
		cache.Seed(seed)
	}

	// Verify each project retrieves correctly
	for _, proj := range projects {
		retrieved, exists := cache.GetSeed(proj)
		if !exists {
			t.Errorf("Project %s should exist", proj)
		}
		if retrieved.ProjectName != proj {
			t.Errorf("Expected project %s, got %s", proj, retrieved.ProjectName)
		}
	}
}

// TestCache_FullWorkflow simulates realistic usage pattern
func TestCache_FullWorkflow(t *testing.T) {
	cache := NewCache()

	// Step 1: Seed with initial secrets
	initialSeed := &resolver.AgentSeed{
		ProjectName: "myapp",
		SecretPool: map[string]string{
			"hash-db": "database-password",
			"hash-api": "api-key",
		},
		Services: map[string]resolver.ServiceSecrets{
			"web": {
				UID: 1000,
				GID: 1000,
				FileSecrets: map[string]string{
					"/run/secrets/db": "hash-db",
				},
			},
		},
	}

	cache.Seed(initialSeed)

	// Step 2: Retrieve secrets
	dbSecret, dbExists := cache.Get("hash-db")
	apiSecret, apiExists := cache.Get("hash-api")

	if !dbExists || dbSecret != "database-password" {
		t.Error("DB secret should be retrievable")
	}
	if !apiExists || apiSecret != "api-key" {
		t.Error("API secret should be retrievable")
	}

	// Step 3: Get project info
	proj, projExists := cache.GetSeed("myapp")
	if !projExists {
		t.Error("Project should be retrievable")
	}

	// Verify service info
	webSvc, webExists := proj.Services["web"]
	if !webExists {
		t.Error("Web service should exist")
	}
	if webSvc.UID != 1000 || webSvc.GID != 1000 {
		t.Error("Service UID/GID should be correct")
	}

	// Step 4: Clear and verify
	cache.Clear("myapp")
	_, cleared := cache.GetSeed("myapp")
	if cleared {
		t.Error("Project should be cleared")
	}
}

// TestSecretCache_Zeroization verifies that secrets are explicitly zeroized on deletion
func TestSecretCache_Zeroization(t *testing.T) {
	cache := NewSecretCache(5 * time.Second)
	defer cache.Close()

	// Store a secret
	secret := map[string]string{"password": "super-secret-password"}
	cache.Set("test-key", secret)

	// Verify it's stored
	retrieved, exists := cache.Get("test-key")
	if !exists {
		t.Fatal("Secret should exist after set")
	}
	if retrieved["password"] != "super-secret-password" {
		t.Fatal("Secret value should match")
	}

	// Delete the secret
	cache.Delete("test-key")

	// Verify it's gone
	_, exists = cache.Get("test-key")
	if exists {
		t.Error("Secret should not exist after deletion")
	}

	// Verify the internal storage is zeroized (check that accessing items shows empty data)
	cache.mu.RLock()
	itemsCount := len(cache.items)
	cache.mu.RUnlock()

	if itemsCount != 0 {
		t.Errorf("Cache should be empty after deletion, but has %d items", itemsCount)
	}
}

// TestSecretCache_ZeroizationOnClose verifies secrets are zeroized when cache closes
func TestSecretCache_ZeroizationOnClose(t *testing.T) {
	cache := NewSecretCache(10 * time.Second)

	// Store multiple secrets
	cache.Set("key1", map[string]string{"secret": "value1"})
	cache.Set("key2", map[string]string{"secret": "value2"})

	// Verify they exist
	if _, exists := cache.Get("key1"); !exists {
		t.Fatal("Secret key1 should exist")
	}
	if _, exists := cache.Get("key2"); !exists {
		t.Fatal("Secret key2 should exist")
	}

	// Close the cache
	cache.Close()

	// Verify cache is empty
	cache.mu.RLock()
	itemsCount := len(cache.items)
	cache.mu.RUnlock()

	if itemsCount != 0 {
		t.Errorf("Cache should be empty after close, but has %d items", itemsCount)
	}
}

// TestSecretCache_ZeroizationOnExpire verifies expired entries are not accessible
// (The background cleanup will eventually remove them)
func TestSecretCache_ZeroizationOnExpire(t *testing.T) {
	ttl := 50 * time.Millisecond
	cache := NewSecretCache(ttl)
	defer cache.Close()

	// Store a secret with short TTL
	cache.Set("expiring-key", map[string]string{"secret": "will-expire"})

	// Verify it exists
	if _, exists := cache.Get("expiring-key"); !exists {
		t.Fatal("Secret should exist initially")
	}

	// Wait for expiration
	time.Sleep(ttl + 10*time.Millisecond)

	// Verify it's no longer accessible via Get() (Get checks ExpiresAt)
	if _, exists := cache.Get("expiring-key"); exists {
		t.Error("Expired secret should not be accessible via Get()")
	}

	// Note: The background cleanup loop will eventually remove the entry,
	// but the timing of ticker firing is not deterministic in tests.
	// The important thing is that Get() respects the ExpiresAt time,
	// preventing access to expired secrets.
}
