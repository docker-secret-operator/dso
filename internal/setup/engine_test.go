package setup

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fullSuccessEvents is the complete ordered event sequence for a successful
// non-dry-run setup at the Engine orchestration level. Transaction-level events
// (transaction_started, operation_started, …) appear between apply_started and
// apply_completed and are intentionally not captured here; they are tested in
// apply_test.go. Tests that use newTestEngine get a noopApplier that emits
// no transaction events, so this sequence stays stable across phases.
var fullSuccessEvents = []EventType{
	EventSetupStarted,
	EventDetectionCompleted,
	EventValidationCompleted,
	EventPlanGenerated,
	EventPreviewGenerated,
	EventApplyStarted,
	EventApplyCompleted,
	EventSetupCompleted,
}

// ─── Test helpers ─────────────────────────────────────────────────────────────

// noopApplier always succeeds and emits no transaction-level events.
// Used by newTestEngine so engine integration tests are OS-free.
type noopApplier struct{}

func (a *noopApplier) Apply(_ context.Context, plan *InstallPlan) (*Transaction, error) {
	return &Transaction{
		PlanID:    plan.ID,
		Status:    StatusCompleted,
		StartTime: time.Now(),
		EndTime:   time.Now(),
	}, nil
}

// stubApplier returns a controlled error so failure-path engine tests work
// without needing real OS interactions.
type stubApplier struct{ err error }

func (a *stubApplier) Apply(_ context.Context, _ *InstallPlan) (*Transaction, error) {
	tx := &Transaction{
		Status:    StatusCompleted,
		StartTime: time.Now(),
		EndTime:   time.Now(),
	}
	if a.err != nil {
		tx.Status = StatusFailed
		return tx, a.err
	}
	return tx, nil
}

func noopWizard(_ context.Context, _, _ string, _, _ bool) error { return nil }

// newTestEngine creates an Engine with a noopApplier so tests never touch the OS.
// The wizard parameter is accepted for call-site compatibility but is unused.
func newTestEngine(wizard LegacyWizardFunc) *Engine {
	e := NewEngine(wizard)
	e.applier = &noopApplier{}
	return e
}

// ─── Engine.Setup ─────────────────────────────────────────────────────────────

func TestEngine_Setup_Success(t *testing.T) {
	eng := newTestEngine(noopWizard)

	result, err := eng.Setup(context.Background(), SetupOptions{
		Mode:     ModeLocal,
		Provider: "local",
	})

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result.Status != "success" {
		t.Errorf("status: want 'success', got %q", result.Status)
	}
	if result.Duration <= 0 {
		t.Error("expected positive duration")
	}
}

func TestEngine_Setup_Failure(t *testing.T) {
	sentinel := errors.New("apply exploded")
	eng := newTestEngine(noopWizard)
	eng.applier = &stubApplier{err: sentinel}

	result, err := eng.Setup(context.Background(), SetupOptions{})

	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error wrapped, got: %v", err)
	}
	if result.Status != "failed" {
		t.Errorf("status: want 'failed', got %q", result.Status)
	}
}

func TestEngine_Setup_DryRun_SkipsApply(t *testing.T) {
	eng := newTestEngine(noopWizard)

	result, err := eng.Setup(context.Background(), SetupOptions{DryRun: true})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "pending" {
		t.Errorf("status: want 'pending', got %q", result.Status)
	}
}

func TestEngine_Setup_DryRun_PlanHasDryRunFlag(t *testing.T) {
	eng := newTestEngine(noopWizard)

	result, _ := eng.Setup(context.Background(), SetupOptions{DryRun: true})

	if result.Plan == nil {
		t.Fatal("expected a plan in the result")
	}
	if !result.Plan.DryRun {
		t.Error("plan.DryRun should be true")
	}
}

func TestEngine_Setup_PlanModeAndProviderPropagated(t *testing.T) {
	eng := newTestEngine(noopWizard)

	result, _ := eng.Setup(context.Background(), SetupOptions{
		Mode:     ModeAgent,
		Provider: "aws",
		DryRun:   true, // dry-run keeps apply from running; plan is still generated
	})

	if result.Plan == nil {
		t.Fatal("expected a plan in the result")
	}
	if result.Plan.Mode != ModeAgent {
		t.Errorf("mode: want 'agent', got %q", result.Plan.Mode)
	}
	if result.Plan.Provider != "aws" {
		t.Errorf("provider: want 'aws', got %q", result.Plan.Provider)
	}
}

// ─── Lifecycle events ─────────────────────────────────────────────────────────

func TestEngine_Setup_EmitsFullLifecycle_Success(t *testing.T) {
	eng := newTestEngine(noopWizard)
	events := collectEvents(eng)

	_, _ = eng.Setup(context.Background(), SetupOptions{})

	assertEventSequence(t, events(), fullSuccessEvents)
}

func TestEngine_Setup_EmitsFailureEvent(t *testing.T) {
	eng := newTestEngine(noopWizard)
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

func TestEngine_Setup_DryRun_StopsAfterPreview(t *testing.T) {
	eng := newTestEngine(noopWizard)
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

// ─── Stage methods ────────────────────────────────────────────────────────────

func TestEngine_detect_ReturnsEnvironment(t *testing.T) {
	eng := newTestEngine(noopWizard)
	env, err := eng.detect(context.Background(), SetupOptions{Mode: ModeLocal, Provider: "local"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env == nil {
		t.Fatal("expected non-nil Environment")
	}
	if env.Timestamp.IsZero() {
		t.Error("expected Timestamp to be set")
	}
}

func TestEngine_validate_EmptyEnvironmentReturnsInvalid(t *testing.T) {
	eng := newTestEngine(noopWizard)
	vr, err := eng.validate(context.Background(), &Environment{}, SetupOptions{Mode: ModeLocal})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vr.Valid {
		t.Error("expected Valid=false for environment with no Docker")
	}
	if len(vr.Errors()) == 0 {
		t.Error("expected at least one validation error")
	}
}

func TestEngine_validate_HealthyLocalEnvironmentReturnsValid(t *testing.T) {
	eng := newTestEngine(noopWizard)
	env := &Environment{
		Docker: DockerInfo{BinaryFound: true, DaemonReachable: true},
		Capabilities: Capabilities{
			SupportsDocker:    true,
			SupportsLocalMode: true,
		},
	}
	vr, err := eng.validate(context.Background(), env, SetupOptions{Mode: ModeLocal, Provider: "local"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !vr.Valid {
		t.Errorf("expected Valid=true for healthy local environment, got errors: %v", vr.Errors())
	}
}

func TestEngine_plan_PropagatesModeAndProvider(t *testing.T) {
	eng := newTestEngine(noopWizard)
	env := &Environment{}
	vr := &ValidationResult{}
	plan, err := eng.plan(context.Background(), env, vr, SetupOptions{
		Mode:     ModeAgent,
		Provider: "aws",
		DryRun:   true,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.Mode != ModeAgent {
		t.Errorf("mode: want 'agent', got %q", plan.Mode)
	}
	if plan.Provider != "aws" {
		t.Errorf("provider: want 'aws', got %q", plan.Provider)
	}
	if !plan.DryRun {
		t.Error("DryRun should be propagated to plan")
	}
}

func TestEngine_plan_FallsBackToCapabilities(t *testing.T) {
	eng := newTestEngine(noopWizard)
	env := &Environment{
		Capabilities: Capabilities{
			SupportsAgentMode: false,
			SupportsLocalMode: true,
		},
		Providers: DetectedProviders{
			Vault: VaultInfo{Detected: true},
		},
	}
	plan, _ := eng.plan(context.Background(), env, &ValidationResult{}, SetupOptions{})

	if plan.Mode != ModeLocal {
		t.Errorf("mode: want 'local' from capabilities, got %q", plan.Mode)
	}
	if plan.Provider != "vault" {
		t.Errorf("provider: want 'vault' from detected providers, got %q", plan.Provider)
	}
}

func TestEngine_plan_HasID(t *testing.T) {
	eng := newTestEngine(noopWizard)
	plan, err := eng.plan(context.Background(), &Environment{}, &ValidationResult{}, SetupOptions{
		Mode:     ModeLocal,
		Provider: "local",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.ID == "" {
		t.Error("expected plan to have a non-empty ID")
	}
}

func TestEngine_preview_TerminalRenderer(t *testing.T) {
	eng := newTestEngine(noopWizard)
	out, err := eng.preview(&InstallPlan{Mode: ModeLocal, Provider: "local"}, "terminal")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == "" {
		t.Error("expected non-empty terminal preview")
	}
}

func TestEngine_preview_JSONRenderer(t *testing.T) {
	eng := newTestEngine(noopWizard)
	out, err := eng.preview(&InstallPlan{Mode: ModeLocal, Provider: "local"}, "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == "" {
		t.Error("expected non-empty JSON preview")
	}
}

func TestEngine_preview_DefaultIsTerminal(t *testing.T) {
	eng := newTestEngine(noopWizard)
	out, err := eng.preview(&InstallPlan{Mode: ModeLocal}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "DSO Setup Plan") {
		t.Errorf("expected terminal header in default format output, got:\n%s", out)
	}
}

func TestEngine_apply_CompletesTransaction(t *testing.T) {
	eng := newTestEngine(noopWizard)
	plan := &InstallPlan{ID: "plan-test-001", Mode: ModeLocal, Provider: "local"}
	tx, err := eng.apply(context.Background(), plan, SetupOptions{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.Status != StatusCompleted {
		t.Errorf("tx.Status: want Completed, got %q", tx.Status)
	}
	if tx.EndTime.IsZero() {
		t.Error("expected EndTime to be set")
	}
}

func TestEngine_apply_FailedApplierReturnsFailedTransaction(t *testing.T) {
	eng := newTestEngine(noopWizard)
	eng.applier = &stubApplier{err: errors.New("disk full")}

	tx, err := eng.apply(context.Background(), &InstallPlan{}, SetupOptions{})

	if err == nil {
		t.Fatal("expected error from apply()")
	}
	if tx.Status != StatusFailed {
		t.Errorf("tx.Status: want Failed, got %q", tx.Status)
	}
}

// ─── Emitter ─────────────────────────────────────────────────────────────────

func TestEmitter_Subscribe_ReceivesEvent(t *testing.T) {
	e := &Emitter{}
	var received []Event
	e.Subscribe(func(evt Event) { received = append(received, evt) })

	e.Emit(Event{Type: EventSetupStarted})

	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %d", len(received))
	}
	if received[0].Type != EventSetupStarted {
		t.Errorf("expected EventSetupStarted, got %q", received[0].Type)
	}
}

func TestEmitter_Emit_SetsTimestamp(t *testing.T) {
	e := &Emitter{}
	var received Event
	e.Subscribe(func(evt Event) { received = evt })

	before := time.Now()
	e.Emit(Event{Type: EventSetupStarted})

	if received.Timestamp.Before(before) {
		t.Error("expected timestamp >= before-emit time")
	}
}

func TestEmitter_MultipleListeners(t *testing.T) {
	e := &Emitter{}
	count := 0
	for range 5 {
		e.Subscribe(func(_ Event) { count++ })
	}
	e.Emit(Event{Type: EventSetupStarted})
	if count != 5 {
		t.Errorf("expected 5 listeners called, got %d", count)
	}
}

func TestEmitter_NoListeners_NoPanic(t *testing.T) {
	e := &Emitter{}
	e.Emit(Event{Type: EventSetupStarted}) // must not panic
}

func TestEmitter_PreservesEventOrder(t *testing.T) {
	e := &Emitter{}
	var received []EventType
	e.Subscribe(func(evt Event) { received = append(received, evt.Type) })

	types := []EventType{
		EventSetupStarted,
		EventDetectionCompleted,
		EventValidationCompleted,
		EventPlanGenerated,
		EventSetupCompleted,
	}
	for _, et := range types {
		e.Emit(Event{Type: et})
	}

	for i, want := range types {
		if received[i] != want {
			t.Errorf("event[%d]: want %q, got %q", i, want, received[i])
		}
	}
}

// ─── Collection helpers ───────────────────────────────────────────────────────

func collectEvents(eng *Engine) func() []EventType {
	var received []EventType
	eng.Events.Subscribe(func(evt Event) {
		received = append(received, evt.Type)
	})
	return func() []EventType { return received }
}

func assertEventSequence(t *testing.T, got []EventType, want []EventType) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("event count: want %d, got %d\n  want: %v\n   got: %v", len(want), len(got), want, got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("event[%d]: want %q, got %q", i, w, got[i])
		}
	}
}
