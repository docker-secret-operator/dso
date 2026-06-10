package policy

import (
	"sync"
	"time"
)

// Metrics tracks policy/rule metrics
type Metrics struct {
	mu                     sync.RWMutex
	totalRules             int
	enabledRules           int
	executions             int
	failures               int
	totalDuration          int64
	lastExecution          *time.Time
	executionsByType       map[string]int
	failuresByType         map[string]int
	executionsByRuleID     map[string]int
	failuresByRuleID       map[string]int
	lastExecutionByRuleID  map[string]*time.Time
}

// NewMetrics creates a new metrics tracker
func NewMetrics() *Metrics {
	return &Metrics{
		executionsByType:      make(map[string]int),
		failuresByType:        make(map[string]int),
		executionsByRuleID:    make(map[string]int),
		failuresByRuleID:      make(map[string]int),
		lastExecutionByRuleID: make(map[string]*time.Time),
	}
}

// RecordExecution records a rule execution
func (m *Metrics) RecordExecution(success bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.executions++
	if !success {
		m.failures++
	}

	now := time.Now()
	m.lastExecution = &now
}

// RecordRuleRegistered records a rule registration
func (m *Metrics) RecordRuleRegistered() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalRules++
}

// RecordRuleEnabled records a rule enable
func (m *Metrics) RecordRuleEnabled() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabledRules++
}

// RecordRuleDisabled records a rule disable
func (m *Metrics) RecordRuleDisabled() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.enabledRules > 0 {
		m.enabledRules--
	}
}

// GetMetrics returns metrics
func (m *Metrics) GetMetrics(rules []*Rule) *RuleMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	enabledCount := 0
	for _, rule := range rules {
		if rule.Enabled {
			enabledCount++
		}
	}

	avgDuration := 0.0
	if m.executions > 0 {
		avgDuration = float64(m.totalDuration) / float64(m.executions)
	}

	return &RuleMetrics{
		TotalRules:       len(rules),
		EnabledRules:     enabledCount,
		Executions:       m.executions,
		Failures:         m.failures,
		AverageDuration:  avgDuration,
		LastExecution:    m.lastExecution,
		ExecutionsByType: m.copyExecutionsByType(),
		FailuresByType:   m.copyFailuresByType(),
	}
}

// copyExecutionsByType returns a copy of executions by type
func (m *Metrics) copyExecutionsByType() map[string]int {
	result := make(map[string]int)
	for k, v := range m.executionsByType {
		result[k] = v
	}
	return result
}

// copyFailuresByType returns a copy of failures by type
func (m *Metrics) copyFailuresByType() map[string]int {
	result := make(map[string]int)
	for k, v := range m.failuresByType {
		result[k] = v
	}
	return result
}
