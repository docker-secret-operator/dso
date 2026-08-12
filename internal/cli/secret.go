package cli

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/docker-secret-operator/dso/pkg/vault"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// parseKey helper: splits "project/path" into "project" and "path" securely.
func parseKey(input string) (string, string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", fmt.Errorf("input cannot be empty")
	}

	parts := strings.SplitN(input, "/", 2)
	var project, path string

	if len(parts) == 1 {
		project = "global"
		path = parts[0]
	} else {
		project = parts[0]
		path = parts[1]
	}

	project = strings.TrimSpace(project)
	path = strings.TrimSpace(path)

	if project == "" || path == "" {
		return "", "", fmt.Errorf("invalid format: project and path cannot be empty")
	}
	if strings.Contains(path, "..") {
		return "", "", fmt.Errorf("invalid format: path cannot contain directory traversal '..'")
	}

	return project, path, nil
}

// NewInitCmd initializes the Native Vault
func NewInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize the DSO Native Vault",
		Long:  "Creates the encrypted vault at ~/.dso/vault.enc. Must be run as a standard user (not root).",
		RunE: func(cmd *cobra.Command, args []string) error {
			// ── Privilege guard: vault must never be root-owned ─────────
			if os.Geteuid() == 0 {
				return fmt.Errorf(
					"'docker dso init' must NOT be run as root.\n" +
						"  The vault must be owned by your user account.\n" +
						"  Please re-run without sudo: docker dso init",
				)
			}
			if err := vault.InitDefault(); err != nil {
				return fmt.Errorf("failed to initialize vault: %w", err)
			}
			fmt.Println("✅ DSO Native Vault initialized successfully.")
			fmt.Println("   Next step: docker dso secret set <project>/<path>")
			return nil
		},
	}
}

// NewSecretCmd wraps all secret-related commands
func NewSecretCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Manage DSO Native Vault secrets",
	}

	cmd.AddCommand(newSecretSetCmd())
	cmd.AddCommand(newSecretGetCmd())
	cmd.AddCommand(newSecretListCmd())

	return cmd
}

func newSecretSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <project>/<path>",
		Short: "Set a secret securely in the vault",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, path, err := parseKey(args[0])
			if err != nil {
				return err
			}

			var value string
			stat, _ := os.Stdin.Stat()
			if (stat.Mode() & os.ModeCharDevice) == 0 {
				// Read from piped stdin securely, max 1MB
				lr := io.LimitReader(os.Stdin, (1<<20)+1)
				bytes, err := io.ReadAll(lr)
				if err != nil {
					return fmt.Errorf("failed to read from stdin: %w", err)
				}
				if len(bytes) > 1<<20 {
					return fmt.Errorf("secret exceeds max size of 1MB")
				}
				value = strings.TrimSpace(string(bytes))
			} else {
				// Interactive hidden prompt
				fmt.Printf("Enter secret for '%s/%s': ", project, path)
				bytes, err := term.ReadPassword(int(os.Stdin.Fd())) // #nosec G115 -- stdin file descriptors are small OS-provided values.
				fmt.Println()
				if err != nil {
					return fmt.Errorf("failed to read password: %w", err)
				}
				value = strings.TrimSpace(string(bytes))
			}

			if value == "" {
				return fmt.Errorf("secret value cannot be empty")
			}

			v, err := vault.LoadDefault()
			if err != nil {
				return fmt.Errorf("failed to load vault (run 'docker dso init' first): %w", err)
			}

			if err := v.Set(project, path, value); err != nil {
				return fmt.Errorf("failed to save secret: %w", err)
			}

			fmt.Printf("✅ Secret '%s/%s' saved successfully.\n", project, path)
			return nil
		},
	}
}

func newSecretGetCmd() *cobra.Command {
	var newline bool
	cmd := &cobra.Command{
		Use:   "get <project>/<path>",
		Short: "Retrieve a secret from the vault",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, path, err := parseKey(args[0])
			if err != nil {
				return err
			}

			v, err := vault.LoadDefault()
			if err != nil {
				return fmt.Errorf("failed to load vault: %w", err)
			}

			sec, err := v.Get(project, path)
			if err != nil {
				return fmt.Errorf("secret not found: %w", err)
			}

			if newline {
				fmt.Println(sec.Value)
			} else {
				fmt.Print(sec.Value)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&newline, "newline", "n", false, "Append a newline to the output")
	return cmd
}

func newSecretListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list [project]",
		Short: "List secret paths in the vault",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			project := "global"
			if len(args) == 1 {
				project = strings.TrimSpace(args[0])
			}

			v, err := vault.LoadDefault()
			if err != nil {
				return fmt.Errorf("failed to load vault: %w", err)
			}

			paths, err := v.List(project)
			if err != nil {
				return fmt.Errorf("failed to list secrets: %w", err)
			}

			if len(paths) == 0 {
				fmt.Printf("No secrets found for project '%s'.\n", project)
				return nil
			}

			sort.Strings(paths)

			fmt.Printf("Secrets in project '%s':\n", project)
			for _, p := range paths {
				fmt.Printf("  - %s/%s\n", project, p)
			}
			return nil
		},
	}
}

// NewEnvCmd is the parent command for .env file operations.
func NewEnvImportCmd() *cobra.Command {
	parent := &cobra.Command{
		Use:   "env",
		Short: "Manage .env file operations",
	}
	parent.AddCommand(newEnvImportSubCmd())
	return parent
}

func newEnvImportSubCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import <file> [project]",
		Short: "Import a .env file into the vault",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			filePath := args[0]
			project := "global"
			if len(args) == 2 {
				project = strings.TrimSpace(args[1])
			}

			safePath, err := config.IsSafePath("", filePath)
			if err != nil {
				return fmt.Errorf("invalid import file path: %w", err)
			}

			file, err := os.Open(safePath) // #nosec G304 -- safePath is constrained by config.IsSafePath.
			if err != nil {
				return fmt.Errorf("failed to open file: %w", err)
			}
			defer func() { _ = file.Close() }()

			v, err := vault.LoadDefault()
			if err != nil {
				return fmt.Errorf("failed to load vault: %w", err)
			}

			parsed, err := parseDotEnv(file)
			if err != nil {
				return err
			}
			for _, skip := range parsed.SkippedLines {
				fmt.Printf("⚠️  Skipping %s.\n", skip)
			}
			for _, dup := range parsed.DuplicateKeys {
				fmt.Printf("⚠️  Duplicate key detected: '%s'. Overwriting with last occurrence.\n", dup)
			}

			batch := parsed.Values
			if len(batch) == 0 {
				fmt.Println("No valid secrets found to import.")
				return nil
			}

			if err := v.SetBatch(project, batch); err != nil {
				return fmt.Errorf("failed to save imported secrets: %w", err)
			}

			fmt.Printf("✅ Successfully imported %d secrets to project '%s'.\n", len(batch), project)
			if len(parsed.DuplicateKeys) > 0 {
				fmt.Println("⚠️  Some keys were duplicated in the source file.")
			}
			fmt.Printf("⚠️  WARNING: Plaintext '%s' still exists on disk. Delete it securely when done.\n", filePath)
			return nil
		},
	}
}
