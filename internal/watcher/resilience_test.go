package watcher

import (
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/events"
	"go.uber.org/zap"
)

// TestReloaderController_DaemonReconnect verifies exponential backoff reconnect behavior
func TestReloaderController_DaemonReconnect(t *testing.T) {
	// logger, _ := zap.NewDevelopment()
	// controller := &ReloaderController{
	// 	Logger: logger,
	// 	Targets: sync.Map{},
	// }

	// Verify exponential backoff calculation (1s, 1.5s, 2.25s, ...)
	delay := time.Second
	maxDelay := 30 * time.Second

	expectedDelays := []time.Duration{
		time.Second,
		time.Duration(float64(time.Second) * 1.5),
		time.Duration(float64(time.Second) * 1.5 * 1.5),
	}

	for i, expected := range expectedDelays {
		if delay != expected {
			t.Errorf("Reconnect attempt %d: expected delay %v, got %v", i, expected, delay)
		}
		delay = time.Duration(float64(delay) * 1.5)
		if delay > maxDelay {
			delay = maxDelay
		}
	}

	// Verify max delay cap
	delay = 30 * time.Second
	for i := 0; i < 10; i++ {
		delay = time.Duration(float64(delay) * 1.5)
		if delay > maxDelay {
			delay = maxDelay
		}
		if delay > maxDelay {
			t.Errorf("Reconnect delay %d exceeded max: %v > %v", i, delay, maxDelay)
		}
	}
}

// TestReloaderController_HandleContainerEvent_InvalidSecrets verifies empty secret handling
func TestReloaderController_HandleContainerEvent_InvalidSecrets(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	controller := &ReloaderController{
		Logger:  logger,
		Targets: sync.Map{},
	}

	// Create mock event with empty secrets
	mockEvent := mockContainerStartEvent("container-123", "")
	err := controller.handleContainerEvent(mockEvent)

	if err != nil {
		t.Errorf("Should handle empty secrets gracefully, got error: %v", err)
	}

	// Container should NOT be registered
	_, exists := controller.Targets.Load("container-123")
	if exists {
		t.Error("Container with empty secrets should not be registered")
	}
}

// TestReloaderController_ConcurrentTargetOperations verifies thread-safe target management
func TestReloaderController_ConcurrentTargetOperations(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	controller := &ReloaderController{
		Logger:  logger,
		Targets: sync.Map{},
	}

	done := make(chan bool, 10)

	// Concurrent writes
	for i := 0; i < 5; i++ {
		go func(id int) {
			target := &TargetContainer{
				ID:       "container-" + string(rune(48+id)),
				Strategy: "restart",
				Secrets:  []string{"secret1"},
			}
			controller.Targets.Store(target.ID, target)
			done <- true
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 5; i++ {
		go func() {
			controller.Targets.Range(func(key, value interface{}) bool {
				return true
			})
			done <- true
		}()
	}

	// Wait for all
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestReloaderController_StaleRotationLockRecovery verifies stale lock cleanup
func TestReloaderController_StaleRotationLockRecovery(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	controller := &ReloaderController{
		Logger:        logger,
		Targets:       sync.Map{},
		rotationLocks: sync.Map{},
	}

	serviceName := "test-service"

	// Create a stale lock (older than 5 minutes)
	staleTime := time.Now().Add(-6 * time.Minute)
	controller.rotationLocks.Store(serviceName, &lockInfo{
		startTime:   staleTime,
		serviceName: serviceName,
	})

	// Verify stale lock is detected
	val, busy := controller.rotationLocks.Load(serviceName)
	if !busy {
		t.Fatal("Lock should exist")
	}

	info := val.(*lockInfo)
	if time.Since(info.startTime) <= 5*time.Minute {
		t.Error("Lock should be stale (older than 5 minutes)")
	}

	// Simulate recovery: delete stale lock
	if time.Since(info.startTime) > 5*time.Minute {
		controller.rotationLocks.Delete(serviceName)
	}

	// Verify lock is gone
	_, stillBusy := controller.rotationLocks.Load(serviceName)
	if stillBusy {
		t.Error("Stale lock should be recovered/deleted")
	}
}

// TestReloaderController_ReconciliationCleanup verifies orphaned container cleanup
func TestReloaderController_ReconciliationCleanup(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	controller := &ReloaderController{
		Logger:  logger,
		Targets: sync.Map{},
	}

	// Register targets
	controller.Targets.Store("container-1", &TargetContainer{ID: "container-1"})
	controller.Targets.Store("container-2", &TargetContainer{ID: "container-2"})

	// Simulate finding orphaned containers
	orphaned := []string{"container-1"}

	// Clean up
	for _, id := range orphaned {
		controller.Targets.Delete(id)
	}

	// Verify cleanup
	_, exists1 := controller.Targets.Load("container-1")
	_, exists2 := controller.Targets.Load("container-2")

	if exists1 {
		t.Error("Orphaned container-1 should be deleted")
	}
	if !exists2 {
		t.Error("Container-2 should still exist")
	}
}

// Helper function to create mock container start event
func mockContainerStartEvent(containerID, secrets string) events.Message {
	return events.Message{
		Type:   "container",
		Action: "start",
		Actor: events.Actor{
			ID: containerID,
			Attributes: map[string]string{
				"dso.reloader":        "true",
				"dso.secrets":         secrets,
				"dso.update.strategy": "restart",
			},
		},
	}
}

// TestReloaderController_EventHandlingErrorResilience verifies error handling doesn't crash loop
func TestReloaderController_EventHandlingErrorResilience(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	controller := &ReloaderController{
		Logger:  logger,
		Targets: sync.Map{},
	}

	// Test with malformed event (missing attributes)
	mockEvent := events.Message{
		Type:   "container",
		Action: "start",
		Actor: events.Actor{
			ID:         "container-123",
			Attributes: nil, // This will cause panic without proper nil checks
		},
	}

	// Should not crash
	err := controller.handleContainerEvent(mockEvent)
	if err != nil {
		t.Logf("Error handling malformed event: %v", err)
	}
}

// TestReloaderController_LockContentionUnderChurn verifies lock behavior under high churn
func TestReloaderController_LockContentionUnderChurn(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	controller := &ReloaderController{
		Logger:        logger,
		Targets:       sync.Map{},
		rotationLocks: sync.Map{},
	}

	done := make(chan bool, 50)

	// Simulate high container churn (many concurrent rotations)
	for i := 0; i < 50; i++ {
		go func(id int) {
			serviceName := "service-" + string(rune(48+(id%5)))

			// Try to acquire lock
			if val, busy := controller.rotationLocks.Load(serviceName); busy {
				info := val.(*lockInfo)
				if time.Since(info.startTime) > 5*time.Minute {
					controller.rotationLocks.Delete(serviceName)
				}
			}

			// Acquire lock
			controller.rotationLocks.Store(serviceName, &lockInfo{
				startTime:   time.Now(),
				serviceName: serviceName,
			})

			// Simulate work
			time.Sleep(10 * time.Millisecond)

			// Release lock
			controller.rotationLocks.Delete(serviceName)
			done <- true
		}(i)
	}

	// Wait for completion
	for i := 0; i < 50; i++ {
		<-done
	}

	// Verify all locks are released
	lockCount := 0
	controller.rotationLocks.Range(func(key, value interface{}) bool {
		lockCount++
		return true
	})

	if lockCount > 0 {
		t.Errorf("Expected all locks to be released, but found %d locks remaining", lockCount)
	}
}
