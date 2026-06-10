package policy

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Engine is the policy evaluation engine
type Engine struct {
	rules       map[string]*Rule
	store       RuleStore
	evaluator   *Evaluator
	runner      *ActionRunner
	metrics     *Metrics
	logger      *zap.Logger
	eventBus    interface{} // Can be EventBus for publishing events
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
}

// NewEngine creates a new policy engine
func NewEngine(store RuleStore, logger *zap.Logger) *Engine {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Engine{
		rules:     make(map[string]*Rule),
		store:     store,
		evaluator: NewEvaluator(),
		runner:    NewActionRunner(),
		metrics:   NewMetrics(),
		logger:    logger,
		done:      make(chan struct{}),
	}
}

// Initialize starts the policy engine
func (e *Engine) Initialize(ctx context.Context) error {
	e.ctx, e.cancel = context.WithCancel(ctx)

	// Load rules from storage
	rules, err := e.store.ListRules(e.ctx)
	if err != nil {
		e.logger.Error("failed to load rules", zap.Error(err))
	} else {
		for _, rule := range rules {
			e.rules[rule.ID] = rule
		}
	}

	// Start background loop for scheduled rules
	go e.runLoop()

	e.logger.Info("Policy Engine initialized", zap.Int("rules", len(e.rules)))
	return nil
}

// Shutdown gracefully stops the policy engine
func (e *Engine) Shutdown(ctx context.Context) error {
	if e.cancel != nil {
		e.cancel()
	}

	select {
	case <-e.done:
	case <-time.After(5 * time.Second):
		e.logger.Warn("policy engine shutdown timeout")
	}

	return nil
}

// SetEventBus sets the event bus for publishing events
func (e *Engine) SetEventBus(eventBus interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.eventBus = eventBus
}

// RegisterRule registers a rule
func (e *Engine) RegisterRule(rule *Rule) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.rules[rule.ID]; exists {
		return fmt.Errorf("rule already registered: %s", rule.ID)
	}

	e.rules[rule.ID] = rule
	e.metrics.RecordRuleRegistered()

	// Persist to storage
	if err := e.store.CreateRule(e.ctx, rule); err != nil {
		e.logger.Error("failed to persist rule", zap.String("rule_id", rule.ID), zap.Error(err))
	}

	e.logger.Info("Rule registered", zap.String("rule_id", rule.ID), zap.String("name", rule.Name))
	return nil
}

// EvaluateRule evaluates a rule manually
func (e *Engine) EvaluateRule(ruleID string) error {
	e.mu.Lock()
	rule, exists := e.rules[ruleID]
	e.mu.Unlock()

	if !exists {
		return fmt.Errorf("rule not found: %s", ruleID)
	}

	return e.executeRule(rule)
}

// EnableRule enables a rule
func (e *Engine) EnableRule(ruleID string) error {
	e.mu.Lock()
	rule, exists := e.rules[ruleID]
	e.mu.Unlock()

	if !exists {
		return fmt.Errorf("rule not found: %s", ruleID)
	}

	rule.Enabled = true
	return e.store.UpdateRule(e.ctx, rule)
}

// DisableRule disables a rule
func (e *Engine) DisableRule(ruleID string) error {
	e.mu.Lock()
	rule, exists := e.rules[ruleID]
	e.mu.Unlock()

	if !exists {
		return fmt.Errorf("rule not found: %s", ruleID)
	}

	rule.Enabled = false
	return e.store.UpdateRule(e.ctx, rule)
}

// DeleteRule removes a rule
func (e *Engine) DeleteRule(ruleID string) error {
	e.mu.Lock()
	delete(e.rules, ruleID)
	e.mu.Unlock()

	return e.store.DeleteRule(e.ctx, ruleID)
}

// ListRules returns all registered rules
func (e *Engine) ListRules() []*Rule {
	e.mu.RLock()
	defer e.mu.RUnlock()

	rules := make([]*Rule, 0, len(e.rules))
	for _, rule := range e.rules {
		rules = append(rules, rule)
	}
	return rules
}

// GetRule returns a specific rule
func (e *Engine) GetRule(ruleID string) *Rule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.rules[ruleID]
}

// GetMetrics returns engine metrics
func (e *Engine) GetMetrics() *RuleMetrics {
	return e.metrics.GetMetrics(e.ListRules())
}

// runLoop is the main evaluation loop
func (e *Engine) runLoop() {
	defer close(e.done)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			e.evaluateScheduledRules()
		}
	}
}

// evaluateScheduledRules evaluates all scheduled rules
func (e *Engine) evaluateScheduledRules() {
	rules := e.ListRules()
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}

		if rule.Trigger != TriggerScheduled {
			continue
		}

		// Execute scheduled rule
		if err := e.executeRule(rule); err != nil {
			e.logger.Error("failed to execute rule",
				zap.String("rule_id", rule.ID),
				zap.Error(err))
		}
	}
}

// executeRule executes a single rule
func (e *Engine) executeRule(rule *Rule) error {
	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("rule panic",
				zap.String("rule_id", rule.ID),
				zap.Any("panic", r))
			e.metrics.RecordExecution(false)
		}
	}()

	startTime := time.Now()

	// Evaluate condition
	conditionMet, err := e.evaluator.Evaluate(rule.Condition)
	if err != nil {
		e.logger.Error("condition evaluation failed",
			zap.String("rule_id", rule.ID),
			zap.Error(err))
		e.recordExecution(rule.ID, false, time.Since(startTime), err.Error(), ResultFailure)
		return err
	}

	// Skip if condition not met
	if !conditionMet {
		e.recordExecution(rule.ID, true, time.Since(startTime), "", ResultSkipped)
		return nil
	}

	// Execute actions
	for _, action := range rule.Actions {
		if err := e.runner.Execute(action); err != nil {
			e.logger.Error("action execution failed",
				zap.String("rule_id", rule.ID),
				zap.String("action_type", action.Type),
				zap.Error(err))
			e.recordExecution(rule.ID, false, time.Since(startTime), err.Error(), ResultFailure)
			return err
		}
	}

	// Publish event
	e.publishEvent("RuleSucceeded", map[string]interface{}{
		"rule_id": rule.ID,
		"name":    rule.Name,
	})

	e.recordExecution(rule.ID, true, time.Since(startTime), "", ResultSuccess)
	return nil
}

// recordExecution records a rule execution
func (e *Engine) recordExecution(ruleID string, success bool, duration time.Duration, errorMsg string, result RuleResult) {
	execution := &RuleExecution{
		ID:        fmt.Sprintf("%s-%d", ruleID, time.Now().UnixNano()),
		RuleID:    ruleID,
		Success:   success,
		Duration:  duration,
		Error:     errorMsg,
		Result:    result,
		CreatedAt: time.Now(),
	}

	if err := e.store.LogExecution(e.ctx, execution); err != nil {
		e.logger.Error("failed to log execution", zap.Error(err))
	}

	e.mu.Lock()
	if rule, exists := e.rules[ruleID]; exists {
		now := time.Now()
		rule.LastRun = &now
		rule.LastResult = result
	}
	e.mu.Unlock()

	e.metrics.RecordExecution(success)
}

// publishEvent publishes a policy event
func (e *Engine) publishEvent(eventType string, data map[string]interface{}) {
	if e.eventBus == nil {
		return
	}

	if bus, ok := e.eventBus.(interface{ Publish(string, map[string]interface{}) }); ok {
		bus.Publish(eventType, data)
	}
}

// OnEvent handles incoming events for event-triggered rules
func (e *Engine) OnEvent(eventType string, data map[string]interface{}) {
	rules := e.ListRules()
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}

		if rule.Trigger != TriggerEvent {
			continue
		}

		if rule.EventType != eventType {
			continue
		}

		// Execute event-triggered rule
		if err := e.executeRule(rule); err != nil {
			e.logger.Error("failed to execute rule",
				zap.String("rule_id", rule.ID),
				zap.Error(err))
		}
	}
}

// RuleStore interface for persistence
type RuleStore interface {
	CreateRule(ctx context.Context, rule *Rule) error
	UpdateRule(ctx context.Context, rule *Rule) error
	GetRule(ctx context.Context, id string) (*Rule, error)
	ListRules(ctx context.Context) ([]*Rule, error)
	DeleteRule(ctx context.Context, id string) error
	LogExecution(ctx context.Context, execution *RuleExecution) error
	GetExecutions(ctx context.Context, ruleID string, limit int) ([]*RuleExecution, error)
	CleanupOldExecutions(ctx context.Context, olderThan time.Time) error
}
