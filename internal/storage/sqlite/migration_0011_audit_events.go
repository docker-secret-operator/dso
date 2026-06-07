package sqlite

var migration0011statements = []string{
	// Create execution_audit_events table (for Phase 4 execution tracing)
	// Note: Renamed from audit_events to avoid conflict with migration 001's audit_events table
	`CREATE TABLE IF NOT EXISTS execution_audit_events (id TEXT PRIMARY KEY, execution_id TEXT NOT NULL, correlation_id TEXT NOT NULL, action TEXT NOT NULL, status TEXT NOT NULL, details TEXT, resource_id TEXT, resource_type TEXT, timestamp DATETIME NOT NULL, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP)`,

	// Index for CorrelationID-based queries (trace explorer)
	`CREATE INDEX IF NOT EXISTS idx_execution_audit_events_correlation_id ON execution_audit_events(correlation_id, timestamp DESC)`,

	// Index for ExecutionID-based queries
	`CREATE INDEX IF NOT EXISTS idx_execution_audit_events_execution_id ON execution_audit_events(execution_id, timestamp DESC)`,

	// Index for action-based queries (filtering)
	`CREATE INDEX IF NOT EXISTS idx_execution_audit_events_action ON execution_audit_events(action, timestamp DESC)`,

	// Index for time-range queries
	`CREATE INDEX IF NOT EXISTS idx_execution_audit_events_timestamp ON execution_audit_events(timestamp DESC)`,

	// Set journal mode to WAL
	`PRAGMA journal_mode = WAL`,
}

const migration0011 = "" // Placeholder for backward compatibility
