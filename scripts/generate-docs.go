package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/docker-secret-operator/dso/internal/cli"
	"github.com/spf13/cobra/doc"
)

func main() {
	// Target directory for generated CLI docs
	docsDir := "./docs/cli-reference"

	// Ensure the target directory exists and is clean
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		log.Fatalf("Failed to create docs directory: %v", err)
	}

	// Clean out any existing markdown files in that directory to prevent stale pages
	files, err := filepath.Glob(filepath.Join(docsDir, "*.md"))
	if err == nil {
		for _, f := range files {
			_ = os.Remove(f)
		}
	}

	fmt.Printf("Generating CLI reference markdown files in %s...\n", docsDir)

	// Obtain the actual Cobra root command
	rootCmd := cli.NewRootCmd()

	// Generate markdown tree
	err = doc.GenMarkdownTree(rootCmd, docsDir)
	if err != nil {
		log.Fatalf("Error generating markdown tree: %v", err)
	}

	fmt.Println("✅ CLI Reference documentation successfully generated!")
}
