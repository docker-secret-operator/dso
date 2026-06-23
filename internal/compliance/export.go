package compliance

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// ComplianceExportRow is one row of the flat compliance export.
// Never contains secret values.
type ComplianceExportRow struct {
	Secret           string     `json:"secret"           csv:"secret"`
	Provider         string     `json:"provider"         csv:"provider"`
	RotationStatus   string     `json:"rotationStatus"   csv:"rotation_status"`
	Version          int        `json:"version"          csv:"version"`
	OpenDrift        int        `json:"openDrift"        csv:"open_drift"`
	LastRotation     *time.Time `json:"lastRotation"     csv:"last_rotation"`
	ComplianceStatus string     `json:"complianceStatus" csv:"compliance_status"`
}

// ToExportRows converts evaluated compliance records to export rows.
func ToExportRows(records []SecretCompliance) []ComplianceExportRow {
	rows := make([]ComplianceExportRow, 0, len(records))
	for _, c := range records {
		rows = append(rows, ComplianceExportRow{
			Secret:           c.SecretName,
			Provider:         c.Provider,
			RotationStatus:   string(c.RotationStatus),
			Version:          c.VersionCount,
			OpenDrift:        c.OpenDriftFindings,
			LastRotation:     c.LastRotatedAt,
			ComplianceStatus: string(c.OverallStatus),
		})
	}
	return rows
}

// WriteJSON marshals rows as a JSON array into w.
func WriteJSON(w io.Writer, v interface{}) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// WriteComplianceCSV writes compliance export rows as CSV.
func WriteComplianceCSV(w io.Writer, rows []ComplianceExportRow) error {
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"secret", "provider", "rotation_status", "version", "open_drift", "last_rotation", "compliance_status"})
	for _, r := range rows {
		lastRot := ""
		if r.LastRotation != nil {
			lastRot = r.LastRotation.UTC().Format(time.RFC3339)
		}
		_ = cw.Write([]string{
			r.Secret,
			r.Provider,
			r.RotationStatus,
			fmt.Sprintf("%d", r.Version),
			fmt.Sprintf("%d", r.OpenDrift),
			lastRot,
			r.ComplianceStatus,
		})
	}
	cw.Flush()
	return cw.Error()
}

// WriteRotationCSV writes rotation report rows as CSV.
func WriteRotationCSV(w io.Writer, rows []RotationReportRow) error {
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"secret_name", "version", "rotated_at", "rotated_by", "rotation_source", "provider"})
	for _, r := range rows {
		_ = cw.Write([]string{
			r.SecretName,
			fmt.Sprintf("%d", r.Version),
			r.RotatedAt.UTC().Format(time.RFC3339),
			r.RotatedBy,
			r.RotationSource,
			r.Provider,
		})
	}
	cw.Flush()
	return cw.Error()
}

// WriteDriftCSV writes drift report rows as CSV.
func WriteDriftCSV(w io.Writer, rows []DriftReportRow) error {
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "resource", "type", "severity", "status", "detected_at", "description"})
	for _, r := range rows {
		_ = cw.Write([]string{
			r.ID, r.Resource, r.Type, r.Severity, r.Status,
			r.DetectedAt.UTC().Format(time.RFC3339),
			r.Description,
		})
	}
	cw.Flush()
	return cw.Error()
}

// WritePolicyCSV writes policy report rows as CSV.
func WritePolicyCSV(w io.Writer, rows []PolicyReportRow) error {
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "name", "enabled", "severity", "last_run", "last_result"})
	for _, r := range rows {
		lastRun := ""
		if r.LastRun != nil {
			lastRun = r.LastRun.UTC().Format(time.RFC3339)
		}
		enabled := "false"
		if r.Enabled {
			enabled = "true"
		}
		_ = cw.Write([]string{r.ID, r.Name, enabled, r.Severity, lastRun, r.LastResult})
	}
	cw.Flush()
	return cw.Error()
}

// WriteActivityCSV writes activity report rows as CSV.
func WriteActivityCSV(w io.Writer, rows []ActivityReportRow) error {
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "action", "actor", "resource_id", "execution_id", "timestamp", "status"})
	for _, r := range rows {
		_ = cw.Write([]string{
			r.ID, r.Action, r.Actor, r.ResourceID,
			r.ExecutionID,
			r.Timestamp.UTC().Format(time.RFC3339),
			r.Status,
		})
	}
	cw.Flush()
	return cw.Error()
}

// BufferedJSON encodes v to a byte slice.
func BufferedJSON(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
