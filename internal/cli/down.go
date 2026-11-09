package cli

import (
	"fmt"
	"github.com/spf13/cobra"
	"os"
	"os/exec"
	"strings"
)

func NewDownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down [args...]",
		Short: "Stop and remove the stack's containers and networks (volumes and images are kept by default)",
		Long: `Runs 'docker compose down' with the given arguments.

By default this removes the stack's containers and networks ONLY. Named volumes
and images are kept. Destructive options are passed through to docker compose:
  -v, --volumes   also remove named volumes declared in the compose file (DATA LOSS)
  --rmi all|local also remove images
  --remove-orphans remove containers for services not in the compose file

Arguments are forwarded unchanged; DSO does not add or imply any of them.`,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			dockerPath, err := exec.LookPath("docker")
			if err != nil {
				return fmt.Errorf("%s", "docker executable not found in PATH")
			}

			// Validate arguments to prevent shell injection (G204)
			// Reject characters that could lead to command substitution or piping
			for _, arg := range args {
				if strings.ContainsAny(arg, ";&|$`\"") {
					return fmt.Errorf("Invalid character in arguments: %s", arg)
				}
			}

			fullArgs := append([]string{"compose", "down"}, args...)

			// #nosec G204 -- docker execution uses strictly validated arguments
			child := exec.Command(dockerPath, fullArgs...)
			child.Stdout = os.Stdout
			child.Stderr = os.Stderr
			child.Stdin = os.Stdin

			if err := child.Run(); err != nil {
				return fmt.Errorf("running down: %w", err)
			}
			return nil
		},
	}
}
