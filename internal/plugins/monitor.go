package plugins

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// HealthMonitor periodically checks plugin health
type HealthMonitor struct {
	manager      *Manager
	interval     time.Duration
	logger       *zap.Logger
	ctx          context.Context
	cancel       context.CancelFunc
	done         chan struct{}
}

// NewHealthMonitor creates a new health monitor
func NewHealthMonitor(manager *Manager, interval time.Duration, logger *zap.Logger) *HealthMonitor {
	if logger == nil {
		logger = zap.NewNop()
	}

	if interval == 0 {
		interval = 60 * time.Second
	}

	return &HealthMonitor{
		manager:  manager,
		interval: interval,
		logger:   logger,
		done:     make(chan struct{}),
	}
}

// Start begins the health monitoring loop
func (hm *HealthMonitor) Start(ctx context.Context) error {
	hm.ctx, hm.cancel = context.WithCancel(ctx)

	go hm.runMonitorLoop()

	return nil
}

// Stop gracefully stops the health monitor
func (hm *HealthMonitor) Stop() {
	if hm.cancel != nil {
		hm.cancel()
	}

	select {
	case <-hm.done:
	case <-time.After(5 * time.Second):
		hm.logger.Warn("health monitor shutdown timeout")
	}
}

// runMonitorLoop is the main monitoring loop
func (hm *HealthMonitor) runMonitorLoop() {
	defer close(hm.done)

	ticker := time.NewTicker(hm.interval)
	defer ticker.Stop()

	for {
		select {
		case <-hm.ctx.Done():
			return
		case <-ticker.C:
			hm.checkHealth()
		}
	}
}

// checkHealth checks the health of all plugins
func (hm *HealthMonitor) checkHealth() {
	plugins := hm.manager.registry.List()

	for _, plugin := range plugins {
		hm.checkPluginHealth(plugin)
	}
}

// checkPluginHealth checks a single plugin's health
func (hm *HealthMonitor) checkPluginHealth(plugin Plugin) {
	defer func() {
		if r := recover(); r != nil {
			hm.logger.Error("plugin health check panic",
				zap.String("plugin_id", plugin.ID()),
				zap.Any("panic", r))
			hm.updatePluginHealth(plugin.ID(), HealthFailed, "health check panic")
		}
	}()

	id := plugin.ID()
	meta := hm.manager.GetMetadata(id)

	if meta == nil {
		return
	}

	// If plugin is disabled, skip health check
	if !meta.Enabled {
		return
	}

	// Invoke heartbeat
	err := plugin.Heartbeat()
	if err != nil {
		hm.logger.Warn("plugin heartbeat failed",
			zap.String("plugin_id", id),
			zap.Error(err))
		hm.updatePluginHealth(id, HealthDegraded, err.Error())
		return
	}

	// Check plugin's reported health
	health := plugin.Health()
	if health != meta.Health {
		hm.updatePluginHealth(id, health, "")
	}

	// Update last heartbeat time
	hm.updateLastHeartbeat(id)
}

// updatePluginHealth updates a plugin's health status
func (hm *HealthMonitor) updatePluginHealth(pluginID string, health PluginHealth, errorMsg string) {
	meta := hm.manager.GetMetadata(pluginID)
	if meta == nil {
		return
	}

	oldHealth := meta.Health

	// Update metadata
	meta.Health = health
	if errorMsg != "" {
		meta.ErrorMessage = &errorMsg
		now := time.Now()
		meta.LastErrorTime = &now
		meta.ErrorCount++
	}

	// Persist change
	if hm.manager.store != nil {
		_ = hm.manager.persistMetadata(hm.ctx, pluginID)
	}

	// Publish health transition events
	if oldHealth != health {
		hm.publishHealthEvent(pluginID, health)
	}
}

// updateLastHeartbeat updates the last heartbeat time
func (hm *HealthMonitor) updateLastHeartbeat(pluginID string) {
	meta := hm.manager.GetMetadata(pluginID)
	if meta == nil {
		return
	}

	now := time.Now()
	meta.LastHeartbeat = &now

	// Persist only if significant time has passed to avoid excessive writes
	if meta.LastHeartbeat != nil && time.Since(*meta.LastHeartbeat) > 5*time.Minute {
		if hm.manager.store != nil {
			_ = hm.manager.persistMetadata(hm.ctx, pluginID)
		}
	}
}

// publishHealthEvent publishes a health transition event
func (hm *HealthMonitor) publishHealthEvent(pluginID string, health PluginHealth) {
	if hm.manager.eventBus == nil {
		return
	}

	var eventType string
	switch health {
	case HealthHealthy:
		eventType = "plugin.recovered"
	case HealthDegraded:
		eventType = "plugin.degraded"
	case HealthFailed:
		eventType = "plugin.failed"
	case HealthDisabled:
		eventType = "plugin.disabled"
	default:
		return
	}

	hm.manager.eventBus.Publish(Event{
		Type:      eventType,
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"plugin_id": pluginID,
			"health":    string(health),
		},
	})
}
