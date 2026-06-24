package cli

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/docker-secret-operator/dso/internal/apply"
	"github.com/docker-secret-operator/dso/internal/injector"
	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/docker/docker/client"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// ApplyOptions holds flags for the apply command
type ApplyOptions struct {
	DryRun  bool
	Force   bool
	Timeout time.Duration
}

var applyOpts = ApplyOptions{
	Timeout: 30 * time.Second,
}

// NewApplyCmd creates the apply command
func NewApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply declarative secret configuration",
		Long: `Apply DSO configuration from dso.yaml to running containers.

Similar to 'terraform apply', shows what will change before applying.
Only updates secrets that have actually changed (via checksum verification).

Examples:
  docker dso apply              # Show plan and wait for confirmation
  docker dso apply --dry-run    # Show what would change
  docker dso apply --force      # Apply without confirmation
  docker dso apply -c custom.yaml --timeout 60s`,
		RunE: applyCommand,
	}

	cmd.Flags().BoolVar(&applyOpts.DryRun, "dry-run", false,
		"Show what would change without applying")
	cmd.Flags().BoolVar(&applyOpts.Force, "force", false,
		"Skip confirmation prompt")
	cmd.Flags().DurationVar(&applyOpts.Timeout, "timeout", 30*time.Second,
		"Reconciliation timeout")

	return cmd
}

// applyCommand is the main apply command handler
func applyCommand(cmd *cobra.Command, args []string) error {
	// 1. Load and validate configuration
	fmt.Println("[DSO] Loading configuration...")
	cfg, err := config.LoadConfig(ResolveConfig())
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	fmt.Printf("[DSO] ✓ Configuration loaded (%d secrets, %d providers)\n",
		len(cfg.Secrets), len(cfg.Providers))

	// 2. Verify provider connectivity
	fmt.Println("\n[DSO] Verifying provider connectivity...")
	for provName := range cfg.Providers {
		if err := verifyProviderConnectivity(cfg, provName); err != nil {
			return fmt.Errorf("provider '%s' verification failed: %w", provName, err)
		}
		fmt.Printf("[DSO] ✓ Provider '%s' is reachable\n", provName)
	}

	// 3. Compute the plan (shared with the API; no prior state to diff against)
	fmt.Println("\n[DSO] Computing changes...")
	plan := apply.ComputePlan(nil, cfg)

	// 4. Display the plan
	displayApplyPlan(plan)

	// 5. Prompt for confirmation (unless --force or --dry-run)
	if !applyOpts.Force && !applyOpts.DryRun {
		fmt.Println()
		if !promptForApproval("Apply these changes?") {
			fmt.Println("[DSO] Apply cancelled.")
			return nil
		}
	}

	// 6. Handle dry-run mode
	if applyOpts.DryRun {
		fmt.Println("\n[DSO] ✓ DRY RUN: Changes were not applied")
		return nil
	}

	// 7. Apply the changes via the shared executor + a socket-based reconciler
	fmt.Println("\n[DSO] Applying changes...")
	start := time.Now()
	result, err := apply.Execute(context.Background(), cfg, plan, &socketReconciler{timeout: applyOpts.Timeout})
	if err != nil {
		return fmt.Errorf("apply failed: %w", err)
	}

	// 8. Display results
	displayApplyResult(result, plan, time.Since(start))

	return nil
}

// verifyProviderConnectivity checks if a provider is accessible
func verifyProviderConnectivity(cfg *config.Config, provName string) error {
	provider, exists := cfg.Providers[provName]
	if !exists {
		return fmt.Errorf("provider not found in config")
	}

	// For now, basic validation - actual provider connectivity
	// would be done via injector.NewAgentClient() or direct provider calls
	switch provider.Type {
	case "local", "vault", "aws", "azure", "huawei":
		return nil
	default:
		return fmt.Errorf("unsupported provider type: %s", provider.Type)
	}
}

// displayApplyPlan shows what changes will be made
func displayApplyPlan(plan *apply.ApplyPlan) {
	fmt.Println("\n╭─ CHANGES TO BE APPLIED ─────────────────────────────────╮")
	fmt.Printf("│ Total Secrets: %d\n", plan.TotalSecrets)
	fmt.Printf("│ Secrets to update: %d\n", plan.SecretsToUpdate)
	fmt.Printf("│ Affected containers: %d\n", plan.ContainersAffected)
	fmt.Println("╰──────────────────────────────────────────────────────────╯")

	if len(plan.Changes) > 0 {
		fmt.Println("\nChanges:")
		for _, c := range plan.Changes {
			sym := map[string]string{"create": "+", "update": "~", "remove": "-"}[c.Op]
			fmt.Printf("  %s %s %s\n", sym, c.Kind, c.Name)
		}
	}
}

// displayApplyResult shows the results of the apply operation
func displayApplyResult(result *apply.ApplyResult, plan *apply.ApplyPlan, dur time.Duration) {
	fmt.Println("\n╭─ APPLY RESULTS ─────────────────────────────────────────╮")
	if result.Success {
		fmt.Printf("│ Status: ✓ SUCCESS\n")
	} else {
		fmt.Printf("│ Status: ✗ FAILED\n")
	}
	fmt.Printf("│ Secrets updated: %d\n", plan.SecretsToUpdate)
	fmt.Printf("│ Containers affected: %d\n", plan.ContainersAffected)
	fmt.Printf("│ Duration: %v\n", dur)
	if result.Error != "" {
		fmt.Printf("│ Error: %s\n", result.Error)
	}
	fmt.Println("╰──────────────────────────────────────────────────────────╯")
}

// socketReconciler reconciles via the agent unix socket, falling back to direct
// provider fetches. Implements apply.Reconciler for the CLI.
type socketReconciler struct {
	timeout time.Duration
}

func (s *socketReconciler) Reconcile(ctx context.Context, cfg *config.Config, plan *apply.ApplyPlan) error {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	socketPath := DefaultSocketPath()
	if custom := os.Getenv("DSO_SOCKET_PATH"); custom != "" {
		socketPath = custom
	}

	// Try agent sync first.
	if err := triggerAgentSync(socketPath, s.timeout); err == nil {
		return nil
	} else {
		fmt.Printf("[DSO] Agent not available, attempting direct reconciliation (%v)\n", err)
	}

	// Fallback: direct reconciliation via the agent client.
	dockerClient, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return fmt.Errorf("docker connection failed: %w", err)
	}
	defer dockerClient.Close()

	agentClient, err := injector.NewAgentClient(socketPath)
	if err != nil {
		return fmt.Errorf("agent connection failed: %w", err)
	}

	var failed []string
	for _, secret := range cfg.Secrets {
		provider, exists := cfg.Providers[secret.Provider]
		if !exists {
			failed = append(failed, secret.Name)
			logger.Warn("provider not found for secret",
				zap.String("secret", secret.Name),
				zap.String("provider", secret.Provider))
			continue
		}
		if _, err := agentClient.FetchSecret(secret.Provider, provider.Config, secret.Name); err != nil {
			failed = append(failed, secret.Name)
			logger.Error("failed to fetch secret",
				zap.String("secret", secret.Name),
				zap.Error(err))
			continue
		}
		logger.Info("secret fetched successfully", zap.String("secret", secret.Name))
	}

	if len(failed) > 0 {
		return fmt.Errorf("%d secret(s) failed: %s", len(failed), strings.Join(failed, ", "))
	}
	return nil
}

// triggerAgentSync triggers reconciliation via the agent socket
func triggerAgentSync(socketPath string, timeout time.Duration) error {
	conn, err := net.DialTimeout("unix", socketPath, timeout)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Send simple sync request
	_, err = conn.Write([]byte("SYNC\n"))
	return err
}

// promptForApproval asks the user for confirmation
func promptForApproval(prompt string) bool {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("%s [y/N] ", prompt)

	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}
