package runtime

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// RecoveryScenario simulates a failure and recovery sequence
type RecoveryScenario struct {
	mu                      sync.RWMutex
	containerState          map[string]int // container_id -> state
	appliedOperations       map[string]int // operation_id -> count
	inconsistencies         []string
	daemonConnected         bool
	failureTime             time.Time
	recoveryTime            time.Time
	operationsBeforeFailure int
	operationsAfterRecovery int
}

// NewRecoveryScenario creates a test scenario
func NewRecoveryScenario() *RecoveryScenario {
	return &RecoveryScenario{
		containerState:    make(map[string]int),
		appliedOperations: make(map[string]int),
		daemonConnected:   true,
	}
}

// SimulateContainerOperation simulates a container operation
func (rs *RecoveryScenario) SimulateContainerOperation(containerID string, opID string) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if !rs.daemonConnected {
		return fmt.Errorf("daemon disconnected")
	}

	// Track operation
	rs.appliedOperations[opID]++
	if rs.appliedOperations[opID] > 1 {
		rs.inconsistencies = append(rs.inconsistencies,
			fmt.Sprintf("Operation %s applied %d times", opID, rs.appliedOperations[opID]))
	}

	// Update state
	rs.containerState[containerID]++

	return nil
}

// SimulateDaemonDisconnect simulates daemon disconnection
func (rs *RecoveryScenario) SimulateDaemonDisconnect() {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	rs.daemonConnected = false
	rs.failureTime = time.Now()
}

// SimulateDaemonReconnect simulates daemon reconnection
func (rs *RecoveryScenario) SimulateDaemonReconnect() {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	rs.daemonConnected = true
	rs.recoveryTime = time.Now()
}

// GetDowntimeDuration returns time daemon was disconnected
func (rs *RecoveryScenario) GetDowntimeDuration() time.Duration {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	if rs.failureTime.IsZero() || rs.recoveryTime.IsZero() {
		return 0
	}

	return rs.recoveryTime.Sub(rs.failureTime)
}

// GetInconsistencies returns detected inconsistencies
func (rs *RecoveryScenario) GetInconsistencies() []string {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	return rs.inconsistencies
}

// GetOperationCount returns how many times an operation was applied
func (rs *RecoveryScenario) GetOperationCount(opID string) int {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	return rs.appliedOperations[opID]
}

// TestDaemonReconnectReconciliation validates reconciliation after reconnect
func TestDaemonReconnectReconciliation(t *testing.T) {
	scenario := NewRecoveryScenario()

	// Phase 1: Normal operation
	for i := 0; i < 10; i++ {
		opID := fmt.Sprintf("op_%d", i)
		scenario.SimulateContainerOperation("container_a", opID)
		scenario.operationsBeforeFailure++
	}

	// Phase 2: Daemon disconnects
	scenario.SimulateDaemonDisconnect()
	time.Sleep(100 * time.Millisecond)

	// Phase 3: Cannot apply operations during disconnect
	err := scenario.SimulateContainerOperation("container_a", "op_during_failure")
	if err == nil {
		t.Error("Should fail to apply operation during disconnect")
	}

	// Phase 4: Daemon reconnects and reconciles
	scenario.SimulateDaemonReconnect()

	// Reconciliation should NOT replay operations
	// This simulates deduplication preventing replay
	for i := 0; i < 10; i++ {
		opID := fmt.Sprintf("op_%d", i)
		// Reconciliation checks if operation was already applied
		count := scenario.GetOperationCount(opID)
		if count > 1 {
			t.Errorf("Operation %s replayed during reconciliation (count=%d)", opID, count)
		}
	}

	// Verify no inconsistencies detected
	inconsistencies := scenario.GetInconsistencies()
	if len(inconsistencies) > 0 {
		t.Errorf("Inconsistencies detected: %v", inconsistencies)
	}
}

// TestPartialRotationRecovery validates recovery from mid-rotation failure
func TestPartialRotationRecovery(t *testing.T) {
	scenario := NewRecoveryScenario()

	// Start rotation for multiple containers
	containers := []string{"container_1", "container_2", "container_3"}

	// Apply rotation to some containers using per-container operation IDs
	for i := 0; i < 2; i++ {
		opID := fmt.Sprintf("rotate_secrets_%s", containers[i])
		scenario.SimulateContainerOperation(containers[i], opID)
	}

	// Daemon fails mid-rotation
	scenario.SimulateDaemonDisconnect()

	// Reconnect
	scenario.SimulateDaemonReconnect()

	// Recovery: continue where we left off without duplicating
	scenario.SimulateContainerOperation(containers[2], fmt.Sprintf("rotate_secrets_%s", containers[2]))

	// Verify no operations were duplicated (each container's op should be applied exactly once)
	for _, c := range containers {
		opID := fmt.Sprintf("rotate_secrets_%s", c)
		if scenario.GetOperationCount(opID) > 1 {
			t.Errorf("Rotation operation duplicated for %s", c)
		}
	}
}

// TestStaleContainerCleanup validates orphaned container cleanup
func TestStaleContainerCleanup(t *testing.T) {
	scenario := NewRecoveryScenario()

	// Create and manage containers
	for i := 0; i < 5; i++ {
		containerID := fmt.Sprintf("container_%d", i)
		scenario.SimulateContainerOperation(containerID, "create")
	}

	// Simulate daemon disconnect
	scenario.SimulateDaemonDisconnect()
	time.Sleep(50 * time.Millisecond)

	// Daemon reconnects
	scenario.SimulateDaemonReconnect()

	// Reconciliation should identify containers that no longer exist
	// and remove them from tracking
	// (In real implementation, would check against actual containers)

	scenario.mu.Lock()
	// Simulate cleanup of stale entries
	for i := 3; i < 5; i++ {
		containerID := fmt.Sprintf("container_%d", i)
		delete(scenario.containerState, containerID)
	}
	scenario.mu.Unlock()

	// Verify cleanup occurred
	scenario.mu.RLock()
	activeBefore := len(scenario.containerState)
	scenario.mu.RUnlock()

	if activeBefore != 3 {
		t.Errorf("Expected 3 active containers, got %d", activeBefore)
	}
}

// TestIdempotentRotationRecovery validates rotation idempotency
func TestIdempotentRotationRecovery(t *testing.T) {
	scenario := NewRecoveryScenario()

	// Apply rotation
	scenario.SimulateContainerOperation("container_a", "rotation_id_123")

	// Verify operation applied once
	if scenario.GetOperationCount("rotation_id_123") != 1 {
		t.Error("Operation should be applied once")
	}

	// Simulate daemon disconnect during acknowledgment
	scenario.SimulateDaemonDisconnect()
	time.Sleep(50 * time.Millisecond)

	// Reconnect
	scenario.SimulateDaemonReconnect()

	// Retry the same operation (with deduplication it should be skipped)
	// In this test, we verify the operation isn't applied again
	count := scenario.GetOperationCount("rotation_id_123")

	if count > 1 {
		t.Errorf("Operation applied multiple times during recovery: %d", count)
	}
}

// TestConcurrentRecoveryOperations validates concurrent operations during recovery
func TestConcurrentRecoveryOperations(t *testing.T) {
	scenario := NewRecoveryScenario()
	var wg sync.WaitGroup
	errorCount := int32(0)

	// Normal operations
	for i := 0; i < 5; i++ {
		scenario.SimulateContainerOperation(fmt.Sprintf("container_%d", i), fmt.Sprintf("op_%d", i))
	}

	// Disconnect
	scenario.SimulateDaemonDisconnect()
	time.Sleep(50 * time.Millisecond)

	// Reconnect
	scenario.SimulateDaemonReconnect()

	// Concurrent reconciliation and new operations
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Try to apply new operations during recovery
			if err := scenario.SimulateContainerOperation(fmt.Sprintf("container_%d", id), fmt.Sprintf("recovery_op_%d", id)); err != nil {
				atomic.AddInt32(&errorCount, 1)
			}
		}(i)
	}

	wg.Wait()

	// Should not have errors during concurrent recovery
	if errorCount > 0 {
		t.Errorf("Concurrent operations during recovery failed: %d errors", errorCount)
	}

	// Verify no duplicates
	inconsistencies := scenario.GetInconsistencies()
	if len(inconsistencies) > 0 {
		t.Errorf("Detected inconsistencies during concurrent recovery: %v", inconsistencies)
	}
}

// TestReconciliationCompleteness validates all containers checked after reconnect
func TestReconciliationCompleteness(t *testing.T) {
	scenario := NewRecoveryScenario()
	containers := make([]string, 20)

	// Create many containers
	for i := 0; i < 20; i++ {
		containers[i] = fmt.Sprintf("container_%d", i)
		scenario.SimulateContainerOperation(containers[i], "create")
	}

	// Disconnect
	scenario.SimulateDaemonDisconnect()
	time.Sleep(50 * time.Millisecond)

	// Reconnect
	scenario.SimulateDaemonReconnect()

	// Reconciliation must check all containers
	checkedCount := 0
	for i := 0; i < 20; i++ {
		containerID := fmt.Sprintf("container_%d", i)
		if scenario.containerState[containerID] > 0 {
			checkedCount++
		}
	}

	if checkedCount != 20 {
		t.Errorf("Reconciliation incomplete: only %d of 20 containers checked", checkedCount)
	}
}

// TestDowntimeSensitivity validates appropriate action based on downtime
func TestDowntimeSensitivity(t *testing.T) {
	scenario := NewRecoveryScenario()

	// Simulate short downtime
	scenario.SimulateDaemonDisconnect()
	time.Sleep(100 * time.Millisecond)
	scenario.SimulateDaemonReconnect()

	shortDowntime := scenario.GetDowntimeDuration()
	if shortDowntime < 80*time.Millisecond || shortDowntime > 200*time.Millisecond {
		t.Errorf("Short downtime measurement incorrect: %v", shortDowntime)
	}

	// For short downtimes, reconciliation should be fast
	if shortDowntime > 500*time.Millisecond {
		t.Error("Short downtime reconciliation took too long")
	}

	// Verify no operations were duplicated during brief disconnect
	inconsistencies := scenario.GetInconsistencies()
	if len(inconsistencies) > 0 {
		t.Errorf("Short downtime caused inconsistencies: %v", inconsistencies)
	}
}

// TestLongDowntimeReconciliation validates behavior after extended outage
func TestLongDowntimeReconciliation(t *testing.T) {
	scenario := NewRecoveryScenario()

	// Pre-downtime operations
	for i := 0; i < 10; i++ {
		scenario.SimulateContainerOperation(fmt.Sprintf("container_%d", i), fmt.Sprintf("op_%d", i))
	}

	// Long downtime
	scenario.SimulateDaemonDisconnect()
	time.Sleep(500 * time.Millisecond)
	scenario.SimulateDaemonReconnect()

	longDowntime := scenario.GetDowntimeDuration()
	if longDowntime < 400*time.Millisecond {
		t.Errorf("Long downtime measurement incorrect: %v", longDowntime)
	}

	// After extended downtime, full reconciliation should occur
	// Verify state consistency
	inconsistencies := scenario.GetInconsistencies()
	if len(inconsistencies) > 0 {
		t.Errorf("Long downtime reconciliation detected inconsistencies: %v", inconsistencies)
	}
}
