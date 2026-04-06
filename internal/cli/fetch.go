package cli

import (
	"fmt"
	"os"

	"github.com/docker-secret-operator/dso/internal/injector"
	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/spf13/cobra"
)

func NewFetchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "fetch [secret-name]",
		Short: "Manually fetch a secret and display it",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			cfg, err := config.LoadConfig(ResolveConfig())
			if err != nil {
				fmt.Printf("Error loading config: %v\nTip: Create /etc/dso/dso.yaml or pass --config /path/to/dso.yaml\n", err)
				os.Exit(1)
			}

			socketPath := "/var/run/dso.sock"
			if custom := os.Getenv("DSO_SOCKET_PATH"); custom != "" {
				socketPath = custom
			}

			client, err := injector.NewAgentClient(socketPath)
			if err != nil {
				fmt.Printf("Error connecting to agent: %v\n", err)
				os.Exit(1)
			}

			secretName := args[0]
			var secMapping *config.SecretMapping
			for _, s := range cfg.Secrets {
				if s.Name == secretName {
					secMapping = &s
					break
				}
			}

			pName := ""
			var pCfg config.ProviderConfig
			if secMapping != nil {
				pName = secMapping.Provider
			}

			if pName == "" {
				// Default to first provider if none specified
				for k, v := range cfg.Providers {
					pName = k
					pCfg = v
					break
				}
			} else {
				pCfg = cfg.Providers[pName]
			}

			data, err := client.FetchSecret(pName, pCfg.Config, secretName)
			if err != nil {
				fmt.Printf("Error fetching secret: %v\n", err)
				os.Exit(1)
			}

			fmt.Printf("Secret: %s\n", secretName)
			for k, v := range data {
				fmt.Printf("  %s: %s\n", k, v)
			}
		},
	}
}
