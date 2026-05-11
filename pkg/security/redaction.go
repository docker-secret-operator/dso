package security

import (
	"regexp"
	"strings"
)

// RedactionPatterns defines sensitive data patterns to redact from logs
type RedactionPatterns struct {
	// Common secret/token patterns
	patterns []*regexp.Regexp
}

// NewRedactionPatterns creates a new redaction pattern matcher
func NewRedactionPatterns() *RedactionPatterns {
	return &RedactionPatterns{
		patterns: []*regexp.Regexp{
			// API keys and tokens (loose patterns)
			regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password|passwd|pwd|auth|credential)\s*[:=]+\s*"?([^",;\s]*)"?`),
			// Bearer tokens
			regexp.MustCompile(`(?i)bearer\s+[a-z0-9._\-]+`),
			// AWS-style credentials
			regexp.MustCompile(`(?i)(AKIA[0-9A-Z]{16})`), // AWS Access Key
			// Docker credentials
			regexp.MustCompile(`(?i)("auth":\s*"[^"]+"|"password":\s*"[^"]+")`),
			// Private keys
			regexp.MustCompile(`(?i)(-----BEGIN.*PRIVATE KEY-----[^-]+-{5}END.*PRIVATE KEY-----)`),
			// Database connection strings with passwords
			regexp.MustCompile(`(?i)(password|passwd|pwd)=([^\s&;]+)`),
			// OAuth tokens
			regexp.MustCompile(`(?i)(access_token|refresh_token|id_token)["\s:=]+([^\s"',;]+)`),
			// URL-embedded passwords: scheme://user:password@host (postgres, mysql, redis, etc.)
			regexp.MustCompile(`(?i)[a-z][a-z0-9+\-.]*://[^:@/\s]+:[^@\s]{1,}@`),
			// sk-style API keys (OpenAI, Anthropic, etc.)
			regexp.MustCompile(`(?i)\bsk-[a-zA-Z0-9]{10,}\b`),
		},
	}
}

// RedactString redacts sensitive information from a string
func (rp *RedactionPatterns) RedactString(input string) string {
	output := input
	for _, pattern := range rp.patterns {
		output = pattern.ReplaceAllString(output, "[REDACTED]")
	}
	return output
}

// RedactError redacts sensitive information from an error message
func (rp *RedactionPatterns) RedactError(err error) string {
	if err == nil {
		return ""
	}
	return rp.RedactString(err.Error())
}

// ShouldLogField checks if a field name typically contains sensitive data
func ShouldLogField(fieldName string) bool {
	sensitiveFields := []string{
		"password", "passwd", "pwd",
		"secret", "token", "apikey", "api_key",
		"credential", "credentials",
		"auth", "authorization",
		"key", "private_key", "public_key",
		"bearer", "oauth",
		"database_url", "db_url",
		"connection_string",
		"vault_token", "vault_key",
		"aws_access_key", "aws_secret_key",
		"azure_client_secret",
		"gcp_service_account",
		"jwt", "jti",
	}

	lowerField := strings.ToLower(fieldName)
	for _, sensitive := range sensitiveFields {
		if strings.Contains(lowerField, sensitive) {
			return false
		}
	}
	return true
}

// RedactStructFields creates a safe representation of a struct by redacting sensitive fields
// This is useful for logging error details without exposing secrets
func RedactStructFields(obj map[string]interface{}) map[string]interface{} {
	safe := make(map[string]interface{})
	for key, value := range obj {
		if !ShouldLogField(key) {
			safe[key] = "[REDACTED]"
		} else if strVal, ok := value.(string); ok {
			// Apply pattern-based redaction to string values
			rp := NewRedactionPatterns()
			safe[key] = rp.RedactString(strVal)
		} else {
			safe[key] = value
		}
	}
	return safe
}

// SafeConfigValue returns a safe representation of config values
// Checks field names and patterns for sensitive data
func SafeConfigValue(key string, value interface{}) interface{} {
	if !ShouldLogField(key) {
		return "[REDACTED]"
	}

	if strVal, ok := value.(string); ok {
		rp := NewRedactionPatterns()
		return rp.RedactString(strVal)
	}

	return value
}
