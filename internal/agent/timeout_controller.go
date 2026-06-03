package agent

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// timerEntry pairs a cancel function with a monotonic generation ID so that
// the cleanup closure can detect whether it is still the current owner of a
// given secret slot before deleting the entry.
type timerEntry struct {
	cancel context.CancelFunc
	gen    uint64
}

// TimeoutController manages per-secret timeout contexts to prevent cascading timeouts
// where one slow provider blocks all other rotations
type TimeoutController struct {
	logger  *zap.Logger
	mu      sync.RWMutex
	timers  map[string]timerEntry
	nextGen uint64 // monotonically increasing generation counter
}

// NewTimeoutController creates a new timeout controller
func NewTimeoutController(logger *zap.Logger) *TimeoutController {
	return &TimeoutController{
		logger: logger,
		timers: make(map[string]timerEntry),
	}
}

// CreateSecretContext creates a context with timeout for a specific secret.
// The timeout is isolated per secret — one slow secret won't affect others.
//
// If a context for secretName already exists (e.g. concurrent webhook + poll),
// the previous cancel is called before the new context is created, preventing
// goroutine leaks from abandoned contexts.
func (tc *TimeoutController) CreateSecretContext(parentCtx context.Context, secretName string, timeout time.Duration) (context.Context, func()) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	// Cancel any existing context for this secret to prevent leaks when two
	// concurrent operations use the same key (e.g. poll + webhook racing).
	if existing, ok := tc.timers[secretName]; ok {
		existing.cancel()
	}

	// Assign a unique generation ID so the cleanup closure can verify it is
	// still the current owner before removing the map entry.
	gen := atomic.AddUint64(&tc.nextGen, 1)

	// Create timeout context for this specific secret.
	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	tc.timers[secretName] = timerEntry{cancel: cancel, gen: gen}

	// Return context and a cleanup function. The cleanup cancels the context
	// (safe to call multiple times via context.WithTimeout) and removes the
	// map entry only if this closure is still the current owner.
	return ctx, func() {
		tc.mu.Lock()
		defer tc.mu.Unlock()
		cancel()
		// Only remove from the map if we are still the current owner.
		// A concurrent CreateSecretContext may have replaced our entry; deleting
		// blindly would evict the newer cancel and break CancelSecret().
		if stored, ok := tc.timers[secretName]; ok && stored.gen == gen {
			delete(tc.timers, secretName)
		}
	}
}

// CancelSecret immediately cancels timeout for a specific secret
func (tc *TimeoutController) CancelSecret(secretName string) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	if entry, exists := tc.timers[secretName]; exists {
		entry.cancel()
		delete(tc.timers, secretName)
	}
}

// GetActiveSecrets returns list of secrets with active timeouts
func (tc *TimeoutController) GetActiveSecrets() []string {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	var secrets []string
	for secret := range tc.timers {
		secrets = append(secrets, secret)
	}
	return secrets
}

// TimeoutIsolationWrapper wraps RPC calls with timeout monitoring
// to detect and log slow operations without cascading failures
type TimeoutIsolationWrapper struct {
	logger     *zap.Logger
	controller *TimeoutController
}

// NewTimeoutIsolationWrapper creates a new wrapper
func NewTimeoutIsolationWrapper(logger *zap.Logger, controller *TimeoutController) *TimeoutIsolationWrapper {
	return &TimeoutIsolationWrapper{
		logger:     logger,
		controller: controller,
	}
}

// ExecuteWithTimeout executes operation with per-secret timeout isolation
// Returns error if timeout exceeded for this specific operation
func (tw *TimeoutIsolationWrapper) ExecuteWithTimeout(
	parentCtx context.Context,
	secretName string,
	timeout time.Duration,
	operation func(ctx context.Context) error,
) error {
	ctx, cleanup := tw.controller.CreateSecretContext(parentCtx, secretName, timeout)
	defer cleanup()

	// Track slow operations
	slowCh := time.After(timeout / 2) // Warn at 50% of timeout

	// Execute operation
	errCh := make(chan error, 1)
	go func() {
		errCh <- operation(ctx)
	}()

	select {
	case <-slowCh:
		tw.logger.Warn("Secret operation approaching timeout",
			zap.String("secret", secretName),
			zap.Duration("timeout", timeout),
			zap.String("action", "consider increasing timeout or checking provider health"))
		// Continue waiting for actual timeout
		return <-errCh

	case err := <-errCh:
		return err

	case <-ctx.Done():
		return fmt.Errorf("secret operation timeout: %s (timeout: %v)", secretName, timeout)
	}
}

// ExecuteWithRaceProtection wraps multiple secrets with race protection
// ensuring they execute in isolation from each other's timeouts
func (tw *TimeoutIsolationWrapper) ExecuteWithRaceProtection(
	parentCtx context.Context,
	secrets []string,
	globalTimeout time.Duration,
	perSecretTimeout time.Duration,
	operation func(ctx context.Context, secretName string) error,
) map[string]error {
	results := make(map[string]error)
	resultMu := sync.Mutex{}

	// Use sync.WaitGroup for coordination
	var wg sync.WaitGroup
	globalCtx, cancel := context.WithTimeout(parentCtx, globalTimeout)
	defer cancel()

	for _, secret := range secrets {
		wg.Add(1)
		go func(secretName string) {
			defer wg.Done()

			ctx, cleanup := tw.controller.CreateSecretContext(globalCtx, secretName, perSecretTimeout)
			defer cleanup()

			err := operation(ctx, secretName)

			resultMu.Lock()
			results[secretName] = err
			resultMu.Unlock()

			if err != nil {
				tw.logger.Debug("Secret operation error",
					zap.String("secret", secretName),
					zap.Error(err))
			}
		}(secret)
	}

	wg.Wait()
	return results
}

// MonitorTimeouts logs active timeouts periodically (useful for debugging)
func (tc *TimeoutController) MonitorTimeouts(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			active := tc.GetActiveSecrets()
			if len(active) > 0 {
				tc.logger.Debug("Active secret operations",
					zap.Int("count", len(active)),
					zap.Strings("secrets", active))
			}
		}
	}
}
