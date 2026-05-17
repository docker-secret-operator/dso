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
			fmt.Println("Docker Secret Operator (DSO) v3.5.6")
		},
	}
}
