package setup

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fullSuccessEvents is the complete ordered event sequence for a successful
// non-dry-run setup. Every test that checks lifecycle events uses this so
// there is a single source of truth for the expected sequence.
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

// ─── Engine.Setup ─────────────────────────────────────────────────────────────

func TestEngine_Setup_Success(t *testing.T) {
	var called bool
	eng := newTestEngine(func(_ context.Context, _, _ string, _, _ bool) error {
		called = true
		return nil
	})

	result, err := eng.Setup(context.Background(), SetupOptions{
		Mode:     ModeLocal,
		Provider: "local",
	})

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !called {
		t.Fatal("expected legacy wizard to be called")
	}
	if result.Status != "success" {
		t.Errorf("status: want 'success', got %q", result.Status)
	}
	if result.Duration <= 0 {
		t.Error("expected positive duration")
	}
}

func TestEngine_Setup_Failure(t *testing.T) {
	sentinel := errors.New("wizard exploded")
	eng := newTestEngine(func(_ context.Context, _, _ string, _, _ bool) error {
		return sentinel
	})

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

func TestEngine_Setup_DryRun_DoesNotCallWizard(t *testing.T) {
	var called bool
	eng := newTestEngine(func(_ context.Context, _, _ string, _, _ bool) error {
		called = true
		return nil
	})

	result, err := eng.Setup(context.Background(), SetupOptions{DryRun: true})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("legacy wizard must not be called during dry-run")
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

func TestEngine_Setup_OptionsPassedToWizard(t *testing.T) {
	var (
		capturedMode     string
		capturedProvider string
		capturedDetect   bool
		capturedNonRoot  bool
	)
	eng := newTestEngine(func(_ context.Context, mode, provider string, autoDetect, nonRoot bool) error {
		capturedMode = mode
		capturedProvider = provider
		capturedDetect = autoDetect
		capturedNonRoot = nonRoot
		return nil
	})

	_, _ = eng.Setup(context.Background(), SetupOptions{
		Mode:       ModeAgent,
		Provider:   "aws",
		AutoDetect: true,
		NonRoot:    true,
	})

	if capturedMode != "agent" {
		t.Errorf("mode: want 'agent', got %q", capturedMode)
	}
	if capturedProvider != "aws" {
		t.Errorf("provider: want 'aws', got %q", capturedProvider)
	}
	if !capturedDetect {
		t.Error("autoDetect: expected true")
	}
	if !capturedNonRoot {
		t.Error("nonRoot: expected true")
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
	eng := newTestEngine(func(_ context.Context, _, _ string, _, _ bool) error {
		return errors.New("boom")
	})
	events := collectEvents(eng)

	_, _ = eng.Setup(context.Background(), SetupOptions{})

	// On apply failure the engine emits: rollback start/complete then setup_failed.
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

	// Dry-run must not emit apply events.
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

// ─── Stage stubs ─────────────────────────────────────────────────────────────

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
	env := &Environment{} // opts override all — env contents don't matter here
	plan, err := eng.plan(context.Background(), env, SetupOptions{
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
	// No systemd+root → ModeLocal; Vault detected → provider "vault".
	env := &Environment{
		Capabilities: Capabilities{
			SupportsAgentMode: false,
			SupportsLocalMode: true,
		},
		Providers: DetectedProviders{
			Vault: VaultInfo{Detected: true},
		},
	}
	// No mode/provider in opts — should fall back to computed recommendation.
	plan, _ := eng.plan(context.Background(), env, SetupOptions{})

	if plan.Mode != ModeLocal {
		t.Errorf("mode: want 'local' from capabilities, got %q", plan.Mode)
	}
	if plan.Provider != "vault" {
		t.Errorf("provider: want 'vault' from detected providers, got %q", plan.Provider)
	}
}

// ─── computeRecommendation ────────────────────────────────────────────────────

func TestComputeRecommendation_LocalModeWhenAgentNotSupported(t *testing.T) {
	env := &Environment{Capabilities: Capabilities{SupportsAgentMode: false}}
	mode, _ := computeRecommendation(env)
	if mode != ModeLocal {
		t.Errorf("want ModeLocal, got %q", mode)
	}
}

func TestComputeRecommendation_AgentModeWhenSupported(t *testing.T) {
	env := &Environment{Capabilities: Capabilities{SupportsAgentMode: true}}
	mode, _ := computeRecommendation(env)
	if mode != ModeAgent {
		t.Errorf("want ModeAgent, got %q", mode)
	}
}

func TestComputeRecommendation_DefaultProviderIsLocal(t *testing.T) {
	env := &Environment{}
	_, provider := computeRecommendation(env)
	if provider != "local" {
		t.Errorf("want 'local', got %q", provider)
	}
}

func TestComputeRecommendation_AWSBeatsAllOthers(t *testing.T) {
	env := &Environment{
		Providers: DetectedProviders{
			AWS:   AWSInfo{Detected: true},
			Azure: AzureInfo{Detected: true},
			Vault: VaultInfo{Detected: true},
		},
	}
	_, provider := computeRecommendation(env)
	if provider != "aws" {
		t.Errorf("want 'aws', got %q", provider)
	}
}

func TestComputeRecommendation_AzureBeatsVault(t *testing.T) {
	env := &Environment{
		Providers: DetectedProviders{
			Azure: AzureInfo{Detected: true},
			Vault: VaultInfo{Detected: true},
		},
	}
	_, provider := computeRecommendation(env)
	if provider != "azure" {
		t.Errorf("want 'azure', got %q", provider)
	}
}

func TestEngine_preview_ReturnsString(t *testing.T) {
	eng := newTestEngine(noopWizard)
	// Phase 1.5 stub returns empty string; test just ensures it doesn't panic.
	result := eng.preview(&InstallPlan{})
	_ = result // empty in Phase 1.5, Terraform-style output in Phase 5
}

func TestEngine_apply_CallsWizard(t *testing.T) {
	var called bool
	eng := newTestEngine(func(_ context.Context, _, _ string, _, _ bool) error {
		called = true
		return nil
	})

	plan := &InstallPlan{Mode: ModeLocal, Provider: "local"}
	tx, err := eng.apply(context.Background(), plan, SetupOptions{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected legacy wizard to be called from apply()")
	}
	if tx.Status != StatusCompleted {
		t.Errorf("tx.Status: want Completed, got %q", tx.Status)
	}
	if tx.EndTime.IsZero() {
		t.Error("expected EndTime to be set")
	}
}

func TestEngine_apply_ReturnsFailedTransaction(t *testing.T) {
	eng := newTestEngine(func(_ context.Context, _, _ string, _, _ bool) error {
		return errors.New("disk full")
	})

	plan := &InstallPlan{}
	tx, err := eng.apply(context.Background(), plan, SetupOptions{})

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

// ─── Helpers ─────────────────────────────────────────────────────────────────

func noopWizard(_ context.Context, _, _ string, _, _ bool) error { return nil }

func newTestEngine(wizard LegacyWizardFunc) *Engine {
	return NewEngine(wizard)
}

// collectEvents subscribes before the test action and returns a func that
// delivers the accumulated slice after the action completes. Because the
// Emitter is synchronous this is race-free without any additional locking.
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
