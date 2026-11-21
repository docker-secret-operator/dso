package cli

import (
	"context"
	"fmt"

	"github.com/docker-secret-operator/dso/internal/notify"
	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/spf13/cobra"
)

// NewNotificationsCmd creates the notifications command group.
func NewNotificationsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "notifications",
		Short: "Manage rotation-event notification destinations",
		Long: `Manage rotation-event notification destinations.

See docs/notifications.md for the event contract, delivery semantics, and
security model.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newNotificationsTestCmd())
	return cmd
}

func newNotificationsTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test",
		Short: "Send a synthetic test event to every configured notification destination",
		Long: `Send a synthetic test event to every configured notification destination.

Loads dso.yaml's notifications.webhooks, builds a real WebhookNotifier for
each, and delivers ONE synthetic event to each destination synchronously
(no queue, no retries beyond each destination's own max_retries) so
success/failure is reported immediately per destination.

The test event's secret_name is the literal string "test-secret" and
carries no real project data -- this command reads configuration only,
never the vault or any provider.

Exit code 0 means every configured destination accepted the test event;
non-zero means at least one did not.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNotificationsTest(cmd.Context())
		},
	}
}

func runNotificationsTest(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	cfgPath := ResolveConfig()
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load %s: %w", cfgPath, err)
	}

	if !cfg.Notifications.Enabled || len(cfg.Notifications.Webhooks) == 0 {
		fmt.Println("No notification destinations configured (notifications.enabled is false, or notifications.webhooks is empty).")
		fmt.Println("See docs/notifications.md to configure one.")
		return nil
	}

	// Delegates to notify.RunTest -- the single implementation shared with
	// the WebUI's POST /api/integrations/notifications/test
	// (internal/server/rest.go) -- so CLI and REST can never drift on what
	// "test notification delivery" actually does.
	results, err := notify.RunTest(ctx, cfg)
	if err != nil {
		return err
	}

	var failed int
	for _, res := range results {
		if res.Success {
			fmt.Printf("✓ %s: test event delivered\n", res.Destination)
			continue
		}
		failed++
		fmt.Printf("✗ %s: %s\n", res.Destination, res.Error)
	}

	if failed > 0 {
		return fmt.Errorf("%d of %d destination(s) failed", failed, len(results))
	}
	return nil
}
