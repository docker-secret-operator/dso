package cli

import (
	"bufio"
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

// knownProviders is the central registry of all supported provider names.
// The key is the short name used in --providers flags and dso.yaml.
// The value is the binary filename installed under pluginInstallDir.
var knownProviders = map[string]string{
	"vault":  "dso-provider-vault",
	"aws":    "dso-provider-aws",
	"azure":  "dso-provider-azure",
	"huawei": "dso-provider-huawei",
}

// defaultProviders are installed when no --providers flag is given.
var defaultProviders = []string{"vault"}

// systemdTemplate is the canonical service definition for the cloud agent.
const systemdTemplate = `[Unit]
Description=DSO Agent (Cloud Mode)
After=network.target docker.service
Requires=docker.service

[Service]
Type=simple
ExecStart=/usr/local/bin/dso legacy-agent --api-addr 127.0.0.1:8080
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
// docker dso system setup
// ─────────────────────────────────────────────────────────────

func newSystemSetupCmd() *cobra.Command {
	var providersFlag string
	var listProvidersFlag bool

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure DSO for Cloud Mode (requires root)",
		Long: `Installs the systemd service and selected provider plugins for Cloud Mode.
Must be run as root (sudo).

Providers to install can be specified via:
  --providers aws,vault       CLI flag
  DSO_PROVIDERS=aws,vault     environment variable
  (no flag)                   interactive prompt if terminal, else default (vault)

Available providers: vault, aws, azure, huawei`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if listProvidersFlag {
				fmt.Println("Available providers:")
				for _, p := range sortedKeys(knownProviders) {
					if p == "vault" {
						fmt.Printf("- %s (default)\n", p)
					} else {
						fmt.Printf("- %s\n", p)
					}
				}
				fmt.Println("- file (local mode only)")
				return nil
			}

			// ── Privilege guard ──────────────────────────────────────────────
			if os.Geteuid() != 0 {
				return fmt.Errorf("'docker dso system setup' must be run as root: re-run with sudo docker dso system setup")
			}

			// Non-Linux guard: systemd operations are Linux-only.
			if runtime.GOOS != "linux" {
				return fmt.Errorf("'docker dso system setup' is only supported on Linux with systemd: detected OS %s", runtime.GOOS)
			}

			// ── Resolve provider list (flag → env → interactive → default) ──
			selectedProviders, err := resolveProviders(providersFlag)
			if err != nil {
				return err
			}

			fmt.Println("[DSO] Starting Cloud Mode setup...")
			fmt.Printf("[DSO] Providers to install: %s\n", strings.Join(selectedProviders, ", "))

			// Track rollback state so defer is atomic.
			svcPath := filepath.Join(systemdServiceDir, serviceFile)
			svcWritten := false
			pluginsWritten := false

			// ── Deferred atomic rollback ─────────────────────────────────────
			var setupErr error
			defer func() {
				if setupErr == nil {
					return
				}
				fmt.Fprintln(os.Stderr, "[DSO] ⚠️  Setup failed — rolling back partial state...")
				if pluginsWritten {
					// Remove only the plugins we successfully wrote, not the entire directory
					for _, p := range selectedProviders {
						bin := filepath.Join(pluginInstallDir, knownProviders[p])
						// Only rollback if we actually tried to install it this run
						if rErr := os.Remove(bin); rErr == nil {
							fmt.Fprintf(os.Stderr, "[DSO]   rolled back: %s removed.\n", bin)
						}
					}
				}
				if svcWritten {
					if rErr := os.Remove(svcPath); rErr != nil {
						fmt.Fprintf(os.Stderr, "[DSO]   warn: failed to remove systemd service: %v\n", rErr)
					} else {
						fmt.Fprintln(os.Stderr, "[DSO]   rolled back: systemd service removed.")
					}
				}
			}()

			// ── Step 1: Create /etc/dso config directory ─────────────────────
			fmt.Println("[DSO] Creating /etc/dso...")
			if setupErr = os.MkdirAll(dsoConfigDir, 0o750); setupErr != nil {
				return fmt.Errorf("failed to create %s: %w", dsoConfigDir, setupErr)
			}

			// ── Step 2: Write systemd service file ───────────────────────────
			fmt.Printf("[DSO] Writing systemd service to %s...\n", svcPath)
			if setupErr = os.WriteFile(svcPath, []byte(systemdTemplate), 0o600); setupErr != nil {
				setupErr = fmt.Errorf("failed to write systemd service: %w", setupErr)
				return setupErr
			}
			svcWritten = true

			// ── Step 3: Install only selected plugins ────────────────────────
			if setupErr = installProviders(selectedProviders); setupErr != nil {
				setupErr = fmt.Errorf("plugin installation failed: %w", setupErr)
				return setupErr
			}
			pluginsWritten = true

			// ── Step 4: Activate systemd service ─────────────────────────────
			if setupErr = activateSystemd(); setupErr != nil {
				return setupErr
			}

			// ── Final success output ──────────────────────────────────────────
			fmt.Println("")
			fmt.Println("[DSO] ✅ Cloud mode configured successfully.")
			fmt.Println("       Agent:   running (dso-agent.service)")
			fmt.Printf("       Plugins: %s → %s\n", strings.Join(selectedProviders, ", "), pluginInstallDir)
			fmt.Println("       Monitor: journalctl -u dso-agent -f")
			fmt.Println("")
			fmt.Println("[DSO] To add more providers later, re-run:")
			fmt.Println("       sudo docker dso system setup --providers aws,azure")
			return nil
		},
	}

	providerList := strings.Join(sortedKeys(knownProviders), ", ")
	cmd.Flags().StringVar(
		&providersFlag,
		"providers",
		"",
		fmt.Sprintf("Comma-separated list of providers to install (%s)", providerList),
	)
	cmd.Flags().BoolVar(
		&listProvidersFlag,
		"list-providers",
		false,
		"List available providers and exit",
	)

	return cmd
}

// resolveProviders determines which providers to install from the flag,
// DSO_PROVIDERS env var, an interactive prompt (if stdin is a terminal), or
// the default list. Returns a validated, deduplicated slice.
func resolveProviders(flagValue string) ([]string, error) {
	raw := flagValue

	// Fall back to environment variable
	if raw == "" {
		raw = os.Getenv("DSO_PROVIDERS")
	}

	// If still empty and we have a real terminal, ask interactively
	if raw == "" {
		if isTerminal() {
			raw = promptProviders()
		}
	}

	// Final fallback: install only vault
	if raw == "" {
		fmt.Printf("[DSO] No providers specified — using default: %s\n", strings.Join(defaultProviders, ", "))
		return defaultProviders, nil
	}

	return validateProviders(raw)
}

// validateProviders parses a comma-separated provider string, validates each
// name against knownProviders, and returns a deduplicated slice.
func validateProviders(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	seen := make(map[string]bool)
	result := []string{}

	for _, p := range parts {
		name := strings.TrimSpace(strings.ToLower(p))
		if name == "" {
			continue
		}
		if _, ok := knownProviders[name]; !ok {
			available := strings.Join(sortedKeys(knownProviders), ", ")
			return nil, fmt.Errorf("unknown provider %q: available providers: %s; fix: sudo docker dso system setup --providers %s", name, available, available)
		}
		if !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("no valid providers specified after parsing '%s'", raw)
	}
	return result, nil
}

// isTerminal returns true when stdin is an interactive terminal.
func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// promptProviders presents an interactive numbered menu and returns the
// comma-separated selection string (e.g. "vault,aws").
func promptProviders() string {
	ordered := []string{"vault", "aws", "azure", "huawei"}
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Println("")
		fmt.Println("[DSO] Select providers to install:")
		for i, p := range ordered {
			if p == "vault" {
				fmt.Printf("  [%d] %-10s  (%s) [default]\n", i+1, p, knownProviders[p])
			} else {
				fmt.Printf("  [%d] %-10s  (%s)\n", i+1, p, knownProviders[p])
			}
		}
		fmt.Printf("\n  Select providers (e.g. 1,3), 'all', or press Enter for default (vault): ")

		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)

		if line == "" {
			return "" // caller will use default
		}

		if strings.ToLower(line) == "all" {
			return strings.Join(ordered, ",")
		}

		// Map numbers → provider names
		var selected []string
		invalid := false
		for _, tok := range strings.Split(line, ",") {
			tok = strings.TrimSpace(tok)
			var idx int
			if n, err := fmt.Sscanf(tok, "%d", &idx); n == 1 && err == nil {
				if idx >= 1 && idx <= len(ordered) {
					selected = append(selected, ordered[idx-1])
				} else {
					invalid = true
				}
			} else {
				invalid = true
			}
		}

		if invalid || len(selected) == 0 {
			fmt.Println("[DSO] ⚠️  Invalid selection. Please try again.")
			continue
		}

		return strings.Join(selected, ",")
	}
}

// installProviders downloads and extracts only the requested provider plugin
// binaries from the GitHub release tarball. It preserves existing plugins
// in the directory that were not part of this install run.
func installProviders(providers []string) error {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	ver := version

	if ver == "dev" {
		return fmt.Errorf("cannot install plugins for a development build (version=dev): use a release binary with a proper version tag")
	}

	// Filter out already installed providers
	toInstall := []string{}
	preserved := []string{}
	for _, p := range providers {
		binName := knownProviders[p]
		dst := filepath.Join(pluginInstallDir, binName)
		if out, err := exec.Command(dst, "--version").Output(); err == nil { // #nosec G204 -- dst is built from fixed directories and known provider names.
			plugVer := strings.TrimSpace(string(out))
			if plugVer == ver {
				fmt.Printf("[DSO] Provider '%s' already installed (v%s) — skipping\n", p, ver)
				preserved = append(preserved, p)
				continue
			}
		}
		toInstall = append(toInstall, p)
	}

	if len(toInstall) == 0 {
		fmt.Printf("[DSO] All requested providers are already installed.\n")
		return nil
	}

	tarballName := fmt.Sprintf("dso-plugins-%s-%s-%s.tar.gz", goos, goarch, ver)
	checksumName := tarballName + ".sha256"
	tarURL := fmt.Sprintf("%s/%s/%s", releaseBaseURL, ver, tarballName)
	csURL := fmt.Sprintf("%s/%s/%s", releaseBaseURL, ver, checksumName)

	// ── Download to temp directory ───────────────────────────────────────────
	tmpDir, err := os.MkdirTemp("", "dso-plugins-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	fmt.Printf("[DSO] Downloading plugin bundle from %s...\n", tarURL)
	tarPath := filepath.Join(tmpDir, tarballName)
	if err := downloadFileWithRetry(tarURL, tarPath); err != nil {
		return fmt.Errorf("failed to download plugins: %w", err)
	}

	fmt.Printf("[DSO] Downloading checksum from %s...\n", csURL)
	csPath := filepath.Join(tmpDir, checksumName)
	if err := downloadFileWithRetry(csURL, csPath); err != nil {
		return fmt.Errorf("failed to download checksum: %w", err)
	}

	// ── Validate SHA256 ──────────────────────────────────────────────────────
	fmt.Println("[DSO] Validating plugin bundle integrity (SHA256)...")
	if err := validateChecksum(tarPath, csPath); err != nil {
		return fmt.Errorf("integrity check failed: %w", err)
	}

	// ── Extract all plugins to a staging directory ───────────────────────────
	stageDir := filepath.Join(tmpDir, "stage")
	if err := os.MkdirAll(stageDir, 0o755); err != nil { // #nosec G301 -- staging only contains executable plugin artifacts.
		return fmt.Errorf("failed to create staging dir: %w", err)
	}
	tarCmd := exec.Command("tar", "-xzf", tarPath, "-C", stageDir, "--strip-components=1") // #nosec G204 -- tarPath and stageDir are created under a private temp dir.
	tarCmd.Stdout = os.Stdout
	tarCmd.Stderr = os.Stderr
	if err := tarCmd.Run(); err != nil {
		return fmt.Errorf("tarball extraction failed: %w", err)
	}

	// ── Ensure plugin install directory exists (preserve existing plugins) ───
	if err := os.MkdirAll(pluginInstallDir, 0o755); err != nil { // #nosec G301 -- plugin binaries must be traversable/executable system-wide.
		return fmt.Errorf("failed to create plugin dir %s: %w", pluginInstallDir, err)
	}

	// ── Copy ONLY the selected plugins ──────────────────────────────────────
	var successful []string
	var failed []string

	for _, p := range toInstall {
		binName := knownProviders[p]
		src := filepath.Join(stageDir, binName)
		dst := filepath.Join(pluginInstallDir, binName)

		srcInfo, err := os.Stat(src)
		if err != nil {
			fmt.Printf("[DSO] ⚠️  Provider '%s' binary not found in release bundle.\n", p)
			failed = append(failed, p)
			continue
		}
		if srcInfo.Mode()&0o111 == 0 {
			if err := os.Chmod(src, 0o755); err != nil { // #nosec G302 -- provider plugins are executable binaries.
				fmt.Printf("[DSO] ⚠️  Failed to set executable on staged %s: %v\n", binName, err)
				failed = append(failed, p)
				continue
			}
		}

		// Copy src → dst
		if err := copyFile(src, dst); err != nil {
			fmt.Printf("[DSO] ⚠️  Failed to install %s: %v\n", binName, err)
			failed = append(failed, p)
			continue
		}
		if err := os.Chmod(dst, 0o755); err != nil { // #nosec G302 -- provider plugins are executable binaries.
			fmt.Printf("[DSO] ⚠️  Failed to set permissions on %s: %v\n", dst, err)
			failed = append(failed, p)
			continue
		}

		// Validate the installed binary responds to --version
		_, err = exec.Command(dst, "--version").Output() // #nosec G204 -- dst is built from fixed directories and known provider names.
		if err != nil {
			fmt.Printf("[DSO] ⚠️  Plugin validation failed for '%s': binary installed but did not respond to --version\n", p)
			failed = append(failed, p)
			continue
		}

		successful = append(successful, p)
	}

	// Print summary
	if len(successful) > 0 {
		fmt.Printf("[DSO] Installed: %s\n", strings.Join(successful, ", "))
	}
	if len(preserved) > 0 {
		fmt.Printf("[DSO] Preserved: %s\n", strings.Join(preserved, ", "))
	}
	if len(failed) > 0 {
		fmt.Printf("[DSO] Failed: %s\n", strings.Join(failed, ", "))
		return fmt.Errorf("failed to install some providers: %s; fix: sudo docker dso system setup --providers %s", strings.Join(failed, ", "), strings.Join(failed, ","))
	}

	return nil
}

// copyFile performs a byte-for-byte copy from src to dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src) // #nosec G304 -- src is constrained by release extraction code before copyFile is called.
	if err != nil {
		return err
	}
	defer func() {
		_ = in.Close()
	}()

	out, err := os.Create(dst) // #nosec G304 -- dst is built from the fixed plugin install directory and known provider names.
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Close()
	}()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// sortedKeys returns the keys of a map[string]string in a stable order.
func sortedKeys(m map[string]string) []string {
	// Return in a fixed, human-friendly order rather than random map iteration.
	preferred := []string{"vault", "aws", "azure", "huawei"}
	result := []string{}
	for _, k := range preferred {
		if _, ok := m[k]; ok {
			result = append(result, k)
		}
	}
	return result
}

// activateSystemd runs daemon-reload, enable, and restart atomically.
func activateSystemd() error {
	cmds := [][]string{
		{"systemctl", "daemon-reload"},
		{"systemctl", "enable", "dso-agent"},
		{"systemctl", "restart", "dso-agent"},
	}
	for _, args := range cmds {
		fmt.Printf("[DSO] Running: %s\n", strings.Join(args, " "))
		c := exec.Command(args[0], args[1:]...) // #nosec G204 -- command list is fixed in activateSystemd.
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			return fmt.Errorf(
				"'%s' failed: %w\n  Diagnose with: journalctl -xe -u dso-agent",
				strings.Join(args, " "), err,
			)
		}
	}

	fmt.Println("[DSO] Verifying dso-agent service status...")
	out, err := exec.Command("systemctl", "is-active", "dso-agent").Output()
	status := strings.TrimSpace(string(out))
	if err != nil || status != "active" {
		return fmt.Errorf("dso-agent service failed to start. Status: %s\n  Diagnose with: journalctl -xe -u dso-agent", status)
	}

	return nil
}

// downloadFileWithRetry performs an HTTP GET with a timeout and retries.
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
		return nil
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
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d downloading %s", resp.StatusCode, url)
	}

	f, err := os.Create(destPath) // #nosec G304 -- destination is a caller-controlled temp or fixed system path.
	if err != nil {
		return fmt.Errorf("create %s: %w", destPath, err)
	}
	defer func() {
		_ = f.Close()
	}()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("writing %s: %w", destPath, err)
	}
	return nil
}

// validateChecksum computes the SHA256 of tarPath and compares it to the
// first hex token on the first line of csPath (standard sha256sum format).
func validateChecksum(tarPath, csPath string) error {
	csBytes, err := os.ReadFile(csPath) // #nosec G304 -- checksum path is created under a private temp dir.
	if err != nil {
		return fmt.Errorf("reading checksum file: %w", err)
	}
	fields := strings.Fields(strings.TrimSpace(string(csBytes)))
	if len(fields) == 0 {
		return fmt.Errorf("checksum file is empty or malformed: %s", csPath)
	}
	expected := fields[0]

	f, err := os.Open(tarPath) // #nosec G304 -- tar path is created under a private temp dir.
	if err != nil {
		return fmt.Errorf("opening tarball: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

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
// docker dso system doctor
// ─────────────────────────────────────────────────────────────

func newSystemDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose DSO installation and runtime environment (read-only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

			_, _ = fmt.Fprintf(w, "DSO System Diagnostics — %s\n", version)
			_, _ = fmt.Fprintln(w, strings.Repeat("═", 68))
			_, _ = fmt.Fprintln(w, "Component\tStatus\tDetail")
			_, _ = fmt.Fprintln(w, strings.Repeat("─", 68))

			// ── Binary ──────────────────────────────────────────────────────
			exe, _ := os.Executable()
			_, _ = fmt.Fprintf(w, "Binary\tOK\t%s (%s)\n", exe, version)

			// ── Effective UID ────────────────────────────────────────────────
			uid := os.Geteuid()
			uidStr := fmt.Sprintf("%d", uid)
			if uid == 0 {
				uidStr += " (root)"
			}
			_, _ = fmt.Fprintf(w, "Effective UID\t%s\t\n", uidStr)

			// ── Mode Detection ───────────────────────────────────────────────
			mode, reason := detectMode("", "")
			_, _ = fmt.Fprintf(w, "Detected Mode\t%s\tReason: %s\n", strings.ToUpper(mode), reason)

			// ── Config (/etc/dso/dso.yaml) ───────────────────────────────────
			configStatus, configDetail := checkPath("/etc/dso/dso.yaml")
			_, _ = fmt.Fprintf(w, "Config\t%s\t%s\n", configStatus, configDetail)

			// ── Vault (~/.dso/vault.enc) ─────────────────────────────────────
			home, _ := os.UserHomeDir()
			vaultPath := filepath.Join(home, ".dso", "vault.enc")
			vaultStatus, vaultDetail := checkPath(vaultPath)
			_, _ = fmt.Fprintf(w, "Vault\t%s\t%s\n", vaultStatus, vaultDetail)

			// ── Systemd service ──────────────────────────────────────────────
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
			_, _ = fmt.Fprintf(w, "Systemd Service\t%s\tFile: %s | Runtime: %s\n",
				svcFileStatus, svcPath, svcRunStatus)

			// ── Plugins: only report plugins that are installed OR expected ───
			// We check all known providers but display a clear remediation hint
			// for any that are missing, distinguishing intentionally-not-installed
			// from broken installs.
			_, _ = fmt.Fprintln(w, strings.Repeat("─", 68))
			_, _ = fmt.Fprintln(w, "Provider Plugins\t\t")
			_, _ = fmt.Fprintln(w, strings.Repeat("─", 68))

			anyPluginInstalled := false
			for _, p := range sortedKeys(knownProviders) {
				pluginPath := filepath.Join(pluginInstallDir, knownProviders[p])
				info, statErr := os.Stat(pluginPath)

				if statErr != nil {
					// Not installed — show hint instead of a scary "MISSING"
					_, _ = fmt.Fprintf(w, "Plugin: %-8s\tNOT INSTALLED\t\n", p)
					_, _ = fmt.Fprintf(w, "  └─ Fix: sudo docker dso system setup --providers %s\t\t\n", p)
					continue
				}

				anyPluginInstalled = true

				if info.Mode()&0o111 == 0 {
					_, _ = fmt.Fprintf(w, "Plugin: %-8s\tINVALID\t%s (not executable — re-run system setup)\n", p, pluginPath)
					continue
				}

				// Best-effort version probe
				detail := pluginPath
				if out, err := exec.Command(pluginPath, "--version").Output(); err == nil { // #nosec G204 -- pluginPath is built from fixed plugin dir and known provider names.
					plugVer := strings.TrimSpace(string(out))
					if plugVer != "" {
						detail = fmt.Sprintf("%s (%s)", pluginPath, plugVer)
					}
				} else {
					detail = fmt.Sprintf("%s (version probe failed)", pluginPath)
				}
				_, _ = fmt.Fprintf(w, "Plugin: %-8s\tOK\t%s\n", p, detail)
			}

			if !anyPluginInstalled {
				_, _ = fmt.Fprintln(w, strings.Repeat("─", 68))
				_, _ = fmt.Fprintln(w, "ℹ️  No provider plugins installed.")
				_, _ = fmt.Fprintln(w, "   Run: sudo docker dso system setup --providers vault")
			}

			_, _ = fmt.Fprintln(w, strings.Repeat("═", 68))
			return w.Flush()
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
