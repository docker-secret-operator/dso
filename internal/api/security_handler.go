package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/services"
)

type SecurityHandler struct {
	securityService *services.SecurityService
}

func NewSecurityHandler(securityService *services.SecurityService) *SecurityHandler {
	return &SecurityHandler{securityService: securityService}
}

type SecurityEventResponse struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Severity  string    `json:"severity"`
	Username  string    `json:"username"`
	IPAddress string    `json:"ip_address"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

type SecurityOverviewResponse struct {
	ActiveSessions      int                    `json:"active_sessions"`
	LockedAccounts      int                    `json:"locked_accounts"`
	DisabledUsers       int                    `json:"disabled_users"`
	FailedLogins24h     int                    `json:"failed_logins_24h"`
	SuccessfulLogins24h int                    `json:"successful_logins_24h"`
	PasswordResets24h   int                    `json:"password_resets_24h"`
	ActiveAdmins        int                    `json:"active_admins"`
	SuspiciousActivities int                  `json:"suspicious_activities"`
	Trends              map[string]string      `json:"trends"`
}

type SecurityAlertResponse struct {
	ID             string     `json:"id"`
	Type           string     `json:"type"`
	Severity       string     `json:"severity"`
	State          string     `json:"state"`
	Title          string     `json:"title"`
	Message        string     `json:"message"`
	AffectedUser   *string    `json:"affected_user,omitempty"`
	IPAddress      *string    `json:"ip_address,omitempty"`
	AcknowledgedBy *string    `json:"acknowledged_by,omitempty"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

type AcknowledgeAlertRequest struct {
	State string `json:"state"`
}

func (h *SecurityHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	user := auth.CurrentUser(r.Context())
	if user == nil || user.Role != "admin" {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "admin access required"})
		return
	}

	if strings.HasSuffix(r.URL.Path, "/security/events") && r.Method == "GET" {
		h.handleGetEvents(w, r)
	} else if strings.HasSuffix(r.URL.Path, "/security/overview") && r.Method == "GET" {
		h.handleGetOverview(w, r)
	} else if strings.HasSuffix(r.URL.Path, "/security/alerts") && r.Method == "GET" {
		h.handleGetAlerts(w, r)
	} else if strings.Contains(r.URL.Path, "/security/alerts/") && strings.HasSuffix(r.URL.Path, "/acknowledge") && r.Method == "POST" {
		h.handleAcknowledgeAlert(w, r)
	} else if strings.HasSuffix(r.URL.Path, "/security/suspicious") && r.Method == "GET" {
		h.handleGetSuspiciousActivities(w, r)
	} else if strings.Contains(r.URL.Path, "/security/suspicious/") && strings.HasSuffix(r.URL.Path, "/acknowledge") && r.Method == "POST" {
		h.handleAcknowledgeSuspiciousActivity(w, r)
	} else if strings.HasSuffix(r.URL.Path, "/security/sessions") && r.Method == "GET" {
		h.handleGetSessions(w, r)
	} else {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "endpoint not found"})
	}
}

func (h *SecurityHandler) handleGetEvents(w http.ResponseWriter, r *http.Request) {
	filters := make(map[string]interface{})

	if severity := r.URL.Query().Get("severity"); severity != "" {
		filters["severity"] = severity
	}
	if eventType := r.URL.Query().Get("type"); eventType != "" {
		filters["type"] = eventType
	}
	if actor := r.URL.Query().Get("actor"); actor != "" {
		filters["actor"] = actor
	}
	if ip := r.URL.Query().Get("ip_address"); ip != "" {
		filters["ip_address"] = ip
	}
	if username := r.URL.Query().Get("username"); username != "" {
		filters["username"] = username
	}

	if startStr := r.URL.Query().Get("start_time"); startStr != "" {
		if start, err := time.Parse(time.RFC3339, startStr); err == nil {
			filters["start_time"] = start
		}
	}
	if endStr := r.URL.Query().Get("end_time"); endStr != "" {
		if end, err := time.Parse(time.RFC3339, endStr); err == nil {
			filters["end_time"] = end
		}
	}

	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}
	filters["limit"] = limit

	offset := 0
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil {
			offset = o
		}
	}
	filters["offset"] = offset

	events, err := h.securityService.GetSecurityEvents(r.Context(), filters)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to fetch events"})
		return
	}

	response := make([]SecurityEventResponse, 0, len(events))
	for _, e := range events {
		response = append(response, SecurityEventResponse{
			ID:        e.ID,
			Type:      e.Type,
			Severity:  e.Severity,
			Username:  e.Username,
			IPAddress: e.IPAddress,
			Message:   e.Message,
			CreatedAt: e.CreatedAt,
		})
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func (h *SecurityHandler) handleGetOverview(w http.ResponseWriter, r *http.Request) {
	overview, err := h.securityService.GetSecurityOverview(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to fetch overview"})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(overview)
}

func (h *SecurityHandler) handleGetAlerts(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}

	offset := 0
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil {
			offset = o
		}
	}

	var state string
	if s := r.URL.Query().Get("state"); s != "" {
		state = s
	}

	alerts, err := h.securityService.GetSecurityAlerts(r.Context(), state, limit, offset)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to fetch alerts"})
		return
	}

	response := make([]SecurityAlertResponse, 0, len(alerts))
	for _, a := range alerts {
		response = append(response, SecurityAlertResponse{
			ID:             a.ID,
			Type:           a.Type,
			Severity:       a.Severity,
			State:          a.State,
			Title:          a.Title,
			Message:        a.Message,
			AffectedUser:   a.AffectedUser,
			IPAddress:      a.IPAddress,
			AcknowledgedBy: a.AcknowledgedBy,
			AcknowledgedAt: a.AcknowledgedAt,
			ResolvedAt:     a.ResolvedAt,
			CreatedAt:      a.CreatedAt,
		})
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func (h *SecurityHandler) handleAcknowledgeAlert(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	alertID := ""
	for i, p := range parts {
		if p == "alerts" && i+1 < len(parts) {
			alertID = parts[i+1]
			break
		}
	}

	if alertID == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid alert id"})
		return
	}

	var req AcknowledgeAlertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	user := auth.CurrentUser(r.Context())
	err := h.securityService.AcknowledgeAlert(r.Context(), alertID, req.State, user.Username)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to acknowledge alert"})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"success": "alert acknowledged"})
}

func (h *SecurityHandler) handleGetSuspiciousActivities(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}

	offset := 0
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil {
			offset = o
		}
	}

	activities, err := h.securityService.GetSuspiciousActivities(r.Context(), limit, offset)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to fetch suspicious activities"})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(activities)
}

func (h *SecurityHandler) handleAcknowledgeSuspiciousActivity(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	activityID := ""
	for i, p := range parts {
		if p == "suspicious" && i+1 < len(parts) {
			activityID = parts[i+1]
			break
		}
	}

	if activityID == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid activity id"})
		return
	}

	user := auth.CurrentUser(r.Context())
	err := h.securityService.AcknowledgeSuspiciousActivity(r.Context(), activityID, user.Username)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to acknowledge activity"})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"success": "activity acknowledged"})
}

func (h *SecurityHandler) handleGetSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := h.securityService.GetActiveSessions(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to fetch sessions"})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(sessions)
}
