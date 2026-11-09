package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

// TestRecoverSingleRotation_RenameGuard verifies that when the original
// container survived a crash but was renamed mid-rotation (e.g.
// "myapp_old_12345"), recoverSingleRotation refuses to auto-start it --
// doing so could create a duplicate instance and a port conflict if a
// replacement container already holds the real name -- and instead marks
// the rotation critical_error for manual operator review.
func TestRecoverSingleRotation_RenameGuard(t *testing.T) {
	var startCalled bool

	cli := newRecoveryTestClient(t, func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == "GET" && req.URL.Path == "/v1.41/containers/orig-id/json":
			return &http.Response{StatusCode: 200, Body: jsonBody(container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{
					Name:  "/myapp_old_12345",
					State: &container.State{Running: false, Status: "exited"},
				},
			})}, nil
		case req.Method == "POST" && req.URL.Path == "/v1.41/containers/orig-id/start":
			startCalled = true
			return &http.Response{StatusCode: 204, Body: emptyBody()}, nil
		default:
			return &http.Response{StatusCode: 204, Body: emptyBody()}, nil
		}
	})

	logger := zaptest.NewLogger(t)
	st, err := NewStateTracker(t.TempDir(), logger)
	if err != nil {
		t.Fatalf("NewStateTracker: %v", err)
	}
	if err := st.StartRotation("aws", "db_pass", "orig-id", "new-id"); err != nil {
		t.Fatalf("StartRotation: %v", err)
	}

	ar := NewAutomaticRecovery(cli, logger, st)
	rotation := &RotationState{
		ProviderName:        "aws",
		SecretName:          "db_pass",
		OriginalContainerID: "orig-id",
	}

	ar.recoverSingleRotation(context.Background(), map[string]string{}, rotation)

	if startCalled {
		t.Error("expected ContainerStart NOT to be called when the original container was renamed mid-rotation")
	}

	pending := st.GetPendingRotations()
	if len(pending) != 1 || pending[0].Status != "critical_error" {
		t.Fatalf("expected rotation to be marked critical_error for manual review, got pending=%+v", pending)
	}
}

// TestRecoverSingleRotation_NormalRollback verifies the actual automatic
// rollback path: original container survived the crash, is stopped, and
// still owns its original (non-renamed) name -- recoverSingleRotation must
// call ContainerStart to restore service, then mark the rotation recovered.
func TestRecoverSingleRotation_NormalRollback(t *testing.T) {
	var startCalled bool

	cli := newRecoveryTestClient(t, func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == "GET" && req.URL.Path == "/v1.41/containers/orig-id/json":
			return &http.Response{StatusCode: 200, Body: jsonBody(container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{
					Name:  "/myapp",
					State: &container.State{Running: false, Status: "exited"},
				},
			})}, nil
		case req.Method == "POST" && req.URL.Path == "/v1.41/containers/orig-id/start":
			startCalled = true
			return &http.Response{StatusCode: 204, Body: emptyBody()}, nil
		default:
			return &http.Response{StatusCode: 204, Body: emptyBody()}, nil
		}
	})

	logger := zaptest.NewLogger(t)
	st, err := NewStateTracker(t.TempDir(), logger)
	if err != nil {
		t.Fatalf("NewStateTracker: %v", err)
	}
	if err := st.StartRotation("aws", "db_pass", "orig-id", "new-id"); err != nil {
		t.Fatalf("StartRotation: %v", err)
	}

	ar := NewAutomaticRecovery(cli, logger, st)
	rotation := &RotationState{
		ProviderName:        "aws",
		SecretName:          "db_pass",
		OriginalContainerID: "orig-id",
	}

	ar.recoverSingleRotation(context.Background(), map[string]string{}, rotation)

	if !startCalled {
		t.Error("expected ContainerStart to be called to complete automatic rollback")
	}

	// MarkRecovered sets Status to "recovered", which GetPendingRotations no
	// longer returns -- so an empty pending list confirms recovery completed.
	pending := st.GetPendingRotations()
	if len(pending) != 0 {
		t.Fatalf("expected rotation to be marked recovered (no longer pending), got pending=%+v", pending)
	}
}

// TestRecoverSingleRotation_MissingOriginal verifies that when the original
// container is gone entirely after a crash, recovery does not panic and
// marks the rotation critical_error rather than silently dropping it.
func TestRecoverSingleRotation_MissingOriginal(t *testing.T) {
	cli := newRecoveryTestClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method == "GET" && req.URL.Path == "/v1.41/containers/orig-id/json" {
			return &http.Response{StatusCode: 404, Body: jsonBody(map[string]string{
				"message": "No such container: orig-id",
			})}, nil
		}
		return &http.Response{StatusCode: 204, Body: emptyBody()}, nil
	})

	logger := zaptest.NewLogger(t)
	st, err := NewStateTracker(t.TempDir(), logger)
	if err != nil {
		t.Fatalf("NewStateTracker: %v", err)
	}
	if err := st.StartRotation("aws", "db_pass", "orig-id", "new-id"); err != nil {
		t.Fatalf("StartRotation: %v", err)
	}

	ar := NewAutomaticRecovery(cli, logger, st)
	rotation := &RotationState{
		ProviderName:        "aws",
		SecretName:          "db_pass",
		OriginalContainerID: "orig-id",
	}

	// Must not panic.
	ar.recoverSingleRotation(context.Background(), map[string]string{}, rotation)

	pending := st.GetPendingRotations()
	if len(pending) != 1 || pending[0].Status != "critical_error" {
		t.Fatalf("expected rotation to be marked critical_error when original container is missing, got pending=%+v", pending)
	}
	if pending[0].ErrorMessage == "" {
		t.Error("expected a non-empty ErrorMessage explaining the missing-original condition")
	}
}

// TestRecoverSingleRotation_OrphanCleanup verifies that orphaned
// "_dso_backup_"/"_dso_new_" containers are stopped and removed regardless
// of the original container's own state -- this cleanup happens
// unconditionally, before the original-container rollback logic runs.
func TestRecoverSingleRotation_OrphanCleanup(t *testing.T) {
	stopped := map[string]bool{}
	removed := map[string]bool{}

	cli := newRecoveryTestClient(t, func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == "GET" && req.URL.Path == "/v1.41/containers/orig-id/json":
			return &http.Response{StatusCode: 200, Body: jsonBody(container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{
					Name:  "/myapp",
					State: &container.State{Running: true, Status: "running"},
				},
			})}, nil
		case req.Method == "POST" && req.URL.Path == "/v1.41/containers/backup-id/stop":
			stopped["backup-id"] = true
			return &http.Response{StatusCode: 204, Body: emptyBody()}, nil
		case req.Method == "DELETE" && req.URL.Path == "/v1.41/containers/backup-id":
			removed["backup-id"] = true
			return &http.Response{StatusCode: 204, Body: emptyBody()}, nil
		case req.Method == "POST" && req.URL.Path == "/v1.41/containers/new-id/stop":
			stopped["new-id"] = true
			return &http.Response{StatusCode: 204, Body: emptyBody()}, nil
		case req.Method == "DELETE" && req.URL.Path == "/v1.41/containers/new-id":
			removed["new-id"] = true
			return &http.Response{StatusCode: 204, Body: emptyBody()}, nil
		default:
			return &http.Response{StatusCode: 204, Body: emptyBody()}, nil
		}
	})

	logger := zaptest.NewLogger(t)
	st, err := NewStateTracker(t.TempDir(), logger)
	if err != nil {
		t.Fatalf("NewStateTracker: %v", err)
	}
	if err := st.StartRotation("aws", "db_pass", "orig-id", ""); err != nil {
		t.Fatalf("StartRotation: %v", err)
	}

	ar := NewAutomaticRecovery(cli, logger, st)
	rotation := &RotationState{
		ProviderName:        "aws",
		SecretName:          "db_pass",
		OriginalContainerID: "orig-id",
	}

	containersByName := map[string]string{
		"myapp_dso_backup_1700000000": "backup-id",
		"myapp_dso_new_1700000000":    "new-id",
	}

	ar.recoverSingleRotation(context.Background(), containersByName, rotation)

	if !stopped["backup-id"] || !removed["backup-id"] {
		t.Error("expected orphaned backup container to be stopped and removed")
	}
	if !stopped["new-id"] || !removed["new-id"] {
		t.Error("expected orphaned new container to be stopped and removed")
	}
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
	pendingBefore[0].StartTime = pendingBefore[0].StartTime.Add(-25 * time.Hour)

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

// TestCleanupOrphanedContainers_RemovesMatchingNames verifies the broad
// startup sweep stops and removes any container whose name matches DSO's
// "_dso_backup_"/"_dso_new_" orphan patterns, and leaves unrelated
// containers untouched.
func TestCleanupOrphanedContainers_RemovesMatchingNames(t *testing.T) {
	removed := map[string]bool{}

	cli := newRecoveryTestClient(t, func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == "GET" && req.URL.Path == "/v1.41/containers/json":
			return &http.Response{StatusCode: 200, Body: jsonBody([]container.Summary{
				{ID: "backup-id", Names: []string{"/myapp_dso_backup_1700000000"}},
				{ID: "unrelated-id", Names: []string{"/unrelated-service"}},
			})}, nil
		case req.Method == "POST" && req.URL.Path == "/v1.41/containers/backup-id/stop":
			return &http.Response{StatusCode: 204, Body: emptyBody()}, nil
		case req.Method == "DELETE" && req.URL.Path == "/v1.41/containers/backup-id":
			removed["backup-id"] = true
			return &http.Response{StatusCode: 204, Body: emptyBody()}, nil
		case req.Method == "DELETE" && req.URL.Path == "/v1.41/containers/unrelated-id":
			removed["unrelated-id"] = true
			return &http.Response{StatusCode: 204, Body: emptyBody()}, nil
		default:
			return &http.Response{StatusCode: 204, Body: emptyBody()}, nil
		}
	})

	logger := zaptest.NewLogger(t)
	ar := NewAutomaticRecovery(cli, logger, nil)

	if err := ar.CleanupOrphanedContainers(context.Background()); err != nil {
		t.Fatalf("CleanupOrphanedContainers: %v", err)
	}

	if !removed["backup-id"] {
		t.Error("expected orphaned backup container to be removed")
	}
	if removed["unrelated-id"] {
		t.Error("expected unrelated container to be left alone")
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

// TestRecoverSingleRotation_OldContainerOrphanCleanup verifies P2-1: a
// restart-strategy "_old_<ts>" orphan left behind by a crash is detected and
// removed by recoverSingleRotation, exactly like the existing
// "_dso_backup_"/"_dso_new_" patterns.
func TestRecoverSingleRotation_OldContainerOrphanCleanup(t *testing.T) {
	stopped := map[string]bool{}
	removed := map[string]bool{}

	cli := newRecoveryTestClient(t, func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == "GET" && req.URL.Path == "/v1.41/containers/orig-id/json":
			return &http.Response{StatusCode: 200, Body: jsonBody(container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{
					Name:  "/myapp",
					State: &container.State{Running: true, Status: "running"},
				},
			})}, nil
		case req.Method == "POST" && req.URL.Path == "/v1.41/containers/old-id/stop":
			stopped["old-id"] = true
			return &http.Response{StatusCode: 204, Body: emptyBody()}, nil
		case req.Method == "DELETE" && req.URL.Path == "/v1.41/containers/old-id":
			removed["old-id"] = true
			return &http.Response{StatusCode: 204, Body: emptyBody()}, nil
		default:
			return &http.Response{StatusCode: 204, Body: emptyBody()}, nil
		}
	})

	logger := zaptest.NewLogger(t)
	st, err := NewStateTracker(t.TempDir(), logger)
	if err != nil {
		t.Fatalf("NewStateTracker: %v", err)
	}
	if err := st.StartRotation("aws", "db_pass", "orig-id", ""); err != nil {
		t.Fatalf("StartRotation: %v", err)
	}

	ar := NewAutomaticRecovery(cli, logger, st)
	rotation := &RotationState{
		ProviderName:        "aws",
		SecretName:          "db_pass",
		OriginalContainerID: "orig-id",
	}

	containersByName := map[string]string{
		"myapp_old_1700000000": "old-id",
	}

	ar.recoverSingleRotation(context.Background(), containersByName, rotation)

	if !stopped["old-id"] || !removed["old-id"] {
		t.Error("expected the restart-strategy \"_old_<ts>\" orphan to be stopped and removed")
	}
}

// TestRecoverSingleRotation_NewAndBackupPatternsStillWork is a direct
// regression guard for P2-1: the fix must not disturb the existing
// "_dso_new_"/"_dso_backup_" detection while adding "_old_" support. This
// duplicates TestRecoverSingleRotation_OrphanCleanup's assertions
// deliberately, as an explicit "did the refactor break the old behavior"
// check independent of that test's continued existence.
func TestRecoverSingleRotation_NewAndBackupPatternsStillWork(t *testing.T) {
	stopped := map[string]bool{}
	removed := map[string]bool{}

	cli := newRecoveryTestClient(t, func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == "GET" && req.URL.Path == "/v1.41/containers/orig-id/json":
			return &http.Response{StatusCode: 200, Body: jsonBody(container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{
					Name:  "/myapp",
					State: &container.State{Running: true, Status: "running"},
				},
			})}, nil
		case req.Method == "POST" && (req.URL.Path == "/v1.41/containers/backup-id/stop" || req.URL.Path == "/v1.41/containers/new-id/stop"):
			stopped[req.URL.Path] = true
			return &http.Response{StatusCode: 204, Body: emptyBody()}, nil
		case req.Method == "DELETE" && (req.URL.Path == "/v1.41/containers/backup-id" || req.URL.Path == "/v1.41/containers/new-id"):
			removed[req.URL.Path] = true
			return &http.Response{StatusCode: 204, Body: emptyBody()}, nil
		default:
			return &http.Response{StatusCode: 204, Body: emptyBody()}, nil
		}
	})

	logger := zaptest.NewLogger(t)
	st, err := NewStateTracker(t.TempDir(), logger)
	if err != nil {
		t.Fatalf("NewStateTracker: %v", err)
	}
	if err := st.StartRotation("aws", "db_pass", "orig-id", ""); err != nil {
		t.Fatalf("StartRotation: %v", err)
	}

	ar := NewAutomaticRecovery(cli, logger, st)
	rotation := &RotationState{ProviderName: "aws", SecretName: "db_pass", OriginalContainerID: "orig-id"}

	containersByName := map[string]string{
		"myapp_dso_backup_1700000000": "backup-id",
		"myapp_dso_new_1700000000":    "new-id",
	}

	ar.recoverSingleRotation(context.Background(), containersByName, rotation)

	if !stopped["/v1.41/containers/backup-id/stop"] || !removed["/v1.41/containers/backup-id"] {
		t.Error("expected the _dso_backup_ orphan to still be stopped and removed after the P2-1 fix")
	}
	if !stopped["/v1.41/containers/new-id/stop"] || !removed["/v1.41/containers/new-id"] {
		t.Error("expected the _dso_new_ orphan to still be stopped and removed after the P2-1 fix")
	}
}

// TestRecoverSingleRotation_UnrelatedContainerNamesNeverRemoved is the
// negative-case security regression for P2-1: a container whose name merely
// contains "_old_" but is not DSO's own rename-away convention must never
// be stopped or removed by recovery.
func TestRecoverSingleRotation_UnrelatedContainerNamesNeverRemoved(t *testing.T) {
	var stopOrRemoveCalled []string

	cli := newRecoveryTestClient(t, func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == "GET" && req.URL.Path == "/v1.41/containers/orig-id/json":
			return &http.Response{StatusCode: 200, Body: jsonBody(container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{
					Name:  "/myapp",
					State: &container.State{Running: true, Status: "running"},
				},
			})}, nil
		case req.Method == "POST" && strings.HasSuffix(req.URL.Path, "/stop"):
			stopOrRemoveCalled = append(stopOrRemoveCalled, req.URL.Path)
			return &http.Response{StatusCode: 204, Body: emptyBody()}, nil
		case req.Method == "DELETE":
			stopOrRemoveCalled = append(stopOrRemoveCalled, req.URL.Path)
			return &http.Response{StatusCode: 204, Body: emptyBody()}, nil
		default:
			return &http.Response{StatusCode: 204, Body: emptyBody()}, nil
		}
	})

	logger := zaptest.NewLogger(t)
	st, err := NewStateTracker(t.TempDir(), logger)
	if err != nil {
		t.Fatalf("NewStateTracker: %v", err)
	}
	if err := st.StartRotation("aws", "db_pass", "orig-id", ""); err != nil {
		t.Fatalf("StartRotation: %v", err)
	}

	ar := NewAutomaticRecovery(cli, logger, st)
	rotation := &RotationState{ProviderName: "aws", SecretName: "db_pass", OriginalContainerID: "orig-id"}

	containersByName := map[string]string{
		"my_old_database":         "unrelated-1",
		"legacy_old_service":      "unrelated-2",
		"customer_old_archive_v2": "unrelated-3",
	}

	ar.recoverSingleRotation(context.Background(), containersByName, rotation)

	if len(stopOrRemoveCalled) != 0 {
		t.Errorf("expected zero stop/remove calls against unrelated operator containers, got %v", stopOrRemoveCalled)
	}
}

// TestCleanupOrphanedContainers_RemovesOldPatternButNotUnrelated is the
// CleanupOrphanedContainers-level counterpart: verifies the broad startup
// sweep now also removes "_old_<ts>" orphans, while continuing to leave
// unrelated containers (including ones that merely contain "_old_") alone.
func TestCleanupOrphanedContainers_RemovesOldPatternButNotUnrelated(t *testing.T) {
	removed := map[string]bool{}

	cli := newRecoveryTestClient(t, func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == "GET" && req.URL.Path == "/v1.41/containers/json":
			return &http.Response{StatusCode: 200, Body: jsonBody([]container.Summary{
				{ID: "old-id", Names: []string{"/myapp_old_1700000000"}},
				{ID: "unrelated-id", Names: []string{"/my_old_database"}},
			})}, nil
		case req.Method == "POST" && req.URL.Path == "/v1.41/containers/old-id/stop":
			return &http.Response{StatusCode: 204, Body: emptyBody()}, nil
		case req.Method == "DELETE" && req.URL.Path == "/v1.41/containers/old-id":
			removed["old-id"] = true
			return &http.Response{StatusCode: 204, Body: emptyBody()}, nil
		case req.Method == "DELETE" && req.URL.Path == "/v1.41/containers/unrelated-id":
			removed["unrelated-id"] = true
			return &http.Response{StatusCode: 204, Body: emptyBody()}, nil
		default:
			return &http.Response{StatusCode: 204, Body: emptyBody()}, nil
		}
	})

	logger := zaptest.NewLogger(t)
	ar := NewAutomaticRecovery(cli, logger, nil)

	if err := ar.CleanupOrphanedContainers(context.Background()); err != nil {
		t.Fatalf("CleanupOrphanedContainers: %v", err)
	}

	if !removed["old-id"] {
		t.Error("expected the _old_<ts> orphan to be removed")
	}
	if removed["unrelated-id"] {
		t.Error("expected an unrelated container merely containing \"_old_\" to be left alone")
	}
}
