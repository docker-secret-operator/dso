package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/execution"
	"github.com/docker-secret-operator/dso/internal/storage"
)

// ExecutionService handles execution request creation, validation, and persistence
type ExecutionService struct {
	draftStore           storage.DraftStore
	reviewStore          storage.ReviewStore
	approvalStore        storage.ApprovalStore
	executionReqStore    storage.ExecutionRequestStore
	executionPlanStore   storage.ExecutionPlanStore
	executionStepStore   storage.ExecutionStepStore
	storageProvider      storage.StorageProvider
	auditService         *AuditService
	validator            *execution.ExecutionValidator
	planner              *execution.ExecutionPlanner
}

// NewExecutionService creates a new execution service with persistence
func NewExecutionService(
	draftStore storage.DraftStore,
	reviewStore storage.ReviewStore,
	approvalStore storage.ApprovalStore,
	auditService *AuditService,
) *ExecutionService {
	return &ExecutionService{
		draftStore:    draftStore,
		reviewStore:   reviewStore,
		approvalStore: approvalStore,
		auditService:  auditService,
		validator:     execution.NewExecutionValidator(draftStore, reviewStore, approvalStore),
		planner:       execution.NewExecutionPlanner(draftStore),
	}
}

// NewExecutionServiceWithPersistence creates service with full persistence support
func NewExecutionServiceWithPersistence(
	provider storage.StorageProvider,
	auditService *AuditService,
) *ExecutionService {
	return &ExecutionService{
		draftStore:         provider.Drafts(),
		reviewStore:        provider.Reviews(),
		approvalStore:      provider.Approvals(),
		executionReqStore:  provider.ExecutionRequests(),
		executionPlanStore: provider.ExecutionPlans(),
		executionStepStore: provider.ExecutionSteps(),
		storageProvider:    provider,
		auditService:       auditService,
		validator:          execution.NewExecutionValidator(provider.Drafts(), provider.Reviews(), provider.Approvals()),
		planner:            execution.NewExecutionPlanner(provider.Drafts()),
	}
}

// CreateExecutionRequest creates a new execution request from an approval with persistence
func (s *ExecutionService) CreateExecutionRequest(
	ctx context.Context,
	draftID string,
	approvalID string,
	correlationID string,
	requestedBy string,
) (*execution.ExecutionRequest, error) {
	// Get approval to find review
	approval, err := s.approvalStore.GetByID(ctx, approvalID)
	if err != nil {
		return nil, fmt.Errorf("approval not found: %v", err)
	}

	if approval == nil {
		return nil, fmt.Errorf("approval not found")
	}

	// Get draft
	draft, err := s.draftStore.GetByID(ctx, draftID)
	if err != nil {
		return nil, fmt.Errorf("draft not found: %v", err)
	}

	// Validate execution readiness
	validationReport, err := s.validator.ValidateRequest(ctx, draftID, approvalID)
	if err != nil {
		return nil, err
	}

	// Create execution request
	now := time.Now()
	ttl := 7 * 24 * time.Hour
	expiresAt := now.Add(ttl)

	execID := fmt.Sprintf("exec-%d", now.Unix())

	storageRequest := &storage.ExecutionRequest{
		ID:            execID,
		CorrelationID: correlationID,
		DraftID:       draftID,
		ReviewID:      approval.ReviewID,
		ApprovalID:    approvalID,
		Status:        "pending",
		CreatedAt:     now,
		ExpiresAt:     expiresAt,
		RequestedBy:   requestedBy,
		Version:       1,
	}

	// Persist request (if store available)
	if s.executionReqStore != nil {
		if err := s.executionReqStore.Create(ctx, storageRequest); err != nil {
			return nil, fmt.Errorf("failed to persist execution request: %w", err)
		}
	}

	// Audit the request creation
	s.auditService.LogEventWithDetails(ctx, &storage.AuditEvent{
		Action:        "execution.requested",
		ResourceID:    execID,
		ResourceType:  "execution",
		ActorID:       requestedBy,
		ActorName:     requestedBy,
		Status:        "success",
		CorrelationID: correlationID,
		Timestamp:     now,
	})

	// If validation passed, validate and generate plan
	if validationReport.AllValid {
		// Generate execution plan
		plan, err := s.planner.GeneratePlan(ctx, execID, draft, approval, correlationID)
		if err != nil {
			storageRequest.Status = "rejected"
			if s.executionReqStore != nil {
				s.executionReqStore.Update(ctx, storageRequest)
			}
			s.auditService.LogEventWithDetails(ctx, &storage.AuditEvent{
				Action:        "execution.validation_failed",
				ResourceID:    execID,
				ResourceType:  "execution",
				ActorID:       requestedBy,
				ActorName:     requestedBy,
				Status:        "failure",
				CorrelationID: correlationID,
				Timestamp:     now,
			})
			return convertStorageRequest(storageRequest), nil
		}

		// Validate plan
		if err := s.planner.ValidatePlan(plan); err != nil {
			storageRequest.Status = "rejected"
			if s.executionReqStore != nil {
				s.executionReqStore.Update(ctx, storageRequest)
			}
			s.auditService.LogEventWithDetails(ctx, &storage.AuditEvent{
				Action:        "execution.validation_failed",
				ResourceID:    execID,
				ResourceType:  "execution",
				ActorID:       requestedBy,
				ActorName:     requestedBy,
				Status:        "failure",
				CorrelationID: correlationID,
				Timestamp:     now,
			})
			return convertStorageRequest(storageRequest), nil
		}

		// Persist plan (if store available)
		if s.executionPlanStore != nil {
			storagePlan := &storage.ExecutionPlan{
				ID:                plan.ID,
				ExecutionID:       execID,
				CorrelationID:     correlationID,
				ApprovalID:        approvalID,
				DraftID:           draftID,
				Status:            "draft",
				TotalSteps:        plan.TotalSteps,
				EstimatedDuration: int(plan.EstimatedDuration.Seconds()),
				RiskScore:         plan.RiskScore,
				AffectedResources: convertResourcesToJSON(plan.AffectedResources),
				RollbackAvailable: plan.RollbackAvailable,
				CreatedAt:         now,
				Version:           1,
			}

			if err := s.executionPlanStore.Create(ctx, storagePlan); err != nil {
				return nil, fmt.Errorf("failed to persist execution plan: %w", err)
			}

			// Persist steps (if store available)
			if s.executionStepStore != nil && len(plan.Steps) > 0 {
				storageSteps := convertPlanStepsToStorage(plan.Steps, storagePlan.ID, now)
				if err := s.executionStepStore.CreateBatch(ctx, storageSteps); err != nil {
					return nil, fmt.Errorf("failed to persist execution steps: %w", err)
				}
			}

			storageRequest.Status = "planned"
			storageRequest.PlanID = &plan.ID
			storageRequest.ValidatedAt = &now

			// Update request status
			if s.executionReqStore != nil {
				if err := s.executionReqStore.Update(ctx, storageRequest); err != nil {
					return nil, fmt.Errorf("failed to update execution request: %w", err)
				}
			}
		}

		// Audit validation
		s.auditService.LogEventWithDetails(ctx, &storage.AuditEvent{
			Action:        "execution.validated",
			ResourceID:    execID,
			ResourceType:  "execution",
			ActorID:       requestedBy,
			ActorName:     requestedBy,
			Status:        "success",
			CorrelationID: correlationID,
			Timestamp:     now,
		})

		// Audit plan creation
		s.auditService.LogEventWithDetails(ctx, &storage.AuditEvent{
			Action:        "execution.planned",
			ResourceID:    plan.ID,
			ResourceType:  "execution_plan",
			ActorID:       requestedBy,
			ActorName:     requestedBy,
			Status:        "success",
			CorrelationID: correlationID,
			Timestamp:     now,
		})
	} else {
		storageRequest.Status = "rejected"
		if s.executionReqStore != nil {
			s.executionReqStore.Update(ctx, storageRequest)
		}
		s.auditService.LogEventWithDetails(ctx, &storage.AuditEvent{
			Action:        "execution.validation_failed",
			ResourceID:    execID,
			ResourceType:  "execution",
			ActorID:       requestedBy,
			ActorName:     requestedBy,
			Status:        "failure",
			CorrelationID: correlationID,
			Timestamp:     now,
		})
	}

	return convertStorageRequest(storageRequest), nil
}

// GetReadinessScore calculates execution readiness
func (s *ExecutionService) GetReadinessScore(
	ctx context.Context,
	draftID string,
	approvalID string,
) (int, error) {
	validationReport, err := s.validator.ValidateRequest(ctx, draftID, approvalID)
	if err != nil {
		return 0, err
	}

	return s.validator.GetReadinessScore(validationReport), nil
}

// CheckExpiration checks if execution request has expired
func (s *ExecutionService) CheckExpiration(request *execution.ExecutionRequest) bool {
	return time.Now().After(request.ExpiresAt)
}

// GetExecutionRequest retrieves a persisted execution request by ID
func (s *ExecutionService) GetExecutionRequest(
	ctx context.Context,
	id string,
) (*execution.ExecutionRequest, error) {
	if s.executionReqStore == nil {
		return nil, fmt.Errorf("execution request store not available")
	}

	storageReq, err := s.executionReqStore.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve execution request: %w", err)
	}

	if storageReq == nil {
		return nil, fmt.Errorf("execution request not found")
	}

	return convertStorageRequest(storageReq), nil
}

// GetExecutionRequestByCorrelation retrieves a request by correlation ID
func (s *ExecutionService) GetExecutionRequestByCorrelation(
	ctx context.Context,
	correlationID string,
) (*execution.ExecutionRequest, error) {
	if s.executionReqStore == nil {
		return nil, fmt.Errorf("execution request store not available")
	}

	storageReq, err := s.executionReqStore.GetByCorrelationID(ctx, correlationID)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve execution request: %w", err)
	}

	if storageReq == nil {
		return nil, fmt.Errorf("execution request not found")
	}

	return convertStorageRequest(storageReq), nil
}

// ListExecutionRequests lists execution requests with pagination
func (s *ExecutionService) ListExecutionRequests(
	ctx context.Context,
	limit int,
	offset int,
) ([]*execution.ExecutionRequest, error) {
	if s.executionReqStore == nil {
		return nil, fmt.Errorf("execution request store not available")
	}

	storageReqs, err := s.executionReqStore.List(ctx, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list execution requests: %w", err)
	}

	var requests []*execution.ExecutionRequest
	for _, req := range storageReqs {
		requests = append(requests, convertStorageRequest(req))
	}

	return requests, nil
}

// ListExecutionsByStatus lists execution requests by status
func (s *ExecutionService) ListExecutionsByStatus(
	ctx context.Context,
	status string,
) ([]*execution.ExecutionRequest, error) {
	if s.executionReqStore == nil {
		return nil, fmt.Errorf("execution request store not available")
	}

	storageReqs, err := s.executionReqStore.ListByStatus(ctx, status)
	if err != nil {
		return nil, fmt.Errorf("failed to list execution requests: %w", err)
	}

	var requests []*execution.ExecutionRequest
	for _, req := range storageReqs {
		requests = append(requests, convertStorageRequest(req))
	}

	return requests, nil
}

// GetExecutionPlan retrieves a persisted execution plan
func (s *ExecutionService) GetExecutionPlan(
	ctx context.Context,
	planID string,
) (*execution.ExecutionPlan, error) {
	if s.executionPlanStore == nil {
		return nil, fmt.Errorf("execution plan store not available")
	}

	storagePlan, err := s.executionPlanStore.GetByID(ctx, planID)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve execution plan: %w", err)
	}

	if storagePlan == nil {
		return nil, fmt.Errorf("execution plan not found")
	}

	// Retrieve steps
	var steps []*execution.ExecutionStep
	if s.executionStepStore != nil {
		storageSteps, err := s.executionStepStore.ListByPlan(ctx, planID)
		if err == nil && len(storageSteps) > 0 {
			steps = convertStorageSteps(storageSteps)
		}
	}

	return convertStoragePlan(storagePlan, steps), nil
}

// GetExecutionPlanByExecution retrieves a plan by execution ID
func (s *ExecutionService) GetExecutionPlanByExecution(
	ctx context.Context,
	executionID string,
) (*execution.ExecutionPlan, error) {
	if s.executionPlanStore == nil {
		return nil, fmt.Errorf("execution plan store not available")
	}

	storagePlan, err := s.executionPlanStore.GetByExecutionID(ctx, executionID)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve execution plan: %w", err)
	}

	if storagePlan == nil {
		return nil, fmt.Errorf("execution plan not found")
	}

	// Retrieve steps
	var steps []*execution.ExecutionStep
	if s.executionStepStore != nil {
		storageSteps, err := s.executionStepStore.ListByPlan(ctx, storagePlan.ID)
		if err == nil && len(storageSteps) > 0 {
			steps = convertStorageSteps(storageSteps)
		}
	}

	return convertStoragePlan(storagePlan, steps), nil
}

// Helper functions for converting between storage and execution models

func convertStorageRequest(req *storage.ExecutionRequest) *execution.ExecutionRequest {
	return &execution.ExecutionRequest{
		ID:            req.ID,
		DraftID:       req.DraftID,
		ReviewID:      req.ReviewID,
		ApprovalID:    req.ApprovalID,
		CorrelationID: req.CorrelationID,
		Status:        req.Status,
		CreatedAt:     req.CreatedAt,
		ValidatedAt:   req.ValidatedAt,
		ExpiresAt:     req.ExpiresAt,
		PlanID:        derefString(req.PlanID),
		RequestedBy:   req.RequestedBy,
		Version:       int64(req.Version),
	}
}

func convertStoragePlan(plan *storage.ExecutionPlan, steps []*execution.ExecutionStep) *execution.ExecutionPlan {
	var resources []string
	if err := json.Unmarshal([]byte(plan.AffectedResources), &resources); err != nil {
		resources = []string{}
	}

	return &execution.ExecutionPlan{
		ID:                plan.ID,
		ExecutionID:       plan.ExecutionID,
		ApprovalID:        plan.ApprovalID,
		DraftID:           plan.DraftID,
		CorrelationID:     plan.CorrelationID,
		Status:            plan.Status,
		Steps:             steps,
		TotalSteps:        plan.TotalSteps,
		EstimatedDuration: time.Duration(plan.EstimatedDuration) * time.Second,
		RiskScore:         plan.RiskScore,
		AffectedResources: resources,
		RollbackAvailable: plan.RollbackAvailable,
		CreatedAt:         plan.CreatedAt,
		ValidatedAt:       plan.ValidatedAt,
		Version:           int64(plan.Version),
	}
}

func convertStorageSteps(steps []*storage.ExecutionStep) []*execution.ExecutionStep {
	var result []*execution.ExecutionStep
	for _, s := range steps {
		result = append(result, &execution.ExecutionStep{
			ID:                s.ID,
			Sequence:          s.Sequence,
			Name:              s.Name,
			Description:       derefString(s.Description),
			Action:            s.Action,
			Payload:           convertPayloadFromJSON(s.Payload),
			RollbackAvailable: s.RollbackAvailable,
			EstimatedTime:     time.Duration(s.EstimatedTime) * time.Second,
			RiskLevel:         s.RiskLevel,
		})
	}
	return result
}

func convertResourcesToJSON(resources []string) string {
	data, _ := json.Marshal(resources)
	return string(data)
}

func convertPayloadFromJSON(payload *string) map[string]string {
	if payload == nil {
		return make(map[string]string)
	}
	var p map[string]string
	json.Unmarshal([]byte(*payload), &p)
	return p
}

func convertPlanStepsToStorage(steps []*execution.ExecutionStep, planID string, now time.Time) []*storage.ExecutionStep {
	var result []*storage.ExecutionStep
	for _, s := range steps {
		payloadJSON, _ := json.Marshal(s.Payload)
		payloadStr := string(payloadJSON)
		desc := s.Description
		result = append(result, &storage.ExecutionStep{
			ID:                s.ID,
			PlanID:            planID,
			Sequence:          s.Sequence,
			Name:              s.Name,
			Description:       &desc,
			Action:            s.Action,
			EstimatedTime:     int(s.EstimatedTime.Seconds()),
			RiskLevel:         s.RiskLevel,
			RollbackAvailable: s.RollbackAvailable,
			Payload:           &payloadStr,
			CreatedAt:         now,
			Version:           1,
		})
	}
	return result
}

// Helper for dereferencing string pointers
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
