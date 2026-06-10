package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/docker-secret-operator/dso/internal/execution"
)

// OrchestrationHandler handles orchestration observability endpoints
type OrchestrationHandler struct {
	dispatcher     *execution.Dispatcher
	workerManager  *execution.WorkerManager
	executionQueue *execution.ExecutionQueue
	auditEvents    *execution.ExecutionAuditEvents
	resilience     *execution.ResilienceManager
}

// NewOrchestrationHandler creates a new orchestration handler
func NewOrchestrationHandler(
	dispatcher *execution.Dispatcher,
	workerManager *execution.WorkerManager,
	executionQueue *execution.ExecutionQueue,
	auditEvents *execution.ExecutionAuditEvents,
	resilience *execution.ResilienceManager,
) *OrchestrationHandler {
	return &OrchestrationHandler{
		dispatcher:     dispatcher,
		workerManager:  workerManager,
		executionQueue: executionQueue,
		auditEvents:    auditEvents,
		resilience:     resilience,
	}
}

// ServeHTTP handles orchestration API requests
func (h *OrchestrationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	path := strings.TrimPrefix(r.URL.Path, "/api/orchestration/")

	// GET /api/orchestration/overview
	if path == "overview" && r.Method == http.MethodGet {
		h.getOverview(w, r)
		return
	}

	// GET /api/orchestration/workers
	if path == "workers" && r.Method == http.MethodGet {
		h.listWorkers(w, r)
		return
	}

	// GET /api/orchestration/workers/{id}
	if strings.HasPrefix(path, "workers/") && !strings.Contains(strings.TrimPrefix(path, "workers/"), "/") && r.Method == http.MethodGet {
		h.getWorker(w, r)
		return
	}

	// GET /api/orchestration/executions
	if path == "executions" && r.Method == http.MethodGet {
		h.listExecutions(w, r)
		return
	}

	// GET /api/orchestration/metrics
	if path == "metrics" && r.Method == http.MethodGet {
		h.getMetrics(w, r)
		return
	}

	// GET /api/orchestration/trace/{correlationID}
	if strings.HasPrefix(path, "trace/") && r.Method == http.MethodGet {
		h.getTrace(w, r)
		return
	}

	// GET /api/orchestration/resilience
	if path == "resilience" && r.Method == http.MethodGet {
		h.getResilience(w, r)
		return
	}

	// GET /api/orchestration/dead-letter-queue
	if path == "dead-letter-queue" && r.Method == http.MethodGet {
		h.getDeadLetterQueue(w, r)
		return
	}

	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{"error": "Not found"})
}

// OverviewResponse represents orchestration overview
type OverviewResponse struct {
	QueuedCount    int                    `json:"queued_count"`
	RunningCount   int                    `json:"running_count"`
	CompletedCount int                    `json:"completed_count"`
	FailedCount    int                    `json:"failed_count"`
	ActiveWorkers  int                    `json:"active_workers"`
	HealthyWorkers int                    `json:"healthy_workers"`
	AvgDuration    string                 `json:"avg_duration"`
	SuccessRate    float64                `json:"success_rate"`
	Throughput     float64                `json:"throughput_per_sec"`
	QueueStats     map[string]interface{} `json:"queue_stats"`
	Timestamp      time.Time              `json:"timestamp"`
}

// getOverview returns orchestration overview
func (h *OrchestrationHandler) getOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	metrics := h.dispatcher.GetMetrics(ctx)
	workers, _ := h.workerManager.ListWorkers(ctx)
	healthyWorkers, _ := h.workerManager.GetHealthyWorkers(ctx)
	queueStats := h.executionQueue.Stats(ctx)

	successRate := 0.0
	if metrics.CompletedCount+metrics.FailedCount > 0 {
		successRate = float64(metrics.CompletedCount) / float64(metrics.CompletedCount+metrics.FailedCount)
	}

	response := OverviewResponse{
		QueuedCount:    metrics.QueuedCount,
		RunningCount:   metrics.ActiveCount,
		CompletedCount: metrics.CompletedCount,
		FailedCount:    metrics.FailedCount,
		ActiveWorkers:  len(workers),
		HealthyWorkers: len(healthyWorkers),
		AvgDuration:    metrics.AverageDuration.String(),
		SuccessRate:    successRate,
		Throughput:     metrics.ThroughputPerSec,
		QueueStats:     queueStats,
		Timestamp:      time.Now(),
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// WorkerResponse represents a worker
type WorkerResponse struct {
	ID               string    `json:"id"`
	State            string    `json:"state"`
	Capabilities     []string  `json:"capabilities"`
	MaxConcurrent    int       `json:"max_concurrent"`
	CurrentlyRunning int       `json:"currently_running"`
	CompletedCount   int       `json:"completed_count"`
	FailedCount      int       `json:"failed_count"`
	LastHeartbeat    time.Time `json:"last_heartbeat"`
	RegisteredAt     time.Time `json:"registered_at"`
	HealthStatus     string    `json:"health_status"`
}

// listWorkers returns all workers
func (h *OrchestrationHandler) listWorkers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	workers, err := h.workerManager.ListWorkers(ctx)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	responses := make([]WorkerResponse, len(workers))
	for i, worker := range workers {
		healthy, _ := h.workerManager.GetWorkerHealth(ctx, worker.ID)
		healthStatus := "healthy"
		if !healthy {
			healthStatus = "unhealthy"
		}

		responses[i] = WorkerResponse{
			ID:               worker.ID,
			State:            string(worker.State),
			Capabilities:     worker.Capabilities,
			MaxConcurrent:    worker.MaxConcurrent,
			CurrentlyRunning: worker.CurrentlyRunning,
			CompletedCount:   worker.CompletedCount,
			FailedCount:      worker.FailedCount,
			LastHeartbeat:    worker.LastHeartbeat,
			RegisteredAt:     worker.RegisteredAt,
			HealthStatus:     healthStatus,
		}
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"workers": responses,
		"count":   len(responses),
	})
}

// getWorker returns a single worker
func (h *OrchestrationHandler) getWorker(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	path := strings.TrimPrefix(r.URL.Path, "/api/orchestration/workers/")
	workerID := path

	worker, err := h.workerManager.GetWorker(ctx, workerID)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Worker not found"})
		return
	}

	healthy, _ := h.workerManager.GetWorkerHealth(ctx, worker.ID)
	healthStatus := "healthy"
	if !healthy {
		healthStatus = "unhealthy"
	}

	response := WorkerResponse{
		ID:               worker.ID,
		State:            string(worker.State),
		Capabilities:     worker.Capabilities,
		MaxConcurrent:    worker.MaxConcurrent,
		CurrentlyRunning: worker.CurrentlyRunning,
		CompletedCount:   worker.CompletedCount,
		FailedCount:      worker.FailedCount,
		LastHeartbeat:    worker.LastHeartbeat,
		RegisteredAt:     worker.RegisteredAt,
		HealthStatus:     healthStatus,
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// ActiveExecutionResponse represents an active execution
type ActiveExecutionResponse struct {
	ExecutionID   string    `json:"execution_id"`
	CorrelationID string    `json:"correlation_id"`
	WorkerID      string    `json:"worker_id"`
	Status        string    `json:"status"`
	StartedAt     time.Time `json:"started_at"`
	Duration      string    `json:"duration,omitempty"`
}

// listExecutions returns active executions
func (h *OrchestrationHandler) listExecutions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	executions, err := h.dispatcher.ListActiveExecutions(ctx)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	responses := make([]ActiveExecutionResponse, len(executions))
	for i, exec := range executions {
		duration := ""
		if exec.CompletedAt != nil {
			duration = exec.Duration.String()
		}

		responses[i] = ActiveExecutionResponse{
			ExecutionID:   exec.ExecutionID,
			CorrelationID: exec.CorrelationID,
			WorkerID:      exec.WorkerID,
			Status:        string(exec.Status),
			StartedAt:     exec.StartedAt,
			Duration:      duration,
		}
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"executions": responses,
		"count":      len(responses),
	})
}

// MetricsResponse represents orchestration metrics
type MetricsResponse struct {
	SuccessRate       float64   `json:"success_rate"`
	FailureRate       float64   `json:"failure_rate"`
	AvgDuration       string    `json:"avg_duration"`
	WorkerUtilization float64   `json:"worker_utilization"`
	QueueDepth        int       `json:"queue_depth"`
	OldestQueuedItem  string    `json:"oldest_queued_item,omitempty"`
	ThroughputPerSec  float64   `json:"throughput_per_sec"`
	ActiveWorkers     int       `json:"active_workers"`
	HealthyWorkers    int       `json:"healthy_workers"`
	TotalCompleted    int       `json:"total_completed"`
	TotalFailed       int       `json:"total_failed"`
	TotalQueued       int       `json:"total_queued"`
	Timestamp         time.Time `json:"timestamp"`
}

// getMetrics returns orchestration metrics
func (h *OrchestrationHandler) getMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	metrics := h.dispatcher.GetMetrics(ctx)
	workers, _ := h.workerManager.ListWorkers(ctx)
	healthyWorkers, _ := h.workerManager.GetHealthyWorkers(ctx)

	successRate := 0.0
	failureRate := 0.0
	total := metrics.CompletedCount + metrics.FailedCount
	if total > 0 {
		successRate = float64(metrics.CompletedCount) / float64(total)
		failureRate = float64(metrics.FailedCount) / float64(total)
	}

	utilization := 0.0
	if len(workers) > 0 {
		totalCapacity := 0
		totalRunning := 0
		for _, w := range workers {
			totalCapacity += w.MaxConcurrent
			totalRunning += w.CurrentlyRunning
		}
		if totalCapacity > 0 {
			utilization = float64(totalRunning) / float64(totalCapacity)
		}
	}

	oldestItem := ""
	item, err := h.executionQueue.Peek(ctx)
	if err == nil && item != nil {
		age := time.Since(item.EnqueuedAt)
		oldestItem = fmt.Sprintf("%s (age: %v)", item.ExecutionID, age)
	}

	response := MetricsResponse{
		SuccessRate:       successRate,
		FailureRate:       failureRate,
		AvgDuration:       metrics.AverageDuration.String(),
		WorkerUtilization: utilization,
		QueueDepth:        metrics.QueuedCount,
		OldestQueuedItem:  oldestItem,
		ThroughputPerSec:  metrics.ThroughputPerSec,
		ActiveWorkers:     len(workers),
		HealthyWorkers:    len(healthyWorkers),
		TotalCompleted:    metrics.CompletedCount,
		TotalFailed:       metrics.FailedCount,
		TotalQueued:       metrics.QueuedCount,
		Timestamp:         time.Now(),
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// TraceResponse represents execution trace
type TraceResponse struct {
	ExecutionID   string                              `json:"execution_id"`
	CorrelationID string                              `json:"correlation_id"`
	Status        string                              `json:"status"`
	CreatedAt     time.Time                           `json:"created_at"`
	AuditEvents   []execution.OrchestrationAuditEvent `json:"audit_events"`
	EventCount    int                                 `json:"event_count"`
	Duration      string                              `json:"duration,omitempty"`
}

// getTrace returns execution trace by correlation ID
func (h *OrchestrationHandler) getTrace(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/orchestration/trace/")
	correlationID := path

	if correlationID == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Correlation ID required"})
		return
	}

	// Filter audit events by correlation ID
	allEvents := h.auditEvents.ListEvents()
	filteredEvents := make([]execution.OrchestrationAuditEvent, 0)

	for _, event := range allEvents {
		if event.CorrelationID == correlationID {
			filteredEvents = append(filteredEvents, event)
		}
	}

	if len(filteredEvents) == 0 {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Trace not found"})
		return
	}

	// Get first and last event for timing
	first := filteredEvents[0]
	last := filteredEvents[len(filteredEvents)-1]
	duration := last.Timestamp.Sub(first.Timestamp)

	// Determine status from last event
	status := last.Status
	for _, event := range filteredEvents {
		if event.Action == "execution.completed" {
			status = "completed"
			break
		} else if event.Action == "execution.failed" {
			status = "failed"
			break
		}
	}

	response := TraceResponse{
		CorrelationID: correlationID,
		Status:        status,
		CreatedAt:     first.Timestamp,
		AuditEvents:   filteredEvents,
		EventCount:    len(filteredEvents),
		Duration:      duration.String(),
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// ResilienceResponse represents resilience metrics
type ResilienceResponse struct {
	CancelledCount  int       `json:"cancelled_count"`
	PausedCount     int       `json:"paused_count"`
	TimeoutCount    int       `json:"timeout_count"`
	DeadLetterCount int       `json:"dead_letter_count"`
	RecoveredCount  int       `json:"recovered_count"`
	WorkerFailures  int       `json:"worker_failures"`
	Timestamp       time.Time `json:"timestamp"`
}

// getResilience returns resilience metrics
func (h *OrchestrationHandler) getResilience(w http.ResponseWriter, r *http.Request) {
	if h.resilience == nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Resilience manager not available"})
		return
	}

	metrics := h.resilience.GetMetrics()

	response := ResilienceResponse{
		CancelledCount:  metrics.CancelledCount,
		PausedCount:     metrics.PausedCount,
		TimeoutCount:    metrics.TimeoutCount,
		DeadLetterCount: metrics.DeadLetterCount,
		RecoveredCount:  metrics.RecoveredCount,
		WorkerFailures:  metrics.WorkerFailures,
		Timestamp:       time.Now(),
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// DeadLetterQueueResponse represents a DLQ item
type DeadLetterQueueResponse struct {
	ID            string    `json:"id"`
	ExecutionID   string    `json:"execution_id"`
	CorrelationID string    `json:"correlation_id"`
	Reason        string    `json:"reason"`
	ErrorMessage  string    `json:"error_message"`
	RetryCount    int       `json:"retry_count"`
	MaxRetries    int       `json:"max_retries"`
	EnqueuedAt    time.Time `json:"enqueued_at"`
}

// getDeadLetterQueue returns all items in dead letter queue
func (h *OrchestrationHandler) getDeadLetterQueue(w http.ResponseWriter, r *http.Request) {
	if h.resilience == nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Resilience manager not available"})
		return
	}

	items := h.resilience.GetDeadLetterQueue()
	responses := make([]DeadLetterQueueResponse, len(items))

	for i, item := range items {
		responses[i] = DeadLetterQueueResponse{
			ID:            item.ID,
			ExecutionID:   item.ExecutionID,
			CorrelationID: item.CorrelationID,
			Reason:        item.Reason,
			ErrorMessage:  item.ErrorMessage,
			RetryCount:    item.RetryCount,
			MaxRetries:    item.MaxRetries,
			EnqueuedAt:    item.EnqueuedAt,
		}
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"items": responses,
		"count": len(responses),
	})
}
