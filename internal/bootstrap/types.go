package bootstrap

import (
	"context"
	"time"
)

// BootstrapMode represents the mode of DSO operation
type BootstrapMode string

const (
	ModeLocal BootstrapMode = "local"
	ModeAgent BootstrapMode = "agent"
)

// Provider type constants
const (
	ProviderAWS    = "aws"
	ProviderAzure  = "azure"
	ProviderHuawei = "huawei"
	ProviderVault  = "vault"
)

// BootstrapOptions configures the bootstrap process
type BootstrapOptions struct {
	// Core settings
	Mode           BootstrapMode
	Provider       string
	NonInteractive bool
	Force          bool
	DryRun         bool

	// Provider-specific
	AWSRegion       string
	AzureVaultURL   string
	HuaweiRegion    string
	HuaweiProjectID string
	VaultAddress    string

	// Secrets
	Secrets []SecretDefinition

	// Permissions
	OwnerUserID  int // Current user UID (auto-detected if 0)
	OwnerGroupID int // Current group GID (auto-detected if 0)

	// Non-root access (agent mode only)
	EnableNonRootAccess bool // Automatically configure user for non-root access

	// Timing
	Timeout time.Duration
	Context context.Context

	// Cached cloud detection result (to avoid duplicate detection)
	CloudInfo *CloudProviderInfo
}

// SecretDefinition represents a secret to be injected
type SecretDefinition struct {
	Name     string
	Provider string
	Mappings map[string]string
}

// BootstrapResult contains the outcome of bootstrap
type BootstrapResult struct {
	ConfigPath     string
	ServicePath    string
	PermissionsSet bool
	Warnings       []string
	DryRunOutput   string
}

// ProviderConfig represents provider-specific configuration
type ProviderConfig struct {
	Type   string            `yaml:"type"`
	Region string            `yaml:"region,omitempty"`
	Config map[string]string `yaml:"config,omitempty"`
	Auth   *AuthConfig       `yaml:"auth,omitempty"`
	Retry  *RetryConfig      `yaml:"retry,omitempty"`
}

// AuthConfig defines authentication method
type AuthConfig struct {
	Method string            `yaml:"method"`
	Params map[string]string `yaml:"params,omitempty"`
}

// RetryConfig defines retry behavior
type RetryConfig struct {
	Attempts int    `yaml:"attempts"`
	Backoff  string `yaml:"backoff"`
}

// RuntimeConfig defines DSO runtime settings
type RuntimeConfig struct {
	Mode     string `yaml:"mode"`
	LogLevel string `yaml:"log_level"`
}

// DefaultConfig defines defaults for all secrets
type DefaultConfig struct {
	Inject   *InjectionConfig `yaml:"inject,omitempty"`
	Rotation *RotationConfig  `yaml:"rotation,omitempty"`
}

// InjectionConfig defines how secrets are injected
type InjectionConfig struct {
	Type string `yaml:"type"` // "env" or "file"
	Path string `yaml:"path,omitempty"`
}

// RotationConfig defines secret rotation
type RotationConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Strategy string `yaml:"strategy"`
	Timeout  string `yaml:"timeout,omitempty"`
}

// AgentConfig defines agent-specific settings
type AgentConfig struct {
	Cache    bool           `yaml:"cache"`
	Watch    WatchConfig    `yaml:"watch"`
	Rotation RotationConfig `yaml:"rotation,omitempty"`
}

// WatchConfig defines secret watching behavior
type WatchConfig struct {
	PollingInterval string `yaml:"polling_interval"`
	DebounceWindow  string `yaml:"debounce_window,omitempty"`
}

// SecretMapping represents a secret in the config
type SecretMapping struct {
	Name     string            `yaml:"name"`
	Provider string            `yaml:"provider"`
	Rotation RotationConfig    `yaml:"rotation,omitempty"`
	Mappings map[string]string `yaml:"mappings"`
}

// Config represents the complete DSO configuration
type Config struct {
	Version   string                    `yaml:"version"`
	Runtime   RuntimeConfig             `yaml:"runtime"`
	Providers map[string]ProviderConfig `yaml:"providers"`
	Defaults  *DefaultConfig            `yaml:"defaults,omitempty"`
	Agent     *AgentConfig              `yaml:"agent,omitempty"`
	Secrets   []SecretMapping           `yaml:"secrets,omitempty"`
}

// CloudProviderInfo contains detected cloud provider information
type CloudProviderInfo struct {
	Provider  string
	Detected  bool
	Metadata  map[string]string
	Timestamp time.Time
}

// BootstrapLock represents a bootstrap lock
type BootstrapLock struct {
	Path       string
	AcquiredAt time.Time
	owner      string
}

// BootstrapTransaction represents a sequence of bootstrap operations
type BootstrapTransaction struct {
	operations []Operation
	rollbacks  []RollbackFunc
}

// Operation represents a single bootstrap step
type Operation interface {
	Name() string
	Execute(context.Context) error
}

// RollbackFunc rolls back a completed operation
type RollbackFunc func(context.Context) error

// DockerInfo contains Docker daemon information
type DockerInfo struct {
	Version string
	OS      string
	Arch    string
	Runtime string
}

// SystemdInfo contains systemd information
type SystemdInfo struct {
	Available bool
	HasPID1   bool
	Sockaddr  string
	Version   string
}

// UserInfo contains user identity information
type UserInfo struct {
	UID      int
	GID      int
	Username string
	Home     string
	Groups   []string
}

// PathValidationError indicates a path validation failure
type PathValidationError struct {
	Path   string
	Reason string
}

func (e *PathValidationError) Error() string {
	return "path validation failed: " + e.Reason + " (" + e.Path + ")"
}
