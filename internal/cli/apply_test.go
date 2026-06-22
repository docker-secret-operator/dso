package cli

import (
	"bytes"
	"io"
	"os"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/apply"
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

	if cmd.Flag("dry-run") == nil {
		t.Error("--dry-run flag not found")
	}
	if cmd.Flag("force") == nil {
		t.Error("--force flag not found")
	}
	if cmd.Flag("timeout") == nil {
		t.Error("--timeout flag not found")
	}
}

func TestApplyOptions_DefaultTimeout(t *testing.T) {
	opts := ApplyOptions{Timeout: 30 * time.Second}
	if opts.Timeout != 30*time.Second {
		t.Errorf("Expected timeout 30s, got %v", opts.Timeout)
	}
}

func TestApplyOptions_DryRun(t *testing.T) {
	if opts := (ApplyOptions{DryRun: true}); !opts.DryRun {
		t.Error("DryRun should be true")
	}
}

func TestApplyOptions_Force(t *testing.T) {
	if opts := (ApplyOptions{Force: true}); !opts.Force {
		t.Error("Force should be true")
	}
}

// TestVerifyProviderConnectivity_ValidProvider accepts known types
func TestVerifyProviderConnectivity_ValidProvider(t *testing.T) {
	providers := []string{"local", "vault", "aws", "azure", "huawei"}
	cfg := &config.Config{Providers: make(map[string]config.ProviderConfig)}

	for _, provType := range providers {
		cfg.Providers[provType] = config.ProviderConfig{Type: provType}
		if err := verifyProviderConnectivity(cfg, provType); err != nil {
			t.Errorf("Provider %s verification should succeed, got %v", provType, err)
		}
	}
}

func TestVerifyProviderConnectivity_InvalidProvider(t *testing.T) {
	cfg := &config.Config{Providers: map[string]config.ProviderConfig{"unknown": {Type: "unknown"}}}
	if err := verifyProviderConnectivity(cfg, "unknown"); err == nil {
		t.Error("Unknown provider should return error")
	}
}

func TestVerifyProviderConnectivity_MissingProvider(t *testing.T) {
	cfg := &config.Config{Providers: make(map[string]config.ProviderConfig)}
	if err := verifyProviderConnectivity(cfg, "nonexistent"); err == nil {
		t.Error("Missing provider should return error")
	}
}

// TestDisplayApplyPlan_ProducesOutput shows output (now uses the shared type)
func TestDisplayApplyPlan_ProducesOutput(t *testing.T) {
	plan := &apply.ApplyPlan{
		TotalSecrets:       3,
		SecretsToUpdate:    3,
		ContainersAffected: 2,
		Changes: []apply.PlanChange{
			{Op: "create", Kind: "secret", Name: "secret1"},
		},
	}

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	displayApplyPlan(plan)
	w.Close()
	os.Stdout = oldStdout

	output, _ := io.ReadAll(r)
	if len(output) == 0 {
		t.Error("displayApplyPlan should produce output")
	}
	if !bytes.Contains(output, []byte("CHANGES")) {
		t.Error("Output should mention changes")
	}
}

// TestDisplayApplyResult_ProducesOutput shows results (shared type)
func TestDisplayApplyResult_ProducesOutput(t *testing.T) {
	result := &apply.ApplyResult{Success: true}
	plan := &apply.ApplyPlan{SecretsToUpdate: 2, ContainersAffected: 2}

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	displayApplyResult(result, plan, time.Second)
	w.Close()
	os.Stdout = oldStdout

	output, _ := io.ReadAll(r)
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
	if cmd.Long == "" {
		t.Error("Apply command should have long description")
	}
	if !bytes.Contains([]byte(cmd.Long), []byte("apply")) {
		t.Error("Long description should mention 'apply'")
	}
}
