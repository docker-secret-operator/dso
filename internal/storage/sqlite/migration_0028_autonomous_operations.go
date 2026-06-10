package sqlite

var migration0028statements = []string{
	`CREATE TABLE IF NOT EXISTS autonomous_actions (
		id TEXT PRIMARY KEY,
		action_type TEXT NOT NULL,
		status TEXT NOT NULL,
		safety_level TEXT NOT NULL,
		resource_id TEXT,
		trigger TEXT,
		reason TEXT,
		rollback_supported INTEGER,
		dry_run INTEGER,
		started_at DATETIME,
		completed_at DATETIME,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL,
		result TEXT,
		error TEXT,
		metadata_json TEXT
	)`,
	`CREATE INDEX IF NOT EXISTS idx_autonomous_actions_type ON autonomous_actions(action_type)`,
	`CREATE INDEX IF NOT EXISTS idx_autonomous_actions_status ON autonomous_actions(status)`,
	`CREATE INDEX IF NOT EXISTS idx_autonomous_actions_created_at ON autonomous_actions(created_at)`,
	`CREATE TABLE IF NOT EXISTS autonomous_rollbacks (
		id TEXT PRIMARY KEY,
		action_id TEXT NOT NULL,
		success INTEGER NOT NULL,
		timestamp DATETIME NOT NULL,
		reason TEXT,
		result TEXT,
		FOREIGN KEY (action_id) REFERENCES autonomous_actions(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_autonomous_rollbacks_action_id ON autonomous_rollbacks(action_id)`,
	`CREATE INDEX IF NOT EXISTS idx_autonomous_rollbacks_timestamp ON autonomous_rollbacks(timestamp)`,
}
