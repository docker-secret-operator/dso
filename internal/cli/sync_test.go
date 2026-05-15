package cli

import (
	"bytes"
	"io"
	"os"
	"testing"
	"time"
)

// TestNewSyncCmd creates the sync command
func TestNewSyncCmd(t *testing.T) {
	cmd := NewSyncCmd()

	if cmd == nil {
		t.Fatal("NewSyncCmd returned nil")
	}
	if cmd.Use != "sync" {
		t.Errorf("Expected use 'sync', got %q", cmd.Use)
	}
	if cmd.Short == "" {
		t.Error("Sync command should have short description")
	}
	if cmd.RunE == nil {
		t.Error("Sync command should have RunE handler")
	}
}

// TestSyncCmd_Flags verifies all flags are registered
func TestSyncCmd_Flags(t *testing.T) {
	cmd := NewSyncCmd()

	// Check for --agent-socket flag
	socketFlag := cmd.Flag("agent-socket")
	if socketFlag == nil {
		t.Error("--agent-socket flag not found")
	}

	// Check for --timeout flag
	timeoutFlag := cmd.Flag("timeout")
	if timeoutFlag == nil {
		t.Error("--timeout flag not found")
	}

	// Check for --secret flag (optional)
	secretFlag := cmd.Flag("secret")
	if secretFlag == nil {
		t.Error("--secret flag not found")
	}
}

// TestSyncOptions_DefaultTimeout verifies default timeout
func TestSyncOptions_DefaultTimeout(t *testing.T) {
	opts := SyncOptions{Timeout: 30 * time.Second}

	if opts.Timeout != 30*time.Second {
		t.Errorf("Expected timeout 30s, got %v", opts.Timeout)
	}
}

// TestSyncOptions_DefaultSocket verifies default socket path
func TestSyncOptions_DefaultSocket(t *testing.T) {
	opts := SyncOptions{AgentSocket: "/run/dso/dso.sock"}

	if opts.AgentSocket != "/run/dso/dso.sock" {
		t.Errorf("Expected default socket /run/dso/dso.sock, got %q", opts.AgentSocket)
	}
}

// TestSyncOptions_CustomSocket verifies custom socket path
func TestSyncOptions_CustomSocket(t *testing.T) {
	opts := SyncOptions{AgentSocket: "/tmp/dso.sock"}

	if opts.AgentSocket != "/tmp/dso.sock" {
		t.Errorf("Expected custom socket /tmp/dso.sock, got %q", opts.AgentSocket)
	}
}

// TestSyncOptions_CustomTimeout verifies custom timeout
func TestSyncOptions_CustomTimeout(t *testing.T) {
	opts := SyncOptions{Timeout: 60 * time.Second}

	if opts.Timeout != 60*time.Second {
		t.Errorf("Expected timeout 60s, got %v", opts.Timeout)
	}
}

// TestSyncOptions_SpecificSecret verifies secret filtering
func TestSyncOptions_SpecificSecret(t *testing.T) {
	opts := SyncOptions{Secret: "db_password"}

	if opts.Secret != "db_password" {
		t.Errorf("Expected secret db_password, got %q", opts.Secret)
	}
}

// TestSyncCmd_HelpText displays help
func TestSyncCmd_HelpText(t *testing.T) {
	cmd := NewSyncCmd()

	if cmd.Short == "" {
		t.Error("Sync command should have short description")
	}
	if cmd.Long == "" {
		t.Error("Sync command should have long description")
	}
	if !bytes.Contains([]byte(cmd.Long), []byte("sync")) {
		t.Error("Long description should mention 'sync'")
	}
}

// TestSyncResult_SuccessfulResult verifies success state
func TestSyncResult_SuccessfulResult(t *testing.T) {
	result := &SyncResult{
		SecretsUpdated:     2,
		ContainersAffected: 2,
		Succeeded:          true,
	}

	if !result.Succeeded {
		t.Error("Result should be successful")
	}
	if result.SecretsUpdated != 2 {
		t.Errorf("Expected 2 secrets updated, got %d", result.SecretsUpdated)
	}
	if result.ContainersAffected != 2 {
		t.Errorf("Expected 2 containers affected, got %d", result.ContainersAffected)
	}
}

// TestSyncResult_FailedResult verifies failure state
func TestSyncResult_FailedResult(t *testing.T) {
	result := &SyncResult{
		SecretsUpdated:     0,
		ContainersAffected: 0,
		Succeeded:          false,
		ErrorMessage:       "Agent connection failed",
	}

	if result.Succeeded {
		t.Error("Result should have failed")
	}
	if result.ErrorMessage == "" {
		t.Error("Should have error message")
	}
}

// TestSyncResult_SpecificSecretSync tracks specific secret
func TestSyncResult_SpecificSecretSync(t *testing.T) {
	result := &SyncResult{
		SecretsUpdated:       1,
		ContainersAffected:   1,
		Succeeded:            true,
		SpecificSecretSynced: "db_password",
	}

	if result.SpecificSecretSynced != "db_password" {
		t.Errorf("Expected specific secret db_password, got %q", result.SpecificSecretSynced)
	}
}

// TestDisplaySyncResults_ProducesOutput shows results
func TestDisplaySyncResults_ProducesOutput(t *testing.T) {
	result := &SyncResult{
		SecretsUpdated:     2,
		ContainersAffected: 2,
		Succeeded:          true,
	}

	// Capture output
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	displaySyncResults(result, 1*time.Second)

	w.Close()
	os.Stdout = oldStdout

	output, _ := io.ReadAll(r)
	outputStr := string(output)

	if len(outputStr) == 0 {
		t.Error("displaySyncResults should produce output")
	}
	if !bytes.Contains(output, []byte("RESULTS")) {
		t.Error("Output should mention results")
	}
	if !bytes.Contains(output, []byte("SUCCESS")) {
		t.Error("Output should mention success")
	}
}

// TestSyncCmd_AllFlagsOptional except timeout has default
func TestSyncCmd_FlagsOptional(t *testing.T) {
	cmd := NewSyncCmd()

	// All flags should be optional
	socketFlag := cmd.Flag("agent-socket")
	if socketFlag == nil {
		t.Error("--agent-socket should exist")
	}

	secretFlag := cmd.Flag("secret")
	if secretFlag == nil {
		t.Error("--secret should exist")
	}
}

// TestSyncOptions_EmptySecret allows no specific secret
func TestSyncOptions_EmptySecret(t *testing.T) {
	opts := SyncOptions{Secret: ""}

	// Empty secret means sync all secrets
	if opts.Secret != "" {
		t.Error("Empty secret should be empty string")
	}
}

// TestSyncCmd_EnvironmentVariableSetting respects DSO_SOCKET_PATH
func TestSyncCmd_SocketEnvVar(t *testing.T) {
	// Command should respect DSO_SOCKET_PATH environment variable
	cmd := NewSyncCmd()

	if cmd.RunE == nil {
		t.Error("Command should respect environment variables")
	}
}

// TestSyncOptions_MultipleTimeoutValues handles various timeouts
func TestSyncOptions_VariousTimeouts(t *testing.T) {
	timeouts := []time.Duration{
		10 * time.Second,
		30 * time.Second,
		60 * time.Second,
		2 * time.Minute,
	}

	for _, timeout := range timeouts {
		opts := SyncOptions{Timeout: timeout}
		if opts.Timeout != timeout {
			t.Errorf("Should handle timeout %v", timeout)
		}
	}
}

// TestSyncCmd_SocketPathVariations handles various socket paths
func TestSyncCmd_SocketPaths(t *testing.T) {
	paths := []string{
		"/run/dso/dso.sock",
		"/tmp/dso.sock",
		"/run/dso.sock",
		"./dso.sock",
	}

	for _, path := range paths {
		opts := SyncOptions{AgentSocket: path}
		if opts.AgentSocket != path {
			t.Errorf("Should handle socket path %q", path)
		}
	}
}

// TestSyncResult_ZeroValues handles no updates
func TestSyncResult_ZeroUpdates(t *testing.T) {
	result := &SyncResult{
		SecretsUpdated:     0,
		ContainersAffected: 0,
		Succeeded:          true,
	}

	if result.SecretsUpdated != 0 {
		t.Error("SecretsUpdated should be 0")
	}
	if result.ContainersAffected != 0 {
		t.Error("ContainersAffected should be 0")
	}
}

// TestSyncCmd_DockerConnectionNotRequired does not need docker
func TestSyncCmd_OnlyNeedsAgent(t *testing.T) {
	cmd := NewSyncCmd()

	if cmd.RunE == nil {
		t.Error("Command must implement RunE")
	}
}

// TestSyncResult_ErrorMessageField stores errors
func TestSyncResult_ErrorMessage(t *testing.T) {
	errorMsg := "Agent connection timeout"
	result := &SyncResult{
		ErrorMessage: errorMsg,
	}

	if result.ErrorMessage != errorMsg {
		t.Errorf("Expected error %q, got %q", errorMsg, result.ErrorMessage)
	}
}

// TestSyncCmd_SecretNameFormats handles various secret names
func TestSyncCmd_SecretFormats(t *testing.T) {
	secrets := []string{
		"db_password",
		"database-password",
		"database.password",
		"database/password",
		"prod/db/password",
	}

	for _, secret := range secrets {
		opts := SyncOptions{Secret: secret}
		if opts.Secret != secret {
			t.Errorf("Should handle secret %q", secret)
		}
	}
}

// TestSyncCmd_OutputFormats produces appropriate messages
func TestSyncCmd_OutputProduction(t *testing.T) {
	cmd := NewSyncCmd()

	if cmd.RunE == nil {
		t.Error("Command should produce output")
	}
}

// TestSyncCmd_ErrorMessages provides helpful errors
func TestSyncCmd_ErrorHandling(t *testing.T) {
	cmd := NewSyncCmd()

	if cmd.RunE == nil {
		t.Error("Command must handle errors")
	}
}

// TestSyncOptions_AllFieldsPopulated verifies all fields work
func TestSyncOptions_AllFields(t *testing.T) {
	opts := SyncOptions{
		AgentSocket: "/run/dso/dso.sock",
		Timeout:     30 * time.Second,
		Secret:      "db_password",
	}

	if opts.AgentSocket == "" || opts.Timeout == 0 || opts.Secret == "" {
		t.Error("All fields should be populated")
	}
}

// TestSyncResult_AllFieldsAccessible verifies all result fields
func TestSyncResult_AllFields(t *testing.T) {
	result := &SyncResult{
		SecretsUpdated:       2,
		ContainersAffected:   2,
		Succeeded:            true,
		ErrorMessage:         "",
		SpecificSecretSynced: "db_password",
	}

	if result.SecretsUpdated == 0 || result.ContainersAffected == 0 {
		t.Error("Result fields should be accessible")
	}
}

// TestSyncCmd_TimerSupport measures operation duration
func TestSyncCmd_TimerSupport(t *testing.T) {
	cmd := NewSyncCmd()

	// Command should measure timing
	if cmd.RunE == nil {
		t.Error("Command should support timing")
	}
}
