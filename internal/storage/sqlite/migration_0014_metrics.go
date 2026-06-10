package sqlite

var migration0014statements = []string{
	`CREATE TABLE IF NOT EXISTS metrics_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp TIMESTAMP NOT NULL,
		success_rate REAL NOT NULL DEFAULT 0,
		failure_rate REAL NOT NULL DEFAULT 0,
		throughput REAL NOT NULL DEFAULT 0,
		queue_depth INTEGER NOT NULL DEFAULT 0,
		worker_utilization REAL NOT NULL DEFAULT 0,
		active_executions INTEGER NOT NULL DEFAULT 0,
		memory_mb REAL NOT NULL DEFAULT 0,
		goroutines INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE INDEX IF NOT EXISTS idx_metrics_history_timestamp ON metrics_history(timestamp)`,
}
