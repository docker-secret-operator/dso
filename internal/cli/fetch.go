package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/docker-secret-operator/dso/internal/injector"
	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/spf13/cobra"
)

func NewFetchCmd() *cobra.Command {
	var reveal bool

	c := &cobra.Command{
		Use:   "fetch [secret-name]",
		Short: "Manually fetch a secret and display it",
		Long: `Fetch a secret from the configured provider and display its keys.

By default secret values are masked (shown as ***) to prevent accidental
exposure in terminal recordings and shared screens. Use --reveal to print
the actual values — only do this in a private terminal session.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig(ResolveConfig())
			if err != nil {
				return fmt.Errorf("loading config: %v\nTip: Create /etc/dso/dso.yaml or pass --config /path/to/dso.yaml", err)
			}

			if len(args) == 0 {
				fmt.Println("Available secrets in configuration:")
				if len(cfg.Secrets) == 0 {
					fmt.Println("  (No secrets defined in dso.yaml)")
				}
				for _, s := range cfg.Secrets {
					fmt.Printf("  - %s\n", s.Name)
				}
				fmt.Println("\nUsage: docker dso fetch [secret-name]")
				return nil
			}

			socketPath := "/run/dso/dso.sock"
			if custom := os.Getenv("DSO_SOCKET_PATH"); custom != "" {
				socketPath = custom
			}

			client, err := injector.NewAgentClient(socketPath)
			if err != nil {
				return fmt.Errorf("connecting to agent: %v (Ensure 'docker dso up' or 'dso-agent' is running)", err)
			}
			defer func() { _ = client.Close() }()

			secretName := args[0]
			var secMapping *config.SecretMapping
			for _, s := range cfg.Secrets {
				if s.Name == secretName {
					secMapping = &s
					break
				}
			}

			if secMapping == nil {
				var names []string
				for _, s := range cfg.Secrets {
					names = append(names, "  - "+s.Name)
				}
				return fmt.Errorf("secret '%s' not found in config.\n\nAvailable secrets:\n%s", secretName, strings.Join(names, "\n"))
			}

			pName := secMapping.Provider
			pCfg := cfg.Providers[pName]

			data, err := client.FetchSecret(pName, pCfg.Config, secretName)
			if err != nil {
				return fmt.Errorf("fetching secret: %w", err)
			}

			fmt.Printf("Secret: %s\n", secretName)
			for k, v := range data {
				display := "***"
				if reveal {
					display = v
				}
				fmt.Printf("  %s: %s\n", k, display)
			}
			if !reveal {
				fmt.Println("\n  (values masked — use --reveal to display plaintext)")
			}
			return nil
		},
	}

	c.Flags().BoolVar(&reveal, "reveal", false, "Print secret values in plaintext (use only in a private terminal)")
	return c
}
