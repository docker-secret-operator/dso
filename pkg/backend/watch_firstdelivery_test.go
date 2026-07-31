package backend_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/pkg/api"
	"github.com/docker-secret-operator/dso/pkg/backend/env"
	"github.com/docker-secret-operator/dso/pkg/backend/file"
)

// firstDeliveryTimeout is deliberately far shorter than the watch interval
// used below. If a provider only emits on its first tick, the interval (1
// hour) guarantees this deadline is missed, which is exactly the bug being
// guarded against.
const firstDeliveryTimeout = 3 * time.Second

// awaitFirstUpdate returns the first update from ch, or fails the test if none
// arrives before firstDeliveryTimeout.
func awaitFirstUpdate(t *testing.T, ch <-chan api.SecretUpdate, provider string) api.SecretUpdate {
	t.Helper()
	select {
	case u, ok := <-ch:
		if !ok {
			t.Fatalf("%s: watch channel closed before delivering an initial value", provider)
		}
		return u
	case <-time.After(firstDeliveryTimeout):
		t.Fatalf("%s: no initial value within %v — the provider is only emitting on its first tick, "+
			"so a consumer stalls for a full interval", provider, firstDeliveryTimeout)
		return api.SecretUpdate{}
	}
}

// TestWatchSecret_FileProvider_DeliversImmediately is the regression test for
// the first-delivery divergence: AWS/Azure/Huawei emitted an initial value
// straight away, while Vault, file and env waited for the first tick. A
// consumer waiting for an initial value therefore stalled for a full interval
// on half the providers.
func TestWatchSecret_FileProvider_DeliversImmediately(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "db"), []byte(`{"password":"s3cret"}`), 0o600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}

	p := &file.FileProvider{}
	if err := p.Init(map[string]string{"path": dir}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// An hour-long interval: only an immediate first send can satisfy the
	// deadline in awaitFirstUpdate.
	ch, err := p.WatchSecret(ctx, "db", time.Hour)
	if err != nil {
		t.Fatalf("WatchSecret: %v", err)
	}

	u := awaitFirstUpdate(t, ch, "file")
	if u.Error != "" {
		t.Fatalf("unexpected error in first update: %s", u.Error)
	}
	if u.Data["password"] != "s3cret" {
		t.Errorf("first update carried %v, want password=s3cret", u.Data)
	}
}

// TestWatchSecret_EnvProvider_DeliversImmediately covers the env backend.
func TestWatchSecret_EnvProvider_DeliversImmediately(t *testing.T) {
	t.Setenv("DSO_TEST_WATCH_SECRET", "from-env")

	p := &env.EnvProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := p.WatchSecret(ctx, "DSO_TEST_WATCH_SECRET", time.Hour)
	if err != nil {
		t.Fatalf("WatchSecret: %v", err)
	}

	u := awaitFirstUpdate(t, ch, "env")
	if u.Error != "" {
		t.Fatalf("unexpected error in first update: %s", u.Error)
	}
	if u.Data["value"] != "from-env" {
		t.Errorf("first update carried %v, want value=from-env", u.Data)
	}
}

// TestWatchSecret_EnvProvider_DeliversErrorImmediately confirms the immediate
// send also reports a failure promptly, rather than leaving the consumer to
// wait an interval to discover the secret is missing. This pairs with the
// SEC-8 change that made an unset variable an error instead of an empty
// secret.
func TestWatchSecret_EnvProvider_DeliversErrorImmediately(t *testing.T) {
	p := &env.EnvProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := p.WatchSecret(ctx, "DSO_TEST_DEFINITELY_UNSET_VAR", time.Hour)
	if err != nil {
		t.Fatalf("WatchSecret: %v", err)
	}

	u := awaitFirstUpdate(t, ch, "env")
	if u.Error == "" {
		t.Error("expected the first update to report the unset variable as an error")
	}
	if len(u.Data) != 0 {
		t.Errorf("expected no data alongside the error, got %v", u.Data)
	}
}

// TestWatchSecret_CancelledContextClosesChannel confirms the immediate-send
// path still honors cancellation: a context cancelled before the watch starts
// must not emit, and the channel must close so consumers ranging over it
// terminate.
func TestWatchSecret_CancelledContextClosesChannel(t *testing.T) {
	t.Setenv("DSO_TEST_WATCH_SECRET", "from-env")

	p := &env.EnvProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already done before WatchSecret runs

	ch, err := p.WatchSecret(ctx, "DSO_TEST_WATCH_SECRET", time.Hour)
	if err != nil {
		t.Fatalf("WatchSecret: %v", err)
	}

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("a pre-cancelled watch emitted an update instead of shutting down")
		}
	case <-time.After(firstDeliveryTimeout):
		t.Error("watch channel was not closed after context cancellation")
	}
}
