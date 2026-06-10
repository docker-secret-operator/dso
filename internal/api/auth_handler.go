package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/services"
)

// AuthHandler handles authentication endpoints
type AuthHandler struct {
	authService      *auth.AuthenticationService
	auditService     *services.AuditService
	securityService  *services.SecurityService
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(authService *auth.AuthenticationService, auditService *services.AuditService) *AuthHandler {
	return &AuthHandler{
		authService:  authService,
		auditService: auditService,
	}
}

// SetSecurityService sets the security service for logging security events
func (h *AuthHandler) SetSecurityService(ss *services.SecurityService) {
	h.securityService = ss
}

// LoginRequest is the login request payload
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse is the login response payload
type LoginResponse struct {
	Token     string            `json:"token"`
	ExpiresAt time.Time         `json:"expires_at"`
	User      *UserInfoResponse `json:"user"`
	Session   *SessionResponse  `json:"session"`
}

// UserInfoResponse represents a user
type UserInfoResponse struct {
	ID                 string     `json:"id"`
	Username           string     `json:"username"`
	DisplayName        string     `json:"display_name"`
	Role               string     `json:"role"`
	MustChangePassword bool       `json:"must_change_password"`
	PasswordExpiresAt  *time.Time `json:"password_expires_at,omitempty"`
}

// SessionResponse represents a session
type SessionResponse struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	IPAddress string    `json:"ip_address"`
}

// LogoutRequest is the logout request (optional session ID in body)
type LogoutRequest struct {
	SessionID string `json:"session_id"`
}

// LogoutResponse is the logout response
type LogoutResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// ServeHTTP handles auth endpoints
func (h *AuthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if strings.HasSuffix(r.URL.Path, "/auth/login") && r.Method == "POST" {
		h.handleLogin(w, r)
	} else if strings.HasSuffix(r.URL.Path, "/auth/logout") && r.Method == "POST" {
		h.handleLogout(w, r)
	} else if strings.HasSuffix(r.URL.Path, "/auth/me") && r.Method == "GET" {
		h.handleMe(w, r)
	} else if strings.HasSuffix(r.URL.Path, "/auth/session") && r.Method == "GET" {
		h.handleSessionInfo(w, r)
	} else if strings.HasSuffix(r.URL.Path, "/auth/change-password") && r.Method == "POST" {
		h.handleChangePassword(w, r)
	} else if strings.HasSuffix(r.URL.Path, "/auth/reset-password") && r.Method == "POST" {
		h.handleResetPassword(w, r)
	} else if strings.HasSuffix(r.URL.Path, "/auth/refresh") && r.Method == "POST" {
		h.handleRefresh(w, r)
	} else {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "endpoint not found"})
	}
}

// handleLogin handles POST /api/auth/login
func (h *AuthHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	if req.Username == "" || req.Password == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "username and password required"})
		return
	}

	// Get client IP
	ipAddress := r.Header.Get("X-Forwarded-For")
	if ipAddress == "" {
		ipAddress = r.RemoteAddr
	}

	// Get user agent
	userAgent := r.Header.Get("User-Agent")

	// Authenticate
	result, err := h.authService.Authenticate(r.Context(), req.Username, req.Password, ipAddress, userAgent)
	if err != nil {
		// Log failed login as security event
		if h.securityService != nil {
			eventType := "LOGIN_FAILURE"
			if err == auth.ErrRateLimited {
				eventType = "RATE_LIMITED"
			} else if err == auth.ErrAccountLocked {
				eventType = "ACCOUNT_LOCKED"
			}
			_ = h.securityService.LogSecurityEvent(r.Context(), eventType, "medium", req.Username, nil,
				ipAddress, &userAgent, err.Error(), map[string]interface{}{"error": err.Error()})
		}

		status := http.StatusUnauthorized
		if err == auth.ErrRateLimited {
			status = http.StatusTooManyRequests
		}
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Log successful login as security event
	if h.securityService != nil {
		_ = h.securityService.LogSecurityEvent(r.Context(), "LOGIN_SUCCESS", "info", result.User.Username,
			&result.User.ID, ipAddress, &userAgent, "Successful login", nil)
	}

	// Build response
	response := &LoginResponse{
		Token:     result.Token,
		ExpiresAt: result.ExpiresAt,
		User: &UserInfoResponse{
			ID:                 result.User.ID,
			Username:           result.User.Username,
			DisplayName:        result.User.DisplayName,
			Role:               result.User.Role,
			MustChangePassword: result.User.MustChangePassword,
			PasswordExpiresAt:  result.User.PasswordExpiresAt,
		},
		Session: &SessionResponse{
			ID:        result.Session.ID,
			CreatedAt: result.Session.CreatedAt,
			ExpiresAt: result.Session.ExpiresAt,
			IPAddress: result.Session.IPAddress,
		},
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// handleLogout handles POST /api/auth/logout
func (h *AuthHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	// Get session from context (requires middleware)
	session := auth.CurrentSession(r.Context())
	if session == nil {
		h.logSessionError(r)
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "not authenticated"})
		return
	}

	// Logout
	if err := h.authService.Logout(r.Context(), session.ID); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to logout"})
		return
	}

	if h.auditService != nil {
		user := auth.CurrentUser(r.Context())
		actorID := "unknown"
		actorName := "unknown"
		if user != nil {
			actorID = user.ID
			actorName = user.Username
		}
		h.auditService.LogEvent(r.Context(), actorID, actorName, "auth.logout", "session", session.ID, "authentication")
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(&LogoutResponse{
		Success: true,
		Message: "logged out successfully",
	})
}

// handleMe handles GET /api/auth/me
func (h *AuthHandler) handleMe(w http.ResponseWriter, r *http.Request) {
	// Get user from context (requires middleware)
	user := auth.CurrentUser(r.Context())
	if user == nil {
		h.logSessionError(r)
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "not authenticated"})
		return
	}

	response := &UserInfoResponse{
		ID:                 user.ID,
		Username:           user.Username,
		DisplayName:        user.DisplayName,
		Role:               user.Role,
		MustChangePassword: user.MustChangePassword,
		PasswordExpiresAt:  user.PasswordExpiresAt,
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// handleRefresh handles POST /api/auth/refresh — extends the current session TTL
func (h *AuthHandler) handleRefresh(w http.ResponseWriter, r *http.Request) {
	session := auth.CurrentSession(r.Context())
	if session == nil {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "not authenticated"})
		return
	}

	newExpiry, err := h.authService.RefreshSession(r.Context(), session.ID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to refresh session"})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"expires_at": newExpiry,
	})
}

// handleSessionInfo handles GET /api/auth/session
func (h *AuthHandler) handleSessionInfo(w http.ResponseWriter, r *http.Request) {
	// Get session from context (requires middleware)
	session := auth.CurrentSession(r.Context())
	if session == nil {
		h.logSessionError(r)
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "not authenticated"})
		return
	}

	response := &SessionResponse{
		ID:        session.ID,
		CreatedAt: session.CreatedAt,
		ExpiresAt: session.ExpiresAt,
		IPAddress: session.IPAddress,
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// handleChangePassword handles POST /api/auth/change-password
func (h *AuthHandler) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r.Context())
	if user == nil {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "not authenticated"})
		return
	}

	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}
	if req.CurrentPassword == "" || req.NewPassword == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "current_password and new_password are required"})
		return
	}

	if err := h.authService.ChangePassword(r.Context(), user.ID, req.CurrentPassword, req.NewPassword); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if h.auditService != nil {
		h.auditService.LogEvent(r.Context(), user.ID, user.Username, "auth.password_changed", "user", user.ID, "authentication")
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "password changed"})
}

// handleResetPassword handles POST /api/auth/reset-password (admin only)
func (h *AuthHandler) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	actor := auth.CurrentUser(r.Context())
	if actor == nil {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "not authenticated"})
		return
	}

	var req struct {
		UserID      string `json:"user_id"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}
	if req.UserID == "" || req.NewPassword == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "user_id and new_password are required"})
		return
	}

	if err := h.authService.AdminResetPassword(r.Context(), req.UserID, req.NewPassword); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if h.auditService != nil {
		h.auditService.LogEvent(r.Context(), actor.ID, actor.Username, "auth.password_reset", "user", req.UserID, "authentication")
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "password reset"})
}

// logSessionError extracts the token and validates it to log specific session errors
func (h *AuthHandler) logSessionError(r *http.Request) {
	if h.auditService == nil {
		return
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		return
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == "" {
		return
	}

	_, err := h.authService.ValidateSession(r.Context(), token)
	if err != nil {
		action := "auth.session_invalid"
		if strings.Contains(err.Error(), "expired") {
			action = "auth.session_expired"
		}
		// We only have the token, we don't know the user, so use "system"
		h.auditService.LogEvent(r.Context(), "system", "system", action, "token", "provided", "authentication")
	}
}
