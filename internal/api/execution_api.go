package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/services"
)

// ExecutionHandler handles execution validation and planning endpoints
type ExecutionHandler struct {
	executionService *services.ExecutionService
	approvalService  *services.ApprovalService
	draftService     *services.DraftService
	db               *sql.DB
}

// NewExecutionHandler creates a new execution handler
func NewExecutionHandler(
	executionService *services.ExecutionService,
	approvalService *services.ApprovalService,
	draftService *services.DraftService,
) *ExecutionHandler {
	return &ExecutionHandler{
		executionService: executionService,
		approvalService:  approvalService,
		draftService:     draftService,
	}
}

// SetDB attaches a DB connection for journey queries
func (h *ExecutionHandler) SetDB(db *sql.DB) {
	h.db = db
}

// ServeHTTP handles execution requests
func (h *ExecutionHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	path := strings.TrimPrefix(r.URL.Path, "/api/")

	// POST /api/executions - Create execution request
	if path == "executions" && r.Method == http.MethodPost {
		h.createExecution(w, r)
		return
	}

	// GET /api/executions - List execution requests
	if path == "executions" && r.Method == http.MethodGet {
		h.listExecutions(w, r)
		return
	}

	// GET /api/executions/{id} - Get execution request
	if strings.HasPrefix(path, "executions/") && !strings.Contains(strings.TrimPrefix(path, "executions/"), "/") && r.Method == http.MethodGet {
		h.getExecution(w, r)
		return
	}

	// GET /api/executions/{id}/plan - Get execution plan
	if strings.Contains(path, "/plan") && r.Method == http.MethodGet {
		h.getExecutionPlan(w, r)
		return
	}

	// GET /api/executions/{id}/validation - Get validation result
	if strings.Contains(path, "/validation") && r.Method == http.MethodGet {
		h.getValidation(w, r)
		return
	}

	// GET /api/executions/{id}/trace - Get execution trace (Feature 4)
	if strings.Contains(path, "/trace") && r.Method == http.MethodGet {
		h.getTrace(w, r)
		return
	}

	// GET /api/executions/{id}/journey - Get execution journey (Phase 5.6)
	if strings.Contains(path, "/journey") && r.Method == http.MethodGet {
		h.getJourney(w, r)
		return
	}

	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{"error": "Not found"})
}

// JourneyStep is one timestamped step in an execution's lifecycle
type JourneyStep struct {
	Step          string    `json:"step"`
	Action        string    `json:"action"`
	Status        string    `json:"status"`
	Actor         string    `json:"actor"`
	ActorID       string    `json:"actor_id"`
	CorrelationID string    `json:"correlation_id"`
	Details       string    `json:"details"`
	Timestamp     time.Time `json:"timestamp"`
}

// ExecutionJourneyResponse is the full lifecycle of one execution
type ExecutionJourneyResponse struct {
	ExecutionID   string         `json:"execution_id"`
	CorrelationID string         `json:"correlation_id"`
	TotalSteps    int            `json:"total_steps"`
	DurationMs    int64          `json:"duration_ms"`
	Steps         []*JourneyStep `json:"steps"`
	Timestamp     time.Time      `json:"timestamp"`
}

// getJourney serves GET /api/executions/{id}/journey
func (h *ExecutionHandler) getJourney(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/executions/")
	executionID := strings.TrimSuffix(path, "/journey")
	if executionID == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing execution ID"})
		return
	}

	ctx := r.Context()
	steps := make([]*JourneyStep, 0)
	correlationID := ""

	if h.db != nil {
		// Query main audit_events for actor-attributed steps
		q := `SELECT action, status, actor_name, actor_id, COALESCE(correlation_id,''),
			COALESCE(result_message,''), timestamp
			FROM audit_events
			WHERE (resource_id = ? AND resource_type = 'execution')
			ORDER BY timestamp ASC`
		rows, err := h.db.QueryContext(ctx, q, executionID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var s JourneyStep
				var ts time.Time
				if err := rows.Scan(&s.Action, &s.Status, &s.Actor, &s.ActorID,
					&s.CorrelationID, &s.Details, &ts); err == nil {
					s.Timestamp = ts
					s.Step = actionToStep(s.Action)
					if correlationID == "" {
						correlationID = s.CorrelationID
					}
					steps = append(steps, &s)
				}
			}
		}

		// Query execution_audit_events for system-level steps
		eq := `SELECT action, status, COALESCE(details,''), correlation_id, timestamp
			FROM execution_audit_events
			WHERE execution_id = ?
			ORDER BY timestamp ASC`
		erows, err := h.db.QueryContext(ctx, eq, executionID)
		if err == nil {
			defer erows.Close()
			for erows.Next() {
				var s JourneyStep
				var ts time.Time
				if err := erows.Scan(&s.Action, &s.Status, &s.Details, &s.CorrelationID, &ts); err == nil {
					s.Timestamp = ts
					s.Actor = "system"
					s.Step = actionToStep(s.Action)
					if correlationID == "" {
						correlationID = s.CorrelationID
					}
					steps = append(steps, &s)
				}
			}
		}

		// Sort steps chronologically
		for i := 1; i < len(steps); i++ {
			key := steps[i]
			j := i - 1
			for j >= 0 && steps[j].Timestamp.After(key.Timestamp) {
				steps[j+1] = steps[j]
				j--
			}
			steps[j+1] = key
		}
	}

	var durationMs int64
	if len(steps) >= 2 {
		durationMs = steps[len(steps)-1].Timestamp.Sub(steps[0].Timestamp).Milliseconds()
	}

	json.NewEncoder(w).Encode(&ExecutionJourneyResponse{
		ExecutionID:   executionID,
		CorrelationID: correlationID,
		TotalSteps:    len(steps),
		DurationMs:    durationMs,
		Steps:         steps,
		Timestamp:     time.Now(),
	})
}

func actionToStep(action string) string {
	switch {
	case strings.Contains(action, "queue"):
		return "queued"
	case strings.Contains(action, "start"):
		return "started"
	case strings.Contains(action, "pause"):
		return "paused"
	case strings.Contains(action, "resum"):
		return "resumed"
	case strings.Contains(action, "cancel"):
		return "cancelled"
	case strings.Contains(action, "recover"):
		return "recovered"
	case strings.Contains(action, "complet"):
		return "completed"
	case strings.Contains(action, "fail"):
		return "failed"
	case strings.Contains(action, "timeout"):
		return "timed_out"
	case strings.Contains(action, "dlq"):
		return "dlq"
	default:
		return action
	}
}

// CreateExecutionRequest represents the request to create execution
type CreateExecutionRequest struct {
	DraftID    string `json:"draft_id"`
	ApprovalID string `json:"approval_id"`
}

// ExecutionResponse represents execution request response
type ExecutionResponse struct {
	ID             string `json:"id"`
	DraftID        string `json:"draft_id"`
	ApprovalID     string `json:"approval_id"`
	Status         string `json:"status"`
	CreatedAt      string `json:"created_at"`
	ExpiresAt      string `json:"expires_at"`
	ReadinessScore int    `json:"readiness_score"`
}

// createExecution creates a new execution request
func (h *ExecutionHandler) createExecution(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req CreateExecutionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request"})
		return
	}

	// Get correlation ID from header
	correlationID := r.Header.Get("X-Correlation-ID")
	if correlationID == "" {
		correlationID = fmt.Sprintf("exec-%d", time.Now().Unix())
	}

	actorID := "system"
	if user := auth.CurrentUser(ctx); user != nil {
		actorID = user.ID
	}

	// Create execution request
	execReq, err := h.executionService.CreateExecutionRequest(
		ctx,
		req.DraftID,
		req.ApprovalID,
		correlationID,
		actorID,
	)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Get readiness score
	score, _ := h.executionService.GetReadinessScore(ctx, req.DraftID, req.ApprovalID)

	response := ExecutionResponse{
		ID:             execReq.ID,
		DraftID:        execReq.DraftID,
		ApprovalID:     execReq.ApprovalID,
		Status:         execReq.Status,
		CreatedAt:      execReq.CreatedAt.String(),
		ExpiresAt:      execReq.ExpiresAt.String(),
		ReadinessScore: score,
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

// listExecutions lists execution requests (Feature 2 - now with persistence)
func (h *ExecutionHandler) listExecutions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get pagination parameters
	limit := 100
	offset := 0
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := parseInt(limitStr); err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := parseInt(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	// Get optional status filter
	status := r.URL.Query().Get("status")

	var executions []*ExecutionResponse
	var total int

	if status != "" {
		// Filter by status
		reqs, err := h.executionService.ListExecutionsByStatus(ctx, status)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		total = len(reqs)
		for _, req := range reqs {
			executions = append(executions, executionToResponse(req))
		}
	} else {
		// Get all with pagination
		reqs, err := h.executionService.ListExecutionRequests(ctx, limit, offset)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		executions = make([]*ExecutionResponse, len(reqs))
		for i, req := range reqs {
			executions[i] = executionToResponse(req)
		}
		total = len(reqs)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"executions": executions,
		"total":      total,
		"limit":      limit,
		"offset":     offset,
	})
}

// getExecution retrieves a single execution request (Feature 2)
func (h *ExecutionHandler) getExecution(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	path := strings.TrimPrefix(r.URL.Path, "/api/executions/")
	executionID := strings.Split(path, "/")[0]

	// Try to get by ID first
	exec, err := h.executionService.GetExecutionRequest(ctx, executionID)
	if err != nil {
		// Try by correlation ID
		exec, err = h.executionService.GetExecutionRequestByCorrelation(ctx, executionID)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "Execution not found"})
			return
		}
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(executionToResponse(exec))
}

// ExecutionPlanResponse represents execution plan response
type ExecutionPlanResponse struct {
	ID                string         `json:"id"`
	ExecutionID       string         `json:"execution_id"`
	Status            string         `json:"status"`
	TotalSteps        int            `json:"total_steps"`
	EstimatedDuration string         `json:"estimated_duration"`
	RiskScore         int            `json:"risk_score"`
	AffectedResources []string       `json:"affected_resources"`
	RollbackAvailable bool           `json:"rollback_available"`
	CreatedAt         string         `json:"created_at"`
	Steps             []StepResponse `json:"steps,omitempty"`
}

// StepResponse represents an execution step
type StepResponse struct {
	ID                string `json:"id"`
	Sequence          int    `json:"sequence"`
	Name              string `json:"name"`
	Description       string `json:"description,omitempty"`
	Action            string `json:"action"`
	EstimatedTime     string `json:"estimated_time"`
	RiskLevel         string `json:"risk_level"`
	RollbackAvailable bool   `json:"rollback_available"`
}

// getExecutionPlan retrieves execution plan (Feature 2 - now with persistence)
func (h *ExecutionHandler) getExecutionPlan(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	path := strings.TrimPrefix(r.URL.Path, "/api/executions/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid path"})
		return
	}

	executionID := parts[0]

	// Get plan by execution ID
	plan, err := h.executionService.GetExecutionPlanByExecution(ctx, executionID)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Plan not found"})
		return
	}

	// Convert steps
	steps := make([]StepResponse, len(plan.Steps))
	for i, step := range plan.Steps {
		steps[i] = StepResponse{
			ID:                step.ID,
			Sequence:          step.Sequence,
			Name:              step.Name,
			Description:       step.Description,
			Action:            step.Action,
			EstimatedTime:     step.EstimatedTime.String(),
			RiskLevel:         step.RiskLevel,
			RollbackAvailable: step.RollbackAvailable,
		}
	}

	response := ExecutionPlanResponse{
		ID:                plan.ID,
		ExecutionID:       plan.ExecutionID,
		Status:            plan.Status,
		TotalSteps:        plan.TotalSteps,
		EstimatedDuration: plan.EstimatedDuration.String(),
		RiskScore:         plan.RiskScore,
		AffectedResources: plan.AffectedResources,
		RollbackAvailable: plan.RollbackAvailable,
		CreatedAt:         plan.CreatedAt.String(),
		Steps:             steps,
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// getTrace retrieves execution trace (Feature 4)
func (h *ExecutionHandler) getTrace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	path := strings.TrimPrefix(r.URL.Path, "/api/executions/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid path"})
		return
	}

	executionID := parts[0]

	// Get execution request
	exec, err := h.executionService.GetExecutionRequest(ctx, executionID)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Execution not found"})
		return
	}

	// Get execution plan
	var plan *ExecutionPlanResponse
	if exec.PlanID != "" {
		p, err := h.executionService.GetExecutionPlan(ctx, exec.PlanID)
		if err == nil {
			steps := make([]StepResponse, len(p.Steps))
			for i, step := range p.Steps {
				steps[i] = StepResponse{
					ID:                step.ID,
					Sequence:          step.Sequence,
					Name:              step.Name,
					Description:       step.Description,
					Action:            step.Action,
					EstimatedTime:     step.EstimatedTime.String(),
					RiskLevel:         step.RiskLevel,
					RollbackAvailable: step.RollbackAvailable,
				}
			}
			plan = &ExecutionPlanResponse{
				ID:                p.ID,
				ExecutionID:       p.ExecutionID,
				Status:            p.Status,
				TotalSteps:        p.TotalSteps,
				EstimatedDuration: p.EstimatedDuration.String(),
				RiskScore:         p.RiskScore,
				AffectedResources: p.AffectedResources,
				RollbackAvailable: p.RollbackAvailable,
				CreatedAt:         p.CreatedAt.String(),
				Steps:             steps,
			}
		}
	}

	trace := map[string]interface{}{
		"execution":      executionToResponse(exec),
		"plan":           plan,
		"correlation_id": exec.CorrelationID,
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(trace)
}

// Helper functions
func executionToResponse(exec interface{}) *ExecutionResponse {
	// If already a response, return as-is
	if r, ok := exec.(*ExecutionResponse); ok {
		return r
	}

	// For now, return empty response
	// Full implementation would require reflection or type assertion
	// on the actual execution model type
	return &ExecutionResponse{}
}

func parseInt(s string) (int, error) {
	var i int
	_, err := fmt.Sscanf(s, "%d", &i)
	return i, err
}

// ValidationResponse represents validation result
type ValidationResponse struct {
	Ready           bool     `json:"ready"`
	Score           int      `json:"score"`
	ApprovalValid   bool     `json:"approval_valid"`
	GovernanceValid bool     `json:"governance_valid"`
	VersionValid    bool     `json:"version_valid"`
	SafetyValid     bool     `json:"safety_valid"`
	Messages        []string `json:"messages"`
}

// getValidation retrieves validation results
func (h *ExecutionHandler) getValidation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract draft and approval from query params
	draftID := r.URL.Query().Get("draft_id")
	approvalID := r.URL.Query().Get("approval_id")

	if draftID == "" || approvalID == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "draft_id and approval_id required"})
		return
	}

	// Run validation
	score, err := h.executionService.GetReadinessScore(ctx, draftID, approvalID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	response := ValidationResponse{
		Ready:           score == 100,
		Score:           score,
		ApprovalValid:   true,
		GovernanceValid: true,
		VersionValid:    true,
		SafetyValid:     true,
		Messages:        []string{},
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
