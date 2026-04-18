// cmd/dso is the entrypoint for the DSO proxy architecture tool.
// It accepts a dso-compose.yml file, validates it using the parser, transforms
// it via the transformer, and writes docker-compose.generated.yml to disk.
//
// Usage:
//
//	dso generate [--input dso-compose.yml] [--output docker-compose.generated.yml]
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
		Short: "Docker Secret Operator — proxy-based zero-downtime deployment tool",
	}
	root.AddCommand(generateCmd())
	return root
}

func generateCmd() *cobra.Command {
	var inputPath string
	var outputPath string

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Parse dso-compose.yml and generate docker-compose.generated.yml",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// --- Parse ---
			cfg, err := parser.ParseFile(inputPath)
			if err != nil {
				return fmt.Errorf("parse error: %w", err)
			}

			fmt.Printf("Parsed %d backing service(s) with %d proxy target(s)\n",
				len(cfg.Services), len(cfg.DSO.Containers))

			// --- Transform ---
			out, err := transformer.Transform(cfg)
			if err != nil {
				return fmt.Errorf("transform error: %w", err)
			}

			// --- Write ---
			if err := os.WriteFile(outputPath, out, 0o644); err != nil {
				return fmt.Errorf("cannot write output: %w", err)
			}

			fmt.Printf("Generated: %s\n", outputPath)
			return nil
		},
	}

	cmd.Flags().StringVarP(&inputPath, "input", "i", "dso-compose.yml",
		"Path to the dso-compose.yml input file")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "docker-compose.generated.yml",
		"Path to write the generated docker-compose file")

	return cmd
}
