package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	dsoConfig "github.com/docker-secret-operator/dso/pkg/config"
	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"go.uber.org/zap"
)

// DiscoveryAPI handles runtime discovery endpoints
type DiscoveryAPI struct {
	cli    *client.Client
	logger *zap.Logger
	config *dsoConfig.Config
	cache  *DiscoveryCache
}

// NewDiscoveryAPI creates a new discovery API handler
func NewDiscoveryAPI(logger *zap.Logger, cli *client.Client, cfg *dsoConfig.Config) *DiscoveryAPI {
	api := &DiscoveryAPI{
		cli:    cli,
		logger: logger,
		config: cfg,
	}
	// Create cache with refresh callback
	api.cache = NewDiscoveryCache(30*time.Second, api.refreshDiscoveryData)
	return api
}

// ContainerMetadata represents a running container's discoverable metadata
type ContainerMetadata struct {
	ID                       string            `json:"id"`                         // Full container ID (SHA256)
	Name                     string            `json:"name"`                       // Container name (without leading /)
	Image                    string            `json:"image"`                      // Image name or ID
	Status                   string            `json:"status"`                     // e.g., "running", "exited"
	Created                  string            `json:"created"`                    // ISO 8601 timestamp
	State                    string            `json:"state"`                      // Current state
	RestartPolicy            RestartPolicyInfo `json:"restart_policy"`             // Restart configuration
	Network                  NetworkInfo       `json:"network"`                    // Network details
	EnvironmentVariableNames []string          `json:"environment_variable_names"` // Container env var names (no values)
	SensitiveVariableCount   int               `json:"sensitive_variable_count"`   // Count of password-like vars
	DSOAwareness             DSOAwarenessInfo  `json:"dso_awareness"`              // DSO configuration status
	Labels                   map[string]string `json:"labels"`                     // Container labels
}

// RestartPolicyInfo describes container restart policy
type RestartPolicyInfo struct {
	Name            string `json:"name"`             // "no", "always", "on-failure", "unless-stopped"
	MaxRetryCount   int    `json:"max_retry_count"`  // For on-failure policy
	BackoffStrategy string `json:"backoff_strategy"` // e.g., "exponential"
}

// NetworkInfo describes container network configuration
type NetworkInfo struct {
	IP       string   `json:"ip"`       // Container IP address
	Gateway  string   `json:"gateway"`  // Network gateway
	Networks []string `json:"networks"` // Connected networks
}

// DSOAwarenessInfo indicates whether container is managed by DSO
type DSOAwarenessInfo struct {
	Status          string   `json:"status"`           // "managed", "unmanaged", "partial"
	ManagedSecrets  []string `json:"managed_secrets"`  // Which secrets this container uses
	ConfigRefs      []string `json:"config_refs"`      // How it's referenced in config (label, name, etc.)
	MissingMappings []string `json:"missing_mappings"` // Env vars not in config but might need mapping
}

// DiscoveryResponse wraps container discovery results
type DiscoveryResponse struct {
	Containers     []ContainerMetadata `json:"containers"`
	TotalCount     int                 `json:"total_count"`
	ManagedCount   int                 `json:"managed_count"`
	UnmanagedCount int                 `json:"unmanaged_count"`
	PartialCount   int                 `json:"partial_count"`
	Timestamp      string              `json:"timestamp"`
}

// SecretMappingSuggestion suggests environment variables that might need secret mapping
type SecretMappingSuggestion struct {
	ContainerID         string                `json:"container_id"`
	ContainerName       string                `json:"container_name"`
	EnvVarName          string                `json:"env_var_name"`
	Confidence          string                `json:"confidence"` // "high", "medium", "low"
	Reason              string                `json:"reason"`     // Why this looks like a secret
	SuggestedSecretName string                `json:"suggested_secret_name"`
	ConfiguredSecret    *ConfiguredSecretInfo `json:"configured_secret,omitempty"` // If already configured
}

// ConfiguredSecretInfo shows if a secret is already configured
type ConfiguredSecretInfo struct {
	SecretName string `json:"secret_name"`
	Provider   string `json:"provider"`
	IsMapped   bool   `json:"is_mapped"` // If this container is already mapped to it
}

// MappingResponse wraps secret mapping suggestions
type MappingResponse struct {
	Suggestions []SecretMappingSuggestion `json:"suggestions"`
	TotalCount  int                       `json:"total_count"`
	Timestamp   string                    `json:"timestamp"`
}

// ErrorResponse is a structured error response
type ErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// sendJSONError sends a structured JSON error response
func (d *DiscoveryAPI) sendJSONError(w http.ResponseWriter, code string, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	resp := ErrorResponse{}
	resp.Error.Code = code
	resp.Error.Message = message
	json.NewEncoder(w).Encode(resp)
}

// HandleGetContainers lists all containers with DSO metadata (uses cache)
func (d *DiscoveryAPI) HandleGetContainers(w http.ResponseWriter, r *http.Request) {
	if d.cli == nil {
		d.sendJSONError(w, "docker_unavailable", "Docker client not available", http.StatusServiceUnavailable)
		return
	}

	response, _, err := d.cache.GetContainers()
	if err != nil {
		d.logger.Error("Failed to get containers", zap.Error(err))
		d.sendJSONError(w, "docker_list_error", "Failed to list containers: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// refreshDiscoveryData fetches fresh container discovery data (cache callback)
func (d *DiscoveryAPI) refreshDiscoveryData() (*DiscoveryResponse, *MappingResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// List all containers
	containers, err := d.cli.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		d.logger.Error("Failed to list containers", zap.Error(err))
		return nil, nil, err
	}

	metadata := make([]ContainerMetadata, 0, len(containers))
	managedCount := 0
	unmanagedCount := 0
	partialCount := 0

	// Get labeled containers for quick lookup
	labeledIDs := make(map[string]bool)
	if d.config != nil {
		labelFilter := filters.NewArgs()
		labelFilter.Add("label", "dso.reloader=true")
		labeled, err := d.cli.ContainerList(ctx, container.ListOptions{Filters: labelFilter})
		if err == nil {
			for _, c := range labeled {
				labeledIDs[c.ID] = true
			}
		}
	}

	// Process each container
	for _, c := range containers {
		meta := d.inspectContainer(ctx, c.ID, c, labeledIDs[c.ID])
		if meta != nil {
			metadata = append(metadata, *meta)

			// Count awareness status
			switch meta.DSOAwareness.Status {
			case "managed":
				managedCount++
			case "partial":
				partialCount++
			default:
				unmanagedCount++
			}
		}
	}

	// Sort by container name
	sort.Slice(metadata, func(i, j int) bool {
		return metadata[i].Name < metadata[j].Name
	})

	discoveryResponse := &DiscoveryResponse{
		Containers:     metadata,
		TotalCount:     len(metadata),
		ManagedCount:   managedCount,
		UnmanagedCount: unmanagedCount,
		PartialCount:   partialCount,
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
	}

	// Also refresh mappings
	mappingsResponse, err := d.computeMappings(ctx, containers)
	if err != nil {
		d.logger.Error("Failed to compute mappings", zap.Error(err))
		// Don't fail the whole request, just skip mappings
		mappingsResponse = &MappingResponse{
			Suggestions: make([]SecretMappingSuggestion, 0),
			TotalCount:  0,
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
		}
	}

	return discoveryResponse, mappingsResponse, nil
}

// inspectContainer gathers metadata for a single container
func (d *DiscoveryAPI) inspectContainer(ctx context.Context, containerID string, listResp interface{}, isLabeled bool) *ContainerMetadata {
	// Inspect full container details
	inspect, err := d.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		d.logger.Debug("Failed to inspect container", zap.String("id", containerID[:12]), zap.Error(err))
		return nil
	}

	// Extract name
	name := strings.TrimPrefix(inspect.Name, "/")

	// Extract network info
	var ip, gateway string
	networks := make([]string, 0)
	if inspect.NetworkSettings != nil && inspect.NetworkSettings.Networks != nil {
		for netName, endpoint := range inspect.NetworkSettings.Networks {
			networks = append(networks, netName)
			if ip == "" && endpoint.IPAddress != "" {
				ip = endpoint.IPAddress
				gateway = endpoint.Gateway
			}
		}
	}
	sort.Strings(networks)

	// Extract restart policy
	restartPolicy := RestartPolicyInfo{
		Name: "unknown",
	}
	if inspect.HostConfig != nil && string(inspect.HostConfig.RestartPolicy.Name) != "" {
		restartPolicy.Name = string(inspect.HostConfig.RestartPolicy.Name)
		restartPolicy.MaxRetryCount = inspect.HostConfig.RestartPolicy.MaximumRetryCount
	}

	// Parse environment variables
	envVars := parseContainerEnv(inspect.Config.Env)

	// Determine DSO awareness status
	awareness := d.assessDSOAwareness(&inspect, envVars, isLabeled, name)

	return &ContainerMetadata{
		ID:            inspect.ID,
		Name:          name,
		Image:         inspect.Config.Image,
		Status:        inspect.State.Status,
		Created:       inspect.Created,
		State:         inspect.State.Status,
		RestartPolicy: restartPolicy,
		Network: NetworkInfo{
			IP:       ip,
			Gateway:  gateway,
			Networks: networks,
		},
		EnvironmentVariableNames: getEnvVarNames(envVars),
		SensitiveVariableCount:   countSensitiveVariables(envVars),
		DSOAwareness:             awareness,
		Labels:                   inspect.Config.Labels,
	}
}

// assessDSOAwareness determines if container is managed by DSO
func (d *DiscoveryAPI) assessDSOAwareness(inspect *dockertypes.ContainerJSON, envVars map[string]string, isLabeled bool, containerName string) DSOAwarenessInfo {
	awareness := DSOAwarenessInfo{
		Status:         "unmanaged",
		ManagedSecrets: make([]string, 0),
		ConfigRefs:     make([]string, 0),
	}

	if d.config == nil {
		return awareness
	}

	// Check label-based management
	if isLabeled {
		awareness.Status = "managed"
		awareness.ConfigRefs = append(awareness.ConfigRefs, "dso.reloader=true label")
	}

	// Check config-driven matching (explicit container targets)
	for _, secret := range d.config.Secrets {
		matches := false

		// Check explicit container targets
		for _, target := range secret.Targets.Containers {
			if target == containerName || target == inspect.ID {
				matches = true
				awareness.ConfigRefs = append(awareness.ConfigRefs, fmt.Sprintf("targets.containers: %s", target))
				break
			}
		}

		if matches {
			awareness.ManagedSecrets = append(awareness.ManagedSecrets, secret.Name)
			if awareness.Status == "unmanaged" {
				awareness.Status = "managed"
			}
		}
	}

	// Check if any environment variables match configured mappings
	// This runs for ALL containers, not just those without explicit targets
	for _, secret := range d.config.Secrets {
		// Skip if already in managed secrets (already explicitly targeted)
		alreadyManaged := false
		for _, managed := range awareness.ManagedSecrets {
			if managed == secret.Name {
				alreadyManaged = true
				break
			}
		}
		if alreadyManaged {
			continue
		}

		// Check if any env vars match this secret's mappings
		for providerKey, envName := range secret.Mappings {
			if _, exists := envVars[envName]; exists {
				// This env var matches a configured mapping
				awareness.ManagedSecrets = append(awareness.ManagedSecrets, secret.Name)
				if awareness.Status == "unmanaged" {
					awareness.Status = "partial"
				}
				break
			}
			// Also check if provider key is in env
			if _, exists := envVars[providerKey]; exists {
				awareness.ManagedSecrets = append(awareness.ManagedSecrets, secret.Name)
				if awareness.Status == "unmanaged" {
					awareness.Status = "partial"
				}
				break
			}
		}
	}

	return awareness
}

// HandleGetMappings suggests secret mappings based on container environment analysis (uses cache)
func (d *DiscoveryAPI) HandleGetMappings(w http.ResponseWriter, r *http.Request) {
	if d.cli == nil {
		d.sendJSONError(w, "docker_unavailable", "Docker client not available", http.StatusServiceUnavailable)
		return
	}

	_, response, err := d.cache.GetMappings()
	if err != nil {
		d.logger.Error("Failed to get mappings", zap.Error(err))
		d.sendJSONError(w, "docker_list_error", "Failed to analyze mappings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// computeMappings analyzes containers and suggests secret mappings
func (d *DiscoveryAPI) computeMappings(ctx context.Context, containers []dockertypes.Container) (*MappingResponse, error) {
	// Collect all environment variables that look like secrets
	suggestions := make([]SecretMappingSuggestion, 0)

	for _, c := range containers {
		inspect, err := d.cli.ContainerInspect(ctx, c.ID)
		if err != nil {
			continue
		}

		containerName := strings.TrimPrefix(inspect.Name, "/")
		envVars := parseContainerEnv(inspect.Config.Env)

		// Analyze each environment variable
		for envName, envValue := range envVars {
			suggestion := d.analyzeSuggestion(envName, envValue, inspect.ID, containerName)
			if suggestion != nil {
				suggestions = append(suggestions, *suggestion)
			}
		}
	}

	// Sort suggestions by confidence and name
	sort.Slice(suggestions, func(i, j int) bool {
		if suggestions[i].Confidence != suggestions[j].Confidence {
			return confidenceScore(suggestions[i].Confidence) > confidenceScore(suggestions[j].Confidence)
		}
		return suggestions[i].ContainerName < suggestions[j].ContainerName
	})

	response := &MappingResponse{
		Suggestions: suggestions,
		TotalCount:  len(suggestions),
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}

	return response, nil
}

// analyzeSuggestion determines if an environment variable looks like a secret
func (d *DiscoveryAPI) analyzeSuggestion(envName string, envValue string, containerID string, containerName string) *SecretMappingSuggestion {
	// Keywords that suggest a variable is a secret
	secretKeywords := []string{
		"PASSWORD", "SECRET", "TOKEN", "KEY", "CREDENTIALS",
		"CREDENTIAL", "AUTH", "APIKEY", "API_KEY", "JWT",
		"CERT", "CERTIFICATE", "PRIVATE", "ACCESS_KEY",
		"SECRET_KEY", "SIGNING_KEY", "DATABASE_PASSWORD",
		"DB_PASSWORD", "MYSQL_PASSWORD", "POSTGRES_PASSWORD",
		// Connection strings and URLs
		"CONNECTION", "CONNECTIONSTRING", "DATABASE_URL", "DBURL", "DB_URI",
		"MONGODB_URI", "MONGODB_URL", "REDIS_URL", "REDIS_HOST",
		"ELASTIC_URL", "ELASTIC_HOST", "KAFKA_BROKERS", "BROKER_URL",
		// Additional secret patterns
		"BEARER", "OAUTH", "SESSION", "HMAC",
	}

	// Check if env var name matches secret patterns
	upperName := strings.ToUpper(envName)
	confidence := "low"
	reason := ""

	for _, keyword := range secretKeywords {
		if strings.Contains(upperName, keyword) {
			confidence = "high"
			reason = fmt.Sprintf("Environment variable name contains '%s' keyword", keyword)
			break
		}
	}

	// If not high confidence, check value characteristics
	if confidence != "high" {
		// Check if value looks encoded (base64, hex, etc.)
		if looksEncoded(envValue) {
			confidence = "medium"
			reason = "Environment variable value appears to be encoded (possibly base64 or hex)"
		}
	}

	// Only include if we have reasonable confidence
	if confidence == "low" {
		return nil
	}

	suggestion := &SecretMappingSuggestion{
		ContainerID:         containerID,
		ContainerName:       containerName,
		EnvVarName:          envName,
		Confidence:          confidence,
		Reason:              reason,
		SuggestedSecretName: normalizeSecretName(envName),
	}

	// Check if this is already configured
	if d.config != nil {
		for _, secret := range d.config.Secrets {
			// Check if env var name matches any mapping
			for _, mappedEnv := range secret.Mappings {
				if mappedEnv == envName {
					suggestion.ConfiguredSecret = &ConfiguredSecretInfo{
						SecretName: secret.Name,
						Provider:   secret.Provider,
						IsMapped:   true,
					}
					break
				}
			}
		}
	}

	return suggestion
}

// Helper functions

// getEnvVarNames extracts and sorts environment variable names
func getEnvVarNames(envMap map[string]string) []string {
	names := make([]string, 0, len(envMap))
	for name := range envMap {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// countSensitiveVariables counts environment variables that look like secrets
func countSensitiveVariables(envMap map[string]string) int {
	sensitivePatterns := []string{
		"PASSWORD", "SECRET", "TOKEN", "KEY",
		"CREDENTIAL", "API", "AUTH", "PRIVATE",
		"SEED", "PIN", "CERT", "SIGNATURE",
	}

	count := 0
	for name := range envMap {
		upperName := strings.ToUpper(name)
		for _, pattern := range sensitivePatterns {
			if strings.Contains(upperName, pattern) {
				count++
				break
			}
		}
	}
	return count
}

// parseContainerEnv parses container environment variable list
func parseContainerEnv(envList []string) map[string]string {
	envMap := make(map[string]string)
	for _, e := range envList {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}
	return envMap
}

// HandleRefresh invalidates and refreshes discovery cache
func (d *DiscoveryAPI) HandleRefresh(w http.ResponseWriter, r *http.Request) {
	if d.cli == nil {
		d.sendJSONError(w, "docker_unavailable", "Docker client not available", http.StatusServiceUnavailable)
		return
	}

	// Invalidate and refresh asynchronously
	d.cache.InvalidateAsync()

	// Return immediate status
	response := map[string]interface{}{
		"status":    "refreshing",
		"message":   "Discovery cache refresh initiated",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandleGetMetrics returns cache performance metrics
func (d *DiscoveryAPI) HandleGetMetrics(w http.ResponseWriter, r *http.Request) {
	metrics := d.cache.GetMetrics()

	cacheAge := d.cache.CacheAge()
	isFresh := d.cache.IsFresh()

	response := map[string]interface{}{
		"cache_hits":         metrics.Hits,
		"cache_misses":       metrics.Misses,
		"refresh_count":      metrics.RefreshCount,
		"refresh_latency_ms": metrics.RefreshLatency.Milliseconds(),
		"cache_age_ms":       cacheAge.Milliseconds(),
		"is_fresh":           isFresh,
		"timestamp":          time.Now().UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// looksEncoded checks if a value looks like it's encoded
func looksEncoded(value string) bool {
	if len(value) < 8 {
		return false
	}

	// Check for base64 characteristics
	base64Chars := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/="
	base64Count := 0
	for _, char := range value {
		if strings.ContainsRune(base64Chars, char) {
			base64Count++
		}
	}
	if float64(base64Count)/float64(len(value)) > 0.95 {
		return true
	}

	// Check for hex characteristics
	hexChars := "0123456789abcdefABCDEF"
	hexCount := 0
	for _, char := range value {
		if strings.ContainsRune(hexChars, char) {
			hexCount++
		}
	}
	if float64(hexCount)/float64(len(value)) > 0.95 && len(value) > 16 {
		return true
	}

	return false
}

// normalizeSecretName converts env var name to secret name format
func normalizeSecretName(envName string) string {
	// Convert SNAKE_CASE to lowercase-with-hyphens
	name := strings.ToLower(envName)
	name = strings.ReplaceAll(name, "_", "-")
	return name
}

// confidenceScore converts confidence string to numeric score for sorting
func confidenceScore(confidence string) int {
	switch confidence {
	case "high":
		return 3
	case "medium":
		return 2
	default:
		return 1
	}
}
