package sqlite

var migration0030statements = []string{
	`CREATE TABLE IF NOT EXISTS secret_versions (
		id             TEXT NOT NULL PRIMARY KEY,
		secret_name    TEXT NOT NULL,
		version        INTEGER NOT NULL,
		provider       TEXT NOT NULL DEFAULT '',
		hash           TEXT NOT NULL DEFAULT '',
		rotated_by     TEXT NOT NULL DEFAULT '',
		rotation_source TEXT NOT NULL DEFAULT 'manual',
		execution_id   TEXT NOT NULL DEFAULT '',
		created_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(secret_name, version)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_secret_versions_name ON secret_versions(secret_name)`,
	`CREATE INDEX IF NOT EXISTS idx_secret_versions_created_at ON secret_versions(created_at)`,
}
