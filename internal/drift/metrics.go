package drift

import (
	"sync"
	"time"
)

// Metrics tracks drift detection metrics
type Metrics struct {
	mu                 sync.RWMutex
	totalFindings      int
	criticalFindings   int
	openFindings       int
	scans              int
	totalDuration      int64
	lastScan           *time.Time
	findingsByType     map[DriftType]int
	findingsBySeverity map[DriftSeverity]int
}

// NewMetrics creates a new metrics tracker
func NewMetrics() *Metrics {
	return &Metrics{
		findingsByType:     make(map[DriftType]int),
		findingsBySeverity: make(map[DriftSeverity]int),
	}
}

// RecordFinding records a detected finding
func (m *Metrics) RecordFinding(finding DriftFinding) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalFindings++
	m.openFindings++

	if finding.Severity == SeverityCritical {
		m.criticalFindings++
	}

	m.findingsByType[finding.Type]++
	m.findingsBySeverity[finding.Severity]++
}

// RecordScan records a scan execution
func (m *Metrics) RecordScan(duration time.Duration, success bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.scans++
	m.totalDuration += duration.Milliseconds()

	now := time.Now()
	m.lastScan = &now
}

// RecordResolution records a resolved finding
func (m *Metrics) RecordResolution() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.openFindings > 0 {
		m.openFindings--
	}
}

// RecordAcknowledgment records an acknowledged finding
func (m *Metrics) RecordAcknowledgment() {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Don't decrement open findings, just track state change
}

// GetMetrics returns current metrics
func (m *Metrics) GetMetrics() *DriftMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	avgDuration := 0.0
	if m.scans > 0 {
		avgDuration = float64(m.totalDuration) / float64(m.scans)
	}

	return &DriftMetrics{
		TotalFindings:      m.totalFindings,
		CriticalFindings:   m.criticalFindings,
		OpenFindings:       m.openFindings,
		Scans:              m.scans,
		AverageDuration:    avgDuration,
		LastScan:           m.lastScan,
		FindingsByType:     m.copyFindingsByType(),
		FindingsBySeverity: m.copyFindingsBySeverity(),
	}
}

// copyFindingsByType returns a copy of findings by type
func (m *Metrics) copyFindingsByType() map[DriftType]int {
	result := make(map[DriftType]int)
	for k, v := range m.findingsByType {
		result[k] = v
	}
	return result
}

// copyFindingsBySeverity returns a copy of findings by severity
func (m *Metrics) copyFindingsBySeverity() map[DriftSeverity]int {
	result := make(map[DriftSeverity]int)
	for k, v := range m.findingsBySeverity {
		result[k] = v
	}
	return result
}
