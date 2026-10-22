package cli

import (
	"fmt"
	"os"

	"github.com/docker-secret-operator/dso/pkg/observability"
	"github.com/spf13/cobra"
)

var CfgFile string

// cfgFileExplicit records whether -c/--config was actually passed on the
// command line, set from cmd.Flags().Changed("config") in NewRootCmd's
// PersistentPreRun. CfgFile alone can't tell us this: its flag default is
// literally "dso.yaml" (see NewRootCmd), so an operator who explicitly runs
// `-c dso.yaml` -- the single most obvious filename, used in every doc
// example -- ends up with the exact same CfgFile value as someone who never
// passed -c at all.
var cfgFileExplicit bool

var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

func ResolveConfig() string {
	// Priority 1: CLI flag (-c). CfgFile != "dso.yaml" already proves intent
	// for any other filename; cfgFileExplicit additionally covers the one
	// ambiguous case where the operator explicitly passed "dso.yaml" itself
	// -- without it, that case was silently indistinguishable from -c never
	// being passed, and fell through to /etc/dso/dso.yaml instead.
	if CfgFile != "" && (CfgFile != "dso.yaml" || cfgFileExplicit) {
		return CfgFile
	}

	// Priority 2: /etc/dso/dso.yaml
	if _, err := os.Stat("/etc/dso/dso.yaml"); err == nil {
		return "/etc/dso/dso.yaml"
	}

	// Priority 3: ./dso.yaml
	if _, err := os.Stat("dso.yaml"); err == nil {
		return "dso.yaml"
	}

	return "dso.yaml"
}

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dso",
		Short: "Docker Secret Operator (DSO) — Secret lifecycle runtime for Docker Compose",
		Long: `Docker Secret Operator (DSO) is a cloud-native secret injection runtime for Docker Compose.
It fetches and injects secrets into containers at runtime without exposing them to the host filesystem.

Quick start:
  docker dso setup                  # Interactive setup wizard (recommended)

Usage:
  docker dso setup                  # Interactive setup wizard (handles everything)

Advanced Usage (Manual Bootstrapping):
  docker dso bootstrap local        # Lower-level manual local mode init
  sudo docker dso bootstrap agent   # Lower-level manual production init

Quick reference:
  docker dso doctor                 # Validate environment
  docker dso status                 # Check operational status
  docker dso config show            # View configuration

DSO supports multiple secret backends: local vault, HashiCorp Vault, AWS Secrets Manager, and Azure Key Vault.`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			cfgFileExplicit = cmd.Flags().Changed("config")
			_, _ = observability.NewLogger("info", "console", false)
		},
	}

	cmd.PersistentFlags().StringVarP(&CfgFile, "config", "c", "dso.yaml", "config file (searches: /etc/dso/dso.yaml, ./dso.yaml, dso.yaml)")

	cmd.AddCommand(NewSetupCmd())
	cmd.AddCommand(NewBootstrapCmd())
	cmd.AddCommand(NewDoctorCmd())
	cmd.AddCommand(NewStatusCmd())
	cmd.AddCommand(NewConfigCmd())
	cmd.AddCommand(NewSystemCmd())
	cmd.AddCommand(NewAgentCmd())
	cmd.AddCommand(NewMetadataCmd())
	cmd.AddCommand(NewComposeCmd())
	cmd.AddCommand(NewFetchCmd())
	cmd.AddCommand(NewInitCmd())
	cmd.AddCommand(NewApplyCmd())
	cmd.AddCommand(NewInjectCmd())
	cmd.AddCommand(NewSyncCmd())
	cmd.AddCommand(NewMigrateCmd())
	cmd.AddCommand(NewNotificationsCmd())
	cmd.AddCommand(NewUpCmd())
	cmd.AddCommand(NewDownCmd())
	cmd.AddCommand(NewWatchCmd())
	cmd.AddCommand(NewVersionCmd())
	cmd.AddCommand(NewValidateCmd())
	cmd.AddCommand(NewExportCmd())
	cmd.AddCommand(NewInspectCmd())
	cmd.AddCommand(NewDiffCmd())
	cmd.AddCommand(NewLogsCmd())
	cmd.AddCommand(NewSecretCmd())
	cmd.AddCommand(NewEnvImportCmd())
	cmd.AddCommand(NewCentralCmd())

	return cmd
}

func Execute() {
	rootCmd := NewRootCmd()

	// Docker CLI plugin fix: strip the plugin name if it's passed as the first argument
	// (Required when called via 'docker dso ...')
	if len(os.Args) > 1 && os.Args[1] == "dso" {
		os.Args = append(os.Args[:1], os.Args[2:]...)
	}

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
