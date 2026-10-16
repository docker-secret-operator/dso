package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/docker-secret-operator/dso/internal/injector"
	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/spf13/cobra"
)

func NewExportCmd() *cobra.Command {
	var format, output string
	var overwrite bool
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export injected secrets for local CI or testing",
		Run: func(cmd *cobra.Command, args []string) {
			cfgPath := ResolveConfig()
			cfg, err := config.LoadConfig(cfgPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Config load failed: %v\n", err)
				os.Exit(1)
			}

			socketPath := "/run/dso/dso.sock"
			if custom := os.Getenv("DSO_SOCKET_PATH"); custom != "" {
				socketPath = custom
			}

			client, err := injector.NewAgentClient(socketPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Agent connection failed. Is the DSO agent running? error: %v\n", err)
				os.Exit(1)
			}
			defer func() { _ = client.Close() }()

			if format != "env" {
				fmt.Fprintf(os.Stderr, "Error: unsupported format %q (only \"env\" is supported)\n", format)
				os.Exit(1)
			}

			injectedEnvs, err := client.FetchAllEnvs(cfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Injection failed: %v\n", err)
				os.Exit(1)
			}

			// G304: Ensure the output path is safe and does not escape the current directory
			safePath, err := config.IsSafePath("", output)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			fmt.Fprintf(os.Stderr, "⚠️  WARNING: writing PLAINTEXT secrets to %s (mode 0600). Never commit this file: add it to .gitignore, keep it out of images and backups, and delete it when no longer needed.\n", output)

			err = writeExportFile(safePath, overwrite, func(w io.Writer) error {
				keys := make([]string, 0, len(injectedEnvs))
				for k := range injectedEnvs {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					if _, err := fmt.Fprintf(w, "%s=%s\n", k, injectedEnvs[k]); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Export failed: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Secrets successfully exported to %s format at: %s\n", format, output)
		},
	}
	cmd.Flags().StringVarP(&format, "format", "f", "env", "Output format (env)")
	cmd.Flags().StringVarP(&output, "output", "o", ".env.local", "Output file destination")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "Replace the output file if it already exists (default: refuse). The replacement is atomic and always mode 0600")
	return cmd
}

// writeExportFile writes a plaintext secrets file with owner-only permissions
// and without ever clobbering data by accident.
//
//   - Default (overwrite=false): the file is created with O_EXCL, which is
//     atomic in the kernel, so there is no check-then-create race and a
//     pre-existing file (or symlink, even a dangling one) is never opened. If
//     writing fails, the file this call created is removed.
//   - overwrite=true: content goes to a fresh 0600 temp file in the same
//     directory and is renamed over the target, so a failed export leaves the
//     previous file untouched and the result is always 0600 regardless of the
//     old file's mode. A symlink at the target is replaced, not followed.
//
// A crash between creating the file and finishing the write can leave a
// partial file in the default mode; it contains only a prefix of the secrets
// and is owner-readable only.
func writeExportFile(path string, overwrite bool, write func(io.Writer) error) (err error) {
	if !overwrite {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- path is constrained by config.IsSafePath.
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				return fmt.Errorf("refusing to overwrite existing file %s (re-run with --overwrite to replace it)", path)
			}
			return fmt.Errorf("failed to create output file %s: %w", path, err)
		}
		if err := finishExport(f, write); err != nil {
			_ = os.Remove(path) // we created it above via O_EXCL
			return err
		}
		return nil
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary output file: %w", err)
	}
	tmpName := tmp.Name()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to restrict output file permissions: %w", err)
	}
	if err := finishExport(tmp, write); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to replace output file %s: %w", path, err)
	}
	return nil
}

// finishExport writes, syncs and closes f, closing it on every path.
func finishExport(f *os.File, write func(io.Writer) error) error {
	if err := write(f); err != nil {
		_ = f.Close()
		return fmt.Errorf("failed to write output file: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("failed to sync output file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close output file: %w", err)
	}
	return nil
}
