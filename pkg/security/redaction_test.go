package security

import (
	"errors"
	"strings"
	"testing"
)

func TestRedactionPatterns_APIKey(t *testing.T) {
	rp := NewRedactionPatterns()

	tests := []struct {
		input    string
		contains string
		notFound string
	}{
		{
			input:    `api_key="sk-1234567890abcdef"`,
			contains: "[REDACTED]",
			notFound: "sk-1234567890abcdef",
		},
		{
			input:    `apikey: super_secret_key_12345`,
			contains: "[REDACTED]",
			notFound: "super_secret_key_12345",
		},
		{
			input:    `API_KEY="test-key-xyz"`,
			contains: "[REDACTED]",
			notFound: "test-key-xyz",
		},
	}

	for _, test := range tests {
		result := rp.RedactString(test.input)
		if !strings.Contains(result, test.contains) {
			t.Errorf("Expected '%s' in redacted output, got: %s", test.contains, result)
		}
		if strings.Contains(result, test.notFound) {
			t.Errorf("Expected '%s' NOT in redacted output, got: %s", test.notFound, result)
		}
	}
}

func TestRedactionPatterns_BearerToken(t *testing.T) {
	rp := NewRedactionPatterns()

	input := "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0"
	result := rp.RedactString(input)

	if !strings.Contains(result, "[REDACTED]") {
		t.Errorf("Bearer token not redacted in: %s", result)
	}

	if strings.Contains(result, "eyJhbGciOi") {
		t.Errorf("JWT token leaked in output: %s", result)
	}
}

func TestRedactionPatterns_AWSCredentials(t *testing.T) {
	rp := NewRedactionPatterns()

	input := "AWS Access Key: AKIAIOSFODNN7EXAMPLE"
	result := rp.RedactString(input)

	if !strings.Contains(result, "[REDACTED]") {
		t.Errorf("AWS credential not redacted in: %s", result)
	}

	if strings.Contains(result, "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("AWS key leaked in output: %s", result)
	}
}

func TestRedactionPatterns_DatabasePassword(t *testing.T) {
	rp := NewRedactionPatterns()

	tests := []string{
		"password=MySecureP@ssw0rd",
		"passwd=secret123",
		"pwd=database_pwd_xyz",
		"postgresql://user:MySecureP@ssw0rd@localhost/db",
	}

	for _, input := range tests {
		result := rp.RedactString(input)
		if strings.Contains(result, "MySecureP@ssw0rd") || strings.Contains(result, "secret123") || strings.Contains(result, "database_pwd_xyz") {
			t.Errorf("Password not redacted in: %s -> %s", input, result)
		}
	}
}

func TestRedactionPatterns_PrivateKey(t *testing.T) {
	rp := NewRedactionPatterns()

	input := `-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA2Z2KjW9kXyQ5...private key content...
-----END RSA PRIVATE KEY-----`

	result := rp.RedactString(input)

	if !strings.Contains(result, "[REDACTED]") {
		t.Errorf("Private key not redacted")
	}
}

func TestRedactionPatterns_RedactError(t *testing.T) {
	rp := NewRedactionPatterns()

	err := errors.New("failed to connect with password=MySecretPassword123")
	redacted := rp.RedactError(err)

	if strings.Contains(redacted, "MySecretPassword123") {
		t.Errorf("Password leaked in error: %s", redacted)
	}

	if !strings.Contains(redacted, "[REDACTED]") {
		t.Errorf("Error not redacted: %s", redacted)
	}
}

func TestShouldLogField_SensitiveFields(t *testing.T) {
	sensitiveFields := []string{
		"password",
		"api_key",
		"apiKey",
		"secret",
		"token",
		"credential",
		"auth_token",
		"jwt",
		"vault_token",
		"aws_access_key",
	}

	for _, field := range sensitiveFields {
		if ShouldLogField(field) {
			t.Errorf("Field '%s' should be marked as sensitive", field)
		}
	}
}

func TestShouldLogField_SafeFields(t *testing.T) {
	safeFields := []string{
		"container_id",
		"image_name",
		"status",
		"error_code",
		"hostname",
		"port",
		"version",
	}

	for _, field := range safeFields {
		if !ShouldLogField(field) {
			t.Errorf("Field '%s' should be marked as safe", field)
		}
	}
}

func TestSafeConfigValue_SensitiveKeys(t *testing.T) {
	tests := []struct {
		key   string
		value interface{}
		want  string
	}{
		{
			key:   "password",
			value: "MySecret123",
			want:  "[REDACTED]",
		},
		{
			key:   "api_key",
			value: "sk-abcd1234",
			want:  "[REDACTED]",
		},
		{
			key:   "vault_token",
			value: "s.xxxxxxxxxxxxxxxx",
			want:  "[REDACTED]",
		},
	}

	for _, test := range tests {
		result := SafeConfigValue(test.key, test.value)
		if result != test.want {
			t.Errorf("SafeConfigValue(%q, %q) = %v, want %q", test.key, test.value, result, test.want)
		}
	}
}

func TestSafeConfigValue_SafeKeys(t *testing.T) {
	tests := []struct {
		key   string
		value interface{}
	}{
		{
			key:   "container_id",
			value: "abc123def456",
		},
		{
			key:   "port",
			value: 5432,
		},
		{
			key:   "hostname",
			value: "localhost",
		},
	}

	for _, test := range tests {
		result := SafeConfigValue(test.key, test.value)
		if result != test.value {
			t.Errorf("SafeConfigValue(%q, %v) = %v, expected unchanged", test.key, test.value, result)
		}
	}
}

func TestRedactStructFields_MixedSensitivity(t *testing.T) {
	input := map[string]interface{}{
		"container_id": "abc123",
		"password":     "SecretPassword",
		"status":       "running",
		"api_key":      "sk-xyz789",
	}

	result := RedactStructFields(input)

	// Safe fields should be unchanged
	if result["container_id"] != "abc123" {
		t.Errorf("Safe field 'container_id' should not be redacted")
	}

	if result["status"] != "running" {
		t.Errorf("Safe field 'status' should not be redacted")
	}

	// Sensitive fields should be redacted
	if result["password"] != "[REDACTED]" {
		t.Errorf("Sensitive field 'password' should be redacted, got %v", result["password"])
	}

	if result["api_key"] != "[REDACTED]" {
		t.Errorf("Sensitive field 'api_key' should be redacted, got %v", result["api_key"])
	}
}

func TestRedaction_NoFalsePositives(t *testing.T) {
	rp := NewRedactionPatterns()

	// Normal log messages should not be overly redacted
	normalMessages := []string{
		"container started successfully",
		"listening on port 8080",
		"connection established",
		"secret rotation completed",
	}

	for _, msg := range normalMessages {
		result := rp.RedactString(msg)
		if result != msg {
			t.Errorf("Normal message incorrectly redacted: %q -> %q", msg, result)
		}
	}
}

func TestRedaction_EdgeCases(t *testing.T) {
	rp := NewRedactionPatterns()

	// Empty string
	if rp.RedactString("") != "" {
		t.Error("Empty string should remain empty")
	}

	// String with only sensitive markers
	result := rp.RedactString("password=")
	if !strings.Contains(result, "[REDACTED]") {
		t.Errorf("Marker at end should still trigger redaction: %s", result)
	}

	// Case insensitivity
	result = rp.RedactString("PASSWORD=secret")
	if !strings.Contains(result, "[REDACTED]") {
		t.Errorf("Case-insensitive redaction failed: %s", result)
	}
}
