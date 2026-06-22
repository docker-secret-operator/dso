package apply

import (
	"context"

	"github.com/docker-secret-operator/dso/pkg/config"
)

// Execute runs best-effort reconciliation for a plan via the supplied
// Reconciler and maps the outcome to an ApplyResult.
//
// It never returns a hard error for a reconcile failure: the config has already
// been written by the caller, so a failed reconcile is reported in
// ApplyResult.Error (Success=false) rather than unwinding state. A nil
// Reconciler means "saved only, no reconcile" and yields Success=true.
func Execute(ctx context.Context, cfg *config.Config, plan *ApplyPlan, r Reconciler) (*ApplyResult, error) {
	if r == nil {
		return &ApplyResult{Success: true}, nil
	}
	if err := r.Reconcile(ctx, cfg, plan); err != nil {
		return &ApplyResult{Success: false, Error: err.Error()}, nil
	}
	return &ApplyResult{Success: true}, nil
}
