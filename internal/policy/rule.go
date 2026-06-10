package policy

import (
	"time"
)

// RuleSeverity represents the severity level of a rule
type RuleSeverity string

const (
	SeverityInfo     RuleSeverity = "info"
	SeverityLow      RuleSeverity = "low"
	SeverityMedium   RuleSeverity = "medium"
	SeverityHigh     RuleSeverity = "high"
	SeverityCritical RuleSeverity = "critical"
)

// RuleTrigger defines how a rule is triggered
type RuleTrigger string

const (
	TriggerScheduled RuleTrigger = "scheduled"
	TriggerEvent     RuleTrigger = "event"
	TriggerManual    RuleTrigger = "manual"
)

// RuleResult represents the outcome of a rule evaluation
type RuleResult string

const (
	ResultSuccess RuleResult = "success"
	ResultFailure RuleResult = "failure"
	ResultSkipped RuleResult = "skipped"
)

// Rule represents a decision rule that can be evaluated
type Rule struct {
	ID          string
	Name        string
	Description string
	Enabled     bool
	Severity    RuleSeverity
	Trigger     RuleTrigger
	Schedule    string // Cron expression for scheduled triggers
	EventType   string // Event type for event-triggered rules
	Condition   RuleCondition
	Actions     []RuleAction
	LastRun     *time.Time
	LastResult  RuleResult
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// RuleCondition represents a condition that must be evaluated
type RuleCondition struct {
	Type   string                 `json:"type"`
	Params map[string]interface{} `json:"params"`
}

// RuleAction represents an action to take when a rule succeeds
type RuleAction struct {
	Type   string                 `json:"type"`
	Params map[string]interface{} `json:"params"`
}

// RuleExecution represents the result of a rule execution
type RuleExecution struct {
	ID        string
	RuleID    string
	Success   bool
	Duration  time.Duration
	Error     string
	Result    RuleResult
	CreatedAt time.Time
}

// RuleMetrics tracks rule metrics
type RuleMetrics struct {
	TotalRules      int
	EnabledRules    int
	Executions      int
	Failures        int
	AverageDuration float64
	LastExecution   *time.Time
	ExecutionsByType map[string]int
	FailuresByType   map[string]int
}

// ConditionEvaluator defines the interface for condition evaluation
type ConditionEvaluator interface {
	Evaluate(condition RuleCondition) (bool, error)
}

// ActionExecutor defines the interface for action execution
type ActionExecutor interface {
	Execute(action RuleAction) error
}
