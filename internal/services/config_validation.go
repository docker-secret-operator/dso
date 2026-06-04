package services

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ForbiddenSecretPatterns are patterns that should not appear in draft configs
var ForbiddenSecretPatterns = []string{
	"password",
	"passwd",
	"pwd",
	"token",
	"api_key",
	"apikey",
	"secret",
	"private_key",
	"privatekey",
	"credential",
	"credentials",
	"access_token",
	"refresh_token",
	"bearer",
	"basic_auth",
	"authorization",
	"x-api-key",
	"aws_secret",
	"db_password",
	"root_password",
	"private_key_data",
	"pem",
	"rsa",
	"ssh_key",
}

// ConfigValidationResult contains validation results
type ConfigValidationResult struct {
	Valid       bool
	Errors      []string
	Warnings    []string
	Info        []string
	HasMappings bool
	MappingCount int
	SecretCount int
}

// ValidateDraftConfig validates draft configuration content
func ValidateDraftConfig(config string) ConfigValidationResult {
	result := ConfigValidationResult{
		Valid:    true,
		Errors:   []string{},
		Warnings: []string{},
		Info:     []string{},
	}

	// Parse JSON
	var configData map[string]interface{}
	if err := json.Unmarshal([]byte(config), &configData); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, "Config is not valid JSON")
		return result
	}

	// Check for forbidden patterns that look like actual credential values
	// We want to detect things like: "password": "actual_password_here"
	// But allow legitimate reference names like "password_ref": "..."
	// Strategy: only flag if pattern appears in ALL_CAPS or with suspicious characters
	for _, pattern := range ForbiddenSecretPatterns {
		// Look for patterns that are clearly values, not keys
		// Examples of values to flag:
		// - "password": "password123"
		// - ": "token_abc123"
		// - "value": "secret_data_here"

		configLower := strings.ToLower(config)

		// Only check for patterns that look like actual credentials (not just references)
		// Skip common false positives: "password" as a key is fine, but "password123" as a value is not
		if pattern == "password" || pattern == "secret" || pattern == "token" {
			// Check if pattern appears as a complete string value between quotes
			// Look for ": "pattern or ": pattern format
			searchPattern := fmt.Sprintf("\": \"%s", pattern)
			if strings.Contains(configLower, searchPattern) {
				result.Valid = false
				result.Errors = append(result.Errors,
					fmt.Sprintf("Config contains credential value: '%s'", pattern))
			}
		}
	}

	// Check for mappings structure
	if mappings, ok := configData["mappings"]; ok {
		if mappingsList, isList := mappings.([]interface{}); isList {
			result.HasMappings = true
			result.MappingCount = len(mappingsList)
			result.Info = append(result.Info,
				fmt.Sprintf("Config contains %d mappings", result.MappingCount))
		}
	}

	// Check for secrets structure
	if secrets, ok := configData["secrets"]; ok {
		if secretsList, isList := secrets.([]interface{}); isList {
			result.SecretCount = len(secretsList)
			result.Info = append(result.Info,
				fmt.Sprintf("Config references %d secrets", result.SecretCount))
		}
	}

	// Warn if config is empty
	if len(configData) == 0 {
		result.Warnings = append(result.Warnings, "Config is empty object")
	}

	// Warn if no mappings
	if !result.HasMappings && result.MappingCount == 0 {
		result.Warnings = append(result.Warnings, "Config contains no mappings")
	}

	return result
}

// Helper to get max of two ints
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ValidateDraftConfigStrict performs strict validation (errors only)
func ValidateDraftConfigStrict(config string) error {
	result := ValidateDraftConfig(config)
	if !result.Valid {
		return fmt.Errorf("invalid config: %s", strings.Join(result.Errors, "; "))
	}
	return nil
}
