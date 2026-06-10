package sqlite

var migration0018statements = []string{
	`CREATE TABLE IF NOT EXISTS plugins (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		version TEXT NOT NULL,
		type TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		status TEXT NOT NULL,
		error_message TEXT,
		loaded_at TIMESTAMP
	)`,
	`CREATE INDEX idx_plugins_type ON plugins(type)`,
	`CREATE INDEX idx_plugins_enabled ON plugins(enabled)`,
}
