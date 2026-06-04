package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"
)

// Migration represents a database schema migration
type migration struct {
	version string
	name    string
	sql     string
}

var migrations = []migration{
	{
		version: "001",
		name: "initial_schema",
		sql: `
-- Schema migrations table
CREATE TABLE IF NOT EXISTS schema_migrations (
    version TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Drafts table
CREATE TABLE IF NOT EXISTS drafts (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    owner_id TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT,
    status TEXT NOT NULL CHECK(status IN ('draft', 'under_review', 'approved', 'rejected', 'archived')),
    version_number INTEGER NOT NULL DEFAULT 1,
    config TEXT NOT NULL,
    checksum TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modified_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_drafts_owner_id ON drafts(owner_id);
CREATE INDEX IF NOT EXISTS idx_drafts_status ON drafts(status);
CREATE INDEX IF NOT EXISTS idx_drafts_created_at ON drafts(created_at);

-- Draft versions table
CREATE TABLE IF NOT EXISTS draft_versions (
    id TEXT PRIMARY KEY,
    draft_id TEXT NOT NULL,
    version_number INTEGER NOT NULL,
    config TEXT NOT NULL,
    checksum TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(draft_id) REFERENCES drafts(id) ON DELETE CASCADE,
    UNIQUE(draft_id, version_number)
);

CREATE INDEX IF NOT EXISTS idx_draft_versions_draft_id ON draft_versions(draft_id);

-- Reviews table
CREATE TABLE IF NOT EXISTS reviews (
    id TEXT PRIMARY KEY,
    draft_id TEXT NOT NULL UNIQUE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT NOT NULL,
    modified_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    status TEXT NOT NULL CHECK(status IN ('draft', 'under_review', 'approved', 'rejected', 'expired')),
    checklist TEXT NOT NULL,
    risk_assessment TEXT NOT NULL,
    required_approvals INTEGER NOT NULL DEFAULT 1,
    approval_timeout_hours INTEGER,
    title TEXT NOT NULL,
    description TEXT,
    FOREIGN KEY(draft_id) REFERENCES drafts(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_reviews_status ON reviews(status);
CREATE INDEX IF NOT EXISTS idx_reviews_created_at ON reviews(created_at);

-- Approvals table
CREATE TABLE IF NOT EXISTS approvals (
    id TEXT PRIMARY KEY,
    review_id TEXT NOT NULL,
    reviewer_id TEXT NOT NULL,
    reviewer_name TEXT NOT NULL,
    decision TEXT NOT NULL CHECK(decision IN ('pending', 'approved', 'rejected', 'abstained')),
    comments TEXT,
    rejection_reason TEXT,
    approval_sequence INTEGER NOT NULL,
    is_required INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    decided_at TIMESTAMP,
    FOREIGN KEY(review_id) REFERENCES reviews(id) ON DELETE CASCADE,
    UNIQUE(review_id, approval_sequence)
);

CREATE INDEX IF NOT EXISTS idx_approvals_review_id ON approvals(review_id);
CREATE INDEX IF NOT EXISTS idx_approvals_reviewer_id ON approvals(reviewer_id);
CREATE INDEX IF NOT EXISTS idx_approvals_decision ON approvals(decision);

-- Review activities table
CREATE TABLE IF NOT EXISTS review_activities (
    id TEXT PRIMARY KEY,
    review_id TEXT NOT NULL,
    type TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    description TEXT NOT NULL,
    metadata TEXT,
    timestamp TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(review_id) REFERENCES reviews(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_review_activities_review_id ON review_activities(review_id);
CREATE INDEX IF NOT EXISTS idx_review_activities_timestamp ON review_activities(timestamp);

-- Snapshots table
CREATE TABLE IF NOT EXISTS snapshots (
    id TEXT PRIMARY KEY,
    draft_id TEXT NOT NULL,
    config TEXT NOT NULL,
    checksum TEXT NOT NULL,
    source TEXT NOT NULL CHECK(source IN ('automated', 'manual', 'pre_execution')),
    source_id TEXT,
    description TEXT,
    tags TEXT,
    verified INTEGER NOT NULL DEFAULT 0,
    applied INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(draft_id) REFERENCES drafts(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_snapshots_draft_id ON snapshots(draft_id);
CREATE INDEX IF NOT EXISTS idx_snapshots_created_at ON snapshots(created_at);

-- Audit events table (APPEND-ONLY, NO UPDATE/DELETE)
CREATE TABLE IF NOT EXISTS audit_events (
    id TEXT PRIMARY KEY,
    timestamp TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    actor_id TEXT NOT NULL,
    actor_name TEXT NOT NULL,
    actor_email TEXT,
    action TEXT NOT NULL,
    resource TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('success', 'failure')),
    result_code TEXT,
    result_message TEXT,
    old_value TEXT,
    new_value TEXT,
    delta TEXT,
    correlation_id TEXT NOT NULL,
    request_id TEXT NOT NULL,
    ip_address TEXT,
    user_agent TEXT,
    severity TEXT NOT NULL CHECK(severity IN ('info', 'warning', 'error', 'critical')),
    retention_until TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_audit_events_timestamp ON audit_events(timestamp);
CREATE INDEX IF NOT EXISTS idx_audit_events_actor_id ON audit_events(actor_id);
CREATE INDEX IF NOT EXISTS idx_audit_events_action ON audit_events(action);
CREATE INDEX IF NOT EXISTS idx_audit_events_resource_id ON audit_events(resource_id);
CREATE INDEX IF NOT EXISTS idx_audit_events_correlation_id ON audit_events(correlation_id);
CREATE INDEX IF NOT EXISTS idx_audit_events_retention ON audit_events(retention_until) WHERE retention_until IS NOT NULL;
`,
	},
}

// runMigrations applies pending migrations to the database
func runMigrations(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create migrations table if it doesn't exist
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}

	// Apply each migration
	for _, m := range migrations {
		// Check if migration already applied
		var count int
		err := db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM schema_migrations WHERE version = ?
		`, m.version).Scan(&count)
		if err != nil {
			return fmt.Errorf("failed to check migration status: %w", err)
		}

		if count > 0 {
			log.Printf("Migration %s already applied, skipping", m.version)
			continue
		}

		// Apply migration
		log.Printf("Applying migration %s: %s", m.version, m.name)
		if _, err := db.ExecContext(ctx, m.sql); err != nil {
			return fmt.Errorf("failed to apply migration %s: %w", m.version, err)
		}

		// Record migration
		_, err = db.ExecContext(ctx, `
			INSERT INTO schema_migrations (version, name, applied_at)
			VALUES (?, ?, ?)
		`, m.version, m.name, time.Now())
		if err != nil {
			return fmt.Errorf("failed to record migration %s: %w", m.version, err)
		}

		log.Printf("Migration %s applied successfully", m.version)
	}

	return nil
}

// GetMigrationVersion returns the current schema version
func GetMigrationVersion(db *sql.DB) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var version string
	err := db.QueryRowContext(ctx, `
		SELECT version FROM schema_migrations
		ORDER BY version DESC
		LIMIT 1
	`).Scan(&version)

	if err == sql.ErrNoRows {
		return "", nil // No migrations applied yet
	}

	return version, err
}
