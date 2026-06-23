package api

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
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
	ActorEmail    string    `json:"actor_email"`
	Resource      string    `json:"resource"`
	ResourceID    string    `json:"resource_id"`
	ResourceType  string    `json:"resource_type"`
	Status        string    `json:"status"`
	Severity      string    `json:"severity"`
	Details       string    `json:"details"`
	IPAddress     string    `json:"ip_address"`
	Timestamp     time.Time `json:"timestamp"`
}

// AuditExplorerResponse represents audit events list
type AuditExplorerResponse struct {
	Total     int                   `json:"total"`
	Count     int                   `json:"count"`
	Offset    int                   `json:"offset"`
	Limit     int                   `json:"limit"`
	Events    []*AuditEventResponse `json:"events"`
	Timestamp time.Time             `json:"timestamp"`
}

// CorrelationChainResponse is the full story of a correlation ID
type CorrelationChainResponse struct {
	CorrelationID string                `json:"correlation_id"`
	Count         int                   `json:"count"`
	Events        []*AuditEventResponse `json:"events"`
	Timestamp     time.Time             `json:"timestamp"`
}

// ActorTimelineResponse is the activity timeline of one actor
type ActorTimelineResponse struct {
	ActorID   string                `json:"actor_id"`
	ActorName string                `json:"actor_name"`
	Period    string                `json:"period"`
	Count     int                   `json:"count"`
	Events    []*AuditEventResponse `json:"events"`
	Timestamp time.Time             `json:"timestamp"`
}

// ServeHTTP routes audit requests
func (h *AuditExplorerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	path := strings.TrimPrefix(r.URL.Path, "/api/audit")

	switch {
	case path == "/export":
		h.handleExport(w, r)
	case strings.HasPrefix(path, "/correlation/"):
		id := strings.TrimPrefix(path, "/correlation/")
		if id == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "missing correlation_id"})
			return
		}
		h.handleCorrelationChain(w, r, id)
	case strings.HasPrefix(path, "/actors/"):
		id := strings.TrimPrefix(path, "/actors/")
		if id == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "missing actor_id"})
			return
		}
		h.handleActorTimeline(w, r, id)
	default:
		h.handleList(w, r)
	}
}

// handleList serves GET /api/audit with full filter support
func (h *AuditExplorerHandler) handleList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	q := r.URL.Query()
	correlationID := q.Get("correlation_id")
	executionID := q.Get("execution_id")
	action := q.Get("action")
	actor := q.Get("actor")
	actorID := q.Get("actor_id")
	resource := q.Get("resource")
	resourceID := q.Get("resource_id")
	resourceType := q.Get("resource_type")
	startTime := q.Get("start_time")
	endTime := q.Get("end_time")

	limit, offset := parseLimitOffset(q.Get("limit"), q.Get("offset"), 50, 1000)

	where, args := buildAuditWhere(correlationID, executionID, action, actor, actorID, resource, resourceID, resourceType, startTime, endTime)

	total := 0
	countQ := "SELECT COUNT(*) FROM audit_events WHERE 1=1" + where
	_ = h.db.QueryRowContext(ctx, countQ, args...).Scan(&total)

	listQ := auditSelectCols + " FROM audit_events WHERE 1=1" + where + " ORDER BY timestamp DESC LIMIT ? OFFSET ?"
	listArgs := append(args, limit, offset)

	rows, err := h.db.QueryContext(ctx, listQ, listArgs...)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	events := scanAuditRows(rows)
	json.NewEncoder(w).Encode(&AuditExplorerResponse{
		Total:     total,
		Count:     len(events),
		Offset:    offset,
		Limit:     limit,
		Events:    events,
		Timestamp: time.Now(),
	})
}

// handleCorrelationChain serves GET /api/audit/correlation/{id}
// Returns a complete execution story: audit events + execution events + review activities + approvals.
func (h *AuditExplorerHandler) handleCorrelationChain(w http.ResponseWriter, r *http.Request, correlationID string) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// 1. Pull actor-attributed audit events.
	mainQ := auditSelectCols + ` FROM audit_events WHERE correlation_id = ? ORDER BY timestamp ASC`
	rows, err := h.db.QueryContext(ctx, mainQ, correlationID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	events := scanAuditRows(rows)
	rows.Close()

	// Collect review IDs seen in the audit chain so we can pull review/approval events.
	reviewIDs := make([]string, 0)
	for _, e := range events {
		if e.ResourceType == "review" && e.ResourceID != "" {
			reviewIDs = append(reviewIDs, e.ResourceID)
		}
	}

	// 2. Execution events from execution_audit_events.
	execQ := `SELECT id, correlation_id, execution_id, action, status, COALESCE(details,''),
		COALESCE(resource_id,''), COALESCE(resource_type,'execution'), timestamp
		FROM execution_audit_events WHERE correlation_id = ? ORDER BY timestamp ASC`
	if execRows, err := h.db.QueryContext(ctx, execQ, correlationID); err == nil {
		for execRows.Next() {
			var ev AuditEventResponse
			var ts time.Time
			if err := execRows.Scan(&ev.ID, &ev.CorrelationID, &ev.ExecutionID, &ev.Action, &ev.Status,
				&ev.Details, &ev.ResourceID, &ev.ResourceType, &ts); err == nil {
				ev.Timestamp = ts
				ev.Actor = "system"
				ev.Severity = "info"
				events = append(events, &ev)
			}
		}
		execRows.Close()
	}

	// 3. Review activities for any review touched by this correlation.
	if len(reviewIDs) > 0 {
		placeholders := strings.Repeat("?,", len(reviewIDs))
		placeholders = strings.TrimSuffix(placeholders, ",")
		raQ := `SELECT id, review_id, type, actor_id, description, timestamp
			FROM review_activities WHERE review_id IN (` + placeholders + `) ORDER BY timestamp ASC`
		raArgs := make([]interface{}, len(reviewIDs))
		for i, id := range reviewIDs {
			raArgs[i] = id
		}
		if raRows, err := h.db.QueryContext(ctx, raQ, raArgs...); err == nil {
			for raRows.Next() {
				var ev AuditEventResponse
				var ts time.Time
				var reviewID string
				if err := raRows.Scan(&ev.ID, &reviewID, &ev.Action, &ev.ActorID, &ev.Details, &ts); err == nil {
					ev.Timestamp = ts
					ev.CorrelationID = correlationID
					ev.Resource = "review"
					ev.ResourceID = reviewID
					ev.ResourceType = "review_activity"
					ev.Status = "success"
					ev.Severity = "info"
					events = append(events, &ev)
				}
			}
			raRows.Close()
		}

		// 4. Approval decisions for those reviews.
		// Scan the timestamp as a string: COALESCE loses the DATETIME type in go-sqlite3.
		appQ := `SELECT id, review_id, reviewer_id, reviewer_name, decision,
			COALESCE(comments,''), COALESCE(decided_at, created_at)
			FROM approvals WHERE review_id IN (` + placeholders + `) ORDER BY created_at ASC`
		if appRows, err := h.db.QueryContext(ctx, appQ, raArgs...); err == nil {
			for appRows.Next() {
				var ev AuditEventResponse
				var tsStr string
				var reviewID string
				if err := appRows.Scan(&ev.ID, &reviewID, &ev.ActorID, &ev.Actor,
					&ev.Status, &ev.Details, &tsStr); err == nil {
					ev.Timestamp = parseFlexTime(tsStr)
					ev.CorrelationID = correlationID
					ev.Action = "approval." + ev.Status
					ev.Resource = "review"
					ev.ResourceID = reviewID
					ev.ResourceType = "approval"
					ev.Severity = "info"
					events = append(events, &ev)
				}
			}
			appRows.Close()
		}
	}

	// Sort all merged events chronologically.
	sortAuditEvents(events)

	json.NewEncoder(w).Encode(&CorrelationChainResponse{
		CorrelationID: correlationID,
		Count:         len(events),
		Events:        events,
		Timestamp:     time.Now(),
	})
}

// handleActorTimeline serves GET /api/audit/actors/{id}
// Supports period=24h (default), 7d, 30d.
func (h *AuditExplorerHandler) handleActorTimeline(w http.ResponseWriter, r *http.Request, actorID string) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	period := r.URL.Query().Get("period")
	var cutoff time.Time
	switch period {
	case "7d":
		cutoff = time.Now().Add(-7 * 24 * time.Hour)
	case "30d":
		cutoff = time.Now().Add(-30 * 24 * time.Hour)
	default:
		cutoff = time.Now().Add(-24 * time.Hour)
		period = "24h"
	}

	q := auditSelectCols + ` FROM audit_events WHERE actor_id = ? AND timestamp >= ? ORDER BY timestamp DESC LIMIT 500`
	rows, err := h.db.QueryContext(ctx, q, actorID, cutoff)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	events := scanAuditRows(rows)

	actorName := ""
	if len(events) > 0 {
		actorName = events[0].Actor
	}

	json.NewEncoder(w).Encode(&ActorTimelineResponse{
		ActorID:   actorID,
		ActorName: actorName,
		Period:    period,
		Count:     len(events),
		Events:    events,
		Timestamp: time.Now(),
	})
}

// handleExport serves GET /api/audit/export
func (h *AuditExplorerHandler) handleExport(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	q := r.URL.Query()
	correlationID := q.Get("correlation_id")
	executionID := q.Get("execution_id")
	action := q.Get("action")
	actor := q.Get("actor")
	actorID := q.Get("actor_id")
	resource := q.Get("resource")
	resourceID := q.Get("resource_id")
	resourceType := q.Get("resource_type")
	startTime := q.Get("start_time")
	endTime := q.Get("end_time")
	format := q.Get("format")
	if format != "csv" {
		format = "json"
	}

	where, args := buildAuditWhere(correlationID, executionID, action, actor, actorID, resource, resourceID, resourceType, startTime, endTime)
	listQ := auditSelectCols + " FROM audit_events WHERE 1=1" + where + " ORDER BY timestamp DESC LIMIT 10000"

	rows, err := h.db.QueryContext(ctx, listQ, args...)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	events := scanAuditRows(rows)

	ts := time.Now().Format("20060102-150405")
	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="audit-export-%s.csv"`, ts))
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"id", "timestamp", "actor", "actor_id", "actor_email", "action", "resource", "resource_id", "resource_type", "status", "severity", "details", "correlation_id", "execution_id", "ip_address"})
		for _, e := range events {
			_ = cw.Write([]string{
				e.ID, e.Timestamp.Format(time.RFC3339), e.Actor, e.ActorID, e.ActorEmail,
				e.Action, e.Resource, e.ResourceID, e.ResourceType, e.Status, e.Severity,
				e.Details, e.CorrelationID, e.ExecutionID, e.IPAddress,
			})
		}
		cw.Flush()
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="audit-export-%s.json"`, ts))
	json.NewEncoder(w).Encode(map[string]interface{}{
		"exported_at": time.Now(),
		"count":       len(events),
		"events":      events,
	})
}

// ── helpers ──────────────────────────────────────────────────────────────────

func buildAuditWhere(correlationID, executionID, action, actor, actorID, resource, resourceID, resourceType, startTime, endTime string) (string, []interface{}) {
	var sb strings.Builder
	var args []interface{}

	if correlationID != "" {
		sb.WriteString(" AND correlation_id = ?")
		args = append(args, correlationID)
	}
	if executionID != "" {
		// audit_events stores execution_id inside resource_id when resource_type='execution'
		sb.WriteString(" AND (resource_id = ? AND resource_type = 'execution')")
		args = append(args, executionID)
	}
	if action != "" {
		sb.WriteString(" AND action = ?")
		args = append(args, action)
	}
	if actor != "" {
		sb.WriteString(" AND actor_name = ?")
		args = append(args, actor)
	}
	if actorID != "" {
		sb.WriteString(" AND actor_id = ?")
		args = append(args, actorID)
	}
	if resource != "" {
		sb.WriteString(" AND resource = ?")
		args = append(args, resource)
	}
	if resourceID != "" {
		sb.WriteString(" AND resource_id = ?")
		args = append(args, resourceID)
	}
	if resourceType != "" {
		sb.WriteString(" AND resource_type = ?")
		args = append(args, resourceType)
	}
	// Parse time strings to time.Time so go-sqlite3 uses native timestamp comparison.
	// String-based RFC3339 comparisons against stored DATETIME values are unreliable.
	if startTime != "" {
		if t, err := time.Parse(time.RFC3339, startTime); err == nil {
			sb.WriteString(" AND timestamp >= ?")
			args = append(args, t)
		}
	}
	if endTime != "" {
		if t, err := time.Parse(time.RFC3339, endTime); err == nil {
			sb.WriteString(" AND timestamp <= ?")
			args = append(args, t)
		}
	}
	return sb.String(), args
}

// auditSelectCols is the canonical SELECT column list for audit_events.
// ExecutionID is derived post-scan: when resource_type='execution', resource_id IS the execution ID.
const auditSelectCols = `SELECT id, COALESCE(correlation_id,''), action, actor_name, actor_id,
	COALESCE(actor_email,''), resource, resource_id, resource_type, status,
	COALESCE(severity,'info'), COALESCE(result_message,''), COALESCE(ip_address,''), timestamp`

func scanAuditRows(rows *sql.Rows) []*AuditEventResponse {
	events := make([]*AuditEventResponse, 0)
	for rows.Next() {
		var ev AuditEventResponse
		var ts time.Time
		if err := rows.Scan(&ev.ID, &ev.CorrelationID, &ev.Action, &ev.Actor, &ev.ActorID,
			&ev.ActorEmail, &ev.Resource, &ev.ResourceID, &ev.ResourceType,
			&ev.Status, &ev.Severity, &ev.Details, &ev.IPAddress, &ts); err != nil {
			continue
		}
		ev.Timestamp = ts
		// Derive ExecutionID: audit_events stores it as resource_id when resource_type='execution'.
		if ev.ResourceType == "execution" {
			ev.ExecutionID = ev.ResourceID
		}
		events = append(events, &ev)
	}
	return events
}

func sortAuditEvents(events []*AuditEventResponse) {
	// insertion sort — event counts are small
	for i := 1; i < len(events); i++ {
		key := events[i]
		j := i - 1
		for j >= 0 && events[j].Timestamp.After(key.Timestamp) {
			events[j+1] = events[j]
			j--
		}
		events[j+1] = key
	}
}

// parseFlexTime parses a timestamp string in multiple formats that SQLite may return
// (e.g. from COALESCE expressions that lose DATETIME type information).
func parseFlexTime(s string) time.Time {
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999+07:00",
		"2006-01-02 15:04:05.999999999+00:00",
		"2006-01-02 15:04:05.999999999 +0000 UTC",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func parseLimitOffset(limitStr, offsetStr string, def, max int) (int, int) {
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 || limit > max {
		limit = def
	}
	offset, _ := strconv.Atoi(offsetStr)
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}
