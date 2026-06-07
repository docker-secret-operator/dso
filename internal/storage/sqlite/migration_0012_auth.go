package sqlite

var migration0012statements = []string{
	// Users table for authentication and RBAC
	`CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		display_name TEXT NOT NULL,
		role TEXT NOT NULL CHECK(role IN ('viewer', 'operator', 'reviewer', 'approver', 'admin')),
		disabled INTEGER NOT NULL DEFAULT 0,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`,

	// Index for username lookups
	`CREATE INDEX IF NOT EXISTS idx_users_username ON users(username)`,

	// Index for role-based queries
	`CREATE INDEX IF NOT EXISTS idx_users_role ON users(role)`,

	// Index for disabled flag
	`CREATE INDEX IF NOT EXISTS idx_users_disabled ON users(disabled)`,

	// Sessions table for session management
	`CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		token_hash TEXT NOT NULL UNIQUE,
		ip_address TEXT,
		user_agent TEXT,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		expires_at TIMESTAMP NOT NULL,
		last_activity TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
	)`,

	// Index for user sessions
	`CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id)`,

	// Index for expiration cleanup
	`CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at)`,

	// Index for token lookups
	`CREATE INDEX IF NOT EXISTS idx_sessions_token_hash ON sessions(token_hash)`,
}

const migration0012 = "" // Placeholder for backward compatibility
