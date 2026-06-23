package sqlite

var migration0029statements = []string{
	`CREATE TABLE IF NOT EXISTS injection_records (
		secret_name   TEXT NOT NULL PRIMARY KEY,
		provider_hash TEXT NOT NULL,
		injected_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`,
	`CREATE INDEX IF NOT EXISTS idx_injection_records_injected_at ON injection_records(injected_at)`,
}
