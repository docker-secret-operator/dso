// cmd/dso is the entrypoint for the DSO proxy architecture tool.
//
// It reads a standard docker-compose.yml, applies the DSO transformation
// (proxy injection, port migration, network setup), and writes a
// docker-compose.generated.yml ready for `docker compose up`.
//
// Usage:
//
//	dso generate [--input docker-compose.yml] [--output docker-compose.generated.yml]
package main

import (
	"fmt"
	"os"

	"github.com/docker-secret-operator/dso/pkg/parser"
	"github.com/docker-secret-operator/dso/pkg/transformer"
	"github.com/spf13/cobra"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "dso",
		Short: "Docker Secret Operator — zero-downtime deployments for plain Docker",
	}
	root.AddCommand(generateCmd())
	return root
}

func generateCmd() *cobra.Command {
	var inputPath string
	var outputPath string

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Transform docker-compose.yml into a DSO-enhanced docker-compose.generated.yml",
		Long: `Reads a standard docker-compose.yml, auto-detects services eligible for
zero-downtime proxy injection, and writes docker-compose.generated.yml.

No changes to your existing compose file are required. Add an x-dso block
to any service for fine-grained control:

  services:
    app:
      image: myapp
      ports:
        - "3000:3000"
      x-dso:
        enabled: true
        strategy: rolling`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// ── Parse ──────────────────────────────────────────────────────────
			cfg, warnings, err := parser.ParseFile(inputPath)
			if err != nil {
				return fmt.Errorf("parse error: %w", err)
			}

			// Print any parser warnings (e.g. deprecated dso-proxy block).
			for _, w := range warnings {
				fmt.Fprintf(os.Stderr, "%s\n", w)
			}

			eligible := 0
			for _, svc := range cfg.Services {
				if svc.IsEligible {
					eligible++
				}
			}
			fmt.Printf("Parsed %d service(s) — %d eligible for proxy injection\n",
				len(cfg.Services), eligible)

			// ── Transform ──────────────────────────────────────────────────────
			out, summary, err := transformer.Transform(cfg)
			if err != nil {
				return fmt.Errorf("transform error: %w", err)
			}

			// Print the diff summary so the user can see exactly what changed.
			if len(summary) > 0 {
				fmt.Println("\nDSO Transform Summary:")
				for _, line := range summary {
					fmt.Printf("  %s\n", line)
				}
				fmt.Println()
			}

			// ── Write ──────────────────────────────────────────────────────────
			if err := os.WriteFile(outputPath, out, 0o644); err != nil {
				return fmt.Errorf("cannot write %q: %w", outputPath, err)
			}

			fmt.Printf("Generated: %s\n", outputPath)
			return nil
		},
	}

	cmd.Flags().StringVarP(&inputPath, "input", "i", "docker-compose.yml",
		"Path to the input docker-compose.yml")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "docker-compose.generated.yml",
		"Path to write the generated compose file")

	return cmd
}
