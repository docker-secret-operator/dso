package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/docker-secret-operator/dso/internal/execution"
)

// OperationsDashboardHandler handles operations console endpoints
type OperationsDashboardHandler struct {
	dispatcher     *execution.Dispatcher
	workerManager  *execution.WorkerManager
	executionQueue *execution.ExecutionQueue
	auditEvents    *execution.ExecutionAuditEvents
	resilience     *execution.ResilienceManager
}

// NewOperationsDashboardHandler creates a new operations dashboard handler
func NewOperationsDashboardHandler(
	dispatcher *execution.Dispatcher,
	workerManager *execution.WorkerManager,
	executionQueue *execution.ExecutionQueue,
	auditEvents *execution.ExecutionAuditEvents,
	resilience *execution.ResilienceManager,
) *OperationsDashboardHandler {
	return &OperationsDashboardHandler{
		dispatcher:     dispatcher,
		workerManager:  workerManager,
		executionQueue: executionQueue,
		auditEvents:    auditEvents,
		resilience:     resilience,
	}
}

// ServeHTTP handles operations dashboard requests
func (h *OperationsDashboardHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	path := strings.TrimPrefix(r.URL.Path, "/api/operations/")

	// GET /api/operations/dashboard
	if path == "dashboard" && r.Method == http.MethodGet {
		h.getDashboard(w, r)
		return
	}

	// GET /api/operations/alerts
	if path == "alerts" && r.Method == http.MethodGet {
		h.getAlerts(w, r)
		return
	}

	// GET /api/operations/recovery-events
	if path == "recovery-events" && r.Method == http.MethodGet {
		h.getRecoveryEvents(w, r)
		return
	}

	// GET /api/operations/metrics-history
	if path == "metrics-history" && r.Method == http.MethodGet {
		h.getMetricsHistory(w, r)
		return
	}

	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{"error": "Not found"})
}

// DashboardResponse represents the operations dashboard
type DashboardResponse struct {
	Timestamp       time.Time           `json:"timestamp"`
	OverviewKPIs    OverviewKPIs        `json:"overview_kpis"`
	QueueHealth     QueueHealth         `json:"queue_health"`
	WorkerHealth    WorkerHealth        `json:"worker_health"`
	ExecutionStatus ExecutionStatusDist `json:"execution_status"`
	RecoveryStats   RecoveryStats       `json:"recovery_stats"`
	DLQStats        DLQStats            `json:"dlq_stats"`
	RecentFailures  []*FailureEvent     `json:"recent_failures"`
	SystemHealth    SystemHealth        `json:"system_health"`
}

// OverviewKPIs represents key performance indicators
type OverviewKPIs struct {
	SuccessRate       float64 `json:"success_rate"`
	FailureRate       float64 `json:"failure_rate"`
	AvgExecutionTime  string  `json:"avg_execution_time"`
	Throughput        float64 `json:"throughput_per_sec"`
	WorkerUtilization float64 `json:"worker_utilization"`
	TotalExecuted     int     `json:"total_executed"`
	TotalSucceeded    int     `json:"total_succeeded"`
	TotalFailed       int     `json:"total_failed"`
}

// QueueHealth represents queue status
type QueueHealth struct {
	Depth           int     `json:"depth"`
	OldestItemAge   string  `json:"oldest_item_age"`
	IncomingRate    float64 `json:"incoming_rate_per_sec"`
	CompletionRate  float64 `json:"completion_rate_per_sec"`
	HealthScore     int     `json:"health_score"` // 0-100
	Status          string  `json:"status"`       // healthy, warning, critical
	AverageWaitTime string  `json:"avg_wait_time"`
}

// WorkerHealth represents worker status
type WorkerHealth struct {
	TotalWorkers       int                   `json:"total_workers"`
	HealthyWorkers     int                   `json:"healthy_workers"`
	UnhealthyWorkers   int                   `json:"unhealthy_workers"`
	AverageCapacity    int                   `json:"avg_capacity"`
	AverageUtilization float64               `json:"avg_utilization"`
	HealthScore        int                   `json:"health_score"` // 0-100
	Status             string                `json:"status"`       // healthy, warning, critical
	Workers            []*WorkerHealthDetail `json:"workers"`
}

// WorkerHealthDetail represents individual worker status
type WorkerHealthDetail struct {
	ID             string    `json:"id"`
	State          string    `json:"state"`
	Healthy        bool      `json:"healthy"`
	Capacity       int       `json:"capacity"`
	Running        int       `json:"running"`
	Utilization    float64   `json:"utilization"`
	CompletedCount int       `json:"completed_count"`
	FailedCount    int       `json:"failed_count"`
	LastHeartbeat  time.Time `json:"last_heartbeat"`
}

// ExecutionStatusDist represents execution status distribution
type ExecutionStatusDist struct {
	Queued    int `json:"queued"`
	Running   int `json:"running"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
	Cancelled int `json:"cancelled"`
	Paused    int `json:"paused"`
	TimedOut  int `json:"timed_out"`
}

// RecoveryStats represents recovery event statistics
type RecoveryStats struct {
	WorkerFailures      int        `json:"worker_failures"`
	AutoRecoveries      int        `json:"auto_recoveries"`
	RecoverySuccessRate float64    `json:"recovery_success_rate"`
	LastRecoveryTime    *time.Time `json:"last_recovery_time"`
	CancelledCount      int        `json:"cancelled_count"`
	PausedCount         int        `json:"paused_count"`
}

// DLQStats represents dead letter queue statistics
type DLQStats struct {
	TotalItems     int            `json:"total_items"`
	GrowthRate     float64        `json:"growth_rate_per_hour"`
	OldestItemAge  string         `json:"oldest_item_age"`
	FailureReasons map[string]int `json:"failure_reasons"`
	Status         string         `json:"status"` // healthy, warning, critical
}

// FailureEvent represents a failure event
type FailureEvent struct {
	ID            string    `json:"id"`
	ExecutionID   string    `json:"execution_id"`
	CorrelationID string    `json:"correlation_id"`
	Reason        string    `json:"reason"`
	Timestamp     time.Time `json:"timestamp"`
	WorkerID      string    `json:"worker_id,omitempty"`
}

// SystemHealth represents overall system health
type SystemHealth struct {
	OverallScore  int    `json:"overall_score"` // 0-100
	Status        string `json:"status"`        // healthy, warning, critical
	AlertCount    int    `json:"alert_count"`
	CriticalCount int    `json:"critical_count"`
}

// getDashboard returns the operations dashboard
func (h *OperationsDashboardHandler) getDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	metrics := h.dispatcher.GetMetrics(ctx)
	workers, _ := h.workerManager.ListWorkers(ctx)
	healthyWorkers, _ := h.workerManager.GetHealthyWorkers(ctx)
	resilience := h.resilience.GetMetrics()

	// Calculate KPIs
	totalExecuted := metrics.CompletedCount + metrics.FailedCount
	successRate := 0.0
	if totalExecuted > 0 {
		successRate = float64(metrics.CompletedCount) / float64(totalExecuted)
	}

	failureRate := 0.0
	if totalExecuted > 0 {
		failureRate = float64(metrics.FailedCount) / float64(totalExecuted)
	}

	// Calculate worker utilization
	workerUtilization := 0.0
	if len(workers) > 0 {
		totalCapacity := 0
		totalRunning := 0
		for _, w := range workers {
			totalCapacity += w.MaxConcurrent
			totalRunning += w.CurrentlyRunning
		}
		if totalCapacity > 0 {
			workerUtilization = float64(totalRunning) / float64(totalCapacity)
		}
	}

	// Queue health
	queueDepth := metrics.QueuedCount
	queueStatus := "healthy"
	queueScore := 100
	if queueDepth > 500 {
		queueStatus = "warning"
		queueScore = 60
	}
	if queueDepth > 1000 {
		queueStatus = "critical"
		queueScore = 20
	}

	// Worker health
	unhealthyCount := len(workers) - len(healthyWorkers)
	workerStatus := "healthy"
	workerScore := 100
	if unhealthyCount > 0 {
		workerStatus = "warning"
		workerScore = 80 - (unhealthyCount * 10)
	}
	if len(healthyWorkers) == 0 {
		workerStatus = "critical"
		workerScore = 0
	}

	// Overall system health
	overallScore := (queueScore + workerScore + int(successRate*100)) / 3
	systemStatus := "healthy"
	if overallScore < 70 {
		systemStatus = "warning"
	}
	if overallScore < 40 {
		systemStatus = "critical"
	}

	// Build worker details
	workerDetails := make([]*WorkerHealthDetail, len(workers))
	for i, w := range workers {
		healthy := false
		for _, hw := range healthyWorkers {
			if hw.ID == w.ID {
				healthy = true
				break
			}
		}

		utilization := 0.0
		if w.MaxConcurrent > 0 {
			utilization = float64(w.CurrentlyRunning) / float64(w.MaxConcurrent)
		}

		workerDetails[i] = &WorkerHealthDetail{
			ID:             w.ID,
			State:          string(w.State),
			Healthy:        healthy,
			Capacity:       w.MaxConcurrent,
			Running:        w.CurrentlyRunning,
			Utilization:    utilization,
			CompletedCount: w.CompletedCount,
			FailedCount:    w.FailedCount,
			LastHeartbeat:  w.LastHeartbeat,
		}
	}

	// Get recent failures (last 10 audit events that are failures)
	recentFailures := make([]*FailureEvent, 0)
	allEvents := h.auditEvents.ListEvents()
	for i := len(allEvents) - 1; i >= 0 && len(recentFailures) < 10; i-- {
		event := allEvents[i]
		if strings.Contains(event.Action, "failed") || strings.Contains(event.Action, "timeout") {
			recentFailures = append(recentFailures, &FailureEvent{
				ID:            event.ID,
				ExecutionID:   event.ExecutionID,
				CorrelationID: event.CorrelationID,
				Reason:        event.Details,
				Timestamp:     event.Timestamp,
			})
		}
	}

	// DLQ stats
	dlqItems := h.resilience.GetDeadLetterQueue()
	dlqStatus := "healthy"
	if len(dlqItems) > 50 {
		dlqStatus = "warning"
	}
	if len(dlqItems) > 200 {
		dlqStatus = "critical"
	}

	response := DashboardResponse{
		Timestamp: time.Now(),
		OverviewKPIs: OverviewKPIs{
			SuccessRate:       successRate,
			FailureRate:       failureRate,
			AvgExecutionTime:  metrics.AverageDuration.String(),
			Throughput:        metrics.ThroughputPerSec,
			WorkerUtilization: workerUtilization,
			TotalExecuted:     totalExecuted,
			TotalSucceeded:    metrics.CompletedCount,
			TotalFailed:       metrics.FailedCount,
		},
		QueueHealth: QueueHealth{
			Depth:           queueDepth,
			IncomingRate:    metrics.ThroughputPerSec,
			CompletionRate:  metrics.ThroughputPerSec,
			HealthScore:     queueScore,
			Status:          queueStatus,
			AverageWaitTime: "30s", // Placeholder
		},
		WorkerHealth: WorkerHealth{
			TotalWorkers:       len(workers),
			HealthyWorkers:     len(healthyWorkers),
			UnhealthyWorkers:   unhealthyCount,
			AverageCapacity:    5, // Placeholder
			AverageUtilization: workerUtilization,
			HealthScore:        workerScore,
			Status:             workerStatus,
			Workers:            workerDetails,
		},
		ExecutionStatus: ExecutionStatusDist{
			Queued:    metrics.QueuedCount,
			Running:   metrics.ActiveCount,
			Completed: metrics.CompletedCount,
			Failed:    metrics.FailedCount,
			Cancelled: resilience.CancelledCount,
			Paused:    resilience.PausedCount,
			TimedOut:  resilience.TimeoutCount,
		},
		RecoveryStats: RecoveryStats{
			WorkerFailures:      resilience.WorkerFailures,
			AutoRecoveries:      metrics.ActiveCount, // Placeholder
			RecoverySuccessRate: 0.95,                // Placeholder
			CancelledCount:      resilience.CancelledCount,
			PausedCount:         resilience.PausedCount,
		},
		DLQStats: DLQStats{
			TotalItems:    resilience.DeadLetterCount,
			GrowthRate:    0.5,  // Placeholder
			OldestItemAge: "2h", // Placeholder
			Status:        dlqStatus,
		},
		RecentFailures: recentFailures,
		SystemHealth: SystemHealth{
			OverallScore:  overallScore,
			Status:        systemStatus,
			AlertCount:    0, // Will be populated by alert engine
			CriticalCount: 0,
		},
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// AlertResponse represents an alert
type AlertResponse struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`     // queue_depth, worker_unhealthy, failure_rate, timeout_rate, dlq_growth, recovery_spike
	Severity  string    `json:"severity"` // info, warning, critical
	Message   string    `json:"message"`
	Value     float64   `json:"value"`
	Threshold float64   `json:"threshold"`
	Timestamp time.Time `json:"timestamp"`
	Dismissed bool      `json:"dismissed"`
}

// getAlerts returns active alerts
func (h *OperationsDashboardHandler) getAlerts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	alerts := make([]*AlertResponse, 0)
	now := time.Now()

	// Evaluate alert conditions
	metrics := h.dispatcher.GetMetrics(ctx)
	workers, _ := h.workerManager.ListWorkers(ctx)
	healthyWorkers, _ := h.workerManager.GetHealthyWorkers(ctx)
	resilience := h.resilience.GetMetrics()

	// Queue depth alert
	if metrics.QueuedCount > 1000 {
		alerts = append(alerts, &AlertResponse{
			ID:        fmt.Sprintf("alert-queue-depth-%d", now.Unix()),
			Type:      "queue_depth",
			Severity:  "critical",
			Message:   fmt.Sprintf("Queue depth exceeded threshold: %d > 1000", metrics.QueuedCount),
			Value:     float64(metrics.QueuedCount),
			Threshold: 1000,
			Timestamp: now,
		})
	}

	// Worker health alert
	unhealthyCount := len(workers) - len(healthyWorkers)
	if unhealthyCount > 0 {
		alerts = append(alerts, &AlertResponse{
			ID:        fmt.Sprintf("alert-workers-unhealthy-%d", now.Unix()),
			Type:      "worker_unhealthy",
			Severity:  "warning",
			Message:   fmt.Sprintf("%d workers unhealthy", unhealthyCount),
			Value:     float64(unhealthyCount),
			Threshold: 1,
			Timestamp: now,
		})
	}

	// Failure rate alert
	total := metrics.CompletedCount + metrics.FailedCount
	if total > 0 {
		failureRate := float64(metrics.FailedCount) / float64(total)
		if failureRate > 0.1 {
			alerts = append(alerts, &AlertResponse{
				ID:        fmt.Sprintf("alert-failure-rate-%d", now.Unix()),
				Type:      "failure_rate",
				Severity:  "warning",
				Message:   fmt.Sprintf("Failure rate exceeded threshold: %.1f%% > 10%%", failureRate*100),
				Value:     failureRate * 100,
				Threshold: 10,
				Timestamp: now,
			})
		}
	}

	// DLQ growth alert
	if resilience.DeadLetterCount > 100 {
		alerts = append(alerts, &AlertResponse{
			ID:        fmt.Sprintf("alert-dlq-growth-%d", now.Unix()),
			Type:      "dlq_growth",
			Severity:  "warning",
			Message:   fmt.Sprintf("Dead letter queue growth: %d items", resilience.DeadLetterCount),
			Value:     float64(resilience.DeadLetterCount),
			Threshold: 100,
			Timestamp: now,
		})
	}

	// Timeout rate alert
	if resilience.TimeoutCount > 50 {
		alerts = append(alerts, &AlertResponse{
			ID:        fmt.Sprintf("alert-timeout-rate-%d", now.Unix()),
			Type:      "timeout_rate",
			Severity:  "warning",
			Message:   fmt.Sprintf("Timeout rate exceeded: %d timeouts", resilience.TimeoutCount),
			Value:     float64(resilience.TimeoutCount),
			Threshold: 50,
			Timestamp: now,
		})
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"alerts": alerts,
		"count":  len(alerts),
	})
}

// RecoveryEventResponse represents a recovery event
type RecoveryEventResponse struct {
	ID            string    `json:"id"`
	Type          string    `json:"type"` // worker_failure, queue_recovery, execution_cancelled, execution_paused
	ExecutionID   string    `json:"execution_id,omitempty"`
	CorrelationID string    `json:"correlation_id,omitempty"`
	WorkerID      string    `json:"worker_id,omitempty"`
	Details       string    `json:"details"`
	Timestamp     time.Time `json:"timestamp"`
}

// getRecoveryEvents returns recovery events
func (h *OperationsDashboardHandler) getRecoveryEvents(w http.ResponseWriter, r *http.Request) {
	events := make([]*RecoveryEventResponse, 0)

	// Get audit events that indicate recovery
	allEvents := h.auditEvents.ListEvents()
	for _, event := range allEvents {
		if strings.Contains(event.Action, "worker") ||
			strings.Contains(event.Action, "queue") ||
			strings.Contains(event.Details, "Cancelled") ||
			strings.Contains(event.Details, "Paused") {
			eventType := "unknown"
			if strings.Contains(event.Action, "worker") {
				eventType = "worker_failure"
			} else if strings.Contains(event.Details, "Cancelled") {
				eventType = "execution_cancelled"
			} else if strings.Contains(event.Details, "Paused") {
				eventType = "execution_paused"
			}

			events = append(events, &RecoveryEventResponse{
				ID:            event.ID,
				Type:          eventType,
				ExecutionID:   event.ExecutionID,
				CorrelationID: event.CorrelationID,
				Details:       event.Details,
				Timestamp:     event.Timestamp,
			})
		}
	}

	// Return most recent first
	for i := 0; i < len(events)/2; i++ {
		events[i], events[len(events)-1-i] = events[len(events)-1-i], events[i]
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"events": events,
		"count":  len(events),
	})
}

// OpsMetricsSnapshot represents historical metrics
type OpsMetricsSnapshot struct {
	Timestamp         time.Time `json:"timestamp"`
	SuccessRate       float64   `json:"success_rate"`
	FailureRate       float64   `json:"failure_rate"`
	Throughput        float64   `json:"throughput_per_sec"`
	QueueDepth        int       `json:"queue_depth"`
	WorkerUtilization float64   `json:"worker_utilization"`
	DLQCount          int       `json:"dlq_count"`
}

// getMetricsHistory returns historical metrics snapshots
func (h *OperationsDashboardHandler) getMetricsHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Return current metrics as a single snapshot
	// (Historical storage would be added in Phase 4.5C)
	metrics := h.dispatcher.GetMetrics(ctx)
	workers, _ := h.workerManager.ListWorkers(ctx)

	workerUtilization := 0.0
	if len(workers) > 0 {
		totalCapacity := 0
		totalRunning := 0
		for _, w := range workers {
			totalCapacity += w.MaxConcurrent
			totalRunning += w.CurrentlyRunning
		}
		if totalCapacity > 0 {
			workerUtilization = float64(totalRunning) / float64(totalCapacity)
		}
	}

	successRate := 0.0
	failureRate := 0.0
	total := metrics.CompletedCount + metrics.FailedCount
	if total > 0 {
		successRate = float64(metrics.CompletedCount) / float64(total)
		failureRate = float64(metrics.FailedCount) / float64(total)
	}

	resilience := h.resilience.GetMetrics()

	snapshot := OpsMetricsSnapshot{
		Timestamp:         time.Now(),
		SuccessRate:       successRate,
		FailureRate:       failureRate,
		Throughput:        metrics.ThroughputPerSec,
		QueueDepth:        metrics.QueuedCount,
		WorkerUtilization: workerUtilization,
		DLQCount:          resilience.DeadLetterCount,
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"snapshots": []*OpsMetricsSnapshot{&snapshot},
	})
}
