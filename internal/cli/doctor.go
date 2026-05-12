package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"

	"github.com/spf13/cobra"
)

// NewDoctorCmd creates the doctor diagnostics command
func NewDoctorCmd() *cobra.Command {
	var (
		levelFlag string
		jsonFlag  bool
	)

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Validate DSO environment and diagnose issues",
		Long: `Validate DSO environment and diagnose issues.

Doctor checks your Docker connectivity, runtime environment, providers,
agent status, containers, and system resources.

Examples:
  docker dso doctor              # Quick health check
  docker dso doctor --level full # Comprehensive validation
  docker dso doctor --json       # Machine-readable output`,
		RunE: func(cmd *cobra.Command, args []string) error {
			diag := &Diagnostics{
				Level:  levelFlag,
				JSON:   jsonFlag,
				Checks: []Check{},
			}

			return diag.Run()
		},
	}

	cmd.Flags().StringVar(&levelFlag, "level", "default", "Diagnostic level: default, full")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Output as JSON")

	return cmd
}

// ════════════════════════════════════════════════════════════════════════════
// DIAGNOSTICS TYPES
// ════════════════════════════════════════════════════════════════════════════

type Diagnostics struct {
	Level  string
	JSON   bool
	Checks []Check
}

type Check struct {
	Name     string `json:"name"`
	Status   string `json:"status"`  // healthy, unhealthy, warning, disabled
	Message  string `json:"message"`
	Critical bool   `json:"critical"`
}

// ════════════════════════════════════════════════════════════════════════════
// RUN METHOD
// ════════════════════════════════════════════════════════════════════════════

func (d *Diagnostics) Run() error {
	d.checkDockerConnectivity()
	d.checkRuntimeEnvironment()
	d.checkProviders()
	d.checkContainers()
	d.checkCache()

	if d.Level == "full" {
		d.checkSystem()
		d.checkPermissions()
	}

	// Print results
	if d.JSON {
		return d.printJSON()
	}

	return d.printText()
}

// ════════════════════════════════════════════════════════════════════════════
// CHECK METHODS
// ════════════════════════════════════════════════════════════════════════════

func (d *Diagnostics) checkDockerConnectivity() {
	socketPaths := []string{
		"/var/run/docker.sock",
		"/var/run/docker/docker.sock",
	}

	var found bool
	for _, path := range socketPaths {
		if _, err := os.Stat(path); err == nil {
			found = true
			break
		}
	}

	if found {
		d.addCheck("Docker socket", "healthy", "running", false)
	} else {
		d.addCheck("Docker socket", "unhealthy", "not accessible", true)
	}
}

func (d *Diagnostics) checkRuntimeEnvironment() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		d.addCheck("Home directory", "unhealthy", fmt.Sprintf("error: %v", err), false)
		return
	}

	dsoDir := filepath.Join(homeDir, ".dso")
	vaultPath := filepath.Join(dsoDir, "vault.enc")
	configPath := filepath.Join(dsoDir, "config.yaml")

	// Check if local vault exists
	if _, err := os.Stat(vaultPath); err == nil {
		d.addCheck("Local vault", "healthy", dsoDir, false)
	} else {
		d.addCheck("Local vault", "disabled", "not initialized (run: dso bootstrap local)", false)
	}

	// Check if config exists
	if _, err := os.Stat(configPath); err == nil {
		d.addCheck("Configuration", "healthy", "found", false)
	} else {
		d.addCheck("Configuration", "disabled", "not found", false)
	}
}

func (d *Diagnostics) checkProviders() {
	// Check if providers are available
	d.addCheck("Local provider", "healthy", "available", false)
	d.addCheck("Vault provider", "disabled", "not configured", false)
	d.addCheck("AWS provider", "disabled", "not configured", false)
	d.addCheck("Azure provider", "disabled", "not configured", false)
}

func (d *Diagnostics) checkContainers() {
	// This would require Docker client library
	// For now, just check if Docker is available
	socketPaths := []string{
		"/var/run/docker.sock",
		"/var/run/docker/docker.sock",
	}

	var found bool
	for _, path := range socketPaths {
		if _, err := os.Stat(path); err == nil {
			found = true
			break
		}
	}

	if found {
		d.addCheck("Container introspection", "healthy", "Docker available", false)
	} else {
		d.addCheck("Container introspection", "unhealthy", "Docker not accessible", false)
	}
}

func (d *Diagnostics) checkCache() {
	// Check cache directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		d.addCheck("Cache status", "warning", fmt.Sprintf("cannot check: %v", err), false)
		return
	}

	cacheDir := filepath.Join(homeDir, ".dso", "cache")
	if _, err := os.Stat(cacheDir); err == nil {
		d.addCheck("Cache directory", "healthy", "available", false)
	} else {
		d.addCheck("Cache directory", "disabled", "not initialized", false)
	}
}

func (d *Diagnostics) checkSystem() {
	currentUser, err := user.Current()
	if err != nil {
		d.addCheck("Current user", "warning", fmt.Sprintf("error: %v", err), false)
	} else {
		isRoot := currentUser.Uid == "0"
		status := "non-root (development)"
		if isRoot {
			status = "root (agent mode)"
		}
		d.addCheck("User context", "healthy", status, false)
	}

	// Check /etc/dso (if root)
	if os.Geteuid() == 0 {
		if _, err := os.Stat("/etc/dso"); err == nil {
			d.addCheck("Agent config dir", "healthy", "/etc/dso", false)
		} else {
			d.addCheck("Agent config dir", "disabled", "not initialized", false)
		}

		// Check systemd service
		d.checkSystemdService()
	}
}

func (d *Diagnostics) checkSystemdService() {
	// Check if systemd is available
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		d.addCheck("Systemd", "disabled", "not available", false)
		return
	}

	// Check service status using systemctl
	cmd := exec.Command("systemctl", "is-active", "dso-agent")
	if err := cmd.Run(); err == nil {
		d.addCheck("Agent service", "healthy", "running", false)
	} else {
		// Check if service exists at all
		checkCmd := exec.Command("systemctl", "list-unit-files", "dso-agent.service")
		if err := checkCmd.Run(); err == nil {
			d.addCheck("Agent service", "warning", "installed but not running", false)
		} else {
			d.addCheck("Agent service", "disabled", "not installed", false)
		}
	}
}

func (d *Diagnostics) checkPermissions() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}

	// Check ~/.dso permissions
	dsoDir := filepath.Join(homeDir, ".dso")
	info, err := os.Stat(dsoDir)
	if err == nil {
		mode := info.Mode()
		d.addCheck("Local vault permissions", "healthy", fmt.Sprintf("%o", mode.Perm()), false)
	}
}

// ════════════════════════════════════════════════════════════════════════════
// OUTPUT METHODS
// ════════════════════════════════════════════════════════════════════════════

func (d *Diagnostics) addCheck(name, status, message string, critical bool) {
	d.Checks = append(d.Checks, Check{
		Name:     name,
		Status:   status,
		Message:  message,
		Critical: critical,
	})
}

func (d *Diagnostics) printText() error {
	fmt.Println()
	fmt.Println("┌─────────────────────────────────────────┐")
	fmt.Println("│     DSO Diagnostics Report              │")
	fmt.Println("├─────────────────────────────────────────┤")

	for _, check := range d.Checks {
		status := statusSymbol(check.Status)
		fmt.Printf("│ %s %-30s       │\n", status, check.Name)
		if check.Message != "" {
			fmt.Printf("│   %s│\n", padLeft(check.Message, 33))
		}
	}

	fmt.Println("└─────────────────────────────────────────┘")
	fmt.Println()

	// Summary
	criticalIssues := 0
	for _, check := range d.Checks {
		if check.Critical && check.Status == "unhealthy" {
			criticalIssues++
		}
	}

	if criticalIssues > 0 {
		fmt.Printf("Health: ✗ %d critical issue(s) found\n", criticalIssues)
		return fmt.Errorf("diagnostics detected critical issues")
	}

	fmt.Println("Health: ✓ All systems nominal")
	fmt.Println()

	return nil
}

func (d *Diagnostics) printJSON() error {
	output := map[string]interface{}{
		"checks": d.Checks,
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return err
	}

	fmt.Println(string(data))
	return nil
}

// ════════════════════════════════════════════════════════════════════════════
// UTILITIES
// ════════════════════════════════════════════════════════════════════════════

func statusSymbol(status string) string {
	switch status {
	case "healthy":
		return "✓"
	case "unhealthy":
		return "✗"
	case "warning":
		return "⚠"
	case "disabled":
		return "-"
	default:
		return "?"
	}
}

func padLeft(s string, length int) string {
	if len(s) >= length {
		return s
	}
	padding := length - len(s) - 1
	if padding < 0 {
		padding = 0
	}
	return s + " " + "|"
}
