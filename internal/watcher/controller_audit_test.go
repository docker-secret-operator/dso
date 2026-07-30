package watcher

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/audit"
	dsoConfig "github.com/docker-secret-operator/dso/pkg/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// syncBuffer is a mutex-guarded bytes.Buffer. Needed (not just a plain
// bytes.Buffer) because the rotation audit log is written from a separate
// goroutine (TriggerReload's rolling-rotation goroutine) while the test polls
// it from the main test goroutine -- bytes.Buffer itself is not safe for
// concurrent use, and a bare buffer here would be a real, race-detector-
// confirmed data race in the test, not the production code.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func (s *syncBuffer) Sync() error { return nil }

// captureAuditLog redirects internal/audit's package-level logger to a
// buffer for the duration of the test and returns it.
func captureAuditLog(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		buf,
		zapcore.DebugLevel,
	)
	audit.InitAuditLogger(zap.New(core))
	return buf
}

// waitForAuditLog polls buf until it contains want or the deadline passes,
// since the rolling-rotation goroutine that emits the audit record runs
// asynchronously to TriggerReload's return.
func waitForAuditLog(t *testing.T, buf *syncBuffer, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for audit log to contain %q; got: %s", want, buf.String())
}

// TestTriggerReload_RollingRotation_AuditLogsFailure is a regression test for
// AUDIT-3: wiring internal/audit into the rolling (blue-green) rotation
// strategy's real, final outcome -- previously AUDIT-1 covered secret
// *access* only, and rotation-completion events produced no audit trail at
// all. Uses an inspect failure (500) to reach the "failed" outcome quickly
// and deterministically, without needing to mock the full multi-step
// container-swap sequence.
func TestTriggerReload_RollingRotation_AuditLogsFailure(t *testing.T) {
	buf := captureAuditLog(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"daemon error"}`))
	})
	rc := newMockController(t, handler)
	rc.Config = &dsoConfig.Config{
		Providers: map[string]dsoConfig.ProviderConfig{"vault": {}},
		Secrets:   []dsoConfig.SecretMapping{{Name: "db_password", Provider: "vault"}},
	}
	rc.Targets.Store("container-1", &TargetContainer{
		ID:       "container-1",
		Strategy: "rolling",
		Secrets:  []string{"db_password"},
	})

	if err := rc.TriggerReload(context.Background(), "db_password", nil); err != nil {
		t.Fatalf("TriggerReload returned an unexpected synchronous error: %v", err)
	}

	waitForAuditLog(t, buf, `"event":"rotate"`, 2*time.Second)
	out := buf.String()
	for _, want := range []string{
		`"event":"rotate"`,
		`"user":"system"`,
		`"provider":"vault"`,
		`"secret_name":"db_password"`,
		`"container_id":"container-1"`,
		`"status":"failed"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected rotation audit log to contain %s, got: %s", want, out)
		}
	}
}

// TestTriggerReload_OnComplete_FiresAfterAsyncRotationFinishes is a
// regression test for REL-1: TriggerReload's rolling-rotation strategy runs
// in a detached goroutine that can still be in flight when TriggerReload
// returns. Before the fix, callers had no way to know when that goroutine
// actually finished, so state (e.g. StateTracker.CompleteRotation) was marked
// "done" prematurely. This test verifies onComplete is invoked exactly once,
// strictly after TriggerReload has returned, with the async goroutine's real
// error outcome -- not before, and not with a false "success".
func TestTriggerReload_OnComplete_FiresAfterAsyncRotationFinishes(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"daemon error"}`))
	})
	rc := newMockController(t, handler)
	rc.Config = &dsoConfig.Config{
		Providers: map[string]dsoConfig.ProviderConfig{"vault": {}},
		Secrets:   []dsoConfig.SecretMapping{{Name: "db_password", Provider: "vault"}},
	}
	rc.Targets.Store("container-1", &TargetContainer{
		ID:       "container-1",
		Strategy: "rolling",
		Secrets:  []string{"db_password"},
	})

	var (
		mu        sync.Mutex
		called    bool
		gotErr    error
		firedTime time.Time
	)
	onComplete := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		called = true
		gotErr = err
		firedTime = time.Now()
	}

	beforeReturn := time.Now()
	if err := rc.TriggerReload(context.Background(), "db_password", onComplete); err != nil {
		t.Fatalf("TriggerReload returned an unexpected synchronous error: %v", err)
	}
	afterReturn := time.Now()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		done := called
		mu.Unlock()
		if done {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if !called {
		t.Fatal("onComplete was never called")
	}
	if gotErr == nil {
		t.Error("onComplete was called with a nil error, but the rolling rotation should have failed (inspect returns 500)")
	}
	if firedTime.Before(afterReturn) {
		t.Errorf("onComplete fired before TriggerReload returned (at %v, TriggerReload returned between %v and %v) -- state should not be marked complete until the async work actually finishes", firedTime, beforeReturn, afterReturn)
	}
}

// TestTriggerReload_NonRollingStrategy_NoRotationAuditLog confirms the
// "signal" strategy (which never calls into internal/rotation) does not
// itself emit a "rotate" audit event -- this fix is deliberately scoped to
// the rolling/blue-green strategy, not every reload path.
func TestTriggerReload_NonRollingStrategy_NoRotationAuditLog(t *testing.T) {
	buf := captureAuditLog(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	rc := newMockController(t, handler)
	rc.Targets.Store("container-2", &TargetContainer{
		ID:       "container-2",
		Strategy: "signal",
		Secrets:  []string{"db_password"},
	})

	if err := rc.TriggerReload(context.Background(), "db_password", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// "signal" completes synchronously (no goroutine), so no polling needed.
	if strings.Contains(buf.String(), `"event":"rotate"`) {
		t.Errorf("expected no rotation audit event for the signal strategy, got: %s", buf.String())
	}
}
