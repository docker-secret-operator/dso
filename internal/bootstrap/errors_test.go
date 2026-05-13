package bootstrap

import (
	"fmt"
	"testing"
)

// TestBootstrapError tests error structure and formatting
func TestBootstrapError(t *testing.T) {
	cause := fmt.Errorf("underlying error")
	err := NewBootstrapError("TEST_CODE", "test_phase", "test message", cause)

	if err.Code != "TEST_CODE" {
		t.Errorf("Code mismatch: got %q, want %q", err.Code, "TEST_CODE")
	}

	if err.Phase != "test_phase" {
		t.Errorf("Phase mismatch: got %q, want %q", err.Phase, "test_phase")
	}

	if err.Message != "test message" {
		t.Errorf("Message mismatch: got %q, want %q", err.Message, "test message")
	}

	if err.Cause != cause {
		t.Errorf("Cause mismatch: got %v, want %v", err.Cause, cause)
	}
}

// TestBootstrapErrorFormatting tests error string formatting
func TestBootstrapErrorFormatting(t *testing.T) {
	tests := []struct {
		name     string
		err      *BootstrapError
		wantMsg  string
		wantHave string
	}{
		{
			name:     "error with cause",
			err:      NewBootstrapError("CODE", "phase", "message", fmt.Errorf("cause")),
			wantHave: "[phase] CODE: message",
		},
		{
			name:     "error without cause",
			err:      NewBootstrapError("CODE", "phase", "message", nil),
			wantHave: "[phase] CODE: message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errStr := tt.err.Error()
			if tt.wantHave != "" && !contains(errStr, tt.wantHave) {
				t.Errorf("Error string mismatch: got %q, expected to contain %q", errStr, tt.wantHave)
			}
		})
	}
}

// TestBootstrapErrorUnwrap tests error unwrapping
func TestBootstrapErrorUnwrap(t *testing.T) {
	cause := fmt.Errorf("underlying cause")
	err := NewBootstrapError("CODE", "phase", "message", cause)

	unwrapped := err.Unwrap()
	if unwrapped != cause {
		t.Errorf("Unwrap() mismatch: got %v, want %v", unwrapped, cause)
	}
}

// TestErrorFactory tests error factory functions
func TestErrorFactory(t *testing.T) {
	tests := []struct {
		name      string
		factory   func() *BootstrapError
		wantCode  string
		wantPhase string
	}{
		{
			name:      "ErrDockerUnavailable",
			factory:   func() *BootstrapError { return ErrDockerUnavailable("test", fmt.Errorf("cause")) },
			wantCode:  ErrCodeDockerUnavailable,
			wantPhase: "test",
		},
		{
			name:      "ErrPermissionDenied",
			factory:   func() *BootstrapError { return ErrPermissionDenied("test", "/etc/dso") },
			wantCode:  ErrCodePermissionDenied,
			wantPhase: "test",
		},
		{
			name:      "ErrInvalidProvider",
			factory:   func() *BootstrapError { return ErrInvalidProvider("test", "invalid") },
			wantCode:  ErrCodeInvalidProvider,
			wantPhase: "test",
		},
		{
			name:      "ErrSymlinkDetected",
			factory:   func() *BootstrapError { return ErrSymlinkDetected("test", "/path") },
			wantCode:  ErrCodeSymlinkDetected,
			wantPhase: "test",
		},
		{
			name:      "ErrPathTraversal",
			factory:   func() *BootstrapError { return ErrPathTraversal("test", "/path") },
			wantCode:  ErrCodePathTraversal,
			wantPhase: "test",
		},
		{
			name:      "ErrPathValidation",
			factory:   func() *BootstrapError { return ErrPathValidation("test", "/path", "reason") },
			wantCode:  ErrCodePathValidation,
			wantPhase: "test",
		},
		{
			name:      "ErrConfigValidation",
			factory:   func() *BootstrapError { return ErrConfigValidation("test", "reason") },
			wantCode:  ErrCodeConfigValidation,
			wantPhase: "test",
		},
		{
			name:      "ErrLockAcquisition",
			factory:   func() *BootstrapError { return ErrLockAcquisition("test", fmt.Errorf("cause")) },
			wantCode:  ErrCodeLockAcquisition,
			wantPhase: "test",
		},
		{
			name:      "ErrYAMLGeneration",
			factory:   func() *BootstrapError { return ErrYAMLGeneration("test", fmt.Errorf("cause")) },
			wantCode:  ErrCodeYAMLGeneration,
			wantPhase: "test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.factory()

			if err.Code != tt.wantCode {
				t.Errorf("Code mismatch: got %q, want %q", err.Code, tt.wantCode)
			}

			if err.Phase != tt.wantPhase {
				t.Errorf("Phase mismatch: got %q, want %q", err.Phase, tt.wantPhase)
			}

			if err.Error() == "" {
				t.Fatal("Error() returned empty string")
			}
		})
	}
}

// TestErrorChaining tests error cause chaining
func TestErrorChaining(t *testing.T) {
	cause1 := fmt.Errorf("original error")
	cause2 := fmt.Errorf("wrapper: %w", cause1)
	err := NewBootstrapError("CODE", "phase", "message", cause2)

	// Test Unwrap
	unwrapped := err.Unwrap()
	if unwrapped.Error() != "wrapper: original error" {
		t.Errorf("Unwrap mismatch: got %q", unwrapped.Error())
	}
}
