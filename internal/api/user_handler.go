package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/services"
	"github.com/docker-secret-operator/dso/internal/storage"
	"github.com/google/uuid"
)

// UserManagementHandler handles user and session management endpoints
type UserManagementHandler struct {
	userStore    storage.UserStore
	sessionStore storage.SessionStore
	authService  *auth.AuthenticationService
	auditService *services.AuditService
}

// NewUserManagementHandler creates a new handler
func NewUserManagementHandler(userStore storage.UserStore, sessionStore storage.SessionStore, authService *auth.AuthenticationService, auditService *services.AuditService) *UserManagementHandler {
	return &UserManagementHandler{
		userStore:    userStore,
		sessionStore: sessionStore,
		authService:  authService,
		auditService: auditService,
	}
}

// userResponse is the safe public representation of a user (no password_hash)
type userResponse struct {
	ID                 string     `json:"id"`
	Username           string     `json:"username"`
	DisplayName        string     `json:"display_name"`
	Role               string     `json:"role"`
	Disabled           bool       `json:"disabled"`
	Locked             bool       `json:"locked"`
	LockedUntil        *time.Time `json:"locked_until,omitempty"`
	MustChangePassword bool       `json:"must_change_password"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

func toUserResponse(u *storage.User) userResponse {
	locked := u.LockedUntil != nil && time.Now().Before(*u.LockedUntil)
	return userResponse{
		ID:                 u.ID,
		Username:           u.Username,
		DisplayName:        u.DisplayName,
		Role:               u.Role,
		Disabled:           u.Disabled,
		Locked:             locked,
		LockedUntil:        u.LockedUntil,
		MustChangePassword: u.MustChangePassword,
		CreatedAt:          u.CreatedAt,
		UpdatedAt:          u.UpdatedAt,
	}
}

// sessionDetailResponse is the safe public representation of a session
type sessionDetailResponse struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	IPAddress    string    `json:"ip_address"`
	UserAgent    string    `json:"user_agent"`
	CreatedAt    time.Time `json:"created_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	LastActivity time.Time `json:"last_activity"`
}

func toSessionResponse(s *storage.Session) sessionDetailResponse {
	return sessionDetailResponse{
		ID:           s.ID,
		UserID:       s.UserID,
		IPAddress:    s.IPAddress,
		UserAgent:    s.UserAgent,
		CreatedAt:    s.CreatedAt,
		ExpiresAt:    s.ExpiresAt,
		LastActivity: s.LastActivity,
	}
}

// ServeHTTP dispatches /api/users/* and /api/sessions/*
func (h *UserManagementHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	path := r.URL.Path

	switch {
	case path == "/api/users" || path == "/api/users/":
		switch r.Method {
		case http.MethodGet:
			h.listUsers(w, r)
		case http.MethodPost:
			h.createUser(w, r)
		default:
			h.methodNotAllowed(w)
		}

	case strings.HasPrefix(path, "/api/users/"):
		id := strings.TrimPrefix(path, "/api/users/")
		switch r.Method {
		case http.MethodGet:
			h.getUser(w, r, id)
		case http.MethodPut:
			h.updateUser(w, r, id)
		case http.MethodDelete:
			h.deleteUser(w, r, id)
		default:
			h.methodNotAllowed(w)
		}

	case path == "/api/sessions" || path == "/api/sessions/":
		if r.Method == http.MethodGet {
			h.listSessions(w, r)
		} else {
			h.methodNotAllowed(w)
		}

	case path == "/api/sessions/revoke-all":
		if r.Method == http.MethodPost {
			h.revokeAllSessions(w, r)
		} else {
			h.methodNotAllowed(w)
		}

	case strings.HasPrefix(path, "/api/sessions/"):
		id := strings.TrimPrefix(path, "/api/sessions/")
		if r.Method == http.MethodDelete {
			h.revokeSession(w, r, id)
		} else {
			h.methodNotAllowed(w)
		}

	case path == "/api/admin/sessions" || path == "/api/admin/sessions/":
		if r.Method == http.MethodGet {
			h.listAdminSessions(w, r)
		} else {
			h.methodNotAllowed(w)
		}

	case strings.HasPrefix(path, "/api/admin/sessions/"):
		id := strings.TrimPrefix(path, "/api/admin/sessions/")
		if r.Method == http.MethodDelete {
			h.adminRevokeSession(w, r, id)
		} else {
			h.methodNotAllowed(w)
		}

	default:
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
	}
}

// ── User CRUD ────────────────────────────────────────────────────────────────

func (h *UserManagementHandler) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.userStore.List(r.Context())
	if err != nil {
		h.internalError(w, "failed to list users", err)
		return
	}

	// Filter by username search
	search := strings.ToLower(r.URL.Query().Get("search"))
	roleFilter := r.URL.Query().Get("role")

	filtered := make([]userResponse, 0, len(users))
	for _, u := range users {
		if search != "" && !strings.Contains(strings.ToLower(u.Username), search) {
			continue
		}
		if roleFilter != "" && u.Role != roleFilter {
			continue
		}
		filtered = append(filtered, toUserResponse(u))
	}

	// Pagination
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize := 50
	if ps, err := strconv.Atoi(r.URL.Query().Get("page_size")); err == nil && ps > 0 && ps <= 200 {
		pageSize = ps
	}
	total := len(filtered)
	start := (page - 1) * pageSize
	if start >= total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"users": filtered[start:end],
		"count": total,
		"page":  page,
	})
}

func (h *UserManagementHandler) getUser(w http.ResponseWriter, r *http.Request, id string) {
	user, err := h.userStore.GetByID(r.Context(), id)
	if err != nil {
		h.internalError(w, "failed to get user", err)
		return
	}
	if user == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "user not found"})
		return
	}
	json.NewEncoder(w).Encode(toUserResponse(user))
}

type createUserRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
}

func (h *UserManagementHandler) createUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.badRequest(w, "invalid request body")
		return
	}
	if req.Username == "" || req.Password == "" || req.Role == "" {
		h.badRequest(w, "username, password, and role are required")
		return
	}
	if !validRole(req.Role) {
		h.badRequest(w, "role must be one of: viewer, operator, reviewer, approver, admin")
		return
	}
	if err := auth.ValidatePasswordPolicy(req.Password); err != nil {
		h.badRequest(w, err.Error())
		return
	}

	// Check username uniqueness
	existing, _ := h.userStore.GetByUsername(r.Context(), req.Username)
	if existing != nil {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "username already exists"})
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		h.internalError(w, "failed to hash password", err)
		return
	}

	now := time.Now()
	user := &storage.User{
		ID:           uuid.New().String(),
		Username:     req.Username,
		PasswordHash: hash,
		DisplayName:  req.DisplayName,
		Role:         req.Role,
		Disabled:     false,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if user.DisplayName == "" {
		user.DisplayName = req.Username
	}

	if err := h.userStore.Create(r.Context(), user); err != nil {
		h.internalError(w, "failed to create user", err)
		return
	}

	actor := auth.CurrentUser(r.Context())
	if h.auditService != nil && actor != nil {
		h.auditService.LogEvent(r.Context(), actor.ID, actor.Username, "user.created", "user", user.ID, "user_management")
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(toUserResponse(user))
}

type updateUserRequest struct {
	DisplayName        *string `json:"display_name"`
	Role               *string `json:"role"`
	Disabled           *bool   `json:"disabled"`
	Unlock             *bool   `json:"unlock"`
	ForcePasswordReset *bool   `json:"force_password_reset"`
}

func (h *UserManagementHandler) updateUser(w http.ResponseWriter, r *http.Request, id string) {
	user, err := h.userStore.GetByID(r.Context(), id)
	if err != nil {
		h.internalError(w, "failed to get user", err)
		return
	}
	if user == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "user not found"})
		return
	}

	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.badRequest(w, "invalid request body")
		return
	}

	action := "user.updated"
	if req.Role != nil {
		if !validRole(*req.Role) {
			h.badRequest(w, "role must be one of: viewer, operator, reviewer, approver, admin")
			return
		}
		user.Role = *req.Role
	}
	if req.DisplayName != nil {
		user.DisplayName = *req.DisplayName
	}
	if req.Disabled != nil {
		if *req.Disabled && !user.Disabled {
			action = "user.disabled"
		} else if !*req.Disabled && user.Disabled {
			action = "user.enabled"
		}
		user.Disabled = *req.Disabled
	}
	if req.Unlock != nil && *req.Unlock {
		user.FailedLoginCount = 0
		user.LockedUntil = nil
		action = "user.unlocked"
	}
	if req.ForcePasswordReset != nil && *req.ForcePasswordReset {
		user.MustChangePassword = true
		action = "user.force_password_reset"
	}
	user.UpdatedAt = time.Now()

	if err := h.userStore.Update(r.Context(), user); err != nil {
		h.internalError(w, "failed to update user", err)
		return
	}

	actor := auth.CurrentUser(r.Context())
	if h.auditService != nil && actor != nil {
		h.auditService.LogEvent(r.Context(), actor.ID, actor.Username, action, "user", user.ID, "user_management")
	}

	json.NewEncoder(w).Encode(toUserResponse(user))
}

func (h *UserManagementHandler) deleteUser(w http.ResponseWriter, r *http.Request, id string) {
	user, err := h.userStore.GetByID(r.Context(), id)
	if err != nil {
		h.internalError(w, "failed to get user", err)
		return
	}
	if user == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "user not found"})
		return
	}

	// Prevent self-deletion
	actor := auth.CurrentUser(r.Context())
	if actor != nil && actor.ID == id {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "cannot delete your own account"})
		return
	}

	// Revoke all sessions before deleting
	_ = h.authService.RevokeAllUserSessions(r.Context(), id)

	if err := h.userStore.Delete(r.Context(), id); err != nil {
		h.internalError(w, "failed to delete user", err)
		return
	}

	if h.auditService != nil && actor != nil {
		h.auditService.LogEvent(r.Context(), actor.ID, actor.Username, "user.deleted", "user", id, "user_management")
	}

	w.WriteHeader(http.StatusNoContent)
}

// ── Session management ───────────────────────────────────────────────────────

func (h *UserManagementHandler) listSessions(w http.ResponseWriter, r *http.Request) {
	actor := auth.CurrentUser(r.Context())
	if actor == nil {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "not authenticated"})
		return
	}

	sessions, err := h.authService.ListUserSessions(r.Context(), actor.ID)
	if err != nil {
		h.internalError(w, "failed to list sessions", err)
		return
	}

	currentSession := auth.CurrentSession(r.Context())
	currentID := ""
	if currentSession != nil {
		currentID = currentSession.ID
	}

	type sessionWithCurrent struct {
		sessionDetailResponse
		IsCurrent bool `json:"is_current"`
	}

	result := make([]sessionWithCurrent, 0, len(sessions))
	for _, s := range sessions {
		result = append(result, sessionWithCurrent{
			sessionDetailResponse: toSessionResponse(s),
			IsCurrent:             s.ID == currentID,
		})
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"sessions": result,
		"count":    len(result),
	})
}

func (h *UserManagementHandler) revokeSession(w http.ResponseWriter, r *http.Request, id string) {
	actor := auth.CurrentUser(r.Context())
	if actor == nil {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "not authenticated"})
		return
	}

	// Non-admins may only revoke their own sessions.
	if actor.Role != "admin" {
		session, err := h.sessionStore.GetByID(r.Context(), id)
		if err != nil || session == nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "session not found"})
			return
		}
		if session.UserID != actor.ID {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"error": "forbidden"})
			return
		}
	}

	if err := h.authService.RevokeSession(r.Context(), id); err != nil {
		h.internalError(w, "failed to revoke session", err)
		return
	}

	if h.auditService != nil {
		h.auditService.LogEvent(r.Context(), actor.ID, actor.Username, "session.revoked", "session", id, "session_management")
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *UserManagementHandler) revokeAllSessions(w http.ResponseWriter, r *http.Request) {
	actor := auth.CurrentUser(r.Context())
	if actor == nil {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "not authenticated"})
		return
	}

	if err := h.authService.RevokeAllUserSessions(r.Context(), actor.ID); err != nil {
		h.internalError(w, "failed to revoke sessions", err)
		return
	}

	if h.auditService != nil {
		h.auditService.LogEvent(r.Context(), actor.ID, actor.Username, "session.revoked_all", "user", actor.ID, "session_management")
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "all sessions revoked"})
}

// ── Admin session management ─────────────────────────────────────────────────

func (h *UserManagementHandler) listAdminSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := h.sessionStore.ListAll(r.Context())
	if err != nil {
		h.internalError(w, "failed to list sessions", err)
		return
	}

	type adminSessionResponse struct {
		sessionDetailResponse
		Username string `json:"username"`
	}

	result := make([]adminSessionResponse, 0, len(sessions))
	for _, s := range sessions {
		username := s.UserID
		if u, err := h.userStore.GetByID(r.Context(), s.UserID); err == nil && u != nil {
			username = u.Username
		}
		result = append(result, adminSessionResponse{
			sessionDetailResponse: toSessionResponse(s),
			Username:              username,
		})
	}

	actor := auth.CurrentUser(r.Context())
	if h.auditService != nil && actor != nil {
		h.auditService.LogEvent(r.Context(), actor.ID, actor.Username, "admin.sessions.listed", "session", "all", "admin")
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"sessions": result,
		"count":    len(result),
	})
}

func (h *UserManagementHandler) adminRevokeSession(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.sessionStore.Delete(r.Context(), id); err != nil {
		h.internalError(w, "failed to revoke session", err)
		return
	}

	actor := auth.CurrentUser(r.Context())
	if h.auditService != nil && actor != nil {
		h.auditService.LogEvent(r.Context(), actor.ID, actor.Username, "admin.session.revoked", "session", id, "admin")
	}

	w.WriteHeader(http.StatusNoContent)
}

// ── helpers ──────────────────────────────────────────────────────────────────

func validRole(role string) bool {
	switch role {
	case "viewer", "operator", "reviewer", "approver", "admin":
		return true
	}
	return false
}

func (h *UserManagementHandler) badRequest(w http.ResponseWriter, msg string) {
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func (h *UserManagementHandler) internalError(w http.ResponseWriter, msg string, _ error) {
	w.WriteHeader(http.StatusInternalServerError)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func (h *UserManagementHandler) methodNotAllowed(w http.ResponseWriter) {
	w.WriteHeader(http.StatusMethodNotAllowed)
	json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
}
