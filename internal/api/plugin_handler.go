package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/plugins"
	"github.com/docker-secret-operator/dso/internal/storage"
	"go.uber.org/zap"
)

// PluginHandler handles plugin-related API requests
type PluginHandler struct {
	manager   *plugins.Manager
	store     storage.PluginStore
	logger    *zap.Logger
	eventBus  *plugins.EventBus
}

// NewPluginHandler creates a new plugin handler
func NewPluginHandler(manager *plugins.Manager, store storage.PluginStore, eventBus *plugins.EventBus, logger *zap.Logger) *PluginHandler {
	return &PluginHandler{
		manager:   manager,
		store:     store,
		logger:    logger,
		eventBus:  eventBus,
	}
}

// PluginResponse represents a plugin in API responses
type PluginResponse struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Version       string    `json:"version"`
	Type          string    `json:"type"`
	Description   string    `json:"description"`
	Enabled       bool      `json:"enabled"`
	Status        string    `json:"status"`
	Health        string    `json:"health"`
	Capabilities  []string  `json:"capabilities"`
	Dependencies  []string  `json:"dependencies"`
	ErrorMessage  *string   `json:"error_message,omitempty"`
	LoadedAt      *time.Time `json:"loaded_at,omitempty"`
	EnabledAt     *time.Time `json:"enabled_at,omitempty"`
	DisabledAt    *time.Time `json:"disabled_at,omitempty"`
	LastErrorTime *time.Time `json:"last_error_time,omitempty"`
	LastHeartbeat *time.Time `json:"last_heartbeat,omitempty"`
	RestartCount  int       `json:"restart_count"`
	EventCount    int       `json:"event_count"`
	UptimeMs      int64     `json:"uptime_ms"`
	ErrorCount    int       `json:"error_count"`
}

// ServeHTTP routes plugin API requests
func (ph *PluginHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	user := auth.CurrentUser(r.Context())
	if user == nil || user.Role != "admin" {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "admin access required"})
		return
	}

	path := r.URL.Path

	switch {
	case path == "/api/plugins" && r.Method == "GET":
		ph.ListPlugins(w, r)
	case path == "/api/plugins/status" && r.Method == "GET":
		ph.GetPluginStatus(w, r)
	case matchPluginPath(path, "/api/plugins/{id}") && r.Method == "GET":
		ph.GetPlugin(w, r)
	case matchPluginPath(path, "/api/plugins/{id}/enable") && r.Method == "POST":
		ph.EnablePlugin(w, r)
	case matchPluginPath(path, "/api/plugins/{id}/disable") && r.Method == "POST":
		ph.DisablePlugin(w, r)
	case matchPluginPath(path, "/api/plugins/{id}/events") && r.Method == "GET":
		ph.GetPluginEvents(w, r)
	case matchPluginPath(path, "/api/plugins/{id}/config") && r.Method == "GET":
		ph.GetPluginConfig(w, r)
	case matchPluginPath(path, "/api/plugins/{id}/config") && r.Method == "PUT":
		ph.SavePluginConfig(w, r)
	case matchPluginPath(path, "/api/plugins/{id}/health") && r.Method == "GET":
		ph.GetPluginHealth(w, r)
	case matchPluginPath(path, "/api/plugins/{id}/metrics") && r.Method == "GET":
		ph.GetPluginMetrics(w, r)
	case matchPluginPath(path, "/api/plugins/{id}/dependencies") && r.Method == "GET":
		ph.GetPluginDependencies(w, r)
	case matchPluginPath(path, "/api/plugins/{id}/restart") && r.Method == "POST":
		ph.RestartPlugin(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "endpoint not found"})
	}
}

// ListPlugins handles GET /api/plugins
func (ph *PluginHandler) ListPlugins(w http.ResponseWriter, r *http.Request) {
	pluginList := ph.manager.GetAllMetadata()
	responses := make([]PluginResponse, 0, len(pluginList))

	for _, meta := range pluginList {
		caps := make([]string, 0)
		for _, c := range meta.Capabilities {
			caps = append(caps, string(c))
		}

		response := PluginResponse{
			ID:            meta.ID,
			Name:          meta.Name,
			Version:       meta.Version,
			Type:          string(meta.Type),
			Description:   meta.Description,
			Enabled:       meta.Enabled,
			Status:        string(meta.Status),
			Health:        string(meta.Health),
			Capabilities:  caps,
			Dependencies:  meta.Dependencies,
			ErrorMessage:  meta.ErrorMessage,
			LoadedAt:      meta.LoadedAt,
			EnabledAt:     meta.EnabledAt,
			DisabledAt:    meta.DisabledAt,
			LastErrorTime: meta.LastErrorTime,
			LastHeartbeat: meta.LastHeartbeat,
			RestartCount:  meta.RestartCount,
			EventCount:    meta.EventCount,
			UptimeMs:      meta.UptimeMs,
			ErrorCount:    meta.ErrorCount,
		}
		responses = append(responses, response)
	}

	json.NewEncoder(w).Encode(responses)
}

// GetPlugin handles GET /api/plugins/{id}
func (ph *PluginHandler) GetPlugin(w http.ResponseWriter, r *http.Request) {
	pluginID := r.PathValue("id")
	meta := ph.manager.GetMetadata(pluginID)
	if meta == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Plugin not found"})
		return
	}

	caps := make([]string, 0)
	for _, c := range meta.Capabilities {
		caps = append(caps, string(c))
	}

	response := PluginResponse{
		ID:            meta.ID,
		Name:          meta.Name,
		Version:       meta.Version,
		Type:          string(meta.Type),
		Description:   meta.Description,
		Enabled:       meta.Enabled,
		Status:        string(meta.Status),
		Health:        string(meta.Health),
		Capabilities:  caps,
		Dependencies:  meta.Dependencies,
		ErrorMessage:  meta.ErrorMessage,
		LoadedAt:      meta.LoadedAt,
		EnabledAt:     meta.EnabledAt,
		DisabledAt:    meta.DisabledAt,
		LastErrorTime: meta.LastErrorTime,
		LastHeartbeat: meta.LastHeartbeat,
		RestartCount:  meta.RestartCount,
		EventCount:    meta.EventCount,
		UptimeMs:      meta.UptimeMs,
		ErrorCount:    meta.ErrorCount,
	}

	json.NewEncoder(w).Encode(response)
}

// EnablePlugin handles POST /api/plugins/{id}/enable
func (ph *PluginHandler) EnablePlugin(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r.Context())
	pluginID := r.PathValue("id")
	if err := ph.manager.EnablePlugin(r.Context(), pluginID); err != nil {
		ph.logger.Error("failed to enable plugin", zap.String("plugin_id", pluginID), zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if ph.eventBus != nil && user != nil {
		ph.eventBus.Publish(plugins.Event{
			Type:          plugins.PluginEnabled,
			CorrelationID: r.Header.Get("X-Correlation-ID"),
			Payload: map[string]interface{}{
				"plugin_id": pluginID,
				"actor_id":  user.ID,
			},
		})
	}

	json.NewEncoder(w).Encode(map[string]string{"message": "Plugin enabled"})
}

// DisablePlugin handles POST /api/plugins/{id}/disable
func (ph *PluginHandler) DisablePlugin(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r.Context())
	pluginID := r.PathValue("id")
	if err := ph.manager.DisablePlugin(r.Context(), pluginID); err != nil {
		ph.logger.Error("failed to disable plugin", zap.String("plugin_id", pluginID), zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if ph.eventBus != nil && user != nil {
		ph.eventBus.Publish(plugins.Event{
			Type:          plugins.PluginDisabled,
			CorrelationID: r.Header.Get("X-Correlation-ID"),
			Payload: map[string]interface{}{
				"plugin_id": pluginID,
				"actor_id":  user.ID,
			},
		})
	}

	json.NewEncoder(w).Encode(map[string]string{"message": "Plugin disabled"})
}

// GetPluginEvents handles GET /api/plugins/{id}/events
func (ph *PluginHandler) GetPluginEvents(w http.ResponseWriter, r *http.Request) {
	pluginID := r.PathValue("id")
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		var parsedLimit int
		_ = json.Unmarshal([]byte(l), &parsedLimit)
		if parsedLimit > 0 {
			limit = parsedLimit
		}
	}

	events, err := ph.store.GetEvents(r.Context(), pluginID, limit)
	if err != nil {
		ph.logger.Error("failed to get plugin events", zap.String("plugin_id", pluginID), zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	eventResponses := make([]map[string]interface{}, 0)
	for _, evt := range events {
		eventResponses = append(eventResponses, map[string]interface{}{
			"id":         evt.ID,
			"plugin_id":  evt.PluginID,
			"level":      evt.Level,
			"message":    evt.Message,
			"created_at": evt.CreatedAt,
		})
	}

	json.NewEncoder(w).Encode(eventResponses)
}

// GetPluginConfig handles GET /api/plugins/{id}/config
func (ph *PluginHandler) GetPluginConfig(w http.ResponseWriter, r *http.Request) {
	pluginID := r.PathValue("id")
	config, err := ph.store.GetConfig(r.Context(), pluginID)
	if err != nil {
		ph.logger.Error("failed to get plugin config", zap.String("plugin_id", pluginID), zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if config == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{})
		return
	}

	var configJSON interface{}
	_ = json.Unmarshal([]byte(config.ConfigJSON), &configJSON)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"config":     configJSON,
		"updated_at": config.UpdatedAt,
	})
}

// SavePluginConfig handles PUT /api/plugins/{id}/config
func (ph *PluginHandler) SavePluginConfig(w http.ResponseWriter, r *http.Request) {
	pluginID := r.PathValue("id")

	var configData map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&configData); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	configJSON, _ := json.Marshal(configData)
	config := &storage.PluginConfig{
		PluginID:   pluginID,
		ConfigJSON: string(configJSON),
		UpdatedAt:  time.Now(),
	}

	if err := ph.store.SaveConfig(r.Context(), config); err != nil {
		ph.logger.Error("failed to save plugin config", zap.String("plugin_id", pluginID), zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"message": "Configuration saved"})
}

// GetPluginStatus handles GET /api/plugins/status (system-wide status)
func (ph *PluginHandler) GetPluginStatus(w http.ResponseWriter, r *http.Request) {
	pluginList := ph.manager.GetAllMetadata()
	healthyCount := 0
	degradedCount := 0
	failedCount := 0
	disabledCount := 0

	for _, p := range pluginList {
		switch p.Health {
		case plugins.HealthHealthy:
			healthyCount++
		case plugins.HealthDegraded:
			degradedCount++
		case plugins.HealthFailed:
			failedCount++
		case plugins.HealthDisabled:
			disabledCount++
		}
	}

	status := "healthy"
	if failedCount > 0 {
		status = "failed"
	} else if degradedCount > 0 {
		status = "degraded"
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":           status,
		"total_plugins":    len(pluginList),
		"healthy":          healthyCount,
		"degraded":         degradedCount,
		"failed":           failedCount,
		"disabled":         disabledCount,
		"timestamp":        time.Now(),
	})
}

// GetPluginHealth handles GET /api/plugins/{id}/health
func (ph *PluginHandler) GetPluginHealth(w http.ResponseWriter, r *http.Request) {
	pluginID := r.PathValue("id")
	meta := ph.manager.GetMetadata(pluginID)
	if meta == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Plugin not found"})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"plugin_id":       meta.ID,
		"health":          string(meta.Health),
		"status":          string(meta.Status),
		"last_heartbeat":  meta.LastHeartbeat,
		"last_error":      meta.ErrorMessage,
		"last_error_time": meta.LastErrorTime,
	})
}

// GetPluginMetrics handles GET /api/plugins/{id}/metrics
func (ph *PluginHandler) GetPluginMetrics(w http.ResponseWriter, r *http.Request) {
	pluginID := r.PathValue("id")
	metrics := ph.manager.GetMetrics(pluginID)
	if metrics == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Plugin not found"})
		return
	}

	json.NewEncoder(w).Encode(metrics)
}

// GetPluginDependencies handles GET /api/plugins/{id}/dependencies
func (ph *PluginHandler) GetPluginDependencies(w http.ResponseWriter, r *http.Request) {
	pluginID := r.PathValue("id")
	meta := ph.manager.GetMetadata(pluginID)
	if meta == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Plugin not found"})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"plugin":       meta.ID,
		"dependencies": meta.Dependencies,
	})
}

// RestartPlugin handles POST /api/plugins/{id}/restart
func (ph *PluginHandler) RestartPlugin(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r.Context())
	pluginID := r.PathValue("id")

	plugin := ph.manager.GetPlugin(pluginID)
	if plugin == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Plugin not found"})
		return
	}

	// Shutdown
	if err := plugin.Shutdown(r.Context()); err != nil {
		ph.logger.Error("failed to shutdown plugin for restart", zap.String("plugin_id", pluginID), zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to shutdown plugin"})
		return
	}

	// Reinitialize
	if err := plugin.Initialize(r.Context()); err != nil {
		ph.logger.Error("failed to reinitialize plugin", zap.String("plugin_id", pluginID), zap.Error(err))
		errMsg := err.Error()
		meta := ph.manager.GetMetadata(pluginID)
		if meta != nil {
			meta.ErrorMessage = &errMsg
			meta.Health = plugins.HealthFailed
			meta.RestartCount++
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to initialize plugin"})
		return
	}

	// Update metadata
	meta := ph.manager.GetMetadata(pluginID)
	if meta != nil {
		meta.Health = plugins.HealthHealthy
		meta.RestartCount++
		meta.ErrorMessage = nil
	}

	// Publish event
	if ph.eventBus != nil && user != nil {
		ph.eventBus.Publish(plugins.Event{
			Type:          "plugin.restarted",
			CorrelationID: r.Header.Get("X-Correlation-ID"),
			Payload: map[string]interface{}{
				"plugin_id": pluginID,
				"actor_id":  user.ID,
			},
		})
	}

	json.NewEncoder(w).Encode(map[string]string{"message": "Plugin restarted"})
}

// matchPluginPath matches parameterized paths like /api/plugins/{id}
func matchPluginPath(path, pattern string) bool {
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
