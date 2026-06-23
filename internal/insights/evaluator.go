// Package insights derives deterministic, evidence-based recommendations
// from compliance, drift, and policy data. It imports both the recommendation
// and compliance packages, and so lives in its own package to avoid import cycles.
package insights

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/docker-secret-operator/dso/internal/compliance"
	"github.com/docker-secret-operator/dso/internal/drift"
	"github.com/docker-secret-operator/dso/internal/policy"
	"github.com/docker-secret-operator/dso/internal/recommendation"
)

// Evaluator derives recommendations deterministically from live evidence.
// Every recommendation is explainable and disappears when the underlying
// problem is resolved — no stored state, no black boxes.
type Evaluator struct {
	complianceEngine *compliance.Engine
	driftStore       drift.Store
	policyStore      policy.RuleStore
}

// NewEvaluator creates a live recommendation evaluator.
func NewEvaluator(
	complianceEngine *compliance.Engine,
	driftStore drift.Store,
	policyStore policy.RuleStore,
) *Evaluator {
	return &Evaluator{
		complianceEngine: complianceEngine,
		driftStore:       driftStore,
		policyStore:      policyStore,
	}
}

// EvaluateAll returns all current recommendations derived from evidence.
// Results are sorted by priority descending (Critical → High → Medium → Low).
func (e *Evaluator) EvaluateAll(ctx context.Context, secrets []compliance.SecretInput) []*recommendation.Recommendation {
	var recs []*recommendation.Recommendation
	now := time.Now().UTC()

	// ── Per-secret compliance rules ───────────────────────────────────────────
	if e.complianceEngine != nil {
		records := e.complianceEngine.EvaluateAll(ctx, secrets)
		for _, c := range records {
			recs = append(recs, fromCompliance(c, now)...)
		}
	}

	// ── Open drift findings (cross-secret sweep) ──────────────────────────────
	if e.driftStore != nil {
		findings, err := e.driftStore.ListFindings(ctx)
		if err == nil {
			for _, f := range findings {
				if f.Status == drift.StatusDetected {
					recs = append(recs, driftRecommendation(f, now))
				}
			}
		}
	}

	// ── Disabled critical policy rules ────────────────────────────────────────
	if e.policyStore != nil {
		rules, err := e.policyStore.ListRules(ctx)
		if err == nil {
			for _, r := range rules {
				if !r.Enabled && r.Severity == policy.SeverityCritical {
					recs = append(recs, disabledCriticalPolicyRec(r, now))
				}
			}
		}
	}

	// Deduplicate by deterministic ID, then sort Critical → Low
	seen := make(map[string]bool, len(recs))
	unique := make([]*recommendation.Recommendation, 0, len(recs))
	for _, r := range recs {
		if !seen[r.ID] {
			seen[r.ID] = true
			unique = append(unique, r)
		}
	}
	sort.Slice(unique, func(i, j int) bool {
		return unique[i].Priority.Score() > unique[j].Priority.Score()
	})
	return unique
}

// ── Secret compliance rules ───────────────────────────────────────────────────

func fromCompliance(c compliance.SecretCompliance, now time.Time) []*recommendation.Recommendation {
	var recs []*recommendation.Recommendation

	switch c.RotationStatus {
	case compliance.RotationNeverRotated:
		recs = append(recs, &recommendation.Recommendation{
			ID:              fmt.Sprintf("rotation:never:%s", c.SecretName),
			Title:           fmt.Sprintf("Rotate %s", c.SecretName),
			Description:     "This secret has never been rotated.",
			Reason:          "No rotation history exists. There is no evidence this secret has ever changed.",
			Resource:        c.SecretName,
			Priority:        recommendation.PriorityHigh,
			Category:        recommendation.CategoryRotation,
			Status:          recommendation.StatusOpen,
			ResourceID:      c.SecretName,
			SuggestedAction: "Perform an initial rotation to establish a baseline version record.",
			Confidence:      1.0,
			CreatedAt:       now,
		})

	case compliance.RotationOverdue:
		recs = append(recs, &recommendation.Recommendation{
			ID:              fmt.Sprintf("rotation:overdue:%s", c.SecretName),
			Title:           fmt.Sprintf("Rotate overdue secret %s", c.SecretName),
			Description:     "This secret's scheduled rotation time has passed.",
			Reason:          "The next_rotation timestamp is in the past. Rotation SLA has been violated.",
			Resource:        c.SecretName,
			Priority:        recommendation.PriorityHigh,
			Category:        recommendation.CategoryRotation,
			Status:          recommendation.StatusOpen,
			ResourceID:      c.SecretName,
			SuggestedAction: "Schedule or execute a rotation immediately.",
			Confidence:      1.0,
			CreatedAt:       now,
		})
	}

	if c.OpenDriftFindings > 0 {
		recs = append(recs, &recommendation.Recommendation{
			ID:              fmt.Sprintf("drift:open:%s", c.SecretName),
			Title:           fmt.Sprintf("Investigate drift affecting %s", c.SecretName),
			Description:     fmt.Sprintf("%d open drift finding(s) detected on this secret.", c.OpenDriftFindings),
			Reason:          "Container version does not match provider version. The injected secret is stale.",
			Resource:        c.SecretName,
			Priority:        recommendation.PriorityHigh,
			Category:        recommendation.CategoryDrift,
			Status:          recommendation.StatusOpen,
			ResourceID:      c.SecretName,
			SuggestedAction: "Review drift findings and reinject affected containers.",
			Confidence:      1.0,
			CreatedAt:       now,
		})
	}

	if c.OverallStatus == compliance.StatusNonCompliant {
		recs = append(recs, &recommendation.Recommendation{
			ID:              fmt.Sprintf("compliance:noncompliant:%s", c.SecretName),
			Title:           fmt.Sprintf("Review non-compliant secret %s", c.SecretName),
			Description:     "This secret is non-compliant with current operational requirements.",
			Reason:          reasonFromCompliance(c),
			Resource:        c.SecretName,
			Priority:        recommendation.PriorityMedium,
			Category:        recommendation.CategoryCompliance,
			Status:          recommendation.StatusOpen,
			ResourceID:      c.SecretName,
			SuggestedAction: "Investigate the underlying rotation or drift issues and resolve them.",
			Confidence:      1.0,
			CreatedAt:       now,
		})
	}

	return recs
}

func reasonFromCompliance(c compliance.SecretCompliance) string {
	if c.RotationStatus == compliance.RotationNeverRotated {
		return "No rotation history exists."
	}
	if c.OpenDriftFindings > 0 {
		return fmt.Sprintf("%d open drift finding(s) detected.", c.OpenDriftFindings)
	}
	return "Secret does not meet compliance requirements."
}

// ── Drift rules ───────────────────────────────────────────────────────────────

func driftRecommendation(f drift.DriftFinding, now time.Time) *recommendation.Recommendation {
	priority := recommendation.PriorityMedium
	switch f.Severity {
	case drift.SeverityCritical, drift.SeverityHigh:
		priority = recommendation.PriorityCritical
	case drift.SeverityMedium:
		priority = recommendation.PriorityHigh
	}
	return &recommendation.Recommendation{
		ID:              fmt.Sprintf("drift:finding:%s", f.ID),
		Title:           fmt.Sprintf("Resolve drift finding on %s", f.Resource),
		Description:     f.Description,
		Reason:          fmt.Sprintf("Drift type %q detected on %s (severity: %s).", f.Type, f.Resource, f.Severity),
		Resource:        f.Resource,
		Priority:        priority,
		Category:        recommendation.CategoryDrift,
		Status:          recommendation.StatusOpen,
		ResourceID:      f.Resource,
		DriftID:         f.ID,
		SuggestedAction: "Acknowledge the finding, reinject the container, and verify the drift resolves.",
		Confidence:      1.0,
		CreatedAt:       now,
	}
}

// ── Policy rules ──────────────────────────────────────────────────────────────

func disabledCriticalPolicyRec(r *policy.Rule, now time.Time) *recommendation.Recommendation {
	return &recommendation.Recommendation{
		ID:              fmt.Sprintf("policy:disabled:%s", r.ID),
		Title:           fmt.Sprintf("Re-enable critical policy: %s", r.Name),
		Description:     "A critical policy rule is currently disabled.",
		Reason:          fmt.Sprintf("Policy rule %q (severity: critical) is disabled. Critical controls must remain active.", r.Name),
		Resource:        r.Name,
		Priority:        recommendation.PriorityCritical,
		Category:        recommendation.CategoryPolicy,
		Status:          recommendation.StatusOpen,
		ResourceID:      r.ID,
		PolicyID:        r.ID,
		SuggestedAction: "Enable the rule or document the exception with an audit event.",
		Confidence:      1.0,
		CreatedAt:       now,
	}
}
