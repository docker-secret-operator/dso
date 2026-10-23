package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

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

// resolveKey parses "<project>/<path>" and, when the project is omitted,
// tells the operator (on stderr, so captured output is unaffected) that the
// legacy default project "global" is being used. The default itself is kept
// for compatibility: set, get, list and env import all share it.
func resolveKey(cmd *cobra.Command, arg string) (project, path string, err error) {
	project, path, err = parseKey(arg)
	if err == nil && !strings.Contains(strings.TrimSpace(arg), "/") {
		fmt.Fprintf(cmd.ErrOrStderr(), "note: no project given; using the default project %q (write %s/%s to be explicit)\n", project, project, path)
	}
	return project, path, err
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

// maxSecretSize is the largest secret value `secret set` accepts.
const maxSecretSize = 1 << 20

// readSecretValue reads a secret value from in without altering it.
//
// Input handling contract:
//   - Piped/redirected stdin (including `< file`): the exact bytes are stored.
//     No whitespace, CR or LF is stripped, so PEM blocks keep their final
//     newline. Note that `echo secret | dso secret set ...` therefore stores
//     "secret\n"; use `printf %s` / `echo -n`, or pass trimNewline.
//   - Interactive terminal: the hidden prompt returns the typed line without
//     its terminating newline; nothing else is altered.
//   - trimNewline (--trim-newline) is the only transformation: it removes
//     exactly one trailing "\n" or "\r\n" and never other whitespace.
//
// Storage is JSON text, so values must be valid UTF-8; anything else is
// rejected rather than silently rewritten.
func readSecretValue(cmd *cobra.Command, label string, trimNewline bool) (string, error) {
	in := cmd.InOrStdin()

	var raw []byte
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) { // #nosec G115 -- file descriptors are small OS-provided values.
		fmt.Fprintf(cmd.ErrOrStderr(), "Enter secret for '%s': ", label)
		b, err := term.ReadPassword(int(f.Fd())) // #nosec G115 -- see above.
		fmt.Fprintln(cmd.ErrOrStderr())
		if err != nil {
			return "", fmt.Errorf("failed to read password: %w", err)
		}
		raw = b
	} else {
		b, err := io.ReadAll(io.LimitReader(in, maxSecretSize+1))
		if err != nil {
			return "", fmt.Errorf("failed to read from stdin: %w", err)
		}
		raw = b
		if trimNewline {
			if bytes.HasSuffix(raw, []byte("\r\n")) {
				raw = raw[:len(raw)-2]
			} else if bytes.HasSuffix(raw, []byte("\n")) {
				raw = raw[:len(raw)-1]
			}
		}
	}

	if len(raw) > maxSecretSize {
		return "", fmt.Errorf("secret exceeds max size of 1MB")
	}
	if !utf8.Valid(raw) {
		return "", fmt.Errorf("secret value is not valid UTF-8; the vault stores text only")
	}
	if len(raw) == 0 {
		return "", fmt.Errorf("secret value cannot be empty")
	}
	return string(raw), nil
}

func newSecretSetCmd() *cobra.Command {
	var trimNewline bool
	cmd := &cobra.Command{
		Use:   "set <project>/<path>",
		Short: "Set a secret securely in the vault",
		Long: `Store a secret read from a hidden prompt or from stdin.

The value is stored byte-for-byte: leading/trailing whitespace, tabs, carriage
returns and trailing newlines are preserved. Piped input is NOT trimmed, so
'echo secret | dso secret set p/k' stores "secret\n". Use 'printf %s secret |',
'dso secret set p/k < file', or --trim-newline (removes one trailing newline).
Values must be valid UTF-8 and at most 1MB.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, path, err := resolveKey(cmd, args[0])
			if err != nil {
				return err
			}

			value, err := readSecretValue(cmd, project+"/"+path, trimNewline)
			if err != nil {
				return err
			}

			v, err := vault.LoadDefault()
			if err != nil {
				return fmt.Errorf("failed to load vault (run 'docker dso init' first): %w", err)
			}

			if err := v.Set(project, path, value); err != nil {
				return fmt.Errorf("failed to save secret: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "✅ Secret '%s/%s' saved successfully.\n", project, path)
			return nil
		},
	}
	cmd.Flags().BoolVar(&trimNewline, "trim-newline", false, "Remove one trailing newline from piped input (nothing else is trimmed)")
	return cmd
}

func newSecretGetCmd() *cobra.Command {
	var newline bool
	cmd := &cobra.Command{
		Use:   "get <project>/<path>",
		Short: "Retrieve a secret from the vault",
		Long: `Print a secret's value to stdout, without a trailing newline unless -n is given.

<project>/<path> selects the secret. If no project is given the default
project "global" is used (the same default as 'secret set', 'secret list' and
'env import'); a note is written to stderr when this happens.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, path, err := resolveKey(cmd, args[0])
			if err != nil {
				return err
			}

			v, err := vault.LoadDefault()
			if err != nil {
				return fmt.Errorf("failed to load vault: %w", err)
			}

			sec, err := v.Get(project, path)
			if err != nil {
				if errors.Is(err, vault.ErrSecretNotFound) {
					return err
				}
				return fmt.Errorf("failed to read secret: %w", err)
			}

			if newline {
				fmt.Fprintln(cmd.OutOrStdout(), sec.Value)
			} else {
				fmt.Fprint(cmd.OutOrStdout(), sec.Value)
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
