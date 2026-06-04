package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/docker-secret-operator/dso/internal/services"
	"github.com/docker-secret-operator/dso/internal/storage"
)

// DraftHandler handles draft-related API endpoints
type DraftHandler struct {
	draftService *services.DraftService
	auditService *services.AuditService
	userID       string
	persistenceEnabled bool
}

// NewDraftHandler creates a new draft handler
func NewDraftHandler(
	draftService *services.DraftService,
	auditService *services.AuditService,
	userID string,
	persistenceEnabled bool,
) *DraftHandler {
	return &DraftHandler{
		draftService:       draftService,
		auditService:       auditService,
		userID:             userID,
		persistenceEnabled: persistenceEnabled,
	}
}

// CreateDraftRequest represents a draft creation request
type CreateDraftRequest struct {
	WorkspaceID string `json:"workspace_id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Config      string `json:"config"`
}

// UpdateDraftRequest represents a draft update request
type UpdateDraftRequest struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Config      string `json:"config,omitempty"`
}

// DraftResponse represents a draft response
type DraftResponse struct {
	ID            string    `json:"id"`
	WorkspaceID   string    `json:"workspace_id"`
	OwnerID       string    `json:"owner_id"`
	Title         string    `json:"title"`
	Description   string    `json:"description,omitempty"`
	Status        string    `json:"status"`
	VersionNumber int       `json:"version_number"`
	Checksum      string    `json:"checksum,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	ModifiedAt    time.Time `json:"modified_at"`
}

// DraftVersionResponse represents a draft version
type DraftVersionResponse struct {
	VersionNumber int       `json:"version_number"`
	Checksum      string    `json:"checksum,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// HandleCreateDraft handles POST /api/drafts
func (h *DraftHandler) HandleCreateDraft(w http.ResponseWriter, r *http.Request) {
	if !h.persistenceEnabled {
		h.writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Draft persistence is not enabled",
		})
		return
	}

	if r.Method != "POST" {
		h.writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
			"error": "Method not allowed",
		})
		return
	}

	var req CreateDraftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid request body",
		})
		return
	}

	if req.Title == "" || req.Config == "" {
		h.writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Title and Config are required",
		})
		return
	}

	ctx := r.Context()
	draft, err := h.draftService.CreateDraft(ctx, req.WorkspaceID, h.userID, req.Title, req.Description, req.Config)
	if err != nil {
		h.auditService.LogEvent(ctx, h.userID, h.userID, "draft.created", "draft", "", "failure")
		h.writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to create draft: %v", err),
		})
		return
	}

	h.auditService.LogEvent(ctx, h.userID, h.userID, "draft.created", "draft", draft.ID, "success")
	h.writeJSON(w, http.StatusCreated, h.draftToResponse(draft))
}

// HandleGetDraft handles GET /api/drafts/{id}
func (h *DraftHandler) HandleGetDraft(w http.ResponseWriter, r *http.Request, draftID string) {
	if !h.persistenceEnabled {
		h.writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Draft persistence is not enabled",
		})
		return
	}

	if r.Method != "GET" {
		h.writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
			"error": "Method not allowed",
		})
		return
	}

	ctx := r.Context()
	draft, err := h.draftService.GetDraft(ctx, draftID)
	if err != nil {
		h.writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "Draft not found",
		})
		return
	}

	h.auditService.LogEvent(ctx, h.userID, h.userID, "draft.viewed", "draft", draftID, "success")
	h.writeJSON(w, http.StatusOK, h.draftToResponse(draft))
}

// HandleListDrafts handles GET /api/drafts
func (h *DraftHandler) HandleListDrafts(w http.ResponseWriter, r *http.Request) {
	if !h.persistenceEnabled {
		h.writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Draft persistence is not enabled",
		})
		return
	}

	if r.Method != "GET" {
		h.writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
			"error": "Method not allowed",
		})
		return
	}

	ctx := r.Context()
	drafts, err := h.draftService.ListDrafts(ctx, h.userID)
	if err != nil {
		h.writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to list drafts: %v", err),
		})
		return
	}

	responses := make([]DraftResponse, 0)
	for _, draft := range drafts {
		responses = append(responses, h.draftToResponse(draft))
	}

	h.auditService.LogEvent(ctx, h.userID, h.userID, "draft.listed", "draft", "all", "success")
	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"drafts": responses,
		"count":  len(responses),
	})
}

// HandleUpdateDraft handles PUT /api/drafts/{id}
func (h *DraftHandler) HandleUpdateDraft(w http.ResponseWriter, r *http.Request, draftID string) {
	if !h.persistenceEnabled {
		h.writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Draft persistence is not enabled",
		})
		return
	}

	if r.Method != "PUT" {
		h.writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
			"error": "Method not allowed",
		})
		return
	}

	var req UpdateDraftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid request body",
		})
		return
	}

	ctx := r.Context()
	draft, err := h.draftService.GetDraft(ctx, draftID)
	if err != nil {
		h.writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "Draft not found",
		})
		return
	}

	// Use existing values if not provided
	title := draft.Title
	description := draft.Description
	config := draft.Config

	if req.Title != "" {
		title = req.Title
	}
	if req.Description != "" {
		description = req.Description
	}
	if req.Config != "" {
		config = req.Config
	}

	updated, err := h.draftService.UpdateDraft(ctx, draftID, title, description, config)
	if err != nil {
		h.auditService.LogEvent(ctx, h.userID, h.userID, "draft.updated", "draft", draftID, "failure")
		h.writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to update draft: %v", err),
		})
		return
	}

	h.auditService.LogEvent(ctx, h.userID, h.userID, "draft.updated", "draft", draftID, "success")
	h.writeJSON(w, http.StatusOK, h.draftToResponse(updated))
}

// HandleDeleteDraft handles DELETE /api/drafts/{id}
func (h *DraftHandler) HandleDeleteDraft(w http.ResponseWriter, r *http.Request, draftID string) {
	if !h.persistenceEnabled {
		h.writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Draft persistence is not enabled",
		})
		return
	}

	if r.Method != "DELETE" {
		h.writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
			"error": "Method not allowed",
		})
		return
	}

	ctx := r.Context()

	// Verify draft exists
	_, err := h.draftService.GetDraft(ctx, draftID)
	if err != nil {
		h.writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "Draft not found",
		})
		return
	}

	err = h.draftService.DeleteDraft(ctx, draftID)
	if err != nil {
		h.auditService.LogEvent(ctx, h.userID, h.userID, "draft.archived", "draft", draftID, "failure")
		h.writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to delete draft: %v", err),
		})
		return
	}

	h.auditService.LogEvent(ctx, h.userID, h.userID, "draft.archived", "draft", draftID, "success")
	w.WriteHeader(http.StatusNoContent)
}

// HandleGetDraftVersions handles GET /api/drafts/{id}/versions
func (h *DraftHandler) HandleGetDraftVersions(w http.ResponseWriter, r *http.Request, draftID string) {
	if !h.persistenceEnabled {
		h.writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Draft persistence is not enabled",
		})
		return
	}

	if r.Method != "GET" {
		h.writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
			"error": "Method not allowed",
		})
		return
	}

	ctx := r.Context()

	// Verify draft exists
	_, err := h.draftService.GetDraft(ctx, draftID)
	if err != nil {
		h.writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "Draft not found",
		})
		return
	}

	versions, err := h.draftService.GetDraftVersions(ctx, draftID)
	if err != nil {
		h.writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to get versions: %v", err),
		})
		return
	}

	responses := make([]DraftVersionResponse, 0)
	for _, v := range versions {
		responses = append(responses, DraftVersionResponse{
			VersionNumber: v.VersionNumber,
			Checksum:      v.Checksum,
			CreatedAt:     v.CreatedAt,
		})
	}

	h.auditService.LogEvent(ctx, h.userID, h.userID, "draft.versions_viewed", "draft", draftID, "success")
	h.writeJSON(w, http.StatusOK, map[string]interface{}{
		"draft_id": draftID,
		"versions": responses,
		"count":    len(responses),
	})
}

// Helper functions

func (h *DraftHandler) draftToResponse(draft *storage.Draft) DraftResponse {
	return DraftResponse{
		ID:            draft.ID,
		WorkspaceID:   draft.WorkspaceID,
		OwnerID:       draft.OwnerID,
		Title:         draft.Title,
		Description:   draft.Description,
		Status:        draft.Status,
		VersionNumber: draft.VersionNumber,
		Checksum:      draft.Checksum,
		CreatedAt:     draft.CreatedAt,
		ModifiedAt:    draft.ModifiedAt,
	}
}

func (h *DraftHandler) writeJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}
