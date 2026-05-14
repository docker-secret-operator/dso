package config

import (
	"testing"
)

// TestValidate_InvalidProviderType rejects unsupported provider types
func TestValidate_InvalidProviderType(t *testing.T) {
	cfg := &Config{
		Providers: map[string]ProviderConfig{
			"unsupported": {Type: "unsupported_provider"},
		},
		Secrets: []SecretMapping{},
	}

	err := cfg.Validate()
	if err == nil || !contains(err.Error(), "invalid type") {
		t.Errorf("Should reject invalid provider type, got: %v", err)
	}
}

// TestValidate_InvalidAuthMethod rejects unsupported auth methods
func TestValidate_InvalidAuthMethod(t *testing.T) {
	cfg := &Config{
		Providers: map[string]ProviderConfig{
			"vault": {
				Type: "vault",
				Auth: AuthConfig{Method: "invalid_method"},
			},
		},
		Secrets: []SecretMapping{},
	}

	err := cfg.Validate()
	if err == nil || !contains(err.Error(), "invalid auth method") {
		t.Errorf("Should reject invalid auth method, got: %v", err)
	}
}

// TestValidate_InvalidInjectionType rejects unsupported injection types
func TestValidate_InvalidInjectionType(t *testing.T) {
	cfg := &Config{
		Providers: map[string]ProviderConfig{
			"vault": {Type: "vault"},
		},
		Secrets: []SecretMapping{
			{
				Name:   "test-secret",
				Inject: InjectionConfig{Type: "invalid_type"},
			},
		},
	}

	err := cfg.Validate()
	if err == nil || !contains(err.Error(), "invalid injection type") {
		t.Errorf("Should reject invalid injection type, got: %v", err)
	}
}

// TestValidate_InvalidRotationStrategy rejects unsupported strategies
func TestValidate_InvalidRotationStrategy(t *testing.T) {
	cfg := &Config{
		Providers: map[string]ProviderConfig{
			"vault": {Type: "vault"},
		},
		Secrets: []SecretMapping{
			{
				Name:   "test-secret",
				Inject: InjectionConfig{Type: "file", Path: "/tmp/secret", UID: 1000, GID: 1000},
				Rotation: RotationConfigV2{
					Strategy: "invalid_strategy",
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil || !contains(err.Error(), "invalid rotation strategy") {
		t.Errorf("Should reject invalid rotation strategy, got: %v", err)
	}
}

// TestValidate_FileInjectionRequiresPath enforces path requirement
func TestValidate_FileInjectionRequiresPath(t *testing.T) {
	cfg := &Config{
		Providers: map[string]ProviderConfig{
			"vault": {Type: "vault"},
		},
		Secrets: []SecretMapping{
			{
				Name:   "test-secret",
				Inject: InjectionConfig{Type: "file"},
			},
		},
	}

	err := cfg.Validate()
	if err == nil || !contains(err.Error(), "must have a 'path'") {
		t.Errorf("Should require path for file injection, got: %v", err)
	}
}

// TestValidate_ProvidersRequired rejects empty provider list
func TestValidate_ProvidersRequired(t *testing.T) {
	cfg := &Config{
		Providers: map[string]ProviderConfig{},
	}

	err := cfg.Validate()
	if err == nil || !contains(err.Error(), "at least one provider") {
		t.Errorf("Should require at least one provider, got: %v", err)
	}
}

// TestValidate_SecretNameRequired rejects missing secret names
func TestValidate_SecretNameRequired(t *testing.T) {
	cfg := &Config{
		Providers: map[string]ProviderConfig{
			"vault": {Type: "vault"},
		},
		Secrets: []SecretMapping{
			{
				Inject: InjectionConfig{Type: "file", Path: "/tmp/secret"},
			},
		},
	}

	err := cfg.Validate()
	if err == nil || !contains(err.Error(), "missing 'name'") {
		t.Errorf("Should require secret name, got: %v", err)
	}
}

// TestValidate_ProviderReferenceExists validates provider reference
func TestValidate_ProviderReferenceExists(t *testing.T) {
	cfg := &Config{
		Providers: map[string]ProviderConfig{
			"vault": {Type: "vault"},
		},
		Secrets: []SecretMapping{
			{
				Name:     "test-secret",
				Provider: "nonexistent",
				Inject:   InjectionConfig{Type: "file", Path: "/tmp/secret", UID: 1000, GID: 1000},
			},
		},
	}

	err := cfg.Validate()
	if err == nil || !contains(err.Error(), "non-existent provider") {
		t.Errorf("Should validate provider reference, got: %v", err)
	}
}

// TestValidate_ValidConfig passes for correct configuration
func TestValidate_ValidConfig(t *testing.T) {
	cfg := &Config{
		Providers: map[string]ProviderConfig{
			"vault": {
				Type: "vault",
				Auth: AuthConfig{Method: "token"},
			},
		},
		Secrets: []SecretMapping{
			{
				Name:     "test-secret",
				Provider: "vault",
				Inject:   InjectionConfig{Type: "file", Path: "/etc/secrets", UID: 1000, GID: 1000},
				Rotation: RotationConfigV2{
					Strategy: "restart",
					Enabled:  true,
				},
			},
		},
	}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("Valid config should not error, got: %v", err)
	}
}

// TestValidate_DefaultsApplied verifies defaults are applied correctly
func TestValidate_DefaultsApplied(t *testing.T) {
	cfg := &Config{
		Providers: map[string]ProviderConfig{
			"vault": {Type: "vault"},
		},
		Defaults: DefaultsConfig{
			Inject: InjectionConfig{
				Type: "file",
				Path: "/run/secrets",
				UID:  1000,
				GID:  1000,
			},
			Rotation: RotationConfigV2{
				Strategy: "restart",
				Enabled:  true,
			},
		},
		Secrets: []SecretMapping{
			{
				Name: "test-secret",
				// Intentionally omit inject and rotation to test defaults
			},
		},
	}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("Config with defaults should be valid, got: %v", err)
	}

	// Verify defaults were applied
	if cfg.Secrets[0].Inject.Type == "" {
		t.Error("Default injection type should be applied")
	}
	if cfg.Secrets[0].Rotation.Strategy == "" {
		t.Error("Default rotation strategy should be applied")
	}
}

// Helper function to check if error message contains substring
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
