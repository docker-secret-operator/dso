package autonomy

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Engine implements the autonomous operations engine
type Engine struct {
	mu           sync.RWMutex
	logger       *zap.Logger
	store        Store
	rules        *RuleEngine
	rollback     *RollbackEngine
	metrics      *Metrics
	eventBus     interface{}
	active       bool
	stopChan     chan struct{}
	actions      map[string]*AutonomousAction
	executors    map[ActionType]ActionExecutor
	cleanupTicker *time.Ticker
}

// NewEngine creates a new autonomy engine
func NewEngine(logger *zap.Logger, store Store) *Engine {
	if logger == nil {
		logger = zap.NewNop()
	}

	if store == nil {
		store = NewInMemoryStore()
	}

	return &Engine{
		logger:       logger,
		store:        store,
		rules:        NewRuleEngine(),
		rollback:     NewRollbackEngine(),
		metrics:      NewMetrics(),
		stopChan:     make(chan struct{}),
		actions:      make(map[string]*AutonomousAction),
		executors:    make(map[ActionType]ActionExecutor),
	}
}

// Initialize initializes the engine
func (e *Engine) Initialize() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.active {
		return fmt.Errorf("engine already initialized")
	}

	// Add default rules and executors
	for _, rule := range DefaultRules() {
		e.rules.AddRule(rule)
	}

	for actionType, executor := range DefaultRollbackExecutors() {
		e.rollback.RegisterRollback(actionType, executor)
	}

	e.active = true
	e.logger.Info("Autonomy engine initialized")

	go e.cleanupLoop()

	return nil
}

// Shutdown gracefully shuts down the engine
func (e *Engine) Shutdown() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.active {
		return nil
	}

	e.active = false
	close(e.stopChan)

	if e.cleanupTicker != nil {
		e.cleanupTicker.Stop()
	}

	e.logger.Info("Autonomy engine shutdown complete")
	return nil
}

// SetEventBus sets the event bus
func (e *Engine) SetEventBus(eventBus interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.eventBus = eventBus
}

// RegisterExecutor registers an action executor
func (e *Engine) RegisterExecutor(actionType ActionType, executor ActionExecutor) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.executors[actionType] = executor
}

// ExecuteAction executes an autonomous action
func (e *Engine) ExecuteAction(action *AutonomousAction) error {
	if action == nil {
		return fmt.Errorf("action is nil")
	}

	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("Panic executing action", zap.Any("recover", r))
			action.Status = StatusFailed
			action.Error = fmt.Sprintf("panic: %v", r)
		}
	}()

	e.mu.Lock()
	e.actions[action.ID] = action
	e.mu.Unlock()

	if err := e.store.SaveAction(action); err != nil {
		e.logger.Error("Failed to save action", zap.Error(err))
	}

	// Check if action can execute automatically
	if !action.CanExecuteAutomatically() && action.SafetyLevel != SafetyApprovalRequired {
		action.Status = StatusPending
		e.publishEvent("AutonomousActionStarted", map[string]interface{}{
			"action_id": action.ID,
			"type":      action.Type,
		})
		return nil
	}

	// Execute action
	action.Status = StatusRunning
	now := time.Now()
	action.StartedAt = &now

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	executor, exists := e.executors[action.Type]
	if !exists {
		action.Status = StatusFailed
		action.Error = fmt.Sprintf("no executor for action type: %s", action.Type)
		e.metrics.RecordFailure()
		return fmt.Errorf("%s", action.Error)
	}

	// Run with context timeout
	doneChan := make(chan string, 1)
	errChan := make(chan error, 1)

	go func() {
		result, err := executor(action)
		if err != nil {
			errChan <- err
		} else {
			doneChan <- result
		}
	}()

	select {
	case result := <-doneChan:
		action.Status = StatusSucceeded
		action.Result = result
		e.metrics.RecordSuccess()
	case err := <-errChan:
		action.Status = StatusFailed
		action.Error = err.Error()
		e.metrics.RecordFailure()
	case <-ctx.Done():
		action.Status = StatusFailed
		action.Error = "action execution timeout"
		e.metrics.RecordFailure()
	}

	completedAt := time.Now()
	action.CompletedAt = &completedAt

	if err := e.store.UpdateAction(action); err != nil {
		e.logger.Error("Failed to update action", zap.Error(err))
	}

	if action.Status == StatusSucceeded {
		e.publishEvent("AutonomousActionSucceeded", map[string]interface{}{
			"action_id": action.ID,
			"type":      action.Type,
			"duration":  action.Duration().Seconds(),
		})
	} else {
		e.publishEvent("AutonomousActionFailed", map[string]interface{}{
			"action_id": action.ID,
			"type":      action.Type,
			"error":     action.Error,
		})
	}

	return nil
}

// CancelAction cancels a pending action
func (e *Engine) CancelAction(actionID string) error {
	e.mu.Lock()
	action, exists := e.actions[actionID]
	e.mu.Unlock()

	if !exists {
		return fmt.Errorf("action not found: %s", actionID)
	}

	if action.Status != StatusPending && action.Status != StatusRunning {
		return fmt.Errorf("cannot cancel action in status: %s", action.Status)
	}

	action.Status = StatusCancelled
	now := time.Now()
	action.CompletedAt = &now

	if err := e.store.UpdateAction(action); err != nil {
		return err
	}

	return nil
}

// RollbackAction rolls back an action
func (e *Engine) RollbackAction(actionID string) error {
	e.mu.RLock()
	action, exists := e.actions[actionID]
	e.mu.RUnlock()

	if !exists {
		return fmt.Errorf("action not found: %s", actionID)
	}

	entry, err := e.rollback.Rollback(action)
	if err != nil {
		e.logger.Error("Rollback failed", zap.Error(err))
		return err
	}

	if err := e.store.SaveRollback(entry); err != nil {
		e.logger.Error("Failed to save rollback entry", zap.Error(err))
	}

	if entry.Success {
		action.Status = StatusRolledBack
		e.metrics.RecordRollback()

		e.publishEvent("AutonomousActionRolledBack", map[string]interface{}{
			"action_id": action.ID,
			"type":      action.Type,
		})
	}

	if err := e.store.UpdateAction(action); err != nil {
		e.logger.Error("Failed to update action", zap.Error(err))
	}

	return nil
}

// ListActions lists actions
func (e *Engine) ListActions(limit int) ([]*AutonomousAction, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.store.ListActions(limit)
}

// GetAction retrieves an action
func (e *Engine) GetAction(id string) (*AutonomousAction, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.store.GetAction(id)
}

// GetMetrics returns metrics
func (e *Engine) GetMetrics() *ActionMetrics {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.metrics.GetMetrics()
}

// ExecuteFromEvent creates and executes actions from events
func (e *Engine) ExecuteFromEvent(triggerType string, data map[string]interface{}) error {
	e.mu.RLock()
	rules := e.rules.GetRulesByTriggerType(triggerType)
	e.mu.RUnlock()

	for _, rule := range rules {
		if MatchRule(rule, data) {
			action := &AutonomousAction{
				ID:                uuid.New().String(),
				Type:              rule.ActionType,
				Status:            StatusPending,
				SafetyLevel:       rule.SafetyLevel,
				Trigger:           triggerType,
				Reason:            rule.Description,
				RollbackSupported: true,
				CreatedAt:         time.Now(),
				UpdatedAt:         time.Now(),
				Metadata:          make(map[string]string),
			}

			if resourceID, ok := data["resource_id"].(string); ok {
				action.ResourceID = resourceID
			}

			e.metrics.RecordAction(action.SafetyLevel)
			e.ExecuteAction(action)
		}
	}

	return nil
}

// cleanupLoop periodically cleans up old actions
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

// cleanup removes old actions
func (e *Engine) cleanup() {
	e.mu.Lock()
	defer e.mu.Unlock()

	cutoff := time.Now().Add(-90 * 24 * time.Hour)

	if err := e.store.CleanupOld(cutoff); err != nil {
		e.logger.Error("Failed to cleanup old actions", zap.Error(err))
	}
}

// publishEvent publishes an event
func (e *Engine) publishEvent(eventType string, data map[string]interface{}) {
	if e.eventBus == nil {
		return
	}

	if bus, ok := e.eventBus.(interface{ Publish(string, map[string]interface{}) }); ok {
		bus.Publish(eventType, data)
	}
}
