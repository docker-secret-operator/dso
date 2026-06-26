package setup

import (
	"context"
	"fmt"
	"time"
)

// LegacyWizardFunc is the signature of the existing setup wizard.
// The engine holds a reference until each stage is replaced by a native
// implementation (Phases 2-6). After Phase 6 it can be removed entirely.
type LegacyWizardFunc func(ctx context.Context, mode, provider string, autoDetect, nonRoot bool) error

// Engine is the reusable setup orchestrator. It owns the event system and
// the permanent control-flow skeleton in Setup(). Individual stages are
// replaced incrementally by later phases without ever touching Setup() again.
//
// The engine must never print to stdout or stderr.
// It emits Events; the CLI subscribes and renders them.
type Engine struct {
	Events       *Emitter
	legacyWizard LegacyWizardFunc
}

// NewEngine constructs an Engine wired to the provided legacy wizard.
// Pass nil for legacyWizard only in tests that stub all stages explicitly.
func NewEngine(wizard LegacyWizardFunc) *Engine {
	return &Engine{
		Events:       &Emitter{},
		legacyWizard: wizard,
	}
}

// ─── Stable orchestrator ──────────────────────────────────────────────────────
//
// Setup is the permanent control-flow skeleton. It must not change after
// Phase 1.5. Only the five private stage methods below evolve over time.

// Setup runs the full setup workflow for the given options.
func (e *Engine) Setup(ctx context.Context, opts SetupOptions) (*SetupResult, error) {
	start := time.Now()
	e.Events.emit(EventSetupStarted, opts, nil)

	// ── Stage 1: Detect ───────────────────────────────────────────────────
	// Phase 2 replaces e.detect() with a native implementation.
	env, err := e.detect(ctx, opts)
	if err != nil {
		return e.fail(start, fmt.Errorf("detection failed: %w", err))
	}
	e.Events.emit(EventDetectionCompleted, env, nil)

	// ── Stage 2: Validate ─────────────────────────────────────────────────
	// Phase 3 replaces e.validate() with a native implementation.
	vr, err := e.validate(ctx, env)
	if err != nil {
		return e.fail(start, fmt.Errorf("validation failed: %w", err))
	}
	e.Events.emit(EventValidationCompleted, vr, nil)

	// ── Stage 3: Plan ─────────────────────────────────────────────────────
	// Phase 4 replaces e.plan() with a native implementation.
	plan, err := e.plan(ctx, env, opts)
	if err != nil {
		return e.fail(start, fmt.Errorf("planning failed: %w", err))
	}
	e.Events.emit(EventPlanGenerated, plan, nil)

	// ── Stage 4: Preview ──────────────────────────────────────────────────
	// Phase 5 replaces e.preview() with a native implementation.
	preview := e.preview(plan)
	e.Events.emit(EventPreviewGenerated, preview, nil)

	// Dry-run gate: stop here without writing anything.
	if plan.DryRun {
		result := &SetupResult{Plan: plan, Status: "pending", Duration: time.Since(start)}
		e.Events.emit(EventSetupCompleted, result, nil)
		return result, nil
	}

	// ── Stage 5: Apply ────────────────────────────────────────────────────
	// Phase 6 replaces e.apply() with a native transactional implementation.
	e.Events.emit(EventApplyStarted, plan, nil)
	tx, err := e.apply(ctx, plan, opts)
	if err != nil {
		e.Events.emit(EventRollbackStarted, tx, nil)
		// Phase 7 will add real rollback here; for now the legacy wizard
		// does not produce partial state that needs undoing.
		e.Events.emit(EventRollbackCompleted, tx, nil)
		return e.fail(start, err)
	}
	e.Events.emit(EventApplyCompleted, tx, nil)

	result := &SetupResult{
		Plan:        plan,
		Transaction: tx,
		Status:      "success",
		Duration:    time.Since(start),
	}
	e.Events.emit(EventSetupCompleted, result, nil)
	return result, nil
}

// fail is a convenience builder for failed results.
func (e *Engine) fail(start time.Time, err error) (*SetupResult, error) {
	result := &SetupResult{Status: "failed", Duration: time.Since(start)}
	e.Events.emit(EventSetupFailed, result, err)
	return result, err
}

// ─── Stage methods ────────────────────────────────────────────────────────────
//
// Each method below is a seam. Phase 2-6 replaces them one at a time.
// Only the method being replaced changes; Setup() above stays frozen.

// detect gathers environmental facts. It must never fail due to missing
// optional data — absence of a credential is a fact, not an error.
//
// Phase 2 replaces this with native detection across detect_docker.go,
// detect_os.go, detect_systemd.go, detect_provider.go, etc.
func (e *Engine) detect(_ context.Context, opts SetupOptions) (*Environment, error) {
	// Stub: return a minimal environment derived from the options so that
	// plan() and apply() have something to work with.
	return &Environment{
		RecommendedMode:     opts.Mode,
		RecommendedProvider: opts.Provider,
		Timestamp:           time.Now(),
		Metadata:            make(map[string]interface{}),
	}, nil
}

// validate checks whether the detected environment is usable.
//
// Phase 3 replaces this with checks for Docker access, permissions,
// provider credentials, and filesystem writability.
func (e *Engine) validate(_ context.Context, _ *Environment) (*ValidationResult, error) {
	// Stub: always valid — the legacy wizard performs its own validation
	// internally and returns an error if something is wrong.
	return &ValidationResult{Valid: true}, nil
}

// plan generates an immutable InstallPlan from the detected environment.
//
// Phase 4 replaces this with a real planner that builds the full file/
// directory/service/permission change list without touching the filesystem.
func (e *Engine) plan(_ context.Context, env *Environment, opts SetupOptions) (*InstallPlan, error) {
	// Stub: capture the options that apply() needs to invoke the legacy wizard.
	mode := opts.Mode
	if mode == "" {
		mode = env.RecommendedMode
	}
	provider := opts.Provider
	if provider == "" {
		provider = env.RecommendedProvider
	}

	return &InstallPlan{
		Mode:     mode,
		Provider: string(provider),
		DryRun:   opts.DryRun,
		Metadata: map[string]string{
			"legacy": "true", // removed when Phase 4 ships
		},
	}, nil
}

// preview renders the InstallPlan for user confirmation.
//
// Phase 5 replaces this with a Terraform-style diff renderer.
func (e *Engine) preview(_ *InstallPlan) string {
	// Stub: no preview yet — the legacy wizard prints its own summary.
	return ""
}

// apply executes the InstallPlan transactionally.
//
// Phase 6 replaces this with a real Applier that records every operation
// in a Transaction and triggers rollback on failure.
func (e *Engine) apply(ctx context.Context, plan *InstallPlan, opts SetupOptions) (*Transaction, error) {
	tx := &Transaction{
		PlanID:    plan.ID,
		Status:    StatusRunning,
		StartTime: time.Now(),
	}

	// All real work still flows through the legacy wizard for now.
	if err := e.legacyWizard(ctx, string(plan.Mode), plan.Provider, opts.AutoDetect, opts.NonRoot); err != nil {
		tx.Status = StatusFailed
		tx.EndTime = time.Now()
		return tx, fmt.Errorf("apply failed: %w", err)
	}

	tx.Status = StatusCompleted
	tx.EndTime = time.Now()
	return tx, nil
}
