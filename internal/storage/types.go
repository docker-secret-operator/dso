package storage

import (
	"context"
	"time"
)

// Draft represents a configuration draft
type Draft struct {
	ID            string     `db:"id"`
	WorkspaceID   string     `db:"workspace_id"`
	OwnerID       string     `db:"owner_id"`
	Title         string     `db:"title"`
	Description   string     `db:"description"`
	Status        string     `db:"status"` // draft, under_review, approved, rejected, archived
	VersionNumber int        `db:"version_number"`
	Config        string     `db:"config"` // JSON-encoded configuration
	Checksum      string     `db:"checksum"`
	CreatedAt     time.Time  `db:"created_at"`
	ModifiedAt    time.Time  `db:"modified_at"`
	ExpiresAt     *time.Time `db:"expires_at"`
}

// DraftVersion represents a historical version of a draft
type DraftVersion struct {
	ID            string    `db:"id"`
	DraftID       string    `db:"draft_id"`
	VersionNumber int       `db:"version_number"`
	Config        string    `db:"config"` // JSON-encoded
	Checksum      string    `db:"checksum"`
	CreatedAt     time.Time `db:"created_at"`
}

// Review represents a configuration review
type Review struct {
	ID                 string    `db:"id"`
	DraftID            string    `db:"draft_id"`
	CreatedAt          time.Time `db:"created_at"`
	CreatedBy          string    `db:"created_by"`
	ModifiedAt         time.Time `db:"modified_at"`
	Status             string    `db:"status"`          // draft, under_review, approved, rejected, expired
	Checklist          string    `db:"checklist"`       // JSON-encoded
	RiskAssessment     string    `db:"risk_assessment"` // JSON-encoded
	RequiredApprovals  int       `db:"required_approvals"`
	ApprovalTimeoutHrs *int      `db:"approval_timeout_hours"`
	Title              string    `db:"title"`
	Description        string    `db:"description"`
}

// Approval represents an approval decision on a review
type Approval struct {
	ID               string     `db:"id"`
	ReviewID         string     `db:"review_id"`
	ReviewerID       string     `db:"reviewer_id"`
	ReviewerName     string     `db:"reviewer_name"`
	Decision         string     `db:"decision"` // pending, approved, rejected, abstained
	Comments         *string    `db:"comments"`
	RejectionReason  *string    `db:"rejection_reason"`
	ApprovalSequence int        `db:"approval_sequence"`
	IsRequired       bool       `db:"is_required"`
	CreatedAt        time.Time  `db:"created_at"`
	DecidedAt        *time.Time `db:"decided_at"`
}

// ReviewActivity represents a timeline entry for a review
type ReviewActivity struct {
	ID          string    `db:"id"`
	ReviewID    string    `db:"review_id"`
	Type        string    `db:"type"` // review_created, validation_performed, approval_given, etc.
	ActorID     string    `db:"actor_id"`
	Description string    `db:"description"`
	Metadata    *string   `db:"metadata"` // JSON-encoded
	Timestamp   time.Time `db:"timestamp"`
}

// Snapshot represents a saved configuration snapshot
type Snapshot struct {
	ID          string    `db:"id"`
	DraftID     string    `db:"draft_id"`
	Config      string    `db:"config"` // JSON-encoded
	Checksum    string    `db:"checksum"`
	Source      string    `db:"source"` // automated, manual, pre_execution
	SourceID    *string   `db:"source_id"`
	Description *string   `db:"description"`
	Tags        *string   `db:"tags"` // JSON-encoded array
	Verified    bool      `db:"verified"`
	Applied     bool      `db:"applied"`
	CreatedAt   time.Time `db:"created_at"`
}

// AuditEvent represents an immutable audit log entry
type AuditEvent struct {
	ID             string     `db:"id"`
	Timestamp      time.Time  `db:"timestamp"`
	ActorID        string     `db:"actor_id"`
	ActorName      string     `db:"actor_name"`
	ActorEmail     *string    `db:"actor_email"`
	Action         string     `db:"action"` // draft.created, review.approved, etc.
	Resource       string     `db:"resource"`
	ResourceID     string     `db:"resource_id"`
	ResourceType   string     `db:"resource_type"`
	Status         string     `db:"status"` // success, failure
	ResultCode     *string    `db:"result_code"`
	ResultMessage  *string    `db:"result_message"`
	OldValue       *string    `db:"old_value"` // JSON-encoded
	NewValue       *string    `db:"new_value"` // JSON-encoded
	Delta          *string    `db:"delta"`     // JSON-encoded
	CorrelationID  string     `db:"correlation_id"`
	RequestID      string     `db:"request_id"`
	IPAddress      *string    `db:"ip_address"`
	UserAgent      *string    `db:"user_agent"`
	Severity       string     `db:"severity"` // info, warning, error, critical
	RetentionUntil *time.Time `db:"retention_until"`
}

// Migration represents a schema migration
type Migration struct {
	Version   string    `db:"version"`
	Name      string    `db:"name"`
	AppliedAt time.Time `db:"applied_at"`
}

// ExecutionRequest represents a request to execute an approved workflow
type ExecutionRequest struct {
	ID            string     `db:"id"`
	CorrelationID string     `db:"correlation_id"`
	DraftID       string     `db:"draft_id"`
	ReviewID      string     `db:"review_id"`
	ApprovalID    string     `db:"approval_id"`
	PlanID        *string    `db:"plan_id"`
	Status        string     `db:"status"` // pending, validated, planned, rejected, expired
	CreatedAt     time.Time  `db:"created_at"`
	ValidatedAt   *time.Time `db:"validated_at"`
	ExpiresAt     time.Time  `db:"expires_at"`
	RequestedBy   string     `db:"requested_by"`
	Version       int        `db:"version"`
}

// ExecutionPlan represents a detailed execution plan
type ExecutionPlan struct {
	ID                string     `db:"id"`
	ExecutionID       string     `db:"execution_id"`
	CorrelationID     string     `db:"correlation_id"`
	ApprovalID        string     `db:"approval_id"`
	DraftID           string     `db:"draft_id"`
	Status            string     `db:"status"` // draft, validated, ready
	TotalSteps        int        `db:"total_steps"`
	EstimatedDuration int        `db:"estimated_duration_seconds"`
	RiskScore         int        `db:"risk_score"`
	AffectedResources string     `db:"affected_resources"` // JSON
	RollbackAvailable bool       `db:"rollback_available"`
	CreatedAt         time.Time  `db:"created_at"`
	ValidatedAt       *time.Time `db:"validated_at"`
	Version           int        `db:"version"`
}

// ExecutionStep represents an individual step in an execution plan
type ExecutionStep struct {
	ID                string    `db:"id"`
	PlanID            string    `db:"plan_id"`
	Sequence          int       `db:"sequence"`
	Name              string    `db:"name"`
	Description       *string   `db:"description"`
	Action            string    `db:"action"`
	EstimatedTime     int       `db:"estimated_time_seconds"`
	RiskLevel         string    `db:"risk_level"` // low, medium, high
	RollbackAvailable bool      `db:"rollback_available"`
	Payload           *string   `db:"payload"` // JSON
	CreatedAt         time.Time `db:"created_at"`
	Version           int       `db:"version"`
}

// Store interfaces define the contract for persistence operations

// DraftStore handles draft persistence
type DraftStore interface {
	Create(ctx context.Context, draft *Draft) error
	Update(ctx context.Context, draft *Draft) error
	GetByID(ctx context.Context, id string) (*Draft, error)
	List(ctx context.Context, ownerID string) ([]*Draft, error)
	Delete(ctx context.Context, id string) error

	// Versions
	SaveVersion(ctx context.Context, version *DraftVersion) error
	GetVersions(ctx context.Context, draftID string) ([]*DraftVersion, error)
}

// ReviewStore handles review persistence
type ReviewStore interface {
	Create(ctx context.Context, review *Review) error
	Update(ctx context.Context, review *Review) error
	GetByID(ctx context.Context, id string) (*Review, error)
	GetByDraftID(ctx context.Context, draftID string) (*Review, error)
	List(ctx context.Context) ([]*Review, error)
}

// ApprovalStore handles approval persistence
type ApprovalStore interface {
	Create(ctx context.Context, approval *Approval) error
	Update(ctx context.Context, approval *Approval) error
	GetByID(ctx context.Context, id string) (*Approval, error)
	ListForReview(ctx context.Context, reviewID string) ([]*Approval, error)
	ListPendingForReviewer(ctx context.Context, reviewerID string) ([]*Approval, error)
}

// ReviewActivityStore handles review activity logging
type ReviewActivityStore interface {
	Log(ctx context.Context, activity *ReviewActivity) error
	ListForReview(ctx context.Context, reviewID string) ([]*ReviewActivity, error)
}

// SnapshotStore handles snapshot persistence
type SnapshotStore interface {
	Create(ctx context.Context, snapshot *Snapshot) error
	GetByID(ctx context.Context, id string) (*Snapshot, error)
	ListForDraft(ctx context.Context, draftID string) ([]*Snapshot, error)
	Delete(ctx context.Context, id string) error
}

// AuditStore handles audit log persistence (append-only)
type AuditStore interface {
	Log(ctx context.Context, event *AuditEvent) error
	Query(ctx context.Context, filters map[string]interface{}) ([]*AuditEvent, error)
	Export(ctx context.Context, startTime, endTime time.Time) ([]*AuditEvent, error)
	// No Update or Delete methods - audit logs are immutable
}

// ExecutionRequestStore handles execution request persistence
type ExecutionRequestStore interface {
	Create(ctx context.Context, req *ExecutionRequest) error
	Update(ctx context.Context, req *ExecutionRequest) error
	GetByID(ctx context.Context, id string) (*ExecutionRequest, error)
	GetByCorrelationID(ctx context.Context, correlationID string) (*ExecutionRequest, error)
	ListByStatus(ctx context.Context, status string) ([]*ExecutionRequest, error)
	ListByApproval(ctx context.Context, approvalID string) ([]*ExecutionRequest, error)
	List(ctx context.Context, limit int, offset int) ([]*ExecutionRequest, error)
}

// ExecutionPlanStore handles execution plan persistence
type ExecutionPlanStore interface {
	Create(ctx context.Context, plan *ExecutionPlan) error
	Update(ctx context.Context, plan *ExecutionPlan) error
	GetByID(ctx context.Context, id string) (*ExecutionPlan, error)
	GetByExecutionID(ctx context.Context, executionID string) (*ExecutionPlan, error)
	ListByStatus(ctx context.Context, status string) ([]*ExecutionPlan, error)
	List(ctx context.Context, limit int, offset int) ([]*ExecutionPlan, error)
}

// ExecutionStepStore handles execution step persistence
type ExecutionStepStore interface {
	Create(ctx context.Context, step *ExecutionStep) error
	GetByID(ctx context.Context, id string) (*ExecutionStep, error)
	ListByPlan(ctx context.Context, planID string) ([]*ExecutionStep, error)
	CreateBatch(ctx context.Context, steps []*ExecutionStep) error
}

// ExecutionResult represents the result of a completed execution
type ExecutionResult struct {
	ID            string    `db:"id"`
	ExecutionID   string    `db:"execution_id"`
	CorrelationID string    `db:"correlation_id"`
	WorkerID      *string   `db:"worker_id"`
	Status        string    `db:"status"` // completed, failed, cancelled
	Error         *string   `db:"error"`
	Duration      int       `db:"duration_seconds"`
	CompletedAt   time.Time `db:"completed_at"`
	Version       int       `db:"version"`
}

// StepResult represents the result of a step
type StepResult struct {
	ID            string     `db:"id"`
	StepID        string     `db:"step_id"`
	ExecutionID   string     `db:"execution_id"`
	CorrelationID string     `db:"correlation_id"`
	Status        string     `db:"status"` // completed, failed, cancelled
	Duration      int        `db:"duration_seconds"`
	Output        *string    `db:"output"` // JSON
	Error         *string    `db:"error"`
	StartedAt     time.Time  `db:"started_at"`
	CompletedAt   *time.Time `db:"completed_at"`
	Version       int        `db:"version"`
}

// WorkerHeartbeat represents a worker health check
type WorkerHeartbeat struct {
	ID             string    `db:"id"`
	WorkerID       string    `db:"worker_id"`
	Timestamp      time.Time `db:"timestamp"`
	State          string    `db:"state"` // healthy, unhealthy, etc
	RunningSteps   int       `db:"running_steps"`
	CompletedCount int       `db:"completed_count"`
	FailedCount    int       `db:"failed_count"`
	LastError      *string   `db:"last_error"`
	SystemLoad     float64   `db:"system_load"`
	MemoryUsage    int64     `db:"memory_usage"`
	Version        int       `db:"version"`
}

// ExecutionResultStore handles execution result persistence
type ExecutionResultStore interface {
	Create(ctx context.Context, result *ExecutionResult) error
	GetByID(ctx context.Context, id string) (*ExecutionResult, error)
	GetByExecutionID(ctx context.Context, executionID string) (*ExecutionResult, error)
	ListByStatus(ctx context.Context, status string, limit int, offset int) ([]*ExecutionResult, error)
	List(ctx context.Context, limit int, offset int) ([]*ExecutionResult, error)
}

// StepResultStore handles step result persistence
type StepResultStore interface {
	Create(ctx context.Context, result *StepResult) error
	GetByID(ctx context.Context, id string) (*StepResult, error)
	ListByExecution(ctx context.Context, executionID string) ([]*StepResult, error)
	ListByStep(ctx context.Context, stepID string) ([]*StepResult, error)
	CreateBatch(ctx context.Context, results []*StepResult) error
}

// WorkerHeartbeatStore handles worker heartbeat persistence
type WorkerHeartbeatStore interface {
	Create(ctx context.Context, heartbeat *WorkerHeartbeat) error
	GetByID(ctx context.Context, id string) (*WorkerHeartbeat, error)
	ListByWorker(ctx context.Context, workerID string, limit int) ([]*WorkerHeartbeat, error)
	GetLatestByWorker(ctx context.Context, workerID string) (*WorkerHeartbeat, error)
}

// User represents a system user for authentication and RBAC
type User struct {
	ID                 string     `db:"id"`
	Username           string     `db:"username"`
	PasswordHash       string     `db:"password_hash"`
	DisplayName        string     `db:"display_name"`
	Role               string     `db:"role"` // viewer, operator, reviewer, approver, admin
	Disabled           bool       `db:"disabled"`
	FailedLoginCount   int        `db:"failed_login_count"`
	LockedUntil        *time.Time `db:"locked_until"`
	PasswordChangedAt  *time.Time `db:"password_changed_at"`
	PasswordExpiresAt  *time.Time `db:"password_expires_at"`
	MustChangePassword bool       `db:"must_change_password"`
	CreatedAt          time.Time  `db:"created_at"`
	UpdatedAt          time.Time  `db:"updated_at"`
}

// Session represents an authenticated user session
type Session struct {
	ID           string    `db:"id"`
	UserID       string    `db:"user_id"`
	TokenHash    string    `db:"token_hash"`
	IPAddress    string    `db:"ip_address"`
	UserAgent    string    `db:"user_agent"`
	CreatedAt    time.Time `db:"created_at"`
	ExpiresAt    time.Time `db:"expires_at"`
	LastActivity time.Time `db:"last_activity"`
}

// UserStore handles user persistence
type UserStore interface {
	Create(ctx context.Context, user *User) error
	Update(ctx context.Context, user *User) error
	GetByID(ctx context.Context, id string) (*User, error)
	GetByUsername(ctx context.Context, username string) (*User, error)
	List(ctx context.Context) ([]*User, error)
	Delete(ctx context.Context, id string) error
}

// SessionStore handles session persistence
type SessionStore interface {
	Create(ctx context.Context, session *Session) error
	GetByID(ctx context.Context, id string) (*Session, error)
	GetByTokenHash(ctx context.Context, tokenHash string) (*Session, error)
	ListByUserID(ctx context.Context, userID string) ([]*Session, error)
	ListAll(ctx context.Context) ([]*Session, error)
	UpdateLastActivity(ctx context.Context, sessionID string) error
	ExtendSession(ctx context.Context, sessionID string, newExpiry time.Time) error
	DeleteExpired(ctx context.Context) error
	Delete(ctx context.Context, sessionID string) error
	DeleteAllByUserID(ctx context.Context, userID string) error
}

// SecurityEvent represents a security-related event (login, lockout, password reset, etc.)
type SecurityEvent struct {
	ID        string     `db:"id"`
	Type      string     `db:"type"`      // LOGIN_SUCCESS, LOGIN_FAILURE, ACCOUNT_LOCKED, etc.
	Severity  string     `db:"severity"`  // low, medium, high, critical
	Username  string     `db:"username"`
	UserID    *string    `db:"user_id"`
	IPAddress string     `db:"ip_address"`
	UserAgent *string    `db:"user_agent"`
	Message   string     `db:"message"`
	Metadata  *string    `db:"metadata"` // JSON-encoded additional data
	CreatedAt time.Time  `db:"created_at"`
}

// SuspiciousActivity represents detected suspicious activity patterns
type SuspiciousActivity struct {
	ID            string     `db:"id"`
	Type          string     `db:"type"`      // brute_force, credential_stuffing, session_anomaly, etc.
	Severity      string     `db:"severity"`  // low, medium, high, critical
	IPAddress     *string    `db:"ip_address"`
	Usernames     *string    `db:"usernames"` // JSON array of affected usernames
	FirstSeen     time.Time  `db:"first_seen"`
	LastSeen      time.Time  `db:"last_seen"`
	OccurrenceCount int       `db:"occurrence_count"`
	Message       string     `db:"message"`
	Metadata      *string    `db:"metadata"` // JSON-encoded additional data
	AcknowledgedBy *string   `db:"acknowledged_by"`
	AcknowledgedAt *time.Time `db:"acknowledged_at"`
	IgnoredAt     *time.Time `db:"ignored_at"`
	CreatedAt     time.Time  `db:"created_at"`
}

// SecurityAlert represents a security alert that requires action
type SecurityAlert struct {
	ID             string     `db:"id"`
	Type           string     `db:"type"` // brute_force, credential_stuffing, session_anomaly, etc.
	Severity       string     `db:"severity"`
	State          string     `db:"state"` // active, acknowledged, resolved
	Title          string     `db:"title"`
	Message        string     `db:"message"`
	AffectedUser   *string    `db:"affected_user"`
	IPAddress      *string    `db:"ip_address"`
	Details        *string    `db:"details"` // JSON-encoded
	AcknowledgedBy *string    `db:"acknowledged_by"`
	AcknowledgedAt *time.Time `db:"acknowledged_at"`
	ResolvedAt     *time.Time `db:"resolved_at"`
	CreatedAt      time.Time  `db:"created_at"`
}

// SecurityEventStore handles security event persistence
type SecurityEventStore interface {
	Log(ctx context.Context, event *SecurityEvent) error
	GetByID(ctx context.Context, id string) (*SecurityEvent, error)
	Query(ctx context.Context, filters map[string]interface{}) ([]*SecurityEvent, error)
	List(ctx context.Context, limit int, offset int) ([]*SecurityEvent, error)
}

// SuspiciousActivityStore handles suspicious activity detection persistence
type SuspiciousActivityStore interface {
	Create(ctx context.Context, activity *SuspiciousActivity) error
	Update(ctx context.Context, activity *SuspiciousActivity) error
	GetByID(ctx context.Context, id string) (*SuspiciousActivity, error)
	List(ctx context.Context, limit int, offset int) ([]*SuspiciousActivity, error)
	ListUnacknowledged(ctx context.Context) ([]*SuspiciousActivity, error)
}

// SecurityAlertStore handles security alert persistence
type SecurityAlertStore interface {
	Create(ctx context.Context, alert *SecurityAlert) error
	Update(ctx context.Context, alert *SecurityAlert) error
	GetByID(ctx context.Context, id string) (*SecurityAlert, error)
	List(ctx context.Context, limit int, offset int) ([]*SecurityAlert, error)
	ListByState(ctx context.Context, state string) ([]*SecurityAlert, error)
}

// AlertRule represents an alert rule for metric-based alerting
type AlertRule struct {
	ID          string     `db:"id"`
	Name        string     `db:"name"`
	Description *string    `db:"description"`
	Enabled     bool       `db:"enabled"`
	Severity    string     `db:"severity"` // low, medium, high, critical
	Metric      string     `db:"metric"`   // queue_depth, failure_rate, memory_usage, etc.
	Operator    string     `db:"operator"` // >, <, >=, <=, ==, !=
	Threshold   float64    `db:"threshold"`
	Duration    int        `db:"duration_seconds"` // how long condition must be true
	Cooldown    int        `db:"cooldown_seconds"` // minimum time between alerts
	CreatedAt   time.Time  `db:"created_at"`
	UpdatedAt   time.Time  `db:"updated_at"`
	IsBuiltin   bool       `db:"is_builtin"` // cannot be deleted
}

// Alert represents an active alert
type Alert struct {
	ID              string     `db:"id"`
	RuleID          string     `db:"rule_id"`
	State           string     `db:"state"` // active, acknowledged, resolved, suppressed
	Severity        string     `db:"severity"`
	Metric          string     `db:"metric"`
	Message         string     `db:"message"`
	Value           float64    `db:"value"`
	Threshold       float64    `db:"threshold"`
	AcknowledgedBy  *string    `db:"acknowledged_by"`
	AcknowledgedAt  *time.Time `db:"acknowledged_at"`
	ResolvedBy      *string    `db:"resolved_by"`
	ResolvedAt      *time.Time `db:"resolved_at"`
	SuppressedBy    *string    `db:"suppressed_by"`
	SuppressedUntil *time.Time `db:"suppressed_until"`
	LastFiredAt     time.Time  `db:"last_fired_at"`
	CreatedAt       time.Time  `db:"created_at"`
}

// AlertRuleStore handles alert rule persistence
type AlertRuleStore interface {
	Create(ctx context.Context, rule *AlertRule) error
	Update(ctx context.Context, rule *AlertRule) error
	GetByID(ctx context.Context, id string) (*AlertRule, error)
	List(ctx context.Context, limit int, offset int) ([]*AlertRule, error)
	ListEnabled(ctx context.Context) ([]*AlertRule, error)
	Delete(ctx context.Context, id string) error
}

// AlertStore handles alert persistence
type AlertStore interface {
	Create(ctx context.Context, alert *Alert) error
	Update(ctx context.Context, alert *Alert) error
	GetByID(ctx context.Context, id string) (*Alert, error)
	List(ctx context.Context, limit int, offset int) ([]*Alert, error)
	ListByState(ctx context.Context, state string, limit int, offset int) ([]*Alert, error)
	ListByRuleID(ctx context.Context, ruleID string) ([]*Alert, error)
	GetActiveByRuleID(ctx context.Context, ruleID string) (*Alert, error)
}

// Backup represents a database backup
type Backup struct {
	ID          string     `db:"id"`
	Filename    string     `db:"filename"`
	SizeBytes   int64      `db:"size_bytes"`
	Checksum    string     `db:"checksum"`
	BackupType  string     `db:"backup_type"` // manual, scheduled
	Status      string     `db:"status"`      // running, completed, failed
	DurationMs  int        `db:"duration_ms"`
	ErrorMsg    *string    `db:"error_msg"`
	CreatedAt   time.Time  `db:"created_at"`
	CompletedAt *time.Time `db:"completed_at"`
}

// BackupStore handles backup metadata persistence
type BackupStore interface {
	Create(ctx context.Context, backup *Backup) error
	Update(ctx context.Context, backup *Backup) error
	GetByID(ctx context.Context, id string) (*Backup, error)
	List(ctx context.Context, limit int, offset int) ([]*Backup, error)
	Delete(ctx context.Context, id string) error
	ListCompleted(ctx context.Context) ([]*Backup, error)
}

// Plugin represents a registered plugin with metadata
type Plugin struct {
	ID             string     `db:"id"`
	Name           string     `db:"name"`
	Version        string     `db:"version"`
	Type           string     `db:"type"`
	Enabled        bool       `db:"enabled"`
	Status         string     `db:"status"`
	Health         string     `db:"health"`
	ErrorMessage   *string    `db:"error_message"`
	LoadedAt       *time.Time `db:"loaded_at"`
	EnabledAt      *time.Time `db:"enabled_at"`
	DisabledAt     *time.Time `db:"disabled_at"`
	RestartCount   int        `db:"restart_count"`
	EventCount     int        `db:"event_count"`
	LastErrorTime  *time.Time `db:"last_error_time"`
	LastHeartbeat  *time.Time `db:"last_heartbeat"`
}

// PluginConfig holds plugin configuration
type PluginConfig struct {
	PluginID  string `db:"plugin_id"`
	ConfigJSON string `db:"config_json"`
	UpdatedAt time.Time `db:"updated_at"`
}

// PluginEvent holds plugin event logs
type PluginEvent struct {
	ID        int       `db:"id"`
	PluginID  string    `db:"plugin_id"`
	Level     string    `db:"level"` // info, warn, error, debug
	Message   string    `db:"message"`
	CreatedAt time.Time `db:"created_at"`
}

// PluginStore handles plugin metadata persistence
type PluginStore interface {
	Create(ctx context.Context, plugin *Plugin) error
	Update(ctx context.Context, plugin *Plugin) error
	GetByID(ctx context.Context, id string) (*Plugin, error)
	List(ctx context.Context) ([]*Plugin, error)

	// Config management
	SaveConfig(ctx context.Context, config *PluginConfig) error
	GetConfig(ctx context.Context, pluginID string) (*PluginConfig, error)

	// Event logging
	LogEvent(ctx context.Context, event *PluginEvent) error
	GetEvents(ctx context.Context, pluginID string, limit int) ([]*PluginEvent, error)
}

// SchedulerJob represents a scheduled job
type SchedulerJob struct {
	ID           string
	Name         string
	Type         string
	Enabled      bool
	Schedule     string
	IntervalSecs int
	NextRun      time.Time
	LastRun      *time.Time
	RetryCount   int
	MaxRetries   int
	TimeoutSecs  int
	Status       string
	MetadataJSON string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// SchedulerExecution tracks job execution
type SchedulerExecution struct {
	ID           string
	JobID        string
	Success      bool
	DurationMs   *int64
	ErrorMessage *string
	CreatedAt    time.Time
}

// SchedulerStore handles job persistence
type SchedulerStore interface {
	CreateJob(ctx context.Context, job *SchedulerJob) error
	UpdateJob(ctx context.Context, job *SchedulerJob) error
	GetJob(ctx context.Context, id string) (*SchedulerJob, error)
	ListJobs(ctx context.Context) ([]*SchedulerJob, error)
	DeleteJob(ctx context.Context, id string) error
}

// ExecutionStore handles execution history
type ExecutionStore interface {
	LogExecution(ctx context.Context, exec *SchedulerExecution) error
	GetExecutions(ctx context.Context, jobID string, limit int) ([]*SchedulerExecution, error)
	CleanupOldExecutions(ctx context.Context, olderThan time.Time) error
}

// RuleStore handles policy rule persistence
// Note: This is a generic interface. SQLite implementation works with policy.Rule
type RuleStore interface {
	CreateRule(ctx context.Context, rule interface{}) error
	UpdateRule(ctx context.Context, rule interface{}) error
	GetRule(ctx context.Context, id string) (interface{}, error)
	ListRules(ctx context.Context) ([]interface{}, error)
	DeleteRule(ctx context.Context, id string) error
}

// DriftStore handles drift detection persistence
type DriftStore interface {
	CreateFinding(ctx context.Context, finding interface{}) error
	UpdateFinding(ctx context.Context, finding interface{}) error
	GetFinding(ctx context.Context, id string) (interface{}, error)
	ListFindings(ctx context.Context) ([]interface{}, error)
	DeleteFinding(ctx context.Context, id string) error
}

// StorageProvider is the root abstraction for all storage operations
type StorageProvider interface {
	// Store accessors
	Drafts() DraftStore
	Reviews() ReviewStore
	Approvals() ApprovalStore
	ReviewActivities() ReviewActivityStore
	Snapshots() SnapshotStore
	Audit() AuditStore
	ExecutionRequests() ExecutionRequestStore
	ExecutionPlans() ExecutionPlanStore
	ExecutionSteps() ExecutionStepStore
	ExecutionResults() ExecutionResultStore
	StepResults() StepResultStore
	WorkerHeartbeats() WorkerHeartbeatStore
	Users() UserStore
	Sessions() SessionStore
	SecurityEvents() SecurityEventStore
	SuspiciousActivities() SuspiciousActivityStore
	SecurityAlerts() SecurityAlertStore
	AlertRules() AlertRuleStore
	Alerts() AlertStore
	Backups() BackupStore
	Plugins() PluginStore
	IntegrationConfigs() IntegrationConfigStore
	IntegrationDeliveries() IntegrationDeliveryStore
	SchedulerJobs() SchedulerStore
	SchedulerExecutions() ExecutionStore
	Rules() RuleStore
	Drift() DriftStore

	// Lifecycle
	Health(ctx context.Context) error
	Close(ctx context.Context) error

	// Transactions
	BeginTx(ctx context.Context) (Transaction, error)
}

// Transaction represents a database transaction
type Transaction interface {
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error

	// Access stores within transaction context
	Drafts() DraftStore
	Reviews() ReviewStore
	Approvals() ApprovalStore
	ReviewActivities() ReviewActivityStore
	Snapshots() SnapshotStore
	Audit() AuditStore
	ExecutionRequests() ExecutionRequestStore
	ExecutionPlans() ExecutionPlanStore
	ExecutionSteps() ExecutionStepStore
	ExecutionResults() ExecutionResultStore
	StepResults() StepResultStore
	WorkerHeartbeats() WorkerHeartbeatStore
	Users() UserStore
	Sessions() SessionStore
	SecurityEvents() SecurityEventStore
	SuspiciousActivities() SuspiciousActivityStore
	SecurityAlerts() SecurityAlertStore
	AlertRules() AlertRuleStore
	Alerts() AlertStore
	Backups() BackupStore
	Plugins() PluginStore

	// Accessors for Phase 5.11 integration framework
	IntegrationConfigs() IntegrationConfigStore
	IntegrationDeliveries() IntegrationDeliveryStore

	// Accessors for Phase 5.12 scheduler
	SchedulerJobs() SchedulerStore
	SchedulerExecutions() ExecutionStore

	// Accessors for Phase 5.13 policy engine
	Rules() RuleStore

	// Accessors for Phase 5.14 drift detection
	Drift() DriftStore
}

// IntegrationConfig holds integration configuration
type IntegrationConfig struct {
	PluginID        string `db:"plugin_id"`
	Enabled         bool   `db:"enabled"`
	Endpoint        string `db:"endpoint"`
	AuthType        string `db:"auth_type"`
	AuthConfigJSON  string `db:"auth_config_json"`
	FiltersJSON     string `db:"filters_json"`
	RetryPolicyJSON string `db:"retry_policy_json"`
	UpdatedAt       time.Time `db:"updated_at"`
}

// IntegrationConfigStore handles integration configuration persistence
type IntegrationConfigStore interface {
	SaveConfig(ctx context.Context, config *IntegrationConfig) error
	GetConfig(ctx context.Context, pluginID string) (*IntegrationConfig, error)
	ListConfigs(ctx context.Context) ([]*IntegrationConfig, error)
	DeleteConfig(ctx context.Context, pluginID string) error
}

// IntegrationDelivery tracks a delivery attempt
type IntegrationDelivery struct {
	ID           string     `db:"id"`
	PluginID     string     `db:"plugin_id"`
	EventType    string     `db:"event_type"`
	EventID      string     `db:"event_id"`
	Success      bool       `db:"success"`
	ResponseCode int        `db:"response_code"`
	ErrorMessage *string    `db:"error_message"`
	Attempt      int        `db:"attempt"`
	CreatedAt    time.Time  `db:"created_at"`
}

// IntegrationDeliveryStore handles delivery history persistence
type IntegrationDeliveryStore interface {
	LogDelivery(ctx context.Context, delivery *IntegrationDelivery) error
	GetDeliveries(ctx context.Context, pluginID string, limit int) ([]*IntegrationDelivery, error)
	GetDeliveriesByEvent(ctx context.Context, eventID string) ([]*IntegrationDelivery, error)
	CleanupOldDeliveries(ctx context.Context, olderThan time.Time) error
}
