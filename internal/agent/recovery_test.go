package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"go.uber.org/zap/zaptest"
)

// recoveryMockTransport routes Docker Engine API requests to a per-test
// handler function, matching the pattern already used in
// internal/rotation/rolling_strategy_test.go.
type recoveryMockTransport struct {
	reqFunc func(req *http.Request) (*http.Response, error)
}

func (m *recoveryMockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.reqFunc(req)
}

func jsonBody(v interface{}) io.ReadCloser {
	b, _ := json.Marshal(v)
	return io.NopCloser(bytes.NewReader(b))
}

func emptyBody() io.ReadCloser {
	return io.NopCloser(bytes.NewReader([]byte{}))
}

func newRecoveryTestClient(t *testing.T, handler func(req *http.Request) (*http.Response, error)) *client.Client {
	t.Helper()
	httpClient := &http.Client{Transport: &recoveryMockTransport{reqFunc: handler}}
	cli, err := client.NewClientWithOpts(
		client.WithVersion("1.41"),
		client.WithHost("tcp://127.0.0.1:2375"),
		client.WithHTTPClient(httpClient),
	)
	if err != nil {
		t.Fatalf("failed to construct mock docker client: %v", err)
	}
	return cli
}

// TestRecoverFromCrash_NoPendingRotations verifies the early-return path:
// when there are no pending rotations, RecoverFromCrash must return
// immediately without ever calling the Docker API (a nil cli would panic if
// it tried, which doubles as this test's own safety check).
func TestRecoverFromCrash_NoPendingRotations(t *testing.T) {
	logger := zaptest.NewLogger(t)
	st, err := NewStateTracker(t.TempDir(), logger)
	if err != nil {
		t.Fatalf("NewStateTracker: %v", err)
	}

	ar := NewAutomaticRecovery(nil, logger, st)

	if err := ar.RecoverFromCrash(context.Background()); err != nil {
		t.Fatalf("expected nil error on empty pending rotations, got %v", err)
	}
}

// TestRecoverFromCrash_NilStateTracker verifies RecoverFromCrash is a no-op
// (not a panic) when no state tracker is configured.
func TestRecoverFromCrash_NilStateTracker(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ar := NewAutomaticRecovery(nil, logger, nil)

	if err := ar.RecoverFromCrash(context.Background()); err != nil {
		t.Fatalf("expected nil error with no state tracker configured, got %v", err)
	}
}

// TestValidateStateOnStartup_DiscardsStaleState verifies that a pending
// rotation older than 24 hours is discarded (marked recovered) rather than
// left to accumulate indefinitely, and that a fresh pending rotation is left
// untouched.
func TestValidateStateOnStartup_DiscardsStaleState(t *testing.T) {
	cli := newRecoveryTestClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method == "GET" && req.URL.Path == "/v1.41/containers/json" {
			return &http.Response{StatusCode: 200, Body: jsonBody([]container.Summary{})}, nil
		}
		return &http.Response{StatusCode: 204, Body: emptyBody()}, nil
	})

	logger := zaptest.NewLogger(t)
	st, err := NewStateTracker(t.TempDir(), logger)
	if err != nil {
		t.Fatalf("NewStateTracker: %v", err)
	}
	if err := st.StartRotation("aws", "stale_secret", "stale-id", ""); err != nil {
		t.Fatalf("StartRotation: %v", err)
	}
	// Force this entry into the pending set via rollback_required, then
	// backdate it past the 24h staleness threshold used by
	// ValidateStateOnStartup.
	if err := st.MarkRollback("aws", "stale_secret", "stale-id"); err != nil {
		t.Fatalf("MarkRollback: %v", err)
	}
	pendingBefore := st.GetPendingRotations()
	if len(pendingBefore) != 1 {
		t.Fatalf("expected 1 pending rotation before backdating, got %d", len(pendingBefore))
	}
	// The tracker returns copies, so backdate the live record under its own lock.
	st.mu.Lock()
	for _, live := range st.states {
		live.StartTime = live.StartTime.Add(-25 * time.Hour)
	}
	st.mu.Unlock()

	ar := NewAutomaticRecovery(cli, logger, st)
	ok := ar.ValidateStateOnStartup(context.Background())
	if !ok {
		t.Error("expected ValidateStateOnStartup to return true (stale state is discarded, not treated as corruption)")
	}

	pendingAfter := st.GetPendingRotations()
	if len(pendingAfter) != 0 {
		t.Fatalf("expected stale rotation to be discarded (marked recovered), still pending=%+v", pendingAfter)
	}
}

// TestValidateStateOnStartup_DockerUnreachable verifies that an unreachable
// Docker daemon is treated as "cannot validate right now", not as state
// corruption -- ValidateStateOnStartup must still return true so startup
// isn't blocked by a transient Docker outage.
func TestValidateStateOnStartup_DockerUnreachable(t *testing.T) {
	cli := newRecoveryTestClient(t, func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("connection refused")
	})

	logger := zaptest.NewLogger(t)
	st, err := NewStateTracker(t.TempDir(), logger)
	if err != nil {
		t.Fatalf("NewStateTracker: %v", err)
	}
	if err := st.StartRotation("aws", "db_pass", "orig-id", ""); err != nil {
		t.Fatalf("StartRotation: %v", err)
	}
	if err := st.MarkRollback("aws", "db_pass", "orig-id"); err != nil {
		t.Fatalf("MarkRollback: %v", err)
	}

	ar := NewAutomaticRecovery(cli, logger, st)
	if ok := ar.ValidateStateOnStartup(context.Background()); !ok {
		t.Error("expected ValidateStateOnStartup to return true when Docker is unreachable (not treated as corruption)")
	}
}

// ============================================================================
// Phase 7 / P2-1: restart-strategy "_old_<timestamp>" orphan naming gap.
// ============================================================================

// TestIsDSORestartOldContainerName_MatchesRealNamingConvention verifies the
// narrow, suffix-anchored regex matches exactly what
// internal/watcher/controller.go's executeRollback/executeSimpleRestart
// actually construct ("<originalName>_old_<unix timestamp>"), and does NOT
// match operator-owned container names that merely contain "_old_"
// somewhere -- this is the specific security requirement from the Phase 6
// audit: never turn this into a broad substring deletion rule.
func TestIsDSORestartOldContainerName_MatchesRealNamingConvention(t *testing.T) {
	cases := []struct {
		name    string
		matches bool
	}{
		{"myapp_old_1700000000", true},
		{"payments-api_old_1789104000", true},
		{"my_old_database", false},             // unrelated operator container
		{"legacy_old_service", false},          // unrelated operator container
		{"old_backup_service", false},          // "_old_" not present at all
		{"myapp_old_", false},                  // no digits after "_old_"
		{"myapp_old_abc", false},               // non-numeric suffix
		{"myapp_old_123_extra", false},         // "_old_<digits>" not at the end
		{"myapp_dso_backup_1700000000", false}, // rolling-strategy pattern, not this one
		{"myapp_dso_new_1700000000", false},    // rolling-strategy pattern, not this one
	}
	for _, c := range cases {
		if got := isDSORestartOldContainerName(c.name); got != c.matches {
			t.Errorf("isDSORestartOldContainerName(%q) = %v, want %v", c.name, got, c.matches)
		}
	}
}
