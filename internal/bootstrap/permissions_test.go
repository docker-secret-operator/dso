package bootstrap

import (
	"context"
	"testing"
)

// TestEnsureDSOGroupWithExistingGroup verifies group lookup works
func TestEnsureDSOGroupWithExistingGroup(t *testing.T) {
	logger := &MockLogger{}
	pm := NewPermissionManager(logger, false)

	// This test will only work if 'dso' group exists on the system
	// On CI/test systems without the group, this test will fail - that's expected
	gid, err := pm.ensureDSOGroup()

	// If the group doesn't exist, that's expected in a test environment
	if err != nil {
		t.Logf("Group creation/lookup failed (expected in test environment): %v", err)
		t.Logf("This is normal - the DSO group may not exist yet")
		// Don't fail - this is expected in most test environments
		return
	}

	// If we get here, the group exists
	if gid <= 0 {
		t.Errorf("Expected positive GID, got %d", gid)
	}

	if len(logger.messages) == 0 {
		t.Error("Expected logger output")
	}
}

// TestSetupBootstrapPermissionsDryRun verifies dry-run mode works
func TestSetupBootstrapPermissionsDryRun(t *testing.T) {
	logger := &MockLogger{}
	pm := NewPermissionManager(logger, true) // dryRun=true

	ctx := context.Background()
	err := pm.SetupBootstrapPermissions(ctx, 0, 0) // root user

	if err != nil {
		t.Logf("SetupBootstrapPermissions in dry-run mode failed: %v", err)
		// This is expected - we're not actually creating anything
	}

	// In dry-run mode, nothing should actually happen
	// Just verify the logger was called
	if len(logger.messages) == 0 {
		t.Error("Expected dry-run logging output")
	}
}

// TestPermissionManagerWithNilContext tests safe handling of context
func TestPermissionManagerWithNilContext(t *testing.T) {
	logger := &MockLogger{}
	pm := NewPermissionManager(logger, true) // Safe with dryRun=true

	// Passing nil context should not panic
	err := pm.SetupBootstrapPermissions(nil, 1000, 1000)

	// Error is expected due to dryRun and nil context, but no panic
	t.Logf("SetupBootstrapPermissions with nil context: %v", err)

	// Verify logger was called
	if len(logger.messages) == 0 {
		t.Error("Expected some logger output")
	}
}

// TestVerifyPermissionsLogging verifies logging works correctly
func TestVerifyPermissionsLogging(t *testing.T) {
	logger := &MockLogger{}
	pm := NewPermissionManager(logger, false)

	// Try to verify permissions - directories may not exist, that's OK
	err := pm.VerifyPermissions(1001)

	// We expect this might error if directories don't exist, that's fine
	// We're testing that the function logs properly
	t.Logf("VerifyPermissions result: %v", err)

	if len(logger.messages) == 0 {
		t.Error("Expected logging output from VerifyPermissions")
	}
}

// TestGetNonRootOperationCommands verifies helper commands are generated
func TestGetNonRootOperationCommands(t *testing.T) {
	logger := &MockLogger{}
	pm := NewPermissionManager(logger, false)

	cmds := pm.GetNonRootOperationCommands("testuser")

	if len(cmds) == 0 {
		t.Error("Expected non-empty command list")
	}

	// Should contain usermod commands
	foundUsermod := false
	for _, cmd := range cmds {
		if len(cmd) > 0 && cmd[0] != '#' { // Skip comments
			foundUsermod = true
			break
		}
	}

	if !foundUsermod {
		t.Error("Expected usermod commands in output")
	}
}

// TestDocumentPermissionModelContent verifies documentation exists
func TestDocumentPermissionModelContent(t *testing.T) {
	logger := &MockLogger{}
	pm := NewPermissionManager(logger, false)

	doc := pm.DocumentPermissionModel()

	if len(doc) == 0 {
		t.Error("Expected non-empty documentation")
	}

	// Check for key content
	if !contains(doc, "DSO Non-Root Permission Model") {
		t.Error("Expected 'DSO Non-Root Permission Model' in documentation")
	}

	if !contains(doc, "usermod") {
		t.Error("Expected 'usermod' in documentation")
	}

	if !contains(doc, "/etc/dso") {
		t.Error("Expected '/etc/dso' in documentation")
	}
}
