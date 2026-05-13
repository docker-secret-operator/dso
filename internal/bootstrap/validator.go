package bootstrap

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"strconv"
	"strings"
)

// ConfigValidator validates bootstrap configuration and system state
type ConfigValidator struct {
	logger Logger
}

// NewConfigValidator creates a new configuration validator
func NewConfigValidator(logger Logger) *ConfigValidator {
	return &ConfigValidator{
		logger: logger,
	}
}

// ValidateBootstrapOptions validates the bootstrap options
func (cv *ConfigValidator) ValidateBootstrapOptions(opts *BootstrapOptions) error {
	// Validate mode
	if opts.Mode != ModeLocal && opts.Mode != ModeAgent {
		return ErrConfigValidation("validation", fmt.Sprintf("invalid mode: %s. Must be '%s' or '%s'", opts.Mode, ModeLocal, ModeAgent))
	}

	// Validate provider
	validProviders := map[string]bool{
		ProviderAWS:    true,
		ProviderAzure:  true,
		ProviderHuawei: true,
		ProviderVault:  true,
	}
	if opts.Provider != "" && !validProviders[opts.Provider] {
		return ErrInvalidProvider("validation", opts.Provider)
	}

	// Validate context
	if opts.Context == nil {
		return ErrConfigValidation("validation", "context is required")
	}

	// Validate timeout
	if opts.Timeout == 0 {
		opts.Timeout = 30 * 60 // Default to 30 minutes
	}

	cv.logger.Info("Bootstrap options validated", "mode", opts.Mode, "provider", opts.Provider)
	return nil
}

// ValidateConfig validates the generated configuration
func (cv *ConfigValidator) ValidateConfig(config *Config) error {
	// Validate version
	if config.Version == "" {
		return ErrConfigValidation("validation", "config version is required")
	}

	// Validate runtime
	if config.Runtime.Mode == "" {
		return ErrConfigValidation("validation", "runtime mode is required")
	}
	if config.Runtime.LogLevel == "" {
		return ErrConfigValidation("validation", "log level is required")
	}

	// Validate providers exist and have correct types
	validProviderTypes := map[string]bool{
		"vault":  true,
		"aws":    true,
		"azure":  true,
		"huawei": true,
	}

	for providerName, providerConfig := range config.Providers {
		if providerConfig.Type == "" {
			return ErrConfigValidation("validation", fmt.Sprintf("provider '%s' has no type", providerName))
		}

		if !validProviderTypes[providerConfig.Type] {
			return ErrProviderConfig("validation", providerConfig.Type, fmt.Sprintf("invalid type '%s'", providerConfig.Type))
		}

		// Provider-specific validation
		if err := cv.validateProviderConfig(providerName, &providerConfig); err != nil {
			return err
		}
	}

	// Validate secrets reference existing providers
	for _, secret := range config.Secrets {
		if secret.Provider == "" {
			return ErrConfigValidation("validation", fmt.Sprintf("secret '%s' has no provider", secret.Name))
		}

		if _, exists := config.Providers[secret.Provider]; !exists {
			return ErrConfigValidation("validation", fmt.Sprintf("secret '%s' references non-existent provider '%s'", secret.Name, secret.Provider))
		}

		if secret.Name == "" {
			return ErrConfigValidation("validation", "secret must have a name")
		}

		if len(secret.Mappings) == 0 {
			return ErrConfigValidation("validation", fmt.Sprintf("secret '%s' has no mappings", secret.Name))
		}
	}

	cv.logger.Info("Configuration validated successfully",
		"providers", len(config.Providers),
		"secrets", len(config.Secrets))
	return nil
}

// validateProviderConfig validates provider-specific configuration
func (cv *ConfigValidator) validateProviderConfig(name string, cfg *ProviderConfig) error {
	switch cfg.Type {
	case "aws":
		// AWS requires region
		if cfg.Region == "" {
			return ErrProviderConfig("validation", "aws", "region is required for AWS provider")
		}

	case "azure":
		// Azure requires vault_url in config
		if cfg.Config == nil || cfg.Config["vault_url"] == "" {
			return ErrProviderConfig("validation", "azure", "vault_url is required in config")
		}

	case "huawei":
		// Huawei requires region
		if cfg.Region == "" {
			return ErrProviderConfig("validation", "huawei", "region is required for Huawei provider")
		}
		// Huawei requires project_id in config
		if cfg.Config == nil || cfg.Config["project_id"] == "" {
			return ErrProviderConfig("validation", "huawei", "project_id is required in config")
		}

	case "vault":
		// Vault requires address, token, mount in config
		if cfg.Config == nil {
			return ErrProviderConfig("validation", "vault", "config is required for Vault provider")
		}
		if cfg.Config["address"] == "" {
			return ErrProviderConfig("validation", "vault", "address is required in config")
		}
		if cfg.Config["token"] == "" && cfg.Config["role_id"] == "" {
			return ErrProviderConfig("validation", "vault", "token or role_id is required in config")
		}
	}

	return nil
}

// ValidateDirectories validates that required directories can be accessed/created
func (cv *ConfigValidator) ValidateDirectories(ctx context.Context, fsOps *FilesystemOps) error {
	requiredDirs := []string{
		"/etc/dso",
		"/var/lib/dso",
		"/var/run/dso",
		"/var/log/dso",
	}

	for _, dir := range requiredDirs {
		// Try to validate the path
		_, err := fsOps.ValidatePath("/", dir)
		if err != nil {
			return ErrPathValidation("directory_validation", dir, "path validation failed: "+err.Error())
		}

		// Check if directory exists
		info, err := os.Stat(dir)
		if err == nil {
			// Directory exists - check if it's actually a directory
			if !info.IsDir() {
				return ErrPathValidation("directory_validation", dir, "path exists but is not a directory")
			}
		} else if os.IsNotExist(err) {
			// Directory doesn't exist - that's OK, we can create it
			cv.logger.Info("Directory does not exist, will be created", "path", dir)
		} else {
			// Some other error
			return ErrPathValidation("directory_validation", dir, fmt.Sprintf("cannot access: %v", err))
		}
	}

	cv.logger.Info("Directory validation passed")
	return nil
}

// ValidatePermissions checks if the current user has necessary permissions
func (cv *ConfigValidator) ValidatePermissions(ctx context.Context) error {
	currentUser, err := user.Current()
	if err != nil {
		return ErrUserValidation("permission_validation", "root")
	}

	// Check if running as root (UID 0)
	if currentUser.Uid != "0" {
		cv.logger.Warn("Running bootstrap as non-root user",
			"user", currentUser.Username,
			"uid", currentUser.Uid)
		// This is a warning, not an error - we'll set up permissions later
	}

	cv.logger.Info("User permission check passed",
		"user", currentUser.Username,
		"uid", currentUser.Uid)
	return nil
}

// ValidateInputSecrets validates secret definitions
func (cv *ConfigValidator) ValidateInputSecrets(secrets []SecretDefinition) error {
	seenNames := make(map[string]bool)

	for _, secret := range secrets {
		// Check for empty name
		if secret.Name == "" {
			return ErrConfigValidation("secret_validation", "secret name cannot be empty")
		}

		// Check for duplicates
		if seenNames[secret.Name] {
			return ErrConfigValidation("secret_validation", fmt.Sprintf("duplicate secret name: %s", secret.Name))
		}
		seenNames[secret.Name] = true

		// Check for empty provider
		if secret.Provider == "" {
			return ErrConfigValidation("secret_validation", fmt.Sprintf("secret '%s' has no provider", secret.Name))
		}

		// Validate provider is known
		validProviders := map[string]bool{
			ProviderAWS:    true,
			ProviderAzure:  true,
			ProviderHuawei: true,
			ProviderVault:  true,
		}
		if !validProviders[secret.Provider] {
			return ErrInvalidProvider("secret_validation", secret.Provider)
		}

		// Check mappings
		if len(secret.Mappings) == 0 {
			return ErrConfigValidation("secret_validation", fmt.Sprintf("secret '%s' has no mappings", secret.Name))
		}

		for key, value := range secret.Mappings {
			if key == "" || value == "" {
				return ErrConfigValidation("secret_validation",
					fmt.Sprintf("secret '%s' has empty key or value in mappings", secret.Name))
			}
		}
	}

	cv.logger.Info("Input secrets validated", "count", len(secrets))
	return nil
}

// ValidateYAML validates that content is valid YAML
func (cv *ConfigValidator) ValidateYAML(content []byte) error {
	if len(content) == 0 {
		return ErrYAMLGeneration("yaml_validation", fmt.Errorf("empty YAML content"))
	}

	// Basic YAML structure validation
	// Check for expected top-level keys
	contentStr := string(content)
	requiredKeys := []string{"version:", "runtime:", "providers:"}

	for _, key := range requiredKeys {
		if !strings.Contains(contentStr, key) {
			return ErrYAMLGeneration("yaml_validation",
				fmt.Errorf("missing required YAML key: %s", key))
		}
	}

	cv.logger.Info("YAML validation passed", "size", len(content))
	return nil
}

// GetCurrentUser returns information about the current user
func (cv *ConfigValidator) GetCurrentUser() (*UserInfo, error) {
	currentUser, err := user.Current()
	if err != nil {
		return nil, ErrUserValidation("user_detection", "current")
	}

	uid, err := strconv.Atoi(currentUser.Uid)
	if err != nil {
		return nil, ErrUserValidation("user_detection", currentUser.Username)
	}

	gid, err := strconv.Atoi(currentUser.Gid)
	if err != nil {
		return nil, ErrUserValidation("user_detection", currentUser.Username)
	}

	groups, err := currentUser.GroupIds()
	if err != nil {
		// Not fatal - just log warning
		cv.logger.Warn("Could not retrieve user groups", "user", currentUser.Username)
		groups = []string{}
	}

	return &UserInfo{
		UID:      uid,
		GID:      gid,
		Username: currentUser.Username,
		Home:     currentUser.HomeDir,
		Groups:   groups,
	}, nil
}

// GetRootUser returns information about root user
func (cv *ConfigValidator) GetRootUser() (*UserInfo, error) {
	rootUser, err := user.Lookup("root")
	if err != nil {
		return nil, ErrUserValidation("root_lookup", "root")
	}

	uid, err := strconv.Atoi(rootUser.Uid)
	if err != nil {
		return nil, ErrUserValidation("root_lookup", "root")
	}

	gid, err := strconv.Atoi(rootUser.Gid)
	if err != nil {
		return nil, ErrUserValidation("root_lookup", "root")
	}

	return &UserInfo{
		UID:      uid,
		GID:      gid,
		Username: rootUser.Username,
		Home:     rootUser.HomeDir,
		Groups:   []string{"root"},
	}, nil
}

// ValidateConfigPath validates that config path is appropriate
func (cv *ConfigValidator) ValidateConfigPath(configPath string) error {
	if configPath == "" {
		return ErrPathValidation("config_validation", configPath, "config path cannot be empty")
	}

	if !strings.Contains(configPath, "dso.yaml") {
		cv.logger.Warn("Unexpected config filename", "path", configPath)
	}

	return nil
}
