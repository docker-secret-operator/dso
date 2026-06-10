package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/docker-secret-operator/dso/internal/services"
	"github.com/docker-secret-operator/dso/internal/storage"
)

// ReviewHandler handles review endpoints
type ReviewHandler struct {
	reviewService *services.ReviewService
	auditService  *services.AuditService
	draftService  *services.DraftService
}

// NewReviewHandler creates a new review handler
func NewReviewHandler(
	reviewService *services.ReviewService,
	auditService *services.AuditService,
	draftService *services.DraftService,
) *ReviewHandler {
	return &ReviewHandler{
		reviewService: reviewService,
		auditService:  auditService,
		draftService:  draftService,
	}
}

// ServeHTTP handles review API routes
func (h *ReviewHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	method := r.Method

	// Route based on path and method
	if strings.HasPrefix(path, "/api/reviews/") {
		id := extractID(path, "/api/reviews/")
		if id == "" {
			h.handleReviewList(w, r)
			return
		}

		if strings.HasSuffix(path, "/history") {
			h.GetReviewHistory(w, r, id)
			return
		}

		if strings.HasSuffix(path, "/comments") {
			if method == "POST" {
				h.AddComment(w, r, id)
			} else {
				h.writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
					"error": "Method not allowed",
				})
			}
			return
		}

		switch method {
		case "GET":
			h.GetReview(w, r, id)
		case "PUT":
			h.UpdateReview(w, r, id)
		case "DELETE":
			h.DeleteReview(w, r, id)
		default:
			h.writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
				"error": "Method not allowed",
			})
		}
		return
	}

	if path == "/api/reviews" {
		h.handleReviewList(w, r)
		return
	}

	h.writeJSON(w, http.StatusNotFound, map[string]string{
		"error": "Not found",
	})
}

func (h *ReviewHandler) handleReviewList(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		h.ListReviews(w, r)
	} else if r.Method == "POST" {
		h.CreateReview(w, r)
	} else {
		h.writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
			"error": "Method not allowed",
		})
	}
}

// extractID extracts ID from path
func extractID(path, prefix string) string {
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := path[len(prefix):]
	// Split by / to handle subresources
	parts := strings.Split(rest, "/")
	if len(parts) > 0 && parts[0] != "" {
		return parts[0]
	}
	return ""
}

// CreateReviewRequest represents a review creation request
type CreateReviewRequest struct {
	DraftID     string `json:"draft_id"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// UpdateReviewRequest represents a review update request
type UpdateReviewRequest struct {
	Status      string `json:"status,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// ReviewResponse represents a review
type ReviewResponse struct {
	ID           string    `json:"id"`
	DraftID      string    `json:"draft_id"`
	CreatedBy    string    `json:"created_by,omitempty"`
	Status       string    `json:"status"`
	Title        string    `json:"title,omitempty"`
	Description  string    `json:"description,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	ModifiedAt   time.Time `json:"modified_at"`
	CommentCount int       `json:"comment_count,omitempty"`
}

// CommentRequest represents adding a comment
type CommentRequest struct {
	Type    string `json:"type"` // "approve", "reject", "general"
	Text    string `json:"text"`
	ActorID string `json:"actor_id"`
}

// CommentResponse represents a comment
type CommentResponse struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Text      string    `json:"text"`
	ActorID   string    `json:"actor_id"`
	CreatedAt time.Time `json:"created_at"`
}

// HistoryEntry represents a timeline entry
type HistoryEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	Type      string                 `json:"type"` // "status_change", "comment", "created"
	Actor     string                 `json:"actor"`
	Message   string                 `json:"message"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// ListReviews lists all reviews
// GET /api/reviews
func (h *ReviewHandler) ListReviews(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	reviews, err := h.reviewService.ListReviews(ctx)
	if err != nil {
		h.writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to list reviews: %v", err),
		})
		return
	}

	responses := make([]ReviewResponse, 0)
	for _, review := range reviews {
		responses = append(responses, h.reviewToResponse(review))
	}

	h.auditService.LogEvent(ctx, "system", "System", "review.listed", "review", "all", "success")
	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"reviews": responses,
		"count":   len(responses),
	})
}

// GetReview gets a review by ID
// GET /api/reviews/{id}
func (h *ReviewHandler) GetReview(w http.ResponseWriter, r *http.Request, reviewID string) {
	ctx := r.Context()

	review, err := h.reviewService.GetReview(ctx, reviewID)
	if err != nil {
		h.writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "Review not found",
		})
		return
	}

	h.auditService.LogEvent(ctx, "system", "System", "review.viewed", "review", reviewID, "success")
	h.writeJSON(w, http.StatusOK, h.reviewToResponse(review))
}

// CreateReview creates a new review
// POST /api/reviews
func (h *ReviewHandler) CreateReview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req CreateReviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid request body",
		})
		return
	}

	// Validate draft exists and is under_review
	draft, err := h.draftService.GetDraft(ctx, req.DraftID)
	if err != nil {
		h.writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Draft not found",
		})
		return
	}

	if draft.Status != "under_review" {
		h.writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("Draft status must be 'under_review', got '%s'", draft.Status),
		})
		return
	}

	// Create review
	review, err := h.reviewService.CreateReview(ctx, &storage.Review{
		DraftID:     req.DraftID,
		CreatedBy:   "system",
		Status:      "draft_review",
		Title:       req.Title,
		Description: req.Description,
	})
	if err != nil {
		h.auditService.LogEvent(ctx, "system", "System", "review.created", "review", req.DraftID, "failure")
		h.writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to create review: %v", err),
		})
		return
	}

	h.auditService.LogEvent(ctx, "system", "System", "review.created", "review", review.ID, "success")
	h.writeJSON(w, http.StatusCreated, h.reviewToResponse(review))
}

// UpdateReview updates a review
// PUT /api/reviews/{id}
func (h *ReviewHandler) UpdateReview(w http.ResponseWriter, r *http.Request, reviewID string) {
	ctx := r.Context()

	var req UpdateReviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid request body",
		})
		return
	}

	review, err := h.reviewService.GetReview(ctx, reviewID)
	if err != nil {
		h.writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "Review not found",
		})
		return
	}

	// Validate status transition if provided
	if req.Status != "" {
		if err := services.ValidateReviewStatusTransition(review.Status, req.Status); err != nil {
			h.writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": err.Error(),
			})
			return
		}

		review.Status = req.Status

		// Log audit event based on status change
		auditAction := "review.updated"
		if req.Status == "approved" {
			auditAction = "review.approved"
		} else if req.Status == "rejected" {
			auditAction = "review.rejected"
		} else if req.Status == "closed" {
			auditAction = "review.closed"
		}

		h.auditService.LogEvent(ctx, "system", "System", auditAction, "review", reviewID, "success")
	}

	if req.Title != "" {
		review.Title = req.Title
	}

	if req.Description != "" {
		review.Description = req.Description
	}

	review.ModifiedAt = time.Now()

	updated, err := h.reviewService.UpdateReview(ctx, review)
	if err != nil {
		h.writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to update review: %v", err),
		})
		return
	}

	h.writeJSON(w, http.StatusOK, h.reviewToResponse(updated))
}

// DeleteReview deletes a review (soft delete)
// DELETE /api/reviews/{id}
func (h *ReviewHandler) DeleteReview(w http.ResponseWriter, r *http.Request, reviewID string) {
	ctx := r.Context()

	review, err := h.reviewService.GetReview(ctx, reviewID)
	if err != nil {
		h.writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "Review not found",
		})
		return
	}

	// Set status to closed if not already
	if review.Status != "closed" {
		review.Status = "closed"
		review.ModifiedAt = time.Now()

		if _, err := h.reviewService.UpdateReview(ctx, review); err != nil {
			h.writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": fmt.Sprintf("Failed to close review: %v", err),
			})
			return
		}
	}

	h.auditService.LogEvent(ctx, "system", "System", "review.closed", "review", reviewID, "success")
	w.WriteHeader(http.StatusNoContent)
}

// GetReviewHistory gets review history and comments
// GET /api/reviews/{id}/history
func (h *ReviewHandler) GetReviewHistory(w http.ResponseWriter, r *http.Request, reviewID string) {
	ctx := r.Context()

	review, err := h.reviewService.GetReview(ctx, reviewID)
	if err != nil {
		h.writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "Review not found",
		})
		return
	}

	// Get activities (comments/status changes)
	activities, err := h.reviewService.GetReviewActivities(ctx, reviewID)
	if err != nil {
		h.writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to get review history: %v", err),
		})
		return
	}

	history := make([]HistoryEntry, 0)

	// Add creation event
	history = append(history, HistoryEntry{
		Timestamp: review.CreatedAt,
		Type:      "created",
		Actor:     review.CreatedBy,
		Message:   "Review created",
	})

	// Add activity events
	for _, activity := range activities {
		history = append(history, HistoryEntry{
			Timestamp: activity.Timestamp,
			Type:      activity.Type,
			Actor:     activity.ActorID,
			Message:   activity.Description,
		})
	}

	// Sort by timestamp descending
	// (activities already sorted, so just reverse)

	h.auditService.LogEvent(ctx, "system", "System", "review.history_viewed", "review", reviewID, "success")
	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"review_id": reviewID,
		"history":   history,
		"count":     len(history),
	})
}

// AddComment adds a comment to a review
// POST /api/reviews/{id}/comments
func (h *ReviewHandler) AddComment(w http.ResponseWriter, r *http.Request, reviewID string) {
	ctx := r.Context()

	var req CommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid request body",
		})
		return
	}

	// Verify review exists
	_, err := h.reviewService.GetReview(ctx, reviewID)
	if err != nil {
		h.writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "Review not found",
		})
		return
	}

	// Add activity entry (comment)
	activity, err := h.reviewService.AddActivity(ctx, reviewID, &storage.ReviewActivity{
		ReviewID:    reviewID,
		Type:        req.Type,
		ActorID:     req.ActorID,
		Description: req.Text,
		Timestamp:   time.Now(),
	})
	if err != nil {
		h.writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to add comment: %v", err),
		})
		return
	}

	h.auditService.LogEvent(ctx, req.ActorID, req.ActorID, "review.comment_added", "review", reviewID, "success")
	h.writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":         activity.ID,
		"type":       activity.Type,
		"text":       activity.Description,
		"actor_id":   activity.ActorID,
		"created_at": activity.Timestamp,
	})
}

// Helper functions

func (h *ReviewHandler) reviewToResponse(review *storage.Review) ReviewResponse {
	return ReviewResponse{
		ID:          review.ID,
		DraftID:     review.DraftID,
		CreatedBy:   review.CreatedBy,
		Status:      review.Status,
		Title:       review.Title,
		Description: review.Description,
		CreatedAt:   review.CreatedAt,
		ModifiedAt:  review.ModifiedAt,
	}
}

func (h *ReviewHandler) writeJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}
