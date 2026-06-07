package storage

import (
	"context"
	"time"
)

// Draft represents a configuration draft
type Draft struct {
	ID            string    `db:"id"`
	WorkspaceID   string    `db:"workspace_id"`
	OwnerID       string    `db:"owner_id"`
	Title         string    `db:"title"`
	Description   string    `db:"description"`
	Status        string    `db:"status"` // draft, under_review, approved, rejected, archived
	VersionNumber int       `db:"version_number"`
	Config        string    `db:"config"` // JSON-encoded configuration
	Checksum      string    `db:"checksum"`
	CreatedAt     time.Time `db:"created_at"`
	ModifiedAt    time.Time `db:"modified_at"`
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
	Status             string    `db:"status"` // draft, under_review, approved, rejected, expired
	Checklist          string    `db:"checklist"` // JSON-encoded
	RiskAssessment     string    `db:"risk_assessment"` // JSON-encoded
	RequiredApprovals  int       `db:"required_approvals"`
	ApprovalTimeoutHrs *int      `db:"approval_timeout_hours"`
	Title              string    `db:"title"`
	Description        string    `db:"description"`
}

// Approval represents an approval decision on a review
type Approval struct {
	ID            string    `db:"id"`
	ReviewID      string    `db:"review_id"`
	ReviewerID    string    `db:"reviewer_id"`
	ReviewerName  string    `db:"reviewer_name"`
	Decision      string    `db:"decision"` // pending, approved, rejected, abstained
	Comments      *string   `db:"comments"`
	RejectionReason *string `db:"rejection_reason"`
	ApprovalSequence int    `db:"approval_sequence"`
	IsRequired    bool      `db:"is_required"`
	CreatedAt     time.Time `db:"created_at"`
	DecidedAt     *time.Time `db:"decided_at"`
}

// ReviewActivity represents a timeline entry for a review
type ReviewActivity struct {
	ID          string                 `db:"id"`
	ReviewID    string                 `db:"review_id"`
	Type        string                 `db:"type"` // review_created, validation_performed, approval_given, etc.
	ActorID     string                 `db:"actor_id"`
	Description string                 `db:"description"`
	Metadata    *string                `db:"metadata"` // JSON-encoded
	Timestamp   time.Time              `db:"timestamp"`
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
	ID             string                 `db:"id"`
	Timestamp      time.Time              `db:"timestamp"`
	ActorID        string                 `db:"actor_id"`
	ActorName      string                 `db:"actor_name"`
	ActorEmail     *string                `db:"actor_email"`
	Action         string                 `db:"action"` // draft.created, review.approved, etc.
	Resource       string                 `db:"resource"`
	ResourceID     string                 `db:"resource_id"`
	ResourceType   string                 `db:"resource_type"`
	Status         string                 `db:"status"` // success, failure
	ResultCode     *string                `db:"result_code"`
	ResultMessage  *string                `db:"result_message"`
	OldValue       *string                `db:"old_value"` // JSON-encoded
	NewValue       *string                `db:"new_value"` // JSON-encoded
	Delta          *string                `db:"delta"` // JSON-encoded
	CorrelationID  string                 `db:"correlation_id"`
	RequestID      string                 `db:"request_id"`
	IPAddress      *string                `db:"ip_address"`
	UserAgent      *string                `db:"user_agent"`
	Severity       string                 `db:"severity"` // info, warning, error, critical
	RetentionUntil *time.Time             `db:"retention_until"`
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
	ID                  string        `db:"id"`
	ExecutionID         string        `db:"execution_id"`
	CorrelationID       string        `db:"correlation_id"`
	ApprovalID          string        `db:"approval_id"`
	DraftID             string        `db:"draft_id"`
	Status              string        `db:"status"` // draft, validated, ready
	TotalSteps          int           `db:"total_steps"`
	EstimatedDuration   int           `db:"estimated_duration_seconds"`
	RiskScore           int           `db:"risk_score"`
	AffectedResources   string        `db:"affected_resources"` // JSON
	RollbackAvailable   bool          `db:"rollback_available"`
	CreatedAt           time.Time     `db:"created_at"`
	ValidatedAt         *time.Time    `db:"validated_at"`
	Version             int           `db:"version"`
}

// ExecutionStep represents an individual step in an execution plan
type ExecutionStep struct {
	ID                string        `db:"id"`
	PlanID            string        `db:"plan_id"`
	Sequence          int           `db:"sequence"`
	Name              string        `db:"name"`
	Description       *string       `db:"description"`
	Action            string        `db:"action"`
	EstimatedTime     int           `db:"estimated_time_seconds"`
	RiskLevel         string        `db:"risk_level"` // low, medium, high
	RollbackAvailable bool          `db:"rollback_available"`
	Payload           *string       `db:"payload"` // JSON
	CreatedAt         time.Time     `db:"created_at"`
	Version           int           `db:"version"`
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
	ID             string        `db:"id"`
	ExecutionID    string        `db:"execution_id"`
	CorrelationID  string        `db:"correlation_id"`
	WorkerID       *string       `db:"worker_id"`
	Status         string        `db:"status"` // completed, failed, cancelled
	Error          *string       `db:"error"`
	Duration       int           `db:"duration_seconds"`
	CompletedAt    time.Time     `db:"completed_at"`
	Version        int           `db:"version"`
}

// StepResult represents the result of a step
type StepResult struct {
	ID             string        `db:"id"`
	StepID         string        `db:"step_id"`
	ExecutionID    string        `db:"execution_id"`
	CorrelationID  string        `db:"correlation_id"`
	Status         string        `db:"status"` // completed, failed, cancelled
	Duration       int           `db:"duration_seconds"`
	Output         *string       `db:"output"` // JSON
	Error          *string       `db:"error"`
	StartedAt      time.Time     `db:"started_at"`
	CompletedAt    *time.Time    `db:"completed_at"`
	Version        int           `db:"version"`
}

// WorkerHeartbeat represents a worker health check
type WorkerHeartbeat struct {
	ID             string        `db:"id"`
	WorkerID       string        `db:"worker_id"`
	Timestamp      time.Time     `db:"timestamp"`
	State          string        `db:"state"` // healthy, unhealthy, etc
	RunningSteps   int           `db:"running_steps"`
	CompletedCount int           `db:"completed_count"`
	FailedCount    int           `db:"failed_count"`
	LastError      *string       `db:"last_error"`
	SystemLoad     float64       `db:"system_load"`
	MemoryUsage    int64         `db:"memory_usage"`
	Version        int           `db:"version"`
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
	ID           string    `db:"id"`
	Username     string    `db:"username"`
	PasswordHash string    `db:"password_hash"`
	DisplayName  string    `db:"display_name"`
	Role         string    `db:"role"` // viewer, operator, reviewer, approver, admin
	Disabled     bool      `db:"disabled"`
	CreatedAt    time.Time `db:"created_at"`
	UpdatedAt    time.Time `db:"updated_at"`
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
	UpdateLastActivity(ctx context.Context, sessionID string) error
	DeleteExpired(ctx context.Context) error
	Delete(ctx context.Context, sessionID string) error
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
}
