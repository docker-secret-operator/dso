// Package compliance derives secret compliance status from existing stores.
// It does NOT persist state — compliance is a live view over version history,
// drift findings, and audit events.
package compliance

import (
	"context"
	"time"

	"github.com/docker-secret-operator/dso/internal/drift"
	"github.com/docker-secret-operator/dso/internal/storage"
	"github.com/docker-secret-operator/dso/internal/storage/sqlite"
)

// RotationStatus describes the rotation posture of a single secret.
type RotationStatus string

const (
	// RotationCompliant — at least one version entry exists and the secret is not overdue.
	RotationCompliant RotationStatus = "compliant"
	// RotationOverdue — version history exists but next_rotation timestamp has passed.
	RotationOverdue RotationStatus = "overdue"
	// RotationNeverRotated — no version entries in history; no evidence of rotation.
	RotationNeverRotated RotationStatus = "never_rotated"
	// RotationUnknown — version store is unavailable; status cannot be determined.
	RotationUnknown RotationStatus = "unknown"
)

// OverallStatus is the rolled-up compliance verdict for a secret.
type OverallStatus string

const (
	StatusCompliant    OverallStatus = "compliant"
	StatusWarning      OverallStatus = "warning"
	StatusNonCompliant OverallStatus = "non_compliant"
)

// SecretCompliance is the computed compliance record for one secret.
// It is derived at query time — never persisted.
type SecretCompliance struct {
	SecretName    string
	Provider      string
	RotationStatus RotationStatus
	DriftFree     bool
	LastRotatedAt *time.Time
	VersionCount  int
	OpenDriftFindings int
	AuditEventCount   int
	OverallStatus OverallStatus
}

// SecretInput carries the lightweight metadata the engine needs per secret.
// Callers build this from the config + secret cache; the engine does not
// talk to the cache directly so it stays testable.
type SecretInput struct {
	Name         string
	Provider     string
	NextRotation *time.Time // nil when not scheduled
}

// Engine derives compliance from stores. All stores are optional; nil values
// cause the related dimension to return conservative defaults.
type Engine struct {
	versions   *sqlite.SecretVersionStore
	driftStore drift.Store
	auditStore storage.AuditStore
}

// NewEngine creates a compliance engine.
func NewEngine(
	versions *sqlite.SecretVersionStore,
	driftStore drift.Store,
	auditStore storage.AuditStore,
) *Engine {
	return &Engine{
		versions:   versions,
		driftStore: driftStore,
		auditStore: auditStore,
	}
}

// Evaluate derives compliance for a single secret.
func (e *Engine) Evaluate(ctx context.Context, s SecretInput) SecretCompliance {
	c := SecretCompliance{
		SecretName:     s.Name,
		Provider:       s.Provider,
		RotationStatus: RotationUnknown,
		DriftFree:      true,
	}

	// ── Rotation dimension ────────────────────────────────────────────────────
	if e.versions != nil {
		vs, err := e.versions.ListBySecret(ctx, s.Name)
		if err == nil {
			c.VersionCount = len(vs)
			if len(vs) == 0 {
				c.RotationStatus = RotationNeverRotated
			} else {
				// newest first (store returns DESC)
				c.LastRotatedAt = &vs[0].CreatedAt
				if s.NextRotation != nil && time.Now().After(*s.NextRotation) {
					c.RotationStatus = RotationOverdue
				} else {
					c.RotationStatus = RotationCompliant
				}
			}
		}
	}

	// ── Drift dimension ───────────────────────────────────────────────────────
	if e.driftStore != nil {
		findings, err := e.driftStore.ListFindings(ctx)
		if err == nil {
			for _, f := range findings {
				if f.Resource == s.Name && f.Status == drift.StatusDetected {
					c.OpenDriftFindings++
				}
			}
		}
	}
	c.DriftFree = c.OpenDriftFindings == 0

	// ── Audit evidence count ─────────────────────────────────────────────────
	if e.auditStore != nil {
		events, err := e.auditStore.Query(ctx, map[string]interface{}{"resource_id": s.Name})
		if err == nil {
			c.AuditEventCount = len(events)
		}
	}

	// ── Overall status ────────────────────────────────────────────────────────
	c.OverallStatus = deriveOverall(c)
	return c
}

// EvaluateAll evaluates compliance for every provided secret.
func (e *Engine) EvaluateAll(ctx context.Context, secrets []SecretInput) []SecretCompliance {
	out := make([]SecretCompliance, 0, len(secrets))
	for _, s := range secrets {
		out = append(out, e.Evaluate(ctx, s))
	}
	return out
}

func deriveOverall(c SecretCompliance) OverallStatus {
	if c.RotationStatus == RotationNonCompliantStatus() || !c.DriftFree {
		return StatusNonCompliant
	}
	if c.RotationStatus == RotationUnknown || c.RotationStatus == RotationOverdue {
		return StatusWarning
	}
	if c.RotationStatus == RotationNeverRotated {
		return StatusNonCompliant
	}
	return StatusCompliant
}

// RotationNonCompliantStatus returns the rotation statuses that cause non-compliance.
// Extracted as a function so callers can check without importing constants twice.
func RotationNonCompliantStatus() RotationStatus {
	return RotationNeverRotated
}

// ComplianceSummary aggregates counts across all evaluated secrets.
type ComplianceSummary struct {
	TotalSecrets  int `json:"totalSecrets"`
	Compliant     int `json:"compliant"`
	Warning       int `json:"warning"`
	NonCompliant  int `json:"nonCompliant"`
}

// Summarize builds aggregate counts from a slice of evaluated records.
func Summarize(records []SecretCompliance) ComplianceSummary {
	s := ComplianceSummary{TotalSecrets: len(records)}
	for _, r := range records {
		switch r.OverallStatus {
		case StatusCompliant:
			s.Compliant++
		case StatusWarning:
			s.Warning++
		case StatusNonCompliant:
			s.NonCompliant++
		}
	}
	return s
}
