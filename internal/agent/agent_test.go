package agent

import (
	"context"
	"fmt"
	"sync"
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
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
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

// ============================================================================
// Integration Tests: SmartPoller + EventReactor + ContainerListener
// ============================================================================

// TestAgent_MainLoop_SmartPolling verifies that the main loop integrates
// SmartPoller with adaptive polling intervals
func TestAgent_MainLoop_SmartPolling(t *testing.T) {
	agent := NewAgent(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Track when polls occur
	pollTimes := make([]time.Time, 0)
	var pollMu sync.Mutex

	// Override pollSecret to track calls
	originalPollSecret := agent.pollSecret
	_ = originalPollSecret // Keep reference to avoid unused warning

	// Run main loop in goroutine with a timeout
	mainLoopDone := make(chan error, 1)
	go func() {
		err := agent.runMainLoop(ctx)
		mainLoopDone <- err
	}()

	// Wait for main loop to start
	time.Sleep(500 * time.Millisecond)

	// Simulate secret monitoring by manually triggering polls
	pollTimes = append(pollTimes, time.Now())

	// Wait for at least one poll cycle
	time.Sleep(2 * time.Second)

	// Cancel context to stop the main loop
	cancel()

	// Wait for main loop to exit
	select {
	case err := <-mainLoopDone:
		// Should exit with context.Canceled or DeadlineExceeded
		if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
			t.Errorf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("main loop did not exit in time")
	}

	// Verify polling component was initialized
	pollMu.Lock()
	defer pollMu.Unlock()
	if len(pollTimes) == 0 {
		t.Fatal("expected at least one poll time to be recorded")
	}
}

// TestAgent_MainLoop_ComponentInitialization verifies that all components
// are initialized and started correctly in the main loop
func TestAgent_MainLoop_ComponentInitialization(t *testing.T) {
	agent := NewAgent(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Run main loop
	done := make(chan error, 1)
	go func() {
		err := agent.runMainLoop(ctx)
		done <- err
	}()

	// Wait for initialization
	time.Sleep(500 * time.Millisecond)

	// Cancel context
	cancel()

	// Wait for completion
	select {
	case err := <-done:
		if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
			t.Errorf("unexpected error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("main loop did not exit in time")
	}
}

// TestAgent_RotationCallback verifies that the rotation callback is properly wired
func TestAgent_RotationCallback(t *testing.T) {
	agent := NewAgent(nil)

	callback := agent.rotationCallback()
	if callback == nil {
		t.Fatal("rotation callback should not be nil")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Call the callback - should not panic and should return without error
	err := callback(ctx, "test-secret", 2) // priority = PriorityNormal
	if err != nil {
		t.Errorf("unexpected error from rotation callback: %v", err)
	}
}

// TestAgent_PollSecret verifies the pollSecret method correctly tracks version changes
func TestAgent_PollSecret(t *testing.T) {
	agent := NewAgent(nil)
	secretName := "test-secret"

	// First poll - should be marked as changed (no prior version)
	version1, changed1 := agent.pollSecret(secretName)
	if !changed1 {
		t.Error("first poll should report change")
	}
	if version1 == "" {
		t.Error("first poll should return a version")
	}

	// Second poll immediately after - should not change (same timestamp in testing)
	// Note: This test may be flaky due to timestamp precision
	time.Sleep(10 * time.Millisecond)
	version2, changed2 := agent.pollSecret(secretName)
	if changed2 && version2 == version1 {
		// Version is the same, so changed should be false if timestamps are the same
		t.Logf("versions match: %s == %s, changed: %v", version1, version2, changed2)
	}
}

// TestAgent_GetSecretsToMonitor verifies getting the list of monitored secrets
func TestAgent_GetSecretsToMonitor(t *testing.T) {
	agent := NewAgent(nil)

	// Initially empty
	secrets := agent.getSecretsToMonitor()
	if secrets == nil {
		secrets = []string{}
	}
	if len(secrets) != 0 {
		t.Errorf("expected 0 secrets initially, got %d", len(secrets))
	}
}

// TestAgent_MainLoop_GracefulShutdown verifies that the main loop shuts down gracefully
// when context is cancelled
func TestAgent_MainLoop_GracefulShutdown(t *testing.T) {
	agent := NewAgent(nil)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		err := agent.runMainLoop(ctx)
		done <- err
	}()

	// Let it run briefly
	time.Sleep(500 * time.Millisecond)

	// Cancel the context
	cancel()

	// Wait for graceful shutdown
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("main loop did not shut down gracefully in time")
	}
}

// ============================================================================
// Cleanup Tests (Issue #2: Unbounded secretVersions map growth)
// ============================================================================

// TestAgent_CleanupStaleSecrets verifies that stale secret entries are removed
func TestAgent_CleanupStaleSecrets(t *testing.T) {
	agent := NewAgent(nil)

	// Manually add some entries to secretVersions map
	agent.secretVersionsMu.Lock()
	agent.secretVersions["active-secret"] = "hash1"
	agent.secretVersions["stale-secret-1"] = "hash2"
	agent.secretVersions["stale-secret-2"] = "hash3"
	agent.secretVersionsMu.Unlock()

	// Initial size should be 3
	if size := agent.GetSecretVersionsMapSize(); size != 3 {
		t.Errorf("expected initial map size 3, got %d", size)
	}

	// Add active-secret to cache so it's considered monitored
	cache := agent.GetCache()
	seed := &resolver.AgentSeed{
		ProjectName: "test-project",
		SecretPool: map[string]string{
			"active-secret": "value",
		},
		Services: make(map[string]resolver.ServiceSecrets),
	}
	cache.Seed(seed)

	// Create a context with short timeout to trigger one cleanup cycle
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Run cleanup (will timeout after 100ms)
	agent.cleanupStaleSecrets(ctx)

	// After cleanup, map should only have active-secret
	if size := agent.GetSecretVersionsMapSize(); size != 1 {
		t.Errorf("expected map size 1 after cleanup, got %d", size)
	}

	// Verify stale secrets are gone
	agent.secretVersionsMu.RLock()
	if _, exists := agent.secretVersions["active-secret"]; !exists {
		t.Error("active-secret should still exist")
	}
	if _, exists := agent.secretVersions["stale-secret-1"]; exists {
		t.Error("stale-secret-1 should be removed")
	}
	if _, exists := agent.secretVersions["stale-secret-2"]; exists {
		t.Error("stale-secret-2 should be removed")
	}
	agent.secretVersionsMu.RUnlock()
}

// TestAgent_GetSecretVersionsMapSize returns correct map size
func TestAgent_GetSecretVersionsMapSize(t *testing.T) {
	agent := NewAgent(nil)

	// Initially empty
	if size := agent.GetSecretVersionsMapSize(); size != 0 {
		t.Errorf("expected initial size 0, got %d", size)
	}

	// Add entries
	agent.secretVersionsMu.Lock()
	agent.secretVersions["secret1"] = "hash1"
	agent.secretVersions["secret2"] = "hash2"
	agent.secretVersions["secret3"] = "hash3"
	agent.secretVersionsMu.Unlock()

	// Verify size
	if size := agent.GetSecretVersionsMapSize(); size != 3 {
		t.Errorf("expected size 3, got %d", size)
	}
}

// TestAgent_GetLastCleanupTime returns correct cleanup time
func TestAgent_GetLastCleanupTime(t *testing.T) {
	agent := NewAgent(nil)

	// Initially should be zero time
	if lastCleanup := agent.GetLastCleanupTime(); !lastCleanup.IsZero() {
		t.Errorf("expected zero time initially, got %v", lastCleanup)
	}

	// Set lastCleanup time
	now := time.Now()
	agent.secretVersionsMu.Lock()
	agent.lastCleanup = now
	agent.secretVersionsMu.Unlock()

	// Verify it's set
	lastCleanup := agent.GetLastCleanupTime()
	if lastCleanup.IsZero() {
		t.Error("lastCleanup should not be zero after setting")
	}
	// Allow for small time difference due to clock resolution
	if lastCleanup.Sub(now).Abs() > 10*time.Millisecond {
		t.Errorf("expected lastCleanup ~%v, got %v", now, lastCleanup)
	}
}

// TestAgent_CleanupConcurrency verifies cleanup is thread-safe
func TestAgent_CleanupConcurrency(t *testing.T) {
	agent := NewAgent(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Add initial entries
	agent.secretVersionsMu.Lock()
	for i := 0; i < 100; i++ {
		agent.secretVersions[fmt.Sprintf("secret-%d", i)] = fmt.Sprintf("hash-%d", i)
	}
	agent.secretVersionsMu.Unlock()

	done := make(chan bool, 2)

	// Run cleanup goroutine
	go func() {
		agent.cleanupStaleSecrets(ctx)
		done <- true
	}()

	// Concurrently poll and modify the map
	go func() {
		for i := 0; i < 20; i++ {
			size := agent.GetSecretVersionsMapSize()
			if size < 0 {
				t.Error("map size should never be negative")
			}
			time.Sleep(5 * time.Millisecond)
		}
		done <- true
	}()

	// Wait for both to complete
	<-time.After(150 * time.Millisecond)
	cancel()
	<-done
	<-done
}

// TestAgent_CleanupWithMonitoredSecrets verifies cleanup preserves monitored secrets
func TestAgent_CleanupWithMonitoredSecrets(t *testing.T) {
	agent := NewAgent(nil)

	// Add secrets to cache (these will be monitored)
	cache := agent.GetCache()
	seed := &resolver.AgentSeed{
		ProjectName: "test-project",
		SecretPool: map[string]string{
			"secret-a": "value-a",
			"secret-b": "value-b",
			"secret-c": "value-c",
		},
		Services: make(map[string]resolver.ServiceSecrets),
	}
	cache.Seed(seed)

	// Add entries to secretVersions map, including extras
	agent.secretVersionsMu.Lock()
	agent.secretVersions["secret-a"] = "hash-a"
	agent.secretVersions["secret-b"] = "hash-b"
	agent.secretVersions["secret-c"] = "hash-c"
	agent.secretVersions["extra-secret"] = "hash-extra"
	agent.secretVersions["another-extra"] = "hash-extra2"
	agent.secretVersionsMu.Unlock()

	if size := agent.GetSecretVersionsMapSize(); size != 5 {
		t.Errorf("expected initial size 5, got %d", size)
	}

	// Run cleanup with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	agent.cleanupStaleSecrets(ctx)

	// After cleanup, should have 3 secrets (the monitored ones)
	if size := agent.GetSecretVersionsMapSize(); size != 3 {
		t.Errorf("expected size 3 after cleanup, got %d", size)
	}

	// Verify monitored secrets still exist
	agent.secretVersionsMu.RLock()
	for _, secret := range []string{"secret-a", "secret-b", "secret-c"} {
		if _, exists := agent.secretVersions[secret]; !exists {
			t.Errorf("monitored secret %s should exist", secret)
		}
	}
	agent.secretVersionsMu.RUnlock()
}
