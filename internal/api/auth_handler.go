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
	authService  *auth.AuthenticationService
	auditService *services.AuditService
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(authService *auth.AuthenticationService, auditService *services.AuditService) *AuthHandler {
	return &AuthHandler{
		authService:  authService,
		auditService: auditService,
	}
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
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
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
		if h.auditService != nil {
			h.auditService.LogEvent(r.Context(), "system", "system", "auth.login_failure", "user", req.Username, "authentication")
		}
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if h.auditService != nil {
		h.auditService.LogEvent(r.Context(), result.User.ID, result.User.Username, "auth.login_success", "session", result.Session.ID, "authentication")
	}

	// Build response
	response := &LoginResponse{
		Token:     result.Token,
		ExpiresAt: result.ExpiresAt,
		User: &UserInfoResponse{
			ID:          result.User.ID,
			Username:    result.User.Username,
			DisplayName: result.User.DisplayName,
			Role:        result.User.Role,
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
		ID:          user.ID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Role:        user.Role,
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
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
