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
)

type AlertHandler struct {
	alertService *services.AlertService
}

func NewAlertHandler(alertService *services.AlertService) *AlertHandler {
	return &AlertHandler{alertService: alertService}
}

type AlertRuleResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	Enabled     bool    `json:"enabled"`
	Severity    string  `json:"severity"`
	Metric      string  `json:"metric"`
	Operator    string  `json:"operator"`
	Threshold   float64 `json:"threshold"`
	Duration    int     `json:"duration_seconds"`
	Cooldown    int     `json:"cooldown_seconds"`
	IsBuiltin   bool    `json:"is_builtin"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type MetricAlertResponse struct {
	ID              string     `json:"id"`
	RuleID          string     `json:"rule_id"`
	State           string     `json:"state"`
	Severity        string     `json:"severity"`
	Metric          string     `json:"metric"`
	Message         string     `json:"message"`
	Value           float64    `json:"value"`
	Threshold       float64    `json:"threshold"`
	AcknowledgedBy  *string    `json:"acknowledged_by,omitempty"`
	AcknowledgedAt  *time.Time `json:"acknowledged_at,omitempty"`
	ResolvedBy      *string    `json:"resolved_by,omitempty"`
	ResolvedAt      *time.Time `json:"resolved_at,omitempty"`
	SuppressedBy    *string    `json:"suppressed_by,omitempty"`
	SuppressedUntil *time.Time `json:"suppressed_until,omitempty"`
	LastFiredAt     time.Time  `json:"last_fired_at"`
	CreatedAt       time.Time  `json:"created_at"`
}

type CreateRuleRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	Enabled     bool    `json:"enabled"`
	Severity    string  `json:"severity"`
	Metric      string  `json:"metric"`
	Operator    string  `json:"operator"`
	Threshold   float64 `json:"threshold"`
	Duration    int     `json:"duration_seconds"`
	Cooldown    int     `json:"cooldown_seconds"`
}

type UpdateRuleRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	Enabled     bool    `json:"enabled"`
	Severity    string  `json:"severity"`
	Threshold   float64 `json:"threshold"`
	Duration    int     `json:"duration_seconds"`
	Cooldown    int     `json:"cooldown_seconds"`
}

type AlertActionRequest struct {
	SuppressUntil *time.Time `json:"suppress_until,omitempty"`
}

func (h *AlertHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	user := auth.CurrentUser(r.Context())
	if user == nil || (r.Method != http.MethodGet && user.Role != "admin") {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "admin access required"})
		return
	}

	switch {
	case strings.HasSuffix(r.URL.Path, "/alerts") && r.Method == "GET":
		h.handleGetAlerts(w, r)
	case strings.HasSuffix(r.URL.Path, "/alerts/rules") && r.Method == "GET":
		h.handleGetRules(w, r)
	case strings.HasSuffix(r.URL.Path, "/alerts/rules") && r.Method == "POST":
		h.handleCreateRule(w, r)
	case strings.Contains(r.URL.Path, "/alerts/rules/") && strings.HasSuffix(r.URL.Path, "") && r.Method == "PUT":
		h.handleUpdateRule(w, r)
	case strings.Contains(r.URL.Path, "/alerts/rules/") && strings.HasSuffix(r.URL.Path, "") && r.Method == "DELETE":
		h.handleDeleteRule(w, r)
	case strings.Contains(r.URL.Path, "/alerts/") && strings.HasSuffix(r.URL.Path, "/acknowledge") && r.Method == "POST":
		h.handleAcknowledgeAlert(w, r)
	case strings.Contains(r.URL.Path, "/alerts/") && strings.HasSuffix(r.URL.Path, "/resolve") && r.Method == "POST":
		h.handleResolveAlert(w, r)
	case strings.Contains(r.URL.Path, "/alerts/") && strings.HasSuffix(r.URL.Path, "/suppress") && r.Method == "POST":
		h.handleSuppressAlert(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "endpoint not found"})
	}
}

func (h *AlertHandler) handleGetAlerts(w http.ResponseWriter, r *http.Request) {
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

	state := r.URL.Query().Get("state")

	alerts, err := h.alertService.GetAlerts(r.Context(), state, limit, offset)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to fetch alerts"})
		return
	}

	response := make([]MetricAlertResponse, 0, len(alerts))
	for _, a := range alerts {
		response = append(response, MetricAlertResponse{
			ID:              a.ID,
			RuleID:          a.RuleID,
			State:           a.State,
			Severity:        a.Severity,
			Metric:          a.Metric,
			Message:         a.Message,
			Value:           a.Value,
			Threshold:       a.Threshold,
			AcknowledgedBy:  a.AcknowledgedBy,
			AcknowledgedAt:  a.AcknowledgedAt,
			ResolvedBy:      a.ResolvedBy,
			ResolvedAt:      a.ResolvedAt,
			SuppressedBy:    a.SuppressedBy,
			SuppressedUntil: a.SuppressedUntil,
			LastFiredAt:     a.LastFiredAt,
			CreatedAt:       a.CreatedAt,
		})
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func (h *AlertHandler) handleGetRules(w http.ResponseWriter, r *http.Request) {
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

	rules, err := h.alertService.GetRules(r.Context(), limit, offset)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to fetch rules"})
		return
	}

	response := make([]AlertRuleResponse, 0, len(rules))
	for _, rule := range rules {
		response = append(response, AlertRuleResponse{
			ID:          rule.ID,
			Name:        rule.Name,
			Description: rule.Description,
			Enabled:     rule.Enabled,
			Severity:    rule.Severity,
			Metric:      rule.Metric,
			Operator:    rule.Operator,
			Threshold:   rule.Threshold,
			Duration:    rule.Duration,
			Cooldown:    rule.Cooldown,
			IsBuiltin:   rule.IsBuiltin,
			CreatedAt:   rule.CreatedAt,
			UpdatedAt:   rule.UpdatedAt,
		})
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func (h *AlertHandler) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	var req CreateRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	rule := &storage.AlertRule{
		Name:        req.Name,
		Description: req.Description,
		Enabled:     req.Enabled,
		Severity:    req.Severity,
		Metric:      req.Metric,
		Operator:    req.Operator,
		Threshold:   req.Threshold,
		Duration:    req.Duration,
		Cooldown:    req.Cooldown,
	}

	if err := h.alertService.CreateRule(r.Context(), rule); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to create rule"})
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(AlertRuleResponse{
		ID:          rule.ID,
		Name:        rule.Name,
		Description: rule.Description,
		Enabled:     rule.Enabled,
		Severity:    rule.Severity,
		Metric:      rule.Metric,
		Operator:    rule.Operator,
		Threshold:   rule.Threshold,
		Duration:    rule.Duration,
		Cooldown:    rule.Cooldown,
		IsBuiltin:   rule.IsBuiltin,
		CreatedAt:   rule.CreatedAt,
		UpdatedAt:   rule.UpdatedAt,
	})
}

func (h *AlertHandler) handleUpdateRule(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	ruleID := ""
	for i, p := range parts {
		if p == "rules" && i+1 < len(parts) {
			ruleID = parts[i+1]
			break
		}
	}

	if ruleID == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid rule id"})
		return
	}

	var req UpdateRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	rule, err := h.alertService.GetRules(r.Context(), 1, 0)
	if err != nil || len(rule) == 0 {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "rule not found"})
		return
	}

	rule[0].Name = req.Name
	rule[0].Description = req.Description
	rule[0].Enabled = req.Enabled
	rule[0].Severity = req.Severity
	rule[0].Threshold = req.Threshold
	rule[0].Duration = req.Duration
	rule[0].Cooldown = req.Cooldown

	if err := h.alertService.UpdateRule(r.Context(), rule[0]); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to update rule"})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"success": "rule updated"})
}

func (h *AlertHandler) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	ruleID := ""
	for i, p := range parts {
		if p == "rules" && i+1 < len(parts) {
			ruleID = parts[i+1]
			break
		}
	}

	if ruleID == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid rule id"})
		return
	}

	if err := h.alertService.DeleteRule(r.Context(), ruleID); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"success": "rule deleted"})
}

func (h *AlertHandler) handleAcknowledgeAlert(w http.ResponseWriter, r *http.Request) {
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

	user := auth.CurrentUser(r.Context())
	if err := h.alertService.AcknowledgeAlert(r.Context(), alertID, user.Username); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to acknowledge alert"})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"success": "alert acknowledged"})
}

func (h *AlertHandler) handleResolveAlert(w http.ResponseWriter, r *http.Request) {
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

	user := auth.CurrentUser(r.Context())
	if err := h.alertService.ResolveAlert(r.Context(), alertID, user.Username); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to resolve alert"})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"success": "alert resolved"})
}

func (h *AlertHandler) handleSuppressAlert(w http.ResponseWriter, r *http.Request) {
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

	var req AlertActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	suppressUntil := time.Now().Add(24 * time.Hour)
	if req.SuppressUntil != nil {
		suppressUntil = *req.SuppressUntil
	}

	user := auth.CurrentUser(r.Context())
	if err := h.alertService.SuppressAlert(r.Context(), alertID, user.Username, suppressUntil); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to suppress alert"})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"success": "alert suppressed"})
}
