package cli

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/docker-secret-operator/dso/internal/injector"
	"github.com/docker-secret-operator/dso/pkg/api"
	"github.com/spf13/cobra"
)

// SyncOptions holds flags for the sync command
type SyncOptions struct {
	AgentSocket string
	Timeout     time.Duration
	Secret      string
}

var syncOpts = SyncOptions{
	Timeout: 30 * time.Second,
}

// NewSyncCmd creates the sync command
func NewSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Trigger immediate secret synchronization",
		Long: `Force immediate secret reconciliation without waiting for watcher events.

This command connects to the running DSO agent and triggers an immediate
reconciliation cycle. Useful for:
- Testing configuration changes
- Emergency secret rotations
- Debugging synchronization issues
- Forcing updates without waiting for watcher

The agent must be running (via 'docker dso up' or systemd).

Examples:
  docker dso sync              # Sync all secrets
  docker dso sync --secret db_password  # Sync only specific secret
  docker dso sync --timeout 60s         # Custom timeout`,
		RunE: syncCommand,
	}

	cmd.Flags().StringVar(&syncOpts.AgentSocket, "agent-socket", "/run/dso/dso.sock",
		"Agent socket path")
	cmd.Flags().DurationVar(&syncOpts.Timeout, "timeout", 30*time.Second,
		"Reconciliation timeout")
	cmd.Flags().StringVar(&syncOpts.Secret, "secret", "",
		"Only sync specific secret (optional)")

	return cmd
}

// syncCommand is the main sync command handler
func syncCommand(cmd *cobra.Command, args []string) error {
	// 1. Check for custom socket path in environment
	socketPath := syncOpts.AgentSocket
	if custom := os.Getenv("DSO_SOCKET_PATH"); custom != "" {
		socketPath = custom
	}

	// 2. Connect to agent
	fmt.Printf("[DSO] Connecting to agent at %s...\n", socketPath)
	if err := verifyAgentRunning(socketPath); err != nil {
		return fmt.Errorf("agent not available: %w\n\nEnsure 'docker dso up' or 'dso-agent' is running", err)
	}

	fmt.Println("[DSO] ✓ Agent is running")

	// 3. Create agent client
	fmt.Println("[DSO] Creating client connection...")
	client, err := injector.NewAgentClient(socketPath)
	if err != nil {
		return fmt.Errorf("failed to create agent client: %w", err)
	}
	defer func() { _ = client.Close() }()

	// 4. Trigger reconciliation
	fmt.Println("[DSO] Triggering reconciliation...")
	ctx, cancel := context.WithTimeout(context.Background(), syncOpts.Timeout)
	defer cancel()

	startTime := time.Now()
	result, err := triggerReconciliation(ctx, client, syncOpts.Secret)
	if err != nil {
		return fmt.Errorf("reconciliation failed: %w", err)
	}

	duration := time.Since(startTime)

	// 5. Display results
	displaySyncResults(result, duration)

	if !result.Succeeded {
		return fmt.Errorf("reconciliation did not complete successfully: %s", result.ErrorMessage)
	}
	return nil
}

// verifyAgentRunning checks if agent socket is accessible
func verifyAgentRunning(socketPath string) error {
	// #nosec G704 -- this dials the "unix" network (a local socket file), not
	// a remote host:port; a Unix domain socket dial cannot reach an internal
	// network resource, so SSRF does not apply regardless of socketPath's origin
	conn, err := net.DialTimeout("unix", socketPath, 5*time.Second)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	return nil
}

// SyncResult holds the results of sync operation
type SyncResult struct {
	SecretsChecked       int
	SecretsRotated       int
	Succeeded            bool
	ErrorMessage         string
	SpecificSecretSynced string
	FailedSecrets        []string
}

// triggerReconciliation triggers a real reconciliation via the agent's
// Agent.TriggerReconciliation RPC, which re-fetches the requested secret(s)
// from their providers and rotates any whose value actually changed.
func triggerReconciliation(ctx context.Context, client *injector.AgentClient, specificSecret string) (*SyncResult, error) {
	respCh := make(chan struct {
		resp *api.ReconcileResponse
		err  error
	}, 1)
	go func() {
		resp, err := client.TriggerReconciliation(specificSecret)
		respCh <- struct {
			resp *api.ReconcileResponse
			err  error
		}{resp, err}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("reconciliation timed out: %w", ctx.Err())
	case r := <-respCh:
		if r.err != nil {
			return nil, r.err
		}

		result := &SyncResult{
			SecretsChecked:       r.resp.SecretsChecked,
			SecretsRotated:       r.resp.SecretsRotated,
			SpecificSecretSynced: specificSecret,
		}
		for _, res := range r.resp.Results {
			if res.Error != "" {
				result.FailedSecrets = append(result.FailedSecrets, fmt.Sprintf("%s: %s", res.Secret, res.Error))
			}
		}
		result.Succeeded = len(result.FailedSecrets) == 0
		if !result.Succeeded {
			result.ErrorMessage = fmt.Sprintf("%d secret(s) failed to reconcile", len(result.FailedSecrets))
		}
		if result.SecretsChecked == 0 {
			result.Succeeded = false
			if specificSecret != "" {
				result.ErrorMessage = fmt.Sprintf("secret %q not found in agent config", specificSecret)
			} else {
				result.ErrorMessage = "no secrets configured on the agent"
			}
		}
		return result, nil
	}
}

// displaySyncResults shows the sync operation results
func displaySyncResults(result *SyncResult, duration time.Duration) {
	fmt.Println("\n╭─ RECONCILIATION RESULTS ────────────────────────────────╮")

	if result.Succeeded {
		fmt.Printf("│ Status: ✓ SUCCESS\n")
	} else {
		fmt.Printf("│ Status: ✗ FAILED\n")
	}

	if result.SpecificSecretSynced != "" {
		fmt.Printf("│ Secret checked: %s\n", result.SpecificSecretSynced)
	}
	fmt.Printf("│ Secrets checked: %d\n", result.SecretsChecked)
	fmt.Printf("│ Secrets rotated: %d\n", result.SecretsRotated)

	fmt.Printf("│ Duration: %v\n", duration)

	if result.ErrorMessage != "" {
		fmt.Printf("│ Error: %s\n", result.ErrorMessage)
	}
	for _, f := range result.FailedSecrets {
		fmt.Printf("│ Failed: %s\n", f)
	}

	fmt.Println("╰──────────────────────────────────────────────────────────╯")
}
