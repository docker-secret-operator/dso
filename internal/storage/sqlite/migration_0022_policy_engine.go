package sqlite

var migration0022statements = []string{
	`CREATE TABLE IF NOT EXISTS rules (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT,
		enabled INTEGER NOT NULL DEFAULT 1,
		severity TEXT NOT NULL,
		trigger TEXT NOT NULL,
		schedule TEXT,
		event_type TEXT,
		condition_json TEXT NOT NULL,
		actions_json TEXT NOT NULL,
		last_run TIMESTAMP,
		last_result TEXT,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`,
	`CREATE INDEX IF NOT EXISTS idx_rules_enabled ON rules(enabled)`,
	`CREATE INDEX IF NOT EXISTS idx_rules_severity ON rules(severity)`,
	`CREATE INDEX IF NOT EXISTS idx_rules_trigger ON rules(trigger)`,
	`CREATE TABLE IF NOT EXISTS rule_executions (
		id TEXT PRIMARY KEY,
		rule_id TEXT NOT NULL,
		success INTEGER NOT NULL,
		duration_ms INTEGER,
		error_message TEXT,
		result TEXT,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(rule_id) REFERENCES rules(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_rule_executions_rule_id ON rule_executions(rule_id)`,
	`CREATE INDEX IF NOT EXISTS idx_rule_executions_created_at ON rule_executions(created_at)`,
}
