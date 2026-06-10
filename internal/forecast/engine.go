package forecast

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Engine implements the forecasting engine
type Engine struct {
	mu              sync.RWMutex
	logger          *zap.Logger
	store           Store
	model           *StatisticalModel
	metrics         *Metrics
	eventBus        interface{}
	active          bool
	stopChan        chan struct{}
	forecasts       map[string]*Forecast
	cleanupTicker   *time.Ticker
	dataSources     map[ForecastResource]DataSourceFunc
}

// DataSourceFunc defines a function that provides data for forecasting
type DataSourceFunc func() []DataPoint

// NewEngine creates a new forecasting engine
func NewEngine(logger *zap.Logger, store Store) *Engine {
	if logger == nil {
		logger = zap.NewNop()
	}

	if store == nil {
		store = NewInMemoryStore()
	}

	return &Engine{
		logger:        logger,
		store:         store,
		model:         NewStatisticalModel(),
		metrics:       NewMetrics(),
		stopChan:      make(chan struct{}),
		forecasts:     make(map[string]*Forecast),
		dataSources:   make(map[ForecastResource]DataSourceFunc),
	}
}

// Initialize initializes the forecasting engine
func (e *Engine) Initialize() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.active {
		return fmt.Errorf("engine already initialized")
	}

	e.active = true
	e.logger.Info("Forecast engine initialized")

	// Start background workers
	go e.generationLoop()
	go e.cleanupLoop()

	return nil
}

// Shutdown gracefully shuts down the forecasting engine
func (e *Engine) Shutdown() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.active {
		return nil
	}

	e.active = false
	close(e.stopChan)

	// Stop cleanup ticker
	if e.cleanupTicker != nil {
		e.cleanupTicker.Stop()
	}

	e.logger.Info("Forecast engine shutdown complete")
	return nil
}

// SetEventBus sets the event bus for publishing events
func (e *Engine) SetEventBus(eventBus interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.eventBus = eventBus
}

// RegisterDataSource registers a data source for a resource type
func (e *Engine) RegisterDataSource(resourceType ForecastResource, fn DataSourceFunc) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.dataSources[resourceType] = fn
}

// GenerateForecasts generates all forecasts
func (e *Engine) GenerateForecasts() error {
	e.mu.RLock()
	if !e.active {
		e.mu.RUnlock()
		return fmt.Errorf("engine not active")
	}
	e.mu.RUnlock()

	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("Panic generating forecasts", zap.Any("recover", r))
		}
	}()

	var wg sync.WaitGroup

	// Generate forecasts for each resource type
	resourceTypes := []ForecastResource{
		ResourceQueue,
		ResourceMemory,
		ResourceBackupStorage,
		ResourceIncident,
		ResourceExecution,
		ResourceAlert,
		ResourcePlugin,
		ResourceIntegration,
		ResourceScheduler,
	}

	for _, resourceType := range resourceTypes {
		wg.Add(1)
		go func(rt ForecastResource) {
			defer wg.Done()
			e.GenerateForecast(rt)
		}(resourceType)
	}

	wg.Wait()

	e.metrics.RecordForecastRun()

	return nil
}

// GenerateForecast generates a forecast for a specific resource type
func (e *Engine) GenerateForecast(resourceType ForecastResource) error {
	e.mu.RLock()
	dataSource := e.dataSources[resourceType]
	e.mu.RUnlock()

	if dataSource == nil {
		// No data source registered, use empty data
		dataSource = func() []DataPoint { return []DataPoint{} }
	}

	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("Panic generating forecast", zap.String("resource", string(resourceType)), zap.Any("recover", r))
		}
	}()

	data := dataSource()

	if len(data) == 0 {
		e.logger.Debug("No data available for forecast", zap.String("resource", string(resourceType)))
		return nil
	}

	// Generate forecasts for different horizons
	horizons := []ForecastHorizon{
		HorizonOneHour,
		HorizonSixHours,
		HorizonOneDay,
		HorizonSevenDays,
	}

	for _, horizon := range horizons {
		forecast := e.buildForecast(resourceType, data, horizon)
		if forecast != nil {
			e.mu.Lock()
			e.forecasts[forecast.ID] = forecast
			e.mu.Unlock()

			if err := e.store.SaveForecast(forecast); err != nil {
				e.logger.Error("Failed to save forecast", zap.Error(err))
			}

			e.metrics.RecordForecast(forecast.Severity, forecast.Confidence)

			// Publish critical forecasts
			if forecast.IsRisky() {
				e.publishEvent("CriticalForecastDetected", map[string]interface{}{
					"forecast_id":  forecast.ID,
					"resource":     forecast.ResourceType,
					"severity":     forecast.Severity,
					"confidence":   forecast.Confidence,
				})
			}
		}
	}

	return nil
}

// ListForecasts lists all forecasts
func (e *Engine) ListForecasts(limit int) ([]*Forecast, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.store.ListForecasts(limit)
}

// GetForecast retrieves a specific forecast
func (e *Engine) GetForecast(id string) (*Forecast, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.store.GetForecast(id)
}

// DeleteForecast deletes a forecast
func (e *Engine) DeleteForecast(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.store.DeleteForecast(id)
}

// GetMetrics returns engine metrics
func (e *Engine) GetMetrics() *ForecastMetrics {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.metrics.GetMetrics()
}

// buildForecast builds a forecast from data
func (e *Engine) buildForecast(resourceType ForecastResource, data []DataPoint, horizon ForecastHorizon) *Forecast {
	if len(data) == 0 {
		return nil
	}

	// Calculate steps ahead based on horizon (assuming data points are hourly)
	stepsAhead := int(horizon.Duration().Hours())
	if stepsAhead < 1 {
		stepsAhead = 1
	}

	currentValue := data[len(data)-1].Value
	predictedValue, confidence := e.model.ForecastWithConfidence(data, stepsAhead)
	growthRate := e.model.CalculateGrowthRateFromData(data)
	trend := e.model.DetectTrend(data)

	// Determine severity based on predicted value and growth
	severity := e.determineSeverity(resourceType, predictedValue, growthRate, confidence)

	forecast := &Forecast{
		ID:             uuid.New().String(),
		ResourceType:   resourceType,
		ResourceID:     fmt.Sprintf("%s-0", resourceType),
		Metric:         fmt.Sprintf("%s_metric", resourceType),
		CurrentValue:   currentValue,
		PredictedValue: predictedValue,
		GrowthRate:     growthRate,
		Confidence:     confidence,
		Horizon:        horizon,
		Severity:       severity,
		Trend:          trend,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		Metadata:       make(map[string]string),
	}

	return forecast
}

// determineSeverity determines the severity of a forecast
func (e *Engine) determineSeverity(resourceType ForecastResource, predictedValue, growthRate, confidence float64) ForecastSeverity {
	// High confidence + high growth = higher severity
	if confidence > 0.8 && growthRate > 0.2 {
		return SeverityCritical
	}

	if confidence > 0.7 && growthRate > 0.1 {
		return SeverityHigh
	}

	// Resource-specific thresholds
	switch resourceType {
	case ResourceMemory:
		if predictedValue > 0.9 {
			return SeverityCritical
		}
		if predictedValue > 0.75 {
			return SeverityHigh
		}
	case ResourceBackupStorage:
		if predictedValue > 0.95 {
			return SeverityCritical
		}
		if predictedValue > 0.85 {
			return SeverityHigh
		}
	case ResourceQueue:
		if predictedValue > 1000 {
			return SeverityCritical
		}
		if predictedValue > 500 {
			return SeverityHigh
		}
	case ResourceIncident:
		if growthRate > 0.5 {
			return SeverityHigh
		}
	case ResourceAlert:
		if growthRate > 0.3 {
			return SeverityHigh
		}
	}

	if growthRate > 0.05 {
		return SeverityMedium
	}

	if growthRate > 0 {
		return SeverityLow
	}

	return SeverityInfo
}

// generationLoop periodically generates new forecasts
func (e *Engine) generationLoop() {
	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("Panic in generation loop", zap.Any("recover", r))
		}
	}()

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopChan:
			return
		case <-ticker.C:
			e.GenerateForecasts()
		}
	}
}

// cleanupLoop periodically cleans up old forecasts
func (e *Engine) cleanupLoop() {
	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("Panic in cleanup loop", zap.Any("recover", r))
		}
	}()

	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopChan:
			return
		case <-ticker.C:
			e.cleanup()
		}
	}
}

// cleanup removes old forecasts
func (e *Engine) cleanup() {
	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("Panic during cleanup", zap.Any("recover", r))
		}
	}()

	e.mu.Lock()
	defer e.mu.Unlock()

	cutoff := time.Now().Add(-30 * 24 * time.Hour) // 30 day retention

	if err := e.store.CleanupOldForecasts(cutoff); err != nil {
		e.logger.Error("Failed to cleanup old forecasts", zap.Error(err))
	}
}

// publishEvent publishes a forecast event
func (e *Engine) publishEvent(eventType string, data map[string]interface{}) {
	if e.eventBus == nil {
		return
	}

	if bus, ok := e.eventBus.(interface{ Publish(string, map[string]interface{}) }); ok {
		bus.Publish(eventType, data)
	}
}
