package autonomy

import (
	"time"
)

// ActionType represents the type of autonomous action
type ActionType string

const (
	ActionRestartPlugin        ActionType = "restart_plugin"
	ActionRetryIntegration     ActionType = "retry_integration"
	ActionRunBackup            ActionType = "run_backup"
	ActionPauseSchedulerJob    ActionType = "pause_scheduler_job"
	ActionResumeSchedulerJob   ActionType = "resume_scheduler_job"
	ActionRetryExecution       ActionType = "retry_execution"
	ActionAcknowledgeDrift     ActionType = "acknowledge_drift"
	ActionResolveIncident      ActionType = "resolve_incident"
	ActionRotateSecret         ActionType = "rotate_secret"
	ActionCleanupRetention     ActionType = "cleanup_retention"
)

// ActionStatus represents the status of an autonomous action
type ActionStatus string

const (
	StatusPending   ActionStatus = "pending"
	StatusRunning   ActionStatus = "running"
	StatusSucceeded ActionStatus = "succeeded"
	StatusFailed    ActionStatus = "failed"
	StatusRolledBack ActionStatus = "rolled_back"
	StatusCancelled ActionStatus = "cancelled"
)

// SafetyLevel represents the required approval level for an action
type SafetyLevel string

const (
	SafetyManualOnly      SafetyLevel = "manual_only"
	SafetyApprovalRequired SafetyLevel = "approval_required"
	SafetyAutomatic       SafetyLevel = "automatic"
)

// AutonomousAction represents an autonomous remediation action
type AutonomousAction struct {
	ID                string
	Type              ActionType
	Status            ActionStatus
	SafetyLevel       SafetyLevel
	ResourceID        string
	Trigger           string
	Reason            string
	RollbackSupported bool
	DryRun            bool
	StartedAt         *time.Time
	CompletedAt       *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
	Metadata          map[string]string
	Result            string
	Error             string
}

// ActionMetrics tracks autonomous action metrics
type ActionMetrics struct {
	TotalActions      int
	SuccessfulActions int
	FailedActions     int
	RollbackCount     int
	AutomaticActions  int
	ManualActions     int
	SuccessRate       float64
	LastUpdate        time.Time
}

// RollbackEntry represents a rollback operation
type RollbackEntry struct {
	ActionID  string
	Success   bool
	Timestamp time.Time
	Reason    string
	Result    string
}

// CanExecuteAutomatically returns true if action can run automatically
func (a *AutonomousAction) CanExecuteAutomatically() bool {
	return a.SafetyLevel == SafetyAutomatic
}

// RequiresApproval returns true if action needs approval
func (a *AutonomousAction) RequiresApproval() bool {
	return a.SafetyLevel == SafetyApprovalRequired
}

// IsManualOnly returns true if action is manual only
func (a *AutonomousAction) IsManualOnly() bool {
	return a.SafetyLevel == SafetyManualOnly
}

// IsCompleted returns true if action is complete
func (a *AutonomousAction) IsCompleted() bool {
	return a.Status == StatusSucceeded || a.Status == StatusFailed || a.Status == StatusRolledBack || a.Status == StatusCancelled
}

// Duration returns the duration of the action execution
func (a *AutonomousAction) Duration() time.Duration {
	if a.StartedAt == nil {
		return 0
	}

	end := time.Now()
	if a.CompletedAt != nil {
		end = *a.CompletedAt
	}

	return end.Sub(*a.StartedAt)
}

// ActionDescriptor describes an action that can be executed
type ActionDescriptor struct {
	Type              ActionType
	Name              string
	Description       string
	SafetyLevel       SafetyLevel
	RollbackSupported bool
	Executor          ActionExecutor
}

// ActionExecutor is a function that executes an action
type ActionExecutor func(action *AutonomousAction) (string, error)

// RollbackExecutor is a function that rolls back an action
type RollbackExecutor func(action *AutonomousAction) (string, error)
