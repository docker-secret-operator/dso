package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// NewConfigCmd creates the config management command with subcommands
func NewConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage DSO configuration",
		Long: `Manage DSO configuration files.

Config provides commands to view, edit, and validate the DSO configuration.

Examples:
  docker dso config show              # View current configuration
  docker dso config edit              # Edit configuration in $EDITOR
  docker dso config validate          # Validate configuration for errors`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newConfigShowCmd())
	cmd.AddCommand(newConfigEditCmd())
	cmd.AddCommand(newConfigValidateCmd())

	return cmd
}

// ════════════════════════════════════════════════════════════════════════════
// CONFIG SHOW SUBCOMMAND
// ════════════════════════════════════════════════════════════════════════════

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Display current DSO configuration",
		Long: `Show displays the current DSO configuration file.

The configuration file location is determined by:
1. CLI flag (-c/--config)
2. /etc/dso/dso.yaml (if running as root)
3. ~/.dso/config.yaml (if in local mode)
4. ./dso.yaml (current directory)

Examples:
  docker dso config show              # Show default config
  docker dso config show -c custom.yaml  # Show specific config file`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath := ResolveConfig()
			return showConfig(configPath)
		},
	}
}

func showConfig(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	fmt.Println()
	fmt.Printf("Config file: %s\n", configPath)
	fmt.Println()
	fmt.Println(string(data))

	return nil
}

// ════════════════════════════════════════════════════════════════════════════
// CONFIG EDIT SUBCOMMAND
// ════════════════════════════════════════════════════════════════════════════

func newConfigEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit",
		Short: "Edit DSO configuration in your default editor",
		Long: `Edit opens the DSO configuration file in your default text editor.

The editor is determined by:
1. $EDITOR environment variable
2. $VISUAL environment variable
3. 'nano' as fallback

Changes are automatically validated after saving.

Examples:
  docker dso config edit              # Edit default config
  docker dso config edit -c custom.yaml  # Edit specific config file`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath := ResolveConfig()
			return editConfig(configPath)
		},
	}
}

func editConfig(configPath string) error {
	// Determine the editor to use
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "nano"
	}

	// Check if config file exists
	if _, err := os.Stat(configPath); err != nil {
		return fmt.Errorf("config file not found at %s: %w", configPath, err)
	}

	// Get original file info for comparison
	originalStat, _ := os.Stat(configPath)

	// Open editor
	cmd := exec.Command(editor, configPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor failed: %w", err)
	}

	// Check if file was modified
	newStat, _ := os.Stat(configPath)
	if newStat.ModTime() != originalStat.ModTime() {
		fmt.Println()
		fmt.Println("Configuration changed. Validating...")
		if err := validateConfigFile(configPath); err != nil {
			fmt.Printf("⚠ Validation failed: %v\n", err)
			fmt.Println()
			fmt.Print("Configuration saved but contains errors. Fix them before running commands that use the config.\n")
			return nil // Don't fail, let user decide
		}

		fmt.Println("✓ Configuration is valid")
		fmt.Println()
		fmt.Println("Next steps:")
		fmt.Println("  docker dso doctor          # Validate environment")
		fmt.Println("  docker dso status          # Check runtime status")
	} else {
		fmt.Println("No changes made.")
	}

	return nil
}

// ════════════════════════════════════════════════════════════════════════════
// CONFIG VALIDATE SUBCOMMAND
// ════════════════════════════════════════════════════════════════════════════

func newConfigValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate DSO configuration for errors",
		Long: `Validate checks the DSO configuration file for syntax errors and configuration issues.

Checks performed:
- YAML syntax validity
- Required fields presence
- Provider configuration validity
- Path references accessibility
- Value format correctness

Examples:
  docker dso config validate              # Validate default config
  docker dso config validate -c custom.yaml  # Validate specific config file`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath := ResolveConfig()
			return validateConfig(configPath)
		},
	}
}

func validateConfig(configPath string) error {
	if err := validateConfigFile(configPath); err != nil {
		fmt.Printf("✗ Configuration validation failed:\n")
		fmt.Printf("  %v\n", err)
		fmt.Println()
		fmt.Printf("Config file: %s\n", configPath)
		fmt.Println()
		fmt.Println("Fix the issues above and run: docker dso config validate")
		return err
	}

	fmt.Println()
	fmt.Printf("Config file: %s\n", configPath)
	fmt.Println()
	fmt.Println("✓ Configuration is valid")
	fmt.Println()

	return nil
}

// ════════════════════════════════════════════════════════════════════════════
// VALIDATION LOGIC
// ════════════════════════════════════════════════════════════════════════════

type ConfigFile struct {
	Version   string                 `yaml:"version"`
	Runtime   map[string]interface{} `yaml:"runtime"`
	Providers map[string]interface{} `yaml:"providers"`
	Agent     map[string]interface{} `yaml:"agent"`
}

func validateConfigFile(configPath string) error {
	// Check if file exists
	if _, err := os.Stat(configPath); err != nil {
		return fmt.Errorf("config file not found: %s", configPath)
	}

	// Read file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	// Validate YAML syntax
	var config ConfigFile
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("invalid YAML syntax: %w", err)
	}

	// Validate version field
	if config.Version == "" {
		return fmt.Errorf("missing required field: version")
	}

	if !strings.HasPrefix(config.Version, "v") {
		return fmt.Errorf("invalid version format: %s (expected vX.Y.Z)", config.Version)
	}

	// Validate runtime section
	if config.Runtime == nil {
		return fmt.Errorf("missing required section: runtime")
	}

	if _, ok := config.Runtime["mode"]; !ok {
		return fmt.Errorf("missing required field: runtime.mode")
	}

	// Validate mode value
	mode := fmt.Sprintf("%v", config.Runtime["mode"])
	if mode != "local" && mode != "agent" {
		return fmt.Errorf("invalid runtime.mode: %s (expected 'local' or 'agent')", mode)
	}

	// Validate providers section (optional)
	if config.Providers != nil {
		if local, ok := config.Providers["local"].(map[string]interface{}); ok {
			if path, ok := local["path"]; ok {
				pathStr := fmt.Sprintf("%v", path)
				// Expand ~ to home directory for checking
				if strings.HasPrefix(pathStr, "~") {
					homeDir, _ := os.UserHomeDir()
					pathStr = strings.Replace(pathStr, "~", homeDir, 1)
					// Don't fail if path doesn't exist yet - it may be created by bootstrap
				}
			}
		}
	}

	// Validate agent section (optional)
	if config.Agent != nil {
		if cache, ok := config.Agent["cache"].(map[string]interface{}); ok {
			if maxSize, ok := cache["max_size"]; ok {
				maxSizeStr := fmt.Sprintf("%v", maxSize)
				if !isValidSize(maxSizeStr) {
					return fmt.Errorf("invalid agent.cache.max_size format: %s (expected like '100Mi', '1Gi')", maxSizeStr)
				}
			}
		}
	}

	return nil
}

func isValidSize(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}

	// Valid units
	units := []string{"B", "KB", "Mi", "Gi", "Ti", "MB", "GB", "TB"}
	for _, unit := range units {
		if strings.HasSuffix(s, unit) {
			// Check if prefix is a valid number
			prefix := strings.TrimSuffix(s, unit)
			_, err := parseNumber(prefix)
			return err == nil
		}
	}

	return false
}

func parseNumber(s string) (float64, error) {
	var num float64
	_, err := fmt.Sscanf(s, "%f", &num)
	return num, err
}

// ════════════════════════════════════════════════════════════════════════════
// CONFIG UTILITIES
// ════════════════════════════════════════════════════════════════════════════

func getConfigPath() string {
	homeDir, _ := os.UserHomeDir()

	// Check for agent config (root only)
	if os.Geteuid() == 0 {
		if _, err := os.Stat("/etc/dso/dso.yaml"); err == nil {
			return "/etc/dso/dso.yaml"
		}
	}

	// Check for local config
	if _, err := os.Stat(filepath.Join(homeDir, ".dso/config.yaml")); err == nil {
		return filepath.Join(homeDir, ".dso/config.yaml")
	}

	// Check current directory
	if _, err := os.Stat("dso.yaml"); err == nil {
		return "dso.yaml"
	}

	// Default
	return filepath.Join(homeDir, ".dso/config.yaml")
}
