package policy

import (
	"fmt"
	"sync"
)

// Evaluator evaluates rule conditions
type Evaluator struct {
	evaluators map[string]ConditionEvaluator
	mu         sync.RWMutex
}

// NewEvaluator creates a new evaluator
func NewEvaluator() *Evaluator {
	return &Evaluator{
		evaluators: make(map[string]ConditionEvaluator),
	}
}

// RegisterEvaluator registers a condition evaluator
func (e *Evaluator) RegisterEvaluator(conditionType string, evaluator ConditionEvaluator) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.evaluators[conditionType] = evaluator
}

// Evaluate evaluates a condition
func (e *Evaluator) Evaluate(condition RuleCondition) (bool, error) {
	e.mu.RLock()
	evaluator, exists := e.evaluators[condition.Type]
	e.mu.RUnlock()

	if !exists {
		return false, fmt.Errorf("unknown condition type: %s", condition.Type)
	}

	return evaluator.Evaluate(condition)
}

// QueueDepthEvaluator evaluates queue depth conditions
type QueueDepthEvaluator struct {
	getQueueDepth func() int
}

// NewQueueDepthEvaluator creates a new queue depth evaluator
func NewQueueDepthEvaluator(getQueueDepth func() int) *QueueDepthEvaluator {
	return &QueueDepthEvaluator{
		getQueueDepth: getQueueDepth,
	}
}

// Evaluate evaluates queue depth
func (q *QueueDepthEvaluator) Evaluate(condition RuleCondition) (bool, error) {
	threshold, ok := condition.Params["threshold"].(float64)
	if !ok {
		return false, fmt.Errorf("invalid threshold parameter")
	}

	depth := q.getQueueDepth()
	return float64(depth) > threshold, nil
}

// WorkerUtilizationEvaluator evaluates worker utilization conditions
type WorkerUtilizationEvaluator struct {
	getUtilization func() float64
}

// NewWorkerUtilizationEvaluator creates a new worker utilization evaluator
func NewWorkerUtilizationEvaluator(getUtilization func() float64) *WorkerUtilizationEvaluator {
	return &WorkerUtilizationEvaluator{
		getUtilization: getUtilization,
	}
}

// Evaluate evaluates worker utilization
func (w *WorkerUtilizationEvaluator) Evaluate(condition RuleCondition) (bool, error) {
	threshold, ok := condition.Params["threshold"].(float64)
	if !ok {
		return false, fmt.Errorf("invalid threshold parameter")
	}

	utilization := w.getUtilization()
	return utilization > threshold, nil
}

// FailureRateEvaluator evaluates failure rate conditions
type FailureRateEvaluator struct {
	getFailureRate func() float64
}

// NewFailureRateEvaluator creates a new failure rate evaluator
func NewFailureRateEvaluator(getFailureRate func() float64) *FailureRateEvaluator {
	return &FailureRateEvaluator{
		getFailureRate: getFailureRate,
	}
}

// Evaluate evaluates failure rate
func (f *FailureRateEvaluator) Evaluate(condition RuleCondition) (bool, error) {
	threshold, ok := condition.Params["threshold"].(float64)
	if !ok {
		return false, fmt.Errorf("invalid threshold parameter")
	}

	failureRate := f.getFailureRate()
	return failureRate > threshold, nil
}

// MemoryUsageEvaluator evaluates memory usage conditions
type MemoryUsageEvaluator struct {
	getMemoryUsage func() float64
}

// NewMemoryUsageEvaluator creates a new memory usage evaluator
func NewMemoryUsageEvaluator(getMemoryUsage func() float64) *MemoryUsageEvaluator {
	return &MemoryUsageEvaluator{
		getMemoryUsage: getMemoryUsage,
	}
}

// Evaluate evaluates memory usage
func (m *MemoryUsageEvaluator) Evaluate(condition RuleCondition) (bool, error) {
	threshold, ok := condition.Params["threshold"].(float64)
	if !ok {
		return false, fmt.Errorf("invalid threshold parameter")
	}

	memoryUsage := m.getMemoryUsage()
	return memoryUsage > threshold, nil
}

// LoginFailureEvaluator evaluates login failure conditions
type LoginFailureEvaluator struct {
	getFailureCount func(minutes int) int
}

// NewLoginFailureEvaluator creates a new login failure evaluator
func NewLoginFailureEvaluator(getFailureCount func(minutes int) int) *LoginFailureEvaluator {
	return &LoginFailureEvaluator{
		getFailureCount: getFailureCount,
	}
}

// Evaluate evaluates login failure count
func (l *LoginFailureEvaluator) Evaluate(condition RuleCondition) (bool, error) {
	threshold, ok := condition.Params["threshold"].(float64)
	if !ok {
		return false, fmt.Errorf("invalid threshold parameter")
	}

	minutes := 5
	if m, ok := condition.Params["minutes"].(float64); ok {
		minutes = int(m)
	}

	failureCount := l.getFailureCount(minutes)
	return float64(failureCount) > threshold, nil
}

// PluginHealthEvaluator evaluates plugin health conditions
type PluginHealthEvaluator struct {
	getPluginStatus func(string) string
}

// NewPluginHealthEvaluator creates a new plugin health evaluator
func NewPluginHealthEvaluator(getPluginStatus func(string) string) *PluginHealthEvaluator {
	return &PluginHealthEvaluator{
		getPluginStatus: getPluginStatus,
	}
}

// Evaluate evaluates plugin health
func (p *PluginHealthEvaluator) Evaluate(condition RuleCondition) (bool, error) {
	pluginID, ok := condition.Params["plugin_id"].(string)
	if !ok {
		return false, fmt.Errorf("invalid plugin_id parameter")
	}

	expectedStatus, ok := condition.Params["status"].(string)
	if !ok {
		return false, fmt.Errorf("invalid status parameter")
	}

	status := p.getPluginStatus(pluginID)
	return status != expectedStatus, nil
}

// BackupAgeEvaluator evaluates backup age conditions
type BackupAgeEvaluator struct {
	getBackupAge func() int // days
}

// NewBackupAgeEvaluator creates a new backup age evaluator
func NewBackupAgeEvaluator(getBackupAge func() int) *BackupAgeEvaluator {
	return &BackupAgeEvaluator{
		getBackupAge: getBackupAge,
	}
}

// Evaluate evaluates backup age
func (b *BackupAgeEvaluator) Evaluate(condition RuleCondition) (bool, error) {
	maxDays, ok := condition.Params["max_days"].(float64)
	if !ok {
		return false, fmt.Errorf("invalid max_days parameter")
	}

	age := b.getBackupAge()
	return float64(age) > maxDays, nil
}

// MetricThresholdEvaluator evaluates metric threshold conditions
type MetricThresholdEvaluator struct {
	getMetricValue func(string) float64
}

// NewMetricThresholdEvaluator creates a new metric threshold evaluator
func NewMetricThresholdEvaluator(getMetricValue func(string) float64) *MetricThresholdEvaluator {
	return &MetricThresholdEvaluator{
		getMetricValue: getMetricValue,
	}
}

// Evaluate evaluates metric threshold
func (m *MetricThresholdEvaluator) Evaluate(condition RuleCondition) (bool, error) {
	metricName, ok := condition.Params["metric"].(string)
	if !ok {
		return false, fmt.Errorf("invalid metric parameter")
	}

	threshold, ok := condition.Params["threshold"].(float64)
	if !ok {
		return false, fmt.Errorf("invalid threshold parameter")
	}

	value := m.getMetricValue(metricName)
	operator, ok := condition.Params["operator"].(string)
	if !ok {
		operator = ">"
	}

	switch operator {
	case ">":
		return value > threshold, nil
	case "<":
		return value < threshold, nil
	case ">=":
		return value >= threshold, nil
	case "<=":
		return value <= threshold, nil
	case "==":
		return value == threshold, nil
	default:
		return false, fmt.Errorf("unknown operator: %s", operator)
	}
}
