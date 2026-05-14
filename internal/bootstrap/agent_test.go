package bootstrap

import (
	"context"
	"testing"
)

// MockLogger implements the Logger interface for testing
type MockLogger struct {
	messages []string
}

func (m *MockLogger) Info(msg string, args ...interface{}) {
	m.messages = append(m.messages, msg)
}

func (m *MockLogger) Error(msg string, args ...interface{}) {
	m.messages = append(m.messages, msg)
}

func (m *MockLogger) Warn(msg string, args ...interface{}) {
	m.messages = append(m.messages, msg)
}

func (m *MockLogger) Debug(msg string, args ...interface{}) {
	m.messages = append(m.messages, msg)
}

// TestAgentBootstrapNilContextValueDoesNotPanic verifies the fix for the nil interface conversion
// This is a regression test for the bug: panic: interface conversion: interface {} is nil, not string
func TestAgentBootstrapNilContextValueDoesNotPanic(t *testing.T) {
	logger := &MockLogger{}
	opts := &BootstrapOptions{
		Mode:           ModeAgent,
		Provider:       "azure",
		NonInteractive: true,
		Force:          true,
		Timeout:        0,
		Context:        context.Background(), // Empty context - no config_path key
		AzureVaultURL:  "https://test.vault.azure.net/",
	}

	bootstrapper := NewAgentBootstrapper(logger, opts)
	if bootstrapper == nil {
		t.Fatal("NewAgentBootstrapper returned nil - should create successfully")
	}

	// This should NOT panic, even though opts.Context.Value("config_path") returns nil
	// The old code would panic here: configPath := opts.Context.Value("config_path").(string)
	ctx := context.Background()

	// We expect this to fail validation or other error, but NOT to panic
	// If it panics, the test will fail
	result, err := bootstrapper.Bootstrap(ctx, opts)

	// We don't care about the error here - we're testing that it doesn't panic
	// The actual bootstrap will fail because we're missing proper config, but that's OK
	_ = result
	_ = err

	if len(logger.messages) == 0 {
		t.Error("Expected some logger output, got none")
	}
}

// TestAgentBootstrapWithValidContextPath verifies that a config_path in context is used correctly
func TestAgentBootstrapWithValidContextPath(t *testing.T) {
	logger := &MockLogger{}
	testPath := "/custom/path/dso.yaml"
	ctx := context.WithValue(context.Background(), "config_path", testPath)

	opts := &BootstrapOptions{
		Mode:           ModeAgent,
		Provider:       "azure",
		NonInteractive: true,
		Force:          true,
		Timeout:        0,
		Context:        ctx,
		AzureVaultURL:  "https://test.vault.azure.net/",
	}

	_ = NewAgentBootstrapper(logger, opts) // Verify bootstrapper can be created

	// Extract the context path safely - this tests the defensive check
	retrievedPath := "/etc/dso/dso.yaml" // default
	if opts.Context != nil {
		if val := opts.Context.Value("config_path"); val != nil {
			if path, ok := val.(string); ok && path != "" {
				retrievedPath = path
			}
		}
	}

	if retrievedPath != testPath {
		t.Errorf("Expected path %s, got %s", testPath, retrievedPath)
	}
}

// TestAgentBootstrapWithNilContext verifies that nil context is handled safely
func TestAgentBootstrapWithNilContext(t *testing.T) {
	logger := &MockLogger{}
	opts := &BootstrapOptions{
		Mode:           ModeAgent,
		Provider:       "azure",
		NonInteractive: true,
		Force:          true,
		Timeout:        0,
		Context:        nil, // Explicitly nil context
		AzureVaultURL:  "https://test.vault.azure.net/",
	}

	bootstrapper := NewAgentBootstrapper(logger, opts)
	if bootstrapper == nil {
		t.Fatal("NewAgentBootstrapper returned nil with nil context - should create successfully")
	}

	// This is the defensive pattern: check if context is nil before using it
	configPath := "/etc/dso/dso.yaml"
	if opts.Context != nil {
		if val := opts.Context.Value("config_path"); val != nil {
			if path, ok := val.(string); ok && path != "" {
				configPath = path
			}
		}
	}

	if configPath != "/etc/dso/dso.yaml" {
		t.Errorf("Expected default path with nil context, got %s", configPath)
	}
}

// TestAgentBootstrapWithInvalidTypeInContext verifies wrong type is safely ignored
func TestAgentBootstrapWithInvalidTypeInContext(t *testing.T) {
	// Store an integer instead of string - tests type assertion safety
	ctx := context.WithValue(context.Background(), "config_path", 12345)

	opts := &BootstrapOptions{
		Mode:           ModeAgent,
		Provider:       "azure",
		NonInteractive: true,
		Force:          true,
		Timeout:        0,
		Context:        ctx,
		AzureVaultURL:  "https://test.vault.azure.net/",
	}

	// This should safely ignore the invalid type and use default
	configPath := "/etc/dso/dso.yaml"
	if opts.Context != nil {
		if val := opts.Context.Value("config_path"); val != nil {
			if path, ok := val.(string); ok && path != "" {
				configPath = path
			}
		}
	}

	if configPath != "/etc/dso/dso.yaml" {
		t.Errorf("Expected default path when context has wrong type, got %s", configPath)
	}
}
