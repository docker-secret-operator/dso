package services

import (
	"context"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/policy"
	"github.com/docker-secret-operator/dso/internal/storage"
)

func getGovernanceActor(ctx context.Context) (string, string) {
	user := auth.CurrentUser(ctx)
	if user != nil {
		return user.ID, user.Username
	}
	return "system", "system"
}

// GovernanceAuditService logs governance-related events
type GovernanceAuditService struct {
	auditService    *AuditService
	approvalService *ApprovalService
}

// NewGovernanceAuditService creates a new governance audit service
func NewGovernanceAuditService(
	auditService *AuditService,
	approvalService *ApprovalService,
) *GovernanceAuditService {
	return &GovernanceAuditService{
		auditService:    auditService,
		approvalService: approvalService,
	}
}

// LogPolicyViolation logs a policy violation event
func (gas *GovernanceAuditService) LogPolicyViolation(
	ctx context.Context,
	draftID string,
	violation policy.PolicyViolation,
	correlationID string,
) error {
	actorID, actorName := getGovernanceActor(ctx)
	message := fmt.Sprintf("Policy: %s, Severity: %s, Message: %s", violation.PolicyName, violation.Severity, violation.Message)
	return gas.auditService.LogEventWithDetails(ctx, &storage.AuditEvent{
		Action:        "policy.violation",
		ResourceID:    draftID,
		ResourceType:  "draft",
		ActorID:       actorID,
		ActorName:     actorName,
		Status:        "failure",
		ResultMessage: &message,
		CorrelationID: correlationID,
		Timestamp:     time.Now(),
	})
}

// LogPolicyPassed logs when a policy passes
func (gas *GovernanceAuditService) LogPolicyPassed(
	ctx context.Context,
	draftID string,
	policyName string,
	score int,
	correlationID string,
) error {
	actorID, actorName := getGovernanceActor(ctx)
	message := fmt.Sprintf("Policy: %s, Score: %d", policyName, score)
	return gas.auditService.LogEventWithDetails(ctx, &storage.AuditEvent{
		Action:        "policy.passed",
		ResourceID:    draftID,
		ResourceType:  "draft",
		ActorID:       actorID,
		ActorName:     actorName,
		Status:        "success",
		ResultMessage: &message,
		CorrelationID: correlationID,
		Timestamp:     time.Now(),
	})
}

// LogApprovalExpired logs when an approval expires
func (gas *GovernanceAuditService) LogApprovalExpired(
	ctx context.Context,
	approval *storage.Approval,
	reason string,
	correlationID string,
) error {
	actorID, actorName := getGovernanceActor(ctx)
	message := fmt.Sprintf("Approval for reviewer %s expired. Reason: %s", approval.ReviewerID, reason)
	return gas.auditService.LogEventWithDetails(ctx, &storage.AuditEvent{
		Action:        "approval.expired",
		ResourceID:    approval.ID,
		ResourceType:  "approval",
		ActorID:       actorID,
		ActorName:     actorName,
		Status:        "success",
		ResultMessage: &message,
		CorrelationID: correlationID,
		Timestamp:     time.Now(),
	})
}

// LogWorkflowInvalid logs when workflow validation fails
func (gas *GovernanceAuditService) LogWorkflowInvalid(
	ctx context.Context,
	draftID string,
	violations []policy.PolicyViolation,
	correlationID string,
) error {
	actorID, actorName := getGovernanceActor(ctx)
	details := fmt.Sprintf("Workflow invalid with %d violations: ", len(violations))
	for _, v := range violations {
		details += fmt.Sprintf("[%s: %s] ", v.PolicyName, v.Message)
	}

	return gas.auditService.LogEventWithDetails(ctx, &storage.AuditEvent{
		Action:        "workflow.invalid",
		ResourceID:    draftID,
		ResourceType:  "draft",
		ActorID:       actorID,
		ActorName:     actorName,
		Status:        "failure",
		ResultMessage: &details,
		CorrelationID: correlationID,
		Timestamp:     time.Now(),
	})
}

// LogWorkflowValid logs when workflow passes validation
func (gas *GovernanceAuditService) LogWorkflowValid(
	ctx context.Context,
	draftID string,
	score int,
	correlationID string,
) error {
	actorID, actorName := getGovernanceActor(ctx)
	message := fmt.Sprintf("Workflow passed validation with score %d", score)
	return gas.auditService.LogEventWithDetails(ctx, &storage.AuditEvent{
		Action:        "workflow.valid",
		ResourceID:    draftID,
		ResourceType:  "draft",
		ActorID:       actorID,
		ActorName:     actorName,
		Status:        "success",
		ResultMessage: &message,
		CorrelationID: correlationID,
		Timestamp:     time.Now(),
	})
}

// LogApprovalAssignmentInvalid logs when approval assignment fails validation
func (gas *GovernanceAuditService) LogApprovalAssignmentInvalid(
	ctx context.Context,
	reviewID string,
	reviewerID string,
	reason string,
	correlationID string,
) error {
	actorID, actorName := getGovernanceActor(ctx)
	message := fmt.Sprintf("Cannot assign approval to reviewer %s: %s", reviewerID, reason)
	return gas.auditService.LogEventWithDetails(ctx, &storage.AuditEvent{
		Action:        "approval.assignment_invalid",
		ResourceID:    reviewID,
		ResourceType:  "review",
		ActorID:       actorID,
		ActorName:     actorName,
		Status:        "failure",
		ResultMessage: &message,
		CorrelationID: correlationID,
		Timestamp:     time.Now(),
	})
}

// LogApprovalAssignmentValid logs when approval assignment passes validation
func (gas *GovernanceAuditService) LogApprovalAssignmentValid(
	ctx context.Context,
	reviewID string,
	approvalID string,
	reviewerID string,
	correlationID string,
) error {
	actorID, actorName := getGovernanceActor(ctx)
	message := fmt.Sprintf("Approval %s assigned to reviewer %s passed validation", approvalID, reviewerID)
	return gas.auditService.LogEventWithDetails(ctx, &storage.AuditEvent{
		Action:        "approval.assignment_valid",
		ResourceID:    reviewID,
		ResourceType:  "review",
		ActorID:       actorID,
		ActorName:     actorName,
		Status:        "success",
		ResultMessage: &message,
		CorrelationID: correlationID,
		Timestamp:     time.Now(),
	})
}

// LogGovernanceDashboardAccessed logs dashboard access
func (gas *GovernanceAuditService) LogGovernanceDashboardAccessed(
	ctx context.Context,
	actorID string,
	correlationID string,
) error {
	message := "Governance dashboard accessed"
	return gas.auditService.LogEventWithDetails(ctx, &storage.AuditEvent{
		Action:        "governance.dashboard_accessed",
		ResourceID:    "governance",
		ResourceType:  "dashboard",
		ActorID:       actorID,
		ActorName:     actorID,
		Status:        "success",
		ResultMessage: &message,
		CorrelationID: correlationID,
		Timestamp:     time.Now(),
	})
}

// LogGovernanceValidationTriggered logs when governance validation is triggered
func (gas *GovernanceAuditService) LogGovernanceValidationTriggered(
	ctx context.Context,
	draftID string,
	triggerReason string,
	correlationID string,
) error {
	actorID, actorName := getGovernanceActor(ctx)
	message := fmt.Sprintf("Governance validation triggered: %s", triggerReason)
	return gas.auditService.LogEventWithDetails(ctx, &storage.AuditEvent{
		Action:        "governance.validation_triggered",
		ResourceID:    draftID,
		ResourceType:  "draft",
		ActorID:       actorID,
		ActorName:     actorName,
		Status:        "success",
		ResultMessage: &message,
		CorrelationID: correlationID,
		Timestamp:     time.Now(),
	})
}

// LogQuotaExceeded logs when a governance quota is exceeded
func (gas *GovernanceAuditService) LogQuotaExceeded(
	ctx context.Context,
	resourceID string,
	quotaType string,
	limit int,
	actual int,
	correlationID string,
) error {
	actorID, actorName := getGovernanceActor(ctx)
	message := fmt.Sprintf("Quota exceeded: %s (limit: %d, actual: %d)", quotaType, limit, actual)
	return gas.auditService.LogEventWithDetails(ctx, &storage.AuditEvent{
		Action:        "governance.quota_exceeded",
		ResourceID:    resourceID,
		ResourceType:  "quota",
		ActorID:       actorID,
		ActorName:     actorName,
		Status:        "failure",
		ResultMessage: &message,
		CorrelationID: correlationID,
		Timestamp:     time.Now(),
	})
}

// LogGovernanceChain logs a complete chain of governance events
func (gas *GovernanceAuditService) LogGovernanceChain(
	ctx context.Context,
	draftID string,
	events []string,
	correlationID string,
) error {
	actorID, actorName := getGovernanceActor(ctx)
	for _, event := range events {
		err := gas.auditService.LogEventWithDetails(ctx, &storage.AuditEvent{
			Action:        "governance.chain",
			ResourceID:    draftID,
			ResourceType:  "draft",
			ActorID:       actorID,
			ActorName:     actorName,
			Status:        "success",
			ResultMessage: &event,
			CorrelationID: correlationID,
			Timestamp:     time.Now(),
		})
		if err != nil {
			return err
		}
	}
	return nil
}
