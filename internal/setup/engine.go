package setup

import (
	"context"
	"fmt"
	"time"
)

// LegacyWizardFunc is the signature of the existing setup wizard.
// The engine holds a reference so it can delegate until each phase is
// migrated to a native implementation.
type LegacyWizardFunc func(ctx context.Context, mode, provider string, autoDetect, nonRoot bool) error

// Engine is the reusable setup orchestrator.
//
// Phase 1: The engine owns the event system and the top-level workflow but
// delegates the actual work to legacyWizard. Subsequent phases will replace
// that delegation with native Detector, Validator, Planner, Applier, and
// Rollback implementations — one phase at a time.
//
// The engine must never print to stdout or stderr. It emits Events; the CLI
// subscribes and renders them.
type Engine struct {
	Events       *Emitter
	legacyWizard LegacyWizardFunc
}

// NewEngine constructs an Engine wired to the provided legacy wizard.
// Pass nil for legacyWizard only in tests that stub every stage.
func NewEngine(wizard LegacyWizardFunc) *Engine {
	return &Engine{
		Events:       &Emitter{},
		legacyWizard: wizard,
	}
}

// Setup runs the full setup workflow for the given options.
//
// Phase 1 behaviour: emits lifecycle events around the legacy wizard so that
// future phases can replace the wizard call with native implementations
// without changing the event contract or the CLI.
func (e *Engine) Setup(ctx context.Context, opts SetupOptions) (*SetupResult, error) {
	start := time.Now()

	e.Events.emit(EventSetupStarted, opts, nil)

	result := &SetupResult{
		Status: "pending",
	}

	// Resolve mode string for the legacy wizard.
	mode := string(opts.Mode)
	provider := opts.Provider

	// Delegate to legacy wizard.
	// Phases 2-6 will replace this call section by section.
	if err := e.legacyWizard(ctx, mode, provider, opts.AutoDetect, opts.NonRoot); err != nil {
		result.Status = "failed"
		result.Duration = time.Since(start)
		e.Events.emit(EventSetupFailed, result, err)
		return result, fmt.Errorf("setup failed: %w", err)
	}

	result.Status = "success"
	result.Duration = time.Since(start)
	e.Events.emit(EventSetupCompleted, result, nil)

	return result, nil
}
