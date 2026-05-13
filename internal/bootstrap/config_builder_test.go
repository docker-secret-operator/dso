package bootstrap

import (
	"testing"
)

// TestConfigBuilder tests type-safe configuration building
func TestConfigBuilder(t *testing.T) {
	builder := NewConfigBuilder().
		WithVersion("1.0").
		WithMode(ModeAgent).
		WithLogLevel("info").
		WithAWSProvider("aws-prod", "us-east-1").
		WithSecret("my-secret", "aws-prod", map[string]string{
			"password": "DB_PASSWORD",
			"username": "DB_USERNAME",
		})

	config, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if config.Version != "1.0" {
		t.Errorf("Version mismatch: got %q, want %q", config.Version, "1.0")
	}

	if config.Runtime.Mode != string(ModeAgent) {
		t.Errorf("Mode mismatch: got %q, want %q", config.Runtime.Mode, ModeAgent)
	}

	if len(config.Providers) != 1 {
		t.Errorf("Providers count mismatch: got %d, want 1", len(config.Providers))
	}

	if len(config.Secrets) != 1 {
		t.Errorf("Secrets count mismatch: got %d, want 1", len(config.Secrets))
	}
}

// TestConfigBuilderValidation tests builder validation
func TestConfigBuilderValidation(t *testing.T) {
	tests := []struct {
		name    string
		builder *ConfigBuilder
		wantErr bool
	}{
		{
			name: "valid complete config",
			builder: NewConfigBuilder().
				WithVersion("1.0").
				WithMode(ModeAgent).
				WithAWSProvider("aws-prod", "us-east-1").
				WithSecret("secret", "aws-prod", map[string]string{"key": "value"}),
			wantErr: false,
		},
		{
			name:    "missing version",
			builder: NewConfigBuilder().WithMode(ModeAgent),
			wantErr: true,
		},
		{
			name:    "missing provider",
			builder: NewConfigBuilder().WithVersion("1.0").WithMode(ModeAgent),
			wantErr: true,
		},
		{
			name: "secret references non-existent provider",
			builder: NewConfigBuilder().
				WithVersion("1.0").
				WithMode(ModeAgent).
				WithAWSProvider("aws-prod", "us-east-1").
				WithSecret("secret", "non-existent", map[string]string{"key": "value"}),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.builder.Build()
			if (err != nil) != tt.wantErr {
				t.Errorf("Build() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestConfigBuilderYAML tests YAML marshaling
func TestConfigBuilderYAML(t *testing.T) {
	builder := NewConfigBuilder().
		WithVersion("1.0").
		WithMode(ModeAgent).
		WithAWSProvider("aws-prod", "us-east-1").
		WithSecret("my-secret", "aws-prod", map[string]string{
			"password": "DB_PASSWORD",
		})

	yaml, err := builder.BuildYAML()
	if err != nil {
		t.Fatalf("BuildYAML() error = %v", err)
	}

	if len(yaml) == 0 {
		t.Fatal("BuildYAML() returned empty bytes")
	}

	// Verify YAML contains expected keys
	yamlStr := string(yaml)
	expectedKeys := []string{"version:", "runtime:", "providers:", "secrets:"}
	for _, key := range expectedKeys {
		if !contains(yamlStr, key) {
			t.Errorf("YAML missing expected key: %q", key)
		}
	}
}

// TestConfigBuilderAzure tests Azure provider configuration
func TestConfigBuilderAzure(t *testing.T) {
	builder := NewConfigBuilder().
		WithVersion("1.0").
		WithMode(ModeLocal).
		WithAzureProvider("azure-prod", "https://my-vault.vault.azure.net/")

	config, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	azureProvider, exists := config.Providers["azure-prod"]
	if !exists {
		t.Fatal("Azure provider not found")
	}

	if azureProvider.Type != "azure" {
		t.Errorf("Provider type mismatch: got %q, want %q", azureProvider.Type, "azure")
	}

	if azureProvider.Config["vault_url"] == "" {
		t.Fatal("Vault URL not configured")
	}
}

// TestConfigBuilderHuawei tests Huawei provider configuration
func TestConfigBuilderHuawei(t *testing.T) {
	builder := NewConfigBuilder().
		WithVersion("1.0").
		WithMode(ModeAgent).
		WithHuaweiProvider("huawei-prod", "ap-southeast-2", "my-project-id")

	config, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	huaweiProvider, exists := config.Providers["huawei-prod"]
	if !exists {
		t.Fatal("Huawei provider not found")
	}

	if huaweiProvider.Region != "ap-southeast-2" {
		t.Errorf("Region mismatch: got %q, want %q", huaweiProvider.Region, "ap-southeast-2")
	}

	if huaweiProvider.Config["project_id"] != "my-project-id" {
		t.Errorf("Project ID mismatch: got %q, want %q", huaweiProvider.Config["project_id"], "my-project-id")
	}
}

// TestConfigBuilderClone tests builder cloning
func TestConfigBuilderClone(t *testing.T) {
	original := NewConfigBuilder().
		WithVersion("1.0").
		WithMode(ModeAgent).
		WithAWSProvider("aws-prod", "us-east-1")

	cloned := original.Clone()

	// Modify clone
	cloned.WithAzureProvider("azure-prod", "https://vault.azure.net/")

	// Verify original is unchanged
	origConfig, _ := original.Build()
	if len(origConfig.Providers) != 1 {
		t.Error("Original was modified")
	}

	// Verify clone has both providers
	clonedConfig, _ := cloned.Build()
	if len(clonedConfig.Providers) != 2 {
		t.Errorf("Clone has incorrect provider count: got %d, want 2", len(clonedConfig.Providers))
	}
}

// TestConfigBuilderReset tests builder reset
func TestConfigBuilderReset(t *testing.T) {
	builder := NewConfigBuilder().
		WithVersion("1.0").
		WithMode(ModeAgent).
		WithAWSProvider("aws-prod", "us-east-1")

	// Reset
	builder = builder.Reset()

	config, err := builder.Build()
	if err == nil {
		// After reset, should have default version but no providers
		if len(config.Providers) != 0 {
			t.Errorf("Reset failed: got %d providers, want 0", len(config.Providers))
		}
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && s[len(substr)] != 0 || s[:len(substr)] == substr)
}
