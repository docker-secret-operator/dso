package forecast

import (
	"sync"
	"time"
)

// Metrics tracks forecast engine metrics
type Metrics struct {
	mu                   sync.RWMutex
	totalForecasts       int
	criticalForecasts    int
	totalConfidence      float64
	forecastCount        int
	predictionAccuracy   float64
	accuracyCount        int
	forecastRuns         int
	lastUpdate           time.Time
}

// NewMetrics creates a new metrics tracker
func NewMetrics() *Metrics {
	return &Metrics{
		lastUpdate: time.Now(),
	}
}

// RecordForecast records a new forecast
func (m *Metrics) RecordForecast(severity ForecastSeverity, confidence float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalForecasts++
	m.forecastCount++
	m.totalConfidence += confidence

	if severity == SeverityCritical || severity == SeverityHigh {
		m.criticalForecasts++
	}

	m.lastUpdate = time.Now()
}

// RecordForecastRun records a forecast generation run
func (m *Metrics) RecordForecastRun() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.forecastRuns++
	m.lastUpdate = time.Now()
}

// RecordPredictionAccuracy records prediction accuracy
func (m *Metrics) RecordPredictionAccuracy(accuracy float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.predictionAccuracy += accuracy
	m.accuracyCount++
	m.lastUpdate = time.Now()
}

// GetMetrics returns current metrics
func (m *Metrics) GetMetrics() *ForecastMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	avgConfidence := 0.0
	if m.forecastCount > 0 {
		avgConfidence = m.totalConfidence / float64(m.forecastCount)
	}

	avgAccuracy := 0.0
	if m.accuracyCount > 0 {
		avgAccuracy = m.predictionAccuracy / float64(m.accuracyCount)
	}

	return &ForecastMetrics{
		TotalForecasts:     m.totalForecasts,
		CriticalForecasts:  m.criticalForecasts,
		AverageConfidence:  avgConfidence,
		PredictionAccuracy: avgAccuracy,
		ForecastRuns:       m.forecastRuns,
		LastUpdate:         m.lastUpdate,
	}
}

// Reset resets all metrics
func (m *Metrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalForecasts = 0
	m.criticalForecasts = 0
	m.totalConfidence = 0
	m.forecastCount = 0
	m.predictionAccuracy = 0
	m.accuracyCount = 0
	m.forecastRuns = 0
	m.lastUpdate = time.Now()
}
