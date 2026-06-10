package sqlite

var migration0027statements = []string{
	`CREATE TABLE IF NOT EXISTS forecasts (
		id TEXT PRIMARY KEY,
		resource_type TEXT NOT NULL,
		resource_id TEXT NOT NULL,
		metric TEXT NOT NULL,
		current_value REAL,
		predicted_value REAL,
		growth_rate REAL,
		confidence REAL,
		horizon TEXT NOT NULL,
		severity TEXT NOT NULL,
		trend TEXT,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL,
		metadata_json TEXT
	)`,
	`CREATE INDEX IF NOT EXISTS idx_forecasts_resource_type ON forecasts(resource_type)`,
	`CREATE INDEX IF NOT EXISTS idx_forecasts_severity ON forecasts(severity)`,
	`CREATE INDEX IF NOT EXISTS idx_forecasts_horizon ON forecasts(horizon)`,
	`CREATE INDEX IF NOT EXISTS idx_forecasts_created_at ON forecasts(created_at)`,
}
