package setup

import (
	"context"
	"fmt"
	"time"
)

// Engine is the reusable setup orchestrator. It owns the event system and
// the permanent control-flow skeleton in Setup(). Individual stages are
// replaced incrementally by later phases without ever touching Setup() again.
//
// The engine must never print to stdout or stderr.
// It emits Events; the CLI subscribes and renders them.
type Engine struct {
	Events  *Emitter
	applier applyIface
}

// NewEngine constructs an Engine wired to a real Applier.
func NewEngine() *Engine {
	e := &Engine{Events: &Emitter{}}
	e.applier = newApplier(e.Events)
	return e
}

// ─── Stable orchestrator ──────────────────────────────────────────────────────

// Setup runs the full setup workflow for the given options.
func (e *Engine) Setup(ctx context.Context, opts SetupOptions) (*SetupResult, error) {
	start := time.Now()
	e.Events.emit(EventSetupStarted, opts, nil)

	// ── Stage 1: Detect ───────────────────────────────────────────────────
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
	e.Events.emit(EventApplyStarted, plan, nil)
	tx, err := e.apply(ctx, plan, opts)
	if err != nil {
		// Stage 6: Rollback — replay in reverse; collect partial failures.
		rb := newRollback(e.Events)
		rr := rb.Execute(ctx, tx)
		result, applyErr := e.fail(start, err)
		result.Rollback = rr
		return result, applyErr
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

func (e *Engine) detect(ctx context.Context, _ SetupOptions) (*Environment, error) {
	return newDetector().Detect(ctx)
}

func (e *Engine) validate(ctx context.Context, env *Environment, opts SetupOptions) (*ValidationResult, error) {
	return newValidator().Validate(ctx, env, opts)
}

func (e *Engine) plan(ctx context.Context, env *Environment, vr *ValidationResult, opts SetupOptions) (*InstallPlan, error) {
	return newPlanner().Plan(ctx, env, vr, opts)
}

func (e *Engine) preview(plan *InstallPlan, format string) (string, error) {
	return NewPreviewEngine(newRenderer(format)).Render(*plan)
}

func (e *Engine) apply(ctx context.Context, plan *InstallPlan, _ SetupOptions) (*Transaction, error) {
	return e.applier.Apply(ctx, plan)
}
