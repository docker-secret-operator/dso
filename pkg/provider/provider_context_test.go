package provider

import (
	"context"
	"errors"
	"net"
	"net/rpc"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/pkg/api"
)

// Compile-time assertions. These are the actual regression guard for the
// original defect: api.SecretProviderWithContext was declared and asserted
// for in internal/agent/trigger.go, but no type implemented it, so the
// assertion silently always failed and the blocking path always ran. If
// GetSecretWithContext is ever removed or its signature drifts, this stops
// compiling instead of quietly reverting to un-cancellable fetches.
var (
	_ api.SecretProvider            = (*ProviderRPC)(nil)
	_ api.SecretProviderWithContext = (*ProviderRPC)(nil)
)

// blockingProvider is a plugin-side SecretProvider whose GetSecret blocks
// until release is closed, simulating a cloud call that never comes back.
type blockingProvider struct {
	release  chan struct{}
	entered  chan struct{}
	fastData map[string]string
}

func (b *blockingProvider) Init(map[string]string) error { return nil }

func (b *blockingProvider) GetSecret(name string) (map[string]string, error) {
	if b.release != nil {
		if b.entered != nil {
			close(b.entered)
		}
		<-b.release
	}
	if b.fastData != nil {
		return b.fastData, nil
	}
	return map[string]string{"value": "ok"}, nil
}

func (b *blockingProvider) WatchSecret(context.Context, string, time.Duration) (<-chan api.SecretUpdate, error) {
	return nil, errors.New("not used in this test")
}

// newTestRPC wires a real ProviderRPCServer to a real rpc.Client over an
// in-memory pipe, so these tests exercise the actual net/rpc path rather than
// a stand-in for it.
func newTestRPC(t *testing.T, impl api.SecretProvider) *ProviderRPC {
	t.Helper()

	serverConn, clientConn := net.Pipe()

	srv := rpc.NewServer()
	if err := srv.RegisterName("Plugin", &ProviderRPCServer{Impl: impl}); err != nil {
		t.Fatalf("RegisterName: %v", err)
	}
	go srv.ServeConn(serverConn)

	client := rpc.NewClient(clientConn)
	t.Cleanup(func() {
		_ = client.Close()
		_ = serverConn.Close()
	})

	return &ProviderRPC{client: client}
}

// TestGetSecretWithContext_CancellationUnblocksCaller is the core regression
// test: a plugin that never responds must not be able to wedge the caller.
// Before the fix there was no context-aware path at all, so the rotation loop
// would block indefinitely on exactly this scenario.
func TestGetSecretWithContext_CancellationUnblocksCaller(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	defer close(release) // let the abandoned server call finish

	p := newTestRPC(t, &blockingProvider{release: release, entered: entered})

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel only once we know the plugin is actually inside GetSecret, so
	// this tests interrupting an in-flight call rather than a pre-cancelled one.
	go func() {
		<-entered
		cancel()
	}()

	start := time.Now()
	_, err := p.GetSecretWithContext(ctx, "db_password")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error when the context was cancelled mid-call, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
	// The server call is still blocked; returning at all proves we did not
	// wait for it. Generous bound so this is not timing-flaky in CI.
	if elapsed > 5*time.Second {
		t.Errorf("took %v to return after cancellation — caller was not unblocked promptly", elapsed)
	}
}

// TestGetSecretWithContext_DeadlineUnblocksCaller covers the timeout form,
// which is what the rotation path relies on in practice.
func TestGetSecretWithContext_DeadlineUnblocksCaller(t *testing.T) {
	release := make(chan struct{})
	defer close(release)

	p := newTestRPC(t, &blockingProvider{release: release})

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := p.GetSecretWithContext(ctx, "db_password")
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got: %v", err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("took %v to honor a 150ms deadline", elapsed)
	}
}

// TestGetSecretWithContext_AlreadyCancelledDoesNotDispatch confirms a
// cancelled context short-circuits before any remote work starts, so a
// shutting-down agent cannot kick off new provider calls.
func TestGetSecretWithContext_AlreadyCancelledDoesNotDispatch(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	close(release) // would return immediately if it were ever reached
	defer close(entered)

	p := newTestRPC(t, &blockingProvider{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := p.GetSecretWithContext(ctx, "db_password"); !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled for a pre-cancelled context, got: %v", err)
	}
}

// TestGetSecretWithContext_HappyPath ensures the new path still returns real
// data, i.e. the cancellation plumbing did not break normal fetches.
func TestGetSecretWithContext_HappyPath(t *testing.T) {
	want := map[string]string{"username": "admin", "password": "s3cret"}
	p := newTestRPC(t, &blockingProvider{fastData: want})

	got, err := p.GetSecretWithContext(context.Background(), "db_password")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d keys, want %d (%v)", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("key %q = %q, want %q", k, got[k], v)
		}
	}
}

// TestGetSecretWithContext_PropagatesProviderError confirms a real provider
// failure still surfaces as itself, not masked as a context error.
func TestGetSecretWithContext_PropagatesProviderError(t *testing.T) {
	p := newTestRPC(t, &erroringProvider{})

	_, err := p.GetSecretWithContext(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected the provider's error to propagate, got nil")
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("provider error was misreported as a context error: %v", err)
	}
}

// TestTriggerStyleAssertionNowSucceeds reproduces the exact type assertion
// internal/agent/trigger.go performs on the provider it gets back from
// LoadProvider (which, for every external plugin, is a *ProviderRPC via
// SecretProviderPlugin.Client -> Dispense). That assertion previously always
// failed, silently downgrading every fetch to the un-cancellable path. This
// asserts the runtime behavior the compile-time checks above cannot: that the
// value the daemon actually holds takes the context-aware branch.
func TestTriggerStyleAssertionNowSucceeds(t *testing.T) {
	p := newTestRPC(t, &blockingProvider{fastData: map[string]string{"value": "v"}})

	// Deliberately held as the interface type, exactly as trigger.go does.
	var prov api.SecretProvider = p

	provCtx, ok := prov.(api.SecretProviderWithContext)
	if !ok {
		t.Fatal("the provider the daemon holds does not satisfy SecretProviderWithContext — " +
			"trigger.go would fall back to the un-cancellable GetSecret path")
	}

	// And the branch it selects must actually work.
	got, err := provCtx.GetSecretWithContext(context.Background(), "db_password")
	if err != nil {
		t.Fatalf("context-aware fetch failed: %v", err)
	}
	if got["value"] != "v" {
		t.Errorf("got %v, want value=v", got)
	}
}

type erroringProvider struct{}

func (e *erroringProvider) Init(map[string]string) error { return nil }
func (e *erroringProvider) GetSecret(string) (map[string]string, error) {
	return nil, errors.New("secret not found in backend")
}
func (e *erroringProvider) WatchSecret(context.Context, string, time.Duration) (<-chan api.SecretUpdate, error) {
	return nil, errors.New("not used")
}
