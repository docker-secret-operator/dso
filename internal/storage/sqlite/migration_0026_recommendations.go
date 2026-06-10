package sqlite

var migration0026statements = []string{
	`CREATE TABLE IF NOT EXISTS recommendations (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		description TEXT,
		priority TEXT NOT NULL,
		category TEXT NOT NULL,
		status TEXT NOT NULL,
		resource_id TEXT,
		incident_id TEXT,
		suggested_action TEXT,
		confidence REAL,
		created_at DATETIME NOT NULL,
		acknowledged_at DATETIME,
		implemented_at DATETIME,
		dismissed_at DATETIME,
		metadata_json TEXT,
		FOREIGN KEY (incident_id) REFERENCES incidents(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_recommendations_priority ON recommendations(priority)`,
	`CREATE INDEX IF NOT EXISTS idx_recommendations_status ON recommendations(status)`,
	`CREATE INDEX IF NOT EXISTS idx_recommendations_category ON recommendations(category)`,
	`CREATE INDEX IF NOT EXISTS idx_recommendations_resource_id ON recommendations(resource_id)`,
	`CREATE INDEX IF NOT EXISTS idx_recommendations_incident_id ON recommendations(incident_id)`,
	`CREATE INDEX IF NOT EXISTS idx_recommendations_created_at ON recommendations(created_at)`,
}
