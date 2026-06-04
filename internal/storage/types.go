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

// StorageProvider is the root abstraction for all storage operations
type StorageProvider interface {
	// Store accessors
	Drafts() DraftStore
	Reviews() ReviewStore
	Approvals() ApprovalStore
	ReviewActivities() ReviewActivityStore
	Snapshots() SnapshotStore
	Audit() AuditStore

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
}
