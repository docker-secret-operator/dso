package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

// NewSystemCmd creates the system management command
func NewSystemCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "system",
		Short: "System-level DSO management",
		Long: `Manage DSO agent and system-level operations.

System provides commands to manage the DSO agent service, view logs, and handle system-level tasks.

Examples:
  docker dso system status              # Show agent service status
  docker dso system enable              # Enable agent service
  docker dso system disable             # Disable agent service
  docker dso system restart             # Restart agent service
  docker dso system logs                # View agent logs`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newSystemStatusCmd())
	cmd.AddCommand(newSystemEnableCmd())
	cmd.AddCommand(newSystemDisableCmd())
	cmd.AddCommand(newSystemRestartCmd())
	cmd.AddCommand(newSystemLogsCmd())

	return cmd
}

// ════════════════════════════════════════════════════════════════════════════
// SYSTEM STATUS SUBCOMMAND
// ════════════════════════════════════════════════════════════════════════════

func newSystemStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show DSO agent service status",
		Long: `Display the current status of the DSO agent systemd service.

Shows whether the service is running, enabled, and any recent errors.

Examples:
  docker dso system status              # Show agent service status`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return showSystemStatus()
		},
	}
}

func showSystemStatus() error {
	// Check if running as root for agent mode
	if os.Geteuid() != 0 {
		return fmt.Errorf("system commands require root privileges (run with sudo)")
	}

	fmt.Println()
	fmt.Println("┌──────────────────────────────────────┐")
	fmt.Println("│   DSO Agent Service Status           │")
	fmt.Println("├──────────────────────────────────────┤")

	// Get service status
	cmd := exec.Command("systemctl", "show", "dso-agent", "--property=ActiveState,SubState,ExecMainStatus")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println("│ Service:    not found                │")
		fmt.Println("│ Status:     ✗ not installed         │")
	} else {
		fmt.Printf("│ Service:    dso-agent                │\n")
		fmt.Printf("│ Status:     ✓ running                │\n")
		fmt.Printf("│ Output:     %s│\n", string(output)[:30])
	}

	// Check if enabled
	enableCmd := exec.Command("systemctl", "is-enabled", "dso-agent")
	if enableCmd.Run() == nil {
		fmt.Println("│ Enabled:    ✓ yes                    │")
	} else {
		fmt.Println("│ Enabled:    - no                     │")
	}

	// Get recent logs
	logsCmd := exec.Command("journalctl", "-u", "dso-agent", "-n", "1", "--no-pager")
	if logsOutput, err := logsCmd.CombinedOutput(); err == nil {
		logsStr := string(logsOutput)
		if len(logsStr) > 34 {
			logsStr = logsStr[:34]
		}
		fmt.Printf("│ Last log:   %s│\n", logsStr)
	}

	fmt.Println("└──────────────────────────────────────┘")
	fmt.Println()

	return nil
}

// ════════════════════════════════════════════════════════════════════════════
// SYSTEM ENABLE SUBCOMMAND
// ════════════════════════════════════════════════════════════════════════════

func newSystemEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable",
		Short: "Enable DSO agent service",
		Long: `Enable and start the DSO agent systemd service.

The service will start automatically on boot and restart on failure.

Examples:
  sudo docker dso system enable              # Enable and start agent`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return enableSystemService()
		},
	}
}

func enableSystemService() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("system commands require root privileges (run with sudo)")
	}

	fmt.Println()
	fmt.Print("  Enabling DSO agent service... ")

	cmd := exec.Command("systemctl", "enable", "--now", "dso-agent")
	if err := cmd.Run(); err != nil {
		fmt.Println("✗")
		return fmt.Errorf("failed to enable service: %w", err)
	}

	fmt.Println("✓")
	fmt.Println()
	fmt.Println("✓ DSO agent service enabled and started")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  docker dso system status              # Check service status")
	fmt.Println("  docker dso system logs                # View service logs")
	fmt.Println("  docker dso doctor                     # Validate environment")
	fmt.Println()

	return nil
}

// ════════════════════════════════════════════════════════════════════════════
// SYSTEM DISABLE SUBCOMMAND
// ════════════════════════════════════════════════════════════════════════════

func newSystemDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Disable DSO agent service",
		Long: `Disable and stop the DSO agent systemd service.

The service will no longer start automatically on boot.

Examples:
  sudo docker dso system disable              # Disable and stop agent`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return disableSystemService()
		},
	}
}

func disableSystemService() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("system commands require root privileges (run with sudo)")
	}

	fmt.Println()
	fmt.Print("  Disabling DSO agent service... ")

	cmd := exec.Command("systemctl", "disable", "--now", "dso-agent")
	if err := cmd.Run(); err != nil {
		fmt.Println("✗")
		return fmt.Errorf("failed to disable service: %w", err)
	}

	fmt.Println("✓")
	fmt.Println()
	fmt.Println("✓ DSO agent service disabled and stopped")
	fmt.Println()

	return nil
}

// ════════════════════════════════════════════════════════════════════════════
// SYSTEM RESTART SUBCOMMAND
// ════════════════════════════════════════════════════════════════════════════

func newSystemRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart DSO agent service",
		Long: `Restart the DSO agent systemd service.

Useful for applying configuration changes or recovering from errors.

Examples:
  sudo docker dso system restart              # Restart agent service`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return restartSystemService()
		},
	}
}

func restartSystemService() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("system commands require root privileges (run with sudo)")
	}

	fmt.Println()
	fmt.Print("  Restarting DSO agent service... ")

	cmd := exec.Command("systemctl", "restart", "dso-agent")
	if err := cmd.Run(); err != nil {
		fmt.Println("✗")
		return fmt.Errorf("failed to restart service: %w", err)
	}

	fmt.Println("✓")
	fmt.Println()
	fmt.Println("✓ DSO agent service restarted")
	fmt.Println()

	return nil
}

// ════════════════════════════════════════════════════════════════════════════
// SYSTEM LOGS SUBCOMMAND
// ════════════════════════════════════════════════════════════════════════════

func newSystemLogsCmd() *cobra.Command {
	var (
		lines     int
		follow    bool
		priority  string
		sinceflag string
	)

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "View DSO agent service logs",
		Long: `Display DSO agent service logs from the systemd journal.

Use -f/--follow to monitor logs in real-time.
Use -n/--lines to control how many log lines to display.
Use -p/--priority to filter by log level (alert, crit, err, warning, notice, info, debug).

Examples:
  docker dso system logs                      # Show recent logs
  docker dso system logs -f                   # Follow logs in real-time
  docker dso system logs -n 50                # Show last 50 lines
  docker dso system logs -p err               # Show only errors`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return showSystemLogs(lines, follow, priority, sinceflag)
		},
	}

	cmd.Flags().IntVarP(&lines, "lines", "n", 20, "Number of log lines to display")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow logs in real-time (Ctrl+C to exit)")
	cmd.Flags().StringVarP(&priority, "priority", "p", "", "Filter by priority (alert, crit, err, warning, notice, info, debug)")
	cmd.Flags().StringVarP(&sinceflag, "since", "S", "", "Show logs since (e.g., '1h', '30m', '1h 30m')")

	return cmd
}

func showSystemLogs(lines int, follow bool, priority, since string) error {
	args := []string{"-u", "dso-agent", "--no-pager"}

	if follow {
		args = append(args, "-f")
	} else {
		args = append(args, "-n", fmt.Sprintf("%d", lines))
	}

	if priority != "" {
		args = append(args, "-p", priority)
	}

	if since != "" {
		args = append(args, "--since", since)
	}

	cmd := exec.Command("journalctl", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to show logs: %w", err)
	}

	return nil
}
