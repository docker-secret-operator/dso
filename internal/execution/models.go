package execution

import (
	"time"
)

// ExecutionRequest represents a request to execute an approved workflow
type ExecutionRequest struct {
	ID            string     // Unique execution request ID
	DraftID       string     // Reference to draft being executed
	ReviewID      string     // Reference to review
	ApprovalID    string     // Reference to approval authorization
	CorrelationID string     // End-to-end request tracing
	Status        string     // pending, validated, planned, rejected, expired
	CreatedAt     time.Time  // When request created
	ValidatedAt   *time.Time // When validated
	PlanID        string     // Reference to generated plan
	ExpiresAt     time.Time  // When request expires (approval TTL)
	RequestedBy   string     // Actor who requested execution
	Version       int64      // Optimistic lock version
}

// ExecutionPlan represents the generated dry-run execution plan
type ExecutionPlan struct {
	ID                string           // Unique plan ID
	ExecutionID       string           // Reference to ExecutionRequest
	ApprovalID        string           // Authorization reference
	DraftID           string           // Draft being planned
	CorrelationID     string           // Tracing
	Status            string           // draft, validated, ready
	Steps             []*ExecutionStep // Ordered steps
	TotalSteps        int              // Total step count
	EstimatedDuration time.Duration    // Estimated execution time
	RiskScore         int              // 0-100, higher = riskier
	AffectedResources []string         // Resources that will change
	RollbackAvailable bool             // Whether rollback is available
	CreatedAt         time.Time        // When plan created
	ValidatedAt       *time.Time       // When validated
	Version           int64            // Optimistic lock version
}

// ExecutionStep represents a single step in the execution plan
type ExecutionStep struct {
	ID                string            // Unique step ID
	Sequence          int               // Execution order
	Name              string            // Step name
	Description       string            // Step description
	Action            string            // Action type (deploy, restart, etc)
	Payload           map[string]string // Step-specific parameters
	RollbackAvailable bool              // Whether rollback is available for this step
	EstimatedTime     time.Duration     // Estimated execution time
	RiskLevel         string            // low, medium, high
}

// ExecutionResult represents the outcome of validation and planning
type ExecutionResult struct {
	ExecutionID string     // Reference to ExecutionRequest
	PlanID      string     // Reference to ExecutionPlan
	Passed      bool       // Overall validation result
	Warnings    []string   // Non-blocking warnings
	Errors      []string   // Blocking errors
	Score       int        // 0-100 readiness score
	ReadyAt     *time.Time // When ready for execution
}

// ValidationReport contains detailed validation results
type ValidationReport struct {
	ApprovalValid     bool
	ApprovalMessage   string
	GovernanceValid   bool
	GovernanceMessage string
	VersionValid      bool
	VersionMessage    string
	SafetyValid       bool
	SafetyMessage     string
	AllValid          bool
}

// ExecutionReadiness represents overall readiness state
type ExecutionReadiness struct {
	ID               string
	ExecutionID      string
	Ready            bool
	Score            int
	ValidationIssues []string
	CheckedAt        time.Time
	ExpiresAt        time.Time
}
