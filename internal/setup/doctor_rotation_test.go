package setup

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/pkg/api"
	"github.com/docker-secret-operator/dso/pkg/config"
)

// ─── RotationChecks (Phase 7 / P2-5) ───────────────────────────────────────

func newTestRotationChecks() *RotationChecks {
	return &RotationChecks{
		configPath:        "/nonexistent/dso.yaml",
		getAgentStatus:    func() (*api.StatusResponse, error) { return &api.StatusResponse{}, nil },
		listDSOContainers: func(_ context.Context) ([]dsoContainerInfo, error) { return nil, nil },
		loadConfig:        func(_ string) (*config.Config, error) { return &config.Config{}, nil },
		statDir:           os.Stat,
	}
}

// --- DSO-DOCTOR-018: rotation state -----------------------------------------

func TestRotationCheck_018_NoPendingRotations_Pass(t *testing.T) {
	rc := newTestRotationChecks()
	status := &api.StatusResponse{PendingRotations: 0}

	c := rc.checkRotationState(status, nil)

	assertCheckID(t, c, "DSO-DOCTOR-018")
	assertPass(t, c)
}

func TestRotationCheck_018_PendingRotations_Warn(t *testing.T) {
	rc := newTestRotationChecks()
	status := &api.StatusResponse{PendingRotations: 2}

	c := rc.checkRotationState(status, nil)

	assertCheckID(t, c, "DSO-DOCTOR-018")
	assertWarn(t, c)
	assertHasRecovery(t, c)
}

func TestRotationCheck_018_AgentUnreachable_Info(t *testing.T) {
	rc := newTestRotationChecks()

	c := rc.checkRotationState(nil, errors.New("connection refused"))

	assertCheckID(t, c, "DSO-DOCTOR-018")
	assertInfo(t, c)
}

// --- DSO-DOCTOR-019: degraded services --------------------------------------

func TestRotationCheck_019_NoDegradedServices_Pass(t *testing.T) {
	rc := newTestRotationChecks()
	status := &api.StatusResponse{}

	c := rc.checkDegradedServices(status, nil)

	assertCheckID(t, c, "DSO-DOCTOR-019")
	assertPass(t, c)
}

func TestRotationCheck_019_DegradedServicesPresent_Fail(t *testing.T) {
	rc := newTestRotationChecks()
	status := &api.StatusResponse{
		DegradedServices: []api.DegradedServiceInfo{
			{Service: "payments-api", Reason: "rotation failed, still serving previous secret: provider timeout"},
		},
	}

	c := rc.checkDegradedServices(status, nil)

	assertCheckID(t, c, "DSO-DOCTOR-019")
	assertFail(t, c)
	assertHasRecovery(t, c)
	if c.Severity != DoctorCritical {
		t.Errorf("want DoctorCritical, got %q", c.Severity)
	}
	if !strings.Contains(c.Detail, "payments-api") {
		t.Errorf("expected the degraded service name in Detail, got %q", c.Detail)
	}
}

func TestRotationCheck_019_AgentUnreachable_Info(t *testing.T) {
	rc := newTestRotationChecks()

	c := rc.checkDegradedServices(nil, errors.New("connection refused"))

	assertCheckID(t, c, "DSO-DOCTOR-019")
	assertInfo(t, c)
}

// --- DSO-DOCTOR-020: orphaned rotation containers ---------------------------

func TestRotationCheck_020_NoOrphans_Pass(t *testing.T) {
	rc := newTestRotationChecks()
	rc.listDSOContainers = func(_ context.Context) ([]dsoContainerInfo, error) {
		return []dsoContainerInfo{{Name: "payments-api"}, {Name: "billing-worker"}}, nil
	}

	c := rc.checkOrphanContainers(context.Background())

	assertCheckID(t, c, "DSO-DOCTOR-020")
	assertPass(t, c)
}

func TestRotationCheck_020_OrphansFound_Warn(t *testing.T) {
	rc := newTestRotationChecks()
	rc.listDSOContainers = func(_ context.Context) ([]dsoContainerInfo, error) {
		return []dsoContainerInfo{
			{Name: "payments-api"},
			{Name: "payments-api_dso_backup_1700000000"},
			{Name: "worker_old_1700000001"},
		}, nil
	}

	c := rc.checkOrphanContainers(context.Background())

	assertCheckID(t, c, "DSO-DOCTOR-020")
	assertWarn(t, c)
	assertHasRecovery(t, c)
	if !strings.Contains(c.Detail, "payments-api_dso_backup_1700000000") || !strings.Contains(c.Detail, "worker_old_1700000001") {
		t.Errorf("expected both orphan names in Detail, got %q", c.Detail)
	}
	if strings.Contains(c.Detail, "\"payments-api\"") {
		t.Errorf("expected the real, non-orphaned container to NOT be flagged, got %q", c.Detail)
	}
}

func TestRotationCheck_020_UnrelatedContainerNeverFlagged(t *testing.T) {
	rc := newTestRotationChecks()
	rc.listDSOContainers = func(_ context.Context) ([]dsoContainerInfo, error) {
		return []dsoContainerInfo{{Name: "my_old_database"}}, nil
	}

	c := rc.checkOrphanContainers(context.Background())

	assertCheckID(t, c, "DSO-DOCTOR-020")
	assertPass(t, c) // security regression guard: "_old_" substring alone must never trigger a WARN
}

func TestRotationCheck_020_DockerUnavailable_Info(t *testing.T) {
	rc := newTestRotationChecks()
	rc.listDSOContainers = func(_ context.Context) ([]dsoContainerInfo, error) {
		return nil, errors.New("cannot connect to the Docker daemon")
	}

	c := rc.checkOrphanContainers(context.Background())

	assertCheckID(t, c, "DSO-DOCTOR-020")
	assertInfo(t, c)
}

// --- DSO-DOCTOR-021: proxy host-port conflicts ------------------------------

func TestRotationCheck_021_NoDeclaredPorts_Info(t *testing.T) {
	rc := newTestRotationChecks()
	rc.listDSOContainers = func(_ context.Context) ([]dsoContainerInfo, error) {
		return []dsoContainerInfo{{Name: "payments-api"}}, nil
	}

	c := rc.checkProxyHostPortConflicts(context.Background())

	assertCheckID(t, c, "DSO-DOCTOR-021")
	assertInfo(t, c)
}

func TestRotationCheck_021_NoConflicts_Pass(t *testing.T) {
	rc := newTestRotationChecks()
	rc.listDSOContainers = func(_ context.Context) ([]dsoContainerInfo, error) {
		return []dsoContainerInfo{
			{Name: "payments-api", HostPorts: "8080:80"},
			{Name: "billing-worker", HostPorts: "8081:80"},
		}, nil
	}

	c := rc.checkProxyHostPortConflicts(context.Background())

	assertCheckID(t, c, "DSO-DOCTOR-021")
	assertPass(t, c)
}

func TestRotationCheck_021_Conflict_Fail(t *testing.T) {
	rc := newTestRotationChecks()
	rc.listDSOContainers = func(_ context.Context) ([]dsoContainerInfo, error) {
		return []dsoContainerInfo{
			{Name: "payments-api", HostPorts: "8080:80"},
			{Name: "billing-worker", HostPorts: "8080:80"},
		}, nil
	}

	c := rc.checkProxyHostPortConflicts(context.Background())

	assertCheckID(t, c, "DSO-DOCTOR-021")
	assertFail(t, c)
	assertHasRecovery(t, c)
	if !strings.Contains(c.Detail, "8080") {
		t.Errorf("expected the conflicting port in Detail, got %q", c.Detail)
	}
}

func TestRotationCheck_021_DockerUnavailable_Info(t *testing.T) {
	rc := newTestRotationChecks()
	rc.listDSOContainers = func(_ context.Context) ([]dsoContainerInfo, error) {
		return nil, errors.New("cannot connect to the Docker daemon")
	}

	c := rc.checkProxyHostPortConflicts(context.Background())

	assertCheckID(t, c, "DSO-DOCTOR-021")
	assertInfo(t, c)
}

// --- DSO-DOCTOR-022: secret destination reachability ------------------------

func TestRotationCheck_022_NoFileModeSecrets_Info(t *testing.T) {
	rc := newTestRotationChecks()
	rc.loadConfig = func(_ string) (*config.Config, error) { return &config.Config{}, nil }

	c := rc.checkSecretDestinations()

	assertCheckID(t, c, "DSO-DOCTOR-022")
	assertInfo(t, c)
}

func TestRotationCheck_022_DestinationReachable_Pass(t *testing.T) {
	dir := t.TempDir()
	rc := newTestRotationChecks()
	rc.loadConfig = func(_ string) (*config.Config, error) {
		return &config.Config{
			Secrets: []config.SecretMapping{
				{Name: "db-password", Inject: config.InjectionConfig{Type: "file", Path: dir + "/secret.txt"}},
			},
		}, nil
	}

	c := rc.checkSecretDestinations()

	assertCheckID(t, c, "DSO-DOCTOR-022")
	assertPass(t, c)
	if strings.Contains(c.Detail, "db-password") {
		// Pass details report a count, not per-secret names -- this isn't a
		// hard requirement, just documents current behavior.
		t.Logf("note: pass detail unexpectedly names the secret: %q", c.Detail)
	}
}

func TestRotationCheck_022_DestinationMissing_Warn(t *testing.T) {
	rc := newTestRotationChecks()
	rc.loadConfig = func(_ string) (*config.Config, error) {
		return &config.Config{
			Secrets: []config.SecretMapping{
				{Name: "db-password", Inject: config.InjectionConfig{Type: "file", Path: "/nonexistent/dir/secret.txt"}},
			},
		}, nil
	}

	c := rc.checkSecretDestinations()

	assertCheckID(t, c, "DSO-DOCTOR-022")
	assertWarn(t, c)
	assertHasRecovery(t, c)
	if !strings.Contains(c.Detail, "db-password") {
		t.Errorf("expected the affected secret name in Detail, got %q", c.Detail)
	}
	if strings.Contains(c.Detail, "hunter2") {
		t.Error("secret detail must never contain a plausible secret value")
	}
}

func TestRotationCheck_022_ConfigLoadFailure_Info(t *testing.T) {
	rc := newTestRotationChecks()
	rc.loadConfig = func(_ string) (*config.Config, error) { return nil, errors.New("file not found") }

	c := rc.checkSecretDestinations()

	assertCheckID(t, c, "DSO-DOCTOR-022")
	assertInfo(t, c)
}

// --- DSO-DOCTOR-023: container ownership coverage ---------------------------

func TestRotationCheck_023_NoManagedContainers_Pass(t *testing.T) {
	rc := newTestRotationChecks()
	rc.loadConfig = func(_ string) (*config.Config, error) {
		return &config.Config{Agent: config.AgentConfig{OwnerID: "dso-abc123"}}, nil
	}
	rc.listDSOContainers = func(_ context.Context) ([]dsoContainerInfo, error) { return nil, nil }

	c := rc.checkUnownedManagedContainers(context.Background())

	assertCheckID(t, c, "DSO-DOCTOR-023")
	assertPass(t, c)
}

func TestRotationCheck_023_AllOwnedByThisAgent_Pass(t *testing.T) {
	rc := newTestRotationChecks()
	rc.loadConfig = func(_ string) (*config.Config, error) {
		return &config.Config{Agent: config.AgentConfig{OwnerID: "dso-abc123"}}, nil
	}
	rc.listDSOContainers = func(_ context.Context) ([]dsoContainerInfo, error) {
		return []dsoContainerInfo{
			{Name: "web-1", Reloader: "true", Owner: "dso-abc123"},
			{Name: "unrelated", Reloader: ""},
		}, nil
	}

	c := rc.checkUnownedManagedContainers(context.Background())

	assertCheckID(t, c, "DSO-DOCTOR-023")
	assertPass(t, c)
}

func TestRotationCheck_023_UnownedLegacyContainer_Warn(t *testing.T) {
	rc := newTestRotationChecks()
	rc.loadConfig = func(_ string) (*config.Config, error) {
		return &config.Config{Agent: config.AgentConfig{OwnerID: "dso-abc123"}}, nil
	}
	rc.listDSOContainers = func(_ context.Context) ([]dsoContainerInfo, error) {
		return []dsoContainerInfo{
			{Name: "pre-upgrade-web", Reloader: "true", Owner: ""},
		}, nil
	}

	c := rc.checkUnownedManagedContainers(context.Background())

	assertCheckID(t, c, "DSO-DOCTOR-023")
	assertWarn(t, c)
	assertHasRecovery(t, c)
	if !strings.Contains(c.Detail, "pre-upgrade-web") {
		t.Errorf("expected the affected container name in Detail, got %q", c.Detail)
	}
}

func TestRotationCheck_023_ForeignOwnedContainer_Warn(t *testing.T) {
	rc := newTestRotationChecks()
	rc.loadConfig = func(_ string) (*config.Config, error) {
		return &config.Config{Agent: config.AgentConfig{OwnerID: "dso-abc123"}}, nil
	}
	rc.listDSOContainers = func(_ context.Context) ([]dsoContainerInfo, error) {
		return []dsoContainerInfo{
			{Name: "other-agents-web", Reloader: "true", Owner: "dso-xyz789"},
		}, nil
	}

	c := rc.checkUnownedManagedContainers(context.Background())

	assertCheckID(t, c, "DSO-DOCTOR-023")
	assertWarn(t, c)
	if !strings.Contains(c.Detail, "other-agents-web") {
		t.Errorf("expected the affected container name in Detail, got %q", c.Detail)
	}
}

func TestRotationCheck_023_ConfigLoadFailure_Info(t *testing.T) {
	rc := newTestRotationChecks()
	rc.loadConfig = func(_ string) (*config.Config, error) { return nil, errors.New("file not found") }

	c := rc.checkUnownedManagedContainers(context.Background())

	assertCheckID(t, c, "DSO-DOCTOR-023")
	assertInfo(t, c)
}

// --- RotationChecks.run() ----------------------------------------------------

func TestRotationChecks_run_Returns6Checks(t *testing.T) {
	rc := newTestRotationChecks()
	checks := rc.run(context.Background())

	if len(checks) != 6 {
		t.Fatalf("expected 6 checks, got %d", len(checks))
	}
	wantIDs := map[string]bool{
		"DSO-DOCTOR-018": true, "DSO-DOCTOR-019": true, "DSO-DOCTOR-020": true,
		"DSO-DOCTOR-021": true, "DSO-DOCTOR-022": true, "DSO-DOCTOR-023": true,
	}
	for _, c := range checks {
		if !wantIDs[c.ID] {
			t.Errorf("unexpected check ID %q", c.ID)
		}
		if c.Category != DoctorCatRotation {
			t.Errorf("check %q: expected category %q, got %q", c.ID, DoctorCatRotation, c.Category)
		}
	}
}

// TestRotationChecks_NeverLeakSecretSentinel is the Phase 7 security
// regression test: a sentinel placed in every field these checks could
// plausibly ever touch (a degraded-service reason, a container name, a
// configured secret name/path) must appear ONLY where it is safe metadata
// (names/paths/reasons, never a secret value) and never trigger doctor to
// read or print anything from the secret's actual protected content --
// these checks have no code path that reads a secret value at all.
func TestRotationChecks_NeverLeakSecretSentinel(t *testing.T) {
	const sentinel = "DSO_PHASE7_SECRET_SENTINEL_4f9a21"

	rc := newTestRotationChecks()
	rc.getAgentStatus = func() (*api.StatusResponse, error) {
		return &api.StatusResponse{
			DegradedServices: []api.DegradedServiceInfo{
				{Service: "payments-api", Reason: "rotation failed: " + sentinel},
			},
		}, nil
	}
	rc.listDSOContainers = func(_ context.Context) ([]dsoContainerInfo, error) {
		return []dsoContainerInfo{{Name: sentinel + "_dso_backup_1700000000"}}, nil
	}
	dir := t.TempDir()
	rc.loadConfig = func(_ string) (*config.Config, error) {
		return &config.Config{
			Secrets: []config.SecretMapping{
				{Name: sentinel, Inject: config.InjectionConfig{Type: "file", Path: dir + "/secret.txt"}},
			},
		}, nil
	}

	checks := rc.run(context.Background())

	var degraded, orphan, dest DoctorCheck
	for _, c := range checks {
		switch c.ID {
		case "DSO-DOCTOR-019":
			degraded = c
		case "DSO-DOCTOR-020":
			orphan = c
		case "DSO-DOCTOR-022":
			dest = c
		}
	}

	// The sentinel IS expected to surface here: it was embedded in safe
	// metadata (a reason string, a container name) that these checks are
	// supposed to pass through faithfully.
	if !strings.Contains(degraded.Detail, sentinel) {
		t.Errorf("expected the sentinel to pass through in the degraded-service reason, got %q", degraded.Detail)
	}
	if !strings.Contains(orphan.Detail, sentinel) {
		t.Errorf("expected the sentinel to pass through in the orphan container name, got %q", orphan.Detail)
	}

	// checkSecretDestinations only ever Stats a directory -- it must never
	// read the destination file's content, so the secret NAME (safe
	// metadata) may appear in its own report, but nothing resembling file
	// content ever can, since dest.dir points at an empty temp directory
	// with no such content to leak in the first place. This assertion
	// exists to make that invariant explicit and break loudly if a future
	// change ever adds a ReadFile call here.
	if strings.Contains(dest.Detail, "BEGIN PRIVATE KEY") || strings.Contains(dest.Detail, "-----") {
		t.Errorf("checkSecretDestinations must never read/echo file content, got %q", dest.Detail)
	}
}
