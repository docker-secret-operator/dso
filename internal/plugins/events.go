package plugins

import (
	"time"
)

// Event represents a published event in the plugin system
type Event struct {
	Type          string      `json:"type"`
	Timestamp     time.Time   `json:"timestamp"`
	CorrelationID string      `json:"correlation_id"`
	Payload       interface{} `json:"payload"`
}

// Event type constants
const (
	// Execution events
	ExecutionStarted   = "execution.started"
	ExecutionCompleted = "execution.completed"
	ExecutionFailed    = "execution.failed"

	// Review events
	ReviewCreated  = "review.created"
	ReviewApproved = "review.approved"
	ReviewRejected = "review.rejected"

	// Alert events
	AlertTriggered = "alert.triggered"
	AlertResolved  = "alert.resolved"

	// Security events
	LoginSuccess       = "security.login_success"
	LoginFailure       = "security.login_failure"
	BruteForceDetected = "security.brute_force_detected"
	SessionExpired     = "security.session_expired"

	// Backup events
	BackupCreated  = "backup.created"
	BackupRestored = "backup.restored"

	// Plugin events
	PluginEnabled   = "plugin.enabled"
	PluginDisabled  = "plugin.disabled"
	PluginFailed    = "plugin.failed"
	PluginRecovered = "plugin.recovered"

	// Scheduler events
	JobStarted   = "job.started"
	JobCompleted = "job.completed"
	JobFailed    = "job.failed"
	JobPaused    = "job.paused"
	JobResumed   = "job.resumed"

	// Policy engine events
	RuleStarted   = "rule.started"
	RuleSucceeded = "rule.succeeded"
	RuleFailed    = "rule.failed"

	// Drift detection events
	DriftDetected      = "drift.detected"
	DriftAcknowledged  = "drift.acknowledged"
	DriftResolved      = "drift.resolved"
	DriftScanStarted   = "drift.scan_started"
	DriftScanCompleted = "drift.scan_completed"

	// Dependency graph events
	GraphUpdated         = "graph.updated"
	CycleDetected        = "graph.cycle_detected"
	CriticalNodeDetected = "graph.critical_node_detected"

	// Correlation engine events
	IncidentCreated  = "incident.created"
	IncidentUpdated  = "incident.updated"
	IncidentResolved = "incident.resolved"

	// Recommendation engine events
	RecommendationCreated    = "recommendation.created"
	RecommendationAcknowledged = "recommendation.acknowledged"
	RecommendationImplemented  = "recommendation.implemented"
	RecommendationDismissed    = "recommendation.dismissed"

	// Forecasting engine events
	ForecastCreated           = "forecast.created"
	ForecastUpdated           = "forecast.updated"
	CriticalForecastDetected  = "forecast.critical_detected"

	// Autonomy events
	AutonomousActionStarted   = "autonomy.action_started"
	AutonomousActionSucceeded = "autonomy.action_succeeded"
	AutonomousActionFailed    = "autonomy.action_failed"
	AutonomousActionRolledBack = "autonomy.action_rolled_back"
)

// Subscriber defines the interface for event subscribers
type Subscriber interface {
	Handle(event Event)
}

// SubscriberFunc is a function adapter for Subscriber
type SubscriberFunc func(event Event)

func (f SubscriberFunc) Handle(event Event) {
	f(event)
}
