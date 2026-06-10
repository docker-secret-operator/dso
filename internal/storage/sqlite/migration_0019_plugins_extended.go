package sqlite

var migration0019statements = []string{
	`CREATE TABLE IF NOT EXISTS plugin_configs (
		plugin_id TEXT PRIMARY KEY,
		config_json TEXT,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(plugin_id) REFERENCES plugins(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_plugin_configs_updated_at ON plugin_configs(updated_at)`,
	`CREATE TABLE IF NOT EXISTS plugin_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		plugin_id TEXT NOT NULL,
		level TEXT NOT NULL,
		message TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(plugin_id) REFERENCES plugins(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_plugin_events_plugin_id ON plugin_events(plugin_id)`,
	`CREATE INDEX IF NOT EXISTS idx_plugin_events_created_at ON plugin_events(created_at)`,
	`ALTER TABLE plugins ADD COLUMN enabled_at TIMESTAMP`,
	`ALTER TABLE plugins ADD COLUMN disabled_at TIMESTAMP`,
	`ALTER TABLE plugins ADD COLUMN restart_count INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE plugins ADD COLUMN event_count INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE plugins ADD COLUMN last_error_time TIMESTAMP`,
	`ALTER TABLE plugins ADD COLUMN last_heartbeat TIMESTAMP`,
	`ALTER TABLE plugins ADD COLUMN health TEXT DEFAULT 'healthy'`,
}
