package cli

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// sortedKeys returns the keys of a map sorted alphabetically
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// checkPath checks if a path exists and returns a status symbol and error
func checkPath(path string) (string, string) {
	if _, err := os.Stat(path); err == nil {
		return "✓", "exists"
	}
	return "❌ ", "not found"
}

// validateChecksum checks if a file matches the expected hash
func validateChecksum(filepath, expectedHash string) error {
	file, err := os.Open(filepath)
	if err != nil {
		return fmt.Errorf("cannot open file: %w", err)
	}
	defer file.Close()

	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return fmt.Errorf("cannot read file: %w", err)
	}

	actualHash := fmt.Sprintf("%x", h.Sum(nil))
	if actualHash != expectedHash {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actualHash)
	}

	return nil
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()

	if _, err := io.Copy(destination, source); err != nil {
		return err
	}

	return nil
}

// isTerminal checks if stdout is a terminal
func isTerminal() bool {
	// Simple check - can be made more sophisticated
	return os.Getenv("TERM") != ""
}

// validateProviders validates a comma-separated list of providers
func validateProviders(providers string) ([]string, error) {
	if providers == "" {
		return []string{"local"}, nil
	}

	validProviders := map[string]bool{
		"local": true,
		"vault": true,
		"aws":   true,
		"azure": true,
	}

	providerList := strings.Split(providers, ",")
	for _, p := range providerList {
		p = strings.TrimSpace(p)
		if !validProviders[p] {
			return nil, fmt.Errorf("invalid provider: %s", p)
		}
	}

	return providerList, nil
}

// resolveProviders resolves the providers to use, with defaults
func resolveProviders(providers string) ([]string, error) {
	if providers == "" {
		return []string{"local"}, nil
	}
	return validateProviders(providers)
}

// Stub command creators for tests
func newSystemSetupCmd() *cobra.Command {
	var providers []string

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Setup DSO provider plugins",
		Long: `Setup DSO system components like provider plugins.

When running from released binaries, provider plugins may not be installed.
Use this command to manually build and install them from source.

Examples:
  # Install AWS provider plugin
  docker dso system setup --provider aws

  # Install multiple providers
  docker dso system setup --provider aws --provider azure`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return setupProviders(cmd.Context(), providers)
		},
	}

	cmd.Flags().StringSliceVar(&providers, "provider", []string{},
		"Provider(s) to install: aws, azure, vault, huawei (can specify multiple times)")

	return cmd
}

func newSystemDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Doctor command",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}
}

// setupProviders installs provider plugins
func setupProviders(ctx context.Context, providers []string) error {
	if len(providers) == 0 {
		fmt.Println("Error: No providers specified. Use --provider flag")
		fmt.Println("\nExample:")
		fmt.Println("  docker dso system setup --provider aws")
		return fmt.Errorf("no providers specified")
	}

	// Determine plugin directory based on permissions
	var pluginDir string
	if os.Geteuid() == 0 {
		pluginDir = "/usr/local/lib/dso/plugins"
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("could not determine home directory: %w", err)
		}
		pluginDir = filepath.Join(home, ".dso", "plugins")
	}

	fmt.Printf("Installing provider plugins to: %s\n\n", pluginDir)

	// Create plugin directory
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		return fmt.Errorf("failed to create plugin directory: %w", err)
	}

	// Build and install each provider
	validProviders := map[string]bool{"aws": true, "azure": true, "vault": true, "huawei": true}
	successCount := 0
	failureCount := 0

	for _, provider := range providers {
		if !validProviders[provider] {
			fmt.Printf("⚠ Unknown provider: %s (skipped)\n", provider)
			failureCount++
			continue
		}

		if err := buildAndInstallProvider(ctx, provider, pluginDir); err != nil {
			fmt.Printf("✗ Failed to install %s: %v\n", provider, err)
			failureCount++
			continue
		}
		fmt.Printf("✓ Installed: %s\n", provider)
		successCount++
	}

	// Summary
	fmt.Printf("\n%d/%d providers installed\n", successCount, len(providers))

	if failureCount > 0 {
		fmt.Println("\nNote: You can manually build from source:")
		fmt.Printf("  cd /path/to/dso/repo\n")
		fmt.Printf("  go build -o %s/dso-provider-<name> ./cmd/plugins/dso-provider-<name>\n", pluginDir)
		return fmt.Errorf("%d provider(s) failed to install", failureCount)
	}

	return nil
}

// buildAndInstallProvider builds and installs a single provider plugin
func buildAndInstallProvider(ctx context.Context, provider string, pluginDir string) error {
	pluginBinary := filepath.Join(pluginDir, fmt.Sprintf("dso-provider-%s", provider))

	// Check if plugin already exists
	if _, err := os.Stat(pluginBinary); err == nil {
		return nil // Already exists
	}

	// Check if source exists
	cmdDir := filepath.Join("cmd", "plugins", fmt.Sprintf("dso-provider-%s", provider))
	if _, err := os.Stat(cmdDir); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("run this command from the DSO repository root directory")
		}
		return err
	}

	// Build the plugin
	cmd := exec.CommandContext(ctx, "go", "build", "-o", pluginBinary, fmt.Sprintf("./%s", cmdDir))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	// Make executable
	if err := os.Chmod(pluginBinary, 0755); err != nil {
		return fmt.Errorf("chmod failed: %w", err)
	}

	return nil
}
