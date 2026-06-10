package sqlite

var migration0020statements = []string{
	`CREATE TABLE IF NOT EXISTS integration_configs (
		plugin_id TEXT PRIMARY KEY,
		enabled INTEGER NOT NULL DEFAULT 1,
		endpoint TEXT,
		auth_type TEXT,
		auth_config_json TEXT,
		filters_json TEXT,
		retry_policy_json TEXT,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`,
	`CREATE INDEX IF NOT EXISTS idx_integration_configs_enabled ON integration_configs(enabled)`,
	`CREATE TABLE IF NOT EXISTS integration_deliveries (
		id TEXT PRIMARY KEY,
		plugin_id TEXT NOT NULL,
		event_type TEXT NOT NULL,
		event_id TEXT,
		success INTEGER NOT NULL,
		response_code INTEGER,
		error_message TEXT,
		attempt INTEGER NOT NULL DEFAULT 1,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`,
	`CREATE INDEX IF NOT EXISTS idx_integration_deliveries_plugin_id ON integration_deliveries(plugin_id)`,
	`CREATE INDEX IF NOT EXISTS idx_integration_deliveries_event_id ON integration_deliveries(event_id)`,
	`CREATE INDEX IF NOT EXISTS idx_integration_deliveries_created_at ON integration_deliveries(created_at)`,
}
