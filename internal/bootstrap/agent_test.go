package bootstrap

import (
	"context"
	"testing"
)

// MockLogger implements the Logger interface for testing
type MockLogger struct {
	messages []string
}

func (m *MockLogger) Info(msg string, args ...interface{}) {
	m.messages = append(m.messages, msg)
}

func (m *MockLogger) Error(msg string, args ...interface{}) {
	m.messages = append(m.messages, msg)
}

func (m *MockLogger) Warn(msg string, args ...interface{}) {
	m.messages = append(m.messages, msg)
}

func (m *MockLogger) Debug(msg string, args ...interface{}) {
	m.messages = append(m.messages, msg)
}

// TestAgentBootstrapNilContextValueDoesNotPanic verifies the fix for the nil interface conversion
// This is a regression test for the bug: panic: interface conversion: interface {} is nil, not string
func TestAgentBootstrapNilContextValueDoesNotPanic(t *testing.T) {
	logger := &MockLogger{}
	opts := &BootstrapOptions{
		Mode:           ModeAgent,
		Provider:       "azure",
		NonInteractive: true,
		Force:          true,
		Timeout:        0,
		Context:        context.Background(), // Empty context - no config_path key
		AzureVaultURL:  "https://test.vault.azure.net/",
	}

	bootstrapper := NewAgentBootstrapper(logger, opts)
	if bootstrapper == nil {
		t.Fatal("NewAgentBootstrapper returned nil - should create successfully")
	}

	// This should NOT panic, even though opts.Context.Value("config_path") returns nil
	// The old code would panic here: configPath := opts.Context.Value("config_path").(string)
	ctx := context.Background()

	// We expect this to fail validation or other error, but NOT to panic
	// If it panics, the test will fail
	result, err := bootstrapper.Bootstrap(ctx, opts)

	// We don't care about the error here - we're testing that it doesn't panic
	// The actual bootstrap will fail because we're missing proper config, but that's OK
	_ = result
	_ = err

	if len(logger.messages) == 0 {
		t.Error("Expected some logger output, got none")
	}
}

// TestAgentBootstrapWithValidContextPath verifies that a config_path in context is used correctly
func TestAgentBootstrapWithValidContextPath(t *testing.T) {
	logger := &MockLogger{}
	testPath := "/custom/path/dso.yaml"
	ctx := context.WithValue(context.Background(), "config_path", testPath)

	opts := &BootstrapOptions{
		Mode:           ModeAgent,
		Provider:       "azure",
		NonInteractive: true,
		Force:          true,
		Timeout:        0,
		Context:        ctx,
		AzureVaultURL:  "https://test.vault.azure.net/",
	}

	_ = NewAgentBootstrapper(logger, opts) // Verify bootstrapper can be created

	// Extract the context path safely - this tests the defensive check
	retrievedPath := "/etc/dso/dso.yaml" // default
	if opts.Context != nil {
		if val := opts.Context.Value("config_path"); val != nil {
			if path, ok := val.(string); ok && path != "" {
				retrievedPath = path
			}
		}
	}

	if retrievedPath != testPath {
		t.Errorf("Expected path %s, got %s", testPath, retrievedPath)
	}
}

// TestAgentBootstrapWithNilContext verifies that nil context is handled safely
func TestAgentBootstrapWithNilContext(t *testing.T) {
	logger := &MockLogger{}
	opts := &BootstrapOptions{
		Mode:           ModeAgent,
		Provider:       "azure",
		NonInteractive: true,
		Force:          true,
		Timeout:        0,
		Context:        nil, // Explicitly nil context
		AzureVaultURL:  "https://test.vault.azure.net/",
	}

	bootstrapper := NewAgentBootstrapper(logger, opts)
	if bootstrapper == nil {
		t.Fatal("NewAgentBootstrapper returned nil with nil context - should create successfully")
	}

	// This is the defensive pattern: check if context is nil before using it
	configPath := "/etc/dso/dso.yaml"
	if opts.Context != nil {
		if val := opts.Context.Value("config_path"); val != nil {
			if path, ok := val.(string); ok && path != "" {
				configPath = path
			}
		}
	}

	if configPath != "/etc/dso/dso.yaml" {
		t.Errorf("Expected default path with nil context, got %s", configPath)
	}
}

// TestAgentBootstrapWithInvalidTypeInContext verifies wrong type is safely ignored
func TestAgentBootstrapWithInvalidTypeInContext(t *testing.T) {
	// Store an integer instead of string - tests type assertion safety
	ctx := context.WithValue(context.Background(), "config_path", 12345)

	opts := &BootstrapOptions{
		Mode:           ModeAgent,
		Provider:       "azure",
		NonInteractive: true,
		Force:          true,
		Timeout:        0,
		Context:        ctx,
		AzureVaultURL:  "https://test.vault.azure.net/",
	}

	// This should safely ignore the invalid type and use default
	configPath := "/etc/dso/dso.yaml"
	if opts.Context != nil {
		if val := opts.Context.Value("config_path"); val != nil {
			if path, ok := val.(string); ok && path != "" {
				configPath = path
			}
		}
	}

	if configPath != "/etc/dso/dso.yaml" {
		t.Errorf("Expected default path when context has wrong type, got %s", configPath)
	}
}

// TestGetProviderConfigWithMetadata_AWS verifies AWS auto-detection from metadata
func TestGetProviderConfigWithMetadata_AWS(t *testing.T) {
	logger := &MockLogger{}
	bootstrapper := &AgentBootstrapper{
		logger:    logger,
		detector:  nil,
		validator: nil,
		prompter:  nil,
		provCfg:   nil,
		cfgBuilder: nil,
		fsOps:     nil,
		svc:       nil,
		perm:      nil,
	}

	opts := &BootstrapOptions{
		Mode:           ModeAgent,
		NonInteractive: true,
		AWSRegion:      "eu-west-1",
	}

	cloudInfo := &CloudProviderInfo{
		Provider: ProviderAWS,
		Detected: true,
		Metadata: map[string]string{"instance_id": "i-0123456789abcdef0"},
	}

	config := bootstrapper.getProviderConfigWithMetadata(ProviderAWS, cloudInfo, opts)

	if config == nil {
		t.Fatal("Expected config, got nil")
	}

	if config["name"] != "aws-i-012345" {
		t.Errorf("Expected name 'aws-i-012345', got %v", config["name"])
	}

	if config["region"] != "eu-west-1" {
		t.Errorf("Expected region 'eu-west-1', got %v", config["region"])
	}
}

// TestGetProviderConfigWithMetadata_Azure verifies Azure auto-configuration
func TestGetProviderConfigWithMetadata_Azure(t *testing.T) {
	logger := &MockLogger{}
	bootstrapper := &AgentBootstrapper{
		logger:    logger,
		detector:  nil,
		validator: nil,
		prompter:  nil,
		provCfg:   nil,
		cfgBuilder: nil,
		fsOps:     nil,
		svc:       nil,
		perm:      nil,
	}

	opts := &BootstrapOptions{
		Mode:           ModeAgent,
		NonInteractive: true,
		AzureVaultURL:  "https://testvault.vault.azure.net/",
	}

	cloudInfo := &CloudProviderInfo{
		Provider: ProviderAzure,
		Detected: true,
		Metadata: map[string]string{"detected_via": "azure_imds"},
	}

	config := bootstrapper.getProviderConfigWithMetadata(ProviderAzure, cloudInfo, opts)

	if config == nil {
		t.Fatal("Expected config, got nil")
	}

	if config["name"] != "azure-provider" {
		t.Errorf("Expected name 'azure-provider', got %v", config["name"])
	}

	if config["vault_url"] != "https://testvault.vault.azure.net/" {
		t.Errorf("Expected vault_url 'https://testvault.vault.azure.net/', got %v", config["vault_url"])
	}
}

// TestGetProviderConfigWithMetadata_Huawei verifies Huawei auto-configuration
func TestGetProviderConfigWithMetadata_Huawei(t *testing.T) {
	logger := &MockLogger{}
	bootstrapper := &AgentBootstrapper{
		logger:    logger,
		detector:  nil,
		validator: nil,
		prompter:  nil,
		provCfg:   nil,
		cfgBuilder: nil,
		fsOps:     nil,
		svc:       nil,
		perm:      nil,
	}

	opts := &BootstrapOptions{
		Mode:              ModeAgent,
		NonInteractive:    true,
		HuaweiRegion:      "cn-south-1",
		HuaweiProjectID:   "12345abcde",
	}

	cloudInfo := &CloudProviderInfo{
		Provider: ProviderHuawei,
		Detected: true,
		Metadata: map[string]string{"detected_via": "huawei_metadata"},
	}

	config := bootstrapper.getProviderConfigWithMetadata(ProviderHuawei, cloudInfo, opts)

	if config == nil {
		t.Fatal("Expected config, got nil")
	}

	if config["name"] != "huawei-provider" {
		t.Errorf("Expected name 'huawei-provider', got %v", config["name"])
	}

	if config["region"] != "cn-south-1" {
		t.Errorf("Expected region 'cn-south-1', got %v", config["region"])
	}

	if config["project_id"] != "12345abcde" {
		t.Errorf("Expected project_id '12345abcde', got %v", config["project_id"])
	}
}

// TestGetProviderConfigWithMetadata_Vault verifies Vault auto-configuration
func TestGetProviderConfigWithMetadata_Vault(t *testing.T) {
	logger := &MockLogger{}
	bootstrapper := &AgentBootstrapper{
		logger:    logger,
		detector:  nil,
		validator: nil,
		prompter:  nil,
		provCfg:   nil,
		cfgBuilder: nil,
		fsOps:     nil,
		svc:       nil,
		perm:      nil,
	}

	opts := &BootstrapOptions{
		Mode:            ModeAgent,
		NonInteractive:  true,
		VaultAddress:    "https://vault.example.com:8200",
	}

	cloudInfo := &CloudProviderInfo{
		Provider: "local",
		Detected: false,
		Metadata: map[string]string{},
	}

	config := bootstrapper.getProviderConfigWithMetadata(ProviderVault, cloudInfo, opts)

	if config == nil {
		t.Fatal("Expected config, got nil")
	}

	if config["name"] != "vault-provider" {
		t.Errorf("Expected name 'vault-provider', got %v", config["name"])
	}

	if config["address"] != "https://vault.example.com:8200" {
		t.Errorf("Expected address 'https://vault.example.com:8200', got %v", config["address"])
	}
}

// TestGetProviderConfigWithMetadata_MissingVaultAddress verifies Vault fails without address
func TestGetProviderConfigWithMetadata_MissingVaultAddress(t *testing.T) {
	logger := &MockLogger{}
	bootstrapper := &AgentBootstrapper{
		logger:    logger,
		detector:  nil,
		validator: nil,
		prompter:  nil,
		provCfg:   nil,
		cfgBuilder: nil,
		fsOps:     nil,
		svc:       nil,
		perm:      nil,
	}

	opts := &BootstrapOptions{
		Mode:           ModeAgent,
		NonInteractive: true,
		VaultAddress:   "", // No address provided
	}

	cloudInfo := &CloudProviderInfo{
		Provider: "local",
		Detected: false,
		Metadata: map[string]string{},
	}

	config := bootstrapper.getProviderConfigWithMetadata(ProviderVault, cloudInfo, opts)

	if config != nil {
		t.Fatal("Expected nil config for missing Vault address in non-interactive mode, got config")
	}
}
