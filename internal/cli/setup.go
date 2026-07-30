package cli

import (
	"fmt"

	"github.com/docker-secret-operator/dso/internal/setup"
	"github.com/spf13/cobra"
)

// NewSetupCmd creates the simplified setup wizard command
func NewSetupCmd() *cobra.Command {
	var (
		mode       string
		provider   string
		autoDetect bool
		nonRoot    bool
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Simple setup wizard for DSO",
		Long: `Setup wizard that configures DSO for your environment.

This command provides an interactive experience to:
  - Detect cloud provider (AWS, Azure, Huawei, or Local)
  - Select deployment mode (Local or Cloud)
  - Automatically install required provider plugins
  - Generate configuration file
  - Verify your setup

Examples:
  docker dso setup              # Interactive setup wizard
  docker dso setup --auto-detect # Auto-detect cloud provider
  docker dso setup --mode local # Setup for local vault mode`,
		RunE: func(cmd *cobra.Command, args []string) error {
			eng := setup.NewEngine()

			// Subscribe to events for rendering (Phase 1: minimal).
			eng.Events.Subscribe(func(evt setup.Event) {
				switch evt.Type {
				case setup.EventSetupFailed:
					if evt.Error != nil {
						_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "setup error: %v\n", evt.Error)
					}
				}
			})

			opts := setup.SetupOptions{
				Mode:        setup.SetupMode(mode),
				Provider:    provider,
				AutoDetect:  autoDetect,
				NonRoot:     nonRoot,
				Interactive: true,
			}

			_, err := eng.Setup(cmd.Context(), opts)
			return err
		},
	}

	cmd.Flags().StringVar(&mode, "mode", "", "Deployment mode: local or agent (cloud)")
	cmd.Flags().StringVar(&provider, "provider", "", "Cloud provider: aws, azure, vault, huawei")
	cmd.Flags().BoolVar(&autoDetect, "auto-detect", false, "Auto-detect cloud provider from instance metadata")
	cmd.Flags().BoolVar(&nonRoot, "enable-nonroot", false, "Enable non-root user access to DSO")

	return cmd
}
