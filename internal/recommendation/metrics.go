package recommendation

import (
	"sync"
	"time"
)

// Metrics tracks recommendation engine metrics
type Metrics struct {
	mu                           sync.RWMutex
	totalRecommendations         int
	openRecommendations          int
	acknowledgedRecommendations  int
	implementedRecommendations   int
	dismissedRecommendations     int
	totalConfidence              float64
	recommendationCount          int
	lastUpdate                   time.Time
}

// NewMetrics creates a new metrics tracker
func NewMetrics() *Metrics {
	return &Metrics{
		lastUpdate: time.Now(),
	}
}

// RecordRecommendation records a new recommendation
func (m *Metrics) RecordRecommendation(confidence float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalRecommendations++
	m.openRecommendations++
	m.totalConfidence += confidence
	m.recommendationCount++
	m.lastUpdate = time.Now()
}

// RecordAcknowledged records an acknowledged recommendation
func (m *Metrics) RecordAcknowledged() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.openRecommendations > 0 {
		m.openRecommendations--
	}
	m.acknowledgedRecommendations++
	m.lastUpdate = time.Now()
}

// RecordImplemented records an implemented recommendation
func (m *Metrics) RecordImplemented() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.openRecommendations > 0 {
		m.openRecommendations--
	}
	if m.acknowledgedRecommendations > 0 {
		m.acknowledgedRecommendations--
	}
	m.implementedRecommendations++
	m.lastUpdate = time.Now()
}

// RecordDismissed records a dismissed recommendation
func (m *Metrics) RecordDismissed() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.openRecommendations > 0 {
		m.openRecommendations--
	}
	if m.acknowledgedRecommendations > 0 {
		m.acknowledgedRecommendations--
	}
	m.dismissedRecommendations++
	m.lastUpdate = time.Now()
}

// GetMetrics returns current metrics
func (m *Metrics) GetMetrics() *RecommendationMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	avgConfidence := 0.0
	if m.recommendationCount > 0 {
		avgConfidence = m.totalConfidence / float64(m.recommendationCount)
	}

	return &RecommendationMetrics{
		TotalRecommendations:       m.totalRecommendations,
		OpenRecommendations:        m.openRecommendations,
		AcknowledgedRecommendations: m.acknowledgedRecommendations,
		ImplementedRecommendations: m.implementedRecommendations,
		DismissedRecommendations:   m.dismissedRecommendations,
		AverageConfidence:          avgConfidence,
		LastUpdate:                 m.lastUpdate,
	}
}

// Reset resets all metrics
func (m *Metrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalRecommendations = 0
	m.openRecommendations = 0
	m.acknowledgedRecommendations = 0
	m.implementedRecommendations = 0
	m.dismissedRecommendations = 0
	m.totalConfidence = 0
	m.recommendationCount = 0
	m.lastUpdate = time.Now()
}
