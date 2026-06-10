package autonomy

import (
	"sync"
	"time"
)

// Metrics tracks autonomy metrics
type Metrics struct {
	mu                sync.RWMutex
	totalActions      int
	successfulActions int
	failedActions     int
	rollbackCount     int
	automaticActions  int
	manualActions     int
	lastUpdate        time.Time
}

// NewMetrics creates a new metrics tracker
func NewMetrics() *Metrics {
	return &Metrics{
		lastUpdate: time.Now(),
	}
}

// RecordAction records a new action
func (m *Metrics) RecordAction(safetyLevel SafetyLevel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalActions++
	if safetyLevel == SafetyAutomatic {
		m.automaticActions++
	} else {
		m.manualActions++
	}
	m.lastUpdate = time.Now()
}

// RecordSuccess records a successful action
func (m *Metrics) RecordSuccess() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.successfulActions++
	m.lastUpdate = time.Now()
}

// RecordFailure records a failed action
func (m *Metrics) RecordFailure() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failedActions++
	m.lastUpdate = time.Now()
}

// RecordRollback records a rollback
func (m *Metrics) RecordRollback() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rollbackCount++
	m.lastUpdate = time.Now()
}

// GetMetrics returns current metrics
func (m *Metrics) GetMetrics() *ActionMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	successRate := 0.0
	if m.totalActions > 0 {
		successRate = float64(m.successfulActions) / float64(m.totalActions)
	}

	return &ActionMetrics{
		TotalActions:      m.totalActions,
		SuccessfulActions: m.successfulActions,
		FailedActions:     m.failedActions,
		RollbackCount:     m.rollbackCount,
		AutomaticActions:  m.automaticActions,
		ManualActions:     m.manualActions,
		SuccessRate:       successRate,
		LastUpdate:        m.lastUpdate,
	}
}

// Reset resets all metrics
func (m *Metrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalActions = 0
	m.successfulActions = 0
	m.failedActions = 0
	m.rollbackCount = 0
	m.automaticActions = 0
	m.manualActions = 0
	m.lastUpdate = time.Now()
}
