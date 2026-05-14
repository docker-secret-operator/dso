package bootstrap

import (
	"context"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"os/user"
	"path/filepath"
)

// LocalBootstrapper handles local mode (development) bootstrap
type LocalBootstrapper struct {
	logger     Logger
	detector   *CloudDetector
	validator  *ConfigValidator
	prompter   *InteractivePrompter
	provCfg    *ProviderConfigHandler
	cfgBuilder *ConfigBuilder
	fsOps      *FilesystemOps
}

// NewLocalBootstrapper creates a new local bootstrapper
func NewLocalBootstrapper(logger Logger, opts *BootstrapOptions) *LocalBootstrapper {
	return &LocalBootstrapper{
		logger:     logger,
		detector:   NewCloudDetector(opts.Timeout, logger),
		validator:  NewConfigValidator(logger),
		prompter:   NewInteractivePrompter(logger),
		provCfg:    NewProviderConfigHandler(logger),
		cfgBuilder: NewConfigBuilder(),
		fsOps:      NewFilesystemOps(logger, opts.DryRun),
	}
}

// Bootstrap orchestrates the local mode bootstrap process
func (lb *LocalBootstrapper) Bootstrap(ctx context.Context, opts *BootstrapOptions) (*BootstrapResult, error) {
	lb.logger.Info("Starting local mode bootstrap")

	// Validate options
	if err := lb.validator.ValidateBootstrapOptions(opts); err != nil {
		return nil, err
	}

	// Get current user (no special checks for local mode)
	currentUser, err := user.Current()
	if err != nil {
		return nil, ErrUserValidation("bootstrap", "current")
	}
	lb.logger.Info("Current user", "username", currentUser.Username, "uid", currentUser.Uid)

	// Determine config path (default to user's home directory)
	homeDir := currentUser.HomeDir
	configPath := filepath.Join(homeDir, ".dso", "dso.yaml") // Default path

	// Safely retrieve config path from context if provided
	if opts.Context != nil {
		if val := opts.Context.Value("config_path"); val != nil {
			if path, ok := val.(string); ok && path != "" {
				configPath = path
				lb.logger.Debug("Using config path from context", "path", configPath)
			}
		}
	}

	lb.logger.Info("Local config path", "path", configPath)

	// Validate config path
	if err := lb.validator.ValidateConfigPath(configPath); err != nil {
		return nil, err
	}

	// Collect configuration from user
	builder, err := lb.collectConfiguration(ctx, opts)
	if err != nil {
		return nil, err
	}

	// Build configuration
	config, err := builder.Build()
	if err != nil {
		return nil, err
	}

	// Validate configuration
	if err := lb.validator.ValidateConfig(config); err != nil {
		return nil, err
	}

	// Generate YAML
	configYAML, err := builder.BuildYAML()
	if err != nil {
		return nil, err
	}

	// Validate YAML
	if err := lb.validator.ValidateYAML(configYAML); err != nil {
		return nil, err
	}

	// Create config directory
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return nil, ErrPathValidation("bootstrap", configDir, fmt.Sprintf("failed to create directory: %v", err))
	}

	// Write config file
	if err := os.WriteFile(configPath, configYAML, 0600); err != nil {
		return nil, ErrFileWrite("bootstrap", configPath, err)
	}

	lb.logger.Info("Configuration written", "path", configPath)

	// Display completion message
	if !opts.NonInteractive {
		_ = lb.prompter.DisplayCompletionMessage(configPath, ModeLocal)
	}

	return &BootstrapResult{
		ConfigPath:     configPath,
		PermissionsSet: false, // Local mode doesn't set system permissions
		Warnings: []string{
			fmt.Sprintf("Configuration stored in user home directory: %s", configPath),
			"This is for development/testing only",
			"For production, use agent mode with proper permissions",
		},
	}, nil
}

// collectConfiguration gathers configuration from user or arguments (local mode)
func (lb *LocalBootstrapper) collectConfiguration(ctx context.Context, opts *BootstrapOptions) (*ConfigBuilder, error) {
	builder := NewConfigBuilder().
		WithVersion("1.0").
		WithMode(ModeLocal)

	var provider string

	// Local mode doesn't detect cloud provider by default
	if opts.Provider != "" {
		provider = opts.Provider
	} else if !opts.NonInteractive {
		p, err := lb.prompter.PromptProviderSelection()
		if err != nil {
			return nil, ErrInteractivePrompt("bootstrap", err)
		}
		provider = p
	} else {
		return nil, ErrConfigValidation("bootstrap", "provider required in non-interactive mode")
	}

	// Configure provider-specific settings
	switch provider {
	case ProviderAWS:
		var region string
		if opts.AWSRegion != "" {
			region = opts.AWSRegion
		} else if !opts.NonInteractive {
			var err error
			region, err = lb.prompter.PromptAWSRegion()
			if err != nil {
				return nil, ErrInteractivePrompt("bootstrap", err)
			}
		} else {
			return nil, ErrConfigValidation("bootstrap", "AWS region required")
		}
		builder.WithAWSProvider("aws-dev", region)

	case ProviderAzure:
		var vaultURL string
		if opts.AzureVaultURL != "" {
			vaultURL = opts.AzureVaultURL
		} else if !opts.NonInteractive {
			var err error
			vaultURL, err = lb.prompter.PromptAzureVaultURL()
			if err != nil {
				return nil, ErrInteractivePrompt("bootstrap", err)
			}
		} else {
			return nil, ErrConfigValidation("bootstrap", "Azure Key Vault URL required")
		}
		builder.WithAzureProvider("azure-dev", vaultURL)

	case ProviderHuawei:
		var region, projectID string
		if opts.HuaweiRegion != "" && opts.HuaweiProjectID != "" {
			region = opts.HuaweiRegion
			projectID = opts.HuaweiProjectID
		} else if !opts.NonInteractive {
			var err error
			region, projectID, err = lb.prompter.PromptHuaweiRegionAndProject()
			if err != nil {
				return nil, ErrInteractivePrompt("bootstrap", err)
			}
		} else {
			return nil, ErrConfigValidation("bootstrap", "Huawei region and project ID required")
		}
		builder.WithHuaweiProvider("huawei-dev", region, projectID)

	case ProviderVault:
		var address string
		if opts.VaultAddress != "" {
			address = opts.VaultAddress
		} else if !opts.NonInteractive {
			var err error
			address, err = lb.prompter.PromptVaultAddress()
			if err != nil {
				return nil, ErrInteractivePrompt("bootstrap", err)
			}
		} else {
			return nil, ErrConfigValidation("bootstrap", "Vault address required")
		}
		builder.WithVaultProvider("vault-dev", address, "${VAULT_TOKEN}")
	}

	// Configure secrets
	if opts.Secrets != nil && len(opts.Secrets) > 0 {
		// Secrets provided via options
		for _, secret := range opts.Secrets {
			builder.WithSecret(secret.Name, secret.Provider, secret.Mappings)
		}
	} else if !opts.NonInteractive {
		// Ask user for secrets
		secrets, err := lb.prompter.PromptSecrets(provider)
		if err != nil {
			return nil, ErrInteractivePrompt("bootstrap", err)
		}

		for _, secret := range secrets {
			builder.WithSecret(secret.Name, provider, secret.Mappings)
		}
	}

	return builder, nil
}

// BootstrapNonInteractive performs non-interactive (script/automation) local bootstrap
func (lb *LocalBootstrapper) BootstrapNonInteractive(ctx context.Context, opts *BootstrapOptions) (*BootstrapResult, error) {
	lb.logger.Info("Starting non-interactive local bootstrap")

	opts.NonInteractive = true
	opts.Force = true

	return lb.Bootstrap(ctx, opts)
}

// GetBootstrapYAML returns the generated YAML for inspection
func (lb *LocalBootstrapper) GetBootstrapYAML(ctx context.Context, opts *BootstrapOptions) (string, error) {
	opts.NonInteractive = true
	builder, err := lb.collectConfiguration(ctx, opts)
	if err != nil {
		return "", err
	}

	return builder.BuildYAMLString()
}

// ValidateConfiguration validates a local configuration
func (lb *LocalBootstrapper) ValidateConfiguration(ctx context.Context, yamlContent []byte) error {
	var config Config
	if err := yaml.Unmarshal(yamlContent, &config); err != nil {
		return ErrConfigValidation("validation", fmt.Sprintf("YAML unmarshal failed: %v", err))
	}

	return lb.validator.ValidateConfig(&config)
}

// GetConfigurationDirectory returns the local configuration directory
func (lb *LocalBootstrapper) GetConfigurationDirectory() (string, error) {
	currentUser, err := user.Current()
	if err != nil {
		return "", ErrUserValidation("bootstrap", "current")
	}

	return filepath.Join(currentUser.HomeDir, ".dso"), nil
}

// ListConfigurations lists all available local configurations
func (lb *LocalBootstrapper) ListConfigurations() ([]string, error) {
	configDir, err := lb.GetConfigurationDirectory()
	if err != nil {
		return nil, err
	}

	// Check if directory exists
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		return []string{}, nil
	}

	// List yaml files in directory
	entries, err := os.ReadDir(configDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read config directory: %w", err)
	}

	var configs []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".yaml" {
			configs = append(configs, entry.Name())
		}
	}

	return configs, nil
}
