package security

import (
	"errors"
	"fmt"
	"regexp"
	"runtime/debug"
	"strings"
)

// LoggingAuditResult represents the result of logging audit validation
type LoggingAuditResult struct {
	Safe    bool
	Leaks   []string // Found leakable patterns
	Message string
}

// LoggingAuditValidator performs runtime validation of logging safety
type LoggingAuditValidator struct {
	redactor   *RedactionPatterns
	stackRegex *regexp.Regexp
}

// NewLoggingAuditValidator creates a new logging audit validator
func NewLoggingAuditValidator() *LoggingAuditValidator {
	return &LoggingAuditValidator{
		redactor:   NewRedactionPatterns(),
		stackRegex: regexp.MustCompile(`(?i)(password|secret|token|key|credential|auth)[^\n]*\n`),
	}
}

// AuditErrorLogging validates that error logging doesn't leak secrets
// Used to audit wrapped errors from provider initialization failures, daemon reconnects, etc.
func (lav *LoggingAuditValidator) AuditErrorLogging(err error, context string) *LoggingAuditResult {
	if err == nil {
		return &LoggingAuditResult{Safe: true, Message: "nil error"}
	}

	result := &LoggingAuditResult{Safe: true}

	// Check the direct error message
	errMsg := err.Error()
	if !lav.isSafeForLogging(errMsg) {
		result.Safe = false
		result.Leaks = append(result.Leaks, fmt.Sprintf("direct error message (%s): contains sensitive pattern", context))
	}

	// Check wrapped errors in chain
	current := err
	for current != nil {
		if !lav.isSafeForLogging(current.Error()) {
			result.Safe = false
			msg := current.Error()
			if len(msg) > 50 {
				msg = msg[:50]
			}
			result.Leaks = append(result.Leaks, fmt.Sprintf("wrapped error (%s): %s", context, msg))
		}

		// Unwrap to next error
		current = errors.Unwrap(current)
	}

	if result.Safe {
		result.Message = fmt.Sprintf("Error logging safe for %s", context)
	} else {
		result.Message = fmt.Sprintf("ERROR: Potential secret leak in %s logging", context)
	}

	return result
}

// AuditPanicPath validates that panic recovery doesn't expose secrets in stack traces
func (lav *LoggingAuditValidator) AuditPanicPath() *LoggingAuditResult {
	defer func() {
		if r := recover(); r != nil {
			// Panic was caught - stack trace captured
		}
	}()

	result := &LoggingAuditResult{Safe: true}

	// Get current stack trace
	stackTrace := string(debug.Stack())

	// Check for sensitive patterns in stack trace
	if !lav.isSafeForLogging(stackTrace) {
		result.Safe = false
		result.Leaks = append(result.Leaks, "Stack trace contains sensitive patterns")
	}

	// Check for common panic-related sensitive fields
	sensitiveStack := []string{
		"password=", "secret=", "token=", "apikey=", "api_key=",
		"Authorization:", "auth:", "credentials=", "credential=",
	}

	for _, sensitive := range sensitiveStack {
		if strings.Contains(stackTrace, sensitive) {
			result.Safe = false
			result.Leaks = append(result.Leaks, fmt.Sprintf("Stack trace contains %q", sensitive))
		}
	}

	if result.Safe {
		result.Message = "Panic path is safe for logging"
	}

	return result
}

// AuditTimeoutPath validates that timeout error messages don't expose context secrets
func (lav *LoggingAuditValidator) AuditTimeoutPath(timeoutErr error, contextData map[string]string) *LoggingAuditResult {
	result := &LoggingAuditResult{Safe: true}

	// Check timeout error itself
	if timeoutErr != nil && !lav.isSafeForLogging(timeoutErr.Error()) {
		result.Safe = false
		result.Leaks = append(result.Leaks, "Timeout error contains sensitive data")
	}

	// Check context data that might be logged with timeout
	for key, value := range contextData {
		if !ShouldLogField(key) {
			result.Safe = false
			result.Leaks = append(result.Leaks, fmt.Sprintf("Context field %q is sensitive", key))
		}
		if !lav.isSafeForLogging(value) {
			result.Safe = false
			result.Leaks = append(result.Leaks, fmt.Sprintf("Context value for %q contains sensitive data", key))
		}
	}

	if result.Safe {
		result.Message = "Timeout path is safe"
	}

	return result
}

// AuditSerializationError validates that RPC/serialization errors don't leak provider credentials
func (lav *LoggingAuditValidator) AuditSerializationError(err error, requestSummary string) *LoggingAuditResult {
	result := &LoggingAuditResult{Safe: true}

	// Check serialization error
	if err != nil {
		errChain := lav.getErrorChain(err)
		for _, chainErr := range errChain {
			if !lav.isSafeForLogging(chainErr) {
				result.Safe = false
				msg := chainErr
				if len(msg) > 50 {
					msg = msg[:50]
				}
				result.Leaks = append(result.Leaks, fmt.Sprintf("Serialization error: %s", msg))
			}
		}
	}

	// Check request summary doesn't contain auth data
	if requestSummary != "" && !lav.isSafeForLogging(requestSummary) {
		result.Safe = false
		result.Leaks = append(result.Leaks, "Request summary contains sensitive data")
	}

	if result.Safe {
		result.Message = "Serialization error path is safe"
	}

	return result
}

// AuditNestedError validates that nested error chains are safely logged
func (lav *LoggingAuditValidator) AuditNestedError(err error) *LoggingAuditResult {
	result := &LoggingAuditResult{Safe: true}

	errChain := lav.getErrorChain(err)

	for i, chainErr := range errChain {
		if !lav.isSafeForLogging(chainErr) {
			result.Safe = false
			msg := chainErr
			if len(msg) > 50 {
				msg = msg[:50]
			}
			result.Leaks = append(result.Leaks, fmt.Sprintf("Nested error [depth %d]: %s", i, msg))
		}
	}

	if result.Safe {
		result.Message = fmt.Sprintf("Nested error chain (%d levels) is safe", len(errChain))
	}

	return result
}

// AuditLogFieldSafety validates that a set of fields are safe to log
func (lav *LoggingAuditValidator) AuditLogFieldSafety(fields map[string]interface{}) *LoggingAuditResult {
	result := &LoggingAuditResult{Safe: true}

	for key, value := range fields {
		// Check field name sensitivity
		if !ShouldLogField(key) {
			result.Safe = false
			result.Leaks = append(result.Leaks, fmt.Sprintf("Field %q is sensitive", key))
			continue
		}

		// Check field value
		if strVal, ok := value.(string); ok {
			if !lav.isSafeForLogging(strVal) {
				result.Safe = false
				result.Leaks = append(result.Leaks, fmt.Sprintf("Value for field %q contains sensitive pattern", key))
			}
		} else if errVal, ok := value.(error); ok {
			// Recursively check error values
			sub := lav.AuditErrorLogging(errVal, key)
			if !sub.Safe {
				result.Safe = false
				result.Leaks = append(result.Leaks, sub.Leaks...)
			}
		}
	}

	if result.Safe {
		result.Message = fmt.Sprintf("%d fields are safe to log", len(fields))
	}

	return result
}

// GetRedactedErrorMessage returns the error message with secrets redacted
func (lav *LoggingAuditValidator) GetRedactedErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return lav.redactor.RedactError(err)
}

// GetRedactedString returns the string with secrets redacted
func (lav *LoggingAuditValidator) GetRedactedString(input string) string {
	return lav.redactor.RedactString(input)
}

// Private helpers

// isSafeForLogging checks if a string contains sensitive patterns
func (lav *LoggingAuditValidator) isSafeForLogging(str string) bool {
	// Check against all redaction patterns
	for _, pattern := range lav.redactor.patterns {
		if pattern.MatchString(str) {
			return false
		}
	}

	// Additional checks for common sensitive patterns not in regex
	lowerStr := strings.ToLower(str)
	unsafePatterns := []string{
		"authorization: bearer",
		"x-api-key:",
		"x-auth-token:",
		"_auth",
		"session_token=",
		"access_token=",
		"refresh_token=",
	}

	for _, pattern := range unsafePatterns {
		if strings.Contains(lowerStr, pattern) {
			return false
		}
	}

	return true
}

// getErrorChain extracts the full error chain by unwrapping
func (lav *LoggingAuditValidator) getErrorChain(err error) []string {
	var chain []string

	current := err
	for current != nil {
		chain = append(chain, current.Error())
		current = errors.Unwrap(current)
	}

	return chain
}

// ValidateErrorWithContext performs comprehensive error validation with context information
func (lav *LoggingAuditValidator) ValidateErrorWithContext(err error, contextType string, contextMap map[string]string) *LoggingAuditResult {
	result := &LoggingAuditResult{Safe: true}

	// Check error itself
	errorAudit := lav.AuditErrorLogging(err, contextType)
	if !errorAudit.Safe {
		result.Safe = false
		result.Leaks = append(result.Leaks, errorAudit.Leaks...)
	}

	// Check context map
	for key, value := range contextMap {
		if !ShouldLogField(key) {
			result.Safe = false
			result.Leaks = append(result.Leaks, fmt.Sprintf("Context field %q is sensitive (%s)", key, contextType))
		}
		if !lav.isSafeForLogging(value) {
			result.Safe = false
			result.Leaks = append(result.Leaks, fmt.Sprintf("Context value for %q contains sensitive data (%s)", key, contextType))
		}
	}

	if result.Safe {
		result.Message = fmt.Sprintf("Error with context (%s) is safe to log", contextType)
	} else {
		result.Message = fmt.Sprintf("ERROR: Potential secret leak in %s logging", contextType)
	}

	return result
}
