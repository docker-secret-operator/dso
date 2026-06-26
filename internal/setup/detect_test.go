package setup

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// ─── Helpers ─────────────────────────────────────────────────────────────────

// noopDetectorConfig returns a DetectorConfig where every system call returns
// "not found". Tests override individual fields as needed.
func noopDetectorConfig() DetectorConfig {
	return DetectorConfig{
		Getenv:            func(string) string { return "" },
		LookPath:          func(string) (string, error) { return "", exec.ErrNotFound },
		Stat:              func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
		ReadFile:          func(string) ([]byte, error) { return nil, os.ErrNotExist },
		DockerSocketPaths: []string{"/var/run/docker.sock"},
		DockerTimeout:     100 * time.Millisecond,
		SystemdTimeout:    100 * time.Millisecond,
	}
}

// ─── Detector orchestrator ────────────────────────────────────────────────────

func TestDetector_Detect_ReturnsNonNilEnvironment(t *testing.T) {
	d := newDetector()
	env, err := d.Detect(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env == nil {
		t.Fatal("expected non-nil Environment")
	}
}

func TestDetector_Detect_SetsTimestamp(t *testing.T) {
	before := time.Now()
	d := newDetector()
	env, _ := d.Detect(context.Background())
	if env.Timestamp.Before(before) {
		t.Error("Timestamp should be >= time before Detect() was called")
	}
}

func TestDetector_Detect_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := newDetector()
	env, err := d.Detect(ctx)
	if err != nil {
		t.Fatalf("Detect should never return a non-nil error, got: %v", err)
	}
	if env == nil {
		t.Fatal("expected non-nil Environment even with cancelled context")
	}
}

func TestDetector_Detect_SetsCapabilities(t *testing.T) {
	d := newDetector()
	env, _ := d.Detect(context.Background())
	// SupportsLocalMode is always true.
	if !env.Capabilities.SupportsLocalMode {
		t.Error("SupportsLocalMode should always be true")
	}
}

func TestDetector_Detect_CapabilitiesMatchRawFacts(t *testing.T) {
	d := newDetector()
	env, _ := d.Detect(context.Background())
	// Capabilities must be consistent with the raw facts they derive from.
	if env.Capabilities.SupportsDocker != env.Docker.DaemonReachable {
		t.Errorf("SupportsDocker (%v) must equal Docker.DaemonReachable (%v)",
			env.Capabilities.SupportsDocker, env.Docker.DaemonReachable)
	}
	if env.Capabilities.SupportsAgentMode != (env.Systemd.Available && env.User.IsRoot) {
		t.Error("SupportsAgentMode must equal Systemd.Available && User.IsRoot")
	}
}

// ─── computeCapabilities ─────────────────────────────────────────────────────

func TestComputeCapabilities_LocalModeAlwaysTrue(t *testing.T) {
	caps := computeCapabilities(&Environment{})
	if !caps.SupportsLocalMode {
		t.Error("SupportsLocalMode should always be true")
	}
}

func TestComputeCapabilities_DockerFromDaemonReachable(t *testing.T) {
	env := &Environment{Docker: DockerInfo{DaemonReachable: true}}
	caps := computeCapabilities(env)
	if !caps.SupportsDocker {
		t.Error("SupportsDocker should be true when DaemonReachable is true")
	}
}

func TestComputeCapabilities_AgentModeRequiresSystemdAndRoot(t *testing.T) {
	tests := []struct {
		name      string
		systemd   bool
		root      bool
		wantAgent bool
	}{
		{"systemd+root", true, true, true},
		{"systemd+nonroot", true, false, false},
		{"nosystemd+root", false, true, false},
		{"nosystemd+nonroot", false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := &Environment{
				Systemd: SystemdInfo{Available: tt.systemd},
				User:    UserInfo{IsRoot: tt.root},
			}
			caps := computeCapabilities(env)
			if caps.SupportsAgentMode != tt.wantAgent {
				t.Errorf("SupportsAgentMode: want %v, got %v", tt.wantAgent, caps.SupportsAgentMode)
			}
		})
	}
}

// ─── detectOS ─────────────────────────────────────────────────────────────────

func TestDetectOS_GOOS(t *testing.T) {
	cfg := noopDetectorConfig()
	info, _ := detectOS(cfg)
	if info.GOOS != runtime.GOOS {
		t.Errorf("want %q, got %q", runtime.GOOS, info.GOOS)
	}
}

func TestDetectOS_Architecture(t *testing.T) {
	cfg := noopDetectorConfig()
	info, _ := detectOS(cfg)
	if info.Architecture != runtime.GOARCH {
		t.Errorf("want %q, got %q", runtime.GOARCH, info.Architecture)
	}
}

func TestDetectOS_WarningWhenReadFileFails(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("os-release reading only applicable on Linux")
	}
	cfg := noopDetectorConfig()
	cfg.ReadFile = func(string) ([]byte, error) {
		return nil, errors.New("permission denied")
	}
	_, warns := detectOS(cfg)
	if len(warns) == 0 {
		t.Error("expected a warning when ReadFile fails on Linux")
	}
	if len(warns) > 0 && warns[0].Code != "os_release_read_failed" {
		t.Errorf("warn.Code: want os_release_read_failed, got %q", warns[0].Code)
	}
}

func TestDetectOS_NoWarningOnRealSystem(t *testing.T) {
	cfg := noopDetectorConfig()
	cfg.ReadFile = os.ReadFile // use the real filesystem
	_, warns := detectOS(cfg)
	// On Linux with a normal /etc/os-release there should be no warnings.
	// On macOS the stub never reads a file so there's also no warning.
	if runtime.GOOS == "linux" {
		// Only flag if there's an unexpected warning code.
		for _, w := range warns {
			if w.Code != "" {
				t.Logf("detection warning on real system: %s: %s", w.Code, w.Message)
			}
		}
	}
}

// ─── detectUser ──────────────────────────────────────────────────────────────

func TestDetectUser_ReturnsCurrentUser(t *testing.T) {
	info, warns := detectUser()
	for _, w := range warns {
		t.Logf("detection warning: %s: %s", w.Code, w.Message)
	}
	if info.Username == "" {
		t.Error("expected non-empty Username")
	}
	if info.UID == "" {
		t.Error("expected non-empty UID")
	}
	if info.HomeDir == "" {
		t.Error("expected non-empty HomeDir")
	}
}

func TestDetectUser_IsRootConsistentWithUID(t *testing.T) {
	info, _ := detectUser()
	if info.UID == "0" && !info.IsRoot {
		t.Error("IsRoot should be true when UID is 0")
	}
	if info.UID != "0" && info.IsRoot {
		t.Error("IsRoot should be false when UID is not 0")
	}
}

// ─── detectDocker ─────────────────────────────────────────────────────────────

func TestDetectDocker_NotFoundWhenLookPathFails(t *testing.T) {
	cfg := noopDetectorConfig()
	info, warns := detectDocker(context.Background(), cfg)
	if info.BinaryFound {
		t.Error("BinaryFound should be false when LookPath returns ErrNotFound")
	}
	if info.DaemonReachable {
		t.Error("DaemonReachable should be false when binary not found")
	}
	// No warning when binary simply isn't installed.
	if len(warns) != 0 {
		t.Errorf("expected no warnings for missing binary, got %d", len(warns))
	}
}

func TestDetectDocker_BinaryFoundWhenLookPathSucceeds(t *testing.T) {
	cfg := noopDetectorConfig()
	cfg.LookPath = func(name string) (string, error) {
		if name == "docker" {
			return "/usr/local/bin/docker", nil
		}
		return "", exec.ErrNotFound
	}
	info, _ := detectDocker(context.Background(), cfg)
	if !info.BinaryFound {
		t.Error("BinaryFound should be true when LookPath succeeds")
	}
	if info.BinaryPath != "/usr/local/bin/docker" {
		t.Errorf("BinaryPath: want /usr/local/bin/docker, got %q", info.BinaryPath)
	}
}

func TestDetectDocker_BinaryFoundButDaemonUnreachable_EmitsWarning(t *testing.T) {
	cfg := noopDetectorConfig()
	cfg.LookPath = func(name string) (string, error) {
		if name == "docker" {
			return "/usr/local/bin/docker", nil // fake path — exec will fail
		}
		return "", exec.ErrNotFound
	}
	_, warns := detectDocker(context.Background(), cfg)
	if len(warns) == 0 {
		t.Error("expected warning when binary found but daemon unreachable")
	}
	if len(warns) > 0 && warns[0].Code != "docker_daemon_unreachable" {
		t.Errorf("warn.Code: want docker_daemon_unreachable, got %q", warns[0].Code)
	}
}

func TestDetectDocker_SocketFoundAndFlagSet(t *testing.T) {
	cfg := noopDetectorConfig()
	cfg.LookPath = func(name string) (string, error) {
		if name == "docker" {
			return "/usr/bin/docker", nil
		}
		return "", exec.ErrNotFound
	}
	cfg.Stat = func(path string) (os.FileInfo, error) {
		if path == "/var/run/docker.sock" {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}
	info, _ := detectDocker(context.Background(), cfg)
	if !info.SocketFound {
		t.Error("SocketFound should be true when Stat succeeds")
	}
	if info.SocketPath != "/var/run/docker.sock" {
		t.Errorf("SocketPath: want /var/run/docker.sock, got %q", info.SocketPath)
	}
}

func TestDetectDocker_SocketPermissionDenied_SocketNotFound(t *testing.T) {
	cfg := noopDetectorConfig()
	cfg.LookPath = func(name string) (string, error) {
		if name == "docker" {
			return "/usr/bin/docker", nil
		}
		return "", exec.ErrNotFound
	}
	cfg.Stat = func(string) (os.FileInfo, error) {
		return nil, os.ErrPermission
	}
	info, _ := detectDocker(context.Background(), cfg)
	if info.SocketFound {
		t.Error("SocketFound should be false when Stat returns permission denied")
	}
	if info.SocketPath != "" {
		t.Errorf("SocketPath should be empty, got %q", info.SocketPath)
	}
}

func TestDetectDocker_NoBinaryPath_SetsNoSocket(t *testing.T) {
	cfg := noopDetectorConfig()
	// Stat would succeed but LookPath fails — SocketPath must stay empty.
	cfg.Stat = func(path string) (os.FileInfo, error) { return nil, nil }
	info, _ := detectDocker(context.Background(), cfg)
	if info.SocketPath != "" {
		t.Error("SocketPath should be empty when binary not found, even if Stat succeeds")
	}
}

// ─── detectSystemd ────────────────────────────────────────────────────────────

func TestDetectSystemd_NotAvailableWhenLookPathFails(t *testing.T) {
	cfg := noopDetectorConfig()
	info, warns := detectSystemd(context.Background(), cfg)
	if info.Available {
		t.Error("Available should be false when systemctl not found")
	}
	if len(warns) != 0 {
		t.Errorf("expected no warnings for missing systemctl, got %d", len(warns))
	}
}

func TestDetectSystemd_AvailableWhenSystemctlFound(t *testing.T) {
	cfg := noopDetectorConfig()
	cfg.LookPath = func(name string) (string, error) {
		if name == "systemctl" {
			return "/bin/systemctl", nil
		}
		return "", exec.ErrNotFound
	}
	info, _ := detectSystemd(context.Background(), cfg)
	if !info.Available {
		t.Error("Available should be true when systemctl found")
	}
	if info.BinaryPath != "/bin/systemctl" {
		t.Errorf("BinaryPath: want /bin/systemctl, got %q", info.BinaryPath)
	}
}

func TestDetectSystemd_WarningWhenVersionCommandFails(t *testing.T) {
	cfg := noopDetectorConfig()
	// Fake path — exec will fail since /fake/systemctl doesn't exist.
	cfg.LookPath = func(name string) (string, error) {
		if name == "systemctl" {
			return "/fake/systemctl", nil
		}
		return "", exec.ErrNotFound
	}
	_, warns := detectSystemd(context.Background(), cfg)
	if len(warns) == 0 {
		t.Error("expected warning when systemctl --version fails")
	}
}

// ─── detectProviders ──────────────────────────────────────────────────────────

func TestDetectProviders_LocalAlwaysInAvailable(t *testing.T) {
	cfg := noopDetectorConfig()
	result, _ := detectProviders(cfg)
	found := false
	for _, p := range result.Available {
		if p == "local" {
			found = true
		}
	}
	if !found {
		t.Errorf("'local' not found in Available: %v", result.Available)
	}
}

func TestDetectProviders_MultipleProviders_AllPresent(t *testing.T) {
	cfg := noopDetectorConfig()
	cfg.Getenv = func(key string) string {
		m := map[string]string{
			"AWS_ACCESS_KEY_ID":     "AKID",
			"AWS_SECRET_ACCESS_KEY": "SECRET",
			"VAULT_ADDR":            "https://vault.example.com",
			"VAULT_TOKEN":           "s.token",
			"AZURE_CLIENT_ID":       "cid",
			"AZURE_CLIENT_SECRET":   "csecret",
			"AZURE_TENANT_ID":       "tid",
		}
		return m[key]
	}
	result, _ := detectProviders(cfg)
	want := map[string]bool{"aws": true, "azure": true, "vault": true, "local": true}
	for _, p := range result.Available {
		delete(want, p)
	}
	if len(want) != 0 {
		t.Errorf("missing providers in Available: %v", want)
	}
}

func TestDetectAWS_DetectedWhenStaticCreds(t *testing.T) {
	cfg := noopDetectorConfig()
	cfg.Getenv = func(key string) string {
		m := map[string]string{
			"AWS_ACCESS_KEY_ID":     "AKIAIOSFODNN7EXAMPLE",
			"AWS_SECRET_ACCESS_KEY": "wJalrXUtnFEMI",
			"AWS_REGION":            "us-east-1",
		}
		return m[key]
	}
	info := detectAWS(cfg)
	if !info.Detected {
		t.Error("expected Detected=true with static credentials")
	}
	if !info.HasStaticCreds {
		t.Error("expected HasStaticCreds=true")
	}
	if info.Region != "us-east-1" {
		t.Errorf("Region: want 'us-east-1', got %q", info.Region)
	}
}

func TestDetectAWS_DetectedWhenSharedCredsExist(t *testing.T) {
	tmp := t.TempDir()
	awsDir := filepath.Join(tmp, ".aws")
	if err := os.MkdirAll(awsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(awsDir, "credentials"), []byte("[default]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := noopDetectorConfig()
	cfg.Getenv = func(key string) string {
		if key == "HOME" {
			return tmp
		}
		return ""
	}
	cfg.Stat = os.Stat
	info := detectAWS(cfg)
	if !info.Detected {
		t.Error("expected Detected=true when ~/.aws/credentials exists")
	}
	if !info.HasSharedCreds {
		t.Error("expected HasSharedCreds=true")
	}
}

func TestDetectAWS_MissingHome_NoCredsFilePanic(t *testing.T) {
	cfg := noopDetectorConfig()
	// HOME not set, os.UserHomeDir will be called — must not panic.
	cfg.Getenv = func(key string) string { return "" }
	// Stat always returns ErrNotExist, so no creds found even if home is resolved.
	info := detectAWS(cfg)
	_ = info // just verify no panic
}

func TestDetectAWS_NotDetectedWhenNoCreds(t *testing.T) {
	cfg := noopDetectorConfig()
	info := detectAWS(cfg)
	if info.Detected {
		t.Error("expected Detected=false when no credentials present")
	}
}

func TestDetectAWS_DetectedWhenRoleARN(t *testing.T) {
	cfg := noopDetectorConfig()
	cfg.Getenv = func(key string) string {
		if key == "AWS_ROLE_ARN" {
			return "arn:aws:iam::123456789012:role/example"
		}
		return ""
	}
	info := detectAWS(cfg)
	if !info.Detected {
		t.Error("expected Detected=true with AWS_ROLE_ARN")
	}
	if !info.HasRole {
		t.Error("expected HasRole=true")
	}
}

func TestDetectVault_DetectedWhenAddrAndToken(t *testing.T) {
	cfg := noopDetectorConfig()
	cfg.Getenv = func(key string) string {
		m := map[string]string{
			"VAULT_ADDR":  "https://vault.example.com",
			"VAULT_TOKEN": "s.exampleToken",
		}
		return m[key]
	}
	info := detectVault(cfg)
	if !info.Detected {
		t.Error("expected Detected=true")
	}
	if info.Address != "https://vault.example.com" {
		t.Errorf("Address: got %q", info.Address)
	}
	if !info.HasToken {
		t.Error("expected HasToken=true")
	}
}

func TestDetectVault_NotDetectedWhenNoAddr(t *testing.T) {
	cfg := noopDetectorConfig()
	cfg.Getenv = func(key string) string {
		if key == "VAULT_TOKEN" {
			return "s.someToken"
		}
		return ""
	}
	info := detectVault(cfg)
	if info.Detected {
		t.Error("expected Detected=false when VAULT_ADDR is missing")
	}
}

func TestDetectVault_DetectedWhenRoleID(t *testing.T) {
	cfg := noopDetectorConfig()
	cfg.Getenv = func(key string) string {
		m := map[string]string{
			"VAULT_ADDR":    "https://vault.example.com",
			"VAULT_ROLE_ID": "role-id-value",
		}
		return m[key]
	}
	info := detectVault(cfg)
	if !info.Detected {
		t.Error("expected Detected=true with VAULT_ROLE_ID")
	}
	if !info.HasRole {
		t.Error("expected HasRole=true")
	}
}

func TestDetectAzure_DetectedWhenEnvCreds(t *testing.T) {
	cfg := noopDetectorConfig()
	cfg.Getenv = func(key string) string {
		m := map[string]string{
			"AZURE_CLIENT_ID":     "client-id",
			"AZURE_CLIENT_SECRET": "client-secret",
			"AZURE_TENANT_ID":     "tenant-id",
		}
		return m[key]
	}
	info := detectAzure(cfg)
	if !info.Detected {
		t.Error("expected Detected=true with Azure env credentials")
	}
	if !info.HasEnvCreds {
		t.Error("expected HasEnvCreds=true")
	}
}

func TestDetectAzure_PartialEnvCreds_NotDetected(t *testing.T) {
	cfg := noopDetectorConfig()
	// Only two of three required env vars set.
	cfg.Getenv = func(key string) string {
		if key == "AZURE_CLIENT_ID" {
			return "cid"
		}
		if key == "AZURE_CLIENT_SECRET" {
			return "csecret"
		}
		return "" // AZURE_TENANT_ID missing
	}
	info := detectAzure(cfg)
	if info.HasEnvCreds {
		t.Error("expected HasEnvCreds=false with incomplete Azure env credentials")
	}
}

func TestDetectAzure_DetectedWhenCLIFound(t *testing.T) {
	cfg := noopDetectorConfig()
	cfg.LookPath = func(name string) (string, error) {
		if name == "az" {
			return "/usr/bin/az", nil
		}
		return "", exec.ErrNotFound
	}
	info := detectAzure(cfg)
	if !info.Detected {
		t.Error("expected Detected=true when az CLI found")
	}
	if !info.HasCLI {
		t.Error("expected HasCLI=true")
	}
}

func TestDetectAzure_NotDetectedByDefault(t *testing.T) {
	cfg := noopDetectorConfig()
	info := detectAzure(cfg)
	if info.Detected {
		t.Error("expected Detected=false with no Azure credentials or CLI")
	}
}

// ─── detectExistingDSO ────────────────────────────────────────────────────────

func TestDetectExistingDSO_NotFoundByDefault(t *testing.T) {
	cfg := noopDetectorConfig()
	info, _ := detectExistingDSO(cfg)
	if info.Installed {
		t.Error("expected Installed=false when no DSO installation present")
	}
	if info.AgentInstalled {
		t.Error("expected AgentInstalled=false")
	}
	if info.ServiceInstalled {
		t.Error("expected ServiceInstalled=false")
	}
}

func TestDetectExistingDSO_InstalledWhenSystemConfigExists(t *testing.T) {
	cfg := noopDetectorConfig()
	cfg.Stat = func(path string) (os.FileInfo, error) {
		if path == dsoSystemConfig {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}
	info, _ := detectExistingDSO(cfg)
	if !info.Installed {
		t.Error("expected Installed=true when system config exists")
	}
	if info.ConfigPath != dsoSystemConfig {
		t.Errorf("ConfigPath: want %q, got %q", dsoSystemConfig, info.ConfigPath)
	}
}

func TestDetectExistingDSO_InstalledWhenUserConfigExists(t *testing.T) {
	tmp := t.TempDir()
	cfg := noopDetectorConfig()
	cfg.Getenv = func(key string) string {
		if key == "HOME" {
			return tmp
		}
		return ""
	}
	cfg.Stat = func(path string) (os.FileInfo, error) {
		userConfig := filepath.Join(tmp, ".dso", "dso.yaml")
		if path == userConfig {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}
	info, _ := detectExistingDSO(cfg)
	if !info.Installed {
		t.Error("expected Installed=true when user config exists")
	}
}

func TestDetectExistingDSO_ServiceInstalledWhenServiceFileExists(t *testing.T) {
	cfg := noopDetectorConfig()
	cfg.Stat = func(path string) (os.FileInfo, error) {
		if path == dsoSystemService {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}
	info, _ := detectExistingDSO(cfg)
	if !info.Installed {
		t.Error("expected Installed=true when service file exists")
	}
	if !info.ServiceInstalled {
		t.Error("expected ServiceInstalled=true when service file exists")
	}
}

func TestDetectExistingDSO_AgentInstalledWhenBinaryFound(t *testing.T) {
	cfg := noopDetectorConfig()
	cfg.LookPath = func(name string) (string, error) {
		if name == "dso" {
			return "/usr/local/bin/dso", nil
		}
		return "", exec.ErrNotFound
	}
	info, _ := detectExistingDSO(cfg)
	if !info.Installed {
		t.Error("expected Installed=true when dso binary found")
	}
	if !info.AgentInstalled {
		t.Error("expected AgentInstalled=true when dso binary found")
	}
}

func TestDetectExistingDSO_MissingHome_NoPanic(t *testing.T) {
	cfg := noopDetectorConfig()
	cfg.Getenv = func(key string) string { return "" } // no HOME
	// Must not panic even when home directory cannot be resolved.
	info, _ := detectExistingDSO(cfg)
	_ = info
}
