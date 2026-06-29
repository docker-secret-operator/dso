package setup

import (
	"context"
	"errors"
	"os"
	"testing"
)

// Integration tests exercise the full Detect → Validate → Plan → Preview →
// Apply pipeline through the Engine orchestrator. All OS interactions are
// replaced by injectable fakes so the suite runs without Docker or systemd.

// ─── Fresh installation ───────────────────────────────────────────────────────

func TestIntegration_FreshInstall_LocalMode_FullPipeline(t *testing.T) {
	eng := newTestEngine()

	result, err := eng.Setup(context.Background(), SetupOptions{
		Mode:     ModeLocal,
		Provider: "local",
		DryRun:   false,
	})

	if err != nil {
		t.Fatalf("fresh install failed: %v", err)
	}
	if result.Status != "success" {
		t.Errorf("status: want 'success', got %q", result.Status)
	}
	if result.Plan == nil {
		t.Fatal("expected plan in result")
	}
	if result.Plan.Mode != ModeLocal {
		t.Errorf("plan mode: want 'local', got %q", result.Plan.Mode)
	}
	if result.Plan.Provider != "local" {
		t.Errorf("plan provider: want 'local', got %q", result.Plan.Provider)
	}
	if result.Duration <= 0 {
		t.Error("expected positive duration")
	}
}

func TestIntegration_FreshInstall_AgentMode_FullPipeline(t *testing.T) {
	eng := newTestEngine()

	result, err := eng.Setup(context.Background(), SetupOptions{
		Mode:     ModeAgent,
		Provider: "aws",
		DryRun:   false,
	})

	if err != nil {
		t.Fatalf("agent install failed: %v", err)
	}
	if result.Status != "success" {
		t.Errorf("status: want 'success', got %q", result.Status)
	}
	if result.Plan.Mode != ModeAgent {
		t.Errorf("plan mode: want 'agent', got %q", result.Plan.Mode)
	}
}

// ─── Dry-run gate ─────────────────────────────────────────────────────────────

func TestIntegration_DryRun_ProducesNoPersistence(t *testing.T) {
	eng := newTestEngine()
	applyCount := 0
	eng.applier = &countingApplier{inner: &noopApplier{}, count: &applyCount}

	result, err := eng.Setup(context.Background(), SetupOptions{
		Mode:   ModeLocal,
		DryRun: true,
	})

	if err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if result.Status != "pending" {
		t.Errorf("dry-run status: want 'pending', got %q", result.Status)
	}
	if applyCount != 0 {
		t.Errorf("dry-run must not call Apply; called %d time(s)", applyCount)
	}
}

// ─── Apply failure → Rollback ─────────────────────────────────────────────────

func TestIntegration_ApplyFailure_TriggersRollback(t *testing.T) {
	eng := newTestEngine()
	applyErr := errors.New("disk full")
	eng.applier = &stubApplier{err: applyErr}

	result, err := eng.Setup(context.Background(), SetupOptions{
		Mode: ModeLocal,
	})

	if err == nil {
		t.Fatal("expected error from failed apply")
	}
	if !errors.Is(err, applyErr) {
		t.Errorf("expected sentinel error; got: %v", err)
	}
	if result.Status != "failed" {
		t.Errorf("status: want 'failed', got %q", result.Status)
	}
	if result.Rollback == nil {
		t.Error("RollbackResult must be attached after apply failure")
	}
}

func TestIntegration_ApplySuccess_NoRollback(t *testing.T) {
	eng := newTestEngine()

	result, err := eng.Setup(context.Background(), SetupOptions{
		Mode: ModeLocal,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Rollback != nil {
		t.Error("RollbackResult must be nil on successful apply")
	}
}

// ─── Event sequence ───────────────────────────────────────────────────────────

func TestIntegration_SuccessfulRun_EmitsCompleteEventSequence(t *testing.T) {
	eng := newTestEngine()
	events := collectEvents(eng)

	_, _ = eng.Setup(context.Background(), SetupOptions{Mode: ModeLocal})

	assertEventSequence(t, events(), fullSuccessEvents)
}

func TestIntegration_DryRun_StopsAtPreview_NoApplyEvents(t *testing.T) {
	eng := newTestEngine()
	events := collectEvents(eng)

	_, _ = eng.Setup(context.Background(), SetupOptions{DryRun: true})

	want := []EventType{
		EventSetupStarted,
		EventDetectionCompleted,
		EventValidationCompleted,
		EventPlanGenerated,
		EventPreviewGenerated,
		EventSetupCompleted,
	}
	assertEventSequence(t, events(), want)
}

func TestIntegration_FailedApply_EmitsRollbackEvents(t *testing.T) {
	eng := newTestEngine()
	eng.applier = &stubApplier{err: errors.New("boom")}
	events := collectEvents(eng)

	_, _ = eng.Setup(context.Background(), SetupOptions{})

	want := []EventType{
		EventSetupStarted,
		EventDetectionCompleted,
		EventValidationCompleted,
		EventPlanGenerated,
		EventPreviewGenerated,
		EventApplyStarted,
		EventRollbackStarted,
		EventRollbackCompleted,
		EventSetupFailed,
	}
	assertEventSequence(t, events(), want)
}

// ─── Plan content ─────────────────────────────────────────────────────────────

func TestIntegration_Plan_HasNonEmptyID(t *testing.T) {
	eng := newTestEngine()
	result, _ := eng.Setup(context.Background(), SetupOptions{
		Mode: ModeLocal, DryRun: true,
	})
	if result.Plan.ID == "" {
		t.Error("plan ID must be non-empty")
	}
}

func TestIntegration_Plan_ContainsConfigFileOperation(t *testing.T) {
	eng := newTestEngine()
	result, _ := eng.Setup(context.Background(), SetupOptions{
		Mode: ModeLocal, DryRun: true,
	})
	if len(result.Plan.Files) == 0 {
		t.Error("expected at least one file operation in plan")
	}
}

func TestIntegration_AgentPlan_ContainsServiceOperations(t *testing.T) {
	eng := newTestEngine()
	result, _ := eng.Setup(context.Background(), SetupOptions{
		Mode: ModeAgent, Provider: "local", DryRun: true,
	})
	if len(result.Plan.Services) == 0 {
		t.Error("agent plan must include service operations")
	}
}

// ─── Doctor → Repair pipeline (unit-level integration) ───────────────────────

func TestIntegration_Doctor_AllPassChecks_EmptyRepairPlan(t *testing.T) {
	r := NewRepair(RepairOptions{})
	plan := r.Plan(context.Background(), allPassDoctorResult())
	if len(plan.Issues) != 0 {
		t.Errorf("expected empty repair plan for all-pass Doctor result; got %d action(s)", len(plan.Issues))
	}
}

func TestIntegration_Doctor_FailingChecks_ProducesRepairActions(t *testing.T) {
	r := NewRepair(RepairOptions{})
	plan := r.Plan(context.Background(), integMixedDoctorResult())
	if len(plan.Issues) == 0 {
		t.Error("expected repair actions for failing Doctor checks")
	}
}

func TestIntegration_Repair_Execute_VerificationRuns(t *testing.T) {
	verificationCalled := false
	r := NewRepair(RepairOptions{})
	r.runDoctor = func(_ context.Context) *DoctorResult {
		verificationCalled = true
		return allPassDoctorResult()
	}
	r.perms.chmod = func(_ string, _ os.FileMode) error { return nil }
	r.config.mkdir = func(_ string, _ os.FileMode) error { return nil }
	r.config.writeFile = func(_ string, _ []byte, _ os.FileMode) error { return nil }
	r.runtime.mkdir = func(_ string, _ os.FileMode) error { return nil }
	r.runtime.glob = func(_ string) ([]string, error) { return nil, nil }
	r.service.writeFile = func(_ string, _ []byte, _ os.FileMode) error { return nil }
	r.service.daemonReload = func(_ context.Context) error { return nil }
	r.service.enable = func(_ context.Context, _ string) error { return nil }
	r.service.start = func(_ context.Context, _ string) error { return nil }

	plan := r.Plan(context.Background(), integMixedDoctorResult())
	repairResult := r.Execute(context.Background(), plan, AlwaysConfirm)

	if repairResult.Verification == nil {
		t.Error("verification must run after Execute")
	}
	if !verificationCalled {
		t.Error("runDoctor must be called for post-repair verification")
	}
}

// ─── Emitter panic safety ─────────────────────────────────────────────────────

func TestIntegration_Emitter_PanicInListener_DoesNotCrashEngine(t *testing.T) {
	eng := newTestEngine()
	eng.Events.Subscribe(func(_ Event) {
		panic("listener exploded")
	})
	eng.Events.Subscribe(func(_ Event) {}) // second listener must still run

	// Must not panic.
	_, _ = eng.Setup(context.Background(), SetupOptions{Mode: ModeLocal, DryRun: true})
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// countingApplier wraps another applier and counts Apply calls.
type countingApplier struct {
	inner applyIface
	count *int
}

func (c *countingApplier) Apply(ctx context.Context, plan *InstallPlan) (*Transaction, error) {
	*c.count++
	return c.inner.Apply(ctx, plan)
}

// integMixedDoctorResult returns a DoctorResult with some failing checks that
// have known repair actions.
func integMixedDoctorResult() *DoctorResult {
	return &DoctorResult{
		Checks: []DoctorCheck{
			{ID: "DSO-DOCTOR-001", Category: DoctorCatDocker, Status: DoctorPass},
			{ID: "DSO-DOCTOR-012", Category: DoctorCatRuntime, Status: DoctorFail,
				Detail: "runtime directory missing"},
			{ID: "DSO-DOCTOR-016", Category: DoctorCatService, Status: DoctorFail,
				Detail: "service not enabled"},
		},
	}
}
