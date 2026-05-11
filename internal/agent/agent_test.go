package agent

import (
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/resolver"
	"github.com/docker/docker/client"
)

// TestNewAgent verifies agent initialization
func TestNewAgent(t *testing.T) {
	mockDocker, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		// Use nil for testing if Docker not available
		mockDocker = nil
	}

	agent := NewAgent(mockDocker)

	if agent == nil {
		t.Fatal("NewAgent returned nil")
	}
	if agent.cache == nil {
		t.Fatal("Agent cache is nil")
	}
	if agent.Ready == nil {
		t.Fatal("Agent Ready channel is nil")
	}
	if agent.injected == nil {
		t.Fatal("Agent injected map is nil")
	}
	if len(agent.injected) != 0 {
		t.Errorf("Expected injected map to be empty, got %d items", len(agent.injected))
	}
}

// TestNewAgent_CacheInitialization verifies cache is created empty
func TestNewAgent_CacheInitialization(t *testing.T) {
	agent := NewAgent(nil)

	cache := agent.GetCache()
	if cache == nil {
		t.Fatal("GetCache returned nil")
	}

	// Cache should be empty initially
	_, exists := cache.Get("nonexistent")
	if exists {
		t.Error("Nonexistent key should not exist in empty cache")
	}
}

// TestNewAgent_ReadyChannelCanClose verifies Ready channel is functional
func TestNewAgent_ReadyChannelCanClose(t *testing.T) {
	agent := NewAgent(nil)

	// Ready channel should not be closed initially
	select {
	case <-agent.Ready:
		t.Fatal("Ready channel should not be closed initially")
	default:
		// Expected - channel is open
	}

	// Simulate agent startup (closing Ready channel)
	close(agent.Ready)

	// Now Ready should be closable
	select {
	case <-agent.Ready:
		// Expected - channel is now closed
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Ready channel should be closed after agent starts")
	}
}

// ============================================================================
// Cache Tests
// ============================================================================

// TestCache_NewCache creates empty cache
func TestCache_NewCache(t *testing.T) {
	cache := NewCache()

	if cache == nil {
		t.Fatal("NewCache returned nil")
	}
	if cache.secrets == nil {
		t.Fatal("Cache secrets is nil")
	}
	if cache.projects == nil {
		t.Fatal("Cache projects is nil")
	}
	if len(cache.secrets) != 0 {
		t.Errorf("Expected 0 secrets, got %d", len(cache.secrets))
	}
	if len(cache.projects) != 0 {
		t.Errorf("Expected 0 projects, got %d", len(cache.projects))
	}
}

// TestCache_Get_NotFound returns false when key doesn't exist
func TestCache_Get_NotFound(t *testing.T) {
	cache := NewCache()

	val, exists := cache.Get("nonexistent-hash")

	if exists {
		t.Error("Nonexistent hash should not exist")
	}
	if val != "" {
		t.Errorf("Expected empty value, got %q", val)
	}
}

// TestCache_Set_And_Get stores and retrieves secret
func TestCache_Set_And_Get(t *testing.T) {
	cache := NewCache()
	testHash := "abc123def456"
	testValue := "my-secret-password"

	// Create seed with secret
	seed := &resolver.AgentSeed{
		ProjectName: "test-project",
		SecretPool: map[string]string{
			testHash: testValue,
		},
		Services: make(map[string]resolver.ServiceSecrets),
	}

	cache.Seed(seed)

	// Verify secret is stored
	val, exists := cache.Get(testHash)
	if !exists {
		t.Error("Secret should exist in cache")
	}
	if val != testValue {
		t.Errorf("Expected %q, got %q", testValue, val)
	}
}

// TestCache_Seed_StoresProject stores project seed in cache
func TestCache_Seed_StoresProject(t *testing.T) {
	cache := NewCache()
	projectName := "test-project"

	seed := &resolver.AgentSeed{
		ProjectName: projectName,
		SecretPool:  make(map[string]string),
		Services:    make(map[string]resolver.ServiceSecrets),
	}

	cache.Seed(seed)

	// Verify project seed is stored
	retrievedSeed, exists := cache.GetSeed(projectName)
	if !exists {
		t.Error("Project seed should exist in cache")
	}
	if retrievedSeed.ProjectName != projectName {
		t.Errorf("Expected %q, got %q", projectName, retrievedSeed.ProjectName)
	}
}

// TestCache_GetSeed_NotFound returns false when project doesn't exist
func TestCache_GetSeed_NotFound(t *testing.T) {
	cache := NewCache()

	seed, exists := cache.GetSeed("nonexistent-project")

	if exists {
		t.Error("Nonexistent project should not exist")
	}
	if seed != nil {
		t.Error("Nonexistent project should return nil seed")
	}
}

// TestCache_Clear_RemovesProject removes project from cache
func TestCache_Clear_RemovesProject(t *testing.T) {
	cache := NewCache()
	projectName := "test-project"

	seed := &resolver.AgentSeed{
		ProjectName: projectName,
		SecretPool:  make(map[string]string),
		Services:    make(map[string]resolver.ServiceSecrets),
	}

	cache.Seed(seed)

	// Verify project exists
	_, exists := cache.GetSeed(projectName)
	if !exists {
		t.Error("Project should exist before clear")
	}

	// Clear project
	cache.Clear(projectName)

	// Verify project is removed
	_, exists = cache.GetSeed(projectName)
	if exists {
		t.Error("Project should not exist after clear")
	}
}

// TestCache_ConcurrentAccess verifies thread-safe operations
func TestCache_ConcurrentAccess(t *testing.T) {
	cache := NewCache()
	done := make(chan bool)

	// Concurrent writers
	for i := 0; i < 5; i++ {
		go func(index int) {
			seed := &resolver.AgentSeed{
				ProjectName: "project-" + string(rune(index)),
				SecretPool: map[string]string{
					"hash-" + string(rune(index)): "value-" + string(rune(index)),
				},
				Services: make(map[string]resolver.ServiceSecrets),
			}
			cache.Seed(seed)
			done <- true
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 5; i++ {
		go func() {
			cache.Get("hash-0")
			cache.GetSeed("project-0")
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestCache_Seed_WithNilSeed handles nil seed gracefully
func TestCache_Seed_WithNilSeed(t *testing.T) {
	cache := NewCache()

	// Should not panic
	cache.Seed(nil)

	// Cache should remain empty
	if len(cache.secrets) != 0 {
		t.Errorf("Expected 0 secrets, got %d", len(cache.secrets))
	}
	if len(cache.projects) != 0 {
		t.Errorf("Expected 0 projects, got %d", len(cache.projects))
	}
}

// TestCache_Seed_WithEmptyProjectName handles empty project name
func TestCache_Seed_WithEmptyProjectName(t *testing.T) {
	cache := NewCache()

	seed := &resolver.AgentSeed{
		ProjectName: "",
		SecretPool: map[string]string{
			"hash": "value",
		},
		Services: make(map[string]resolver.ServiceSecrets),
	}

	// Should not panic or store
	cache.Seed(seed)

	// Cache should remain empty
	if len(cache.projects) != 0 {
		t.Errorf("Expected 0 projects, got %d", len(cache.projects))
	}
}

// ============================================================================
// SecretCache Tests (TTL-based cache)
// ============================================================================

// TestSecretCache_NewSecretCache creates cache with TTL
func TestSecretCache_NewSecretCache(t *testing.T) {
	ttl := 1 * time.Hour
	sc := NewSecretCache(ttl)

	if sc == nil {
		t.Fatal("NewSecretCache returned nil")
	}
	if sc.ttl != ttl {
		t.Errorf("Expected TTL %v, got %v", ttl, sc.ttl)
	}
	if len(sc.items) != 0 {
		t.Errorf("Expected 0 items, got %d", len(sc.items))
	}
}

// TestSecretCache_Set_And_Get stores and retrieves data with TTL
func TestSecretCache_Set_And_Get(t *testing.T) {
	sc := NewSecretCache(1 * time.Hour)
	key := "test-key"
	data := map[string]string{
		"username": "admin",
		"password": "secret",
	}

	sc.Set(key, data)

	retrieved, exists := sc.Get(key)
	if !exists {
		t.Error("Key should exist in cache")
	}
	if len(retrieved) != len(data) {
		t.Errorf("Expected %d items, got %d", len(data), len(retrieved))
	}
	if retrieved["username"] != "admin" || retrieved["password"] != "secret" {
		t.Error("Data should match original")
	}
}

// TestSecretCache_Get_ExpiredItem returns false for expired items
func TestSecretCache_Get_ExpiredItem(t *testing.T) {
	// Use very short TTL to test expiration
	sc := NewSecretCache(1 * time.Millisecond)
	key := "test-key"
	data := map[string]string{"key": "value"}

	sc.Set(key, data)

	// Wait for expiration
	time.Sleep(10 * time.Millisecond)

	retrieved, exists := sc.Get(key)
	if exists {
		t.Error("Expired item should not exist")
	}
	if retrieved != nil {
		t.Error("Expired item should return nil")
	}
}

// TestSecretCache_Delete removes item from cache
func TestSecretCache_Delete(t *testing.T) {
	sc := NewSecretCache(1 * time.Hour)
	key := "test-key"
	data := map[string]string{"key": "value"}

	sc.Set(key, data)

	// Verify it exists
	_, exists := sc.Get(key)
	if !exists {
		t.Error("Key should exist before delete")
	}

	// Delete
	sc.Delete(key)

	// Verify it's gone
	_, exists = sc.Get(key)
	if exists {
		t.Error("Key should not exist after delete")
	}
}

// TestSecretCache_ListKeys returns all active keys
func TestSecretCache_ListKeys(t *testing.T) {
	sc := NewSecretCache(1 * time.Hour)

	sc.Set("key1", map[string]string{"data": "1"})
	sc.Set("key2", map[string]string{"data": "2"})
	sc.Set("key3", map[string]string{"data": "3"})

	keys := sc.ListKeys()

	if len(keys) != 3 {
		t.Errorf("Expected 3 keys, got %d", len(keys))
	}

	// Check each key exists
	found := make(map[string]bool)
	for _, k := range keys {
		found[k] = true
	}

	for _, expected := range []string{"key1", "key2", "key3"} {
		if !found[expected] {
			t.Errorf("Expected key %q not found", expected)
		}
	}
}

// TestSecretCache_ConcurrentAccess verifies thread-safety
func TestSecretCache_ConcurrentAccess(t *testing.T) {
	sc := NewSecretCache(1 * time.Hour)
	done := make(chan bool)

	// Concurrent writes
	for i := 0; i < 10; i++ {
		go func(index int) {
			key := "key-" + string(rune(index))
			data := map[string]string{"value": string(rune(index))}
			sc.Set(key, data)
			done <- true
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		go func(index int) {
			key := "key-" + string(rune(index%5))
			sc.Get(key)
			done <- true
		}(i)
	}

	// Wait for all
	for i := 0; i < 20; i++ {
		<-done
	}

	// Verify data integrity
	if len(sc.ListKeys()) == 0 {
		t.Error("Cache should have items after concurrent operations")
	}
}

// ============================================================================
// ComputeHash Tests
// ============================================================================

// TestComputeHash_GeneratesSHA256 produces SHA256 hash
func TestComputeHash_GeneratesSHA256(t *testing.T) {
	data := map[string]string{
		"key1": "value1",
		"key2": "value2",
	}

	hash := ComputeHash(data)

	// SHA256 hex string is 64 characters
	if len(hash) != 64 {
		t.Errorf("Expected hash length 64, got %d", len(hash))
	}

	// Should be hex (no special chars except 0-9a-f)
	for _, c := range hash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("Invalid hex character in hash: %c", c)
		}
	}
}

// TestComputeHash_Deterministic produces same hash for same data
func TestComputeHash_Deterministic(t *testing.T) {
	data := map[string]string{
		"key1": "value1",
		"key2": "value2",
	}

	hash1 := ComputeHash(data)
	hash2 := ComputeHash(data)

	if hash1 != hash2 {
		t.Errorf("Expected same hash for same data, got %q and %q", hash1, hash2)
	}
}

// TestComputeHash_DifferentForDifferentData produces different hash
func TestComputeHash_DifferentForDifferentData(t *testing.T) {
	data1 := map[string]string{"key": "value1"}
	data2 := map[string]string{"key": "value2"}

	hash1 := ComputeHash(data1)
	hash2 := ComputeHash(data2)

	if hash1 == hash2 {
		t.Errorf("Expected different hashes for different data, got %q", hash1)
	}
}

// TestComputeHash_EmptyData handles empty data
func TestComputeHash_EmptyData(t *testing.T) {
	data := make(map[string]string)

	hash := ComputeHash(data)

	if len(hash) != 64 {
		t.Errorf("Expected hash length 64, got %d", len(hash))
	}
}

// ============================================================================
// Integration Tests (Agent + Cache)
// ============================================================================

// TestAgent_GetCache returns same cache instance
func TestAgent_GetCache(t *testing.T) {
	agent := NewAgent(nil)

	cache1 := agent.GetCache()
	cache2 := agent.GetCache()

	if cache1 != cache2 {
		t.Error("GetCache should return same instance")
	}
}

// TestAgent_InjectedTracking tracks injected containers
func TestAgent_InjectedTracking(t *testing.T) {
	agent := NewAgent(nil)
	containerID := "abc123def456"

	agent.mu.Lock()
	agent.injected[containerID] = true
	agent.mu.Unlock()

	agent.mu.Lock()
	wasInjected, exists := agent.injected[containerID]
	agent.mu.Unlock()

	if !exists {
		t.Error("Container should be tracked in injected map")
	}
	if !wasInjected {
		t.Error("Container injection flag should be true")
	}
}

// TestAgent_ClearInjectedTracking removes injected container
func TestAgent_ClearInjectedTracking(t *testing.T) {
	agent := NewAgent(nil)
	containerID := "abc123def456"

	agent.mu.Lock()
	agent.injected[containerID] = true
	agent.mu.Unlock()

	agent.mu.Lock()
	delete(agent.injected, containerID)
	agent.mu.Unlock()

	agent.mu.Lock()
	_, exists := agent.injected[containerID]
	agent.mu.Unlock()

	if exists {
		t.Error("Container should be removed from injected map")
	}
}

// ============================================================================
// Helper Types for Testing (mock structures)
// ============================================================================

// AgentSeed represents the seed data passed to agent
type AgentSeed struct {
	ProjectName string
	SecretPool  map[string]string
	Services    map[string]ServiceSecrets
}

// ServiceSecrets represents secrets for a service
type ServiceSecrets struct {
	FileSecrets map[string]string
	UID         int
	GID         int
}
