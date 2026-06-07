package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// AuditExplorerHandler handles audit event exploration
type AuditExplorerHandler struct {
	db *sql.DB
}

// NewAuditExplorerHandler creates a new audit explorer handler
func NewAuditExplorerHandler(db *sql.DB) *AuditExplorerHandler {
	return &AuditExplorerHandler{db: db}
}

// AuditEventResponse represents an audit event
type AuditEventResponse struct {
	ID            string    `json:"id"`
	CorrelationID string    `json:"correlation_id"`
	ExecutionID   string    `json:"execution_id"`
	Action        string    `json:"action"`
	Actor         string    `json:"actor"`
	ActorID       string    `json:"actor_id"`
	Resource      string    `json:"resource"`
	ResourceID    string    `json:"resource_id"`
	ResourceType  string    `json:"resource_type"`
	Status        string    `json:"status"`
	Details       string    `json:"details"`
	Timestamp     time.Time `json:"timestamp"`
}

// AuditExplorerResponse represents audit events list
type AuditExplorerResponse struct {
	Total      int                   `json:"total"`
	Count      int                   `json:"count"`
	Offset     int                   `json:"offset"`
	Limit      int                   `json:"limit"`
	Events     []*AuditEventResponse `json:"events"`
	Timestamp  time.Time             `json:"timestamp"`
}

// ServeHTTP handles audit explorer requests
func (h *AuditExplorerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Parse query parameters
	correlationID := r.URL.Query().Get("correlation_id")
	action := r.URL.Query().Get("action")
	actor := r.URL.Query().Get("actor")
	resource := r.URL.Query().Get("resource")
	startTime := r.URL.Query().Get("start_time")
	endTime := r.URL.Query().Get("end_time")

	limitStr := r.URL.Query().Get("limit")
	if limitStr == "" {
		limitStr = "50"
	}
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 || limit > 1000 {
		limit = 50
	}

	offsetStr := r.URL.Query().Get("offset")
	offset, _ := strconv.Atoi(offsetStr)
	if offset < 0 {
		offset = 0
	}

	// Build query
	query := "SELECT id, COALESCE(correlation_id, ''), COALESCE(execution_id, ''), action, actor_name, actor_id, resource, resource_id, resource_type, status, COALESCE(result_message, ''), timestamp FROM audit_events WHERE 1=1"
	args := []interface{}{}

	if correlationID != "" {
		query += " AND correlation_id = ?"
		args = append(args, correlationID)
	}
	if action != "" {
		query += " AND action = ?"
		args = append(args, action)
	}
	if actor != "" {
		query += " AND actor_name = ?"
		args = append(args, actor)
	}
	if resource != "" {
		query += " AND resource = ?"
		args = append(args, resource)
	}

	if startTime != "" {
		query += " AND timestamp >= ?"
		args = append(args, startTime)
	}
	if endTime != "" {
		query += " AND timestamp <= ?"
		args = append(args, endTime)
	}

	// Count total
	countQuery := "SELECT COUNT(*) FROM audit_events WHERE 1=1"
	countArgs := []interface{}{}

	if correlationID != "" {
		countQuery += " AND correlation_id = ?"
		countArgs = append(countArgs, correlationID)
	}
	if action != "" {
		countQuery += " AND action = ?"
		countArgs = append(countArgs, action)
	}
	if actor != "" {
		countQuery += " AND actor_name = ?"
		countArgs = append(countArgs, actor)
	}
	if resource != "" {
		countQuery += " AND resource = ?"
		countArgs = append(countArgs, resource)
	}
	if startTime != "" {
		countQuery += " AND timestamp >= ?"
		countArgs = append(countArgs, startTime)
	}
	if endTime != "" {
		countQuery += " AND timestamp <= ?"
		countArgs = append(countArgs, endTime)
	}

	var total int
	err := h.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&total)
	if err != nil && err != sql.ErrNoRows {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Sort by timestamp descending (newest first) and paginate
	query += " ORDER BY timestamp DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	events := make([]*AuditEventResponse, 0)
	for rows.Next() {
		var evt AuditEventResponse
		err := rows.Scan(
			&evt.ID,
			&evt.CorrelationID,
			&evt.ExecutionID,
			&evt.Action,
			&evt.Actor,
			&evt.ActorID,
			&evt.Resource,
			&evt.ResourceID,
			&evt.ResourceType,
			&evt.Status,
			&evt.Details,
			&evt.Timestamp,
		)
		if err != nil {
			continue
		}
		events = append(events, &evt)
	}

	response := &AuditExplorerResponse{
		Total:     total,
		Count:     len(events),
		Offset:    offset,
		Limit:     limit,
		Events:    events,
		Timestamp: time.Now(),
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
