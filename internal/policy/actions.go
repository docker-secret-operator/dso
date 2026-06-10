package policy

import (
	"fmt"
	"sync"
)

// ActionRunner runs rule actions
type ActionRunner struct {
	executors map[string]ActionExecutor
	mu        sync.RWMutex
}

// NewActionRunner creates a new action runner
func NewActionRunner() *ActionRunner {
	return &ActionRunner{
		executors: make(map[string]ActionExecutor),
	}
}

// RegisterExecutor registers an action executor
func (ar *ActionRunner) RegisterExecutor(actionType string, executor ActionExecutor) {
	ar.mu.Lock()
	defer ar.mu.Unlock()
	ar.executors[actionType] = executor
}

// Execute executes an action
func (ar *ActionRunner) Execute(action RuleAction) error {
	ar.mu.RLock()
	executor, exists := ar.executors[action.Type]
	ar.mu.RUnlock()

	if !exists {
		return fmt.Errorf("unknown action type: %s", action.Type)
	}

	return executor.Execute(action)
}

// CreateAlertExecutor executes create alert actions
type CreateAlertExecutor struct {
	createAlert func(title, description string, severity string) error
}

// NewCreateAlertExecutor creates a new create alert executor
func NewCreateAlertExecutor(createAlert func(title, description string, severity string) error) *CreateAlertExecutor {
	return &CreateAlertExecutor{
		createAlert: createAlert,
	}
}

// Execute creates an alert
func (c *CreateAlertExecutor) Execute(action RuleAction) error {
	title, ok := action.Params["title"].(string)
	if !ok {
		return fmt.Errorf("invalid title parameter")
	}

	description, ok := action.Params["description"].(string)
	if !ok {
		description = ""
	}

	severity, ok := action.Params["severity"].(string)
	if !ok {
		severity = "medium"
	}

	return c.createAlert(title, description, severity)
}

// SendNotificationExecutor executes send notification actions
type SendNotificationExecutor struct {
	sendNotification func(channel, message string) error
}

// NewSendNotificationExecutor creates a new send notification executor
func NewSendNotificationExecutor(sendNotification func(channel, message string) error) *SendNotificationExecutor {
	return &SendNotificationExecutor{
		sendNotification: sendNotification,
	}
}

// Execute sends a notification
func (s *SendNotificationExecutor) Execute(action RuleAction) error {
	channel, ok := action.Params["channel"].(string)
	if !ok {
		return fmt.Errorf("invalid channel parameter")
	}

	message, ok := action.Params["message"].(string)
	if !ok {
		return fmt.Errorf("invalid message parameter")
	}

	return s.sendNotification(channel, message)
}

// TriggerIntegrationExecutor executes trigger integration actions
type TriggerIntegrationExecutor struct {
	triggerIntegration func(integrationID, eventType string, data map[string]interface{}) error
}

// NewTriggerIntegrationExecutor creates a new trigger integration executor
func NewTriggerIntegrationExecutor(triggerIntegration func(integrationID, eventType string, data map[string]interface{}) error) *TriggerIntegrationExecutor {
	return &TriggerIntegrationExecutor{
		triggerIntegration: triggerIntegration,
	}
}

// Execute triggers an integration
func (t *TriggerIntegrationExecutor) Execute(action RuleAction) error {
	integrationID, ok := action.Params["integration_id"].(string)
	if !ok {
		return fmt.Errorf("invalid integration_id parameter")
	}

	eventType, ok := action.Params["event_type"].(string)
	if !ok {
		eventType = "rule_triggered"
	}

	data, ok := action.Params["data"].(map[string]interface{})
	if !ok {
		data = make(map[string]interface{})
	}

	return t.triggerIntegration(integrationID, eventType, data)
}

// PauseWorkersExecutor executes pause workers actions
type PauseWorkersExecutor struct {
	pauseWorkers func(durationSeconds int) error
}

// NewPauseWorkersExecutor creates a new pause workers executor
func NewPauseWorkersExecutor(pauseWorkers func(durationSeconds int) error) *PauseWorkersExecutor {
	return &PauseWorkersExecutor{
		pauseWorkers: pauseWorkers,
	}
}

// Execute pauses workers
func (p *PauseWorkersExecutor) Execute(action RuleAction) error {
	duration := 300 // Default 5 minutes
	if d, ok := action.Params["duration_seconds"].(float64); ok {
		duration = int(d)
	}

	return p.pauseWorkers(duration)
}

// RetryExecutionExecutor executes retry execution actions
type RetryExecutionExecutor struct {
	retryExecution func(executionID string) error
}

// NewRetryExecutionExecutor creates a new retry execution executor
func NewRetryExecutionExecutor(retryExecution func(executionID string) error) *RetryExecutionExecutor {
	return &RetryExecutionExecutor{
		retryExecution: retryExecution,
	}
}

// Execute retries an execution
func (r *RetryExecutionExecutor) Execute(action RuleAction) error {
	executionID, ok := action.Params["execution_id"].(string)
	if !ok {
		return fmt.Errorf("invalid execution_id parameter")
	}

	return r.retryExecution(executionID)
}

// DisableUserExecutor executes disable user actions
type DisableUserExecutor struct {
	disableUser func(userID string) error
}

// NewDisableUserExecutor creates a new disable user executor
func NewDisableUserExecutor(disableUser func(userID string) error) *DisableUserExecutor {
	return &DisableUserExecutor{
		disableUser: disableUser,
	}
}

// Execute disables a user
func (d *DisableUserExecutor) Execute(action RuleAction) error {
	userID, ok := action.Params["user_id"].(string)
	if !ok {
		return fmt.Errorf("invalid user_id parameter")
	}

	return d.disableUser(userID)
}

// RunSchedulerJobExecutor executes run scheduler job actions
type RunSchedulerJobExecutor struct {
	runJob func(jobID string) error
}

// NewRunSchedulerJobExecutor creates a new run scheduler job executor
func NewRunSchedulerJobExecutor(runJob func(jobID string) error) *RunSchedulerJobExecutor {
	return &RunSchedulerJobExecutor{
		runJob: runJob,
	}
}

// Execute runs a scheduler job
func (r *RunSchedulerJobExecutor) Execute(action RuleAction) error {
	jobID, ok := action.Params["job_id"].(string)
	if !ok {
		return fmt.Errorf("invalid job_id parameter")
	}

	return r.runJob(jobID)
}

// PublishEventExecutor executes publish event actions
type PublishEventExecutor struct {
	publishEvent func(eventType string, data map[string]interface{}) error
}

// NewPublishEventExecutor creates a new publish event executor
func NewPublishEventExecutor(publishEvent func(eventType string, data map[string]interface{}) error) *PublishEventExecutor {
	return &PublishEventExecutor{
		publishEvent: publishEvent,
	}
}

// Execute publishes an event
func (p *PublishEventExecutor) Execute(action RuleAction) error {
	eventType, ok := action.Params["event_type"].(string)
	if !ok {
		return fmt.Errorf("invalid event_type parameter")
	}

	data, ok := action.Params["data"].(map[string]interface{})
	if !ok {
		data = make(map[string]interface{})
	}

	return p.publishEvent(eventType, data)
}
