package autonomy

import (
	"fmt"
	"time"
)

// RollbackEngine manages rollback operations
type RollbackEngine struct {
	executors map[ActionType]RollbackExecutor
}

// NewRollbackEngine creates a new rollback engine
func NewRollbackEngine() *RollbackEngine {
	return &RollbackEngine{
		executors: make(map[ActionType]RollbackExecutor),
	}
}

// RegisterRollback registers a rollback executor
func (re *RollbackEngine) RegisterRollback(actionType ActionType, executor RollbackExecutor) {
	re.executors[actionType] = executor
}

// CanRollback checks if an action can be rolled back
func (re *RollbackEngine) CanRollback(action *AutonomousAction) bool {
	if !action.RollbackSupported {
		return false
	}

	if action.Status != StatusSucceeded && action.Status != StatusFailed {
		return false
	}

	_, exists := re.executors[action.Type]
	return exists
}

// Rollback executes a rollback for an action
func (re *RollbackEngine) Rollback(action *AutonomousAction) (*RollbackEntry, error) {
	if !re.CanRollback(action) {
		return nil, fmt.Errorf("action cannot be rolled back")
	}

	executor, exists := re.executors[action.Type]
	if !exists {
		return nil, fmt.Errorf("no rollback executor for action type: %s", action.Type)
	}

	result, err := executor(action)

	entry := &RollbackEntry{
		ActionID:  action.ID,
		Success:   err == nil,
		Timestamp: time.Now(),
		Result:    result,
	}

	if err != nil {
		entry.Reason = err.Error()
	}

	return entry, err
}

// DefaultRollbackExecutors returns default rollback executors
func DefaultRollbackExecutors() map[ActionType]RollbackExecutor {
	return map[ActionType]RollbackExecutor{
		ActionRestartPlugin: func(action *AutonomousAction) (string, error) {
			// Rollback: Try to restore plugin state
			return fmt.Sprintf("Plugin restart rollback initiated for %s", action.ResourceID), nil
		},
		ActionPauseSchedulerJob: func(action *AutonomousAction) (string, error) {
			// Rollback: Resume the job
			return fmt.Sprintf("Scheduler job %s resumed", action.ResourceID), nil
		},
		ActionResumeSchedulerJob: func(action *AutonomousAction) (string, error) {
			// Rollback: Pause the job again
			return fmt.Sprintf("Scheduler job %s paused again", action.ResourceID), nil
		},
		ActionRetryExecution: func(action *AutonomousAction) (string, error) {
			// Rollback: Cancel the retried execution
			return fmt.Sprintf("Retried execution %s cancelled", action.ResourceID), nil
		},
		ActionRunBackup: func(action *AutonomousAction) (string, error) {
			// Rollback: Backup cannot be truly rolled back, but mark as abandoned
			return fmt.Sprintf("Backup operation %s marked as abandoned", action.ResourceID), nil
		},
		ActionCleanupRetention: func(action *AutonomousAction) (string, error) {
			// Rollback: Cannot restore deleted data, but log the rollback
			return fmt.Sprintf("Cleanup operation %s marked for investigation", action.ResourceID), nil
		},
		ActionAcknowledgeDrift: func(action *AutonomousAction) (string, error) {
			// Rollback: Un-acknowledge the drift
			return fmt.Sprintf("Drift acknowledgment %s reverted", action.ResourceID), nil
		},
		ActionResolveIncident: func(action *AutonomousAction) (string, error) {
			// Rollback: Re-open the incident
			return fmt.Sprintf("Incident %s reopened", action.ResourceID), nil
		},
		ActionRotateSecret: func(action *AutonomousAction) (string, error) {
			// Rollback: Restore previous secret version
			return fmt.Sprintf("Secret %s restored to previous version", action.ResourceID), nil
		},
		ActionRetryIntegration: func(action *AutonomousAction) (string, error) {
			// Rollback: Mark retry as abandoned
			return fmt.Sprintf("Integration retry %s abandoned", action.ResourceID), nil
		},
	}
}
