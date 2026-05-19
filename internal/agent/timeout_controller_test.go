package agent

import (
	"context"
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
