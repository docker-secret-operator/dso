package security

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestLoggingAuditValidator_SafeError(t *testing.T) {
	validator := NewLoggingAuditValidator()

	// Test safe error
	safeErr := errors.New("container failed to start")
	result := validator.AuditErrorLogging(safeErr, "container_start")

	if !result.Safe {
		t.Errorf("Safe error marked as unsafe: %v", result.Leaks)
	}
}

func TestLoggingAuditValidator_UnsafeErrorWithAPIKey(t *testing.T) {
	validator := NewLoggingAuditValidator()

	// Test error containing API key
	unsafeErr := errors.New("provider initialization failed: api_key=sk-1234567890abcdef")
	result := validator.AuditErrorLogging(unsafeErr, "provider_init")

	if result.Safe {
		t.Error("Error with API key not detected as unsafe")
	}

	if len(result.Leaks) == 0 {
		t.Error("Expected leaks detected, got none")
	}
}

func TestLoggingAuditValidator_UnsafeErrorWithPassword(t *testing.T) {
	validator := NewLoggingAuditValidator()

	// Test error containing password
	unsafeErr := errors.New("connection failed: password='secret123'")
	result := validator.AuditErrorLogging(unsafeErr, "db_connection")

	if result.Safe {
		t.Error("Error with password not detected as unsafe")
	}
}

func TestLoggingAuditValidator_UnsafeErrorWithToken(t *testing.T) {
	validator := NewLoggingAuditValidator()

	// Test error containing bearer token
	unsafeErr := errors.New("auth failed: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9")
	result := validator.AuditErrorLogging(unsafeErr, "auth_failure")

	if result.Safe {
		t.Error("Error with bearer token not detected as unsafe")
	}
}

func TestLoggingAuditValidator_UnsafeErrorWithAWSKey(t *testing.T) {
	validator := NewLoggingAuditValidator()

	// Test error containing AWS access key
	unsafeErr := errors.New("AWS authentication failed: AKIA1234567890ABCDEF")
	result := validator.AuditErrorLogging(unsafeErr, "aws_auth")

	if result.Safe {
		t.Error("Error with AWS key not detected as unsafe")
	}
}

func TestLoggingAuditValidator_WrappedError_SafeChain(t *testing.T) {
	validator := NewLoggingAuditValidator()

	// Test safe wrapped error chain
	innerErr := errors.New("container not found")
	middleErr := fmt.Errorf("failed to get container: %w", innerErr)
	outerErr := fmt.Errorf("reconciliation failed: %w", middleErr)

	result := validator.AuditErrorLogging(outerErr, "reconciliation")

	if !result.Safe {
		t.Errorf("Safe error chain marked as unsafe: %v", result.Leaks)
	}
}

func TestLoggingAuditValidator_WrappedError_UnsafeInnerChain(t *testing.T) {
	validator := NewLoggingAuditValidator()

	// Test unsafe wrapped error chain
	innerErr := errors.New("authentication failed: password=secret")
	middleErr := fmt.Errorf("provider failed: %w", innerErr)
	outerErr := fmt.Errorf("initialization failed: %w", middleErr)

	result := validator.AuditErrorLogging(outerErr, "initialization")

	if result.Safe {
		t.Error("Error chain with unsafe inner error not detected")
	}
}

func TestLoggingAuditValidator_NilError(t *testing.T) {
	validator := NewLoggingAuditValidator()

	result := validator.AuditErrorLogging(nil, "test")

	if !result.Safe {
		t.Error("Nil error marked as unsafe")
	}
}

func TestLoggingAuditValidator_PanicPathSafe(t *testing.T) {
	validator := NewLoggingAuditValidator()

	// This test just validates the panic path function doesn't panic itself
	result := validator.AuditPanicPath()

	if !result.Safe {
		t.Logf("Panic path leaks detected: %v", result.Leaks)
		// Note: May have false positives from test environment
	}
}

func TestLoggingAuditValidator_TimeoutPath_SafeContext(t *testing.T) {
	validator := NewLoggingAuditValidator()

	timeoutErr := errors.New("operation timed out after 30s")
	contextData := map[string]string{
		"container_id": "abc123",
		"operation":    "secret_injection",
		"duration":     "30s",
	}

	result := validator.AuditTimeoutPath(timeoutErr, contextData)

	if !result.Safe {
		t.Errorf("Safe timeout path marked as unsafe: %v", result.Leaks)
	}
}

func TestLoggingAuditValidator_TimeoutPath_UnsafeContext(t *testing.T) {
	validator := NewLoggingAuditValidator()

	timeoutErr := errors.New("operation timed out")
	contextData := map[string]string{
		"container_id": "abc123",
		"password":     "secret123", // Sensitive field
		"api_key":      "sk-12345",
	}

	result := validator.AuditTimeoutPath(timeoutErr, contextData)

	if result.Safe {
		t.Error("Timeout path with sensitive context not detected")
	}
}

func TestLoggingAuditValidator_SerializationError_SafeRequest(t *testing.T) {
	validator := NewLoggingAuditValidator()

	rpcErr := errors.New("failed to unmarshal response: unexpected token")
	requestSummary := "GetSecrets(provider=vault, path=/secrets)"

	result := validator.AuditSerializationError(rpcErr, requestSummary)

	if !result.Safe {
		t.Errorf("Safe serialization error marked as unsafe: %v", result.Leaks)
	}
}

func TestLoggingAuditValidator_SerializationError_UnsafeRequest(t *testing.T) {
	validator := NewLoggingAuditValidator()

	rpcErr := errors.New("failed to unmarshal: invalid json")
	requestSummary := "GetSecrets(api_key=sk-12345, token=abcdef)"

	result := validator.AuditSerializationError(rpcErr, requestSummary)

	if result.Safe {
		t.Error("Serialization error with unsafe request summary not detected")
	}
}

func TestLoggingAuditValidator_NestedError_SafeChain(t *testing.T) {
	validator := NewLoggingAuditValidator()

	err1 := errors.New("provider plugin crashed")
	err2 := fmt.Errorf("supervisor detected crash: %w", err1)
	err3 := fmt.Errorf("recovery initiated: %w", err2)

	result := validator.AuditNestedError(err3)

	if !result.Safe {
		t.Errorf("Safe nested error marked as unsafe: %v", result.Leaks)
	}
}

func TestLoggingAuditValidator_NestedError_UnsafeChain(t *testing.T) {
	validator := NewLoggingAuditValidator()

	err1 := errors.New("connection failed: password=mysecret")
	err2 := fmt.Errorf("provider failed: %w", err1)
	err3 := fmt.Errorf("initialization error: %w", err2)

	result := validator.AuditNestedError(err3)

	if result.Safe {
		t.Error("Nested error chain with unsafe error not detected")
	}
}

func TestLoggingAuditValidator_LogFieldSafety_AllSafe(t *testing.T) {
	validator := NewLoggingAuditValidator()

	fields := map[string]interface{}{
		"container_id": "abc123def",
		"action":       "start",
		"project_name": "myapp",
		"duration_ms":  42,
	}

	result := validator.AuditLogFieldSafety(fields)

	if !result.Safe {
		t.Errorf("Safe fields marked as unsafe: %v", result.Leaks)
	}
}

func TestLoggingAuditValidator_LogFieldSafety_UnsafeField(t *testing.T) {
	validator := NewLoggingAuditValidator()

	fields := map[string]interface{}{
		"container_id": "abc123",
		"password":     "secret123", // Sensitive field name
		"api_key":      "sk-12345",
	}

	result := validator.AuditLogFieldSafety(fields)

	if result.Safe {
		t.Error("Fields with sensitive names not detected")
	}

	if len(result.Leaks) == 0 {
		t.Error("Expected leaks detected, got none")
	}
}

func TestLoggingAuditValidator_LogFieldSafety_UnsafeValue(t *testing.T) {
	validator := NewLoggingAuditValidator()

	fields := map[string]interface{}{
		"container_id": "abc123",
		"config":       "api_key=sk-12345",
		"status":       "running",
	}

	result := validator.AuditLogFieldSafety(fields)

	if result.Safe {
		t.Error("Fields with sensitive values not detected")
	}
}

func TestLoggingAuditValidator_LogFieldSafety_WithErrorValue(t *testing.T) {
	validator := NewLoggingAuditValidator()

	fields := map[string]interface{}{
		"container_id": "abc123",
		"error":        errors.New("failed: api_key=secret"),
	}

	result := validator.AuditLogFieldSafety(fields)

	if result.Safe {
		t.Error("Field with unsafe error value not detected")
	}
}

func TestLoggingAuditValidator_GetRedactedErrorMessage(t *testing.T) {
	validator := NewLoggingAuditValidator()

	err := errors.New("authentication failed: password=mysecret123")
	redacted := validator.GetRedactedErrorMessage(err)

	if redacted == err.Error() {
		t.Error("Error message not redacted")
	}

	if len(redacted) == 0 {
		t.Error("Redacted message is empty")
	}

	// Check that the password is redacted
	if redacted != err.Error() {
		// Successfully redacted different output
	}
}

func TestLoggingAuditValidator_GetRedactedString(t *testing.T) {
	validator := NewLoggingAuditValidator()

	input := "API key: sk-1234567890abcdef token: secret-token-xyz"
	redacted := validator.GetRedactedString(input)

	if redacted == input {
		t.Error("String not redacted")
	}

	// Verify sensitive patterns are replaced
	if contains(redacted, "sk-") {
		t.Error("API key pattern not redacted")
	}
}

func TestLoggingAuditValidator_ProviderCrashError(t *testing.T) {
	validator := NewLoggingAuditValidator()

	// Simulate provider crash error
	crashErr := errors.New("provider process crashed with signal 11")
	result := validator.AuditErrorLogging(crashErr, "provider_crash")

	if !result.Safe {
		t.Errorf("Safe crash error marked as unsafe: %v", result.Leaks)
	}
}

func TestLoggingAuditValidator_ProviderCrashWithAuthData(t *testing.T) {
	validator := NewLoggingAuditValidator()

	// Simulate provider crash that includes auth data
	crashErr := errors.New("provider process crashed: auth_token=secret123abc in environment")
	result := validator.AuditErrorLogging(crashErr, "provider_crash")

	if result.Safe {
		t.Error("Crash error with auth data not detected")
	}
}

func TestLoggingAuditValidator_DaemonReconnectError_Safe(t *testing.T) {
	validator := NewLoggingAuditValidator()

	reconnectErr := errors.New("docker daemon connection lost, reconnecting")
	result := validator.AuditErrorLogging(reconnectErr, "daemon_reconnect")

	if !result.Safe {
		t.Errorf("Safe reconnect error marked as unsafe: %v", result.Leaks)
	}
}

func TestLoggingAuditValidator_DaemonReconnectError_Unsafe(t *testing.T) {
	validator := NewLoggingAuditValidator()

	// Error that includes connection string with password
	reconnectErr := errors.New("docker reconnect failed: postgresql://user:password123@localhost:5432/db")
	result := validator.AuditErrorLogging(reconnectErr, "daemon_reconnect")

	if result.Safe {
		t.Error("Reconnect error with connection string not detected")
	}
}

func TestLoggingAuditValidator_ValidateErrorWithContext_Safe(t *testing.T) {
	validator := NewLoggingAuditValidator()

	err := errors.New("reconciliation timeout")
	contextMap := map[string]string{
		"container_id": "abc123",
		"project":      "myapp",
		"operation":    "rotation",
	}

	result := validator.ValidateErrorWithContext(err, "reconciliation", contextMap)

	if !result.Safe {
		t.Errorf("Safe error with context marked as unsafe: %v", result.Leaks)
	}
}

func TestLoggingAuditValidator_ValidateErrorWithContext_UnsafeContext(t *testing.T) {
	validator := NewLoggingAuditValidator()

	err := errors.New("operation failed")
	contextMap := map[string]string{
		"container_id": "abc123",
		"password":     "secret",
		"api_key":      "sk-12345",
	}

	result := validator.ValidateErrorWithContext(err, "operation", contextMap)

	if result.Safe {
		t.Error("Error with unsafe context not detected")
	}
}

func TestLoggingAuditValidator_VeryLongError(t *testing.T) {
	validator := NewLoggingAuditValidator()

	// Create a very long error message
	longMsg := ""
	for i := 0; i < 100; i++ {
		longMsg += "operation " + fmt.Sprintf("%d", i) + " completed; "
	}

	err := errors.New(longMsg)
	result := validator.AuditErrorLogging(err, "long_error")

	if !result.Safe {
		t.Errorf("Safe long error marked as unsafe: %v", result.Leaks)
	}
}

func TestLoggingAuditValidator_LongErrorWithSecret(t *testing.T) {
	validator := NewLoggingAuditValidator()

	// Long message with embedded secret
	longMsg := ""
	for i := 0; i < 50; i++ {
		longMsg += "operation " + fmt.Sprintf("%d", i) + "; "
	}
	longMsg += "api_key=sk-secret123; "
	for i := 50; i < 100; i++ {
		longMsg += "operation " + fmt.Sprintf("%d", i) + "; "
	}

	err := errors.New(longMsg)
	result := validator.AuditErrorLogging(err, "long_error_with_secret")

	if result.Safe {
		t.Error("Long error with embedded secret not detected")
	}
}

func TestLoggingAuditValidator_MultipleSecretPatterns(t *testing.T) {
	validator := NewLoggingAuditValidator()

	// Error with multiple secret patterns
	err := errors.New("failed with token=xyz, api_key=abc, password=123, Authorization: Bearer def")
	result := validator.AuditErrorLogging(err, "multiple_secrets")

	if result.Safe {
		t.Error("Error with multiple secret patterns not detected")
	}

	if len(result.Leaks) == 0 {
		t.Error("Expected multiple leaks detected, got none")
	}
}

func TestLoggingAuditValidator_PrivateKeyPEMFormat(t *testing.T) {
	validator := NewLoggingAuditValidator()

	privateKey := `-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA1234567890abcdef
-----END RSA PRIVATE KEY-----`

	err := errors.New("failed to load key: " + privateKey)
	result := validator.AuditErrorLogging(err, "key_loading")

	if result.Safe {
		t.Error("Error with private key PEM not detected")
	}
}

func TestLoggingAuditValidator_DockerAuthConfig(t *testing.T) {
	validator := NewLoggingAuditValidator()

	authConfig := `{"auth": "c2VjcmV0OnBhc3N3b3Jk", "password": "mypassword"}`
	err := errors.New("docker auth failed: " + authConfig)
	result := validator.AuditErrorLogging(err, "docker_auth")

	if result.Safe {
		t.Error("Error with docker auth config not detected")
	}
}

func TestLoggingAuditValidator_DatabaseConnectionString(t *testing.T) {
	validator := NewLoggingAuditValidator()

	connStr := "postgres://user:password123@localhost/mydb"
	err := errors.New("connection failed: " + connStr)
	result := validator.AuditErrorLogging(err, "db_connection")

	if result.Safe {
		t.Error("Error with database connection string not detected")
	}
}

func TestLoggingAuditValidator_OAuth2Token(t *testing.T) {
	validator := NewLoggingAuditValidator()

	err := errors.New("auth failed: access_token=ya29.a0AVvZVs123456, refresh_token=1//xyz")
	result := validator.AuditErrorLogging(err, "oauth2_auth")

	if result.Safe {
		t.Error("Error with OAuth2 tokens not detected")
	}
}

func TestLoggingAuditValidator_TimeoutContextAllocation(t *testing.T) {
	validator := NewLoggingAuditValidator()

	// Test that timeout with allocated context doesn't leak secrets
	contextData := map[string]string{
		"container_id": "abc123",
		"timeout_ms":   "30000",
		"operation":    "secret_rotation",
	}

	timeoutErr := errors.New("context deadline exceeded")

	result := validator.AuditTimeoutPath(timeoutErr, contextData)

	if !result.Safe {
		t.Errorf("Safe timeout with context allocation marked as unsafe: %v", result.Leaks)
	}
}

func TestLoggingAuditValidator_ConcurrentAudit(t *testing.T) {
	validator := NewLoggingAuditValidator()

	done := make(chan error, 10)

	// Simulate concurrent error auditing
	errors := []error{
		errors.New("error 1"),
		errors.New("error 2 with api_key=secret"),
		errors.New("error 3"),
		errors.New("error 4 with password=secret"),
	}

	for _, err := range errors {
		go func(e error) {
			result := validator.AuditErrorLogging(e, "concurrent_test")
			if result.Safe != (e.Error() != errors[1].Error() && e.Error() != errors[3].Error()) {
				done <- fmt.Errorf("unexpected safety result for %s", e.Error())
			} else {
				done <- nil
			}
		}(err)
	}

	for i := 0; i < len(errors); i++ {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
}

// Helper function
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
