package bootstrap

import (
	"context"
	"fmt"
	"gopkg.in/yaml.v3"
)

// Bootstrapper is the public API for DSO bootstrap operations
type Bootstrapper interface {
	Bootstrap(ctx context.Context, opts *BootstrapOptions) (*BootstrapResult, error)
}

// NewBootstrapper creates a new bootstrapper based on mode
func NewBootstrapper(mode BootstrapMode, logger Logger, opts *BootstrapOptions) (Bootstrapper, error) {
	switch mode {
	case ModeLocal:
		return NewLocalBootstrapper(logger, opts), nil
	case ModeAgent:
		return NewAgentBootstrapper(logger, opts), nil
	default:
		return nil, fmt.Errorf("unknown bootstrap mode: %s", mode)
	}
}

// BootstrapManager is the main orchestrator for DSO bootstrap
type BootstrapManager struct {
	logger Logger
}

// NewBootstrapManager creates a new bootstrap manager
func NewBootstrapManager(logger Logger) *BootstrapManager {
	return &BootstrapManager{
		logger: logger,
	}
}

// Execute runs the bootstrap process based on options
func (bm *BootstrapManager) Execute(ctx context.Context, opts *BootstrapOptions) (*BootstrapResult, error) {
	// Set defaults
	if opts.Mode == "" {
		opts.Mode = ModeAgent
	}
	if opts.Context == nil {
		opts.Context = context.Background()
	}

	bm.logger.Info("Bootstrap starting",
		"mode", opts.Mode,
		"provider", opts.Provider,
		"non_interactive", opts.NonInteractive)

	// Create mode-specific bootstrapper
	bootstrapper, err := NewBootstrapper(opts.Mode, bm.logger, opts)
	if err != nil {
		return nil, err
	}

	// Execute bootstrap
	result, err := bootstrapper.Bootstrap(ctx, opts)
	if err != nil {
		bm.logger.Error("Bootstrap failed", "error", err.Error())
		return nil, err
	}

	bm.logger.Info("Bootstrap completed successfully")
	return result, nil
}

// ValidateConfiguration validates a DSO configuration
func (bm *BootstrapManager) ValidateConfiguration(ctx context.Context, yamlContent []byte) error {
	validator := NewConfigValidator(bm.logger)

	if err := validator.ValidateYAML(yamlContent); err != nil {
		return err
	}

	// Try to parse as config
	var config Config
	if err := UnmarshalConfig(yamlContent, &config); err != nil {
		return ErrConfigValidation("validation", fmt.Sprintf("failed to parse YAML: %v", err))
	}

	return validator.ValidateConfig(&config)
}

// GenerateYAML generates configuration YAML for given options
func (bm *BootstrapManager) GenerateYAML(ctx context.Context, opts *BootstrapOptions) ([]byte, error) {
	if opts.Mode == "" {
		opts.Mode = ModeAgent
	}
	if opts.Context == nil {
		opts.Context = ctx
	}

	bootstrapper, err := NewBootstrapper(opts.Mode, bm.logger, opts)
	if err != nil {
		return nil, err
	}

	// Different methods based on type
	switch b := bootstrapper.(type) {
	case *AgentBootstrapper:
		yamlStr, err := b.GetBootstrapYAML(ctx, opts)
		return []byte(yamlStr), err
	case *LocalBootstrapper:
		yamlStr, err := b.GetBootstrapYAML(ctx, opts)
		return []byte(yamlStr), err
	default:
		return nil, fmt.Errorf("unknown bootstrapper type")
	}
}

// GetCloudProvider detects the cloud provider of the current system
func (bm *BootstrapManager) GetCloudProvider(ctx context.Context, timeout int) (*CloudProviderInfo, error) {
	detector := NewCloudDetector(0, bm.logger)
	return detector.DetectCloudProvider(ctx)
}

// VerifySystemd checks if systemd is available
func (bm *BootstrapManager) VerifySystemd(ctx context.Context) error {
	manager := NewSystemdManager(bm.logger, false)
	return manager.VerifySystemd(ctx)
}

// GetSystemdVersion returns the systemd version
func (bm *BootstrapManager) GetSystemdVersion(ctx context.Context) (string, error) {
	manager := NewSystemdManager(bm.logger, false)
	return manager.GetSystemdVersion(ctx)
}

// PrintProviderComparison prints a provider comparison table
func (bm *BootstrapManager) PrintProviderComparison() string {
	handler := NewProviderConfigHandler(bm.logger)
	return handler.GetProviderComparisonTable()
}

// PrintSystemdHardening prints systemd hardening documentation
func (bm *BootstrapManager) PrintSystemdHardening() string {
	manager := NewSystemdManager(bm.logger, false)
	return manager.GetHardeningExplanation()
}

// PrintPermissionModel prints the permission model documentation
func (bm *BootstrapManager) PrintPermissionModel() string {
	manager := NewPermissionManager(bm.logger, false)
	return manager.DocumentPermissionModel()
}

// GetBootstrapOptions creates bootstrap options with sensible defaults
func GetBootstrapOptions(mode BootstrapMode, provider string) *BootstrapOptions {
	return &BootstrapOptions{
		Mode:           mode,
		Provider:       provider,
		NonInteractive: false,
		Force:          false,
		DryRun:         false,
		Timeout:        30 * 60, // 30 minutes
		Context:        context.Background(),
	}
}

// UnmarshalConfig unmarshals YAML into Config struct
func UnmarshalConfig(data []byte, config *Config) error {
	// Using gopkg.in/yaml.v3 for YAML parsing
	return UnmarshalYAML(data, config)
}

// UnmarshalYAML is a helper for YAML unmarshaling
func UnmarshalYAML(data []byte, v interface{}) error {
	if len(data) == 0 {
		return fmt.Errorf("cannot unmarshal empty YAML")
	}

	if err := yaml.Unmarshal(data, v); err != nil {
		return fmt.Errorf("YAML unmarshal error: %w", err)
	}

	return nil
}

// BootstrapPhase represents different bootstrap phases for progress tracking
type BootstrapPhase string

const (
	PhaseValidation           BootstrapPhase = "validation"
	PhaseDetection            BootstrapPhase = "detection"
	PhaseConfiguration        BootstrapPhase = "configuration"
	PhaseYAMLGeneration       BootstrapPhase = "yaml_generation"
	PhaseTransactionSetup     BootstrapPhase = "transaction_setup"
	PhaseTransactionExecution BootstrapPhase = "transaction_execution"
	PhaseCompletion           BootstrapPhase = "completion"
)

// ProgressCallback is called to report bootstrap progress
type ProgressCallback func(phase BootstrapPhase, message string, progress int)

// BootstrapWithProgress executes bootstrap with progress reporting
func (bm *BootstrapManager) BootstrapWithProgress(ctx context.Context, opts *BootstrapOptions, callback ProgressCallback) (*BootstrapResult, error) {
	if callback == nil {
		callback = func(phase BootstrapPhase, message string, progress int) {}
	}

	// Phase 1: Validation
	callback(PhaseValidation, "Validating bootstrap options", 10)
	if err := NewConfigValidator(bm.logger).ValidateBootstrapOptions(opts); err != nil {
		return nil, err
	}

	// Phase 2: Detection (for cloud mode) - detect once and reuse
	if opts.Mode == ModeAgent {
		callback(PhaseDetection, "Detecting cloud provider", 20)
		detector := NewCloudDetector(0, bm.logger)
		cloudInfo, err := detector.DetectCloudProvider(ctx)
		if err != nil {
			bm.logger.Warn("Cloud detection failed, continuing", "error", err.Error())
		}
		// Store detected cloud info in options to avoid duplicate detection
		if opts.CloudInfo == nil && cloudInfo != nil {
			opts.CloudInfo = cloudInfo
		}
	}

	// Phase 3-6: Actual bootstrap
	callback(PhaseConfiguration, "Collecting configuration", 30)
	callback(PhaseYAMLGeneration, "Generating YAML configuration", 50)

	if opts.Mode == ModeAgent {
		callback(PhaseTransactionSetup, "Setting up bootstrap transaction", 70)
	}

	callback(PhaseTransactionExecution, "Executing bootstrap", 80)

	// Execute
	result, err := bm.Execute(ctx, opts)
	if err != nil {
		return nil, err
	}

	callback(PhaseCompletion, "Bootstrap completed successfully", 100)
	return result, nil
}

// GetBootstrapReport returns a detailed report of bootstrap configuration
func GetBootstrapReport(config *Config) string {
	report := fmt.Sprintf(`
DSO Bootstrap Configuration Report
===================================

Version: %s
Runtime Mode: %s
Log Level: %s

Providers (%d):
`, config.Version, config.Runtime.Mode, config.Runtime.LogLevel, len(config.Providers))

	for name, prov := range config.Providers {
		report += fmt.Sprintf("  - %s (%s)\n", name, prov.Type)
		if prov.Region != "" {
			report += fmt.Sprintf("    Region: %s\n", prov.Region)
		}
		if prov.Config != nil {
			for key, value := range prov.Config {
				report += fmt.Sprintf("    %s: %s\n", key, value)
			}
		}
	}

	report += fmt.Sprintf("\nSecrets (%d):\n", len(config.Secrets))
	for _, secret := range config.Secrets {
		report += fmt.Sprintf("  - %s (provider: %s)\n", secret.Name, secret.Provider)
		for source, target := range secret.Mappings {
			report += fmt.Sprintf("    %s -> %s\n", source, target)
		}
	}

	return report
}
