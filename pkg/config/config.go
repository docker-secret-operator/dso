package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type RestartStrategy struct {
	Type        string `yaml:"type"`
	GracePeriod string `yaml:"grace_period"`
}

type WatchConfig struct {
	Mode            string `yaml:"mode"` // polling, event, hybrid
	PollingInterval string `yaml:"polling_interval"`
}

type WebhookConfig struct {
	Enabled   bool   `yaml:"enabled"`
	AuthToken string `yaml:"auth_token"`
}

type RotationConfig struct {
	Strategy           string `yaml:"strategy"`
	HealthCheckTimeout string `yaml:"health_check_timeout"`
	MaxParallel        int    `yaml:"max_parallel"`
}

type AgentConfig struct {
	Cache           bool            `yaml:"cache"`
	RefreshInterval string          `yaml:"refresh_interval"` 
	AutoSync        bool            `yaml:"auto_sync"`
	RestartStrategy RestartStrategy `yaml:"restart_strategy"`
	Watch           WatchConfig     `yaml:"watch"`
	Webhook         WebhookConfig   `yaml:"webhook"`
	Rotation        RotationConfig  `yaml:"rotation"`
}

type ReloadStrategy struct {
	Type string `yaml:"type"` // "signal" | "restart" | "none"
}

type SecretMapping struct {
	Name           string            `yaml:"name"`
	Inject         string            `yaml:"inject"` // "file", "env", "socket"
	Path           string            `yaml:"path,omitempty"`
	Rotation       bool              `yaml:"rotation"`
	ReloadStrategy ReloadStrategy    `yaml:"reload_strategy"`
	Mappings       map[string]string `yaml:"mappings"`
}

type Config struct {
	Provider string            `yaml:"provider"`
	Config   map[string]string `yaml:"config,omitempty"`
	Region   string            `yaml:"region,omitempty"`
	Agent    AgentConfig       `yaml:"agent"`
	Secrets  []SecretMapping   `yaml:"secrets"`
}

// IsSafePath validates that a user-provided path does not escape the base directory
// and is not absolute.
func IsSafePath(baseDir, userPath string) (string, error) {
	clean := filepath.Clean(userPath)

	// Reject absolute paths
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("absolute paths not allowed: %s", userPath)
	}

	// If no baseDir is provided, at least reject ".."
	if baseDir == "" {
		if strings.Contains(clean, "..") {
			return "", fmt.Errorf("path traversal attempt: %s", userPath)
		}
		return clean, nil
	}

	fullPath := filepath.Join(baseDir, clean)

	// Ensure no directory escape
	if !strings.HasPrefix(fullPath, baseDir) {
		return "", fmt.Errorf("path traversal detected: %s", userPath)
	}

	return fullPath, nil
}

func LoadConfig(cfgFile string) (*Config, error) {
	if cfgFile == "" {
		cfgFile = "dso.yaml"
	}

	// Standard validation for config file path
	safePath, err := IsSafePath("", cfgFile)
	if err != nil {
		return nil, fmt.Errorf("invalid config path: %w", err)
	}

	data, err := os.ReadFile(safePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", cfgFile, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse yaml config: %w", err)
	}

	return &cfg, nil
}
