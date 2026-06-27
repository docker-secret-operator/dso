package setup

import (
	"context"
	"fmt"
	"time"
)

// LegacyWizardFunc is retained for interface compatibility with existing callers.
// It is no longer invoked by the engine after Phase 6; removal is Phase 10.
type LegacyWizardFunc func(ctx context.Context, mode, provider string, autoDetect, nonRoot bool) error

// Engine is the reusable setup orchestrator. It owns the event system and
// the permanent control-flow skeleton in Setup(). Individual stages are
// replaced incrementally by later phases without ever touching Setup() again.
//
// The engine must never print to stdout or stderr.
// It emits Events; the CLI subscribes and renders them.
type Engine struct {
	Events       *Emitter
	legacyWizard LegacyWizardFunc // retained for signature compatibility; unused after Phase 6
	applier      applyIface
}

// NewEngine constructs an Engine wired to a real Applier.
// The wizard parameter is retained for call-site compatibility but no longer used in apply().
func NewEngine(wizard LegacyWizardFunc) *Engine {
	e := &Engine{
		Events:       &Emitter{},
		legacyWizard: wizard,
	}
	e.applier = newApplier(e.Events)
	return e
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
	vr, err := e.validate(ctx, env, opts)
	if err != nil {
		return e.fail(start, fmt.Errorf("validation failed: %w", err))
	}
	e.Events.emit(EventValidationCompleted, vr, nil)

	// ── Stage 3: Plan ─────────────────────────────────────────────────────
	plan, err := e.plan(ctx, env, vr, opts)
	if err != nil {
		return e.fail(start, fmt.Errorf("planning failed: %w", err))
	}
	e.Events.emit(EventPlanGenerated, plan, nil)

	// ── Stage 4: Preview ──────────────────────────────────────────────────
	preview, err := e.preview(plan, opts.Format)
	if err != nil {
		return e.fail(start, fmt.Errorf("preview failed: %w", err))
	}
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
// opts are intentionally ignored here; plan() applies user overrides on top.
func (e *Engine) detect(ctx context.Context, _ SetupOptions) (*Environment, error) {
	return newDetector().Detect(ctx)
}

// validate checks whether the detected environment can support the requested
// setup. It delegates to the Validator; the result is informational — the
// engine emits it via EventValidationCompleted and the CLI decides whether to
// abort. The legacy wizard performs its own final check during apply().
func (e *Engine) validate(ctx context.Context, env *Environment, opts SetupOptions) (*ValidationResult, error) {
	return newValidator().Validate(ctx, env, opts)
}

// plan generates an immutable InstallPlan. The ValidationResult is threaded in
// so the Planner can inspect existing-installation findings without re-validating.
func (e *Engine) plan(ctx context.Context, env *Environment, vr *ValidationResult, opts SetupOptions) (*InstallPlan, error) {
	return newPlanner().Plan(ctx, env, vr, opts)
}

// preview renders the InstallPlan using the renderer selected by format.
// "json" → JSONRenderer; anything else (including "") → TerminalRenderer.
// Returns (string, error) so failures surface through the normal error path.
func (e *Engine) preview(plan *InstallPlan, format string) (string, error) {
	return NewPreviewEngine(newRenderer(format)).Render(*plan)
}

// apply executes the InstallPlan using the transactional Applier.
// Every operation is tracked in a Transaction for Phase 7 rollback.
// The legacy wizard is no longer called.
func (e *Engine) apply(ctx context.Context, plan *InstallPlan, _ SetupOptions) (*Transaction, error) {
	return e.applier.Apply(ctx, plan)
}
