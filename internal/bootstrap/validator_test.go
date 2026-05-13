package bootstrap

import (
	"context"
	"testing"
)

// TestConfigValidator tests configuration validation
func TestConfigValidator(t *testing.T) {
	logger := &testLogger{}
	validator := NewConfigValidator(logger)

	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: &Config{
				Version: "1.0",
				Runtime: RuntimeConfig{
					Mode:     "agent",
					LogLevel: "info",
				},
				Providers: map[string]ProviderConfig{
					"aws-prod": {
						Type:   "aws",
						Region: "us-east-1",
					},
				},
				Secrets: []SecretMapping{
					{
						Name:     "my-secret",
						Provider: "aws-prod",
						Mappings: map[string]string{"key": "value"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "missing version",
			config: &Config{
				Runtime: RuntimeConfig{
					Mode:     "agent",
					LogLevel: "info",
				},
			},
			wantErr: true,
		},
		{
			name: "invalid provider type",
			config: &Config{
				Version: "1.0",
				Runtime: RuntimeConfig{
					Mode:     "agent",
					LogLevel: "info",
				},
				Providers: map[string]ProviderConfig{
					"invalid": {
						Type: "invalid-provider",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "AWS without region",
			config: &Config{
				Version: "1.0",
				Runtime: RuntimeConfig{
					Mode:     "agent",
					LogLevel: "info",
				},
				Providers: map[string]ProviderConfig{
					"aws": {
						Type: "aws",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Azure without vault URL",
			config: &Config{
				Version: "1.0",
				Runtime: RuntimeConfig{
					Mode:     "agent",
					LogLevel: "info",
				},
				Providers: map[string]ProviderConfig{
					"azure": {
						Type: "azure",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "secret references non-existent provider",
			config: &Config{
				Version: "1.0",
				Runtime: RuntimeConfig{
					Mode:     "agent",
					LogLevel: "info",
				},
				Providers: map[string]ProviderConfig{
					"aws": {
						Type:   "aws",
						Region: "us-east-1",
					},
				},
				Secrets: []SecretMapping{
					{
						Name:     "secret",
						Provider: "non-existent",
						Mappings: map[string]string{"key": "value"},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidateBootstrapOptions tests bootstrap option validation
func TestValidateBootstrapOptions(t *testing.T) {
	logger := &testLogger{}
	validator := NewConfigValidator(logger)

	tests := []struct {
		name    string
		opts    *BootstrapOptions
		wantErr bool
	}{
		{
			name: "valid local options",
			opts: &BootstrapOptions{
				Mode:    ModeLocal,
				Context: context.Background(),
			},
			wantErr: false,
		},
		{
			name: "valid agent options",
			opts: &BootstrapOptions{
				Mode:    ModeAgent,
				Context: context.Background(),
			},
			wantErr: false,
		},
		{
			name: "invalid mode",
			opts: &BootstrapOptions{
				Mode:    "invalid",
				Context: context.Background(),
			},
			wantErr: true,
		},
		{
			name: "missing context",
			opts: &BootstrapOptions{
				Mode: ModeAgent,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateBootstrapOptions(tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateBootstrapOptions() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidateInputSecrets tests secret validation
func TestValidateInputSecrets(t *testing.T) {
	logger := &testLogger{}
	validator := NewConfigValidator(logger)

	tests := []struct {
		name    string
		secrets []SecretDefinition
		wantErr bool
	}{
		{
			name: "valid secrets",
			secrets: []SecretDefinition{
				{
					Name:     "secret1",
					Provider: "aws",
					Mappings: map[string]string{"key": "value"},
				},
			},
			wantErr: false,
		},
		{
			name: "duplicate secret names",
			secrets: []SecretDefinition{
				{
					Name:     "secret",
					Provider: "aws",
					Mappings: map[string]string{"key": "value"},
				},
				{
					Name:     "secret",
					Provider: "aws",
					Mappings: map[string]string{"key": "value"},
				},
			},
			wantErr: true,
		},
		{
			name: "empty secret name",
			secrets: []SecretDefinition{
				{
					Name:     "",
					Provider: "aws",
					Mappings: map[string]string{"key": "value"},
				},
			},
			wantErr: true,
		},
		{
			name: "empty mappings",
			secrets: []SecretDefinition{
				{
					Name:     "secret",
					Provider: "aws",
					Mappings: map[string]string{},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateInputSecrets(tt.secrets)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateInputSecrets() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidateYAML tests YAML validation
func TestValidateYAML(t *testing.T) {
	logger := &testLogger{}
	validator := NewConfigValidator(logger)

	tests := []struct {
		name    string
		content []byte
		wantErr bool
	}{
		{
			name: "valid YAML",
			content: []byte(`version: 1.0
runtime:
  mode: agent
  log_level: info
providers:`),
			wantErr: false,
		},
		{
			name:    "empty content",
			content: []byte{},
			wantErr: true,
		},
		{
			name:    "missing required keys",
			content: []byte("some: content"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateYAML(tt.content)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateYAML() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
