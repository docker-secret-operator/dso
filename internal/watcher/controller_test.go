package watcher

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	dsoConfig "github.com/docker-secret-operator/dso/pkg/config"
	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// TestNewReloaderController creates controller with proper initialization
func TestNewReloaderController(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	// Skip if Docker not available
	controller, err := NewReloaderController(logger)
	if err != nil {
		t.Skip("Docker not available, skipping controller tests")
	}

	if controller == nil {
		t.Fatal("NewReloaderController returned nil")
	}
	if controller.Logger != logger {
		t.Error("Logger not set correctly")
	}
	if controller.cli == nil {
		t.Error("Docker client should be initialized")
	}
}

// TestReloaderController_InitializeTargets creates empty targets map
func TestReloaderController_InitializeTargets(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	controller, err := NewReloaderController(logger)
	if err != nil {
		t.Skip("Docker not available")
	}

	// Targets should be empty initially
	count := 0
	controller.Targets.Range(func(k, v interface{}) bool {
		count++
		return true
	})

	if count != 0 {
		t.Errorf("Expected 0 initial targets, got %d", count)
	}
}

// TestReloaderController_StoreTarget stores target container
func TestReloaderController_StoreTarget(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	controller, err := NewReloaderController(logger)
	if err != nil {
		t.Skip("Docker not available")
	}

	target := &TargetContainer{
		ID:          "abc123",
		Strategy:    "restart",
		ComposePath: "/path/to/docker-compose.yml",
		Secrets:     []string{"db_password", "api_key"},
	}

	controller.Targets.Store(target.ID, target)

	// Verify target is stored
	retrieved, exists := controller.Targets.Load(target.ID)
	if !exists {
		t.Error("Target should be stored")
	}

	retrievedTarget := retrieved.(*TargetContainer)
	if retrievedTarget.ID != target.ID {
		t.Errorf("Expected ID %s, got %s", target.ID, retrievedTarget.ID)
	}
	if retrievedTarget.Strategy != target.Strategy {
		t.Errorf("Expected strategy %s, got %s", target.Strategy, retrievedTarget.Strategy)
	}
}

// TestReloaderController_DeleteTarget removes target container
func TestReloaderController_DeleteTarget(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	controller, err := NewReloaderController(logger)
	if err != nil {
		t.Skip("Docker not available")
	}

	targetID := "abc123"
	target := &TargetContainer{
		ID:       targetID,
		Strategy: "restart",
		Secrets:  []string{"secret1"},
	}

	controller.Targets.Store(targetID, target)

	// Verify target exists
	_, exists := controller.Targets.Load(targetID)
	if !exists {
		t.Fatal("Target should exist before delete")
	}

	// Delete target
	controller.Targets.Delete(targetID)

	// Verify target is deleted
	_, exists = controller.Targets.Load(targetID)
	if exists {
		t.Error("Target should not exist after delete")
	}
}

// TestReloaderController_MultipleTargets handles multiple containers
func TestReloaderController_MultipleTargets(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	controller, err := NewReloaderController(logger)
	if err != nil {
		t.Skip("Docker not available")
	}

	// Store multiple targets
	targets := map[string]*TargetContainer{
		"id1": {
			ID:       "id1",
			Strategy: "signal",
			Secrets:  []string{"secret1"},
		},
		"id2": {
			ID:       "id2",
			Strategy: "restart",
			Secrets:  []string{"secret2"},
		},
		"id3": {
			ID:       "id3",
			Strategy: "rolling",
			Secrets:  []string{"secret1", "secret2"},
		},
	}

	for id, target := range targets {
		controller.Targets.Store(id, target)
	}

	// Verify all targets are stored
	count := 0
	controller.Targets.Range(func(k, v interface{}) bool {
		count++
		return true
	})

	if count != 3 {
		t.Errorf("Expected 3 targets, got %d", count)
	}
}

// TestReloaderController_RotationLocks manages lock creation
func TestReloaderController_RotationLocks(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	controller, err := NewReloaderController(logger)
	if err != nil {
		t.Skip("Docker not available")
	}

	serviceName := "web"
	lock := &lockInfo{
		startTime:   time.Now(),
		serviceName: serviceName,
	}

	controller.rotationLocks.Store(serviceName, lock)

	// Verify lock is stored
	retrieved, exists := controller.rotationLocks.Load(serviceName)
	if !exists {
		t.Error("Lock should be stored")
	}

	retrievedLock := retrieved.(*lockInfo)
	if retrievedLock.serviceName != serviceName {
		t.Errorf("Expected service %s, got %s", serviceName, retrievedLock.serviceName)
	}
}

// TestReloaderController_StaleLockRecovery detects and removes stale locks
func TestReloaderController_StaleLockRecovery(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	controller, err := NewReloaderController(logger)
	if err != nil {
		t.Skip("Docker not available")
	}

	serviceName := "api"
	staleTime := time.Now().Add(-6 * time.Minute) // 6 minutes old

	stalelock := &lockInfo{
		startTime:   staleTime,
		serviceName: serviceName,
	}

	controller.rotationLocks.Store(serviceName, stalelock)

	// Verify lock exists
	_, exists := controller.rotationLocks.Load(serviceName)
	if !exists {
		t.Fatal("Lock should exist")
	}

	// Check if lock is stale (older than 5 minutes)
	val, _ := controller.rotationLocks.Load(serviceName)
	lock := val.(*lockInfo)
	isStale := time.Since(lock.startTime) > 5*time.Minute

	if !isStale {
		t.Error("Lock should be detected as stale")
	}

	// Remove stale lock
	controller.rotationLocks.Delete(serviceName)

	// Verify lock is removed
	_, exists = controller.rotationLocks.Load(serviceName)
	if exists {
		t.Error("Stale lock should be removed")
	}
}

// TestReloaderController_ConcurrentTargetAccess handles parallel target operations
func TestReloaderController_ConcurrentTargetAccess(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	controller, err := NewReloaderController(logger)
	if err != nil {
		t.Skip("Docker not available")
	}

	done := make(chan bool, 100)

	// Concurrent writes
	for i := 0; i < 20; i++ {
		go func(id int) {
			target := &TargetContainer{
				ID:       string(rune('a'+id%26)) + "-" + fmt.Sprintf("%d", id),
				Strategy: "restart",
				Secrets:  []string{"secret1"},
			}
			controller.Targets.Store(target.ID, target)
			done <- true
		}(i)
	}

	// Wait for writes
	for i := 0; i < 20; i++ {
		<-done
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		go func(id int) {
			count := 0
			controller.Targets.Range(func(k, v interface{}) bool {
				count++
				return true
			})
			done <- true
		}(i)
	}

	// Wait for reads
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify consistency
	count := 0
	controller.Targets.Range(func(k, v interface{}) bool {
		count++
		return true
	})

	if count != 20 {
		t.Errorf("Expected 20 targets, got %d", count)
	}
}

// TestReloaderController_DegradedStateTracking stores degraded services
func TestReloaderController_DegradedStateTracking(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	controller, err := NewReloaderController(logger)
	if err != nil {
		t.Skip("Docker not available")
	}

	serviceName := "web"
	errorMsg := "Rotation failed, rollback failed after 3 attempts"

	controller.degraded.Store(serviceName, errorMsg)

	// Verify degraded state is stored
	retrieved, exists := controller.degraded.Load(serviceName)
	if !exists {
		t.Error("Degraded state should be stored")
	}

	retrievedMsg := retrieved.(string)
	if retrievedMsg != errorMsg {
		t.Errorf("Expected error %q, got %q", errorMsg, retrievedMsg)
	}

	// Clear degraded state
	controller.degraded.Delete(serviceName)

	// Verify cleared
	_, exists = controller.degraded.Load(serviceName)
	if exists {
		t.Error("Degraded state should be cleared")
	}
}

// TestReloaderController_SetServerInterface stores server interface
func TestReloaderController_SetServerInterface(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	controller, err := NewReloaderController(logger)
	if err != nil {
		t.Skip("Docker not available")
	}

	// Create mock server
	type mockServer struct{}
	server := &mockServer{}

	controller.Server = server

	if controller.Server != server {
		t.Error("Server interface should be set")
	}
}

// TestReloaderController_SetConfig stores config reference
func TestReloaderController_SetConfig(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	controller, err := NewReloaderController(logger)
	if err != nil {
		t.Skip("Docker not available")
	}

	config := &dsoConfig.Config{
		Providers: make(map[string]dsoConfig.ProviderConfig),
		Secrets:   make([]dsoConfig.SecretMapping, 0),
	}

	controller.Config = config

	if controller.Config != config {
		t.Error("Config should be set")
	}
}

// TestReloaderController_TargetContainerStructure verifies target fields
func TestReloaderController_TargetContainerStructure(t *testing.T) {
	target := &TargetContainer{
		ID:          "container-id",
		Strategy:    "rolling",
		ComposePath: "/path/to/compose.yml",
		Secrets:     []string{"db-pass", "api-key", "jwt-secret"},
	}

	if target.ID != "container-id" {
		t.Error("Target ID mismatch")
	}
	if target.Strategy != "rolling" {
		t.Error("Target strategy mismatch")
	}
	if target.ComposePath != "/path/to/compose.yml" {
		t.Error("Target compose path mismatch")
	}
	if len(target.Secrets) != 3 {
		t.Errorf("Expected 3 secrets, got %d", len(target.Secrets))
	}
}

// TestReloaderController_EmptySecretsList handles empty secrets
func TestReloaderController_EmptySecretsList(t *testing.T) {
	target := &TargetContainer{
		ID:       "id1",
		Strategy: "restart",
		Secrets:  []string{},
	}

	if len(target.Secrets) != 0 {
		t.Error("Secrets list should be empty")
	}
}

// TestReloaderController_ConcurrentLockOperations handles lock concurrency
func TestReloaderController_ConcurrentLockOperations(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	controller, err := NewReloaderController(logger)
	if err != nil {
		t.Skip("Docker not available")
	}

	done := make(chan bool, 100)
	lockCount := int32(0)

	// Concurrent lock creation
	for i := 0; i < 10; i++ {
		go func(id int) {
			serviceName := "service-" + string(rune('0'+id))
			lock := &lockInfo{
				startTime:   time.Now(),
				serviceName: serviceName,
			}
			controller.rotationLocks.Store(serviceName, lock)
			atomic.AddInt32(&lockCount, 1)
			done <- true
		}(i)
	}

	// Wait for lock creation
	for i := 0; i < 10; i++ {
		<-done
	}

	// Concurrent lock deletion
	for i := 0; i < 5; i++ {
		go func(id int) {
			serviceName := "service-" + string(rune('0'+id))
			controller.rotationLocks.Delete(serviceName)
			done <- true
		}(i)
	}

	// Wait for deletions
	for i := 0; i < 5; i++ {
		<-done
	}

	// Verify remaining locks
	remainingLocks := 0
	controller.rotationLocks.Range(func(k, v interface{}) bool {
		remainingLocks++
		return true
	})

	if remainingLocks != 5 {
		t.Errorf("Expected 5 remaining locks, got %d", remainingLocks)
	}
}

// TestReloaderController_RangeOverTargets iterates all targets
func TestReloaderController_RangeOverTargets(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	controller, err := NewReloaderController(logger)
	if err != nil {
		t.Skip("Docker not available")
	}

	// Store multiple targets
	targetIDs := []string{"id1", "id2", "id3", "id4", "id5"}
	for _, id := range targetIDs {
		controller.Targets.Store(id, &TargetContainer{
			ID:       id,
			Strategy: "restart",
		})
	}

	// Collect all target IDs during range
	found := make(map[string]bool)
	controller.Targets.Range(func(k, v interface{}) bool {
		target := v.(*TargetContainer)
		found[target.ID] = true
		return true
	})

	// Verify all targets were found
	if len(found) != len(targetIDs) {
		t.Errorf("Expected %d targets, got %d", len(targetIDs), len(found))
	}

	for _, id := range targetIDs {
		if !found[id] {
			t.Errorf("Target %s not found during range", id)
		}
	}
}

// TestReloaderController_LockInfoStructure verifies lock fields
func TestReloaderController_LockInfoStructure(t *testing.T) {
	startTime := time.Now()
	serviceName := "test-service"

	lock := &lockInfo{
		startTime:   startTime,
		serviceName: serviceName,
	}

	if lock.serviceName != serviceName {
		t.Error("Lock service name mismatch")
	}
	if !lock.startTime.Equal(startTime) {
		t.Error("Lock start time mismatch")
	}
}

// TestReloaderController_TargetStrategyVariations handles all strategy types
func TestReloaderController_TargetStrategyVariations(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	controller, err := NewReloaderController(logger)
	if err != nil {
		t.Skip("Docker not available")
	}

	strategies := []string{"signal", "restart", "rolling", "auto"}

	for i, strategy := range strategies {
		target := &TargetContainer{
			ID:       "id-" + string(rune('0'+i)),
			Strategy: strategy,
			Secrets:  []string{"secret"},
		}
		controller.Targets.Store(target.ID, target)
	}

	// Verify all strategies are stored
	count := 0
	controller.Targets.Range(func(k, v interface{}) bool {
		count++
		return true
	})

	if count != len(strategies) {
		t.Errorf("Expected %d targets, got %d", len(strategies), count)
	}
}

// TestReloaderController_DegradedStateConcurrency handles concurrent degraded updates
func TestReloaderController_DegradedStateConcurrency(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	controller, err := NewReloaderController(logger)
	if err != nil {
		t.Skip("Docker not available")
	}

	done := make(chan bool, 20)

	// Concurrent degraded state updates
	for i := 0; i < 10; i++ {
		go func(id int) {
			serviceName := "service-" + string(rune('0'+id))
			controller.degraded.Store(serviceName, "error message")
			done <- true
		}(i)
	}

	// Wait for updates
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify all states are stored
	count := 0
	controller.degraded.Range(func(k, v interface{}) bool {
		count++
		return true
	})

	if count != 10 {
		t.Errorf("Expected 10 degraded states, got %d", count)
	}
}

// TestReloaderController_LoadAndDelete uses sync.Map atomic operation
func TestReloaderController_LoadAndDelete(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	controller, err := NewReloaderController(logger)
	if err != nil {
		t.Skip("Docker not available")
	}

	targetID := "test-id"
	target := &TargetContainer{
		ID:       targetID,
		Strategy: "restart",
	}

	controller.Targets.Store(targetID, target)

	// LoadAndDelete should return the value and true
	retrieved, loaded := controller.Targets.LoadAndDelete(targetID)
	if !loaded {
		t.Error("Target should exist for LoadAndDelete")
	}

	retrievedTarget := retrieved.(*TargetContainer)
	if retrievedTarget.ID != targetID {
		t.Error("Retrieved target ID mismatch")
	}

	// Target should be deleted now
	_, exists := controller.Targets.Load(targetID)
	if exists {
		t.Error("Target should be deleted after LoadAndDelete")
	}
}

// TestReloaderController_ContainerStartEventFlow simulates container start
func TestReloaderController_ContainerStartEventFlow(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	controller, err := NewReloaderController(logger)
	if err != nil {
		t.Skip("Docker not available")
	}

	// Simulate: container with dso.reloader label starts
	containerID := "abc123def456"
	target := &TargetContainer{
		ID:          containerID,
		Strategy:    "signal", // from dso.update.strategy label
		ComposePath: "/path/to/compose.yml",
		Secrets:     []string{"db_password", "api_key"},
	}

	controller.Targets.Store(containerID, target)

	// Verify target is registered
	retrieved, exists := controller.Targets.Load(containerID)
	if !exists {
		t.Fatal("Target should be registered")
	}

	registeredTarget := retrieved.(*TargetContainer)
	if registeredTarget.Strategy != "signal" {
		t.Error("Strategy should be set correctly")
	}
	if len(registeredTarget.Secrets) != 2 {
		t.Error("Secrets should be parsed correctly")
	}
}

// TestReloaderController_ContainerStopEventFlow simulates container stop
func TestReloaderController_ContainerStopEventFlow(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	controller, err := NewReloaderController(logger)
	if err != nil {
		t.Skip("Docker not available")
	}

	containerID := "abc123def456"
	target := &TargetContainer{
		ID:       containerID,
		Strategy: "restart",
	}

	// Register target
	controller.Targets.Store(containerID, target)

	// Verify it exists
	_, exists := controller.Targets.Load(containerID)
	if !exists {
		t.Fatal("Target should exist")
	}

	// Simulate stop event: unregister
	controller.Targets.Delete(containerID)

	// Verify it's removed
	_, exists = controller.Targets.Load(containerID)
	if exists {
		t.Error("Target should be unregistered after stop")
	}
}

// ---- helpers for Docker-mock-based tests ------------------------------------

// newMockController creates a ReloaderController pointed at a mock Docker HTTP
// server. The server and Docker client are cleaned up via t.Cleanup.
func newMockController(t *testing.T, handler http.Handler) *ReloaderController {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cli, err := client.NewClientWithOpts(
		client.WithHost("tcp://"+strings.TrimPrefix(srv.URL, "http://")),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		t.Fatalf("failed to create mock Docker client: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	return &ReloaderController{
		Logger: zaptest.NewLogger(t),
		cli:    cli,
	}
}

// minimalInspect returns a ContainerJSON with just enough fields populated for
// executeSimpleRestart to succeed (name, config, hostconfig, network settings).
func minimalInspect(name, id string) dockertypes.ContainerJSON {
	return dockertypes.ContainerJSON{
		ContainerJSONBase: &dockertypes.ContainerJSONBase{
			ID:         id,
			Name:       "/" + name,
			HostConfig: &container.HostConfig{},
		},
		Config: &container.Config{
			Env: []string{"EXISTING=old"},
		},
		NetworkSettings: &dockertypes.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{},
		},
	}
}

// ---- executeRollback tests (H2 regression) ----------------------------------

// TestExecuteRollback_SuccessOnFirstAttempt verifies that a single successful
// ContainerStart on the original is sufficient — service must NOT be degraded.
func TestExecuteRollback_SuccessOnFirstAttempt(t *testing.T) {
	var startCalls int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/start"):
			atomic.AddInt32(&startCalls, 1)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/rename"):
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusOK)
		}
	})

	rc := newMockController(t, handler)
	rc.executeRollback(context.Background(), "new-id", "old-id", "myapp", "myapp-svc")

	if _, deg := rc.degraded.Load("myapp-svc"); deg {
		t.Error("H2 regression: service marked degraded despite successful rollback")
	}
	if atomic.LoadInt32(&startCalls) == 0 {
		t.Error("executeRollback made no ContainerStart calls")
	}
}

// TestExecuteRollback_AllAttemptsFail verifies that after all 3 retries the
// service is stored in the degraded map (H2 regression test).
func TestExecuteRollback_AllAttemptsFail(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/start"):
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"message":"start failed"}`))
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/rename"):
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusOK)
		}
	})

	rc := newMockController(t, handler)
	rc.executeRollback(context.Background(), "new-id", "old-id", "myapp", "myapp-svc")

	if _, deg := rc.degraded.Load("myapp-svc"); !deg {
		t.Error("H2 regression: service not marked degraded after all rollback attempts failed")
	}
}

// TestExecuteRollback_SucceedsOnThirdAttempt verifies retry behaviour: first two
// start calls fail, third succeeds → service must NOT be degraded.
func TestExecuteRollback_SucceedsOnThirdAttempt(t *testing.T) {
	var startCalls int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/start"):
			n := atomic.AddInt32(&startCalls, 1)
			if n < 3 {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"message":"not yet"}`))
			} else {
				w.WriteHeader(http.StatusNoContent)
			}
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/rename"):
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusOK)
		}
	})

	rc := newMockController(t, handler)
	rc.executeRollback(context.Background(), "new-id", "old-id", "myapp", "myapp-svc")

	if _, deg := rc.degraded.Load("myapp-svc"); deg {
		t.Error("service incorrectly degraded when rollback succeeded on attempt 3")
	}
	if atomic.LoadInt32(&startCalls) != 3 {
		t.Errorf("expected 3 start attempts, got %d", atomic.LoadInt32(&startCalls))
	}
}

// ---- executeSimpleRestart tests (L5 regression) ----------------------------

// TestExecuteSimpleRestart_HappyPath verifies: inspect → rename → create →
// stop old → start new → remove old — all in order, no error returned.
func TestExecuteSimpleRestart_HappyPath(t *testing.T) {
	const (
		origID   = "orig-container-id"
		newID    = "new-container-id"
		origName = "myapp"
	)
	inspectJSON, _ := json.Marshal(minimalInspect(origName, origID))
	createJSON, _ := json.Marshal(container.CreateResponse{ID: newID})

	var newStarted, origRemoved int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case strings.Contains(p, "/containers/"+origID+"/json"):
			w.Header().Set("Content-Type", "application/json")
			w.Write(inspectJSON)
		case strings.Contains(p, "/rename"):
			w.WriteHeader(http.StatusNoContent)
		case strings.Contains(p, "/containers/create"):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			w.Write(createJSON)
		case strings.Contains(p, "/containers/"+origID+"/stop"):
			w.WriteHeader(http.StatusNoContent)
		case strings.Contains(p, "/containers/"+newID+"/start"):
			atomic.AddInt32(&newStarted, 1)
			w.WriteHeader(http.StatusNoContent)
		case strings.Contains(p, "/containers/"+origID) && r.Method == http.MethodDelete:
			atomic.AddInt32(&origRemoved, 1)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusOK)
		}
	})

	rc := newMockController(t, handler)
	err := rc.executeSimpleRestart(context.Background(), origID, map[string]string{"SECRET": "newval"})
	if err != nil {
		t.Fatalf("L5 regression: unexpected error: %v", err)
	}
	if atomic.LoadInt32(&newStarted) == 0 {
		t.Error("new container was not started")
	}
	if atomic.LoadInt32(&origRemoved) == 0 {
		t.Error("original container was not removed on success")
	}
}

// TestExecuteSimpleRestart_InspectFailure verifies the error is propagated and
// nothing further happens when inspect fails.
func TestExecuteSimpleRestart_InspectFailure(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message":"daemon error"}`))
	})

	rc := newMockController(t, handler)
	err := rc.executeSimpleRestart(context.Background(), "bad-id", nil)
	if err == nil {
		t.Error("expected error when inspect fails, got nil")
	}
}

// TestExecuteSimpleRestart_StartFailureRollsBack verifies that when the new
// container's start fails, the original name is restored and its restart is
// attempted.
func TestExecuteSimpleRestart_StartFailureRollsBack(t *testing.T) {
	const (
		origID   = "orig-id"
		newID    = "new-id"
		origName = "myapp"
	)
	inspectJSON, _ := json.Marshal(minimalInspect(origName, origID))
	createJSON, _ := json.Marshal(container.CreateResponse{ID: newID})

	var origRestarted int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case strings.Contains(p, "/containers/"+origID+"/json"):
			w.Header().Set("Content-Type", "application/json")
			w.Write(inspectJSON)
		case strings.Contains(p, "/rename"):
			w.WriteHeader(http.StatusNoContent)
		case strings.Contains(p, "/containers/create"):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			w.Write(createJSON)
		case strings.Contains(p, "/containers/"+origID+"/stop"):
			w.WriteHeader(http.StatusNoContent)
		case strings.Contains(p, "/containers/"+newID+"/start"):
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"message":"oom"}`))
		case strings.Contains(p, "/containers/"+newID) && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case strings.Contains(p, "/containers/"+origID+"/start"):
			atomic.AddInt32(&origRestarted, 1)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusOK)
		}
	})

	rc := newMockController(t, handler)
	err := rc.executeSimpleRestart(context.Background(), origID, nil)
	if err == nil {
		t.Error("expected error when new container start fails")
	}
	if atomic.LoadInt32(&origRestarted) == 0 {
		t.Error("original container was not restarted after new container start failure")
	}
}
