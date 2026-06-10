package correlation

import (
	"sync"
	"time"
)

// Metrics tracks correlation engine metrics
type Metrics struct {
	mu                    sync.RWMutex
	totalIncidents        int
	openIncidents         int
	resolvedIncidents     int
	acknowledgedIncidents int
	totalScore            float64
	incidentCount         int
	eventsProcessed       int
	mergesPerformed       int
	lastUpdate            time.Time
}

// NewMetrics creates a new metrics tracker
func NewMetrics() *Metrics {
	return &Metrics{
		lastUpdate: time.Now(),
	}
}

// RecordIncident records a new incident
func (m *Metrics) RecordIncident(severity Severity) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalIncidents++
	m.openIncidents++
	m.incidentCount++
	m.lastUpdate = time.Now()
}

// RecordResolved records a resolved incident
func (m *Metrics) RecordResolved() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.openIncidents > 0 {
		m.openIncidents--
	}
	m.resolvedIncidents++
	m.lastUpdate = time.Now()
}

// RecordAcknowledged records an acknowledged incident
func (m *Metrics) RecordAcknowledged() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.acknowledgedIncidents++
	m.lastUpdate = time.Now()
}

// RecordEventProcessed records a processed event
func (m *Metrics) RecordEventProcessed() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventsProcessed++
	m.lastUpdate = time.Now()
}

// RecordMerge records an incident merge
func (m *Metrics) RecordMerge() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mergesPerformed++
	m.lastUpdate = time.Now()
}

// RecordCorrelationScore records a correlation score
func (m *Metrics) RecordCorrelationScore(score float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalScore += score
	m.lastUpdate = time.Now()
}

// GetMetrics returns current metrics
func (m *Metrics) GetMetrics() *IncidentMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	avgScore := 0.0
	if m.incidentCount > 0 {
		avgScore = m.totalScore / float64(m.incidentCount)
	}

	return &IncidentMetrics{
		TotalIncidents:        m.totalIncidents,
		OpenIncidents:         m.openIncidents,
		ResolvedIncidents:     m.resolvedIncidents,
		AcknowledgedIncidents: m.acknowledgedIncidents,
		AverageScore:          avgScore,
		EventsProcessed:       m.eventsProcessed,
		MergesPerformed:       m.mergesPerformed,
		LastUpdate:            m.lastUpdate,
	}
}

// Reset resets all metrics
func (m *Metrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalIncidents = 0
	m.openIncidents = 0
	m.resolvedIncidents = 0
	m.acknowledgedIncidents = 0
	m.totalScore = 0
	m.incidentCount = 0
	m.eventsProcessed = 0
	m.mergesPerformed = 0
	m.lastUpdate = time.Now()
}
