package plugins

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
	"go.uber.org/zap"
)

// Manager handles plugin lifecycle
type Manager struct {
	registry     *Registry
	store        storage.PluginStore
	logger       *zap.Logger
	eventBus     *EventBus
	metadata     map[string]*PluginMetadata
	mu           sync.RWMutex
	monitor      *HealthMonitor
	startTimes   map[string]time.Time
	metricsLock  sync.RWMutex
}

// NewManager creates a new plugin manager
func NewManager(registry *Registry, store storage.PluginStore, logger *zap.Logger) *Manager {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Manager{
		registry:   registry,
		store:      store,
		logger:     logger,
		metadata:   make(map[string]*PluginMetadata),
		startTimes: make(map[string]time.Time),
	}
}

// SetEventBus sets the event bus for the manager
func (m *Manager) SetEventBus(eventBus *EventBus) {
	m.eventBus = eventBus
}

// GetPlugin returns a plugin by ID
func (m *Manager) GetPlugin(pluginID string) Plugin {
	return m.registry.Get(pluginID)
}

// Initialize starts all registered plugins
func (m *Manager) Initialize(ctx context.Context) error {
	plugins := m.registry.List()

	// Build dependency graph
	depGraph := NewDependencyGraph(plugins)

	// Get sorted initialization order
	order, err := depGraph.TopologicalSort()
	if err != nil {
		m.logger.Error("failed to resolve plugin dependencies", zap.Error(err))
		// Continue initialization with disabled plugins for unresolvable dependencies
	}

	// Initialize all plugins (disabled ones will be skipped)
	for _, pluginID := range order {
		plugin := m.registry.Get(pluginID)
		if plugin == nil {
			continue
		}

		m.initializePlugin(ctx, plugin)
	}

	// Start health monitor
	m.monitor = NewHealthMonitor(m, 60*time.Second, m.logger)
	if err := m.monitor.Start(ctx); err != nil {
		m.logger.Error("failed to start health monitor", zap.Error(err))
	}

	return nil
}

// initializePlugin initializes a single plugin
func (m *Manager) initializePlugin(ctx context.Context, plugin Plugin) {
	id := plugin.ID()
	loadedAt := time.Now()

	// Get stored metadata
	var storedMeta *storage.Plugin
	if m.store != nil {
		var err error
		storedMeta, err = m.store.GetByID(ctx, id)
		if err != nil {
			m.logger.Warn("failed to get plugin metadata", zap.String("plugin_id", id), zap.Error(err))
		}
	}

	// Check if plugin is disabled
	if storedMeta != nil && !storedMeta.Enabled {
		m.logger.Info("Skipping disabled plugin", zap.String("plugin_id", id))
		m.setMetadata(id, &PluginMetadata{
			ID:           id,
			Name:         plugin.Name(),
			Version:      plugin.Version(),
			Type:         plugin.Type(),
			Description:  plugin.Description(),
			Capabilities: plugin.Capabilities(),
			Dependencies: plugin.Dependencies(),
			Enabled:      false,
			Status:       StatusDisabled,
			Health:       HealthDisabled,
			LoadedAt:     &loadedAt,
			RestartCount: storedMeta.RestartCount,
			EventCount:   storedMeta.EventCount,
		})
		return
	}

	// Initialize plugin
	if err := plugin.Initialize(ctx); err != nil {
		m.logger.Error("failed to initialize plugin", zap.String("plugin_id", id), zap.Error(err))
		errMsg := err.Error()
		now := time.Now()
		m.setMetadata(id, &PluginMetadata{
			ID:           id,
			Name:         plugin.Name(),
			Version:      plugin.Version(),
			Type:         plugin.Type(),
			Description:  plugin.Description(),
			Capabilities: plugin.Capabilities(),
			Dependencies: plugin.Dependencies(),
			Enabled:      true,
			Status:       StatusFailed,
			Health:       HealthFailed,
			ErrorMessage: &errMsg,
			LoadedAt:     &loadedAt,
			LastErrorTime: &now,
			RestartCount: 1,
		})
		// Persist to storage
		if m.store != nil {
			m.persistMetadata(ctx, id)
		}
		return
	}

	m.logger.Info("Plugin initialized",
		zap.String("plugin_id", id),
		zap.String("name", plugin.Name()),
		zap.String("version", plugin.Version()))

	enabledAt := time.Now()
	m.setMetadata(id, &PluginMetadata{
		ID:           id,
		Name:         plugin.Name(),
		Version:      plugin.Version(),
		Type:         plugin.Type(),
		Description:  plugin.Description(),
		Capabilities: plugin.Capabilities(),
		Dependencies: plugin.Dependencies(),
		Enabled:      true,
		Status:       StatusEnabled,
		Health:       HealthHealthy,
		LoadedAt:     &loadedAt,
		EnabledAt:    &enabledAt,
		EventCount:   0,
		RestartCount: 0,
	})

	// Track start time for metrics
	m.metricsLock.Lock()
	m.startTimes[id] = enabledAt
	m.metricsLock.Unlock()

	// Persist to storage
	if m.store != nil {
		m.persistMetadata(ctx, id)
	}
}

// Shutdown stops all initialized plugins
func (m *Manager) Shutdown(ctx context.Context) error {
	// Stop health monitor first
	if m.monitor != nil {
		m.monitor.Stop()
	}

	plugins := m.registry.List()
	var errs []error

	for _, plugin := range plugins {
		if err := plugin.Shutdown(ctx); err != nil {
			m.logger.Error("failed to shutdown plugin", zap.String("plugin_id", plugin.ID()), zap.Error(err))
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}

	return nil
}

// GetMetadata returns metadata for a plugin
func (m *Manager) GetMetadata(id string) *PluginMetadata {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.metadata[id]
}

// GetAllMetadata returns metadata for all plugins
func (m *Manager) GetAllMetadata() []*PluginMetadata {
	m.mu.RLock()
	defer m.mu.RUnlock()

	metadata := make([]*PluginMetadata, 0, len(m.metadata))
	for _, meta := range m.metadata {
		metadata = append(metadata, meta)
	}
	return metadata
}

// EnablePlugin enables a plugin
func (m *Manager) EnablePlugin(ctx context.Context, id string) error {
	meta := m.GetMetadata(id)
	if meta == nil {
		return fmt.Errorf("plugin not found")
	}

	meta.Enabled = true
	meta.Status = StatusEnabled
	meta.ErrorMessage = nil

	if m.store != nil {
		if err := m.persistMetadata(ctx, id); err != nil {
			return fmt.Errorf("failed to persist plugin metadata: %w", err)
		}
	}

	return nil
}

// DisablePlugin disables a plugin
func (m *Manager) DisablePlugin(ctx context.Context, id string) error {
	meta := m.GetMetadata(id)
	if meta == nil {
		return fmt.Errorf("plugin not found")
	}

	meta.Enabled = false
	meta.Status = StatusDisabled

	if m.store != nil {
		if err := m.persistMetadata(ctx, id); err != nil {
			return fmt.Errorf("failed to persist plugin metadata: %w", err)
		}
	}

	return nil
}

// setMetadata sets metadata for a plugin (internal, not locked)
func (m *Manager) setMetadata(id string, meta *PluginMetadata) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metadata[id] = meta
}

// persistMetadata saves metadata to storage
func (m *Manager) persistMetadata(ctx context.Context, id string) error {
	if m.store == nil {
		return nil
	}

	meta := m.GetMetadata(id)
	if meta == nil {
		return fmt.Errorf("metadata not found")
	}

	// Check if already exists
	existing, err := m.store.GetByID(ctx, id)
	if err != nil {
		return err
	}

	storagePlugin := &storage.Plugin{
		ID:            meta.ID,
		Name:          meta.Name,
		Version:       meta.Version,
		Type:          string(meta.Type),
		Enabled:       meta.Enabled,
		Status:        string(meta.Status),
		Health:        string(meta.Health),
		ErrorMessage:  meta.ErrorMessage,
		LoadedAt:      meta.LoadedAt,
		EnabledAt:     meta.EnabledAt,
		DisabledAt:    meta.DisabledAt,
		RestartCount:  meta.RestartCount,
		EventCount:    meta.EventCount,
		LastErrorTime: meta.LastErrorTime,
		LastHeartbeat: meta.LastHeartbeat,
	}

	if existing == nil {
		return m.store.Create(ctx, storagePlugin)
	}

	return m.store.Update(ctx, storagePlugin)
}

// PluginMetrics represents runtime metrics for a plugin
type PluginMetrics struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	UptimeSeconds int64  `json:"uptime_seconds"`
	EventCount    int    `json:"events_total"`
	ErrorCount    int    `json:"errors_total"`
	RestartCount  int    `json:"restarts_total"`
	Health        string `json:"health"`
	Status        string `json:"status"`
}

// GetMetrics returns metrics for a specific plugin
func (m *Manager) GetMetrics(pluginID string) *PluginMetrics {
	meta := m.GetMetadata(pluginID)
	if meta == nil {
		return nil
	}

	m.metricsLock.RLock()
	startTime, ok := m.startTimes[pluginID]
	m.metricsLock.RUnlock()

	uptime := int64(0)
	if ok {
		uptime = int64(time.Since(startTime).Seconds())
	}

	return &PluginMetrics{
		ID:            meta.ID,
		Name:          meta.Name,
		UptimeSeconds: uptime,
		EventCount:    meta.EventCount,
		ErrorCount:    meta.ErrorCount,
		RestartCount:  meta.RestartCount,
		Health:        string(meta.Health),
		Status:        string(meta.Status),
	}
}

// GetAllMetrics returns metrics for all plugins
func (m *Manager) GetAllMetrics() []*PluginMetrics {
	plugins := m.GetAllMetadata()
	metrics := make([]*PluginMetrics, 0, len(plugins))

	for _, meta := range plugins {
		if pm := m.GetMetrics(meta.ID); pm != nil {
			metrics = append(metrics, pm)
		}
	}

	return metrics
}
