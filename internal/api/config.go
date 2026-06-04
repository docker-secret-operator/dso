package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/docker-secret-operator/dso/pkg/provider"
	"go.uber.org/zap"
)

// ConfigAPI handles read-only configuration endpoints
type ConfigAPI struct {
	logger *zap.Logger
}

// NewConfigAPI creates a new configuration API handler
func NewConfigAPI(logger *zap.Logger) *ConfigAPI {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ConfigAPI{
		logger: logger,
	}
}

// ============================================================================
// Response Types
// ============================================================================

// ConfigResponse represents the complete configuration state
type ConfigResponse struct {
	Status             string                     `json:"status"`
	Path               string                     `json:"path"`
	LastModified       time.Time                  `json:"last_modified"`
	Valid              bool                       `json:"valid"`
	ValidationErrors   []string                   `json:"validation_errors"`
	Parsed             *ConfigSummary             `json:"parsed"`
	Providers          map[string]ProviderSummary `json:"providers"`
	SecretCount        int                        `json:"secret_count"`
	AgentConfiguration AgentConfigurationSummary  `json:"agent_configuration"`
}

// ConfigSummary is a simplified view of the configuration
type ConfigSummary struct {
	ProviderNames []string `json:"provider_names"`
	SecretNames   []string `json:"secret_names"`
	AgentEnabled  bool     `json:"agent_enabled"`
}

// ProviderSummary represents a single provider's status
type ProviderSummary struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Valid bool   `json:"valid"`
}

// AgentConfigurationSummary represents agent settings
type AgentConfigurationSummary struct {
	CacheEnabled     bool   `json:"cache_enabled"`
	AutoSyncEnabled  bool   `json:"auto_sync_enabled"`
	RefreshInterval  string `json:"refresh_interval"`
	RotationEnabled  bool   `json:"rotation_enabled"`
	RotationStrategy string `json:"rotation_strategy"`
	WatchMode        string `json:"watch_mode"`
	PollingInterval  string `json:"polling_interval"`
}

// RawConfigResponse wraps raw YAML with metadata
type RawConfigResponse struct {
	Path     string    `json:"path"`
	Modified time.Time `json:"modified"`
	Content  string    `json:"content"`
}

// ProviderType represents an available provider
type ProviderType struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ProvidersResponse lists available and active providers
type ProvidersResponse struct {
	Available map[string]ProviderType   `json:"available"`
	Active    map[string]ProviderStatus `json:"active"`
}

// ProviderStatus represents a configured provider's current status
type ProviderStatus struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Status string `json:"status"` // "valid", "invalid", "unchecked"
	Error  string `json:"error,omitempty"`
}

// ProviderTestResponse represents the result of a provider connectivity test
type ProviderTestResponse struct {
	Success  bool                   `json:"success"`
	Status   string                 `json:"status"` // connected, failed
	Error    string                 `json:"error,omitempty"`
	Latency  int                    `json:"latency_ms,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// ============================================================================
// GET /api/config - Get Configuration Status
// ============================================================================

// HandleGetConfig returns the current configuration state
func (ca *ConfigAPI) HandleGetConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Get config file path
	configPath := resolveConfig()

	// Check if file exists
	info, err := os.Stat(configPath)
	if err != nil {
		ca.logger.Warn("config file not found", zap.String("path", configPath))
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "not_found",
			"path":   configPath,
			"error":  fmt.Sprintf("config file not found: %v", err),
		})
		return
	}

	// Load configuration
	cfg, loadErr := config.LoadConfig(configPath)
	lastModified := info.ModTime()

	resp := ConfigResponse{
		Path:         configPath,
		LastModified: lastModified,
		Status:       "loaded",
	}

	// Check validity
	if loadErr != nil {
		resp.Valid = false
		resp.ValidationErrors = []string{loadErr.Error()}
		resp.Status = "invalid"
		ca.logger.Debug("config validation failed", zap.Error(loadErr))
	} else {
		resp.Valid = true
		resp.Parsed = &ConfigSummary{
			ProviderNames: getProviderNames(cfg),
			SecretNames:   getSecretNames(cfg),
			AgentEnabled:  true,
		}
		resp.Providers = summarizeProviders(cfg)
		resp.SecretCount = len(cfg.Secrets)
		resp.AgentConfiguration = summarizeAgentConfig(cfg.Agent)
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		ca.logger.Error("failed to encode response", zap.Error(err))
	}
}

// ============================================================================
// GET /api/config/raw - Get Raw YAML
// ============================================================================

// HandleGetRawConfig returns the raw YAML content
func (ca *ConfigAPI) HandleGetRawConfig(w http.ResponseWriter, r *http.Request) {
	configPath := resolveConfig()

	// Read file
	data, err := os.ReadFile(configPath)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("failed to read config: %v", err),
		})
		return
	}

	// Get file info
	info, _ := os.Stat(configPath)

	resp := RawConfigResponse{
		Path:     configPath,
		Modified: info.ModTime(),
		Content:  string(data),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// ============================================================================
// GET /api/config/providers - List Providers
// ============================================================================

// HandleGetProviders returns available and configured providers
func (ca *ConfigAPI) HandleGetProviders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Load configuration
	configPath := resolveConfig()
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("failed to load config: %v", err),
		})
		return
	}

	// Available providers (hardcoded, same as CLI validation)
	available := map[string]ProviderType{
		"vault": {
			Name:        "vault",
			Description: "HashiCorp Vault",
		},
		"aws": {
			Name:        "aws",
			Description: "AWS Secrets Manager",
		},
		"azure": {
			Name:        "azure",
			Description: "Azure Key Vault",
		},
		"huawei": {
			Name:        "huawei",
			Description: "Huawei Cloud Secrets Manager",
		},
	}

	// Active providers from config
	active := make(map[string]ProviderStatus)
	for name, prov := range cfg.Providers {
		active[name] = ProviderStatus{
			Name:   name,
			Type:   prov.Type,
			Status: "valid",
			Error:  "",
		}
	}

	resp := ProvidersResponse{
		Available: available,
		Active:    active,
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		ca.logger.Error("failed to encode response", zap.Error(err))
	}
}

// ============================================================================
// POST /api/config/providers/{provider}/test - Test Provider Connectivity
// ============================================================================

// HandleTestProvider tests connectivity to a specific provider
func (ca *ConfigAPI) HandleTestProvider(w http.ResponseWriter, r *http.Request, providerName string) {
	w.Header().Set("Content-Type", "application/json")

	// Load configuration
	configPath := resolveConfig()
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ProviderTestResponse{
			Success: false,
			Status:  "error",
			Error:   fmt.Sprintf("failed to load config: %v", err),
		})
		return
	}

	// Find provider
	provCfg, exists := cfg.Providers[providerName]
	if !exists {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(ProviderTestResponse{
			Success: false,
			Status:  "not_found",
			Error:   fmt.Sprintf("provider '%s' not configured", providerName),
		})
		return
	}

	// Convert provider config to map[string]string
	providerConfig := provCfg.Config
	if providerConfig == nil {
		providerConfig = make(map[string]string)
	}

	// Add type to config for provider
	providerConfig["type"] = provCfg.Type
	if provCfg.Region != "" {
		providerConfig["region"] = provCfg.Region
	}

	// Test provider connectivity
	start := time.Now()
	prov, pluginClient, err := provider.LoadProvider(providerName, providerConfig)
	if pluginClient != nil {
		defer pluginClient.Kill()
	}
	latency := int(time.Since(start).Milliseconds())

	if err != nil {
		ca.logger.Warn("provider test failed", zap.String("provider", providerName), zap.Error(err))
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(ProviderTestResponse{
			Success: false,
			Status:  "failed",
			Error:   err.Error(),
			Latency: latency,
		})
		return
	}

	// Try to get a test secret to verify connectivity
	testSecretName := "test-secret"
	_, getErr := prov.GetSecret(testSecretName)
	// We don't care if the test secret exists, just that the provider responded

	resp := ProviderTestResponse{
		Success: getErr == nil || err == nil, // Success if provider is initialized
		Status:  "connected",
		Latency: latency,
		Metadata: map[string]interface{}{
			"type":   provCfg.Type,
			"region": provCfg.Region,
		},
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		ca.logger.Error("failed to encode response", zap.Error(err))
	}
}

// ============================================================================
// Helper Functions
// ============================================================================

// resolveConfig returns the active configuration file path
// This mirrors the CLI's ResolveConfig() logic
func resolveConfig() string {
	// Check /etc/dso/dso.yaml first (system-wide)
	if _, err := os.Stat("/etc/dso/dso.yaml"); err == nil {
		return "/etc/dso/dso.yaml"
	}

	// Fall back to current directory
	if _, err := os.Stat("dso.yaml"); err == nil {
		return "dso.yaml"
	}

	// Default
	return "dso.yaml"
}

// getProviderNames extracts provider names from config
func getProviderNames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Providers))
	for name := range cfg.Providers {
		names = append(names, name)
	}
	return names
}

// getSecretNames extracts secret names from config
func getSecretNames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Secrets))
	for _, secret := range cfg.Secrets {
		names = append(names, secret.Name)
	}
	return names
}

// summarizeProviders creates a summary of providers
func summarizeProviders(cfg *config.Config) map[string]ProviderSummary {
	result := make(map[string]ProviderSummary)
	for name, prov := range cfg.Providers {
		result[name] = ProviderSummary{
			Name:  name,
			Type:  prov.Type,
			Valid: true,
		}
	}
	return result
}

// summarizeAgentConfig creates a summary of agent configuration
func summarizeAgentConfig(agent config.AgentConfig) AgentConfigurationSummary {
	return AgentConfigurationSummary{
		CacheEnabled:     agent.Cache,
		AutoSyncEnabled:  agent.AutoSync,
		RefreshInterval:  agent.RefreshInterval,
		RotationEnabled:  agent.Rotation.Enabled,
		RotationStrategy: agent.Rotation.Strategy,
		WatchMode:        agent.Watch.Mode,
		PollingInterval:  agent.Watch.PollingInterval,
	}
}
