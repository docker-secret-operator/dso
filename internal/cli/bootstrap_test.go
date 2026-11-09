package cli

import (
	"context"
	"testing"
)

// TestBuildLocalBootstrapOptions_PropagatesProviderFlags is the P1-1
// regression test: `dso bootstrap local --non-interactive --provider <X>
// <X-specific flag>` failed for every non-trivial provider because
// bootstrapLocal() built its BootstrapOptions without copying the
// provider-specific CLI flags (bootstrapAWSRegion, bootstrapAzureVaultURL,
// bootstrapHuaweiRegion, bootstrapHuaweiProjectID, bootstrapVaultAddress)
// into it -- unlike bootstrapAgent(), which always did. In non-interactive
// mode, internal/bootstrap/local.go treats a missing opts.<Field> exactly
// like the flag was never passed and returns e.g. "Vault address required",
// even though the operator supplied it on the command line.
func TestBuildLocalBootstrapOptions_PropagatesProviderFlags(t *testing.T) {
	// Save and restore the package-level flag vars so this test can't leak
	// state into any other test that runs in this package/process.
	origProvider := bootstrapProvider
	origNonInteractive := bootstrapNonInteractive
	origAWSRegion := bootstrapAWSRegion
	origAzureVaultURL := bootstrapAzureVaultURL
	origHuaweiRegion := bootstrapHuaweiRegion
	origHuaweiProjectID := bootstrapHuaweiProjectID
	origVaultAddress := bootstrapVaultAddress
	t.Cleanup(func() {
		bootstrapProvider = origProvider
		bootstrapNonInteractive = origNonInteractive
		bootstrapAWSRegion = origAWSRegion
		bootstrapAzureVaultURL = origAzureVaultURL
		bootstrapHuaweiRegion = origHuaweiRegion
		bootstrapHuaweiProjectID = origHuaweiProjectID
		bootstrapVaultAddress = origVaultAddress
	})

	bootstrapNonInteractive = true

	t.Run("vault", func(t *testing.T) {
		bootstrapProvider = "vault"
		bootstrapVaultAddress = "http://127.0.0.1:8200"
		opts := buildLocalBootstrapOptions(context.Background())
		if opts.VaultAddress != "http://127.0.0.1:8200" {
			t.Fatalf("P1-1 regression: --vault-address not propagated to BootstrapOptions.VaultAddress, got %q", opts.VaultAddress)
		}
		if !opts.NonInteractive {
			t.Fatalf("NonInteractive not propagated")
		}
	})

	t.Run("aws", func(t *testing.T) {
		bootstrapProvider = "aws"
		bootstrapAWSRegion = "eu-west-1"
		opts := buildLocalBootstrapOptions(context.Background())
		if opts.AWSRegion != "eu-west-1" {
			t.Fatalf("P1-1 regression: --aws-region not propagated to BootstrapOptions.AWSRegion, got %q", opts.AWSRegion)
		}
	})

	t.Run("azure", func(t *testing.T) {
		bootstrapProvider = "azure"
		bootstrapAzureVaultURL = "https://example.vault.azure.net"
		opts := buildLocalBootstrapOptions(context.Background())
		if opts.AzureVaultURL != "https://example.vault.azure.net" {
			t.Fatalf("P1-1 regression: --azure-vault-url not propagated to BootstrapOptions.AzureVaultURL, got %q", opts.AzureVaultURL)
		}
	})

	t.Run("huawei", func(t *testing.T) {
		bootstrapProvider = "huawei"
		bootstrapHuaweiRegion = "cn-north-4"
		bootstrapHuaweiProjectID = "proj-123"
		opts := buildLocalBootstrapOptions(context.Background())
		if opts.HuaweiRegion != "cn-north-4" {
			t.Fatalf("P1-1 regression: --huawei-region not propagated to BootstrapOptions.HuaweiRegion, got %q", opts.HuaweiRegion)
		}
		if opts.HuaweiProjectID != "proj-123" {
			t.Fatalf("P1-1 regression: --huawei-project-id not propagated to BootstrapOptions.HuaweiProjectID, got %q", opts.HuaweiProjectID)
		}
	})
}

func TestNewBootstrapCmd(t *testing.T) {
	cmd := NewBootstrapCmd()
	if cmd == nil {
		t.Fatal("expected command, got nil")
	}
	if cmd.Use != "bootstrap [local|agent]" {
		t.Fatalf("expected 'bootstrap [local|agent]', got '%s'", cmd.Use)
	}
}

func TestBootstrapCmd_HelpText(t *testing.T) {
	cmd := NewBootstrapCmd()
	help := cmd.Long
	if help == "" {
		t.Fatal("expected help text")
	}
	if !contains(help, "local development") && !contains(help, "production") {
		t.Fatal("help text missing mode descriptions")
	}
}

func contains(s, substr string) bool {
	for i := 0; i < len(s); i++ {
		if len(s[i:]) < len(substr) {
			return false
		}
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
