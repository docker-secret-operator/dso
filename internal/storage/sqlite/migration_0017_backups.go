package sqlite

var migration0017statements = []string{
	`CREATE TABLE IF NOT EXISTS backups (
		id TEXT PRIMARY KEY,
		filename TEXT NOT NULL,
		size_bytes INTEGER NOT NULL,
		checksum TEXT NOT NULL,
		backup_type TEXT NOT NULL,
		status TEXT NOT NULL,
		duration_ms INTEGER NOT NULL DEFAULT 0,
		error_msg TEXT,
		created_at TIMESTAMP NOT NULL,
		completed_at TIMESTAMP
	)`,
	`CREATE INDEX idx_backups_status ON backups(status)`,
	`CREATE INDEX idx_backups_created_at ON backups(created_at)`,
	`CREATE INDEX idx_backups_backup_type ON backups(backup_type)`,
}
