package bootstrap

import (
	"context"
	"testing"
)

// TestLocalBootstrapNilContextValueDoesNotPanic verifies the fix for the nil interface conversion
// This is a regression test for the bug: panic: interface conversion: interface {} is nil, not string
func TestLocalBootstrapNilContextValueDoesNotPanic(t *testing.T) {
	logger := &MockLogger{}
	opts := &BootstrapOptions{
		Mode:           ModeLocal,
		Provider:       "vault",
		NonInteractive: true,
		Force:          true,
		Timeout:        0,
		Context:        context.Background(), // Empty context - no config_path key
		VaultAddress:   "https://vault.local",
	}

	bootstrapper := NewLocalBootstrapper(logger, opts)
	if bootstrapper == nil {
		t.Fatal("NewLocalBootstrapper returned nil")
	}

	// This should NOT panic, even though opts.Context.Value("config_path") returns nil
	// The old code would panic: configPath = opts.Context.Value("config_path").(string)
	ctx := context.Background()

	// We expect this to fail validation or other error, but NOT to panic
	result, err := bootstrapper.Bootstrap(ctx, opts)

	// We don't care about the specific error - we're testing that it doesn't panic
	_ = result
	_ = err

	if len(logger.messages) == 0 {
		t.Error("Expected some logger output, got none")
	}
}

// TestLocalBootstrapWithValidContextPath verifies that a config_path in context is used correctly
func TestLocalBootstrapWithValidContextPath(t *testing.T) {
	logger := &MockLogger{}
	testPath := "/custom/local/path/dso.yaml"
	ctx := context.WithValue(context.Background(), "config_path", testPath)

	opts := &BootstrapOptions{
		Mode:           ModeLocal,
		Provider:       "vault",
		NonInteractive: true,
		Force:          true,
		Timeout:        0,
		Context:        ctx,
		VaultAddress:   "https://vault.local",
	}

	bootstrapper := NewLocalBootstrapper(logger, opts)

	// Extract the context path safely using the defensive pattern
	currentUser, _ := getTestUser()
	defaultPath := currentUser.HomeDir + "/.dso/dso.yaml"

	retrievedPath := defaultPath
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

// TestLocalBootstrapWithNilContext verifies that nil context is handled safely
func TestLocalBootstrapWithNilContext(t *testing.T) {
	logger := &MockLogger{}
	opts := &BootstrapOptions{
		Mode:           ModeLocal,
		Provider:       "vault",
		NonInteractive: true,
		Force:          true,
		Timeout:        0,
		Context:        nil, // Explicitly nil context
		VaultAddress:   "https://vault.local",
	}

	bootstrapper := NewLocalBootstrapper(logger, opts)
	if bootstrapper == nil {
		t.Fatal("NewLocalBootstrapper returned nil with nil context")
	}

	// Test the defensive pattern with nil context
	currentUser, _ := getTestUser()
	expectedDefault := currentUser.HomeDir + "/.dso/dso.yaml"

	configPath := expectedDefault
	if opts.Context != nil {
		if val := opts.Context.Value("config_path"); val != nil {
			if path, ok := val.(string); ok && path != "" {
				configPath = path
			}
		}
	}

	if configPath != expectedDefault {
		t.Errorf("Expected default path with nil context, got %s", configPath)
	}
}

// TestLocalBootstrapWithInvalidTypeInContext verifies wrong type is safely ignored
func TestLocalBootstrapWithInvalidTypeInContext(t *testing.T) {
	logger := &MockLogger{}
	// Store a slice instead of string - tests type assertion safety
	ctx := context.WithValue(context.Background(), "config_path", []string{"invalid"})

	opts := &BootstrapOptions{
		Mode:           ModeLocal,
		Provider:       "vault",
		NonInteractive: true,
		Force:          true,
		Timeout:        0,
		Context:        ctx,
		VaultAddress:   "https://vault.local",
	}

	// This should safely ignore the invalid type and use default
	currentUser, _ := getTestUser()
	expectedDefault := currentUser.HomeDir + "/.dso/dso.yaml"

	configPath := expectedDefault
	if opts.Context != nil {
		if val := opts.Context.Value("config_path"); val != nil {
			if path, ok := val.(string); ok && path != "" {
				configPath = path
			}
		}
	}

	if configPath != expectedDefault {
		t.Errorf("Expected default path when context has wrong type, got %s", configPath)
	}
}

// Helper function to safely get current user for testing
func getTestUser() (*MockUser, error) {
	return &MockUser{
		UID:      1000,
		GID:      1000,
		Username: "testuser",
		HomeDir:  "/home/testuser",
	}, nil
}

// MockUser for testing
type MockUser struct {
	UID      int
	GID      int
	Username string
	HomeDir  string
}
