package cli

import (
	"fmt"
	"os"

	"github.com/docker-secret-operator/dso/pkg/observability"
	"github.com/spf13/cobra"
)

var CfgFile string

func ResolveConfig() string {
	// Priority 1: CLI flag (-c)
	if CfgFile != "" && CfgFile != "dso.yaml" {
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
  docker dso bootstrap local        # For development
  sudo docker dso bootstrap agent   # For production

Quick reference:
  docker dso doctor                 # Validate environment
  docker dso status                 # Check operational status
  docker dso config show            # View configuration

DSO supports multiple secret backends: local vault, HashiCorp Vault, AWS Secrets Manager, and Azure Key Vault.`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
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
