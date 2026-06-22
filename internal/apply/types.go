// Package apply computes and executes DSO configuration apply plans.
//
// It is shared by the CLI (`dso apply`) and the REST API (dashboard config
// editing) so there is exactly one implementation of "what will change" and
// "make it so". The package depends only on pkg/config; the mechanism for
// reconciling running containers is injected via the Reconciler interface, so
// callers supply their own (socket-based for the CLI, in-process trigger engine
// for the API).
package apply

import (
	"context"

	"github.com/docker-secret-operator/dso/pkg/config"
)

// Op is the kind of change a PlanChange represents.
type Op string

const (
	OpCreate Op = "create"
	OpUpdate Op = "update"
	OpRemove Op = "remove"
)

// Kind is the type of object a PlanChange applies to.
type Kind string

const (
	KindProvider Kind = "provider"
	KindSecret   Kind = "secret"
)

// PlanChange is a single create/update/remove of a provider or secret.
//
// Security note: OldValue/NewValue intentionally carry only non-sensitive
// summaries (e.g. a provider type), never raw provider config — which can hold
// credentials — so the plan is safe to return to the dashboard.
type PlanChange struct {
	Op       string `json:"op"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	OldValue any    `json:"old_value,omitempty"`
	NewValue any    `json:"new_value,omitempty"`
	Impact   string `json:"impact,omitempty"`
}

// ApplyPlan summarises the changes between the current and desired config.
type ApplyPlan struct {
	TotalSecrets       int          `json:"total_secrets"`
	ContainersAffected int          `json:"containers_affected"`
	SecretsToUpdate    int          `json:"secrets_to_update"`
	Changes            []PlanChange `json:"changes"`
}

// ApplyResult is the outcome of executing a plan's reconciliation.
type ApplyResult struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// Reconciler applies the desired config to running containers (best effort).
// Implementations: a socket client (CLI) or the in-process trigger engine (API).
type Reconciler interface {
	Reconcile(ctx context.Context, cfg *config.Config, plan *ApplyPlan) error
}
