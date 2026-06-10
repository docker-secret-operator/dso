package sqlite

var migration0015statements = []string{
	`CREATE TABLE IF NOT EXISTS security_events (
		id TEXT PRIMARY KEY,
		type TEXT NOT NULL,
		severity TEXT NOT NULL,
		username TEXT NOT NULL,
		user_id TEXT,
		ip_address TEXT NOT NULL,
		user_agent TEXT,
		message TEXT NOT NULL,
		metadata TEXT,
		created_at TIMESTAMP NOT NULL,
		FOREIGN KEY (user_id) REFERENCES users(id)
	)`,
	`CREATE INDEX idx_security_events_type ON security_events(type)`,
	`CREATE INDEX idx_security_events_severity ON security_events(severity)`,
	`CREATE INDEX idx_security_events_username ON security_events(username)`,
	`CREATE INDEX idx_security_events_ip_address ON security_events(ip_address)`,
	`CREATE INDEX idx_security_events_created_at ON security_events(created_at)`,

	`CREATE TABLE IF NOT EXISTS suspicious_activities (
		id TEXT PRIMARY KEY,
		type TEXT NOT NULL,
		severity TEXT NOT NULL,
		ip_address TEXT,
		usernames TEXT,
		first_seen TIMESTAMP NOT NULL,
		last_seen TIMESTAMP NOT NULL,
		occurrence_count INTEGER NOT NULL DEFAULT 1,
		message TEXT NOT NULL,
		metadata TEXT,
		acknowledged_by TEXT,
		acknowledged_at TIMESTAMP,
		ignored_at TIMESTAMP,
		created_at TIMESTAMP NOT NULL
	)`,
	`CREATE INDEX idx_suspicious_activities_type ON suspicious_activities(type)`,
	`CREATE INDEX idx_suspicious_activities_severity ON suspicious_activities(severity)`,
	`CREATE INDEX idx_suspicious_activities_ip_address ON suspicious_activities(ip_address)`,
	`CREATE INDEX idx_suspicious_activities_created_at ON suspicious_activities(created_at)`,

	`CREATE TABLE IF NOT EXISTS security_alerts (
		id TEXT PRIMARY KEY,
		type TEXT NOT NULL,
		severity TEXT NOT NULL,
		state TEXT NOT NULL,
		title TEXT NOT NULL,
		message TEXT NOT NULL,
		affected_user TEXT,
		ip_address TEXT,
		details TEXT,
		acknowledged_by TEXT,
		acknowledged_at TIMESTAMP,
		resolved_at TIMESTAMP,
		created_at TIMESTAMP NOT NULL
	)`,
	`CREATE INDEX idx_security_alerts_state ON security_alerts(state)`,
	`CREATE INDEX idx_security_alerts_severity ON security_alerts(severity)`,
	`CREATE INDEX idx_security_alerts_type ON security_alerts(type)`,
	`CREATE INDEX idx_security_alerts_created_at ON security_alerts(created_at)`,
}
