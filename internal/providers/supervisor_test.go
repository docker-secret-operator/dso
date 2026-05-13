package providers

import (
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestProviderSupervisor_MarkHealthy(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	ps := NewProviderSupervisor(logger, "test-provider")

	// Initially unknown
	if ps.GetHealth() != HealthUnknown {
		t.Errorf("Expected HealthUnknown initially, got %d", ps.GetHealth())
	}

	// Mark healthy
	ps.MarkHealthy()

	if ps.GetHealth() != HealthHealthy {
		t.Errorf("Expected HealthHealthy after MarkHealthy, got %d", ps.GetHealth())
	}

	// Counters should be reset
	if ps.GetStats()["consecutive_crashes"] != int32(0) {
		t.Error("Consecutive crashes should be reset to 0")
	}
}

func TestProviderSupervisor_CrashTracking(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	ps := NewProviderSupervisor(logger, "test-provider")

	// First crash - should not trigger threshold
	triggerRestart := ps.RecordCrash()
	if triggerRestart {
		t.Error("Should not trigger restart on first crash")
	}

	if ps.GetStats()["consecutive_crashes"] != int32(1) {
		t.Error("Crash counter not incremented")
	}

	// Second crash
	triggerRestart = ps.RecordCrash()
	if triggerRestart {
		t.Error("Should not trigger restart on second crash")
	}

	// Third crash - should trigger
	triggerRestart = ps.RecordCrash()
	if !triggerRestart {
		t.Error("Should trigger restart at crash threshold (3)")
	}

	if ps.GetHealth() != HealthUnhealthy {
		t.Errorf("Expected HealthUnhealthy after crashes, got %d", ps.GetHealth())
	}
}

func TestProviderSupervisor_HeartbeatFailure(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	ps := NewProviderSupervisor(logger, "test-provider")

	// Record failures up to threshold
	for i := 0; i < 4; i++ {
		triggerRestart := ps.RecordHeartbeatFailure()
		if i < 4 {
			if triggerRestart {
				t.Errorf("Should not trigger at failure %d (threshold is 5)", i+1)
			}
		}
	}

	// 5th failure should trigger
	triggerRestart := ps.RecordHeartbeatFailure()
	if !triggerRestart {
		t.Error("Should trigger restart at heartbeat failure threshold (5)")
	}
}

func TestProviderSupervisor_ExponentialBackoff(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	ps := NewProviderSupervisor(logger, "test-provider")

	testCases := []struct {
		restartCount int32
		minBackoff   time.Duration
		maxBackoff   time.Duration
	}{
		{0, 0, 0}, // No backoff on first restart
		{1, 1 * time.Second, 2 * time.Second},
		{2, 2 * time.Second, 4 * time.Second},
		{3, 4 * time.Second, 8 * time.Second},
		{4, 8 * time.Second, 16 * time.Second},
		{5, 16 * time.Second, 32 * time.Second}, // Capped at 30s max
	}

	for _, tc := range testCases {
		ps.restartCount = tc.restartCount
		backoff := ps.GetRestartBackoff()

		if tc.minBackoff == 0 && tc.maxBackoff == 0 {
			if backoff != 0 {
				t.Errorf("Expected 0 backoff at restart %d, got %v", tc.restartCount, backoff)
			}
		} else {
			// Check with jitter tolerance (±10%)
			if backoff < tc.minBackoff || backoff > tc.maxBackoff+ps.maxRestartBackoff/10 {
				t.Errorf("Backoff at restart %d out of range [%v, %v], got %v",
					tc.restartCount, tc.minBackoff, tc.maxBackoff, backoff)
			}
		}
	}
}

func TestProviderSupervisor_RecordRestart(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	ps := NewProviderSupervisor(logger, "test-provider")

	// Record crashes to increment counter
	ps.RecordCrash()
	ps.RecordCrash()
	ps.RecordCrash()

	stats := ps.GetStats()
	if stats["consecutive_crashes"] != int32(3) {
		t.Error("Crash counter not incremented")
	}

	// Record restart
	ps.RecordRestart("crash_recovery")

	// Crash counter should be reset
	stats = ps.GetStats()
	if stats["consecutive_crashes"] != int32(0) {
		t.Error("Consecutive crashes not reset on restart")
	}

	if stats["restart_count"] != int32(1) {
		t.Error("Restart counter not incremented")
	}

	// Health should be restored
	if ps.GetHealth() != HealthHealthy {
		t.Error("Health not reset to healthy after restart")
	}
}

func TestProviderSupervisor_ResetRestartCount(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	ps := NewProviderSupervisor(logger, "test-provider")

	// Record restart
	ps.RecordRestart("test")
	if ps.GetStats()["restart_count"] != int32(1) {
		t.Error("Restart count not incremented")
	}

	// Reset
	ps.ResetRestartCount()
	if ps.GetStats()["restart_count"] != int32(0) {
		t.Error("Restart count not reset")
	}
}

func TestProviderSupervisor_UptimeTracking(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	ps := NewProviderSupervisor(logger, "test-provider")

	uptime1 := ps.GetUptime()
	time.Sleep(50 * time.Millisecond)
	uptime2 := ps.GetUptime()

	if uptime2 <= uptime1 {
		t.Errorf("Uptime should increase, got %v then %v", uptime1, uptime2)
	}

	// After restart, uptime should reset
	ps.RecordRestart("test")
	uptime3 := ps.GetUptime()
	if uptime3 >= uptime2 {
		t.Errorf("Uptime should reset on restart, got %v", uptime3)
	}
}

func TestProviderSupervisor_HeartbeatStaleness(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	ps := NewProviderSupervisor(logger, "test-provider")
	ps.heartbeatTimeout = 100 * time.Millisecond

	// Fresh heartbeat should not be stale
	ps.MarkHealthy()
	if ps.IsHeartbeatStale() {
		t.Error("Fresh heartbeat should not be stale")
	}

	// After timeout, should be stale
	time.Sleep(150 * time.Millisecond)
	if !ps.IsHeartbeatStale() {
		t.Error("Stale heartbeat not detected")
	}

	// Refresh
	ps.MarkHealthy()
	if ps.IsHeartbeatStale() {
		t.Error("Refreshed heartbeat should not be stale")
	}
}

func TestProviderSupervisor_ConcurrentOperations(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	ps := NewProviderSupervisor(logger, "test-provider")
	done := make(chan bool)
	errorCount := int32(0)

	// Multiple goroutines performing concurrent operations
	for i := 0; i < 4; i++ {
		go func(id int) {
			for j := 0; j < 50; j++ {
				switch j % 4 {
				case 0:
					ps.MarkHealthy()
				case 1:
					ps.RecordCrash()
				case 2:
					ps.RecordHeartbeatFailure()
				case 3:
					_ = ps.GetStats()
				}
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 4; i++ {
		<-done
	}

	if errorCount > 0 {
		t.Errorf("Concurrent operations produced %d errors", errorCount)
	}

	// Verify consistency
	stats := ps.GetStats()
	if stats == nil {
		t.Error("Stats should be non-nil after concurrent operations")
	}
}

func TestProviderSupervisor_StatisticsConsistency(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	ps := NewProviderSupervisor(logger, "test-provider")

	// Perform sequence of operations
	ps.RecordCrash()
	ps.RecordHeartbeatFailure()
	ps.RecordHeartbeatFailure()
	ps.RecordRestart("test")
	ps.MarkHealthy()

	stats := ps.GetStats()

	// Verify all expected fields present
	expected := []string{
		"provider", "health", "uptime_seconds", "restart_count",
		"crash_count", "consecutive_crashes", "consecutive_failures",
		"time_since_heartbeat", "heartbeat_stale",
	}

	for _, key := range expected {
		if _, ok := stats[key]; !ok {
			t.Errorf("Missing stat key: %s", key)
		}
	}

	// Verify reasonable values
	if stats["restart_count"] != int32(1) {
		t.Errorf("Restart count incorrect: %v", stats["restart_count"])
	}

	if stats["crash_count"] != int32(1) {
		t.Errorf("Crash count incorrect: %v", stats["crash_count"])
	}

	if stats["health"] != int(HealthHealthy) {
		t.Errorf("Health status incorrect: %v", stats["health"])
	}
}

func TestProviderSupervisor_FailureCountReset(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	ps := NewProviderSupervisor(logger, "test-provider")

	// Record failures
	ps.RecordHeartbeatFailure()
	ps.RecordHeartbeatFailure()

	if ps.GetStats()["consecutive_failures"] != int32(2) {
		t.Error("Failures not recorded")
	}

	// Healthy should reset failures
	ps.MarkHealthy()

	if ps.GetStats()["consecutive_failures"] != int32(0) {
		t.Error("Consecutive failures not reset on MarkHealthy")
	}
}

func TestProviderSupervisor_HealthStateTransitions(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	ps := NewProviderSupervisor(logger, "test-provider")

	states := []struct {
		action   func()
		expected ProviderHealthState
		desc     string
	}{
		{func() {}, HealthUnknown, "initial state"},
		{func() { ps.MarkHealthy() }, HealthHealthy, "after mark healthy"},
		{func() { ps.RecordCrash(); ps.RecordCrash(); ps.RecordCrash() }, HealthUnhealthy, "after crashes"},
		{func() { ps.MarkHealthy() }, HealthHealthy, "after recovery"},
		{func() { ps.MarkUnhealthy("test") }, HealthUnhealthy, "after mark unhealthy"},
	}

	for _, s := range states {
		s.action()
		if ps.GetHealth() != s.expected {
			t.Errorf("%s: expected %d, got %d", s.desc, s.expected, ps.GetHealth())
		}
	}
}

func TestProviderSupervisor_CrashAndRecoverySequence(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	ps := NewProviderSupervisor(logger, "test-provider")

	// Simulate crash and recovery cycle
	for cycle := 0; cycle < 3; cycle++ {
		// Crash
		ps.RecordCrash()
		if ps.GetStats()["consecutive_crashes"] != int32(1) {
			t.Errorf("Cycle %d: crash not recorded", cycle)
		}

		// Restart
		backoff := ps.GetRestartBackoff()
		if backoff < 0 {
			t.Errorf("Cycle %d: negative backoff", cycle)
		}

		ps.RecordRestart("crash_recovery")
		if ps.GetStats()["consecutive_crashes"] != int32(0) {
			t.Errorf("Cycle %d: crash not reset on restart", cycle)
		}

		// Recover
		ps.MarkHealthy()
		ps.ResetRestartCount()
	}

	if ps.GetStats()["crash_count"] != int32(3) {
		t.Error("Total crash count incorrect")
	}
}
