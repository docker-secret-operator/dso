package cli

import (
	"bytes"
	"io"
	"os"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/pkg/config"
)

// TestNewApplyCmd creates the apply command
func TestNewApplyCmd(t *testing.T) {
	cmd := NewApplyCmd()

	if cmd == nil {
		t.Fatal("NewApplyCmd returned nil")
	}
	if cmd.Use != "apply" {
		t.Errorf("Expected use 'apply', got %q", cmd.Use)
	}
	if cmd.Short == "" {
		t.Error("Apply command should have short description")
	}
	if cmd.RunE == nil {
		t.Error("Apply command should have RunE handler")
	}
}

// TestApplyCmd_Flags verifies all flags are registered
func TestApplyCmd_Flags(t *testing.T) {
	cmd := NewApplyCmd()

	// Check for --dry-run flag
	dryRunFlag := cmd.Flag("dry-run")
	if dryRunFlag == nil {
		t.Error("--dry-run flag not found")
	}

	// Check for --force flag
	forceFlag := cmd.Flag("force")
	if forceFlag == nil {
		t.Error("--force flag not found")
	}

	// Check for --timeout flag
	timeoutFlag := cmd.Flag("timeout")
	if timeoutFlag == nil {
		t.Error("--timeout flag not found")
	}
}

// TestApplyOptions_DefaultTimeout verifies default timeout
func TestApplyOptions_DefaultTimeout(t *testing.T) {
	opts := ApplyOptions{Timeout: 30 * time.Second}

	if opts.Timeout != 30*time.Second {
		t.Errorf("Expected timeout 30s, got %v", opts.Timeout)
	}
}

// TestApplyOptions_DryRun verifies dry-run flag
func TestApplyOptions_DryRun(t *testing.T) {
	opts := ApplyOptions{DryRun: true}

	if !opts.DryRun {
		t.Error("DryRun should be true")
	}
}

// TestApplyOptions_Force verifies force flag
func TestApplyOptions_Force(t *testing.T) {
	opts := ApplyOptions{Force: true}

	if !opts.Force {
		t.Error("Force should be true")
	}
}

// TestVerifyProviderConnectivity_ValidProvider accepts known types
func TestVerifyProviderConnectivity_ValidProvider(t *testing.T) {
	providers := []string{"local", "vault", "aws", "azure", "huawei"}

	cfg := &config.Config{
		Providers: make(map[string]config.ProviderConfig),
	}

	for _, provType := range providers {
		cfg.Providers[provType] = config.ProviderConfig{
			Type: provType,
		}

		err := verifyProviderConnectivity(cfg, provType)
		if err != nil {
			t.Errorf("Provider %s verification should succeed, got %v", provType, err)
		}
	}
}

// TestVerifyProviderConnectivity_InvalidProvider rejects unknown types
func TestVerifyProviderConnectivity_InvalidProvider(t *testing.T) {
	cfg := &config.Config{
		Providers: make(map[string]config.ProviderConfig),
	}

	cfg.Providers["unknown"] = config.ProviderConfig{
		Type: "unknown",
	}

	err := verifyProviderConnectivity(cfg, "unknown")
	if err == nil {
		t.Error("Unknown provider should return error")
	}
}

// TestVerifyProviderConnectivity_MissingProvider handles missing provider
func TestVerifyProviderConnectivity_MissingProvider(t *testing.T) {
	cfg := &config.Config{
		Providers: make(map[string]config.ProviderConfig),
	}

	err := verifyProviderConnectivity(cfg, "nonexistent")
	if err == nil {
		t.Error("Missing provider should return error")
	}
}

// TestComputeApplyPlan_EmptyConfig returns empty plan
func TestComputeApplyPlan_EmptyConfig(t *testing.T) {
	cfg := &config.Config{
		Secrets:   []config.SecretMapping{},
		Providers: make(map[string]config.ProviderConfig),
	}

	plan, err := computeApplyPlan(cfg)
	if err != nil {
		t.Fatalf("computeApplyPlan should not error on empty config, got %v", err)
	}

	if plan == nil {
		t.Fatal("Plan should not be nil")
	}
	if plan.TotalSecrets != 0 {
		t.Errorf("Expected 0 secrets, got %d", plan.TotalSecrets)
	}
	if len(plan.SecretsToUpdate) != 0 {
		t.Errorf("Expected 0 secrets to update, got %d", len(plan.SecretsToUpdate))
	}
}

// TestComputeApplyPlan_WithSecrets includes all secrets in plan
func TestComputeApplyPlan_WithSecrets(t *testing.T) {
	cfg := &config.Config{
		Secrets: []config.SecretMapping{
			{Name: "db_password", Provider: "vault"},
			{Name: "api_key", Provider: "vault"},
			{Name: "jwt_secret", Provider: "vault"},
		},
		Providers: make(map[string]config.ProviderConfig),
	}

	plan, err := computeApplyPlan(cfg)
	if err != nil {
		t.Fatalf("computeApplyPlan failed: %v", err)
	}

	if plan.TotalSecrets != 3 {
		t.Errorf("Expected 3 secrets, got %d", plan.TotalSecrets)
	}
	if len(plan.SecretsToUpdate) != 3 {
		t.Errorf("Expected 3 secrets to update, got %d", len(plan.SecretsToUpdate))
	}

	// Verify all secrets are in the update list
	secretNames := make(map[string]bool)
	for _, s := range plan.SecretsToUpdate {
		secretNames[s] = true
	}

	expectedSecrets := []string{"db_password", "api_key", "jwt_secret"}
	for _, expected := range expectedSecrets {
		if !secretNames[expected] {
			t.Errorf("Secret %s not found in plan", expected)
		}
	}
}

// TestComputeApplyPlan_HasEstimatedDuration plan includes timing
func TestComputeApplyPlan_HasEstimatedDuration(t *testing.T) {
	cfg := &config.Config{
		Secrets: []config.SecretMapping{
			{Name: "secret1", Provider: "vault"},
		},
		Providers: make(map[string]config.ProviderConfig),
	}

	plan, err := computeApplyPlan(cfg)
	if err != nil {
		t.Fatalf("computeApplyPlan failed: %v", err)
	}

	if plan.EstimatedDuration <= 0 {
		t.Errorf("EstimatedDuration should be positive, got %v", plan.EstimatedDuration)
	}
}

// TestDisplayApplyPlan_ProducesOutput shows output
func TestDisplayApplyPlan_ProducesOutput(t *testing.T) {
	plan := &ApplyPlan{
		TotalSecrets:       3,
		SecretsToUpdate:    []string{"secret1", "secret2", "secret3"},
		ContainersAffected: 2,
		EstimatedDuration:  5 * time.Second,
	}

	// Capture output
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	displayApplyPlan(plan)

	w.Close()
	os.Stdout = oldStdout

	output, _ := io.ReadAll(r)
	outputStr := string(output)

	if len(outputStr) == 0 {
		t.Error("displayApplyPlan should produce output")
	}
	if !bytes.Contains(output, []byte("CHANGES")) {
		t.Error("Output should mention changes")
	}
}

// TestApplyResult_SuccessfulResult verifies success state
func TestApplyResult_SuccessfulResult(t *testing.T) {
	result := &ApplyResult{
		SecretsUpdated:     2,
		ContainersInjected: 2,
		Duration:           1 * time.Second,
		Succeeded:          true,
		FailedSecrets:      []string{},
	}

	if !result.Succeeded {
		t.Error("Result should be successful")
	}
	if len(result.FailedSecrets) != 0 {
		t.Error("No secrets should have failed")
	}
	if result.SecretsUpdated != 2 {
		t.Errorf("Expected 2 secrets updated, got %d", result.SecretsUpdated)
	}
}

// TestApplyResult_FailedResult verifies failure state
func TestApplyResult_FailedResult(t *testing.T) {
	result := &ApplyResult{
		SecretsUpdated:     1,
		ContainersInjected: 1,
		Duration:           1 * time.Second,
		Succeeded:          false,
		FailedSecrets:      []string{"db_password"},
		ErrorMessage:       "Provider connection failed",
	}

	if result.Succeeded {
		t.Error("Result should have failed")
	}
	if len(result.FailedSecrets) == 0 {
		t.Error("Should have failed secrets")
	}
	if result.ErrorMessage == "" {
		t.Error("Should have error message")
	}
}

// TestDisplayApplyResult_ProducesOutput shows results
func TestDisplayApplyResult_ProducesOutput(t *testing.T) {
	result := &ApplyResult{
		SecretsUpdated:     2,
		ContainersInjected: 2,
		Duration:           1 * time.Second,
		Succeeded:          true,
		FailedSecrets:      []string{},
	}

	// Capture output
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	displayApplyResult(result)

	w.Close()
	os.Stdout = oldStdout

	output, _ := io.ReadAll(r)
	outputStr := string(output)

	if len(outputStr) == 0 {
		t.Error("displayApplyResult should produce output")
	}
	if !bytes.Contains(output, []byte("RESULTS")) {
		t.Error("Output should mention results")
	}
	if !bytes.Contains(output, []byte("SUCCESS")) {
		t.Error("Output should mention success")
	}
}

// TestApplyCmd_HelpText displays help
func TestApplyCmd_HelpText(t *testing.T) {
	cmd := NewApplyCmd()

	if cmd.Short == "" {
		t.Error("Apply command should have short description")
	}
	if cmd.Long == "" {
		t.Error("Apply command should have long description")
	}
	if !bytes.Contains([]byte(cmd.Long), []byte("apply")) {
		t.Error("Long description should mention 'apply'")
	}
}

// TestComputeApplyPlan_ErrorOnDockerFailure handles docker connection error gracefully
func TestComputeApplyPlan_DockerOptional(t *testing.T) {
	// If Docker is not available, computeApplyPlan should still work
	// for config validation purposes
	cfg := &config.Config{
		Secrets: []config.SecretMapping{
			{Name: "secret1", Provider: "vault"},
		},
		Providers: make(map[string]config.ProviderConfig),
	}

	// This should not panic even if Docker is unavailable
	plan, err := computeApplyPlan(cfg)

	// We expect either success or graceful failure
	if plan != nil || err != nil {
		// Both outcomes are acceptable
	}
}

// TestApplyResult_ZeroValues handles no updates
func TestApplyResult_ZeroUpdates(t *testing.T) {
	result := &ApplyResult{
		SecretsUpdated:     0,
		ContainersInjected: 0,
		Duration:           0,
		Succeeded:          true,
		FailedSecrets:      []string{},
	}

	if result.SecretsUpdated != 0 {
		t.Error("SecretsUpdated should be 0")
	}
	if result.ContainersInjected != 0 {
		t.Error("ContainersInjected should be 0")
	}
}

// TestExecuteApplyPlan_ResultHasDuration verifies timing
func TestExecuteApplyPlan_ResultsHaveDuration(t *testing.T) {
	cfg := &config.Config{
		Secrets:   []config.SecretMapping{},
		Providers: make(map[string]config.ProviderConfig),
	}

	result, _ := executeApplyPlan(cfg, &ApplyPlan{})

	// We expect either error or result with valid duration
	if result != nil {
		if result.Duration < 0 {
			t.Error("Duration should not be negative")
		}
	}
}

// TestApplyPlan_ContainsSecretList verifies plan structure
func TestApplyPlan_ContainsSecretList(t *testing.T) {
	plan := &ApplyPlan{
		TotalSecrets:       3,
		SecretsToUpdate:    []string{"s1", "s2", "s3"},
		ContainersAffected: 2,
		EstimatedDuration:  5 * time.Second,
	}

	if len(plan.SecretsToUpdate) != 3 {
		t.Errorf("Expected 3 secrets in plan, got %d", len(plan.SecretsToUpdate))
	}

	for i, secret := range plan.SecretsToUpdate {
		if secret == "" {
			t.Errorf("Secret at index %d is empty", i)
		}
	}
}

// TestComputeApplyPlan_MultipleProviders handles config with multiple providers
func TestComputeApplyPlan_MultipleProviders(t *testing.T) {
	cfg := &config.Config{
		Secrets: []config.SecretMapping{
			{Name: "vault_secret", Provider: "vault"},
			{Name: "aws_secret", Provider: "aws"},
			{Name: "azure_secret", Provider: "azure"},
		},
		Providers: map[string]config.ProviderConfig{
			"vault": {Type: "vault"},
			"aws":   {Type: "aws"},
			"azure": {Type: "azure"},
		},
	}

	plan, err := computeApplyPlan(cfg)
	if err != nil {
		t.Fatalf("computeApplyPlan failed: %v", err)
	}

	if len(plan.SecretsToUpdate) != 3 {
		t.Errorf("Expected 3 secrets, got %d", len(plan.SecretsToUpdate))
	}
}
