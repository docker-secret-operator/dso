package apply

import (
	"fmt"
	"reflect"

	"github.com/docker-secret-operator/dso/pkg/config"
)

// ComputePlan diffs the desired config against the current one and returns the
// set of provider/secret changes. It is pure (no Docker, no I/O), so it is safe
// to use for dry-run previews.
//
// When current is nil (e.g. `dso apply`, which applies a file with no prior
// state to diff against), every provider and secret in desired is reported as a
// create.
func ComputePlan(current, desired *config.Config) *ApplyPlan {
	plan := &ApplyPlan{
		TotalSecrets: len(desired.Secrets),
		Changes:      []PlanChange{},
	}

	curProviders := map[string]config.ProviderConfig{}
	curSecrets := map[string]config.SecretMapping{}
	if current != nil {
		curProviders = current.Providers
		for _, s := range current.Secrets {
			curSecrets[s.Name] = s
		}
	}

	desSecrets := map[string]config.SecretMapping{}
	for _, s := range desired.Secrets {
		desSecrets[s.Name] = s
	}

	// Providers: create / update / remove.
	for name, p := range desired.Providers {
		cur, ok := curProviders[name]
		switch {
		case !ok:
			plan.Changes = append(plan.Changes, PlanChange{
				Op: string(OpCreate), Kind: string(KindProvider), Name: name,
				NewValue: p.Type, Impact: fmt.Sprintf("add %s provider", p.Type),
			})
		case !reflect.DeepEqual(cur, p):
			plan.Changes = append(plan.Changes, PlanChange{
				Op: string(OpUpdate), Kind: string(KindProvider), Name: name,
				OldValue: cur.Type, NewValue: p.Type, Impact: "reconfigure provider",
			})
		}
	}
	for name, cur := range curProviders {
		if _, ok := desired.Providers[name]; !ok {
			plan.Changes = append(plan.Changes, PlanChange{
				Op: string(OpRemove), Kind: string(KindProvider), Name: name,
				OldValue: cur.Type, Impact: "remove provider",
			})
		}
	}

	// Secrets: create / update / remove.
	for name, s := range desSecrets {
		cur, ok := curSecrets[name]
		switch {
		case !ok:
			plan.SecretsToUpdate++
			plan.Changes = append(plan.Changes, PlanChange{
				Op: string(OpCreate), Kind: string(KindSecret), Name: name,
				NewValue: s.Provider, Impact: "fetch + inject secret",
			})
		case !reflect.DeepEqual(cur, s):
			plan.SecretsToUpdate++
			plan.Changes = append(plan.Changes, PlanChange{
				Op: string(OpUpdate), Kind: string(KindSecret), Name: name,
				OldValue: cur.Provider, NewValue: s.Provider, Impact: "re-inject secret",
			})
		}
	}
	for name, cur := range curSecrets {
		if _, ok := desSecrets[name]; !ok {
			plan.Changes = append(plan.Changes, PlanChange{
				Op: string(OpRemove), Kind: string(KindSecret), Name: name,
				OldValue: cur.Provider, Impact: "stop managing secret",
			})
		}
	}

	// Rough impact estimate: each created/updated secret touches at least one
	// container. Without live Docker state this is an upper-bound heuristic.
	plan.ContainersAffected = plan.SecretsToUpdate
	return plan
}

// RequiresRestart reports whether moving from current to desired changes
// anything the running agent cannot pick up via reconcile alone — i.e. provider
// definitions or global/agent settings. Secret-only changes do not require a
// restart. When current is nil there is nothing to compare, so it returns false.
func RequiresRestart(current, desired *config.Config) bool {
	if current == nil {
		return false
	}
	if !reflect.DeepEqual(current.Providers, desired.Providers) {
		return true
	}
	if !reflect.DeepEqual(current.Agent, desired.Agent) {
		return true
	}
	if !reflect.DeepEqual(current.Defaults, desired.Defaults) {
		return true
	}
	return false
}
