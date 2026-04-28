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
	Mode            string        `yaml:"mode"` // polling, event, hybrid
	PollingInterval string        `yaml:"polling_interval"`
	Webhook         WebhookConfig `yaml:"webhook"`
}

type WebhookConfig struct {
	Enabled   bool   `yaml:"enabled"`
	AuthToken string `yaml:"auth_token"`
	Endpoint  string `yaml:"endpoint,omitempty"`
}

type RotationConfigV2 struct {
	Enabled            bool   `yaml:"enabled"`
	Strategy           string `yaml:"strategy"` // restart, signal, none
	Signal             string `yaml:"signal,omitempty"`
	HealthCheckTimeout string `yaml:"health_check_timeout,omitempty"`
}

type AuthConfig struct {
	Method string            `yaml:"method"` // iam_role, access_key, token, env
	Params map[string]string `yaml:"params,omitempty"`
}

type RetryConfig struct {
	Attempts int    `yaml:"attempts"`
	Backoff  string `yaml:"backoff"`
}

type ProviderConfig struct {
	Type   string            `yaml:"type"`
	Region string            `yaml:"region,omitempty"`
	Auth   AuthConfig        `yaml:"auth,omitempty"`
	Retry  RetryConfig       `yaml:"retry,omitempty"`
	Config map[string]string `yaml:"config,omitempty"`
}

type InjectionConfig struct {
	Type string `yaml:"type"` // env, file
	Path string `yaml:"path,omitempty"`
	UID  int    `yaml:"uid,omitempty"`
	GID  int    `yaml:"gid,omitempty"`
}

type TargetConfig struct {
	Containers []string          `yaml:"containers,omitempty"`
	Labels     map[string]string `yaml:"labels,omitempty"`
}

type DefaultsConfig struct {
	Inject   InjectionConfig  `yaml:"inject,omitempty"`
	Rotation RotationConfigV2 `yaml:"rotation,omitempty"`
}

type LoggingConfig struct {
	Level  string `yaml:"level"`  // info, debug, error
	Format string `yaml:"format"` // json, text
}

type AgentConfig struct {
	Cache           bool             `yaml:"cache"`
	RefreshInterval string           `yaml:"refresh_interval"`
	AutoSync        bool             `yaml:"auto_sync"`
	RestartStrategy RestartStrategy  `yaml:"restart_strategy"`
	Watch           WatchConfig      `yaml:"watch"`
	Rotation        RotationConfigV2 `yaml:"rotation"`
}

type SecretMapping struct {
	Name     string            `yaml:"name"`
	Provider string            `yaml:"provider,omitempty"`
	Inject   InjectionConfig   `yaml:"inject"`
	Rotation RotationConfigV2  `yaml:"rotation"`
	Targets  TargetConfig      `yaml:"targets,omitempty"`
	Mappings map[string]string `yaml:"mappings"`
}

type Config struct {
	Providers map[string]ProviderConfig `yaml:"providers"`
	Agent     AgentConfig               `yaml:"agent"`
	Defaults  DefaultsConfig            `yaml:"defaults,omitempty"`
	Logging   LoggingConfig             `yaml:"logging,omitempty"`
	Secrets   []SecretMapping           `yaml:"secrets"`

	// Legacy fields for backward compatibility detection
	LegacyProvider string            `yaml:"provider,omitempty"`
	LegacyConfig   map[string]string `yaml:"config,omitempty"`
}

func (c *Config) UnmarshalYAML(value *yaml.Node) error {
	// Internal type to avoid infinite recursion
	type alias Config
	var aux alias

	if err := value.Decode(&aux); err != nil {
		return err
	}

	*c = Config(aux)

	// Handle Legacy Top-Level Provider
	if c.LegacyProvider != "" {
		if c.Providers == nil {
			c.Providers = make(map[string]ProviderConfig)
		}
		if _, exists := c.Providers[c.LegacyProvider]; !exists {
			c.Providers[c.LegacyProvider] = ProviderConfig{
				Type:   c.LegacyProvider,
				Config: c.LegacyConfig,
			}
		}
	}

	// Handle Legacy Secret Mappings
	for range c.Secrets {
		// Detect if Inject was a simple string in raw YAML (v1)
		// Since we've already decoded into structured InjectionConfig,
		// we need to check if the 'type' is empty but was likely intended.
		// However, yaml.v3 handles this better if we use an intermediate map or custom unmarshaler for SecretMapping too.
	}

	return nil
}

// SecretMapping custom unmarshaler to handle legacy formats
func (s *SecretMapping) UnmarshalYAML(value *yaml.Node) error {
	// 1. Try to decode as the standard V2 structure first
	type v2Type SecretMapping
	var v2 v2Type
	// Ignore errors on the first pass as it might fail due to legacy field types
	_ = value.Decode(&v2)
	*s = SecretMapping(v2)

	// 2. Handle legacy field disambiguation using a raw map representation
	var raw map[string]interface{}
	if err := value.Decode(&raw); err != nil {
		return err
	}

	// Always ensure Name is populated from raw if it wasn't caught by V2 decode
	if name, ok := raw["name"].(string); ok && s.Name == "" {
		s.Name = name
	}

	// Always ensure Mappings are populated
	if mappings, ok := raw["mappings"].(map[string]interface{}); ok && len(s.Mappings) == 0 {
		s.Mappings = make(map[string]string)
		for k, v := range mappings {
			if strVal, ok := v.(string); ok {
				s.Mappings[k] = strVal
			}
		}
	}

	// Handle Legacy 'inject' as a string (v1 format: inject: env)
	if val, ok := raw["inject"].(string); ok {
		s.Inject.Type = val
	}

	// Handle Legacy 'rotation' as a bool (v1 format: rotation: true)
	if val, ok := raw["rotation"].(bool); ok {
		s.Rotation.Enabled = val
	}

	// Handle Legacy 'reload_strategy'
	if reloadRaw, ok := raw["reload_strategy"].(map[string]interface{}); ok {
		if reloadType, ok := reloadRaw["type"].(string); ok {
			s.Rotation.Strategy = reloadType
			s.Rotation.Enabled = true
		}
	}

	return nil
}

func (c *Config) Validate() error {
	if len(c.Providers) == 0 {
		return fmt.Errorf("at least one provider must be configured")
	}

	for i, sec := range c.Secrets {
		if sec.Name == "" {
			return fmt.Errorf("secret at index %d is missing 'name'", i)
		}

		// 1. Merge Injection Defaults
		if sec.Inject.Type == "" {
			sec.Inject.Type = c.Defaults.Inject.Type
		}
		if sec.Inject.Type == "" {
			return fmt.Errorf("secret '%s' is missing injection type", sec.Name)
		}

		if sec.Inject.Path == "" {
			sec.Inject.Path = c.Defaults.Inject.Path
		}
		if sec.Inject.Type == "file" && sec.Inject.Path == "" {
			return fmt.Errorf("secret '%s' with type 'file' must have a 'path'", sec.Name)
		}

		if sec.Inject.UID == 0 {
			sec.Inject.UID = c.Defaults.Inject.UID
		}
		if sec.Inject.GID == 0 {
			sec.Inject.GID = c.Defaults.Inject.GID
		}

		// 2. Merge Rotation Defaults
		if !sec.Rotation.Enabled && c.Defaults.Rotation.Enabled {
			sec.Rotation.Enabled = true
		}
		if sec.Rotation.Strategy == "" {
			sec.Rotation.Strategy = c.Defaults.Rotation.Strategy
		}
		if sec.Rotation.Strategy == "" {
			sec.Rotation.Strategy = "restart" // Final fallback
		}

		// Sync back
		c.Secrets[i] = sec
	}

	return nil
}

// IsSafePath validates that a user-provided path does not escape the base directory.
func IsSafePath(baseDir, userPath string) (string, error) {
	clean := filepath.Clean(userPath)

	// List of allowed absolute system directories for DSO
	allowedSystemDirs := []string{
		"/etc/dso/",
		"/usr/local/lib/dso/",
		"/usr/local/bin/",
		"/var/run/dso/",
		"/run/dso/",
	}

	isSystemPath := false
	for _, dir := range allowedSystemDirs {
		if pathWithinDir(clean, dir) {
			isSystemPath = true
			break
		}
	}

	// Reject absolute paths in "anywhere" mode unless they are in allowed system directories.
	if filepath.IsAbs(clean) && !isSystemPath && baseDir == "" {
		return "", fmt.Errorf("absolute paths not allowed: %s", userPath)
	}

	// If no baseDir is provided, at least reject ".." for security
	if baseDir == "" {
		if !filepath.IsLocal(clean) && !isSystemPath {
			return "", fmt.Errorf("path traversal attempt: %s", userPath)
		}
		return clean, nil
	}

	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("invalid base path: %w", err)
	}
	var fullPath string
	if filepath.IsAbs(clean) {
		fullPath = clean
	} else {
		fullPath = filepath.Join(baseAbs, clean)
	}
	fullAbs, err := filepath.Abs(fullPath)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}

	// Ensure no directory escape
	rel, err := filepath.Rel(baseAbs, fullAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path traversal detected: %s", userPath)
	}

	return fullAbs, nil
}

func pathWithinDir(path, dir string) bool {
	pathAbs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return false
	}
	dirAbs, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(dirAbs, pathAbs)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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

	data, err := os.ReadFile(safePath) // #nosec G304 -- safePath is constrained by IsSafePath.
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", cfgFile, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse yaml config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	return &cfg, nil
}
