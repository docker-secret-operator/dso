package auth

import "context"

// AuditLogger is a minimal interface for auth-package audit logging.
// Avoids circular imports with the services package.
type AuditLogger interface {
	LogEvent(ctx context.Context, actorID, actorName, action, resource, resourceID, resourceType string) error
}
