package bootstrap

import (
	"fmt"
	"gopkg.in/yaml.v3"
)

// ConfigBuilder provides a type-safe fluent interface for building DSO configuration
// This replaces unsafe string concatenation with structured YAML marshaling
type ConfigBuilder struct {
	config *Config
	errors []error
}

// NewConfigBuilder creates a new configuration builder
func NewConfigBuilder() *ConfigBuilder {
	return &ConfigBuilder{
		config: &Config{
			Version: "1.0",
			Runtime: RuntimeConfig{
				Mode:     "agent",
				LogLevel: "info",
			},
			Providers: make(map[string]ProviderConfig),
			Secrets:   []SecretMapping{},
		},
		errors: []error{},
	}
}

// WithVersion sets the configuration version
func (cb *ConfigBuilder) WithVersion(version string) *ConfigBuilder {
	if version == "" {
		cb.errors = append(cb.errors, fmt.Errorf("version cannot be empty"))
		return cb
	}
	cb.config.Version = version
	return cb
}

// WithMode sets the runtime mode (local or agent)
func (cb *ConfigBuilder) WithMode(mode BootstrapMode) *ConfigBuilder {
	if mode != ModeLocal && mode != ModeAgent {
		cb.errors = append(cb.errors, fmt.Errorf("invalid mode: %s", mode))
		return cb
	}
	cb.config.Runtime.Mode = string(mode)
	return cb
}

// WithLogLevel sets the runtime log level
func (cb *ConfigBuilder) WithLogLevel(level string) *ConfigBuilder {
	validLevels := map[string]bool{
		"debug":   true,
		"info":    true,
		"warn":    true,
		"warning": true,
		"error":   true,
	}
	if !validLevels[level] {
		cb.errors = append(cb.errors, fmt.Errorf("invalid log level: %s", level))
		return cb
	}
	cb.config.Runtime.LogLevel = level
	return cb
}

// WithAWSProvider adds or configures an AWS provider
func (cb *ConfigBuilder) WithAWSProvider(name, region string) *ConfigBuilder {
	if name == "" {
		cb.errors = append(cb.errors, fmt.Errorf("provider name cannot be empty"))
		return cb
	}
	if region == "" {
		cb.errors = append(cb.errors, fmt.Errorf("AWS region cannot be empty"))
		return cb
	}

	cb.config.Providers[name] = ProviderConfig{
		Type:   "aws",
		Region: region,
	}
	return cb
}

// WithAzureProvider adds or configures an Azure provider
func (cb *ConfigBuilder) WithAzureProvider(name, vaultURL string) *ConfigBuilder {
	if name == "" {
		cb.errors = append(cb.errors, fmt.Errorf("provider name cannot be empty"))
		return cb
	}
	if vaultURL == "" {
		cb.errors = append(cb.errors, fmt.Errorf("Azure vault URL cannot be empty"))
		return cb
	}

	config := make(map[string]string)
	config["vault_url"] = vaultURL

	cb.config.Providers[name] = ProviderConfig{
		Type:   "azure",
		Config: config,
	}
	return cb
}

// WithHuaweiProvider adds or configures a Huawei provider
func (cb *ConfigBuilder) WithHuaweiProvider(name, region, projectID string) *ConfigBuilder {
	if name == "" {
		cb.errors = append(cb.errors, fmt.Errorf("provider name cannot be empty"))
		return cb
	}
	if region == "" {
		cb.errors = append(cb.errors, fmt.Errorf("Huawei region cannot be empty"))
		return cb
	}
	if projectID == "" {
		cb.errors = append(cb.errors, fmt.Errorf("Huawei project ID cannot be empty"))
		return cb
	}

	config := make(map[string]string)
	config["project_id"] = projectID

	cb.config.Providers[name] = ProviderConfig{
		Type:   "huawei",
		Region: region,
		Config: config,
	}
	return cb
}

// WithVaultProvider adds or configures a Vault provider
func (cb *ConfigBuilder) WithVaultProvider(name, address, token string) *ConfigBuilder {
	if name == "" {
		cb.errors = append(cb.errors, fmt.Errorf("provider name cannot be empty"))
		return cb
	}
	if address == "" {
		cb.errors = append(cb.errors, fmt.Errorf("Vault address cannot be empty"))
		return cb
	}
	if token == "" {
		cb.errors = append(cb.errors, fmt.Errorf("Vault token cannot be empty"))
		return cb
	}

	config := make(map[string]string)
	config["address"] = address
	config["token"] = token

	cb.config.Providers[name] = ProviderConfig{
		Type:   "vault",
		Config: config,
	}
	return cb
}

// WithVaultProviderConfig adds a Vault provider with full config options
func (cb *ConfigBuilder) WithVaultProviderConfig(name string, cfg map[string]string) *ConfigBuilder {
	if name == "" {
		cb.errors = append(cb.errors, fmt.Errorf("provider name cannot be empty"))
		return cb
	}
	if cfg == nil || cfg["address"] == "" {
		cb.errors = append(cb.errors, fmt.Errorf("Vault config must contain 'address'"))
		return cb
	}

	cb.config.Providers[name] = ProviderConfig{
		Type:   "vault",
		Config: cfg,
	}
	return cb
}

// WithSecret adds a secret mapping
func (cb *ConfigBuilder) WithSecret(name, provider string, mappings map[string]string) *ConfigBuilder {
	if name == "" {
		cb.errors = append(cb.errors, fmt.Errorf("secret name cannot be empty"))
		return cb
	}
	if provider == "" {
		cb.errors = append(cb.errors, fmt.Errorf("secret provider cannot be empty"))
		return cb
	}
	if len(mappings) == 0 {
		cb.errors = append(cb.errors, fmt.Errorf("secret mappings cannot be empty"))
		return cb
	}

	// Check for duplicate secret names
	for _, existing := range cb.config.Secrets {
		if existing.Name == name {
			cb.errors = append(cb.errors, fmt.Errorf("duplicate secret name: '%s'", name))
			return cb
		}
	}

	// Validate that provider exists
	if _, exists := cb.config.Providers[provider]; !exists {
		cb.errors = append(cb.errors, fmt.Errorf("provider '%s' does not exist", provider))
		return cb
	}

	cb.config.Secrets = append(cb.config.Secrets, SecretMapping{
		Name:     name,
		Provider: provider,
		Mappings: mappings,
	})
	return cb
}

// GetErrors returns accumulated builder errors
func (cb *ConfigBuilder) GetErrors() []error {
	return cb.errors
}

// HasErrors returns true if there are accumulated errors
func (cb *ConfigBuilder) HasErrors() bool {
	return len(cb.errors) > 0
}

// WithDefaults sets default injection and rotation settings
func (cb *ConfigBuilder) WithDefaults(inject *InjectionConfig, rotation *RotationConfig) *ConfigBuilder {
	cb.config.Defaults = &DefaultConfig{
		Inject:   inject,
		Rotation: rotation,
	}
	return cb
}

// WithAgentConfig sets agent-specific settings
func (cb *ConfigBuilder) WithAgentConfig(cfg *AgentConfig) *ConfigBuilder {
	if cfg == nil {
		cb.errors = append(cb.errors, fmt.Errorf("agent config cannot be nil"))
		return cb
	}
	cb.config.Agent = cfg
	return cb
}

// Validate performs comprehensive validation on the configuration
func (cb *ConfigBuilder) Validate() error {
	// First check for builder errors
	if len(cb.errors) > 0 {
		return fmt.Errorf("configuration builder errors: %v", cb.errors)
	}

	// Validate required fields
	if cb.config.Version == "" {
		return fmt.Errorf("version is required")
	}

	if cb.config.Runtime.Mode == "" {
		return fmt.Errorf("runtime mode is required")
	}

	if cb.config.Runtime.LogLevel == "" {
		return fmt.Errorf("log level is required")
	}

	if len(cb.config.Providers) == 0 {
		return fmt.Errorf("at least one provider is required")
	}

	// Validate providers
	validProviderTypes := map[string]bool{
		"aws":    true,
		"azure":  true,
		"huawei": true,
		"vault":  true,
	}

	for providerName, providerCfg := range cb.config.Providers {
		if providerCfg.Type == "" {
			return fmt.Errorf("provider '%s' has no type", providerName)
		}

		if !validProviderTypes[providerCfg.Type] {
			return fmt.Errorf("provider '%s' has invalid type: %s", providerName, providerCfg.Type)
		}

		// Provider-specific validation
		switch providerCfg.Type {
		case "aws":
			if providerCfg.Region == "" {
				return fmt.Errorf("AWS provider '%s' requires region", providerName)
			}
		case "azure":
			if providerCfg.Config == nil || providerCfg.Config["vault_url"] == "" {
				return fmt.Errorf("Azure provider '%s' requires vault_url in config", providerName)
			}
		case "huawei":
			if providerCfg.Region == "" {
				return fmt.Errorf("Huawei provider '%s' requires region", providerName)
			}
			if providerCfg.Config == nil || providerCfg.Config["project_id"] == "" {
				return fmt.Errorf("Huawei provider '%s' requires project_id in config", providerName)
			}
		case "vault":
			if providerCfg.Config == nil || providerCfg.Config["address"] == "" {
				return fmt.Errorf("Vault provider '%s' requires address in config", providerName)
			}
		}
	}

	// Validate secrets reference existing providers
	for _, secret := range cb.config.Secrets {
		if _, exists := cb.config.Providers[secret.Provider]; !exists {
			return fmt.Errorf("secret '%s' references non-existent provider '%s'", secret.Name, secret.Provider)
		}
	}

	return nil
}

// Build constructs and validates the configuration, returning error if validation fails
func (cb *ConfigBuilder) Build() (*Config, error) {
	if err := cb.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}
	return cb.config, nil
}

// BuildYAML builds and marshals the configuration to YAML bytes
// This replaces unsafe string concatenation with safe structured marshaling
func (cb *ConfigBuilder) BuildYAML() ([]byte, error) {
	config, err := cb.Build()
	if err != nil {
		return nil, fmt.Errorf("configuration build failed: %w", err)
	}

	// Marshal to YAML using standard library
	// This is safe from injection attacks - the marshaler escapes all values
	data, err := yaml.Marshal(config)
	if err != nil {
		return nil, ErrYAMLGeneration("config_builder", fmt.Errorf("YAML marshaling failed: %w", err))
	}

	return data, nil
}

// BuildYAMLString builds and marshals the configuration to YAML string
func (cb *ConfigBuilder) BuildYAMLString() (string, error) {
	data, err := cb.BuildYAML()
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// BuildYAMLWithTemplate builds YAML with comments and examples
func (cb *ConfigBuilder) BuildYAMLWithTemplate() ([]byte, error) {
	baseYAML, err := cb.BuildYAML()
	if err != nil {
		return nil, err
	}

	// Build template with comments and examples
	// Note: Defaults (inject and rotation) are included from YAML marshaling above
	template := fmt.Sprintf(`# DSO Configuration (V3.4)
# Reference: pkg/config/config.go

%s

# ── Agent Configuration ────────────────────────────────────────────────────────
agent:
  cache: true
  watch:
    polling_interval: 1m

# ── Secret Mappings ────────────────────────────────────────────────────────────
# For Azure Key Vault: Azure secrets are plain strings. DSO wraps as {"value": "<string>"}
# For AWS Secrets Manager: JSON strings are parsed automatically
# For Vault: Values stored exactly as provided
# For Huawei KMS: Plain strings

secrets:
  # Example: Uncomment and customize for your secrets
  # - name: MYSQL-ROOT-PASSWORD    # Exact secret name in provider
  #   provider: %s
  #   mappings:
  #     value: MYSQL_ROOT_PASSWORD

`, string(baseYAML), cb.getFirstProviderName())

	return []byte(template), nil
}

// getFirstProviderName returns the first configured provider name
func (cb *ConfigBuilder) getFirstProviderName() string {
	for name := range cb.config.Providers {
		return name
	}
	return "my-provider"
}

// GetConfig returns the underlying Config object without validation
// Use Build() for validated configuration
func (cb *ConfigBuilder) GetConfig() *Config {
	return cb.config
}

// GetProviders returns the providers map
func (cb *ConfigBuilder) GetProviders() map[string]ProviderConfig {
	return cb.config.Providers
}

// GetSecrets returns the secrets list
func (cb *ConfigBuilder) GetSecrets() []SecretMapping {
	return cb.config.Secrets
}

// Reset clears all configuration and returns a fresh builder
func (cb *ConfigBuilder) Reset() *ConfigBuilder {
	return NewConfigBuilder()
}

// Clone creates a deep copy of the builder's configuration
func (cb *ConfigBuilder) Clone() *ConfigBuilder {
	newBuilder := NewConfigBuilder()
	newBuilder.config = &Config{
		Version:   cb.config.Version,
		Runtime:   cb.config.Runtime,
		Providers: make(map[string]ProviderConfig),
		Secrets:   make([]SecretMapping, len(cb.config.Secrets)),
	}

	// Copy providers
	for k, v := range cb.config.Providers {
		configCopy := make(map[string]string)
		if v.Config != nil {
			for ck, cv := range v.Config {
				configCopy[ck] = cv
			}
		}
		newBuilder.config.Providers[k] = ProviderConfig{
			Type:   v.Type,
			Region: v.Region,
			Config: configCopy,
		}
	}

	// Copy secrets
	for i, s := range cb.config.Secrets {
		mappings := make(map[string]string)
		for k, v := range s.Mappings {
			mappings[k] = v
		}
		newBuilder.config.Secrets[i] = SecretMapping{
			Name:     s.Name,
			Provider: s.Provider,
			Mappings: mappings,
		}
	}

	return newBuilder
}
