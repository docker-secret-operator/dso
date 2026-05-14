package bootstrap

import (
	"context"
	"fmt"
	"gopkg.in/yaml.v3"
)

// AgentBootstrapper handles agent mode (cloud/production) bootstrap
type AgentBootstrapper struct {
	logger     Logger
	detector   *CloudDetector
	validator  *ConfigValidator
	prompter   *InteractivePrompter
	provCfg    *ProviderConfigHandler
	cfgBuilder *ConfigBuilder
	fsOps      *FilesystemOps
	svc        *SystemdManager
	perm       *PermissionManager
}

// NewAgentBootstrapper creates a new agent bootstrapper
func NewAgentBootstrapper(logger Logger, opts *BootstrapOptions) *AgentBootstrapper {
	return &AgentBootstrapper{
		logger:     logger,
		detector:   NewCloudDetector(opts.Timeout, logger),
		validator:  NewConfigValidator(logger),
		prompter:   NewInteractivePrompter(logger),
		provCfg:    NewProviderConfigHandler(logger),
		cfgBuilder: NewConfigBuilder(),
		fsOps:      NewFilesystemOps(logger, opts.DryRun),
		svc:        NewSystemdManager(logger, opts.DryRun),
		perm:       NewPermissionManager(logger, opts.DryRun),
	}
}

// Bootstrap orchestrates the agent mode bootstrap process
func (ab *AgentBootstrapper) Bootstrap(ctx context.Context, opts *BootstrapOptions) (*BootstrapResult, error) {
	ab.logger.Info("Starting agent mode bootstrap")

	// Step 1: Validate options
	if err := ab.validator.ValidateBootstrapOptions(opts); err != nil {
		return nil, err
	}

	// Step 2: Get current user info
	currentUser, err := ab.validator.GetCurrentUser()
	if err != nil {
		return nil, err
	}
	ab.logger.Info("Current user", "username", currentUser.Username, "uid", currentUser.UID, "gid", currentUser.GID)

	// Step 3: Detect cloud provider
	cloudInfo, err := ab.detector.DetectCloudProvider(ctx)
	if err != nil {
		return nil, err
	}

	// Step 4: Collect configuration from user
	builder, err := ab.collectConfiguration(ctx, opts, cloudInfo)
	if err != nil {
		return nil, err
	}

	// Build configuration
	config, err := builder.Build()
	if err != nil {
		return nil, err
	}

	// Validate final configuration
	if err := ab.validator.ValidateConfig(config); err != nil {
		return nil, err
	}

	// Step 5: Generate YAML
	configYAML, err := builder.BuildYAML()
	if err != nil {
		return nil, err
	}

	// Validate YAML
	if err := ab.validator.ValidateYAML(configYAML); err != nil {
		return nil, err
	}

	// Step 6: Validate directories can be created
	if err := ab.validator.ValidateDirectories(ctx, ab.fsOps); err != nil {
		return nil, err
	}

	// Step 7: Verify systemd is available
	if err := ab.svc.VerifySystemd(ctx); err != nil {
		return nil, err
	}

	// Step 8: Build and execute transaction
	// Safely retrieve config path from options or context, with fallback
	configPath := "/etc/dso/dso.yaml" // Default path

	// Check if context has config_path set (defensive check)
	if opts.Context != nil {
		if val := opts.Context.Value("config_path"); val != nil {
			if path, ok := val.(string); ok && path != "" {
				configPath = path
			}
		}
	}

	ab.logger.Debug("Using config path", "path", configPath)

	// Determine DSO group ID (placeholder - would be looked up or created)
	dsoGID := 1001 // Typical DSO group ID

	tx := BuildBootstrapTransaction(ab.logger, ab.fsOps, ab.svc, ab.perm,
		configPath, configYAML, currentUser.UID, currentUser.GID, dsoGID)

	if err := tx.Execute(ctx); err != nil {
		return nil, ErrRollback("bootstrap", "transaction_execution", err)
	}

	// Step 9: Display completion message
	if opts.NonInteractive {
		ab.logger.Info("Agent bootstrap completed successfully")
	} else {
		_ = ab.prompter.DisplayCompletionMessage(configPath, ModeAgent)
	}

	// Return result
	return &BootstrapResult{
		ConfigPath:     configPath,
		ServicePath:    "/etc/systemd/system/dso-agent.service",
		PermissionsSet: true,
		Warnings: []string{
			"Run 'sudo usermod -aG dso $USER' and 'sudo usermod -aG docker $USER' for non-root access",
			"User must log out and log back in for group membership changes to take effect",
		},
	}, nil
}

// collectConfiguration gathers configuration from user or arguments
func (ab *AgentBootstrapper) collectConfiguration(ctx context.Context, opts *BootstrapOptions, cloudInfo *CloudProviderInfo) (*ConfigBuilder, error) {
	builder := NewConfigBuilder().
		WithVersion("1.0").
		WithMode(ModeAgent)

	var provider string

	// Determine provider
	if opts.Provider != "" {
		// User specified provider
		provider = opts.Provider
		ab.logger.Info("Using specified provider", "provider", provider)
	} else if cloudInfo.Detected {
		// Cloud provider detected
		if opts.NonInteractive {
			provider = cloudInfo.Provider
			ab.logger.Info("Using detected cloud provider", "provider", provider)
		} else {
			// Ask user to confirm
			useDetected, err := ab.prompter.PromptCloudProviderConfirmation(cloudInfo)
			if err != nil {
				return nil, ErrInteractivePrompt("bootstrap", err)
			}

			if useDetected {
				provider = cloudInfo.Provider
			} else {
				// User declined, ask for manual selection
				p, err := ab.prompter.PromptProviderSelection()
				if err != nil {
					return nil, ErrInteractivePrompt("bootstrap", err)
				}
				provider = p
			}
		}
	} else {
		// No cloud detection, ask user
		if opts.NonInteractive {
			return nil, ErrConfigValidation("bootstrap", "no provider specified and not in interactive mode")
		}

		p, err := ab.prompter.PromptProviderSelection()
		if err != nil {
			return nil, ErrInteractivePrompt("bootstrap", err)
		}
		provider = p
	}

	// Configure provider-specific settings
	switch provider {
	case ProviderAWS:
		var region string
		if opts.AWSRegion != "" {
			region = opts.AWSRegion
		} else if !opts.NonInteractive {
			var err error
			region, err = ab.prompter.PromptAWSRegion()
			if err != nil {
				return nil, ErrInteractivePrompt("bootstrap", err)
			}
		} else {
			return nil, ErrConfigValidation("bootstrap", "AWS region required")
		}
		builder.WithAWSProvider("aws-prod", region)

	case ProviderAzure:
		var vaultURL string
		if opts.AzureVaultURL != "" {
			vaultURL = opts.AzureVaultURL
		} else if !opts.NonInteractive {
			var err error
			vaultURL, err = ab.prompter.PromptAzureVaultURL()
			if err != nil {
				return nil, ErrInteractivePrompt("bootstrap", err)
			}
		} else {
			return nil, ErrConfigValidation("bootstrap", "Azure Key Vault URL required")
		}
		builder.WithAzureProvider("azure-prod", vaultURL)

	case ProviderHuawei:
		var region, projectID string
		if opts.HuaweiRegion != "" && opts.HuaweiProjectID != "" {
			region = opts.HuaweiRegion
			projectID = opts.HuaweiProjectID
		} else if !opts.NonInteractive {
			var err error
			region, projectID, err = ab.prompter.PromptHuaweiRegionAndProject()
			if err != nil {
				return nil, ErrInteractivePrompt("bootstrap", err)
			}
		} else {
			return nil, ErrConfigValidation("bootstrap", "Huawei region and project ID required")
		}
		builder.WithHuaweiProvider("huawei-prod", region, projectID)

	case ProviderVault:
		var address string
		if opts.VaultAddress != "" {
			address = opts.VaultAddress
		} else if !opts.NonInteractive {
			var err error
			address, err = ab.prompter.PromptVaultAddress()
			if err != nil {
				return nil, ErrInteractivePrompt("bootstrap", err)
			}
		} else {
			return nil, ErrConfigValidation("bootstrap", "Vault address required")
		}
		builder.WithVaultProvider("vault-prod", address, "${VAULT_TOKEN}")
	}

	// Configure secrets
	if opts.Secrets != nil && len(opts.Secrets) > 0 {
		// Secrets provided via options
		for _, secret := range opts.Secrets {
			builder.WithSecret(secret.Name, secret.Provider, secret.Mappings)
		}
	} else if !opts.NonInteractive {
		// Ask user for secrets
		secrets, err := ab.prompter.PromptSecrets(provider)
		if err != nil {
			return nil, ErrInteractivePrompt("bootstrap", err)
		}

		for _, secret := range secrets {
			builder.WithSecret(secret.Name, provider, secret.Mappings)
		}
	}

	return builder, nil
}

// BootstrapNonInteractive performs non-interactive (CI/automation) bootstrap
func (ab *AgentBootstrapper) BootstrapNonInteractive(ctx context.Context, opts *BootstrapOptions) (*BootstrapResult, error) {
	ab.logger.Info("Starting non-interactive agent bootstrap")

	// For CI mode, use provided options directly
	opts.NonInteractive = true
	opts.Force = true // Skip confirmations

	return ab.Bootstrap(ctx, opts)
}

// GetBootstrapYAML returns the generated YAML for inspection before apply
func (ab *AgentBootstrapper) GetBootstrapYAML(ctx context.Context, opts *BootstrapOptions) (string, error) {
	// Detect cloud provider
	cloudInfo, err := ab.detector.DetectCloudProvider(ctx)
	if err != nil {
		return "", err
	}

	// Collect configuration (non-interactive)
	opts.NonInteractive = true
	builder, err := ab.collectConfiguration(ctx, opts, cloudInfo)
	if err != nil {
		return "", err
	}

	// Return YAML
	return builder.BuildYAMLString()
}

// ValidateConfiguration validates a configuration without executing bootstrap
func (ab *AgentBootstrapper) ValidateConfiguration(ctx context.Context, yamlContent []byte) error {
	var config Config
	if err := yaml.Unmarshal(yamlContent, &config); err != nil {
		return ErrConfigValidation("validation", fmt.Sprintf("YAML unmarshal failed: %v", err))
	}

	return ab.validator.ValidateConfig(&config)
}
