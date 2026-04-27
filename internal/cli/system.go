package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

// version is injected at build time via -ldflags "-X main.version=..."
// Fallback to "dev" for local builds.
var version = "dev"

const (
	pluginInstallDir  = "/usr/local/lib/dso/plugins"
	systemdServiceDir = "/etc/systemd/system"
	dsoConfigDir      = "/etc/dso"
	serviceFile       = "dso-agent.service"
	releaseBaseURL    = "https://github.com/docker-secret-operator/dso/releases/download"

	// downloadTimeout caps total time for any single file download.
	downloadTimeout = 90 * time.Second
	// downloadRetries is the number of additional attempts after the first.
	downloadRetries = 2
)

// systemdTemplate is the canonical service definition for the legacy cloud agent.
// ExecStart intentionally calls `dso agent` (unchanged) for zero-touch V2 upgrades.
const systemdTemplate = `[Unit]
Description=DSO Agent (Legacy Cloud Mode)
After=network.target docker.service
Requires=docker.service

[Service]
Type=simple
ExecStart=/usr/local/bin/dso agent --api-addr :8080
Restart=on-failure
RestartSec=5
EnvironmentFile=-/etc/dso/agent.env
RuntimeDirectory=dso

[Install]
WantedBy=multi-user.target
`

// NewSystemCmd is the parent for all system-level operations.
func NewSystemCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "system",
		Short: "System-level DSO management commands",
	}
	cmd.AddCommand(newSystemSetupCmd())
	cmd.AddCommand(newSystemDoctorCmd())
	return cmd
}

// ─────────────────────────────────────────────────────────────
// dso system setup
// ─────────────────────────────────────────────────────────────

func newSystemSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Configure DSO for Cloud Mode (requires root)",
		Long: `Installs the systemd service and provider plugins required for Cloud Mode.
Must be run as root (sudo).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// ── Privilege guard: MUST be first, before any disk operation ──
			if os.Geteuid() != 0 {
				return fmt.Errorf(
					"'dso system setup' must be run as root.\n" +
						"  Please re-run with: sudo dso system setup",
				)
			}

			// Non-Linux guard: systemd operations are Linux-only.
			if runtime.GOOS != "linux" {
				return fmt.Errorf(
					"'dso system setup' is only supported on Linux (systemd required).\n" +
						"  Detected OS: %s", runtime.GOOS,
				)
			}

			fmt.Println("[DSO] Starting Cloud Mode setup...")

			// Track rollback state with explicit flags so defer is atomic.
			svcPath := filepath.Join(systemdServiceDir, serviceFile)
			svcWritten := false
			pluginsWritten := false

			// ── Deferred atomic rollback ───────────────────────────────────
			var setupErr error
			defer func() {
				if setupErr == nil {
					return // success — nothing to rollback
				}
				fmt.Fprintln(os.Stderr, "[DSO] ⚠️  Setup failed — rolling back partial state...")
				if pluginsWritten {
					if err := os.RemoveAll(pluginInstallDir); err != nil {
						fmt.Fprintf(os.Stderr, "[DSO]   warn: failed to remove plugins: %v\n", err)
					} else {
						fmt.Fprintln(os.Stderr, "[DSO]   rolled back: plugins removed.")
					}
				}
				if svcWritten {
					if err := os.Remove(svcPath); err != nil {
						fmt.Fprintf(os.Stderr, "[DSO]   warn: failed to remove systemd service: %v\n", err)
					} else {
						fmt.Fprintln(os.Stderr, "[DSO]   rolled back: systemd service removed.")
					}
				}
			}()

			// ── Step 1: Create /etc/dso config directory ───────────────────
			fmt.Println("[DSO] Creating /etc/dso...")
			if setupErr = os.MkdirAll(dsoConfigDir, 0o755); setupErr != nil {
				return fmt.Errorf("failed to create %s: %w", dsoConfigDir, setupErr)
			}

			// ── Step 2: Write systemd service file ─────────────────────────
			fmt.Printf("[DSO] Writing systemd service to %s...\n", svcPath)
			if setupErr = os.WriteFile(svcPath, []byte(systemdTemplate), 0o644); setupErr != nil {
				setupErr = fmt.Errorf("failed to write systemd service: %w", setupErr)
				return setupErr
			}
			svcWritten = true

			// ── Step 3: Download + validate + extract plugins ──────────────
			if setupErr = installPlugins(); setupErr != nil {
				setupErr = fmt.Errorf("plugin installation failed: %w", setupErr)
				return setupErr
			}
			pluginsWritten = true

			// ── Step 4: Activate systemd service ───────────────────────────
			if setupErr = activateSystemd(); setupErr != nil {
				return setupErr
			}

			// ── Final success output ────────────────────────────────────────
			fmt.Println("")
			fmt.Println("[DSO] ✅ Cloud mode configured successfully.")
			fmt.Println("       Agent:   running (dso-agent.service)")
			fmt.Printf("       Plugins: installed to %s\n", pluginInstallDir)
			fmt.Println("       Monitor: journalctl -u dso-agent -f")
			return nil
		},
	}
}

// installPlugins downloads, validates, and extracts prebuilt provider plugins.
// It enforces that the plugin tarball version matches the current binary version.
func installPlugins() error {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	ver := version

	// Reject "dev" builds from trying to download non-existent release artifacts.
	if ver == "dev" {
		return fmt.Errorf(
			"cannot install plugins for a development build (version=dev).\n" +
				"  Use a release binary with a proper version tag.",
		)
	}

	tarballName := fmt.Sprintf("dso-plugins-%s-%s-%s.tar.gz", goos, goarch, ver)
	checksumName := tarballName + ".sha256"
	tarURL := fmt.Sprintf("%s/%s/%s", releaseBaseURL, ver, tarballName)
	csURL := fmt.Sprintf("%s/%s/%s", releaseBaseURL, ver, checksumName)

	// ── Download to temp directory ─────────────────────────────────────────
	tmpDir, err := os.MkdirTemp("", "dso-plugins-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir) // Always clean up temp files regardless of outcome

	fmt.Printf("[DSO] Downloading plugin tarball from %s...\n", tarURL)
	tarPath := filepath.Join(tmpDir, tarballName)
	if err := downloadFileWithRetry(tarURL, tarPath); err != nil {
		return fmt.Errorf("failed to download plugins: %w", err)
	}

	// ── Download checksum ──────────────────────────────────────────────────
	fmt.Printf("[DSO] Downloading checksum from %s...\n", csURL)
	csPath := filepath.Join(tmpDir, checksumName)
	if err := downloadFileWithRetry(csURL, csPath); err != nil {
		return fmt.Errorf("failed to download checksum: %w", err)
	}

	// ── Validate SHA256 before touching the install target ────────────────
	fmt.Println("[DSO] Validating plugin integrity (SHA256)...")
	if err := validateChecksum(tarPath, csPath); err != nil {
		return fmt.Errorf("integrity check failed: %w", err)
	}

	// ── Clean + recreate install directory before extraction ───────────────
	// RemoveAll is safe when the directory does not yet exist (returns nil).
	// This guarantees no stale binaries from a previous version linger.
	fmt.Printf("[DSO] Cleaning plugin directory %s...\n", pluginInstallDir)
	if err := os.RemoveAll(pluginInstallDir); err != nil {
		return fmt.Errorf("failed to clean plugin dir %s: %w", pluginInstallDir, err)
	}
	if err := os.MkdirAll(pluginInstallDir, 0o755); err != nil {
		return fmt.Errorf("failed to create plugin dir %s: %w", pluginInstallDir, err)
	}

	// ── Extract tarball ────────────────────────────────────────────────────
	fmt.Printf("[DSO] Extracting plugins to %s...\n", pluginInstallDir)
	tarCmd := exec.Command("tar", "-xzf", tarPath, "-C", pluginInstallDir, "--strip-components=1")
	tarCmd.Stdout = os.Stdout
	tarCmd.Stderr = os.Stderr
	if err := tarCmd.Run(); err != nil {
		// Rollback partial extraction immediately
		_ = os.RemoveAll(pluginInstallDir)
		return fmt.Errorf("tarball extraction failed: %w", err)
	}

	// ── Verify extracted binaries exist and are executable ─────────────────
	knownPlugins := []string{"aws", "azure", "vault", "huawei"}
	missing := []string{}
	for _, p := range knownPlugins {
		pluginPath := filepath.Join(pluginInstallDir, "dso-provider-"+p)
		info, err := os.Stat(pluginPath)
		if err != nil {
			missing = append(missing, p)
			continue
		}
		// Ensure executable bit is set
		if info.Mode()&0o111 == 0 {
			if err := os.Chmod(pluginPath, 0o755); err != nil {
				return fmt.Errorf("failed to set executable on %s: %w", pluginPath, err)
			}
		}
	}
	if len(missing) > 0 {
		_ = os.RemoveAll(pluginInstallDir) // rollback
		return fmt.Errorf(
			"tarball is missing expected plugins: %s\n"+
				"  Expected path: %s/dso-provider-{name}",
			strings.Join(missing, ", "), pluginInstallDir,
		)
	}

	fmt.Printf("[DSO] Plugins verified: %s\n", strings.Join(knownPlugins, ", "))
	return nil
}

// activateSystemd runs daemon-reload, enable, and restart atomically.
// Only called on Linux — guarded at the call site.
func activateSystemd() error {
	cmds := [][]string{
		{"systemctl", "daemon-reload"},
		{"systemctl", "enable", "dso-agent"},
		{"systemctl", "restart", "dso-agent"},
	}
	for _, args := range cmds {
		fmt.Printf("[DSO] Running: %s\n", strings.Join(args, " "))
		c := exec.Command(args[0], args[1:]...)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			return fmt.Errorf(
				"'%s' failed: %w\n  Diagnose with: journalctl -xe -u dso-agent",
				strings.Join(args, " "), err,
			)
		}
	}
	return nil
}

// downloadFileWithRetry performs an HTTP GET with a timeout and retries.
// It retries up to downloadRetries additional times on failure.
func downloadFileWithRetry(url, destPath string) error {
	var lastErr error
	for attempt := 0; attempt <= downloadRetries; attempt++ {
		if attempt > 0 {
			fmt.Printf("[DSO]   Retry %d/%d for %s...\n", attempt, downloadRetries, url)
		}
		if err := downloadFile(url, destPath); err != nil {
			lastErr = err
			continue
		}
		return nil // success
	}
	return fmt.Errorf("download failed after %d attempts: %w", downloadRetries+1, lastErr)
}

// downloadFile performs a single HTTP GET with a global timeout.
func downloadFile(url, destPath string) error {
	client := &http.Client{Timeout: downloadTimeout}
	resp, err := client.Get(url) //nolint:gosec // URL is constructed internally from known constants
	if err != nil {
		return fmt.Errorf("HTTP GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d downloading %s", resp.StatusCode, url)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", destPath, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("writing %s: %w", destPath, err)
	}
	return nil
}

// validateChecksum computes the SHA256 of tarPath and compares it to the
// first hex token on the first line of csPath (standard sha256sum format).
func validateChecksum(tarPath, csPath string) error {
	csBytes, err := os.ReadFile(csPath)
	if err != nil {
		return fmt.Errorf("reading checksum file: %w", err)
	}
	fields := strings.Fields(strings.TrimSpace(string(csBytes)))
	if len(fields) == 0 {
		return fmt.Errorf("checksum file is empty or malformed: %s", csPath)
	}
	expected := fields[0]

	f, err := os.Open(tarPath)
	if err != nil {
		return fmt.Errorf("opening tarball: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hashing tarball: %w", err)
	}
	actual := hex.EncodeToString(h.Sum(nil))

	if actual != expected {
		return fmt.Errorf("SHA256 mismatch:\n  expected: %s\n  actual:   %s", expected, actual)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────
// dso system doctor
// ─────────────────────────────────────────────────────────────

func newSystemDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose DSO installation and runtime environment (read-only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

			fmt.Fprintf(w, "DSO System Diagnostics — %s\n", version)
			fmt.Fprintln(w, strings.Repeat("═", 68))
			fmt.Fprintln(w, "Component\tStatus\tDetail")
			fmt.Fprintln(w, strings.Repeat("─", 68))

			// ── Binary ────────────────────────────────────────────────────
			exe, _ := os.Executable()
			fmt.Fprintf(w, "Binary\tOK\t%s (%s)\n", exe, version)

			// ── Effective UID ─────────────────────────────────────────────
			uid := os.Geteuid()
			uidStr := fmt.Sprintf("%d", uid)
			if uid == 0 {
				uidStr += " (root)"
			}
			fmt.Fprintf(w, "Effective UID\t%s\t\n", uidStr)

			// ── Mode Detection ────────────────────────────────────────────
			mode, reason := detectMode("", "")
			fmt.Fprintf(w, "Detected Mode\t%s\tReason: %s\n", strings.ToUpper(mode), reason)

			// ── Config (/etc/dso/dso.yaml) ────────────────────────────────
			configStatus, configDetail := checkPath("/etc/dso/dso.yaml")
			fmt.Fprintf(w, "Config\t%s\t%s\n", configStatus, configDetail)

			// ── Vault (~/.dso/vault.enc) ──────────────────────────────────
			home, _ := os.UserHomeDir()
			vaultPath := filepath.Join(home, ".dso", "vault.enc")
			vaultStatus, vaultDetail := checkPath(vaultPath)
			fmt.Fprintf(w, "Vault\t%s\t%s\n", vaultStatus, vaultDetail)

			// ── Systemd service ───────────────────────────────────────────
			svcPath := filepath.Join(systemdServiceDir, serviceFile)
			svcFileStatus, _ := checkPath(svcPath)
			var svcRunStatus string
			if runtime.GOOS != "linux" {
				svcRunStatus = "not supported (non-Linux)"
			} else {
				out, err := exec.Command("systemctl", "is-active", "dso-agent").Output()
				if err == nil {
					svcRunStatus = strings.TrimSpace(string(out))
				} else {
					svcRunStatus = "inactive/unknown"
				}
			}
			fmt.Fprintf(w, "Systemd Service\t%s\tFile: %s | Runtime: %s\n",
				svcFileStatus, svcPath, svcRunStatus)

			// ── Plugins (existence + executability) ───────────────────────
			knownPlugins := []string{"aws", "azure", "vault", "huawei"}
			for _, p := range knownPlugins {
				pluginPath := filepath.Join(pluginInstallDir, "dso-provider-"+p)
				status, detail := checkPluginPath(pluginPath)
				// Best-effort version probe — silently skipped if plugin missing or unresponsive
				if status == "OK" {
					if out, err := exec.Command(pluginPath, "--version").Output(); err == nil {
						plugVer := strings.TrimSpace(string(out))
						if plugVer != "" {
							detail = fmt.Sprintf("%s (version: %s)", detail, plugVer)
						}
					}
				}
				fmt.Fprintf(w, "Plugin: %s\t%s\t%s\n", p, status, detail)
			}

			fmt.Fprintln(w, strings.Repeat("═", 68))
			w.Flush()
			return nil
		},
	}
}

// checkPath returns (status, detail) for a simple filesystem existence check.
func checkPath(path string) (string, string) {
	if _, err := os.Stat(path); err == nil {
		return "OK", path
	}
	return "NOT FOUND", path
}

// checkPluginPath checks that the plugin exists AND has an executable bit set.
// Returns "OK", "MISSING", or "INVALID" with an explanatory detail string.
func checkPluginPath(path string) (string, string) {
	info, err := os.Stat(path)
	if err != nil {
		return "MISSING", path
	}
	if info.Mode()&0o111 == 0 {
		return "INVALID", fmt.Sprintf("%s (not executable)", path)
	}
	return "OK", path
}
