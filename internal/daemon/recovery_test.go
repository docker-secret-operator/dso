package daemon

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestRecoveryManager_StateTransitions(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	rm := NewRecoveryManager(logger, nil, 3)

	// Initial state should be disconnected
	if rm.GetState() != StateDisconnected {
		t.Errorf("Expected initial state Disconnected, got %d", rm.GetState())
	}

	// Transition to connecting
	rm.SetState(StateConnecting)
	if rm.GetState() != StateConnecting {
		t.Errorf("Expected state Connecting, got %d", rm.GetState())
	}

	// Transition to connected
	rm.SetState(StateConnected)
	if rm.GetState() != StateConnected {
		t.Errorf("Expected state Connected, got %d", rm.GetState())
	}

	// Transition to resyncing
	rm.SetState(StateResyncing)
	if rm.GetState() != StateResyncing {
		t.Errorf("Expected state Resyncing, got %d", rm.GetState())
	}
}

func TestRecoveryManager_MarkHealthy(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	rm := NewRecoveryManager(logger, nil, 3)
	rm.SetState(StateDisconnected)

	// Mark as healthy
	rm.MarkHealthy()

	// Should transition to connected state
	if rm.GetState() != StateConnected {
		t.Errorf("Expected state Connected after MarkHealthy, got %d", rm.GetState())
	}

	// Failure count should be reset
	if rm.GetFailureCount() != 0 {
		t.Errorf("Expected failure count 0, got %d", rm.GetFailureCount())
	}

	// TimeSinceLastHealthy should be small
	since := rm.GetTimeSinceLastHealthy()
	if since > 100*time.Millisecond {
		t.Errorf("Expected recent health check time, got %v", since)
	}
}

func TestRecoveryManager_FailureTracking(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	rm := NewRecoveryManager(logger, nil, 3)

	// Mark failures up to but not exceeding threshold
	triggerRecovery := rm.MarkFailure() // 1
	if triggerRecovery {
		t.Error("Should not trigger recovery at 1 failure (max is 3)")
	}

	triggerRecovery = rm.MarkFailure() // 2
	if triggerRecovery {
		t.Error("Should not trigger recovery at 2 failures (max is 3)")
	}

	// This should trigger recovery
	triggerRecovery = rm.MarkFailure() // 3
	if !triggerRecovery {
		t.Error("Should trigger recovery at max failures (3)")
	}

	if rm.GetFailureCount() != 3 {
		t.Errorf("Expected failure count 3, got %d", rm.GetFailureCount())
	}
}

func TestRecoveryManager_StalenessDetection(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	rm := NewRecoveryManager(logger, nil, 3)
	rm.MarkHealthy()

	// Should not be stale immediately
	if rm.IsStale(1 * time.Second) {
		t.Error("Connection should not be stale immediately after health check")
	}

	// Should be stale after sleep with small threshold
	time.Sleep(100 * time.Millisecond)
	if !rm.IsStale(50 * time.Millisecond) {
		t.Error("Connection should be stale after sleep exceeds threshold")
	}
}

func TestRecoveryManager_Reset(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	rm := NewRecoveryManager(logger, nil, 3)
	rm.SetState(StateDisconnected)
	rm.MarkFailure()
	rm.MarkFailure()

	// Verify failures tracked
	if rm.GetFailureCount() != 2 {
		t.Errorf("Expected 2 failures before reset, got %d", rm.GetFailureCount())
	}

	// Reset
	rm.Reset()

	// Verify state reset
	if rm.GetState() != StateConnected {
		t.Errorf("Expected Connected state after reset, got %d", rm.GetState())
	}

	if rm.GetFailureCount() != 0 {
		t.Errorf("Expected 0 failures after reset, got %d", rm.GetFailureCount())
	}
}

func TestRecoveryManager_StateChangeCallback(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	callCount := 0
	rm := NewRecoveryManager(logger, nil, 3)
	rm.onStateChange = func(oldState, newState ConnectionState) {
		callCount++
		if oldState == newState {
			t.Error("State change callback called with same states")
		}
	}

	// Trigger state changes
	rm.SetState(StateConnecting)
	rm.SetState(StateConnected)
	rm.SetState(StateDisconnected)

	if callCount != 3 {
		t.Errorf("Expected 3 state changes, got %d", callCount)
	}
}

func TestRecoveryManager_MaxFailuresEnforced(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	maxFails := int32(5)
	rm := NewRecoveryManager(logger, nil, maxFails)

	var triggerRecovery bool
	for i := int32(0); i < maxFails; i++ {
		triggerRecovery = rm.MarkFailure()
		if i < maxFails-1 {
			if triggerRecovery {
				t.Errorf("Should not trigger recovery before max failures (at %d of %d)", i+1, maxFails)
			}
		}
	}

	if !triggerRecovery {
		t.Errorf("Should trigger recovery at max failures (%d)", maxFails)
	}
}

func TestRecoveryManager_ResubscribeEvents(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	rm := NewRecoveryManager(logger, nil, 3)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rm.SetState(StateConnected)
	err := rm.ResubscribeEvents(ctx)

	if err != nil {
		t.Errorf("ResubscribeEvents should not return error, got %v", err)
	}

	// Should be back in connected state
	if rm.GetState() != StateConnected {
		t.Errorf("Expected Connected state after resubscribe, got %d", rm.GetState())
	}
}

func TestRecoveryManager_TimeSinceLastHealthy(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	rm := NewRecoveryManager(logger, nil, 3)
	rm.MarkHealthy()

	since1 := rm.GetTimeSinceLastHealthy()
	time.Sleep(50 * time.Millisecond)
	since2 := rm.GetTimeSinceLastHealthy()

	if since2 <= since1 {
		t.Errorf("Time since healthy should increase, got %v then %v", since1, since2)
	}

	if since2 < 50*time.Millisecond {
		t.Errorf("Expected at least 50ms elapsed, got %v", since2)
	}
}

func TestRecoveryManager_ConcurrentStateAccess(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	rm := NewRecoveryManager(logger, nil, 3)

	done := make(chan bool)

	// Multiple goroutines reading and writing state
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				rm.SetState(StateConnected)
				rm.MarkHealthy()
				_ = rm.GetState()
				_ = rm.GetFailureCount()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should still be in valid state
	state := rm.GetState()
	if state != StateConnected {
		t.Errorf("Expected Connected state after concurrent access, got %d", state)
	}
}
