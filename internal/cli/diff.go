package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func NewDiffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff [stack-name]",
		Short: "Show what changed between local configuration and deployed stack",
		RunE: func(cmd *cobra.Command, args []string) error {
			// This command serves as a lightweight diff checking local config (dso.yaml vs running container)
			stackName := "default"
			if len(args) > 0 {
				stackName = args[0]
			}

			// In v1.1, diff shows the structural differences of mapping keys before deployment.
			fmt.Fprintf(cmd.ErrOrStderr(), "Analyzing stack '%s'...\n", stackName)
			cfgPath := ResolveConfig()
			if _, err := os.Stat(cfgPath); err != nil {
				return fmt.Errorf("config %s not found", cfgPath)
			}

			// No comparison is implemented yet. Say so instead of reporting a
			// fabricated "no differences" result that scripts could trust.
			fmt.Fprintln(cmd.ErrOrStderr(), "diff: comparison against a deployed stack is not implemented; no differences were checked.")
			return fmt.Errorf("diff is not implemented")
		},
	}
}
