package cli

import (
	"fmt"
	"github.com/spf13/cobra"
)

func NewVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version number of DSO",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Version: %s\nCommit: %s\nBuilt: %s\n", Version, Commit, BuildDate)
		},
	}
}
