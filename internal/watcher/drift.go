package watcher

import (
	"context"
	"strings"

	"github.com/docker/docker/api/types/container"
)

// Drift classification values. Kept deliberately small for the first
// implementation -- only the container/secret-mapping axis DSO can already
// observe from data it already reads (declared config + live container
// labels), not provider-config drift, injected-content drift, or
// schedule drift, none of which DSO has any introspection capability for
// today.
const (
	DriftMissingContainer = "MISSING_CONTAINER"
	DriftMissingMapping   = "MISSING_MAPPING"
	DriftMappingMismatch  = "MAPPING_MISMATCH"
)

// DriftFinding describes one declared secret/container mapping that does
// not match Docker's live state. Only genuine mismatches are ever
// returned -- there is no finding for a mapping that already matches.
// Fields are strictly safe metadata (secret/container/target names, label
// presence) -- never a secret value.
type DriftFinding struct {
	Secret    string `json:"secret"`
	Target    string `json:"target"`
	Container string `json:"container,omitempty"`
	DriftType string `json:"drift_type"`
	Expected  string `json:"expected"`
	Actual    string `json:"actual"`
}

// DriftSummary counts the declared secret/target pairs that were checked.
type DriftSummary struct {
	Total   int `json:"total"`
	InSync  int `json:"in_sync"`
	Drifted int `json:"drifted"`
}

// ComputeDrift compares every SecretMapping's explicitly declared
// Targets.Containers against Docker's live container list, using the same
// name/ID matching semantics as populateInitialTargets's config-driven
// fallback pass (target == name || target == c.ID; exact match only --
// deliberately no substring/fuzzy matching, so a similarly-named unrelated
// container never produces a false positive).
//
// Secrets with no declared Targets.Containers are skipped entirely: there
// is no specific expected container to check against, matching how
// populateInitialTargets already treats them (covered by label-driven
// registration only, with no config-declared name to compare).
//
// This is read-only: it never mutates r.Targets, never triggers a
// rotation, and issues exactly one Docker ContainerList call -- the same
// call populateInitialTargets's config-driven pass already makes, at the
// same "running containers only" scope (no All:true), so "live" means the
// same thing here as it does everywhere else in this package.
func (r *ReloaderController) ComputeDrift(ctx context.Context) (*DriftSummary, []DriftFinding, error) {
	summary := &DriftSummary{}
	findings := make([]DriftFinding, 0)

	if r.Config == nil {
		return summary, findings, nil
	}

	live, err := r.listLiveContainersForDrift(ctx)
	if err != nil {
		return nil, nil, err
	}

	for _, sec := range r.Config.Secrets {
		for _, target := range sec.Targets.Containers {
			if target == "" {
				continue
			}
			summary.Total++

			match := findLiveContainerByTarget(live, target)
			if match == nil {
				summary.Drifted++
				findings = append(findings, DriftFinding{
					Secret:    sec.Name,
					Target:    target,
					DriftType: DriftMissingContainer,
					Expected:  "a running container named or with ID \"" + target + "\"",
					Actual:    "no matching running container found",
				})
				continue
			}

			if !match.hasReloaderLabel {
				summary.Drifted++
				findings = append(findings, DriftFinding{
					Secret:    sec.Name,
					Target:    target,
					Container: match.id,
					DriftType: DriftMissingMapping,
					Expected:  "dso.reloader=true label with dso.secrets including \"" + sec.Name + "\"",
					Actual:    "dso.reloader label absent on the running container (matched via config target-name fallback only)",
				})
				continue
			}

			if !match.secrets[sec.Name] {
				summary.Drifted++
				findings = append(findings, DriftFinding{
					Secret:    sec.Name,
					Target:    target,
					Container: match.id,
					DriftType: DriftMappingMismatch,
					Expected:  "dso.secrets label including \"" + sec.Name + "\"",
					Actual:    "dso.secrets label present but does not include \"" + sec.Name + "\"",
				})
				continue
			}

			summary.InSync++
		}
	}

	return summary, findings, nil
}

// liveDriftContainer is the subset of a live container's identity/labels
// this comparator needs. Kept local to this file -- it is not the same
// shape as TargetContainer (which tracks DSO's own resolved bookkeeping,
// not raw label state) and has no reason to be exported.
type liveDriftContainer struct {
	id               string
	name             string
	hasReloaderLabel bool
	secrets          map[string]bool
}

func (r *ReloaderController) listLiveContainersForDrift(ctx context.Context) ([]liveDriftContainer, error) {
	containers, err := r.cli.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		return nil, err
	}

	live := make([]liveDriftContainer, 0, len(containers))
	for _, c := range containers {
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		secretSet := make(map[string]bool)
		for _, s := range strings.Split(c.Labels["dso.secrets"], ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				secretSet[s] = true
			}
		}
		live = append(live, liveDriftContainer{
			id:               c.ID,
			name:             name,
			hasReloaderLabel: c.Labels["dso.reloader"] == "true",
			secrets:          secretSet,
		})
	}
	return live, nil
}

func findLiveContainerByTarget(live []liveDriftContainer, target string) *liveDriftContainer {
	for i := range live {
		if live[i].name == target || live[i].id == target {
			return &live[i]
		}
	}
	return nil
}
