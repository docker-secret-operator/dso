package setup

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// ─── Helpers ─────────────────────────────────────────────────────────────────

// noopDetectorConfig returns a DetectorConfig where every system call is
// stubbed to return "not found". Tests layer real values on top as needed.
func noopDetectorConfig() DetectorConfig {
	return DetectorConfig{
		Getenv:            func(string) string { return "" },
		LookPath:          func(string) (string, error) { return "", exec.ErrNotFound },
		Stat:              func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
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
	cancel() // cancel immediately
	d := newDetector()
	env, err := d.Detect(ctx)
	// Detect never errors; cancelled context only affects timed exec calls.
	if err != nil {
		t.Fatalf("unexpected error from Detect with cancelled context: %v", err)
	}
	if env == nil {
		t.Fatal("expected non-nil Environment even with cancelled context")
	}
}

// ─── computeRecommendation ────────────────────────────────────────────────────

func TestComputeRecommendation_LocalModeWhenNoSystemd(t *testing.T) {
	env := &Environment{
		Systemd: SystemdInfo{Available: false},
		User:    UserInfo{IsRoot: true},
	}
	mode, _ := computeRecommendation(env)
	if mode != ModeLocal {
		t.Errorf("want ModeLocal, got %q", mode)
	}
}

func TestComputeRecommendation_AgentModeWhenSystemdAndRoot(t *testing.T) {
	env := &Environment{
		Systemd: SystemdInfo{Available: true},
		User:    UserInfo{IsRoot: true},
	}
	mode, _ := computeRecommendation(env)
	if mode != ModeAgent {
		t.Errorf("want ModeAgent, got %q", mode)
	}
}

func TestComputeRecommendation_LocalModeWhenSystemdButNotRoot(t *testing.T) {
	env := &Environment{
		Systemd: SystemdInfo{Available: true},
		User:    UserInfo{IsRoot: false},
	}
	mode, _ := computeRecommendation(env)
	if mode != ModeLocal {
		t.Errorf("want ModeLocal (not root), got %q", mode)
	}
}

func TestComputeRecommendation_DefaultProviderIsLocal(t *testing.T) {
	env := &Environment{}
	_, provider := computeRecommendation(env)
	if provider != "local" {
		t.Errorf("want 'local', got %q", provider)
	}
}

func TestComputeRecommendation_AWSBeatsAllOthers(t *testing.T) {
	env := &Environment{
		Providers: DetectedProviders{
			AWS:   AWSInfo{Detected: true},
			Azure: AzureInfo{Detected: true},
			Vault: VaultInfo{Detected: true},
		},
	}
	_, provider := computeRecommendation(env)
	if provider != "aws" {
		t.Errorf("want 'aws', got %q", provider)
	}
}

func TestComputeRecommendation_AzureBeatsVault(t *testing.T) {
	env := &Environment{
		Providers: DetectedProviders{
			Azure: AzureInfo{Detected: true},
			Vault: VaultInfo{Detected: true},
		},
	}
	_, provider := computeRecommendation(env)
	if provider != "azure" {
		t.Errorf("want 'azure', got %q", provider)
	}
}

func TestComputeRecommendation_VaultWhenOnlyVault(t *testing.T) {
	env := &Environment{
		Providers: DetectedProviders{
			Vault: VaultInfo{Detected: true},
		},
	}
	_, provider := computeRecommendation(env)
	if provider != "vault" {
		t.Errorf("want 'vault', got %q", provider)
	}
}

// ─── detectOS ─────────────────────────────────────────────────────────────────

func TestDetectOS_GOOS(t *testing.T) {
	info := detectOS()
	if info.GOOS != runtime.GOOS {
		t.Errorf("want %q, got %q", runtime.GOOS, info.GOOS)
	}
}

func TestDetectOS_Architecture(t *testing.T) {
	info := detectOS()
	if info.Architecture != runtime.GOARCH {
		t.Errorf("want %q, got %q", runtime.GOARCH, info.Architecture)
	}
}

func TestParseOSRelease_ValidContent(t *testing.T) {
	// parseOSRelease reads the real /etc/os-release; on Linux it should parse,
	// on macOS the file won't exist so we get empty strings.
	distro, version := parseOSRelease()
	if runtime.GOOS == "linux" {
		if distro == "" {
			t.Error("expected non-empty distro on Linux")
		}
	} else {
		// macOS — file absent, expect empty.
		if distro != "" || version != "" {
			t.Errorf("expected empty on non-Linux, got distro=%q version=%q", distro, version)
		}
	}
}

// ─── detectUser ──────────────────────────────────────────────────────────────

func TestDetectUser_ReturnsCurrentUser(t *testing.T) {
	info := detectUser()
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

func TestDetectUser_IsRootWhenUID0(t *testing.T) {
	info := detectUser()
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
	info := detectDocker(context.Background(), cfg)
	if info.BinaryFound {
		t.Error("BinaryFound should be false when LookPath returns ErrNotFound")
	}
	if info.DaemonAvailable {
		t.Error("DaemonAvailable should be false when binary not found")
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
	info := detectDocker(context.Background(), cfg)
	if !info.BinaryFound {
		t.Error("BinaryFound should be true when LookPath succeeds")
	}
	if info.BinaryPath != "/usr/local/bin/docker" {
		t.Errorf("BinaryPath: want /usr/local/bin/docker, got %q", info.BinaryPath)
	}
	// Daemon is unavailable because /usr/local/bin/docker doesn't actually exist.
	if info.DaemonAvailable {
		t.Error("DaemonAvailable should be false for a fake binary path")
	}
}

func TestDetectDocker_SocketFoundWhenStatSucceeds(t *testing.T) {
	cfg := noopDetectorConfig()
	cfg.LookPath = func(name string) (string, error) {
		if name == "docker" {
			return "/usr/bin/docker", nil
		}
		return "", exec.ErrNotFound
	}
	cfg.Stat = func(path string) (os.FileInfo, error) {
		if path == "/var/run/docker.sock" {
			return nil, nil // signal existence
		}
		return nil, os.ErrNotExist
	}
	info := detectDocker(context.Background(), cfg)
	if info.SocketPath != "/var/run/docker.sock" {
		t.Errorf("SocketPath: want /var/run/docker.sock, got %q", info.SocketPath)
	}
}

func TestDetectDocker_DaemonUnavailableWhenSocketMissing(t *testing.T) {
	cfg := noopDetectorConfig()
	cfg.LookPath = func(name string) (string, error) {
		if name == "docker" {
			return "/usr/bin/docker", nil
		}
		return "", exec.ErrNotFound
	}
	// Stat returns ErrNotExist for all sockets (already default in noopDetectorConfig).
	info := detectDocker(context.Background(), cfg)
	if info.SocketPath != "" {
		t.Errorf("SocketPath should be empty, got %q", info.SocketPath)
	}
}

// ─── detectSystemd ────────────────────────────────────────────────────────────

func TestDetectSystemd_NotAvailableWhenLookPathFails(t *testing.T) {
	cfg := noopDetectorConfig()
	info := detectSystemd(context.Background(), cfg)
	if info.Available {
		t.Error("Available should be false when systemctl not found")
	}
	if info.BinaryPath != "" {
		t.Errorf("BinaryPath should be empty, got %q", info.BinaryPath)
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
	info := detectSystemd(context.Background(), cfg)
	if !info.Available {
		t.Error("Available should be true when systemctl found")
	}
	if info.BinaryPath != "/bin/systemctl" {
		t.Errorf("BinaryPath: want /bin/systemctl, got %q", info.BinaryPath)
	}
	// Version may be empty (fake binary won't run), that's fine.
}

// ─── detectProviders ──────────────────────────────────────────────────────────

func TestDetectProviders_LocalAlwaysInAvailable(t *testing.T) {
	cfg := noopDetectorConfig()
	result := detectProviders(cfg)

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
	credsPath := filepath.Join(awsDir, "credentials")
	if err := os.WriteFile(credsPath, []byte("[default]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := noopDetectorConfig()
	cfg.Getenv = func(key string) string {
		if key == "HOME" {
			return tmp
		}
		return ""
	}
	cfg.Stat = os.Stat // use real Stat so the temp file is found

	info := detectAWS(cfg)
	if !info.Detected {
		t.Error("expected Detected=true when ~/.aws/credentials exists")
	}
	if !info.HasSharedCreds {
		t.Error("expected HasSharedCreds=true")
	}
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
		t.Errorf("Address: want https://vault.example.com, got %q", info.Address)
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
		t.Error("expected Detected=false when no Azure credentials or CLI")
	}
}

// ─── detectExistingDSO ────────────────────────────────────────────────────────

func TestDetectExistingDSO_NotFoundByDefault(t *testing.T) {
	cfg := noopDetectorConfig()
	info := detectExistingDSO(cfg)
	if info.Found {
		t.Error("expected Found=false when no DSO installation present")
	}
}

func TestDetectExistingDSO_FoundWhenSystemConfigExists(t *testing.T) {
	cfg := noopDetectorConfig()
	cfg.Stat = func(path string) (os.FileInfo, error) {
		if path == dsoSystemConfig {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}
	info := detectExistingDSO(cfg)
	if !info.Found {
		t.Error("expected Found=true when system config exists")
	}
	if info.ConfigPath != dsoSystemConfig {
		t.Errorf("ConfigPath: want %q, got %q", dsoSystemConfig, info.ConfigPath)
	}
}

func TestDetectExistingDSO_FoundWhenUserConfigExists(t *testing.T) {
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
	info := detectExistingDSO(cfg)
	if !info.Found {
		t.Error("expected Found=true when user config exists")
	}
}

func TestDetectExistingDSO_FoundWhenServiceExists(t *testing.T) {
	cfg := noopDetectorConfig()
	cfg.Stat = func(path string) (os.FileInfo, error) {
		if path == dsoSystemService {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}
	info := detectExistingDSO(cfg)
	if !info.Found {
		t.Error("expected Found=true when service file exists")
	}
	if info.ServicePath != dsoSystemService {
		t.Errorf("ServicePath: want %q, got %q", dsoSystemService, info.ServicePath)
	}
}
