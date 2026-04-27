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
			fmt.Println("Docker Secret Operator (DSO) v3.2.0")
		},
	}
}

func NewApplyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "apply",
		Short: "Apply a DSO configuration file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not yet implemented")
		},
	}
}

func NewInjectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inject",
		Short: "Inject secrets directly into a running specific container",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not yet implemented")
		},
	}
}

func NewSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Synchronize secrets manually against cloud providers",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not yet implemented")
		},
	}
}
