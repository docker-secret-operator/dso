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
	// NewSQLiteProvider runs migrations automatically — no ApplyMigrations call needed.
	provider, err := sqlite.NewSQLiteProvider(":memory:")
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

	// 1. Draft Create — (ctx, workspaceID, ownerID, title, description, config)
	draft, err := draftService.CreateDraft(ctx, "secret-1", "user-123", "Initial draft", "Initial draft", "{}")
	if err != nil {
		panic(err)
	}

	auditService.LogEvent(ctx, user.ID, user.Username, "draft.created", "draft", draft.ID, "draft")

	// 2. Review Request
	review, err := reviewService.CreateReview(ctx, &storage.Review{
		DraftID:           draft.ID,
		CreatedBy:         user.ID,
		Title:             "Please review",
		Status:            "under_review",
		Checklist:         "{}",
		RiskAssessment:    "{}",
		RequiredApprovals: 1,
	})
	if err != nil {
		panic(err)
	}
	auditService.LogEvent(ctx, user.ID, user.Username, "review.requested", "review", review.ID, "review")

	// 3. Approval — create then approve
	approval, err := approvalService.CreateApproval(ctx, &storage.Approval{
		ReviewID:     review.ID,
		ReviewerID:   user.ID,
		ReviewerName: user.Username,
	})
	if err != nil {
		panic(err)
	}
	approval, err = approvalService.ApproveApproval(ctx, approval.ID, "Looks good")
	if err != nil {
		panic(err)
	}
	auditService.LogEvent(ctx, user.ID, user.Username, "approval.granted", "approval", approval.ID, "approval")

	// 4. Execution Request
	execReq, err := executionService.CreateExecutionRequest(ctx, draft.ID, approval.ID, correlationID, user.ID)
	if err != nil {
		panic(err)
	}

	// 5. Execution Queued
	auditEvents.LogExecutionQueued(execReq.ID, correlationID, user.ID, user.Username)

	// 6. Execution Completed — (executionID, correlationID, workerID, duration, actorID, actorName)
	auditEvents.LogExecutionCompleted(execReq.ID, correlationID, "worker-1", 0, user.ID, user.Username)

	// Query Audits
	fmt.Println("\nQuerying Audit Events:")
	events, err := provider.Audit().Query(ctx, nil)
	if err != nil {
		panic(err)
	}

	for _, e := range events {
		fmt.Printf("Action: %s | Actor: %s (%s) | CorrelationID: %s\n", e.Action, e.ActorName, e.ActorID, e.CorrelationID)
	}

	fmt.Println("\nSuccess!")
}
