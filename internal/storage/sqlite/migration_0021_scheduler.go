package sqlite

var migration0021statements = []string{
	`CREATE TABLE IF NOT EXISTS scheduler_jobs (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		type TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		schedule TEXT,
		interval_seconds INTEGER,
		next_run TIMESTAMP,
		last_run TIMESTAMP,
		retry_count INTEGER NOT NULL DEFAULT 0,
		max_retries INTEGER NOT NULL DEFAULT 3,
		timeout_seconds INTEGER NOT NULL DEFAULT 300,
		status TEXT NOT NULL,
		metadata_json TEXT,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`,
	`CREATE INDEX IF NOT EXISTS idx_scheduler_jobs_next_run ON scheduler_jobs(next_run)`,
	`CREATE INDEX IF NOT EXISTS idx_scheduler_jobs_status ON scheduler_jobs(status)`,
	`CREATE TABLE IF NOT EXISTS scheduler_executions (
		id TEXT PRIMARY KEY,
		job_id TEXT NOT NULL,
		success INTEGER NOT NULL,
		duration_ms INTEGER,
		error_message TEXT,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(job_id) REFERENCES scheduler_jobs(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_scheduler_executions_job_id ON scheduler_executions(job_id)`,
	`CREATE INDEX IF NOT EXISTS idx_scheduler_executions_created_at ON scheduler_executions(created_at)`,
}
