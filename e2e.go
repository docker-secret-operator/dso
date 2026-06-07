package main

import (
	"context"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/execution"
	"github.com/docker-secret-operator/dso/internal/services"
	"github.com/docker-secret-operator/dso/internal/storage"
	"github.com/docker-secret-operator/dso/internal/storage/sqlite"
)

// eventPersisterAdapter bridges execution.EventPersister to services.AuditService
type eventPersisterAdapter struct {
	auditService *services.AuditService
}

func (a *eventPersisterAdapter) LogExecutionEvent(event execution.OrchestrationAuditEvent) error {
	auditEvent := &storage.AuditEvent{
		ID:            event.ID,
		Timestamp:     event.Timestamp,
		ActorID:       event.ActorID,
		ActorName:     event.ActorName,
		Action:        event.Action,
		Status:        event.Status,
		Resource:      "execution",
		ResourceID:    event.ExecutionID,
		ResourceType:  "execution",
		CorrelationID: event.CorrelationID,
		RequestID:     fmt.Sprintf("req-%d", time.Now().UnixNano()),
		Severity:      "info",
	}
	return a.auditService.LogEventWithDetails(context.Background(), auditEvent)
}

func main() {
	provider, err := sqlite.NewSQLiteProvider(":memory:")
	if err != nil {
		panic(err)
	}
	
	err = provider.ApplyMigrations(context.Background())
	if err != nil {
		panic(err)
	}

	auditService := services.NewAuditService(provider.Audit())
	draftService := services.NewDraftService(provider.Drafts())
	reviewService := services.NewReviewService(provider.Reviews(), provider.Approvals(), provider.ReviewActivities(), provider.Audit())
	approvalService := services.NewApprovalService(provider.Approvals(), provider.Audit())
	executionService := services.NewExecutionServiceWithPersistence(provider, auditService)

	persister := &eventPersisterAdapter{auditService: auditService}
	auditEvents := execution.NewExecutionAuditEvents(persister)
	
	ctx := context.Background()
	user := &storage.User{
		ID:       "u-123",
		Username: "admin",
		Role:     "admin",
	}
	
	provider.Users().Create(ctx, user)
	
	fmt.Println("Simulating workflow...")
	
	correlationID := "corr-chain-1"
	
	// 1. Draft Create
	draft, err := draftService.CreateDraft(ctx, "secret-1", "user-123", "AWS", "eu-west-1", "{}", "Initial draft")
	if err != nil { panic(err) }
	
	auditService.LogEvent(ctx, user.ID, user.Username, "draft.created", "draft", draft.ID, "draft")
	
	// 2. Review Request
	review, err := reviewService.RequestReview(ctx, draft.ID, user.ID, "Please review")
	if err != nil { panic(err) }
	auditService.LogEvent(ctx, user.ID, user.Username, "review.requested", "review", review.ID, "review")
	
	// 3. Approval Grant
	approval, err := approvalService.GrantApproval(ctx, draft.ID, review.ID, user.ID, "Looks good", time.Hour)
	if err != nil { panic(err) }
	auditService.LogEvent(ctx, user.ID, user.Username, "approval.granted", "approval", approval.ID, "approval")
	
	// 4. Execution Request
	execReq, err := executionService.CreateExecutionRequest(ctx, draft.ID, approval.ID, correlationID, user.ID)
	if err != nil { panic(err) }
	
	// 5. Execution Queued
	auditEvents.LogExecutionQueued(execReq.ID, correlationID, user.ID, user.Username)
	
	// 6. Execution Completed
	auditEvents.LogExecutionCompleted(execReq.ID, correlationID, user.ID, user.Username, "success")
	
	// Query Audits
	fmt.Println("\nQuerying Audit Events:")
	events, err := provider.Audit().Query(ctx, nil)
	if err != nil { panic(err) }
	
	for _, e := range events {
		fmt.Printf("Action: %s | Actor: %s (%s) | CorrelationID: %s\n", e.Action, e.ActorName, e.ActorID, e.CorrelationID)
	}
	
	fmt.Println("\nSuccess!")
}
