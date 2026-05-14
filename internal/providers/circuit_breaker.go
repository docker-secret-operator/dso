package providers

import (
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// CircuitBreakerState represents the state of a circuit breaker
type CircuitBreakerState string

const (
	StateClosed CircuitBreakerState = "closed"   // Normal operation
	StateOpen   CircuitBreakerState = "open"     // Too many failures, rejecting requests
	StateHalf   CircuitBreakerState = "half_open" // Testing if the provider recovered
)

// CircuitBreaker implements the circuit breaker pattern for provider reliability
// This prevents cascading failures when a provider is having issues
type CircuitBreaker struct {
	logger              *zap.Logger
	provider            string
	state               atomic.Value // CircuitBreakerState
	failureCount        int32
	successCount        int32
	lastFailureTime     time.Time
	lastFailureTimeMu   sync.Mutex
	failureThreshold    int32
	successThreshold    int32
	resetTimeout        time.Duration
	lastStateChange     time.Time
	lastStateChangeMu   sync.Mutex
}

// NewCircuitBreaker creates a new circuit breaker for a provider
//
// failureThreshold: consecutive failures before opening (e.g., 5)
// successThreshold: consecutive successes before closing from half-open (e.g., 3)
// resetTimeout: time to wait before trying again when open (e.g., 30s)
func NewCircuitBreaker(provider string, logger *zap.Logger, failureThreshold, successThreshold int32, resetTimeout time.Duration) *CircuitBreaker {
	cb := &CircuitBreaker{
		logger:           logger,
		provider:         provider,
		failureThreshold: failureThreshold,
		successThreshold: successThreshold,
		resetTimeout:     resetTimeout,
		lastFailureTime:  time.Now(),
		lastStateChange:  time.Now(),
	}
	cb.state.Store(StateClosed)
	return cb
}

// IsAvailable returns true if the circuit breaker allows requests
func (cb *CircuitBreaker) IsAvailable() bool {
	state := cb.getState()
	switch state {
	case StateClosed:
		return true // Accept all requests
	case StateOpen:
		// Check if we should transition to half-open
		if time.Since(cb.getLastFailureTime()) > cb.resetTimeout {
			cb.setState(StateHalf)
			cb.resetCounters()
			return true // Allow one test request
		}
		return false // Still open, reject requests
	case StateHalf:
		return true // Allow test requests
	default:
		return false
	}
}

// RecordSuccess records a successful request
func (cb *CircuitBreaker) RecordSuccess() {
	state := cb.getState()

	atomic.AddInt32(&cb.successCount, 1)
	failures := atomic.LoadInt32(&cb.failureCount)

	if failures > 0 {
		cb.logger.Info("Provider recovering",
			zap.String("provider", cb.provider),
			zap.Int32("consecutive_failures_reset", failures))
	}

	atomic.StoreInt32(&cb.failureCount, 0)

	// If in half-open, transition to closed after N successes
	if state == StateHalf {
		successes := atomic.LoadInt32(&cb.successCount)
		if successes >= cb.successThreshold {
			cb.setState(StateClosed)
			cb.resetCounters()
			cb.logger.Info("Provider circuit breaker closed",
				zap.String("provider", cb.provider),
				zap.Int32("consecutive_successes", successes))
		}
	}
}

// RecordFailure records a failed request
func (cb *CircuitBreaker) RecordFailure() {
	state := cb.getState()

	failures := atomic.AddInt32(&cb.failureCount, 1)
	atomic.StoreInt32(&cb.successCount, 0)

	cb.lastFailureTimeMu.Lock()
	cb.lastFailureTime = time.Now()
	cb.lastFailureTimeMu.Unlock()

	cb.logger.Warn("Provider request failed",
		zap.String("provider", cb.provider),
		zap.Int32("consecutive_failures", failures))

	// If failures exceed threshold, open the circuit
	if state == StateClosed && failures >= cb.failureThreshold {
		cb.setState(StateOpen)
		cb.logger.Error("Provider circuit breaker opened",
			zap.String("provider", cb.provider),
			zap.Int32("failure_threshold", cb.failureThreshold),
			zap.Duration("reset_timeout", cb.resetTimeout))
	}

	// If in half-open, immediately reopen on any failure
	if state == StateHalf {
		cb.setState(StateOpen)
		cb.resetCounters()
		cb.logger.Warn("Provider circuit breaker reopened (half-open test failed)",
			zap.String("provider", cb.provider))
	}
}

// GetState returns the current circuit breaker state
func (cb *CircuitBreaker) GetState() CircuitBreakerState {
	return cb.getState()
}

// GetStatus returns detailed status of the circuit breaker
func (cb *CircuitBreaker) GetStatus() map[string]interface{} {
	return map[string]interface{}{
		"provider":                cb.provider,
		"state":                   cb.getState(),
		"consecutive_failures":    atomic.LoadInt32(&cb.failureCount),
		"consecutive_successes":   atomic.LoadInt32(&cb.successCount),
		"time_since_last_failure": time.Since(cb.getLastFailureTime()),
		"failure_threshold":       cb.failureThreshold,
		"success_threshold":       cb.successThreshold,
		"reset_timeout":           cb.resetTimeout,
	}
}

// Private helper methods

func (cb *CircuitBreaker) getState() CircuitBreakerState {
	return cb.state.Load().(CircuitBreakerState)
}

func (cb *CircuitBreaker) setState(state CircuitBreakerState) {
	cb.lastStateChangeMu.Lock()
	cb.lastStateChange = time.Now()
	cb.lastStateChangeMu.Unlock()
	cb.state.Store(state)
}

func (cb *CircuitBreaker) getLastFailureTime() time.Time {
	cb.lastFailureTimeMu.Lock()
	defer cb.lastFailureTimeMu.Unlock()
	return cb.lastFailureTime
}

func (cb *CircuitBreaker) resetCounters() {
	atomic.StoreInt32(&cb.failureCount, 0)
	atomic.StoreInt32(&cb.successCount, 0)
}
