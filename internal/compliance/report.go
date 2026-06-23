package compliance

import (
	"context"
	"time"

	"github.com/docker-secret-operator/dso/internal/drift"
	"github.com/docker-secret-operator/dso/internal/policy"
	"github.com/docker-secret-operator/dso/internal/storage"
	"github.com/docker-secret-operator/dso/internal/storage/sqlite"
)

// RotationReportRow is one row of the rotation report.
// Never contains secret values.
type RotationReportRow struct {
	SecretName     string    `json:"secretName"     csv:"secret_name"`
	Version        int       `json:"version"        csv:"version"`
	RotatedAt      time.Time `json:"rotatedAt"      csv:"rotated_at"`
	RotatedBy      string    `json:"rotatedBy"      csv:"rotated_by"`
	RotationSource string    `json:"rotationSource" csv:"rotation_source"`
	Provider       string    `json:"provider"       csv:"provider"`
}

// DriftReportRow is one row of the drift report.
type DriftReportRow struct {
	ID          string    `json:"id"          csv:"id"`
	Resource    string    `json:"resource"    csv:"resource"`
	Type        string    `json:"type"        csv:"type"`
	Severity    string    `json:"severity"    csv:"severity"`
	Status      string    `json:"status"      csv:"status"`
	DetectedAt  time.Time `json:"detectedAt"  csv:"detected_at"`
	Description string    `json:"description" csv:"description"`
}

// PolicyReportRow is one row of the policy report.
type PolicyReportRow struct {
	ID          string     `json:"id"          csv:"id"`
	Name        string     `json:"name"        csv:"name"`
	Enabled     bool       `json:"enabled"     csv:"enabled"`
	Severity    string     `json:"severity"    csv:"severity"`
	LastRun     *time.Time `json:"lastRun"     csv:"last_run"`
	LastResult  string     `json:"lastResult"  csv:"last_result"`
}

// ActivityReportRow is one row of the activity report.
type ActivityReportRow struct {
	ID          string    `json:"id"          csv:"id"`
	Action      string    `json:"action"      csv:"action"`
	Actor       string    `json:"actor"       csv:"actor"`
	ResourceID  string    `json:"resourceId"  csv:"resource_id"`
	ExecutionID string    `json:"executionId" csv:"execution_id"`
	Timestamp   time.Time `json:"timestamp"   csv:"timestamp"`
	Status      string    `json:"status"      csv:"status"`
}

// Reporter generates compliance reports from stores.
type Reporter struct {
	versions   *sqlite.SecretVersionStore
	driftStore drift.Store
	policyStore policy.RuleStore
	auditStore storage.AuditStore
}

// NewReporter creates a reporter.
func NewReporter(
	versions *sqlite.SecretVersionStore,
	driftStore drift.Store,
	policyStore policy.RuleStore,
	auditStore storage.AuditStore,
) *Reporter {
	return &Reporter{
		versions:    versions,
		driftStore:  driftStore,
		policyStore: policyStore,
		auditStore:  auditStore,
	}
}

// RotationReport returns all recorded rotation events, newest first.
func (r *Reporter) RotationReport(ctx context.Context, secretNames []string) ([]RotationReportRow, error) {
	if r.versions == nil {
		return []RotationReportRow{}, nil
	}
	var rows []RotationReportRow
	for _, name := range secretNames {
		vs, err := r.versions.ListBySecret(ctx, name)
		if err != nil {
			continue
		}
		for _, v := range vs {
			rows = append(rows, RotationReportRow{
				SecretName:     v.SecretName,
				Version:        v.Version,
				RotatedAt:      v.CreatedAt,
				RotatedBy:      v.RotatedBy,
				RotationSource: v.RotationSource,
				Provider:       v.Provider,
			})
		}
	}
	return rows, nil
}

// DriftReport returns all findings, optionally filtered by status.
func (r *Reporter) DriftReport(ctx context.Context) ([]DriftReportRow, error) {
	if r.driftStore == nil {
		return []DriftReportRow{}, nil
	}
	findings, err := r.driftStore.ListFindings(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]DriftReportRow, 0, len(findings))
	for _, f := range findings {
		rows = append(rows, DriftReportRow{
			ID:          f.ID,
			Resource:    f.Resource,
			Type:        string(f.Type),
			Severity:    string(f.Severity),
			Status:      string(f.Status),
			DetectedAt:  f.DetectedAt,
			Description: f.Description,
		})
	}
	return rows, nil
}

// PolicyReport returns all rules with their current state.
func (r *Reporter) PolicyReport(ctx context.Context) ([]PolicyReportRow, error) {
	if r.policyStore == nil {
		return []PolicyReportRow{}, nil
	}
	rules, err := r.policyStore.ListRules(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]PolicyReportRow, 0, len(rules))
	for _, rule := range rules {
		row := PolicyReportRow{
			ID:       rule.ID,
			Name:     rule.Name,
			Enabled:  rule.Enabled,
			Severity: string(rule.Severity),
			LastRun:  rule.LastRun,
		}
		if rule.LastResult != "" {
			row.LastResult = string(rule.LastResult)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// ActivityReport returns audit events for the given resource IDs (or all if empty).
func (r *Reporter) ActivityReport(ctx context.Context, resourceIDs []string) ([]ActivityReportRow, error) {
	if r.auditStore == nil {
		return []ActivityReportRow{}, nil
	}
	var rows []ActivityReportRow
	if len(resourceIDs) == 0 {
		events, err := r.auditStore.Query(ctx, map[string]interface{}{})
		if err != nil {
			return nil, err
		}
		for _, e := range events {
			rows = append(rows, activityRow(e))
		}
	} else {
		for _, rid := range resourceIDs {
			events, err := r.auditStore.Query(ctx, map[string]interface{}{"resource_id": rid})
			if err != nil {
				continue
			}
			for _, e := range events {
				rows = append(rows, activityRow(e))
			}
		}
	}
	return rows, nil
}

func activityRow(e *storage.AuditEvent) ActivityReportRow {
	row := ActivityReportRow{
		ID:         e.ID,
		Action:     e.Action,
		Actor:      e.ActorName,
		ResourceID: e.ResourceID,
		Timestamp:  e.Timestamp,
		Status:     e.Status,
	}
	// ExecutionID is stored in CorrelationID for audit events originating from executions.
	row.ExecutionID = e.CorrelationID
	return row
}
