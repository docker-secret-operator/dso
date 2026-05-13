package cli

import (
	"context"
	"fmt"

	"github.com/docker-secret-operator/dso/internal/bootstrap"
	"github.com/spf13/cobra"
)

// Logger implements the bootstrap.Logger interface
type cliLogger struct{}

func (l *cliLogger) Info(msg string, args ...interface{}) {
	if len(args) > 0 {
		fmt.Printf("[INFO] "+msg+" %v\n", args)
	} else {
		fmt.Printf("[INFO] %s\n", msg)
	}
}

func (l *cliLogger) Error(msg string, args ...interface{}) {
	if len(args) > 0 {
		fmt.Printf("[ERROR] "+msg+" %v\n", args)
	} else {
		fmt.Printf("[ERROR] %s\n", msg)
	}
}

func (l *cliLogger) Warn(msg string, args ...interface{}) {
	if len(args) > 0 {
		fmt.Printf("[WARN] "+msg+" %v\n", args)
	} else {
		fmt.Printf("[WARN] %s\n", msg)
	}
}

func (l *cliLogger) Debug(msg string, args ...interface{}) {
	if len(args) > 0 {
		fmt.Printf("[DEBUG] "+msg+" %v\n", args)
	} else {
		fmt.Printf("[DEBUG] %s\n", msg)
	}
}

// NewBootstrapCmd creates the bootstrap command with subcommands for local and agent modes
func NewBootstrapCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bootstrap [local|agent]",
		Short: "Initialize DSO runtime environment",
		Long: `Initialize DSO for either local development or production agent mode.

Bootstrap creates the runtime directory structure, generates configuration,
initializes encryption, and validates your environment.

Examples:
  docker dso bootstrap local              # For local development
  sudo docker dso bootstrap agent         # For production deployment`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := args[0]

			switch mode {
			case "local":
				return bootstrapLocal()
			case "agent":
				return bootstrapAgent()
			default:
				return fmt.Errorf("invalid mode: %s (expected 'local' or 'agent')", mode)
			}
		},
	}

	return cmd
}

// ════════════════════════════════════════════════════════════════════════════
// LOCAL MODE BOOTSTRAP
// ════════════════════════════════════════════════════════════════════════════

func bootstrapLocal() error {
	fmt.Println()
	fmt.Println("Initializing DSO Local Runtime...")
	fmt.Println()

	// Create bootstrap manager with logger
	logger := &cliLogger{}
	manager := bootstrap.NewBootstrapManager(logger)

	// Create bootstrap options for local mode
	ctx := context.Background()
	opts := &bootstrap.BootstrapOptions{
		Mode:           bootstrap.ModeLocal,
		Provider:       "", // Will prompt user
		NonInteractive: false,
		Force:          false,
		DryRun:         false,
		Timeout:        30 * 60,
		Context:        ctx,
	}

	// Execute with progress reporting
	result, err := manager.BootstrapWithProgress(ctx, opts, func(phase bootstrap.BootstrapPhase, msg string, progress int) {
		fmt.Printf("  [%2d%%] %s\n", progress, msg)
	})

	if err != nil {
		fmt.Println()
		fmt.Printf("✗ Bootstrap failed: %v\n", err)
		return err
	}

	// Print success summary
	fmt.Println()
	fmt.Println("✓ DSO Local Runtime Initialized")
	fmt.Printf("  Configuration: %s\n", result.ConfigPath)
	fmt.Println()

	return nil
}

// ════════════════════════════════════════════════════════════════════════════
// AGENT MODE BOOTSTRAP
// ════════════════════════════════════════════════════════════════════════════

func bootstrapAgent() error {
	fmt.Println()
	fmt.Println("Initializing DSO Agent Runtime...")
	fmt.Println()

	// Create bootstrap manager with logger
	logger := &cliLogger{}
	manager := bootstrap.NewBootstrapManager(logger)

	// Create bootstrap options for agent mode
	ctx := context.Background()
	opts := &bootstrap.BootstrapOptions{
		Mode:           bootstrap.ModeAgent,
		Provider:       "", // Will prompt user or detect
		NonInteractive: false,
		Force:          false,
		DryRun:         false,
		Timeout:        30 * 60,
		Context:        ctx,
	}

	// Execute with progress reporting
	result, err := manager.BootstrapWithProgress(ctx, opts, func(phase bootstrap.BootstrapPhase, msg string, progress int) {
		fmt.Printf("  [%2d%%] %s\n", progress, msg)
	})

	if err != nil {
		fmt.Println()
		fmt.Printf("✗ Bootstrap failed: %v\n", err)
		return err
	}

	// Print success summary
	fmt.Println()
	fmt.Println("✓ DSO Agent Runtime Initialized")
	fmt.Printf("  Configuration: %s\n", result.ConfigPath)
	fmt.Printf("  Service: %s\n", result.ServicePath)
	if result.PermissionsSet {
		fmt.Println("  Permissions: Configured")
	}
	fmt.Println()

	if len(result.Warnings) > 0 {
		fmt.Println("⚠ Important Notes:")
		for _, warning := range result.Warnings {
			fmt.Printf("  - %s\n", warning)
		}
		fmt.Println()
	}

	return nil
}
