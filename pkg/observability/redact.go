package observability

import (
	"strings"
)

// Redact returns a masked version of a secret value.
// It also provides a standard way to mask keys if they are sensitive.
func Redact(value string) string {
	if value == "" {
		return ""
	}
	return "[REDACTED]"
}

// ShouldRedactKey returns true if the key name suggests it contains a secret.
func ShouldRedactKey(key string) bool {
	k := strings.ToLower(key)
	secretKeywords := []string{
		"secret", "password", "token", "key", "auth", "credential", "pwd", "apikey",
	}
	for _, kw := range secretKeywords {
		if strings.Contains(k, kw) {
			return true
		}
	}
	return false
}
