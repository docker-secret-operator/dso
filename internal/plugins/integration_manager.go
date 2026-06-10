package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
	"go.uber.org/zap"
)

// IntegrationManager orchestrates event delivery to external systems
type IntegrationManager struct {
	plugins        map[string]interface{} // Integration plugins
	queue          *IntegrationQueue
	store          storage.IntegrationConfigStore
	deliveryStore  storage.IntegrationDeliveryStore
	eventBus       *EventBus
	logger         *zap.Logger
	metrics        map[string]*IntegrationMetrics
	mu             sync.RWMutex
	ctx            context.Context
	cancel         context.CancelFunc
	done           chan struct{}
}

// NewIntegrationManager creates a new integration manager
func NewIntegrationManager(
	queue *IntegrationQueue,
	configStore storage.IntegrationConfigStore,
	deliveryStore storage.IntegrationDeliveryStore,
	eventBus *EventBus,
	logger *zap.Logger,
) *IntegrationManager {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &IntegrationManager{
		plugins:       make(map[string]interface{}),
		queue:         queue,
		store:         configStore,
		deliveryStore: deliveryStore,
		eventBus:      eventBus,
		logger:        logger,
		metrics:       make(map[string]*IntegrationMetrics),
		done:          make(chan struct{}),
	}
}

// Initialize starts the integration manager
func (im *IntegrationManager) Initialize(ctx context.Context) error {
	im.ctx, im.cancel = context.WithCancel(ctx)

	// Start queue
	if err := im.queue.Start(im.ctx); err != nil {
		return fmt.Errorf("failed to start queue: %w", err)
	}

	// Load configurations
	configs, err := im.store.ListConfigs(im.ctx)
	if err != nil {
		im.logger.Error("failed to load integration configs", zap.Error(err))
	} else {
		for _, config := range configs {
			im.initializeIntegration(config)
		}
	}

	// Subscribe to EventBus
	im.subscribeToEvents()

	// Start metrics processor
	go im.metricsLoop()

	im.logger.Info("Integration manager initialized")
	return nil
}

// Shutdown gracefully stops the manager
func (im *IntegrationManager) Shutdown(ctx context.Context) error {
	if im.cancel != nil {
		im.cancel()
	}

	if im.queue != nil {
		im.queue.Stop()
	}

	select {
	case <-im.done:
	case <-time.After(5 * time.Second):
		im.logger.Warn("integration manager shutdown timeout")
	}

	return nil
}

// Register registers an integration plugin
func (im *IntegrationManager) Register(pluginID string, plugin interface{}) error {
	im.mu.Lock()
	defer im.mu.Unlock()

	if _, exists := im.plugins[pluginID]; exists {
		return fmt.Errorf("plugin already registered: %s", pluginID)
	}

	im.plugins[pluginID] = plugin

	// Initialize metrics
	im.metrics[pluginID] = &IntegrationMetrics{
		PluginID: pluginID,
	}

	im.logger.Info("Registered integration plugin", zap.String("plugin_id", pluginID))
	return nil
}

// Deliver pushes an event to the integration queue
func (im *IntegrationManager) Deliver(event Event) error {
	configs, err := im.store.ListConfigs(im.ctx)
	if err != nil {
		return fmt.Errorf("failed to load configs: %w", err)
	}

	for _, config := range configs {
		if !config.Enabled {
			continue
		}

		// Check filters
		if !im.matchesFilter(event, config) {
			continue
		}

		// Enqueue
		item := &DeliveryQueueItem{
			ID:            fmt.Sprintf("%s-%s-%d", config.PluginID, event.Type, time.Now().UnixNano()),
			IntegrationID: config.PluginID,
			Event:         event,
			Attempt:       1,
			NextRetryTime: time.Now(),
			CreatedAt:     time.Now(),
		}

		im.queue.Enqueue(item)

		// Update metrics
		im.updateMetrics(config.PluginID, false, 0, nil)
	}

	return nil
}

// matchesFilter checks if event matches integration filter
func (im *IntegrationManager) matchesFilter(event Event, config *storage.IntegrationConfig) bool {
	if config.FiltersJSON == "" {
		return true // No filter means all events
	}

	var filter EventFilter
	if err := json.Unmarshal([]byte(config.FiltersJSON), &filter); err != nil {
		return false
	}

	// If event type list is empty, match all
	if len(filter.EventTypes) == 0 {
		return true
	}

	// Check if event type is in filter
	for _, et := range filter.EventTypes {
		if et == event.Type {
			return true
		}
	}

	return false
}

// TestDelivery sends a test event
func (im *IntegrationManager) TestDelivery(pluginID string) error {
	config, err := im.store.GetConfig(im.ctx, pluginID)
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}

	if config == nil {
		return fmt.Errorf("integration not configured: %s", pluginID)
	}

	testEvent := Event{
		Type:      "integration.test",
		Timestamp: time.Now(),
		Payload: map[string]interface{}{
			"test": true,
			"message": "Test delivery from DSO",
		},
	}

	// Try to deliver synchronously for test
	plugin := im.plugins[pluginID]
	if plugin == nil {
		return fmt.Errorf("plugin not found: %s", pluginID)
	}

	// Type assert and deliver based on plugin type
	switch p := plugin.(type) {
	case interface{ DeliverEvent(context.Context, Event) error }:
		return p.DeliverEvent(im.ctx, testEvent)
	default:
		return fmt.Errorf("plugin does not support event delivery: %s", pluginID)
	}
}

// GetMetrics returns metrics for a plugin
func (im *IntegrationManager) GetMetrics(pluginID string) *IntegrationMetrics {
	im.mu.RLock()
	defer im.mu.RUnlock()
	return im.metrics[pluginID]
}

// GetAllMetrics returns metrics for all plugins
func (im *IntegrationManager) GetAllMetrics() []*IntegrationMetrics {
	im.mu.RLock()
	defer im.mu.RUnlock()

	metrics := make([]*IntegrationMetrics, 0, len(im.metrics))
	for _, m := range im.metrics {
		metrics = append(metrics, m)
	}
	return metrics
}

// updateMetrics updates metrics for a plugin
func (im *IntegrationManager) updateMetrics(pluginID string, success bool, responseCode int, err error) {
	im.mu.Lock()
	defer im.mu.Unlock()

	metrics, exists := im.metrics[pluginID]
	if !exists {
		return
	}

	metrics.TotalEvents++

	if success {
		metrics.SuccessfulCount++
		now := time.Now()
		metrics.LastSuccessTime = &now
	} else {
		metrics.FailedCount++
		now := time.Now()
		metrics.LastErrorTime = &now
		if err != nil {
			errMsg := err.Error()
			metrics.LastError = &errMsg
		}
	}
}

// subscribeToEvents subscribes to EventBus events
func (im *IntegrationManager) subscribeToEvents() {
	if im.eventBus == nil {
		return
	}

	// Subscribe to all relevant event types
	eventTypes := []string{
		AlertTriggered, AlertResolved,
		ExecutionStarted, ExecutionCompleted, ExecutionFailed,
		BackupCreated, BackupRestored,
		LoginFailure, BruteForceDetected,
		PluginFailed, PluginRecovered,
	}

	for _, eventType := range eventTypes {
		im.eventBus.Subscribe(eventType, SubscriberFunc(func(event Event) {
			// Push to queue
			_ = im.Deliver(event)
		}))
	}

	im.logger.Info("Subscribed to EventBus events")
}

// metricsLoop periodically updates metrics
func (im *IntegrationManager) metricsLoop() {
	defer close(im.done)

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-im.ctx.Done():
			return
		case <-ticker.C:
			// Cleanup old deliveries (30 day retention)
			cutoff := time.Now().AddDate(0, 0, -30)
			if err := im.deliveryStore.CleanupOldDeliveries(im.ctx, cutoff); err != nil {
				im.logger.Error("failed to cleanup old deliveries", zap.Error(err))
			}
		}
	}
}

// initializeIntegration initializes an integration from config
func (im *IntegrationManager) initializeIntegration(config *storage.IntegrationConfig) {
	// Set config on plugin if it supports it
	plugin := im.plugins[config.PluginID]
	if plugin == nil {
		im.logger.Warn("plugin not registered", zap.String("plugin_id", config.PluginID))
		return
	}

	if p, ok := plugin.(interface{ SetConfig(*storage.IntegrationConfig) }); ok {
		p.SetConfig(config)
	}

	im.logger.Info("Initialized integration",
		zap.String("plugin_id", config.PluginID),
		zap.Bool("enabled", config.Enabled))
}

// GetQueueStats returns queue statistics
func (im *IntegrationManager) GetQueueStats() map[string]interface{} {
	return im.queue.GetQueueStats()
}
