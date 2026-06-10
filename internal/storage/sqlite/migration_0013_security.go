package sqlite

var migration0013statements = []string{
	`ALTER TABLE users ADD COLUMN failed_login_count INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE users ADD COLUMN locked_until TIMESTAMP`,
	`ALTER TABLE users ADD COLUMN password_changed_at TIMESTAMP`,
	`ALTER TABLE users ADD COLUMN password_expires_at TIMESTAMP`,
	`ALTER TABLE users ADD COLUMN must_change_password INTEGER NOT NULL DEFAULT 0`,
}
