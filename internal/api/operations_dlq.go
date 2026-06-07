package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/docker-secret-operator/dso/internal/execution"
)

// OperationsDLQHandler handles DLQ console endpoints
type OperationsDLQHandler struct {
	resilience *execution.ResilienceManager
}

// NewOperationsDLQHandler creates a new DLQ handler
func NewOperationsDLQHandler(resilience *execution.ResilienceManager) *OperationsDLQHandler {
	return &OperationsDLQHandler{
		resilience: resilience,
	}
}

// ServeHTTP handles DLQ console requests
func (h *OperationsDLQHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	path := strings.TrimPrefix(r.URL.Path, "/api/operations/dlq/")

	// GET /api/operations/dlq/items
	if path == "items" && r.Method == http.MethodGet {
		h.getDLQItems(w, r)
		return
	}

	// GET /api/operations/dlq/stats
	if path == "stats" && r.Method == http.MethodGet {
		h.getDLQStats(w, r)
		return
	}

	// GET /api/operations/dlq/export
	if path == "export" && r.Method == http.MethodGet {
		h.exportDLQ(w, r)
		return
	}

	// POST /api/operations/dlq/retry/{id}
	if strings.HasPrefix(path, "retry/") && r.Method == http.MethodPost {
		h.retryDLQItem(w, r)
		return
	}

	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{"error": "Not found"})
}

// DLQItemResponse represents a DLQ item
type DLQItemResponse struct {
	ID            string    `json:"id"`
	ExecutionID   string    `json:"execution_id"`
	CorrelationID string    `json:"correlation_id"`
	Reason        string    `json:"reason"`
	ErrorMessage  string    `json:"error_message"`
	RetryCount    int       `json:"retry_count"`
	MaxRetries    int       `json:"max_retries"`
	EnqueuedAt    time.Time `json:"enqueued_at"`
	Age           string    `json:"age"`
	Retryable     bool      `json:"retryable"`
}

// getDLQItems returns items in dead letter queue
func (h *OperationsDLQHandler) getDLQItems(w http.ResponseWriter, r *http.Request) {
	items := h.resilience.GetDeadLetterQueue()
	responses := make([]*DLQItemResponse, len(items))

	now := time.Now()
	for i, item := range items {
		age := now.Sub(item.EnqueuedAt)
		ageStr := fmt.Sprintf("%.0f minutes", age.Minutes())
		if age.Hours() > 1 {
			ageStr = fmt.Sprintf("%.1f hours", age.Hours())
		}

		retryable := item.RetryCount < item.MaxRetries

		responses[i] = &DLQItemResponse{
			ID:            item.ID,
			ExecutionID:   item.ExecutionID,
			CorrelationID: item.CorrelationID,
			Reason:        item.Reason,
			ErrorMessage:  item.ErrorMessage,
			RetryCount:    item.RetryCount,
			MaxRetries:    item.MaxRetries,
			EnqueuedAt:    item.EnqueuedAt,
			Age:           ageStr,
			Retryable:     retryable,
		}
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"items":           responses,
		"count":           len(responses),
		"retryable_count": countRetryable(responses),
	})
}

// countRetryable counts retryable items
func countRetryable(items []*DLQItemResponse) int {
	count := 0
	for _, item := range items {
		if item.Retryable {
			count++
		}
	}
	return count
}

// DLQStatsResponse represents DLQ statistics
type DLQStatsResponse struct {
	TotalItems      int                       `json:"total_items"`
	RetryableCount  int                       `json:"retryable_count"`
	PermanentCount  int                       `json:"permanent_count"`
	FailureReasons  map[string]int            `json:"failure_reasons"`
	ReasonBreakdown []*FailureReasonBreakdown `json:"reason_breakdown"`
	OldestItemAge   string                    `json:"oldest_item_age"`
	Status          string                    `json:"status"`
}

// FailureReasonBreakdown represents failure breakdown
type FailureReasonBreakdown struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
	Percentage float64 `json:"percentage"`
}

// getDLQStats returns DLQ statistics
func (h *OperationsDLQHandler) getDLQStats(w http.ResponseWriter, r *http.Request) {
	items := h.resilience.GetDeadLetterQueue()

	// Count by reason
	reasonCount := make(map[string]int)
	retryableCount := 0
	permanentCount := 0

	for _, item := range items {
		reasonCount[item.Reason]++
		if item.RetryCount < item.MaxRetries {
			retryableCount++
		} else {
			permanentCount++
		}
	}

	// Build reason breakdown
	breakdown := make([]*FailureReasonBreakdown, 0)
	for reason, count := range reasonCount {
		percentage := 0.0
		if len(items) > 0 {
			percentage = float64(count) / float64(len(items)) * 100
		}
		breakdown = append(breakdown, &FailureReasonBreakdown{
			Reason:     reason,
			Count:      count,
			Percentage: percentage,
		})
	}

	// Calculate oldest item age
	oldestAge := ""
	if len(items) > 0 {
		oldest := items[0]
		for _, item := range items {
			if item.EnqueuedAt.Before(oldest.EnqueuedAt) {
				oldest = item
			}
		}
		age := time.Since(oldest.EnqueuedAt)
		oldestAge = fmt.Sprintf("%.1f hours", age.Hours())
	}

	// Determine status
	status := "healthy"
	if len(items) > 50 {
		status = "warning"
	}
	if len(items) > 200 {
		status = "critical"
	}

	response := DLQStatsResponse{
		TotalItems:      len(items),
		RetryableCount:  retryableCount,
		PermanentCount:  permanentCount,
		FailureReasons:  reasonCount,
		ReasonBreakdown: breakdown,
		OldestItemAge:   oldestAge,
		Status:          status,
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// exportDLQ exports DLQ as JSON
func (h *OperationsDLQHandler) exportDLQ(w http.ResponseWriter, r *http.Request) {
	items := h.resilience.GetDeadLetterQueue()

	// Convert to export format
	exportItems := make([]map[string]interface{}, len(items))
	for i, item := range items {
		exportItems[i] = map[string]interface{}{
			"id":              item.ID,
			"execution_id":    item.ExecutionID,
			"correlation_id":  item.CorrelationID,
			"reason":          item.Reason,
			"error_message":   item.ErrorMessage,
			"retry_count":     item.RetryCount,
			"max_retries":     item.MaxRetries,
			"enqueued_at":     item.EnqueuedAt,
			"age_hours":       time.Since(item.EnqueuedAt).Hours(),
		}
	}

	// Set export headers
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=dlq-export-%d.json", time.Now().Unix()))
	w.Header().Set("Content-Type", "application/json")

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"export_date": time.Now(),
		"total_items": len(items),
		"items":       exportItems,
	})
}

// retryDLQItem retries a DLQ item
func (h *OperationsDLQHandler) retryDLQItem(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/operations/dlq/retry/")
	itemID := path

	if itemID == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Item ID required"})
		return
	}

	err := h.resilience.RetryDLQItem(r.Context(), itemID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Item queued for retry"})
}
