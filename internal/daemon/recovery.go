package daemon

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/docker-secret-operator/dso/pkg/observability"
	"github.com/docker/docker/client"
	"go.uber.org/zap"
)

// ConnectionState represents the state of the Docker daemon connection
type ConnectionState int32

const (
	StateDisconnected ConnectionState = iota
	StateConnecting
	StateConnected
	StateResyncing
)

// RecoveryManager handles Docker daemon reconnection and state recovery
type RecoveryManager struct {
	logger              *zap.Logger
	client              *client.Client
	state               atomic.Int32
	lastHealthyTime     time.Time
	consecutiveFailures int32
	maxFailures         int32
	mu                  sync.RWMutex
	onStateChange       func(oldState, newState ConnectionState)
	healthCheckInterval time.Duration
}

// NewRecoveryManager creates a new daemon recovery manager
func NewRecoveryManager(logger *zap.Logger, client *client.Client, maxFailures int32) *RecoveryManager {
	return &RecoveryManager{
		logger:              logger,
		client:              client,
		lastHealthyTime:     time.Now(),
		maxFailures:         maxFailures,
		healthCheckInterval: 30 * time.Second,
		onStateChange: func(oldState, newState ConnectionState) {
			logger.Info("Daemon connection state changed",
				zap.Int32("from", int32(oldState)),
				zap.Int32("to", int32(newState)))
		},
	}
}

// GetState returns current connection state
func (rm *RecoveryManager) GetState() ConnectionState {
	return ConnectionState(rm.state.Load())
}

// SetState transitions to a new state
func (rm *RecoveryManager) SetState(newState ConnectionState) {
	oldState := ConnectionState(rm.state.Swap(int32(newState)))
	if oldState != newState {
		rm.onStateChange(oldState, newState)
	}
}

// MarkHealthy resets failure counter and updates timestamp
func (rm *RecoveryManager) MarkHealthy() {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.lastHealthyTime = time.Now()
	atomic.StoreInt32(&rm.consecutiveFailures, 0)
	rm.SetState(StateConnected)
}

// MarkFailure increments failure counter and potentially triggers recovery
func (rm *RecoveryManager) MarkFailure() bool {
	failures := atomic.AddInt32(&rm.consecutiveFailures, 1)

	if failures >= rm.maxFailures {
		rm.logger.Error("Max consecutive failures reached, initiating recovery",
			zap.Int32("failures", failures),
			zap.Int32("max", rm.maxFailures))
		observability.BackendFailuresTotal.WithLabelValues("daemon", "max_failures_exceeded").Inc()
		return true // Trigger recovery
	}

	observability.BackendFailuresTotal.WithLabelValues("daemon", "failure_tracked").Inc()
	return false
}

// StartHealthCheck runs periodic daemon health checks
func (rm *RecoveryManager) StartHealthCheck(ctx context.Context) {
	ticker := time.NewTicker(rm.healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rm.performHealthCheck(ctx)
		}
	}
}

// performHealthCheck verifies daemon connectivity
func (rm *RecoveryManager) performHealthCheck(ctx context.Context) {
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := rm.client.Ping(checkCtx)
	if err != nil {
		rm.logger.Warn("Health check failed", zap.Error(err))
		if rm.MarkFailure() {
			rm.SetState(StateDisconnected)
		}
	} else {
		rm.MarkHealthy()
	}
}

// ResubscribeEvents resubscribes to Docker events after reconnection
// This prevents missing events during daemon restarts
func (rm *RecoveryManager) ResubscribeEvents(ctx context.Context) error {
	rm.SetState(StateResyncing)
	defer rm.SetState(StateConnected)

	rm.logger.Info("Resubscribing to Docker events after daemon recovery")
	observability.DaemonReconnectsTotal.WithLabelValues("resubscribe").Inc()

	// Note: Actual event subscription is handled by the caller
	// This method ensures the state machine is in sync with actual reconnection

	return nil
}

// GetTimeSinceLastHealthy returns duration since last successful operation
func (rm *RecoveryManager) GetTimeSinceLastHealthy() time.Duration {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return time.Since(rm.lastHealthyTime)
}

// GetFailureCount returns current consecutive failure count
func (rm *RecoveryManager) GetFailureCount() int32 {
	return atomic.LoadInt32(&rm.consecutiveFailures)
}

// IsStale checks if connection is considered stale (no activity > threshold)
func (rm *RecoveryManager) IsStale(threshold time.Duration) bool {
	return rm.GetTimeSinceLastHealthy() > threshold
}

// Reset resets the recovery state to initial conditions
func (rm *RecoveryManager) Reset() {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	atomic.StoreInt32(&rm.consecutiveFailures, 0)
	rm.lastHealthyTime = time.Now()
	rm.SetState(StateConnected)
	rm.logger.Info("Recovery state reset")
}

func stateString(state ConnectionState) string {
	switch state {
	case StateDisconnected:
		return "disconnected"
	case StateConnecting:
		return "connecting"
	case StateConnected:
		return "connected"
	case StateResyncing:
		return "resyncing"
	default:
		return "unknown"
	}
}
