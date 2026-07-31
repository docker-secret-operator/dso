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

// TestRedactString verifies secrets are redacted from arbitrary strings
func TestRedactString(t *testing.T) {
	rp := NewRedactionPatterns()

	tests := []struct {
		name             string
		input            string
		shouldNotContain string
	}{
		{
			name:             "generic_api_key",
			input:            "api_key=super_secret_key_12345",
			shouldNotContain: "super_secret_key_12345",
		},
		{
			name:             "vault_token",
			input:            "vault_token: s.xxxxxxxxxxxxxxxx",
			shouldNotContain: "s.xxxxxxxxxxxxxxxx",
		},
		{
			name:             "postgres_url_password",
			input:            "postgresql://user:MySecureP@ssw0rd@localhost:5432/mydb",
			shouldNotContain: "MySecureP@ssw0rd",
		},
		{
			name:             "aws_access_key",
			input:            "AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE",
			shouldNotContain: "AKIAIOSFODNN7EXAMPLE",
		},
		{
			name:             "bearer_token",
			input:            "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			shouldNotContain: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
		},
		{
			name:             "docker_password",
			input:            `{"auth": "dXNlcm5hbWU6cGFzc3dvcmQ="}`,
			shouldNotContain: "dXNlcm5hbWU6cGFzc3dvcmQ=",
		},
		{
			name:             "database_password",
			input:            "password=MyDatabasePassword123!",
			shouldNotContain: "MyDatabasePassword123!",
		},
		{
			name:             "sk_style_api_key",
			input:            "sk-abcd1234efgh5678ijkl9012",
			shouldNotContain: "sk-abcd1234efgh5678ijkl9012",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := rp.RedactString(test.input)

			// Verify secret is NOT in output
			if strings.Contains(result, test.shouldNotContain) {
				t.Errorf("Secret leaked in output. Input: %s, Output: %s", test.input, result)
			}

			// Verify [REDACTED] IS in output
			if !strings.Contains(result, "[REDACTED]") {
				t.Errorf("Expected [REDACTED] in output, got: %s", result)
			}
		})
	}
}

// TestRedactError verifies secrets are redacted from error messages
func TestRedactError(t *testing.T) {
	rp := NewRedactionPatterns()

	tests := []struct {
		name             string
		errMsg           string
		shouldNotContain string
	}{
		{
			name:             "secret_in_error",
			errMsg:           "connection failed with secret: my_super_secret_value_xyz",
			shouldNotContain: "my_super_secret_value_xyz",
		},
		{
			name:             "password_in_error",
			errMsg:           "authentication failed: password=MySecretPassword123",
			shouldNotContain: "MySecretPassword123",
		},
		{
			name:             "api_key_in_error",
			errMsg:           "API request failed with key sk-1234567890abcdefghij",
			shouldNotContain: "sk-1234567890abcdefghij",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := errMsg(test.errMsg)
			redacted := rp.RedactError(err)

			// Secrets don't leak even in errors
			if strings.Contains(redacted, test.shouldNotContain) {
				t.Errorf("Secret leaked in error redaction. Input: %s, Output: %s", test.errMsg, redacted)
			}

			// Verify redaction occurred
			if !strings.Contains(redacted, "[REDACTED]") {
				t.Errorf("Expected [REDACTED] in error redaction, got: %s", redacted)
			}
		})
	}

	// Test nil error
	result := rp.RedactError(nil)
	if result != "" {
		t.Errorf("RedactError(nil) should return empty string, got: %s", result)
	}
}

// TestShouldLogField verifies sensitive field detection
func TestShouldLogField(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		shouldLog bool
	}{
		// Sensitive fields that should NOT be logged
		{name: "password", fieldName: "password", shouldLog: false},
		{name: "api_key", fieldName: "api_key", shouldLog: false},
		{name: "apikey", fieldName: "apikey", shouldLog: false},
		{name: "secret", fieldName: "secret", shouldLog: false},
		{name: "token", fieldName: "token", shouldLog: false},
		{name: "vault_token", fieldName: "vault_token", shouldLog: false},
		{name: "aws_access_key", fieldName: "aws_access_key", shouldLog: false},
		{name: "azure_client_secret", fieldName: "azure_client_secret", shouldLog: false},
		{name: "private_key", fieldName: "private_key", shouldLog: false},
		{name: "jwt", fieldName: "jwt", shouldLog: false},
		{name: "authorization", fieldName: "authorization", shouldLog: false},
		{name: "credential", fieldName: "credential", shouldLog: false},
		{name: "passwd", fieldName: "passwd", shouldLog: false},

		// Safe fields that SHOULD be logged
		{name: "username", fieldName: "username", shouldLog: true},
		{name: "container_id", fieldName: "container_id", shouldLog: true},
		{name: "status", fieldName: "status", shouldLog: true},
		{name: "error_code", fieldName: "error_code", shouldLog: true},
		{name: "hostname", fieldName: "hostname", shouldLog: true},
		{name: "port", fieldName: "port", shouldLog: true},
		{name: "version", fieldName: "version", shouldLog: true},
		{name: "image_name", fieldName: "image_name", shouldLog: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := ShouldLogField(test.fieldName)
			if result != test.shouldLog {
				t.Errorf("ShouldLogField(%q) = %v, want %v", test.fieldName, result, test.shouldLog)
			}
		})
	}
}

// TestRedactStructFields verifies struct field redaction
func TestRedactStructFields(t *testing.T) {
	tests := []struct {
		name        string
		input       map[string]interface{}
		checkFields map[string]interface{}
	}{
		{
			name: "mixed_sensitivity",
			input: map[string]interface{}{
				"container_id": "abc123",
				"password":     "SecretPassword",
				"status":       "running",
				"api_key":      "sk-xyz789",
				"hostname":     "localhost",
				"secret":       "my_secret_value",
				"port":         8080,
				"vault_token":  "s.xxxxx",
			},
			checkFields: map[string]interface{}{
				"container_id": "abc123",     // Should be preserved
				"password":     "[REDACTED]", // Should be redacted
				"status":       "running",    // Should be preserved
				"api_key":      "[REDACTED]", // Should be redacted
				"hostname":     "localhost",  // Should be preserved
				"secret":       "[REDACTED]", // Should be redacted
				"port":         8080,         // Should be preserved
				"vault_token":  "[REDACTED]", // Should be redacted
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := RedactStructFields(test.input)

			// Verify sensitive fields are redacted
			for key, expectedValue := range test.checkFields {
				actualValue := result[key]
				if actualValue != expectedValue {
					t.Errorf("Field %q: expected %v, got %v", key, expectedValue, actualValue)
				}
			}
		})
	}
}

// errMsg creates an error with the given message
func errMsg(msg string) error {
	return errors.New(msg)
}

// redactStringUnguarded is the pre-SEC-1.1 implementation: every pattern's
// ReplaceAllString runs unconditionally. Kept here purely as the reference
// oracle for the differential test below.
func (rp *RedactionPatterns) redactStringUnguarded(input string) string {
	output := input
	for _, pattern := range rp.patterns {
		output = pattern.ReplaceAllString(output, "[REDACTED]")
	}
	return output
}

// TestRedactString_GuardIsEquivalentToUnguarded is the correctness proof for
// the SEC-1.1 optimization. The backlog warned explicitly that "a 'faster'
// redaction path that redacts *less* would be a regression disguised as an
// optimization", so speed alone is not sufficient evidence.
//
// The optimization skips ReplaceAllString when MatchString is false. That is
// equivalence-preserving by definition, and this asserts it directly across
// matching, non-matching and adversarial inputs: the guarded and unguarded
// implementations must produce byte-identical output for every case.
func TestRedactString_GuardIsEquivalentToUnguarded(t *testing.T) {
	rp := NewRedactionPatterns()

	inputs := []string{
		// No-match cases (the ones the guard short-circuits).
		"",
		"container started successfully",
		"listening on port 8080",
		"secret rotation completed for db_password",
		"level=info msg=\"rotation finished\" container_id=abc123",

		// Single-pattern matches.
		`api_key="sk-1234567890abcdef"`,
		"Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
		"AWS Access Key: AKIAIOSFODNN7EXAMPLE",
		"password=MySecureP@ssw0rd",
		"postgresql://user:MySecureP@ssw0rd@localhost/db",
		"vault token hvs.CAESIJlN0aXNhbXBsZXRva2Vu",
		"legacy token s.abcdefghijklmnopqrstuvwx",
		`{"password": "hunter2"}`,
		"access_token=ya29.a0AfH6SMBexample",

		// Multiple patterns in one string — order of application matters, so
		// this is the case most likely to expose a divergence.
		`api_key="sk-abc1234567" and password=hunter2 and AKIAIOSFODNN7EXAMPLE`,
		`Bearer abc.def.ghi password=x api_key=y`,

		// Adversarial / edge shapes.
		"password=",
		"PASSWORD=secret",
		"[REDACTED]",
		"password=[REDACTED]",
		"====",
		"://:@",
	}

	for _, in := range inputs {
		guarded := rp.RedactString(in)
		unguarded := rp.redactStringUnguarded(in)
		if guarded != unguarded {
			t.Errorf("guarded and unguarded redaction diverged\n  input:     %q\n  guarded:   %q\n  unguarded: %q",
				in, guarded, unguarded)
		}
	}
}

// TestRedactString_GuardIsEquivalent_Fuzzy widens the equivalence check beyond
// the hand-picked cases above by assembling inputs from fragments that each
// touch a different pattern, including combinations no single test author
// would think to enumerate.
func TestRedactString_GuardIsEquivalent_Fuzzy(t *testing.T) {
	rp := NewRedactionPatterns()

	fragments := []string{
		"", " ", "plain text ", "container_id=abc ",
		`api_key="sk-1234567890"`, "password=p1 ", "Bearer tok.en.here ",
		"AKIAIOSFODNN7EXAMPLE ", "hvs.CAESIJexampletoken ", "s.abcdefghijklmnopqrstuvwx ",
		`"password": "x"`, "https://u:p@host ", "access_token=abc ",
	}

	for i, a := range fragments {
		for j, b := range fragments {
			for k, c := range fragments {
				in := a + b + c
				if rp.RedactString(in) != rp.redactStringUnguarded(in) {
					t.Fatalf("divergence at [%d,%d,%d] for input %q\n  guarded:   %q\n  unguarded: %q",
						i, j, k, in, rp.RedactString(in), rp.redactStringUnguarded(in))
				}
			}
		}
	}
}
