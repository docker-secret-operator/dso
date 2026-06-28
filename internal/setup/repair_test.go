package setup

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

// ─── RepairPermissions ────────────────────────────────────────────────────────

func TestRepairPerms_Plan_004_Moderate_RequiresConfirm(t *testing.T) {
	rp := newRepairPermissions("/var/run/docker.sock", "/etc/dso/dso.yaml")
	a := rp.planForCheck(failingDoctorCheck("DSO-DOCTOR-004"))

	if a == nil {
		t.Fatal("expected non-nil action for DSO-DOCTOR-004")
	}
	if a.ID != "REPAIR-PERM-001" {
		t.Errorf("ID: want REPAIR-PERM-001, got %s", a.ID)
	}
	if a.RiskLevel != RepairRiskModerate {
		t.Errorf("risk: want Moderate, got %s", a.RiskLevel)
	}
	if !a.RequiresConfirmation {
		t.Error("DSO-DOCTOR-004 repair must require confirmation")
	}
}

func TestRepairPerms_Plan_005_Safe_NoConfirm(t *testing.T) {
	rp := newRepairPermissions("/var/run/docker.sock", "/etc/dso/dso.yaml")
	a := rp.planForCheck(failingDoctorCheck("DSO-DOCTOR-005"))

	if a == nil {
		t.Fatal("expected non-nil action for DSO-DOCTOR-005")
	}
	if a.RiskLevel != RepairRiskSafe {
		t.Errorf("risk: want Safe, got %s", a.RiskLevel)
	}
	if a.RequiresConfirmation {
		t.Error("DSO-DOCTOR-005 is Safe — must not require confirmation")
	}
}

func TestRepairPerms_Plan_009_Safe_NoConfirm(t *testing.T) {
	rp := newRepairPermissions("/var/run/docker.sock", "/etc/dso/dso.yaml")
	a := rp.planForCheck(warnDoctorCheck("DSO-DOCTOR-009"))

	if a == nil {
		t.Fatal("expected non-nil action for DSO-DOCTOR-009")
	}
	if a.RiskLevel != RepairRiskSafe {
		t.Errorf("risk: want Safe, got %s", a.RiskLevel)
	}
}

func TestRepairPerms_SocketPerms_CallsChmod660(t *testing.T) {
	var called string
	var calledMode os.FileMode
	rp := newRepairPermissions("/var/run/docker.sock", "/etc/dso/dso.yaml")
	rp.chmod = func(path string, mode os.FileMode) error {
		called = path
		calledMode = mode
		return nil
	}
	if err := rp.repairSocketPerms(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called != "/var/run/docker.sock" {
		t.Errorf("chmod path: want /var/run/docker.sock, got %s", called)
	}
	if calledMode != 0660 {
		t.Errorf("chmod mode: want 0660, got %04o", calledMode)
	}
}

func TestRepairPerms_SocketPerms_PropagatesError(t *testing.T) {
	rp := newRepairPermissions("/var/run/docker.sock", "/etc/dso/dso.yaml")
	rp.chmod = func(_ string, _ os.FileMode) error { return errors.New("permission denied") }
	err := rp.repairSocketPerms()
	if err == nil {
		t.Error("expected error when chmod fails")
	}
}

func TestRepairPerms_ConfigPerms_CallsChmod600(t *testing.T) {
	var calledMode os.FileMode
	rp := newRepairPermissions("/var/run/docker.sock", "/etc/dso/dso.yaml")
	rp.chmod = func(_ string, mode os.FileMode) error {
		calledMode = mode
		return nil
	}
	if err := rp.repairConfigPerms(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calledMode != 0600 {
		t.Errorf("chmod mode: want 0600, got %04o", calledMode)
	}
}

func TestRepairPerms_ConfigPerms_PropagatesError(t *testing.T) {
	rp := newRepairPermissions("/var/run/docker.sock", "/etc/dso/dso.yaml")
	rp.chmod = func(_ string, _ os.FileMode) error { return errors.New("read-only filesystem") }
	if err := rp.repairConfigPerms(); err == nil {
		t.Error("expected error when chmod fails")
	}
}

// ─── RepairConfiguration ──────────────────────────────────────────────────────

func TestRepairConfig_Plan_007_Destructive_RequiresConfirm(t *testing.T) {
	rc := newRepairConfiguration("/etc/dso/dso.yaml", "aws")
	a := rc.planForCheck(failingDoctorCheck("DSO-DOCTOR-007"))

	if a == nil {
		t.Fatal("expected non-nil action for DSO-DOCTOR-007")
	}
	if a.ID != "REPAIR-CFG-001" {
		t.Errorf("ID: want REPAIR-CFG-001, got %s", a.ID)
	}
	if a.RiskLevel != RepairRiskDestructive {
		t.Errorf("risk: want Destructive, got %s", a.RiskLevel)
	}
	if !a.RequiresConfirmation {
		t.Error("DSO-DOCTOR-007 repair must require confirmation")
	}
}

func TestRepairConfig_Plan_008_Destructive_RequiresConfirm(t *testing.T) {
	rc := newRepairConfiguration("/etc/dso/dso.yaml", "aws")
	a := rc.planForCheck(failingDoctorCheck("DSO-DOCTOR-008"))

	if a == nil {
		t.Fatal("expected non-nil action for DSO-DOCTOR-008")
	}
	if a.ID != "REPAIR-CFG-002" {
		t.Errorf("ID: want REPAIR-CFG-002, got %s", a.ID)
	}
	if a.RiskLevel != RepairRiskDestructive {
		t.Errorf("risk: want Destructive, got %s", a.RiskLevel)
	}
}

func TestRepairConfig_WriteConfig_WritesFileWithProvider(t *testing.T) {
	var writtenContent []byte
	rc := newRepairConfiguration("/etc/dso/dso.yaml", "aws")
	rc.mkdir = func(_ string, _ os.FileMode) error { return nil }
	rc.writeFile = func(_ string, content []byte, _ os.FileMode) error {
		writtenContent = content
		return nil
	}
	if err := rc.createDefaultConfig(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(writtenContent), "provider: aws") {
		t.Errorf("expected 'provider: aws' in written content, got:\n%s", writtenContent)
	}
}

func TestRepairConfig_WriteConfig_DefaultsProviderToLocal(t *testing.T) {
	var writtenContent []byte
	rc := newRepairConfiguration("/etc/dso/dso.yaml", "")
	rc.mkdir = func(_ string, _ os.FileMode) error { return nil }
	rc.writeFile = func(_ string, content []byte, _ os.FileMode) error {
		writtenContent = content
		return nil
	}
	if err := rc.createDefaultConfig(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(writtenContent), "provider: local") {
		t.Errorf("expected 'provider: local' in written content, got:\n%s", writtenContent)
	}
}

func TestRepairConfig_WriteConfig_CreatesDirFirst(t *testing.T) {
	mkdirCalled := false
	rc := newRepairConfiguration("/etc/dso/dso.yaml", "local")
	rc.mkdir = func(_ string, _ os.FileMode) error {
		mkdirCalled = true
		return nil
	}
	rc.writeFile = func(_ string, _ []byte, _ os.FileMode) error { return nil }
	if err := rc.writeConfig(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mkdirCalled {
		t.Error("expected mkdir to be called before writing config")
	}
}

func TestRepairConfig_WriteConfig_PropagatesWriteError(t *testing.T) {
	rc := newRepairConfiguration("/etc/dso/dso.yaml", "local")
	rc.mkdir = func(_ string, _ os.FileMode) error { return nil }
	rc.writeFile = func(_ string, _ []byte, _ os.FileMode) error {
		return errors.New("disk full")
	}
	if err := rc.writeConfig(); err == nil {
		t.Error("expected error when writeFile fails")
	}
}

func TestRepairConfig_RecreateEmpty_SameAsCreate(t *testing.T) {
	var called bool
	rc := newRepairConfiguration("/etc/dso/dso.yaml", "vault")
	rc.mkdir = func(_ string, _ os.FileMode) error { return nil }
	rc.writeFile = func(_ string, _ []byte, _ os.FileMode) error {
		called = true
		return nil
	}
	if err := rc.recreateEmptyConfig(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected writeFile to be called")
	}
}

// ─── RepairRuntime ────────────────────────────────────────────────────────────

func TestRepairRuntime_Plan_012_Safe_NoConfirm(t *testing.T) {
	rr := newRepairRuntime("/var/run/dso")
	a := rr.planForCheck(failingDoctorCheck("DSO-DOCTOR-012"))

	if a == nil {
		t.Fatal("expected non-nil action for DSO-DOCTOR-012")
	}
	if a.ID != "REPAIR-RUNTIME-001" {
		t.Errorf("ID: want REPAIR-RUNTIME-001, got %s", a.ID)
	}
	if a.RiskLevel != RepairRiskSafe {
		t.Errorf("risk: want Safe, got %s", a.RiskLevel)
	}
	if a.RequiresConfirmation {
		t.Error("DSO-DOCTOR-012 repair is Safe — must not require confirmation")
	}
}

func TestRepairRuntime_Plan_013_Moderate_RequiresConfirm(t *testing.T) {
	rr := newRepairRuntime("/var/run/dso")
	a := rr.planForCheck(warnDoctorCheck("DSO-DOCTOR-013"))

	if a == nil {
		t.Fatal("expected non-nil action for DSO-DOCTOR-013")
	}
	if a.RiskLevel != RepairRiskModerate {
		t.Errorf("risk: want Moderate, got %s", a.RiskLevel)
	}
	if !a.RequiresConfirmation {
		t.Error("DSO-DOCTOR-013 repair must require confirmation")
	}
}

func TestRepairRuntime_CreateDir_CallsMkdir(t *testing.T) {
	var mkdirPath string
	rr := newRepairRuntime("/var/run/dso")
	rr.mkdir = func(path string, _ os.FileMode) error {
		mkdirPath = path
		return nil
	}
	if err := rr.createRuntimeDir(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mkdirPath != "/var/run/dso" {
		t.Errorf("mkdir path: want /var/run/dso, got %s", mkdirPath)
	}
}

func TestRepairRuntime_CreateDir_PropagatesError(t *testing.T) {
	rr := newRepairRuntime("/var/run/dso")
	rr.mkdir = func(_ string, _ os.FileMode) error { return errors.New("permission denied") }
	if err := rr.createRuntimeDir(); err == nil {
		t.Error("expected error when mkdir fails")
	}
}

func TestRepairRuntime_RemoveLocks_NoLocks_NoError(t *testing.T) {
	rr := newRepairRuntime("/var/run/dso")
	rr.glob = func(_ string) ([]string, error) { return nil, nil }
	if err := rr.removeStaleLocks(); err != nil {
		t.Errorf("expected no error for empty lock list, got: %v", err)
	}
}

func TestRepairRuntime_RemoveLocks_RemovesAllLocks(t *testing.T) {
	removed := make(map[string]bool)
	rr := newRepairRuntime("/var/run/dso")
	rr.glob = func(_ string) ([]string, error) {
		return []string{"/var/run/dso/a.lock", "/var/run/dso/b.lock"}, nil
	}
	rr.removeFile = func(path string) error {
		removed[path] = true
		return nil
	}
	if err := rr.removeStaleLocks(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed["/var/run/dso/a.lock"] || !removed["/var/run/dso/b.lock"] {
		t.Error("expected both lock files to be removed")
	}
}

func TestRepairRuntime_RemoveLocks_ToleratesErrNotExist(t *testing.T) {
	rr := newRepairRuntime("/var/run/dso")
	rr.glob = func(_ string) ([]string, error) {
		return []string{"/var/run/dso/gone.lock"}, nil
	}
	rr.removeFile = func(_ string) error { return os.ErrNotExist }
	if err := rr.removeStaleLocks(); err != nil {
		t.Errorf("ErrNotExist must be tolerated, got: %v", err)
	}
}

func TestRepairRuntime_RemoveLocks_PropagatesRealError(t *testing.T) {
	rr := newRepairRuntime("/var/run/dso")
	rr.glob = func(_ string) ([]string, error) {
		return []string{"/var/run/dso/stuck.lock"}, nil
	}
	rr.removeFile = func(_ string) error { return errors.New("device busy") }
	if err := rr.removeStaleLocks(); err == nil {
		t.Error("expected error when remove fails with non-NotExist error")
	}
}

// ─── RepairService ────────────────────────────────────────────────────────────

func TestRepairService_Plan_015_Moderate_RequiresConfirm(t *testing.T) {
	rs := newRepairService()
	a := rs.planForCheck(failingDoctorCheck("DSO-DOCTOR-015"))

	if a == nil {
		t.Fatal("expected non-nil action for DSO-DOCTOR-015")
	}
	if a.ID != "REPAIR-SVC-001" {
		t.Errorf("ID: want REPAIR-SVC-001, got %s", a.ID)
	}
	if a.RiskLevel != RepairRiskModerate {
		t.Errorf("risk: want Moderate, got %s", a.RiskLevel)
	}
	if !a.RequiresConfirmation {
		t.Error("DSO-DOCTOR-015 repair must require confirmation")
	}
}

func TestRepairService_Plan_016_Moderate_RequiresConfirm(t *testing.T) {
	rs := newRepairService()
	a := rs.planForCheck(warnDoctorCheck("DSO-DOCTOR-016"))
	if a == nil {
		t.Fatal("expected non-nil action for DSO-DOCTOR-016")
	}
	if a.ID != "REPAIR-SVC-002" {
		t.Errorf("ID: want REPAIR-SVC-002, got %s", a.ID)
	}
}

func TestRepairService_Plan_017_Moderate_RequiresConfirm(t *testing.T) {
	rs := newRepairService()
	a := rs.planForCheck(failingDoctorCheck("DSO-DOCTOR-017"))
	if a == nil {
		t.Fatal("expected non-nil action for DSO-DOCTOR-017")
	}
	if a.ID != "REPAIR-SVC-003" {
		t.Errorf("ID: want REPAIR-SVC-003, got %s", a.ID)
	}
}

func TestRepairService_WriteUnitFile_WritesContent(t *testing.T) {
	var written []byte
	rs := newRepairService()
	rs.writeFile = func(_ string, content []byte, _ os.FileMode) error {
		written = content
		return nil
	}
	rs.daemonReload = func(_ context.Context) error { return nil }

	if err := rs.writeUnitFile(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(written), "[Unit]") {
		t.Error("expected [Unit] section in written unit file")
	}
	if !strings.Contains(string(written), "dso-agent") {
		t.Error("expected dso-agent in unit file content")
	}
}

func TestRepairService_WriteUnitFile_CallsDaemonReload(t *testing.T) {
	reloadCalled := false
	rs := newRepairService()
	rs.writeFile = func(_ string, _ []byte, _ os.FileMode) error { return nil }
	rs.daemonReload = func(_ context.Context) error {
		reloadCalled = true
		return nil
	}
	if err := rs.writeUnitFile(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reloadCalled {
		t.Error("expected daemonReload to be called after writing unit file")
	}
}

func TestRepairService_WriteUnitFile_WriteError_NoDaemonReload(t *testing.T) {
	reloadCalled := false
	rs := newRepairService()
	rs.writeFile = func(_ string, _ []byte, _ os.FileMode) error {
		return errors.New("permission denied")
	}
	rs.daemonReload = func(_ context.Context) error {
		reloadCalled = true
		return nil
	}
	err := rs.writeUnitFile()
	if err == nil {
		t.Error("expected error when writeFile fails")
	}
	if reloadCalled {
		t.Error("daemonReload must not be called when writeFile fails")
	}
}

func TestRepairService_WriteUnitFile_DaemonReloadError(t *testing.T) {
	rs := newRepairService()
	rs.writeFile = func(_ string, _ []byte, _ os.FileMode) error { return nil }
	rs.daemonReload = func(_ context.Context) error { return errors.New("systemd not running") }
	if err := rs.writeUnitFile(); err == nil {
		t.Error("expected error when daemonReload fails")
	}
}

func TestRepairService_EnableService_Success(t *testing.T) {
	var enabledName string
	rs := newRepairService()
	rs.enable = func(_ context.Context, name string) error {
		enabledName = name
		return nil
	}
	if err := rs.enableService(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enabledName != "dso-agent.service" {
		t.Errorf("enable called with %q, want dso-agent.service", enabledName)
	}
}

func TestRepairService_EnableService_PropagatesError(t *testing.T) {
	rs := newRepairService()
	rs.enable = func(_ context.Context, _ string) error { return errors.New("systemctl not found") }
	if err := rs.enableService(context.Background()); err == nil {
		t.Error("expected error when enable fails")
	}
}

func TestRepairService_StartService_Success(t *testing.T) {
	var startedName string
	rs := newRepairService()
	rs.start = func(_ context.Context, name string) error {
		startedName = name
		return nil
	}
	if err := rs.startService(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if startedName != "dso-agent.service" {
		t.Errorf("start called with %q, want dso-agent.service", startedName)
	}
}

func TestRepairService_StartService_PropagatesError(t *testing.T) {
	rs := newRepairService()
	rs.start = func(_ context.Context, _ string) error { return errors.New("failed to start") }
	if err := rs.startService(context.Background()); err == nil {
		t.Error("expected error when start fails")
	}
}

// ─── RepairProvider ───────────────────────────────────────────────────────────

func TestRepairProvider_PlanForCheck_AlwaysNil(t *testing.T) {
	rp := newRepairProvider("aws")
	for _, id := range []string{"DSO-DOCTOR-010", "DSO-DOCTOR-011"} {
		if a := rp.planForCheck(failingDoctorCheck(id)); a != nil {
			t.Errorf("expected nil action for %s (provider repairs are not automated), got: %+v", id, a)
		}
	}
}

// ─── Repair engine (repair.go) ────────────────────────────────────────────────

func TestRepair_Plan_NoActionsForPassingChecks(t *testing.T) {
	r := testRepair()
	result := &DoctorResult{
		Checks: []DoctorCheck{
			{ID: "DSO-DOCTOR-005", Status: DoctorPass},
			{ID: "DSO-DOCTOR-009", Status: DoctorPass},
		},
	}
	plan := r.Plan(context.Background(), result)
	if len(plan.Issues) != 0 {
		t.Errorf("expected no actions for all-pass result, got %d", len(plan.Issues))
	}
}

func TestRepair_Plan_ActionsForFailingChecks(t *testing.T) {
	r := testRepair()
	result := &DoctorResult{
		Checks: []DoctorCheck{
			{ID: "DSO-DOCTOR-005", Status: DoctorFail},
			{ID: "DSO-DOCTOR-009", Status: DoctorWarn},
			{ID: "DSO-DOCTOR-012", Status: DoctorFail},
		},
	}
	plan := r.Plan(context.Background(), result)
	if len(plan.Issues) != 3 {
		t.Errorf("expected 3 actions, got %d", len(plan.Issues))
	}
}

func TestRepair_Plan_NoActionForDockerInfraChecks(t *testing.T) {
	r := testRepair()
	for _, id := range []string{"DSO-DOCTOR-001", "DSO-DOCTOR-002", "DSO-DOCTOR-003", "DSO-DOCTOR-014"} {
		result := &DoctorResult{
			Checks: []DoctorCheck{{ID: id, Status: DoctorFail}},
		}
		plan := r.Plan(context.Background(), result)
		if len(plan.Issues) != 0 {
			t.Errorf("%s: expected no auto-repair action, got %d", id, len(plan.Issues))
		}
	}
}

func TestRepair_Plan_NoActionForCredentialCheck(t *testing.T) {
	r := testRepair()
	result := &DoctorResult{
		Checks: []DoctorCheck{{ID: "DSO-DOCTOR-011", Status: DoctorFail}},
	}
	plan := r.Plan(context.Background(), result)
	if len(plan.Issues) != 0 {
		t.Errorf("expected no action for credential check, got %d", len(plan.Issues))
	}
}

func TestRepair_Execute_AlwaysConfirm_AllApplied(t *testing.T) {
	r := testRepair()
	plan := &RepairPlan{
		Issues: []RepairAction{
			{ID: "REPAIR-PERM-002", IssueID: "DSO-DOCTOR-005", RiskLevel: RepairRiskSafe, Status: RepairStatusPending},
			{ID: "REPAIR-RUNTIME-002", IssueID: "DSO-DOCTOR-013", RiskLevel: RepairRiskModerate, RequiresConfirmation: true, Status: RepairStatusPending},
		},
	}
	result := r.Execute(context.Background(), plan, AlwaysConfirm)

	if len(result.Applied) != 2 {
		t.Errorf("expected 2 applied, got %d", len(result.Applied))
	}
	if len(result.Declined) != 0 {
		t.Errorf("expected 0 declined, got %d", len(result.Declined))
	}
}

func TestRepair_Execute_NeverConfirm_ModerateDeclined_SafeApplied(t *testing.T) {
	r := testRepair()
	plan := &RepairPlan{
		Issues: []RepairAction{
			{ID: "REPAIR-PERM-002", IssueID: "DSO-DOCTOR-005", RiskLevel: RepairRiskSafe, RequiresConfirmation: false, Status: RepairStatusPending},
			{ID: "REPAIR-RUNTIME-002", IssueID: "DSO-DOCTOR-013", RiskLevel: RepairRiskModerate, RequiresConfirmation: true, Status: RepairStatusPending},
		},
	}
	result := r.Execute(context.Background(), plan, NeverConfirm)

	// Safe action (no confirmation needed) must still be applied.
	if len(result.Applied) != 1 || result.Applied[0] != "DSO-DOCTOR-005" {
		t.Errorf("expected DSO-DOCTOR-005 applied, got applied=%v", result.Applied)
	}
	// Moderate action declined.
	if len(result.Declined) != 1 || result.Declined[0] != "DSO-DOCTOR-013" {
		t.Errorf("expected DSO-DOCTOR-013 declined, got declined=%v", result.Declined)
	}
}

func TestRepair_Execute_ActionError_SetsFailed(t *testing.T) {
	r := testRepair()
	r.perms.chmod = func(_ string, _ os.FileMode) error { return errors.New("operation not permitted") }

	plan := &RepairPlan{
		Issues: []RepairAction{
			{ID: "REPAIR-PERM-002", IssueID: "DSO-DOCTOR-005", RiskLevel: RepairRiskSafe, RequiresConfirmation: false, Status: RepairStatusPending},
		},
	}
	result := r.Execute(context.Background(), plan, AlwaysConfirm)

	if len(result.Failed) != 1 {
		t.Errorf("expected 1 failed action, got %d", len(result.Failed))
	}
	if len(result.Applied) != 0 {
		t.Errorf("expected 0 applied when action errors, got %d", len(result.Applied))
	}
}

func TestRepair_Execute_CallsRunDoctorForVerification(t *testing.T) {
	doctorCalled := false
	r := testRepair()
	r.runDoctor = func(_ context.Context) *DoctorResult {
		doctorCalled = true
		return &DoctorResult{OverallStatus: DoctorPass, Timestamp: testTime()}
	}
	r.Execute(context.Background(), &RepairPlan{}, AlwaysConfirm)
	if !doctorCalled {
		t.Error("expected runDoctor to be called for verification")
	}
}

func TestRepair_Execute_VerificationPopulatedInResult(t *testing.T) {
	r := testRepair()
	r.runDoctor = func(_ context.Context) *DoctorResult {
		return &DoctorResult{OverallStatus: DoctorFail, Timestamp: testTime()}
	}
	result := r.Execute(context.Background(), &RepairPlan{}, AlwaysConfirm)
	if result.Verification == nil {
		t.Fatal("expected Verification to be set in RepairResult")
	}
	if result.Verification.OverallStatus != DoctorFail {
		t.Errorf("verification status: want DoctorFail, got %s", result.Verification.OverallStatus)
	}
}

// ─── Rendering ────────────────────────────────────────────────────────────────

func TestRepairResult_RenderTerminal_ContainsDivider(t *testing.T) {
	result := emptyRepairResult()
	out := result.RenderTerminal()
	if !strings.Contains(out, "─") {
		t.Error("expected terminal output to contain divider")
	}
}

func TestRepairResult_RenderTerminal_EmptyPlan_ShowsHealthy(t *testing.T) {
	result := emptyRepairResult()
	out := result.RenderTerminal()
	if !strings.Contains(out, "healthy") {
		t.Errorf("expected 'healthy' in empty plan output, got:\n%s", out)
	}
}

func TestRepairResult_RenderTerminal_ShowsActionStatus(t *testing.T) {
	result := singleActionRepairResult(RepairStatusApplied)
	out := result.RenderTerminal()
	if !strings.Contains(out, "APPLIED") {
		t.Errorf("expected APPLIED status in output, got:\n%s", out)
	}
}

func TestRepairResult_RenderTerminal_ShowsDeclined(t *testing.T) {
	result := singleActionRepairResult(RepairStatusDeclined)
	out := result.RenderTerminal()
	if !strings.Contains(out, "DECLINED") {
		t.Errorf("expected DECLINED status in output, got:\n%s", out)
	}
}

func TestRepairResult_RenderTerminal_ShowsVerificationStatus(t *testing.T) {
	result := emptyRepairResult()
	result.Verification = &DoctorResult{OverallStatus: DoctorPass}
	out := result.RenderTerminal()
	if !strings.Contains(out, "PASS") {
		t.Errorf("expected verification status in output, got:\n%s", out)
	}
}

func TestRepairResult_RenderJSON_ValidJSON(t *testing.T) {
	result := emptyRepairResult()
	out, err := result.RenderJSON()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("expected JSON object, got: %s", out[:truncLen(out, 30)])
	}
}

func TestRepairResult_RenderJSON_ContainsTimestamp(t *testing.T) {
	result := emptyRepairResult()
	out, _ := result.RenderJSON()
	if !strings.Contains(out, `"timestamp"`) {
		t.Errorf("expected timestamp in JSON output, got:\n%s", out)
	}
}

func TestRepairResult_RenderJSON_ContainsVerificationStatus(t *testing.T) {
	result := emptyRepairResult()
	result.Verification = &DoctorResult{OverallStatus: DoctorPass}
	out, _ := result.RenderJSON()
	if !strings.Contains(out, `"verification_status"`) {
		t.Errorf("expected verification_status in JSON output, got:\n%s", out)
	}
}

func TestRepairResult_RenderJSON_SummaryPresent(t *testing.T) {
	result := emptyRepairResult()
	out, _ := result.RenderJSON()
	if !strings.Contains(out, `"summary"`) {
		t.Errorf("expected summary in JSON output, got:\n%s", out)
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// testRepair returns a Repair with all OS hooks replaced by no-ops.
func testRepair() *Repair {
	opts := RepairOptions{
		Mode:         ModeLocal,
		Provider:     "local",
		DockerSocket: "/var/run/docker.sock",
		ConfigPath:   "/etc/dso/dso.yaml",
		RuntimeDir:   "/var/run/dso",
	}
	r := NewRepair(opts)

	r.perms.chmod = func(_ string, _ os.FileMode) error { return nil }

	r.config.mkdir = func(_ string, _ os.FileMode) error { return nil }
	r.config.writeFile = noopWriteFile

	r.runtime.mkdir = func(_ string, _ os.FileMode) error { return nil }
	r.runtime.glob = func(_ string) ([]string, error) { return nil, nil }
	r.runtime.removeFile = func(_ string) error { return nil }

	r.service.writeFile = noopWriteFile
	r.service.enable = func(_ context.Context, _ string) error { return nil }
	r.service.start = func(_ context.Context, _ string) error { return nil }
	r.service.daemonReload = func(_ context.Context) error { return nil }

	r.runDoctor = func(_ context.Context) *DoctorResult {
		return &DoctorResult{
			OverallStatus: DoctorPass,
			Checks:        []DoctorCheck{},
			Timestamp:     testTime(),
		}
	}
	return r
}

func emptyRepairResult() *RepairResult {
	return &RepairResult{
		Plan:      &RepairPlan{Issues: []RepairAction{}},
		Timestamp: testTime(),
	}
}

func singleActionRepairResult(status RepairStatus) *RepairResult {
	result := emptyRepairResult()
	result.Plan.Issues = []RepairAction{
		{
			ID:          "REPAIR-PERM-002",
			IssueID:     "DSO-DOCTOR-005",
			Category:    DoctorCatPermissions,
			Description: "Restrict config file permissions",
			RiskLevel:   RepairRiskSafe,
			Status:      status,
		},
	}
	return result
}

// failingDoctorCheck creates a DoctorCheck stub with Status=DoctorFail.
func failingDoctorCheck(id string) DoctorCheck {
	return DoctorCheck{ID: id, Status: DoctorFail, Severity: DoctorHigh}
}

// warnDoctorCheck creates a DoctorCheck stub with Status=DoctorWarn.
func warnDoctorCheck(id string) DoctorCheck {
	return DoctorCheck{ID: id, Status: DoctorWarn, Severity: DoctorMedium}
}
