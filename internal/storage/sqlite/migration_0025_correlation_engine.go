package sqlite

var migration0025statements = []string{
	`CREATE TABLE IF NOT EXISTS incidents (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		severity TEXT NOT NULL,
		status TEXT NOT NULL,
		root_cause TEXT,
		correlation_score REAL,
		first_seen DATETIME NOT NULL,
		last_seen DATETIME NOT NULL,
		acknowledged_at DATETIME,
		resolved_at DATETIME,
		metadata_json TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`,
	`CREATE INDEX IF NOT EXISTS idx_incidents_severity ON incidents(severity)`,
	`CREATE INDEX IF NOT EXISTS idx_incidents_status ON incidents(status)`,
	`CREATE INDEX IF NOT EXISTS idx_incidents_first_seen ON incidents(first_seen)`,
	`CREATE INDEX IF NOT EXISTS idx_incidents_last_seen ON incidents(last_seen)`,
	`CREATE TABLE IF NOT EXISTS incident_events (
		id TEXT PRIMARY KEY,
		incident_id TEXT NOT NULL,
		event_id TEXT NOT NULL,
		event_type TEXT NOT NULL,
		event_data TEXT,
		correlation_key TEXT,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (incident_id) REFERENCES incidents(id) ON DELETE CASCADE,
		UNIQUE(incident_id, event_id)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_incident_events_incident_id ON incident_events(incident_id)`,
	`CREATE INDEX IF NOT EXISTS idx_incident_events_event_id ON incident_events(event_id)`,
	`CREATE INDEX IF NOT EXISTS idx_incident_events_event_type ON incident_events(event_type)`,
}
