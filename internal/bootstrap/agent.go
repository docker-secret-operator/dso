package bootstrap

import (
	"context"
	"fmt"
	"os"

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

	// Step 3: Detect cloud provider (reuse cached result if available)
	cloudInfo := opts.CloudInfo
	if cloudInfo == nil {
		var err error
		cloudInfo, err = ab.detector.DetectCloudProvider(ctx)
		if err != nil {
			return nil, err
		}
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

	// Step 5: Generate YAML with template and examples
	configYAML, err := builder.BuildYAMLWithTemplate()
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

	// Step 9: Configure non-root access if requested
	warnings := []string{}
	if opts.EnableNonRootAccess && currentUser.UID != 0 {
		ab.logger.Info("Configuring non-root access for user", "uid", currentUser.UID, "username", currentUser.Username)
		if err := ab.perm.ConfigureNonRootAccess(currentUser.UID); err != nil {
			ab.logger.Warn("Non-root access configuration had issues", "error", err.Error())
			warnings = append(warnings, fmt.Sprintf("Non-root access setup: %v", err))
		} else {
			warnings = append(warnings, fmt.Sprintf("User %s configured for non-root access - log out and log back in to apply group changes", currentUser.Username))
		}
	} else if !opts.EnableNonRootAccess && currentUser.UID != 0 {
		warnings = append(warnings, fmt.Sprintf("To enable non-root CLI access for %s, run: sudo docker dso bootstrap agent --enable-nonroot", currentUser.Username))
	}

	// Step 10: Display completion message
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
		Warnings:       warnings,
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
		// Cloud provider detected - auto-use without confirmation
		provider = cloudInfo.Provider
		ab.logger.Info("Using detected cloud provider", "provider", provider)
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

	// Validate provider is not empty
	if provider == "" {
		return nil, ErrConfigValidation("bootstrap", "no provider selected")
	}

	// Configure provider-specific settings
	// Try to use detected cloud metadata first
	providerConfig := ab.getProviderConfigWithMetadata(provider, cloudInfo, opts)
	if providerConfig == nil {
		return nil, ErrConfigValidation("bootstrap", fmt.Sprintf("failed to configure provider: %s", provider))
	}

	switch provider {
	case ProviderAWS:
		builder.WithAWSProvider(providerConfig["name"].(string), providerConfig["region"].(string))
	case ProviderAzure:
		builder.WithAzureProvider(providerConfig["name"].(string), providerConfig["vault_url"].(string))
	case ProviderHuawei:
		builder.WithHuaweiProvider(providerConfig["name"].(string), providerConfig["region"].(string), providerConfig["project_id"].(string))
	case ProviderVault:
		builder.WithVaultProvider(providerConfig["name"].(string), providerConfig["address"].(string), "${VAULT_TOKEN}")
	default:
		return nil, ErrConfigValidation("bootstrap", fmt.Sprintf("unknown provider: %s", provider))
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

	// Check for builder errors immediately
	if builder.HasErrors() {
		errs := builder.GetErrors()
		return nil, ErrConfigValidation("bootstrap", fmt.Sprintf("configuration errors: %v", errs))
	}

	// Add default injection and rotation settings
	builder.WithDefaults(
		&InjectionConfig{Type: "env"},
		&RotationConfig{Enabled: true, Strategy: "restart"},
	)

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
	// Detect cloud provider (reuse cached result if available)
	cloudInfo := opts.CloudInfo
	if cloudInfo == nil {
		var err error
		cloudInfo, err = ab.detector.DetectCloudProvider(ctx)
		if err != nil {
			return "", err
		}
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

// getProviderConfigWithMetadata extracts configuration from cloud metadata and options
func (ab *AgentBootstrapper) getProviderConfigWithMetadata(provider string, cloudInfo *CloudProviderInfo, opts *BootstrapOptions) map[string]interface{} {
	config := make(map[string]interface{})

	switch provider {
	case ProviderAWS:
		// AWS: Use instance ID or default name
		name := "aws-provider"
		if cloudInfo.Detected && cloudInfo.Metadata["instance_id"] != "" {
			name = "aws-" + cloudInfo.Metadata["instance_id"][:8]
		}

		// Use region from options if provided, otherwise try environment or default
		region := opts.AWSRegion
		if region == "" {
			region = os.Getenv("AWS_REGION")
		}
		if region == "" {
			region = os.Getenv("AWS_DEFAULT_REGION")
		}
		if region == "" && !opts.NonInteractive {
			// Prompt user for region if not found
			var err error
			region, err = ab.prompter.PromptAWSRegion()
			if err != nil {
				ab.logger.Error("Failed to prompt for AWS region", "error", err.Error())
				return nil
			}
		}
		if region == "" {
			region = "us-east-1" // Default fallback
		}

		ab.logger.Info("AWS provider configured", "name", name, "region", region)
		config["name"] = name
		config["region"] = region
		return config

	case ProviderAzure:
		// Azure: Use default name
		name := "azure-provider"

		// Use vault URL from options if provided, otherwise try environment
		vaultURL := opts.AzureVaultURL
		if vaultURL == "" {
			vaultURL = os.Getenv("AZURE_VAULT_URL")
		}
		if vaultURL == "" && !opts.NonInteractive {
			// Prompt user for vault URL
			var err error
			vaultURL, err = ab.prompter.PromptAzureVaultURL()
			if err != nil {
				ab.logger.Error("Failed to prompt for Azure Vault URL", "error", err.Error())
				return nil
			}
		}
		if vaultURL == "" {
			ab.logger.Error("Azure Vault URL not provided")
			return nil
		}

		ab.logger.Info("Azure provider configured", "name", name, "vault_url", vaultURL)
		config["name"] = name
		config["vault_url"] = vaultURL
		return config

	case ProviderHuawei:
		// Huawei: Use default name
		name := "huawei-provider"

		// Use region and project ID from options if provided
		region := opts.HuaweiRegion
		projectID := opts.HuaweiProjectID

		// Try environment variables
		if region == "" {
			region = os.Getenv("HUAWEI_REGION")
		}
		if projectID == "" {
			projectID = os.Getenv("HUAWEI_PROJECT_ID")
		}

		// Prompt if needed
		if (region == "" || projectID == "") && !opts.NonInteractive {
			var err error
			region, projectID, err = ab.prompter.PromptHuaweiRegionAndProject()
			if err != nil {
				ab.logger.Error("Failed to prompt for Huawei configuration", "error", err.Error())
				return nil
			}
		}

		// Apply defaults if still empty
		if region == "" {
			region = "cn-north-4"
		}
		if projectID == "" {
			ab.logger.Error("Huawei project ID not provided")
			return nil
		}

		ab.logger.Info("Huawei provider configured", "name", name, "region", region)
		config["name"] = name
		config["region"] = region
		config["project_id"] = projectID
		return config

	case ProviderVault:
		// Vault: Must have address (can't auto-detect self-hosted)
		name := "vault-provider"

		// Use vault address from options if provided, otherwise try environment
		vaultAddr := opts.VaultAddress
		if vaultAddr == "" {
			vaultAddr = os.Getenv("VAULT_ADDR")
		}
		if vaultAddr == "" && !opts.NonInteractive {
			// Prompt user for vault address
			var err error
			vaultAddr, err = ab.prompter.PromptVaultAddress()
			if err != nil {
				ab.logger.Error("Failed to prompt for Vault address", "error", err.Error())
				return nil
			}
		}
		if vaultAddr == "" {
			ab.logger.Error("Vault address not provided")
			return nil
		}

		ab.logger.Info("Vault provider configured", "name", name, "address", vaultAddr)
		config["name"] = name
		config["address"] = vaultAddr
		return config

	default:
		ab.logger.Error("Unknown provider", "provider", provider)
		return nil
	}
}

// ValidateConfiguration validates a configuration without executing bootstrap
func (ab *AgentBootstrapper) ValidateConfiguration(ctx context.Context, yamlContent []byte) error {
	var config Config
	if err := yaml.Unmarshal(yamlContent, &config); err != nil {
		return ErrConfigValidation("validation", fmt.Sprintf("YAML unmarshal failed: %v", err))
	}

	return ab.validator.ValidateConfig(&config)
}
