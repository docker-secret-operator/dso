package setup

import (
	"os"
	"time"
)

// SetupMode represents the DSO deployment mode.
type SetupMode string

const (
	ModeLocal SetupMode = "local"
	ModeAgent SetupMode = "agent"
)

// TransactionStatus tracks the lifecycle of a setup transaction.
type TransactionStatus string

const (
	StatusPending    TransactionStatus = "pending"
	StatusRunning    TransactionStatus = "running"
	StatusCompleted  TransactionStatus = "completed"
	StatusFailed     TransactionStatus = "failed"
	StatusRolledBack TransactionStatus = "rolled_back"
)

// ─── Install Plan ────────────────────────────────────────────────────────────

// InstallPlan is the immutable blueprint generated before any filesystem
// changes are made. Every stage — preview, apply, rollback — consumes this
// struct. Nothing touches disk until the plan is approved and executed.
type InstallPlan struct {
	ID        string
	Timestamp time.Time
	Mode      SetupMode
	Provider  string // "aws", "vault", "azure", "local"

	Directories []DirectoryChange
	Files       []FileChange
	Permissions []PermissionChange
	Services    []ServiceChange
	Groups      []GroupChange

	// ConfigYAML is the rendered dso.yaml content that will be written.
	ConfigYAML string

	DryRun      bool
	ResumeToken string            // non-empty when resuming a prior run
	Metadata    map[string]string
}

// FileChange describes a file create or modify operation.
type FileChange struct {
	Path      string
	Content   []byte
	Mode      os.FileMode
	Owner     string // e.g. "root:dso"
	Operation string // "create" | "modify"
}

// DirectoryChange describes a directory create operation.
type DirectoryChange struct {
	Path      string
	Mode      os.FileMode
	Owner     string
	Operation string
}

// PermissionChange describes a chmod/chown operation.
type PermissionChange struct {
	Path    string
	Current os.FileMode
	Target  os.FileMode
	Owner   string
}

// ServiceChange describes a systemd unit operation.
type ServiceChange struct {
	Name      string // e.g. "dso-agent.service"
	Content   []byte // unit file content
	Operation string // "create" | "enable" | "start"
}

// GroupChange describes a Unix group create or membership operation.
type GroupChange struct {
	Name      string
	Operation string // "create"
	Users     []string
}

// ─── Environment ─────────────────────────────────────────────────────────────

// Environment holds facts gathered during detection. Detection never fails;
// it collects whatever is knowable and leaves the rest empty.
type Environment struct {
	OS               string // "linux" | "darwin"
	IsRoot           bool
	CurrentUser      string
	DockerVersion    string // empty if Docker not found
	SystemdAvailable bool

	// AvailableProviders lists providers whose credentials were detected.
	AvailableProviders []string
	// DetectedProvider is the first (highest-priority) detected provider.
	DetectedProvider string

	// Per-provider metadata (populated when detected).
	AWSRegion    string
	AWSIdentity  string // arn:aws:iam::...
	AzureSubID   string
	VaultAddress string

	// Recommendations derived from the detected environment.
	RecommendedMode     SetupMode
	RecommendedProvider string

	// ExistingConfig is the path to an existing dso.yaml, or empty.
	ExistingConfig string
	// ExistingDSOVersion is the version string from an existing installation.
	ExistingDSOVersion string

	Timestamp time.Time
	Metadata  map[string]interface{}
}

// ─── Validation ──────────────────────────────────────────────────────────────

// ValidationResult summarises whether the detected environment is usable.
type ValidationResult struct {
	Valid    bool
	Errors   []ValidationError   // must-fix issues; setup cannot proceed
	Warnings []ValidationWarning // nice-to-fix; setup can proceed
}

// ValidationError describes a blocking problem and how to recover from it.
type ValidationError struct {
	Code     string   // machine-readable, e.g. "docker_unavailable"
	Message  string   // human-readable summary
	Recovery []string // ordered steps the user can take to fix it
}

// ValidationWarning describes a non-blocking concern.
type ValidationWarning struct {
	Code    string
	Message string
}

// ─── Options & Result ────────────────────────────────────────────────────────

// SetupOptions controls how the engine runs Setup.
type SetupOptions struct {
	Mode     SetupMode
	Provider string
	DryRun   bool

	// Interactive controls whether the engine may prompt the user.
	Interactive bool
	// Approve skips the preview confirmation prompt.
	Approve bool
	// Advanced exposes extra configuration questions.
	Advanced bool

	// NonRoot enables non-root access configuration (agent mode only).
	NonRoot bool
	// AutoDetect forces cloud provider auto-detection.
	AutoDetect bool

	// Resume continues a previously interrupted transaction.
	Resume string

	// Overrides are applied on top of planner defaults (for scripting).
	Overrides map[string]interface{}
}

// SetupResult is returned by Engine.Setup.
type SetupResult struct {
	Plan        *InstallPlan
	Transaction *Transaction
	Status      string // "success" | "failed" | "pending" | "rolled_back"
	Duration    time.Duration
}

// ─── Transaction ─────────────────────────────────────────────────────────────

// Transaction records every operation performed during apply so that rollback
// and resume are deterministic.
type Transaction struct {
	ID          string
	PlanID      string
	Operations  []TxOperation
	Checkpoints []Checkpoint
	Status      TransactionStatus
	StartTime   time.Time
	EndTime     time.Time
}

// TxOperation is a single tracked step within a transaction.
type TxOperation struct {
	Sequence   int
	Type       string      // "file_create" | "chmod" | "service_start" etc.
	Target     string      // affected path or resource name
	Before     interface{} // state before the change (for rollback)
	After      interface{} // state after the change
	Reversible bool
	Error      error
}

// Checkpoint is a named save-point within a transaction.
type Checkpoint struct {
	Name           string
	OperationCount int // number of operations committed at this point
	Timestamp      time.Time
}
