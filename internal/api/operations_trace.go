package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/docker-secret-operator/dso/internal/execution"
)

// OperationsTraceHandler handles trace explorer endpoints
type OperationsTraceHandler struct {
	auditEvents *execution.ExecutionAuditEvents
}

// NewOperationsTraceHandler creates a new trace handler
func NewOperationsTraceHandler(auditEvents *execution.ExecutionAuditEvents) *OperationsTraceHandler {
	return &OperationsTraceHandler{
		auditEvents: auditEvents,
	}
}

// ServeHTTP handles trace explorer requests
func (h *OperationsTraceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	path := strings.TrimPrefix(r.URL.Path, "/api/operations/trace/")

	// GET /api/operations/trace/search
	if path == "search" && r.Method == http.MethodGet {
		h.searchTrace(w, r)
		return
	}

	// GET /api/operations/trace/{correlationID}
	if r.Method == http.MethodGet && path != "" {
		h.getTrace(w, r)
		return
	}

	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{"error": "Not found"})
}

// TraceEventResponse represents a trace event
type TraceEventResponse struct {
	ID            string    `json:"id"`
	CorrelationID string    `json:"correlation_id"`
	ExecutionID   string    `json:"execution_id"`
	Action        string    `json:"action"`
	Status        string    `json:"status"`
	Details       string    `json:"details"`
	Timestamp     time.Time `json:"timestamp"`
	DurationMs    int       `json:"duration_ms,omitempty"`
}

// TraceExplorerResponse represents a complete trace
type TraceExplorerResponse struct {
	CorrelationID     string                `json:"correlation_id"`
	ExecutionID       string                `json:"execution_id"`
	Status            string                `json:"status"`
	StartTime         time.Time             `json:"start_time"`
	EndTime           *time.Time            `json:"end_time,omitempty"`
	Duration          string                `json:"duration"`
	EventCount        int                   `json:"event_count"`
	Events            []*TraceEventResponse `json:"events"`
	Timeline          []*TimelineEvent      `json:"timeline"`
	StatusTransitions []*StatusTransition   `json:"status_transitions"`
	FailureDetails    *FailureDetails       `json:"failure_details,omitempty"`
}

// TimelineEvent represents an event in the timeline
type TimelineEvent struct {
	Time    time.Time `json:"time"`
	Action  string    `json:"action"`
	Details string    `json:"details"`
	Status  string    `json:"status"`
}

// StatusTransition represents a status transition
type StatusTransition struct {
	FromStatus string    `json:"from_status"`
	ToStatus   string    `json:"to_status"`
	Time       time.Time `json:"time"`
	Reason     string    `json:"reason"`
}

// FailureDetails represents failure information
type FailureDetails struct {
	Reason       string    `json:"reason"`
	ErrorMessage string    `json:"error_message"`
	Timestamp    time.Time `json:"timestamp"`
	FailedStep   string    `json:"failed_step,omitempty"`
}

// searchTrace searches for traces by correlation ID
func (h *OperationsTraceHandler) searchTrace(w http.ResponseWriter, r *http.Request) {
	correlationID := r.URL.Query().Get("correlation_id")

	if correlationID == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "correlation_id parameter required"})
		return
	}

	// Get all events for this correlation ID
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

	// Build response
	response := h.buildTraceResponse(correlationID, filteredEvents)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// getTrace retrieves a trace by correlation ID from URL path
func (h *OperationsTraceHandler) getTrace(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/operations/trace/")
	correlationID := path

	if correlationID == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Correlation ID required"})
		return
	}

	// Get all events for this correlation ID
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

	// Build response
	response := h.buildTraceResponse(correlationID, filteredEvents)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// buildTraceResponse builds a complete trace response
func (h *OperationsTraceHandler) buildTraceResponse(correlationID string, events []execution.OrchestrationAuditEvent) *TraceExplorerResponse {
	// Convert events to trace events
	traceEvents := make([]*TraceEventResponse, len(events))
	for i, event := range events {
		traceEvents[i] = &TraceEventResponse{
			ID:            event.ID,
			CorrelationID: event.CorrelationID,
			ExecutionID:   event.ExecutionID,
			Action:        event.Action,
			Status:        event.Status,
			Details:       event.Details,
			Timestamp:     event.Timestamp,
		}
	}

	// Determine execution ID
	executionID := ""
	if len(events) > 0 {
		executionID = events[0].ExecutionID
	}

	// Determine status and timing
	startTime := events[0].Timestamp
	endTime := events[len(events)-1].Timestamp
	duration := endTime.Sub(startTime)

	status := "running"
	failureDetails := (*FailureDetails)(nil)

	for _, event := range events {
		if strings.Contains(event.Action, "completed") {
			status = "completed"
			break
		} else if strings.Contains(event.Action, "failed") {
			status = "failed"
			failureDetails = &FailureDetails{
				Reason:    event.Details,
				Timestamp: event.Timestamp,
			}
			break
		} else if strings.Contains(event.Details, "Cancelled") {
			status = "cancelled"
			break
		} else if strings.Contains(event.Details, "Paused") {
			status = "paused"
			break
		}
	}

	// Build timeline
	timeline := make([]*TimelineEvent, len(events))
	for i, event := range events {
		timeline[i] = &TimelineEvent{
			Time:    event.Timestamp,
			Action:  event.Action,
			Details: event.Details,
			Status:  event.Status,
		}
	}

	// Build status transitions
	transitions := make([]*StatusTransition, 0)
	previousStatus := "pending"
	for _, event := range events {
		newStatus := previousStatus
		if strings.Contains(event.Action, "started") {
			newStatus = "started"
		} else if strings.Contains(event.Action, "completed") {
			newStatus = "completed"
		} else if strings.Contains(event.Action, "failed") {
			newStatus = "failed"
		}

		if newStatus != previousStatus {
			transitions = append(transitions, &StatusTransition{
				FromStatus: previousStatus,
				ToStatus:   newStatus,
				Time:       event.Timestamp,
				Reason:     event.Details,
			})
			previousStatus = newStatus
		}
	}

	response := &TraceExplorerResponse{
		CorrelationID:     correlationID,
		ExecutionID:       executionID,
		Status:            status,
		StartTime:         startTime,
		EndTime:           &endTime,
		Duration:          duration.String(),
		EventCount:        len(events),
		Events:            traceEvents,
		Timeline:          timeline,
		StatusTransitions: transitions,
		FailureDetails:    failureDetails,
	}

	return response
}
