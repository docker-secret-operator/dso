package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
)

func TestTimeoutController_Basic(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tc := NewTimeoutController(logger)

	// CreateSecretContext and cleanup
	ctx, cleanup := tc.CreateSecretContext(context.Background(), "my-secret", 5*time.Second)
	if ctx.Err() != nil {
		t.Fatal("context should not be cancelled yet")
	}

	active := tc.GetActiveSecrets()
	if len(active) != 1 || active[0] != "my-secret" {
		t.Errorf("expected [my-secret], got %v", active)
	}

	cleanup()

	active = tc.GetActiveSecrets()
	if len(active) != 0 {
		t.Errorf("expected empty after cleanup, got %v", active)
	}
}

func TestTimeoutController_CancelSecret(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tc := NewTimeoutController(logger)

	_, cleanup := tc.CreateSecretContext(context.Background(), "cancel-me", 30*time.Second)
	defer cleanup()

	tc.CancelSecret("cancel-me")
	if len(tc.GetActiveSecrets()) != 0 {
		t.Error("expected secret removed after CancelSecret")
	}

	// CancelSecret on non-existent key — should not panic
	tc.CancelSecret("does-not-exist")
}

func TestTimeoutIsolationWrapper_Execute(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tc := NewTimeoutController(logger)
	wrapper := NewTimeoutIsolationWrapper(logger, tc)

	// Fast operation — completes before timeout
	err := wrapper.ExecuteWithTimeout(context.Background(), "fast-secret", 5*time.Second, func(ctx context.Context) error {
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestTimeoutController_NoCancelLeak_H1 verifies that creating a second context
// for the same secret name cancels the first context (H1 regression test).
// Without the fix, ctx1 would be leaked until its 30s timeout fires.
func TestTimeoutController_NoCancelLeak_H1(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tc := NewTimeoutController(logger)

	ctx1, cleanup1 := tc.CreateSecretContext(context.Background(), "shared", 30*time.Second)
	defer cleanup1()

	// Second call with the same name — must cancel ctx1.
	ctx2, cleanup2 := tc.CreateSecretContext(context.Background(), "shared", 30*time.Second)
	defer cleanup2()

	select {
	case <-ctx1.Done():
		// Correct: previous context was cancelled.
	case <-time.After(200 * time.Millisecond):
		t.Error("H1 regression: first context was not cancelled when overwritten by second CreateSecretContext call")
	}

	// ctx2 must still be live.
	select {
	case <-ctx2.Done():
		t.Error("second context should not be cancelled yet")
	default:
	}
}

// TestTimeoutController_ConcurrentSameName_H1 stress-tests the concurrent-
// overwrite path with the race detector enabled.
func TestTimeoutController_ConcurrentSameName_H1(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tc := NewTimeoutController(logger)

	const goroutines = 20
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cleanup := tc.CreateSecretContext(context.Background(), "race-key", 5*time.Second)
			defer cleanup()
			// May be cancelled concurrently — that is correct.
			_ = ctx
		}()
	}
	wg.Wait()
}

func TestTimeoutIsolationWrapper_RaceProtection(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tc := NewTimeoutController(logger)
	wrapper := NewTimeoutIsolationWrapper(logger, tc)

	results := wrapper.ExecuteWithRaceProtection(
		context.Background(),
		[]string{"s1", "s2"},
		10*time.Second,
		5*time.Second,
		func(ctx context.Context, name string) error {
			return nil
		},
	)

	for secret, err := range results {
		if err != nil {
			t.Errorf("secret %s got error: %v", secret, err)
		}
	}
}
