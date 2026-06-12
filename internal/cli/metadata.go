package cli

import (
	"fmt"
	"github.com/spf13/cobra"
)

func NewMetadataCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "docker-cli-plugin-metadata",
		Short:  "Return Docker CLI plugin metadata",
		Hidden: true,
		Run: func(cmd *cobra.Command, args []string) {
			metadata := fmt.Sprintf(`{
  "SchemaVersion": "0.1.0",
  "Vendor": "Umair",
  "Version": "%s",
  "ShortDescription": "Docker Secret Operator CLI"
}`, Version)
			fmt.Println(metadata)
		},
	}
}
