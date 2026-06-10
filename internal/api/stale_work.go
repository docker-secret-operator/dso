package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/docker-secret-operator/dso/internal/execution"
)

// StaleWorkHandler handles stale work detection
type StaleWorkHandler struct {
	db         *sql.DB
	resilience *execution.ResilienceManager
}

// NewStaleWorkHandler creates a new stale work handler
func NewStaleWorkHandler(db *sql.DB, resilience *execution.ResilienceManager) *StaleWorkHandler {
	return &StaleWorkHandler{
		db:         db,
		resilience: resilience,
	}
}

// StaleWorkCategory represents a category of stale work
type StaleWorkCategory struct {
	Category      string           `json:"category"`
	Count         int              `json:"count"`
	OldestItemAge string           `json:"oldest_item_age"`
	AgeMs         int64            `json:"age_ms"`
	Threshold     string           `json:"threshold"`
	Status        string           `json:"status"` // normal, warning, critical
	Items         []*StaleWorkItem `json:"items"`
}

// StaleWorkItem represents a single stale item
type StaleWorkItem struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Details   string    `json:"details"`
	Age       string    `json:"age"`
	AgeMs     int64     `json:"age_ms"`
	CreatedAt time.Time `json:"created_at"`
}

// StaleWorkResponse represents stale work status
type StaleWorkResponse struct {
	Timestamp  time.Time            `json:"timestamp"`
	Total      int                  `json:"total"`
	Categories []*StaleWorkCategory `json:"categories"`
}

// ServeHTTP handles stale work requests
func (h *StaleWorkHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	response, err := h.getStaleWork(ctx)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// getStaleWork identifies all stale work
func (h *StaleWorkHandler) getStaleWork(ctx context.Context) (*StaleWorkResponse, error) {
	now := time.Now()
	response := &StaleWorkResponse{
		Timestamp:  now,
		Categories: make([]*StaleWorkCategory, 0, 5),
	}

	// Reviews pending >24h
	reviewsCategory, err := h.getStaleReviews(ctx, now)
	if err == nil {
		response.Categories = append(response.Categories, reviewsCategory)
		response.Total += reviewsCategory.Count
	}

	// Approvals pending >24h
	approvalsCategory, err := h.getStaleApprovals(ctx, now)
	if err == nil {
		response.Categories = append(response.Categories, approvalsCategory)
		response.Total += approvalsCategory.Count
	}

	// Executions queued >30m
	queuedCategory, err := h.getStaleQueuedExecutions(ctx, now)
	if err == nil {
		response.Categories = append(response.Categories, queuedCategory)
		response.Total += queuedCategory.Count
	}

	// Executions paused >24h
	pausedCategory, err := h.getStalePausedExecutions(ctx, now)
	if err == nil {
		response.Categories = append(response.Categories, pausedCategory)
		response.Total += pausedCategory.Count
	}

	// DLQ entries >7d
	dlqCategory, err := h.getStaleDLQEntries(ctx, now)
	if err == nil {
		response.Categories = append(response.Categories, dlqCategory)
		response.Total += dlqCategory.Count
	}

	return response, nil
}

// getStaleReviews finds reviews pending >24h
func (h *StaleWorkHandler) getStaleReviews(ctx context.Context, now time.Time) (*StaleWorkCategory, error) {
	category := &StaleWorkCategory{
		Category:  "Pending Reviews",
		Threshold: "24 hours",
		Items:     make([]*StaleWorkItem, 0),
	}

	cutoff := now.Add(-24 * time.Hour)
	query := `SELECT id, title, created_at FROM reviews
	         WHERE status IN ('draft', 'under_review')
	         AND created_at < ?
	         ORDER BY created_at ASC LIMIT 10`

	rows, err := h.db.QueryContext(ctx, query, cutoff)
	if err != nil {
		return category, nil
	}
	defer rows.Close()

	for rows.Next() {
		var id, title string
		var createdAt time.Time
		if err := rows.Scan(&id, &title, &createdAt); err != nil {
			continue
		}

		age := now.Sub(createdAt)
		ageMs := age.Milliseconds()
		category.Count++

		item := &StaleWorkItem{
			ID:        id,
			Type:      "Review",
			Details:   title,
			Age:       formatAge(age),
			AgeMs:     ageMs,
			CreatedAt: createdAt,
		}
		category.Items = append(category.Items, item)

		if category.AgeMs == 0 || ageMs > category.AgeMs {
			category.AgeMs = ageMs
			category.OldestItemAge = item.Age
		}
	}

	if category.Count > 3 {
		category.Status = "critical"
	} else if category.Count > 0 {
		category.Status = "warning"
	} else {
		category.Status = "normal"
	}

	return category, nil
}

// getStaleApprovals finds approvals pending >24h
func (h *StaleWorkHandler) getStaleApprovals(ctx context.Context, now time.Time) (*StaleWorkCategory, error) {
	category := &StaleWorkCategory{
		Category:  "Pending Approvals",
		Threshold: "24 hours",
		Items:     make([]*StaleWorkItem, 0),
	}

	cutoff := now.Add(-24 * time.Hour)
	query := `SELECT a.id, r.title, a.created_at FROM approvals a
	         JOIN reviews r ON a.review_id = r.id
	         WHERE a.decision = 'pending'
	         AND a.created_at < ?
	         ORDER BY a.created_at ASC LIMIT 10`

	rows, err := h.db.QueryContext(ctx, query, cutoff)
	if err != nil {
		return category, nil
	}
	defer rows.Close()

	for rows.Next() {
		var id, title string
		var createdAt time.Time
		if err := rows.Scan(&id, &title, &createdAt); err != nil {
			continue
		}

		age := now.Sub(createdAt)
		ageMs := age.Milliseconds()
		category.Count++

		item := &StaleWorkItem{
			ID:        id,
			Type:      "Approval",
			Details:   title,
			Age:       formatAge(age),
			AgeMs:     ageMs,
			CreatedAt: createdAt,
		}
		category.Items = append(category.Items, item)

		if category.AgeMs == 0 || ageMs > category.AgeMs {
			category.AgeMs = ageMs
			category.OldestItemAge = item.Age
		}
	}

	if category.Count > 3 {
		category.Status = "critical"
	} else if category.Count > 0 {
		category.Status = "warning"
	} else {
		category.Status = "normal"
	}

	return category, nil
}

// getStaleQueuedExecutions finds executions queued >30m
func (h *StaleWorkHandler) getStaleQueuedExecutions(ctx context.Context, now time.Time) (*StaleWorkCategory, error) {
	category := &StaleWorkCategory{
		Category:  "Long-Queued Executions",
		Threshold: "30 minutes",
		Items:     make([]*StaleWorkItem, 0),
	}

	cutoff := now.Add(-30 * time.Minute)
	query := `SELECT id, correlation_id, created_at FROM execution_requests
	         WHERE status IN ('pending', 'validated')
	         AND created_at < ?
	         ORDER BY created_at ASC LIMIT 10`

	rows, err := h.db.QueryContext(ctx, query, cutoff)
	if err != nil {
		return category, nil
	}
	defer rows.Close()

	for rows.Next() {
		var id, correlationID string
		var createdAt time.Time
		if err := rows.Scan(&id, &correlationID, &createdAt); err != nil {
			continue
		}

		age := now.Sub(createdAt)
		ageMs := age.Milliseconds()
		category.Count++

		item := &StaleWorkItem{
			ID:        id,
			Type:      "Execution",
			Details:   correlationID,
			Age:       formatAge(age),
			AgeMs:     ageMs,
			CreatedAt: createdAt,
		}
		category.Items = append(category.Items, item)

		if category.AgeMs == 0 || ageMs > category.AgeMs {
			category.AgeMs = ageMs
			category.OldestItemAge = item.Age
		}
	}

	if category.Count > 5 {
		category.Status = "critical"
	} else if category.Count > 1 {
		category.Status = "warning"
	} else {
		category.Status = "normal"
	}

	return category, nil
}

// getStalePausedExecutions finds executions in planned status >24h without progress
func (h *StaleWorkHandler) getStalePausedExecutions(ctx context.Context, now time.Time) (*StaleWorkCategory, error) {
	category := &StaleWorkCategory{
		Category:  "Long-Paused Executions",
		Threshold: "24 hours",
		Items:     make([]*StaleWorkItem, 0),
	}

	cutoff := now.Add(-24 * time.Hour)
	// Query for execution plans that are in 'draft' or 'validated' status (not executed) and are old
	query := `SELECT ep.id, ep.correlation_id, ep.created_at FROM execution_plans ep
	         WHERE ep.status IN ('draft', 'validated')
	         AND ep.created_at < ?
	         ORDER BY ep.created_at ASC LIMIT 10`

	rows, err := h.db.QueryContext(ctx, query, cutoff)
	if err != nil {
		return category, nil
	}
	defer rows.Close()

	for rows.Next() {
		var id, correlationID string
		var createdAt time.Time
		if err := rows.Scan(&id, &correlationID, &createdAt); err != nil {
			continue
		}

		age := now.Sub(createdAt)
		ageMs := age.Milliseconds()
		category.Count++

		item := &StaleWorkItem{
			ID:        id,
			Type:      "Execution Plan",
			Details:   correlationID,
			Age:       formatAge(age),
			AgeMs:     ageMs,
			CreatedAt: createdAt,
		}
		category.Items = append(category.Items, item)

		if category.AgeMs == 0 || ageMs > category.AgeMs {
			category.AgeMs = ageMs
			category.OldestItemAge = item.Age
		}
	}

	if category.Count > 3 {
		category.Status = "critical"
	} else if category.Count > 0 {
		category.Status = "warning"
	} else {
		category.Status = "normal"
	}

	return category, nil
}

// getStaleDLQEntries finds DLQ entries >7d
func (h *StaleWorkHandler) getStaleDLQEntries(ctx context.Context, now time.Time) (*StaleWorkCategory, error) {
	category := &StaleWorkCategory{
		Category:  "Old DLQ Entries",
		Threshold: "7 days",
		Items:     make([]*StaleWorkItem, 0),
	}

	if h.resilience == nil {
		return category, nil
	}

	cutoff := now.Add(-7 * 24 * time.Hour)
	dlqItems := h.resilience.GetDeadLetterQueue()

	for _, dlqItem := range dlqItems {
		if dlqItem.EnqueuedAt.Before(cutoff) {
			age := now.Sub(dlqItem.EnqueuedAt)
			ageMs := age.Milliseconds()
			category.Count++

			item := &StaleWorkItem{
				ID:        dlqItem.ID,
				Type:      "DLQ Entry",
				Details:   dlqItem.ExecutionID,
				Age:       formatAge(age),
				AgeMs:     ageMs,
				CreatedAt: dlqItem.EnqueuedAt,
			}
			category.Items = append(category.Items, item)

			if category.AgeMs == 0 || ageMs > category.AgeMs {
				category.AgeMs = ageMs
				category.OldestItemAge = item.Age
			}
		}
	}

	if category.Count > 5 {
		category.Status = "critical"
	} else if category.Count > 1 {
		category.Status = "warning"
	} else {
		category.Status = "normal"
	}

	return category, nil
}

// formatAge formats age in human-readable format
func formatAge(d time.Duration) string {
	if d < time.Minute {
		return "< 1 min"
	}
	if d < time.Hour {
		minutes := int(d.Minutes())
		return fmt.Sprintf("%d min", minutes)
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		return fmt.Sprintf("%d h", hours)
	}
	days := int(d.Hours() / 24)
	return fmt.Sprintf("%d d", days)
}
