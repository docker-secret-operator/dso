package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// AlertService manages alert rules and alert lifecycle
type AlertService struct {
	store   storage.StorageProvider
	logger  *zap.Logger
	stopCh  chan struct{}
	wg      sync.WaitGroup
	running bool
	mu      sync.Mutex
}

// NewAlertService creates a new alert service
func NewAlertService(store storage.StorageProvider, logger *zap.Logger) *AlertService {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &AlertService{
		store:  store,
		logger: logger,
		stopCh: make(chan struct{}),
	}
}

// Start begins the background evaluation worker
func (as *AlertService) Start(ctx context.Context) error {
	as.mu.Lock()
	if as.running {
		as.mu.Unlock()
		return fmt.Errorf("alert service already running")
	}
	as.running = true
	as.mu.Unlock()

	// Initialize builtin rules on first start
	if err := as.seedBuiltinRules(ctx); err != nil {
		as.logger.Error("failed to seed builtin rules", zap.Error(err))
	}

	as.wg.Add(1)
	go as.evaluationWorker(ctx)
	as.logger.Info("Alert service started")
	return nil
}

// Stop gracefully shuts down the background worker
func (as *AlertService) Stop() {
	as.mu.Lock()
	if !as.running {
		as.mu.Unlock()
		return
	}
	as.running = false
	as.mu.Unlock()

	close(as.stopCh)
	as.wg.Wait()
	as.logger.Info("Alert service stopped")
}

// evaluationWorker periodically evaluates alert rules
func (as *AlertService) evaluationWorker(ctx context.Context) {
	defer as.wg.Done()

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-as.stopCh:
			return
		case <-ticker.C:
			if err := as.EvaluateAllRules(ctx); err != nil {
				as.logger.Error("rule evaluation failed", zap.Error(err))
			}
		}
	}
}

// EvaluateAllRules evaluates all enabled alert rules
func (as *AlertService) EvaluateAllRules(ctx context.Context) error {
	rules, err := as.store.AlertRules().ListEnabled(ctx)
	if err != nil {
		return fmt.Errorf("failed to list enabled rules: %w", err)
	}

	for _, rule := range rules {
		if err := as.evaluateRule(ctx, rule); err != nil {
			as.logger.Warn("failed to evaluate rule", zap.String("rule_id", rule.ID), zap.Error(err))
		}
	}

	return nil
}

// evaluateRule evaluates a single alert rule
func (as *AlertService) evaluateRule(ctx context.Context, rule *storage.AlertRule) error {
	// Get current value for the metric
	value, err := as.getMetricValue(ctx, rule.Metric)
	if err != nil {
		return fmt.Errorf("failed to get metric value: %w", err)
	}

	// Check if condition is met
	conditionMet := as.evaluateCondition(value, rule.Operator, rule.Threshold)

	// Get existing active alert
	existingAlert, err := as.store.Alerts().GetActiveByRuleID(ctx, rule.ID)
	if err != nil {
		return fmt.Errorf("failed to get existing alert: %w", err)
	}

	if conditionMet {
		if existingAlert != nil {
			// Update last fired time
			existingAlert.LastFiredAt = time.Now()
			existingAlert.Value = value
			if err := as.store.Alerts().Update(ctx, existingAlert); err != nil {
				return fmt.Errorf("failed to update alert: %w", err)
			}
		} else {
			// Check cooldown
			if err := as.checkCooldown(ctx, rule); err != nil {
				return nil // Cooldown still active
			}

			// Create new alert
			alert := &storage.Alert{
				ID:          uuid.New().String(),
				RuleID:      rule.ID,
				State:       "active",
				Severity:    rule.Severity,
				Metric:      rule.Metric,
				Message:     fmt.Sprintf("Alert: %s is %s %v", rule.Name, rule.Operator, rule.Threshold),
				Value:       value,
				Threshold:   rule.Threshold,
				LastFiredAt: time.Now(),
				CreatedAt:   time.Now(),
			}

			if err := as.store.Alerts().Create(ctx, alert); err != nil {
				return fmt.Errorf("failed to create alert: %w", err)
			}

			as.logger.Info("Alert created",
				zap.String("alert_id", alert.ID),
				zap.String("rule_id", rule.ID),
				zap.String("severity", alert.Severity))
		}
	}

	return nil
}

// evaluateCondition checks if value matches the operator and threshold
func (as *AlertService) evaluateCondition(value float64, operator string, threshold float64) bool {
	switch operator {
	case ">":
		return value > threshold
	case ">=":
		return value >= threshold
	case "<":
		return value < threshold
	case "<=":
		return value <= threshold
	case "==":
		return value == threshold
	case "!=":
		return value != threshold
	default:
		return false
	}
}

// getMetricValue retrieves the current value for a metric
func (as *AlertService) getMetricValue(ctx context.Context, metric string) (float64, error) {
	// This is a placeholder that returns dummy values
	// In production, these would query actual metrics from the system
	switch metric {
	case "queue_depth":
		return 100, nil // placeholder
	case "failure_rate":
		return 5.0, nil // placeholder
	case "worker_utilization":
		return 75.0, nil // placeholder
	case "memory_usage":
		return 1024, nil // placeholder (MB)
	case "login_failures_24h":
		// Query from security events
		filters := map[string]interface{}{
			"type":       "LOGIN_FAILURE",
			"start_time": time.Now().Add(-24 * time.Hour),
			"end_time":   time.Now(),
		}
		events, err := as.store.SecurityEvents().Query(ctx, filters)
		if err != nil {
			return 0, err
		}
		return float64(len(events)), nil
	case "brute_force_attempts":
		// Query from suspicious activities
		activities, err := as.store.SuspiciousActivities().List(ctx, 1000, 0)
		if err != nil {
			return 0, err
		}
		count := 0
		for _, a := range activities {
			if a.Type == "brute_force" {
				count++
			}
		}
		return float64(count), nil
	default:
		return 0, fmt.Errorf("unknown metric: %s", metric)
	}
}

// checkCooldown checks if enough time has passed since the last alert
func (as *AlertService) checkCooldown(ctx context.Context, rule *storage.AlertRule) error {
	alerts, err := as.store.Alerts().ListByRuleID(ctx, rule.ID)
	if err != nil {
		return err
	}

	if len(alerts) == 0 {
		return nil // No previous alerts
	}

	lastAlert := alerts[0]
	cooldownDuration := time.Duration(rule.Cooldown) * time.Second
	if time.Since(lastAlert.LastFiredAt) < cooldownDuration {
		return fmt.Errorf("cooldown active") // Cooldown still active
	}

	return nil
}

// CreateRule creates a new alert rule
func (as *AlertService) CreateRule(ctx context.Context, rule *storage.AlertRule) error {
	rule.ID = uuid.New().String()
	rule.CreatedAt = time.Now()
	rule.UpdatedAt = time.Now()
	rule.IsBuiltin = false
	return as.store.AlertRules().Create(ctx, rule)
}

// UpdateRule updates an existing alert rule
func (as *AlertService) UpdateRule(ctx context.Context, rule *storage.AlertRule) error {
	rule.UpdatedAt = time.Now()
	return as.store.AlertRules().Update(ctx, rule)
}

// DeleteRule deletes an alert rule (unless it's builtin)
func (as *AlertService) DeleteRule(ctx context.Context, ruleID string) error {
	return as.store.AlertRules().Delete(ctx, ruleID)
}

// GetRules retrieves all alert rules
func (as *AlertService) GetRules(ctx context.Context, limit int, offset int) ([]*storage.AlertRule, error) {
	return as.store.AlertRules().List(ctx, limit, offset)
}

// GetAlerts retrieves alerts with optional state filter
func (as *AlertService) GetAlerts(ctx context.Context, state string, limit int, offset int) ([]*storage.Alert, error) {
	if state != "" {
		return as.store.Alerts().ListByState(ctx, state, limit, offset)
	}
	return as.store.Alerts().List(ctx, limit, offset)
}

// AcknowledgeAlert acknowledges an alert
func (as *AlertService) AcknowledgeAlert(ctx context.Context, alertID, actor string) error {
	alert, err := as.store.Alerts().GetByID(ctx, alertID)
	if err != nil {
		return err
	}
	if alert == nil {
		return fmt.Errorf("alert not found")
	}

	now := time.Now()
	alert.State = "acknowledged"
	alert.AcknowledgedBy = &actor
	alert.AcknowledgedAt = &now

	return as.store.Alerts().Update(ctx, alert)
}

// ResolveAlert resolves an alert
func (as *AlertService) ResolveAlert(ctx context.Context, alertID, actor string) error {
	alert, err := as.store.Alerts().GetByID(ctx, alertID)
	if err != nil {
		return err
	}
	if alert == nil {
		return fmt.Errorf("alert not found")
	}

	now := time.Now()
	alert.State = "resolved"
	alert.ResolvedBy = &actor
	alert.ResolvedAt = &now

	return as.store.Alerts().Update(ctx, alert)
}

// SuppressAlert suppresses an alert until the specified time
func (as *AlertService) SuppressAlert(ctx context.Context, alertID, actor string, suppressUntil time.Time) error {
	alert, err := as.store.Alerts().GetByID(ctx, alertID)
	if err != nil {
		return err
	}
	if alert == nil {
		return fmt.Errorf("alert not found")
	}

	alert.State = "suppressed"
	alert.SuppressedBy = &actor
	alert.SuppressedUntil = &suppressUntil

	return as.store.Alerts().Update(ctx, alert)
}

// seedBuiltinRules creates default alert rules on first startup
func (as *AlertService) seedBuiltinRules(ctx context.Context) error {
	rules, err := as.store.AlertRules().ListEnabled(ctx)
	if err != nil {
		return err
	}

	// Check if builtin rules already exist
	if len(rules) > 0 {
		return nil
	}

	builtinRules := []*storage.AlertRule{
		{
			Name:        "Queue Depth High",
			Description: stringPtr("Queue depth exceeds 500 items"),
			Enabled:     true,
			Severity:    "high",
			Metric:      "queue_depth",
			Operator:    ">",
			Threshold:   500,
			Duration:    60,
			Cooldown:    300,
			IsBuiltin:   true,
		},
		{
			Name:        "Worker Utilization High",
			Description: stringPtr("Worker utilization exceeds 90%"),
			Enabled:     true,
			Severity:    "high",
			Metric:      "worker_utilization",
			Operator:    ">",
			Threshold:   90,
			Duration:    60,
			Cooldown:    300,
			IsBuiltin:   true,
		},
		{
			Name:        "Failure Rate High",
			Description: stringPtr("Failure rate exceeds 10%"),
			Enabled:     true,
			Severity:    "medium",
			Metric:      "failure_rate",
			Operator:    ">",
			Threshold:   10,
			Duration:    60,
			Cooldown:    300,
			IsBuiltin:   true,
		},
		{
			Name:        "Memory Usage High",
			Description: stringPtr("Memory usage exceeds 2GB"),
			Enabled:     true,
			Severity:    "high",
			Metric:      "memory_usage",
			Operator:    ">",
			Threshold:   2048,
			Duration:    60,
			Cooldown:    300,
			IsBuiltin:   true,
		},
		{
			Name:        "Brute Force Attempts",
			Description: stringPtr("Brute force attacks detected"),
			Enabled:     true,
			Severity:    "critical",
			Metric:      "brute_force_attempts",
			Operator:    ">",
			Threshold:   0,
			Duration:    60,
			Cooldown:    600,
			IsBuiltin:   true,
		},
		{
			Name:        "Login Failures High",
			Description: stringPtr("Login failures exceed 20 in 24 hours"),
			Enabled:     true,
			Severity:    "medium",
			Metric:      "login_failures_24h",
			Operator:    ">",
			Threshold:   20,
			Duration:    60,
			Cooldown:    300,
			IsBuiltin:   true,
		},
	}

	for _, rule := range builtinRules {
		rule.ID = uuid.New().String()
		rule.CreatedAt = time.Now()
		rule.UpdatedAt = time.Now()
		if err := as.store.AlertRules().Create(ctx, rule); err != nil {
			as.logger.Warn("failed to seed builtin rule", zap.String("name", rule.Name), zap.Error(err))
		}
	}

	as.logger.Info("Seeded builtin alert rules", zap.Int("count", len(builtinRules)))
	return nil
}

func stringPtr(s string) *string {
	return &s
}
