package sqlite

var migration0016statements = []string{
	`CREATE TABLE IF NOT EXISTS alert_rules (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT,
		enabled INTEGER NOT NULL DEFAULT 1,
		severity TEXT NOT NULL,
		metric TEXT NOT NULL,
		operator TEXT NOT NULL,
		threshold REAL NOT NULL,
		duration_seconds INTEGER NOT NULL DEFAULT 60,
		cooldown_seconds INTEGER NOT NULL DEFAULT 300,
		is_builtin INTEGER NOT NULL DEFAULT 0,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL
	)`,
	`CREATE INDEX idx_alert_rules_enabled ON alert_rules(enabled)`,
	`CREATE INDEX idx_alert_rules_metric ON alert_rules(metric)`,

	`CREATE TABLE IF NOT EXISTS alerts (
		id TEXT PRIMARY KEY,
		rule_id TEXT NOT NULL,
		state TEXT NOT NULL,
		severity TEXT NOT NULL,
		metric TEXT NOT NULL,
		message TEXT NOT NULL,
		value REAL NOT NULL,
		threshold REAL NOT NULL,
		acknowledged_by TEXT,
		acknowledged_at TIMESTAMP,
		resolved_by TEXT,
		resolved_at TIMESTAMP,
		suppressed_by TEXT,
		suppressed_until TIMESTAMP,
		last_fired_at TIMESTAMP NOT NULL,
		created_at TIMESTAMP NOT NULL,
		FOREIGN KEY (rule_id) REFERENCES alert_rules(id)
	)`,
	`CREATE INDEX idx_alerts_state ON alerts(state)`,
	`CREATE INDEX idx_alerts_severity ON alerts(severity)`,
	`CREATE INDEX idx_alerts_rule_id ON alerts(rule_id)`,
	`CREATE INDEX idx_alerts_created_at ON alerts(created_at)`,
}
