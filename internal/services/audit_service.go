package services

import (
	"context"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// AuditService handles audit logging
type AuditService struct {
	store storage.AuditStore
}

// NewAuditService creates a new audit service
func NewAuditService(store storage.AuditStore) *AuditService {
	return &AuditService{store: store}
}

// LogEvent logs an audit event
func (as *AuditService) LogEvent(ctx context.Context, actorID, actorName, action, resource, resourceID, resourceType string) error {
	event := &storage.AuditEvent{
		ID:            generateID(),
		Timestamp:     time.Now(),
		ActorID:       actorID,
		ActorName:     actorName,
		Action:        action,
		Resource:      resource,
		ResourceID:    resourceID,
		ResourceType:  resourceType,
		Status:        "success",
		CorrelationID: generateID(),
		RequestID:     generateID(),
		Severity:      "info",
	}

	return as.store.Log(ctx, event)
}

// LogEventWithDetails logs an audit event with detailed information
func (as *AuditService) LogEventWithDetails(ctx context.Context, event *storage.AuditEvent) error {
	if event.ID == "" {
		event.ID = generateID()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	if event.CorrelationID == "" {
		event.CorrelationID = generateID()
	}
	if event.RequestID == "" {
		event.RequestID = generateID()
	}

	return as.store.Log(ctx, event)
}

// QueryEvents queries audit events
func (as *AuditService) QueryEvents(ctx context.Context, filters map[string]interface{}) ([]*storage.AuditEvent, error) {
	return as.store.Query(ctx, filters)
}

// ExportEvents exports events within a time range
func (as *AuditService) ExportEvents(ctx context.Context, startTime, endTime time.Time) ([]*storage.AuditEvent, error) {
	if startTime.IsZero() || endTime.IsZero() {
		return nil, fmt.Errorf("start and end times required")
	}
	if startTime.After(endTime) {
		return nil, fmt.Errorf("start time must be before end time")
	}

	return as.store.Export(ctx, startTime, endTime)
}

// GetEventsByActor retrieves events for an actor
func (as *AuditService) GetEventsByActor(ctx context.Context, actorID string) ([]*storage.AuditEvent, error) {
	return as.store.Query(ctx, map[string]interface{}{"actor_id": actorID})
}

// GetEventsByResource retrieves events for a resource
func (as *AuditService) GetEventsByResource(ctx context.Context, resourceID string) ([]*storage.AuditEvent, error) {
	return as.store.Query(ctx, map[string]interface{}{"resource_id": resourceID})
}

// GetEventsByAction retrieves events of a specific action
func (as *AuditService) GetEventsByAction(ctx context.Context, action string) ([]*storage.AuditEvent, error) {
	return as.store.Query(ctx, map[string]interface{}{"action": action})
}
