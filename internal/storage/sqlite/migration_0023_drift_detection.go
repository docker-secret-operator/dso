package sqlite

var migration0023statements = []string{
	`CREATE TABLE IF NOT EXISTS drift_findings (
		id TEXT PRIMARY KEY,
		type TEXT NOT NULL,
		severity TEXT NOT NULL,
		status TEXT NOT NULL,
		resource TEXT NOT NULL,
		description TEXT,
		metadata_json TEXT,
		detected_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		acknowledged_at TIMESTAMP,
		resolved_at TIMESTAMP
	)`,
	`CREATE INDEX IF NOT EXISTS idx_drift_findings_type ON drift_findings(type)`,
	`CREATE INDEX IF NOT EXISTS idx_drift_findings_severity ON drift_findings(severity)`,
	`CREATE INDEX IF NOT EXISTS idx_drift_findings_status ON drift_findings(status)`,
	`CREATE INDEX IF NOT EXISTS idx_drift_findings_detected_at ON drift_findings(detected_at)`,
	`CREATE TABLE IF NOT EXISTS drift_scans (
		id TEXT PRIMARY KEY,
		detector_id TEXT NOT NULL,
		findings_count INTEGER NOT NULL DEFAULT 0,
		duration_ms INTEGER,
		success INTEGER NOT NULL DEFAULT 1,
		error_message TEXT,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`,
	`CREATE INDEX IF NOT EXISTS idx_drift_scans_detector_id ON drift_scans(detector_id)`,
	`CREATE INDEX IF NOT EXISTS idx_drift_scans_created_at ON drift_scans(created_at)`,
}
