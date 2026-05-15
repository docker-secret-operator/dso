package bootstrap

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ProviderPluginInstaller handles building and installing provider plugins
type ProviderPluginInstaller struct {
	logger Logger
	dryRun bool
}

// NewProviderPluginInstaller creates a new provider plugin installer
func NewProviderPluginInstaller(logger Logger, dryRun bool) *ProviderPluginInstaller {
	return &ProviderPluginInstaller{
		logger: logger,
		dryRun: dryRun,
	}
}

// InstallProviderPlugins builds and installs the required provider plugins
func (ppi *ProviderPluginInstaller) InstallProviderPlugins(ctx context.Context, providers []string) error {
	if len(providers) == 0 {
		ppi.logger.Info("No providers to install")
		return nil
	}

	// Determine plugin install directory
	pluginDir := "/usr/local/lib/dso/plugins"

	if ppi.dryRun {
		ppi.logger.Info("DRY_RUN: Would install provider plugins",
			"providers", providers,
			"directory", pluginDir)
		return nil
	}

	// Ensure plugin directory exists
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		return fmt.Errorf("failed to create plugin directory %s: %w", pluginDir, err)
	}
	ppi.logger.Info("Plugin directory ready", "path", pluginDir)

	// Build and install each provider plugin
	for _, provider := range providers {
		if err := ppi.buildAndInstallPlugin(ctx, provider, pluginDir); err != nil {
			ppi.logger.Warn("Failed to install provider plugin, continuing anyway",
				"provider", provider, "error", err.Error())
			// Don't fail bootstrap if a single provider plugin fails
			// User can manually install later
			continue
		}
		ppi.logger.Info("Provider plugin installed", "provider", provider, "path", pluginDir)
	}

	return nil
}

// buildAndInstallPlugin builds and installs a single provider plugin
func (ppi *ProviderPluginInstaller) buildAndInstallPlugin(ctx context.Context, provider string, pluginDir string) error {
	// Map provider name to plugin command directory
	cmdDir := filepath.Join("cmd", "plugins", fmt.Sprintf("dso-provider-%s", provider))

	// Check if plugin source exists
	if _, err := os.Stat(cmdDir); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("provider plugin source not found: %s", cmdDir)
		}
		return fmt.Errorf("failed to stat plugin source: %w", err)
	}

	pluginBinary := filepath.Join(pluginDir, fmt.Sprintf("dso-provider-%s", provider))
	ppi.logger.Info("Building provider plugin", "provider", provider)

	// Build the plugin binary
	cmd := exec.CommandContext(ctx, "go", "build", "-o", pluginBinary, fmt.Sprintf("./%s", cmdDir))
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to build provider plugin %s: %w", provider, err)
	}

	// Make it executable
	if err := os.Chmod(pluginBinary, 0755); err != nil {
		return fmt.Errorf("failed to chmod plugin binary: %w", err)
	}

	ppi.logger.Info("Provider plugin built successfully",
		"provider", provider,
		"path", pluginBinary)

	return nil
}

// GetRequiredProviders returns list of providers to install based on configuration
func GetRequiredProviders(providers map[string]ProviderConfig) []string {
	seen := make(map[string]bool)
	var required []string

	for _, cfg := range providers {
		if !seen[cfg.Type] {
			required = append(required, cfg.Type)
			seen[cfg.Type] = true
		}
	}

	return required
}
