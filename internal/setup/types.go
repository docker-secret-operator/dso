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
// ID is a stable, sequential identifier (e.g. "FILE-001") used in preview
// output, logs, rollback references, and debug traces.
type FileChange struct {
	ID        string // e.g. "FILE-001"
	Path      string
	Content   []byte
	Mode      os.FileMode
	Owner     string // e.g. "root:dso"
	Operation string // "create" | "modify"
}

// DirectoryChange describes a directory create operation.
type DirectoryChange struct {
	ID        string // e.g. "DIR-001"
	Path      string
	Mode      os.FileMode
	Owner     string
	Operation string
}

// PermissionChange describes a chmod/chown operation.
type PermissionChange struct {
	ID      string // e.g. "PERM-001"
	Path    string
	Current os.FileMode
	Target  os.FileMode
	Owner   string
}

// ServiceChange describes a systemd unit operation.
type ServiceChange struct {
	ID        string // e.g. "SERVICE-001"
	Name      string // e.g. "dso-agent.service"
	Content   []byte // unit file content
	Operation string // "create" | "enable" | "start"
}

// GroupChange describes a Unix group create or membership operation.
type GroupChange struct {
	ID        string // e.g. "GROUP-001"
	Name      string
	Operation string // "create" | "add-member"
	Users     []string
}

// ─── Environment sub-types ───────────────────────────────────────────────────

// OSInfo contains facts about the host operating system.
type OSInfo struct {
	GOOS         string // "linux" | "darwin" | ...
	Architecture string // "amd64" | "arm64"
	Distribution string // e.g. "ubuntu" (Linux only; empty otherwise)
	Version      string // OS version string, e.g. "22.04"
}

// UserInfo contains facts about the process user.
type UserInfo struct {
	Username string
	UID      string
	GID      string
	HomeDir  string
	IsRoot   bool // true when UID == "0"
}

// DockerInfo contains facts about the Docker installation.
type DockerInfo struct {
	BinaryFound     bool
	BinaryPath      string
	SocketFound     bool   // true when at least one socket path was successfully stat'd
	SocketPath      string // first socket path stat'd successfully; empty if none
	DaemonReachable bool   // true when "docker version" returned a server version
	Version         string // server version; empty if daemon unreachable
}

// SystemdInfo contains facts about the systemd service manager.
type SystemdInfo struct {
	Available  bool
	BinaryPath string
	Version    string // e.g. "252"; empty if not found
}

// AWSInfo contains AWS credential-detection results.
type AWSInfo struct {
	Detected       bool
	Region         string
	HasStaticCreds bool // AWS_ACCESS_KEY_ID + AWS_SECRET_ACCESS_KEY
	HasSharedCreds bool // ~/.aws/credentials file exists
	HasRole        bool // AWS_ROLE_ARN or AWS_WEB_IDENTITY_TOKEN_FILE
}

// AzureInfo contains Azure credential-detection results.
type AzureInfo struct {
	Detected    bool
	HasEnvCreds bool // AZURE_CLIENT_ID + AZURE_CLIENT_SECRET + AZURE_TENANT_ID
	HasCLI      bool // az CLI binary found
}

// VaultInfo contains HashiCorp Vault detection results.
type VaultInfo struct {
	Detected bool
	Address  string // VAULT_ADDR env var
	HasToken bool   // VAULT_TOKEN env var
	HasRole  bool   // VAULT_ROLE_ID env var
}

// DetectedProviders summarises which secret providers were found.
type DetectedProviders struct {
	// Available is ordered by detection priority; always includes "local".
	Available []string
	AWS       AWSInfo
	Azure     AzureInfo
	Vault     VaultInfo
}

// ExistingDSOInfo describes a prior DSO installation, if any.
// This structure is designed to support upgrade, repair, and migration flows.
type ExistingDSOInfo struct {
	Installed        bool   // true when any part of DSO is found
	ConfigPath       string // e.g. /etc/dso/dso.yaml or ~/.dso/dso.yaml
	AgentInstalled   bool   // true when the dso binary is found in PATH
	ServiceInstalled bool   // true when the systemd unit file exists
	Version          string // version string; empty if undetectable
}

// DetectionWarning records a non-fatal problem that occurred during detection.
// The detector continues collecting facts even when a warning is generated.
type DetectionWarning struct {
	Code    string // machine-readable, e.g. "os_release_read_failed"
	Message string // human-readable explanation
}

// Capabilities summarises what this environment can support. The detector
// computes these once from raw facts so later phases never re-derive them.
type Capabilities struct {
	SupportsSystemd   bool // systemctl binary is present
	SupportsDocker    bool // docker binary and daemon are reachable
	SupportsAgentMode bool // systemd + root — required for agent mode
	SupportsLocalMode bool // always true; local mode has no system requirements
}

// ─── Environment ─────────────────────────────────────────────────────────────

// Environment holds facts gathered during detection. Detection never fails;
// absence of a credential or binary is recorded as false/empty, not an error.
// Treat this struct as stable after Phase 2 — later phases consume it, not modify it.
type Environment struct {
	OS          OSInfo
	User        UserInfo
	Docker      DockerInfo
	Systemd     SystemdInfo
	Providers   DetectedProviders
	ExistingDSO ExistingDSOInfo

	// Capabilities are derived once from raw facts. Later phases use these
	// rather than re-inspecting raw fields like Docker.DaemonReachable.
	Capabilities Capabilities

	// DetectionWarnings holds non-fatal problems encountered during detection.
	// The validator (Phase 3) may promote these to errors when appropriate.
	DetectionWarnings []DetectionWarning

	Timestamp time.Time
}

// ─── Validation ──────────────────────────────────────────────────────────────

// ValidationSeverity classifies a validation finding.
type ValidationSeverity string

const (
	SeverityError   ValidationSeverity = "error"
	SeverityWarning ValidationSeverity = "warning"
	SeverityInfo    ValidationSeverity = "info"
)

// ValidationCategory groups related findings for Doctor and Repair routing.
type ValidationCategory string

const (
	CategoryDocker        ValidationCategory = "docker"
	CategoryPermissions   ValidationCategory = "permissions"
	CategoryProvider      ValidationCategory = "provider"
	CategorySecurity      ValidationCategory = "security"
	CategoryFilesystem    ValidationCategory = "filesystem"
	CategoryConfiguration ValidationCategory = "configuration"
)

// Validation codes — machine-readable identifiers reused by Doctor, Repair, UI,
// and documentation. Use these constants; never scatter string literals.
const (
	// Docker
	CodeDockerNotInstalled       = "docker_not_installed"
	CodeDockerDaemonNotRunning   = "docker_daemon_not_running"
	CodeDockerDaemonUnreachable  = "docker_daemon_unreachable"
	CodeDockerSocketInaccessible = "docker_socket_inaccessible"

	// Permissions
	CodeAgentModeRequiresRoot    = "agent_mode_requires_root"
	CodeAgentModeRequiresSystemd = "agent_mode_requires_systemd"

	// Provider
	CodeUnknownProvider         = "unknown_provider"
	CodeNoCloudProviderDetected = "no_cloud_provider_detected"
	CodeAWSCredentialsMissing   = "aws_credentials_missing"
	CodeAWSConnectivityFailed   = "aws_connectivity_failed"
	CodeVaultCredentialsMissing = "vault_credentials_missing"
	CodeVaultConnectivityFailed = "vault_connectivity_failed"
	CodeAzureCredentialsMissing = "azure_credentials_missing"
	CodeAzureConnectivityFailed = "azure_connectivity_failed"

	// Existing installation
	CodeExistingInstallationFound = "existing_installation_found"
	CodeServiceWithoutAgent       = "service_without_agent"
	CodeAgentWithoutService       = "agent_without_service"
)

// ValidationIssue is a single finding from the validator. A unified type with
// a Severity field (rather than separate Errors/Warnings/Suggestions slices)
// lets Doctor and Repair route by Category without unwrapping multiple collections.
type ValidationIssue struct {
	Severity  ValidationSeverity
	Category  ValidationCategory
	Code      string   // one of the Code* constants above
	Message   string   // human-readable explanation
	Recovery  []string // ordered fix steps; populated for SeverityError only
}

// ValidationResult summarises the validator's findings.
// Treat this as a stable API after Phase 3 — later phases consume it, not modify it.
type ValidationResult struct {
	Valid  bool
	Issues []ValidationIssue
}

// Errors returns all issues with SeverityError.
func (r *ValidationResult) Errors() []ValidationIssue {
	return r.filterBySeverity(SeverityError)
}

// Warnings returns all issues with SeverityWarning.
func (r *ValidationResult) Warnings() []ValidationIssue {
	return r.filterBySeverity(SeverityWarning)
}

// Info returns all issues with SeverityInfo.
func (r *ValidationResult) Info() []ValidationIssue {
	return r.filterBySeverity(SeverityInfo)
}

// InCategory returns all issues belonging to the given category.
func (r *ValidationResult) InCategory(cat ValidationCategory) []ValidationIssue {
	var out []ValidationIssue
	for _, i := range r.Issues {
		if i.Category == cat {
			out = append(out, i)
		}
	}
	return out
}

func (r *ValidationResult) filterBySeverity(s ValidationSeverity) []ValidationIssue {
	var out []ValidationIssue
	for _, i := range r.Issues {
		if i.Severity == s {
			out = append(out, i)
		}
	}
	return out
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
