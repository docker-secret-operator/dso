package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func NewDiffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff [stack-name]",
		Short: "Not implemented: compare local configuration with a deployed stack",
		Long: `Not implemented. This command always exits 1.

It is reserved for comparing the provider mappings in dso.yaml with a deployed
stack. A real comparison needs a defined source of truth for the deployed side
(container labels written by 'dso up', or the agent's tracked targets) and a
stable identity for "the stack" (project/environment selection), neither of
which the CLI exposes yet. Until then it reports that no comparison was made
rather than claiming "no differences".

Alternatives today:
  docker dso validate       # check the project and configuration
  docker dso inspect <id>   # show injected environment/mounts of one container
  docker dso config show    # view the (redacted) configuration`,
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
