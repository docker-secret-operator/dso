package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/plugins"
	"github.com/docker-secret-operator/dso/internal/storage"
	"go.uber.org/zap"
)

// IntegrationHandler handles integration API requests
type IntegrationHandler struct {
	manager           *plugins.IntegrationManager
	configStore       storage.IntegrationConfigStore
	deliveryStore     storage.IntegrationDeliveryStore
	logger            *zap.Logger
}

// NewIntegrationHandler creates a new integration handler
func NewIntegrationHandler(
	manager *plugins.IntegrationManager,
	configStore storage.IntegrationConfigStore,
	deliveryStore storage.IntegrationDeliveryStore,
	logger *zap.Logger,
) *IntegrationHandler {
	return &IntegrationHandler{
		manager:       manager,
		configStore:   configStore,
		deliveryStore: deliveryStore,
		logger:        logger,
	}
}

// ServeHTTP routes integration API requests
func (ih *IntegrationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	user := auth.CurrentUser(r.Context())
	if user == nil || user.Role != "admin" {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "admin access required"})
		return
	}

	path := r.URL.Path

	switch {
	case path == "/api/integrations" && r.Method == "GET":
		ih.ListIntegrations(w, r)
	case matchIntegrationPath(path, "/api/integrations/{id}") && r.Method == "GET":
		ih.GetIntegration(w, r)
	case matchIntegrationPath(path, "/api/integrations/{id}/config") && r.Method == "PUT":
		ih.UpdateConfig(w, r)
	case matchIntegrationPath(path, "/api/integrations/{id}/deliveries") && r.Method == "GET":
		ih.GetDeliveries(w, r)
	case matchIntegrationPath(path, "/api/integrations/{id}/test") && r.Method == "POST":
		ih.TestDelivery(w, r)
	case matchIntegrationPath(path, "/api/integrations/{id}/enable") && r.Method == "POST":
		ih.EnableIntegration(w, r)
	case matchIntegrationPath(path, "/api/integrations/{id}/disable") && r.Method == "POST":
		ih.DisableIntegration(w, r)
	case matchIntegrationPath(path, "/api/integrations/{id}/metrics") && r.Method == "GET":
		ih.GetMetrics(w, r)
	case matchIntegrationPath(path, "/api/integrations/{id}/health") && r.Method == "GET":
		ih.GetHealth(w, r)
	case path == "/api/integrations/queue" && r.Method == "GET":
		ih.GetQueueStats(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "endpoint not found"})
	}
}

// ListIntegrations handles GET /api/integrations
func (ih *IntegrationHandler) ListIntegrations(w http.ResponseWriter, r *http.Request) {
	configs, err := ih.configStore.ListConfigs(r.Context())
	if err != nil {
		ih.logger.Error("failed to list configs", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to list integrations"})
		return
	}

	response := make([]map[string]interface{}, 0, len(configs))
	for _, config := range configs {
		metrics := ih.manager.GetMetrics(config.PluginID)
		response = append(response, map[string]interface{}{
			"plugin_id":   config.PluginID,
			"enabled":     config.Enabled,
			"endpoint":    config.Endpoint,
			"auth_type":   config.AuthType,
			"updated_at":  config.UpdatedAt,
			"metrics":     metrics,
		})
	}

	json.NewEncoder(w).Encode(response)
}

// GetIntegration handles GET /api/integrations/{id}
func (ih *IntegrationHandler) GetIntegration(w http.ResponseWriter, r *http.Request) {
	pluginID := r.PathValue("id")
	config, err := ih.configStore.GetConfig(r.Context(), pluginID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to get config"})
		return
	}

	if config == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "integration not found"})
		return
	}

	metrics := ih.manager.GetMetrics(pluginID)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"plugin_id":        config.PluginID,
		"enabled":          config.Enabled,
		"endpoint":         config.Endpoint,
		"auth_type":        config.AuthType,
		"filters_json":     config.FiltersJSON,
		"retry_policy_json": config.RetryPolicyJSON,
		"updated_at":       config.UpdatedAt,
		"metrics":          metrics,
	})
}

// UpdateConfig handles PUT /api/integrations/{id}/config
func (ih *IntegrationHandler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	pluginID := r.PathValue("id")

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	// Load existing config
	config, _ := ih.configStore.GetConfig(r.Context(), pluginID)
	if config == nil {
		config = &storage.IntegrationConfig{PluginID: pluginID}
	}

	// Update fields
	if endpoint, ok := req["endpoint"].(string); ok {
		config.Endpoint = endpoint
	}
	if authType, ok := req["auth_type"].(string); ok {
		config.AuthType = authType
	}
	if authConfig, ok := req["auth_config_json"].(string); ok {
		config.AuthConfigJSON = authConfig
	}
	if filters, ok := req["filters_json"].(string); ok {
		config.FiltersJSON = filters
	}
	if retryPolicy, ok := req["retry_policy_json"].(string); ok {
		config.RetryPolicyJSON = retryPolicy
	}

	if err := ih.configStore.SaveConfig(r.Context(), config); err != nil {
		ih.logger.Error("failed to save config", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to save config"})
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"message": "configuration updated"})
}

// GetDeliveries handles GET /api/integrations/{id}/deliveries
func (ih *IntegrationHandler) GetDeliveries(w http.ResponseWriter, r *http.Request) {
	pluginID := r.PathValue("id")
	limit := 50

	deliveries, err := ih.deliveryStore.GetDeliveries(r.Context(), pluginID, limit)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to get deliveries"})
		return
	}

	response := make([]map[string]interface{}, 0, len(deliveries))
	for _, delivery := range deliveries {
		item := map[string]interface{}{
			"id":            delivery.ID,
			"event_type":    delivery.EventType,
			"success":       delivery.Success,
			"response_code": delivery.ResponseCode,
			"attempt":       delivery.Attempt,
			"created_at":    delivery.CreatedAt,
		}
		if delivery.ErrorMessage != nil {
			item["error_message"] = *delivery.ErrorMessage
		}
		response = append(response, item)
	}

	json.NewEncoder(w).Encode(response)
}

// TestDelivery handles POST /api/integrations/{id}/test
func (ih *IntegrationHandler) TestDelivery(w http.ResponseWriter, r *http.Request) {
	pluginID := r.PathValue("id")

	if err := ih.manager.TestDelivery(pluginID); err != nil {
		ih.logger.Error("test delivery failed", zap.String("plugin_id", pluginID), zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// EnableIntegration handles POST /api/integrations/{id}/enable
func (ih *IntegrationHandler) EnableIntegration(w http.ResponseWriter, r *http.Request) {
	pluginID := r.PathValue("id")

	config, _ := ih.configStore.GetConfig(r.Context(), pluginID)
	if config == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "integration not found"})
		return
	}

	config.Enabled = true
	if err := ih.configStore.SaveConfig(r.Context(), config); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to enable integration"})
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"message": "integration enabled"})
}

// DisableIntegration handles POST /api/integrations/{id}/disable
func (ih *IntegrationHandler) DisableIntegration(w http.ResponseWriter, r *http.Request) {
	pluginID := r.PathValue("id")

	config, _ := ih.configStore.GetConfig(r.Context(), pluginID)
	if config == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "integration not found"})
		return
	}

	config.Enabled = false
	if err := ih.configStore.SaveConfig(r.Context(), config); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to disable integration"})
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"message": "integration disabled"})
}

// GetMetrics handles GET /api/integrations/{id}/metrics
func (ih *IntegrationHandler) GetMetrics(w http.ResponseWriter, r *http.Request) {
	pluginID := r.PathValue("id")
	metrics := ih.manager.GetMetrics(pluginID)

	if metrics == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "metrics not found"})
		return
	}

	json.NewEncoder(w).Encode(metrics)
}

// GetHealth handles GET /api/integrations/{id}/health
func (ih *IntegrationHandler) GetHealth(w http.ResponseWriter, r *http.Request) {
	pluginID := r.PathValue("id")
	config, _ := ih.configStore.GetConfig(r.Context(), pluginID)

	if config == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "integration not found"})
		return
	}

	health := "healthy"
	if !config.Enabled {
		health = "disabled"
	}

	metrics := ih.manager.GetMetrics(pluginID)
	if metrics != nil && metrics.FailedCount > metrics.SuccessfulCount {
		health = "degraded"
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"health":       health,
		"enabled":      config.Enabled,
		"last_success": metrics.LastSuccessTime,
		"last_failure": metrics.LastErrorTime,
		"failures":     metrics.FailedCount,
	})
}

// GetQueueStats handles GET /api/integrations/queue
func (ih *IntegrationHandler) GetQueueStats(w http.ResponseWriter, r *http.Request) {
	stats := ih.manager.GetQueueStats()
	json.NewEncoder(w).Encode(stats)
}

// matchIntegrationPath matches parameterized paths
func matchIntegrationPath(path, pattern string) bool {
	parts := strings.Split(path, "/")
	patternParts := strings.Split(pattern, "/")

	if len(parts) != len(patternParts) {
		return false
	}

	for i := range parts {
		if !strings.HasPrefix(patternParts[i], "{") && parts[i] != patternParts[i] {
			return false
		}
	}

	return true
}
