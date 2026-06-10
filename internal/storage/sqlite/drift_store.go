package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/drift"
)

// DriftStore implements drift.Store
type DriftStore struct {
	db interface {
		QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
		QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
		ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	}
}

// CreateFinding creates a new drift finding
func (s *DriftStore) CreateFinding(ctx context.Context, finding interface{}) error {
	f, ok := finding.(drift.DriftFinding)
	if !ok {
		return fmt.Errorf("invalid finding type")
	}

	metadataJSON, err := json.Marshal(f.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO drift_findings (id, type, severity, status, resource, description, metadata_json, detected_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.Type, f.Severity, f.Status, f.Resource,
		f.Description, metadataJSON, f.DetectedAt)

	return err
}

// UpdateFinding updates a drift finding
func (s *DriftStore) UpdateFinding(ctx context.Context, finding interface{}) error {
	f, ok := finding.(drift.DriftFinding)
	if !ok {
		return fmt.Errorf("invalid finding type")
	}

	metadataJSON, err := json.Marshal(f.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	var acknowledgedAt interface{} = nil
	if f.AcknowledgedAt != nil {
		acknowledgedAt = *f.AcknowledgedAt
	}

	var resolvedAt interface{} = nil
	if f.ResolvedAt != nil {
		resolvedAt = *f.ResolvedAt
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE drift_findings SET type=?, severity=?, status=?, resource=?, description=?,
		 metadata_json=?, detected_at=?, acknowledged_at=?, resolved_at=?
		 WHERE id=?`,
		f.Type, f.Severity, f.Status, f.Resource,
		f.Description, metadataJSON, f.DetectedAt, acknowledgedAt, resolvedAt, f.ID)

	return err
}

// GetFinding retrieves a finding by ID
func (s *DriftStore) GetFinding(ctx context.Context, id string) (interface{}, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, type, severity, status, resource, description, metadata_json, detected_at, acknowledged_at, resolved_at
		 FROM drift_findings WHERE id=?`, id)

	finding := &drift.DriftFinding{}
	var metadataJSON []byte
	var acknowledgedAt, resolvedAt sql.NullTime

	err := row.Scan(&finding.ID, &finding.Type, &finding.Severity, &finding.Status, &finding.Resource,
		&finding.Description, &metadataJSON, &finding.DetectedAt, &acknowledgedAt, &resolvedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("finding not found")
		}
		return nil, fmt.Errorf("failed to scan finding: %w", err)
	}

	if err := json.Unmarshal(metadataJSON, &finding.Metadata); err != nil {
		finding.Metadata = make(map[string]interface{})
	}

	if acknowledgedAt.Valid {
		finding.AcknowledgedAt = &acknowledgedAt.Time
	}

	if resolvedAt.Valid {
		finding.ResolvedAt = &resolvedAt.Time
	}

	return finding, nil
}

// ListFindings lists all findings
func (s *DriftStore) ListFindings(ctx context.Context) ([]interface{}, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, type, severity, status, resource, description, metadata_json, detected_at, acknowledged_at, resolved_at
		 FROM drift_findings ORDER BY detected_at DESC`)

	if err != nil {
		return nil, fmt.Errorf("failed to query findings: %w", err)
	}
	defer rows.Close()

	var findings []interface{}
	for rows.Next() {
		finding := drift.DriftFinding{}
		var metadataJSON []byte
		var acknowledgedAt, resolvedAt sql.NullTime

		if err := rows.Scan(&finding.ID, &finding.Type, &finding.Severity, &finding.Status, &finding.Resource,
			&finding.Description, &metadataJSON, &finding.DetectedAt, &acknowledgedAt, &resolvedAt); err != nil {
			return nil, fmt.Errorf("failed to scan finding: %w", err)
		}

		if err := json.Unmarshal(metadataJSON, &finding.Metadata); err != nil {
			finding.Metadata = make(map[string]interface{})
		}

		if acknowledgedAt.Valid {
			finding.AcknowledgedAt = &acknowledgedAt.Time
		}

		if resolvedAt.Valid {
			finding.ResolvedAt = &resolvedAt.Time
		}

		findings = append(findings, finding)
	}

	return findings, rows.Err()
}

// DeleteFinding deletes a finding
func (s *DriftStore) DeleteFinding(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM drift_findings WHERE id=?", id)
	return err
}

// LogScan logs a drift scan
func (s *DriftStore) LogScan(ctx context.Context, scan *drift.DriftScan) error {
	durationMs := int64(scan.Duration.Milliseconds())
	var errorMsg interface{} = nil
	if scan.Error != "" {
		errorMsg = scan.Error
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO drift_scans (id, detector_id, findings_count, duration_ms, success, error_message, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		scan.ID, scan.DetectorID, scan.FindingsCount, durationMs, scan.Success, errorMsg, scan.CreatedAt)

	return err
}

// GetScans retrieves scan history
func (s *DriftStore) GetScans(ctx context.Context, limit int) ([]*drift.DriftScan, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, detector_id, findings_count, duration_ms, success, error_message, created_at
		 FROM drift_scans ORDER BY created_at DESC LIMIT ?`, limit)

	if err != nil {
		return nil, fmt.Errorf("failed to query scans: %w", err)
	}
	defer rows.Close()

	var scans []*drift.DriftScan
	for rows.Next() {
		scan := &drift.DriftScan{}
		var durationMs int64
		var errorMsg sql.NullString

		if err := rows.Scan(&scan.ID, &scan.DetectorID, &scan.FindingsCount, &durationMs,
			&scan.Success, &errorMsg, &scan.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan: %w", err)
		}

		scan.Duration = time.Duration(durationMs) * time.Millisecond
		if errorMsg.Valid {
			scan.Error = errorMsg.String
		}

		scans = append(scans, scan)
	}

	return scans, rows.Err()
}

// CleanupOldFindings removes old findings
func (s *DriftStore) CleanupOldFindings(ctx context.Context, olderThan time.Time) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM drift_findings WHERE detected_at < ?", olderThan)
	return err
}
