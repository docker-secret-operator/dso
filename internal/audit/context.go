package audit

import (
	"context"

	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/storage"
)

// ActorInfo contains authenticated user information for audit
type ActorInfo struct {
	UserID   string
	Username string
	Role     string
}

// GetActorInfo retrieves authenticated user information from context
func GetActorInfo(ctx context.Context) ActorInfo {
	user := auth.CurrentUser(ctx)
	if user == nil {
		// Unauthenticated request (system action)
		return ActorInfo{
			UserID:   "system",
			Username: "system",
			Role:     "system",
		}
	}

	return ActorInfo{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
	}
}

// BuildAuditEvent creates an audit event with actor information from context
func BuildAuditEvent(ctx context.Context, action, resourceType, resourceID, status string) *storage.AuditEvent {
	actor := GetActorInfo(ctx)
	session := auth.CurrentSession(ctx)

	event := &storage.AuditEvent{
		Action:        action,
		ActorName:     actor.Username,
		ActorID:       actor.UserID,
		ResourceType:  resourceType,
		ResourceID:    resourceID,
		Status:        status,
	}

	// Add session ID if available
	if session != nil {
		// Store session ID in a way the audit schema supports
		// This might need to be added to AuditEvent struct if not present
	}

	return event
}
