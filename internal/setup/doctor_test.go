package setup

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// ─── DockerChecks ─────────────────────────────────────────────────────────────

func TestDockerCheck_001_BinaryInstalled_Pass(t *testing.T) {
	dc := newDockerChecks("/var/run/docker.sock")
	dc.lookupBinary = func(_ string) (string, error) { return "/usr/bin/docker", nil }

	c := dc.checkBinaryInstalled()

	assertCheckID(t, c, "DSO-DOCTOR-001")
	assertPass(t, c)
}

func TestDockerCheck_001_BinaryNotFound_Fail(t *testing.T) {
	dc := newDockerChecks("/var/run/docker.sock")
	dc.lookupBinary = func(_ string) (string, error) { return "", errors.New("not found") }

	c := dc.checkBinaryInstalled()

	assertCheckID(t, c, "DSO-DOCTOR-001")
	assertFail(t, c)
	assertHasRecovery(t, c)
	if c.Severity != DoctorCritical {
		t.Errorf("want DoctorCritical, got %q", c.Severity)
	}
}

func TestDockerCheck_002_DaemonReachable_Pass(t *testing.T) {
	dc := newDockerChecks("/var/run/docker.sock")
	dc.runVersion = func(_ context.Context) error { return nil }

	c := dc.checkDaemonReachable(context.Background())

	assertCheckID(t, c, "DSO-DOCTOR-002")
	assertPass(t, c)
}

func TestDockerCheck_002_DaemonUnreachable_Fail(t *testing.T) {
	dc := newDockerChecks("/var/run/docker.sock")
	dc.runVersion = func(_ context.Context) error { return errors.New("connection refused") }

	c := dc.checkDaemonReachable(context.Background())

	assertCheckID(t, c, "DSO-DOCTOR-002")
	assertFail(t, c)
	assertHasRecovery(t, c)
}

func TestDockerCheck_003_SocketAccessible_Pass(t *testing.T) {
	dc := newDockerChecks("/var/run/docker.sock")
	dc.statSocket = func(_ string) (os.FileInfo, error) { return os.Stat(".") }

	c := dc.checkSocketAccessible()

	assertCheckID(t, c, "DSO-DOCTOR-003")
	assertPass(t, c)
}

func TestDockerCheck_003_SocketNotFound_Fail(t *testing.T) {
	dc := newDockerChecks("/var/run/docker.sock")
	dc.statSocket = func(_ string) (os.FileInfo, error) { return nil, os.ErrNotExist }

	c := dc.checkSocketAccessible()

	assertCheckID(t, c, "DSO-DOCTOR-003")
	assertFail(t, c)
	if c.Severity != DoctorHigh {
		t.Errorf("want DoctorHigh, got %q", c.Severity)
	}
}

func TestDockerChecks_run_Returns3Checks(t *testing.T) {
	dc := newDockerChecks("/var/run/docker.sock")
	dc.lookupBinary = func(_ string) (string, error) { return "/usr/bin/docker", nil }
	dc.runVersion = func(_ context.Context) error { return nil }
	dc.statSocket = func(_ string) (os.FileInfo, error) { return os.Stat(".") }

	checks := dc.run(context.Background())
	if len(checks) != 3 {
		t.Errorf("expected 3 docker checks, got %d", len(checks))
	}
}

// ─── PermissionChecks ─────────────────────────────────────────────────────────

func TestPermCheck_004_SocketWorldReadable_Warn(t *testing.T) {
	pc := newPermissionChecks("/var/run/docker.sock", "/etc/dso/dso.yaml")
	pc.statSocket = fakeStatWithMode(0666)
	pc.statConfig = noopStat

	c := pc.checkSocketPermissions()

	assertCheckID(t, c, "DSO-DOCTOR-004")
	assertWarn(t, c)
}

func TestPermCheck_004_SocketNotWorldReadable_Pass(t *testing.T) {
	pc := newPermissionChecks("/var/run/docker.sock", "/etc/dso/dso.yaml")
	pc.statSocket = fakeStatWithMode(0660)
	pc.statConfig = noopStat

	c := pc.checkSocketPermissions()

	assertCheckID(t, c, "DSO-DOCTOR-004")
	assertPass(t, c)
}

func TestPermCheck_004_SocketNotFound_Info(t *testing.T) {
	pc := newPermissionChecks("/var/run/docker.sock", "/etc/dso/dso.yaml")
	pc.statSocket = func(_ string) (os.FileInfo, error) { return nil, os.ErrNotExist }

	c := pc.checkSocketPermissions()

	assertCheckID(t, c, "DSO-DOCTOR-004")
	assertInfo(t, c)
}

func TestPermCheck_005_ConfigWorldReadable_Fail(t *testing.T) {
	pc := newPermissionChecks("/var/run/docker.sock", "/etc/dso/dso.yaml")
	pc.statSocket = noopStat
	pc.statConfig = fakeStatWithMode(0644) // world-readable

	c := pc.checkConfigPermissions()

	assertCheckID(t, c, "DSO-DOCTOR-005")
	assertFail(t, c)
	if c.Severity != DoctorHigh {
		t.Errorf("want DoctorHigh severity, got %q", c.Severity)
	}
}

func TestPermCheck_005_ConfigNotWorldReadable_Pass(t *testing.T) {
	pc := newPermissionChecks("/var/run/docker.sock", "/etc/dso/dso.yaml")
	pc.statSocket = noopStat
	pc.statConfig = fakeStatWithMode(0600)

	c := pc.checkConfigPermissions()

	assertCheckID(t, c, "DSO-DOCTOR-005")
	assertPass(t, c)
}

func TestPermCheck_006_NonRoot_Info(t *testing.T) {
	pc := newPermissionChecks("/var/run/docker.sock", "/etc/dso/dso.yaml")
	pc.currentUID = func() int { return 1000 }

	c := pc.checkRunningAsRoot()

	assertCheckID(t, c, "DSO-DOCTOR-006")
	assertInfo(t, c)
}

func TestPermCheck_006_Root_Pass(t *testing.T) {
	pc := newPermissionChecks("/var/run/docker.sock", "/etc/dso/dso.yaml")
	pc.currentUID = func() int { return 0 }

	c := pc.checkRunningAsRoot()

	assertCheckID(t, c, "DSO-DOCTOR-006")
	assertPass(t, c)
}

// ─── ConfigurationChecks ──────────────────────────────────────────────────────

func TestConfigCheck_007_ConfigExists_Pass(t *testing.T) {
	cc := newConfigurationChecks("/etc/dso/dso.yaml")
	cc.stat = func(_ string) (os.FileInfo, error) { return os.Stat(".") }
	cc.readFile = func(_ string) ([]byte, error) { return []byte("key: value"), nil }

	c := cc.checkConfigExists()

	assertCheckID(t, c, "DSO-DOCTOR-007")
	assertPass(t, c)
}

func TestConfigCheck_007_ConfigMissing_Fail(t *testing.T) {
	cc := newConfigurationChecks("/etc/dso/dso.yaml")
	cc.stat = func(_ string) (os.FileInfo, error) { return nil, os.ErrNotExist }

	c := cc.checkConfigExists()

	assertCheckID(t, c, "DSO-DOCTOR-007")
	assertFail(t, c)
	if c.Severity != DoctorCritical {
		t.Errorf("want DoctorCritical, got %q", c.Severity)
	}
}

func TestConfigCheck_008_ValidYAML_Pass(t *testing.T) {
	cc := newConfigurationChecks("/etc/dso/dso.yaml")
	cc.stat = func(_ string) (os.FileInfo, error) { return os.Stat(".") }
	cc.readFile = func(_ string) ([]byte, error) { return []byte("provider: aws\nregion: us-east-1\n"), nil }

	c := cc.checkConfigSyntax()

	assertCheckID(t, c, "DSO-DOCTOR-008")
	assertPass(t, c)
}

func TestConfigCheck_008_InvalidYAML_Fail(t *testing.T) {
	cc := newConfigurationChecks("/etc/dso/dso.yaml")
	cc.stat = func(_ string) (os.FileInfo, error) { return os.Stat(".") }
	cc.readFile = func(_ string) ([]byte, error) { return []byte("key: [unclosed bracket"), nil }

	c := cc.checkConfigSyntax()

	assertCheckID(t, c, "DSO-DOCTOR-008")
	assertFail(t, c)
}

func TestConfigCheck_008_EmptyFile_Warn(t *testing.T) {
	cc := newConfigurationChecks("/etc/dso/dso.yaml")
	cc.stat = func(_ string) (os.FileInfo, error) { return os.Stat(".") }
	cc.readFile = func(_ string) ([]byte, error) { return []byte{}, nil }

	c := cc.checkConfigSyntax()

	assertCheckID(t, c, "DSO-DOCTOR-008")
	assertWarn(t, c)
}

func TestConfigCheck_008_UnreadableFile_Info(t *testing.T) {
	cc := newConfigurationChecks("/etc/dso/dso.yaml")
	cc.stat = func(_ string) (os.FileInfo, error) { return os.Stat(".") }
	cc.readFile = func(_ string) ([]byte, error) { return nil, errors.New("permission denied") }

	c := cc.checkConfigSyntax()

	assertCheckID(t, c, "DSO-DOCTOR-008")
	assertInfo(t, c)
}

func TestConfigCheck_009_ModePermissive_Warn(t *testing.T) {
	cc := newConfigurationChecks("/etc/dso/dso.yaml")
	cc.stat = fakeStatWithMode(0644)

	c := cc.checkConfigMode()

	assertCheckID(t, c, "DSO-DOCTOR-009")
	assertWarn(t, c)
}

func TestConfigCheck_009_ModeStrict_Pass(t *testing.T) {
	cc := newConfigurationChecks("/etc/dso/dso.yaml")
	cc.stat = fakeStatWithMode(0600)

	c := cc.checkConfigMode()

	assertCheckID(t, c, "DSO-DOCTOR-009")
	assertPass(t, c)
}

func TestConfigCheck_009_Mode0640_Pass(t *testing.T) {
	cc := newConfigurationChecks("/etc/dso/dso.yaml")
	cc.stat = fakeStatWithMode(0640)

	c := cc.checkConfigMode()

	assertCheckID(t, c, "DSO-DOCTOR-009")
	assertPass(t, c)
}

func TestConfigCheck_009_NotFound_Info(t *testing.T) {
	cc := newConfigurationChecks("/etc/dso/dso.yaml")
	cc.stat = func(_ string) (os.FileInfo, error) { return nil, os.ErrNotExist }

	c := cc.checkConfigMode()

	assertCheckID(t, c, "DSO-DOCTOR-009")
	assertInfo(t, c)
}

// ─── ProviderChecks ───────────────────────────────────────────────────────────

func TestProviderCheck_010_KnownProvider_Pass(t *testing.T) {
	for _, p := range []string{"local", "aws", "vault", "azure"} {
		pc := newProviderChecks(p)
		c := pc.checkProviderKnown()
		assertCheckID(t, c, "DSO-DOCTOR-010")
		assertPass(t, c)
	}
}

func TestProviderCheck_010_UnknownProvider_Fail(t *testing.T) {
	pc := newProviderChecks("gcp")
	c := pc.checkProviderKnown()
	assertCheckID(t, c, "DSO-DOCTOR-010")
	assertFail(t, c)
}

func TestProviderCheck_010_EmptyProvider_Info(t *testing.T) {
	pc := newProviderChecks("")
	c := pc.checkProviderKnown()
	assertCheckID(t, c, "DSO-DOCTOR-010")
	assertInfo(t, c)
}

func TestProviderCheck_011_LocalProvider_Info(t *testing.T) {
	pc := newProviderChecks("local")
	pc.lookupEnv = func(_ string) string { return "" }
	c := pc.checkCredentials()
	assertCheckID(t, c, "DSO-DOCTOR-011")
	assertInfo(t, c)
}

func TestProviderCheck_011_AWSCredsMissing_Fail(t *testing.T) {
	pc := newProviderChecks("aws")
	pc.lookupEnv = func(_ string) string { return "" }
	c := pc.checkCredentials()
	assertCheckID(t, c, "DSO-DOCTOR-011")
	assertFail(t, c)
}

func TestProviderCheck_011_AWSStaticCreds_Pass(t *testing.T) {
	pc := newProviderChecks("aws")
	pc.lookupEnv = func(k string) string {
		env := map[string]string{
			"AWS_ACCESS_KEY_ID":     "AKIA...",
			"AWS_SECRET_ACCESS_KEY": "secret",
		}
		return env[k]
	}
	c := pc.checkCredentials()
	assertCheckID(t, c, "DSO-DOCTOR-011")
	assertPass(t, c)
}

func TestProviderCheck_011_VaultAddrMissing_Fail(t *testing.T) {
	pc := newProviderChecks("vault")
	pc.lookupEnv = func(_ string) string { return "" }
	c := pc.checkCredentials()
	assertCheckID(t, c, "DSO-DOCTOR-011")
	assertFail(t, c)
	if !strings.Contains(c.Detail, "VAULT_ADDR") {
		t.Errorf("expected detail to mention VAULT_ADDR, got: %s", c.Detail)
	}
}

func TestProviderCheck_011_VaultAddrNoToken_Fail(t *testing.T) {
	pc := newProviderChecks("vault")
	pc.lookupEnv = func(k string) string {
		if k == "VAULT_ADDR" {
			return "https://vault.example.com"
		}
		return ""
	}
	c := pc.checkCredentials()
	assertCheckID(t, c, "DSO-DOCTOR-011")
	assertFail(t, c)
}

func TestProviderCheck_011_VaultTokenPresent_Pass(t *testing.T) {
	pc := newProviderChecks("vault")
	pc.lookupEnv = func(k string) string {
		env := map[string]string{
			"VAULT_ADDR":  "https://vault.example.com",
			"VAULT_TOKEN": "s.xxxx",
		}
		return env[k]
	}
	c := pc.checkCredentials()
	assertCheckID(t, c, "DSO-DOCTOR-011")
	assertPass(t, c)
}

func TestProviderCheck_011_AzureCredsPresent_Pass(t *testing.T) {
	pc := newProviderChecks("azure")
	pc.lookupEnv = func(k string) string {
		env := map[string]string{
			"AZURE_CLIENT_ID":     "client-id",
			"AZURE_CLIENT_SECRET": "secret",
			"AZURE_TENANT_ID":     "tenant-id",
		}
		return env[k]
	}
	c := pc.checkCredentials()
	assertCheckID(t, c, "DSO-DOCTOR-011")
	assertPass(t, c)
}

func TestProviderCheck_011_AzureCredsMissing_Fail(t *testing.T) {
	pc := newProviderChecks("azure")
	pc.lookupEnv = func(_ string) string { return "" }
	c := pc.checkCredentials()
	assertCheckID(t, c, "DSO-DOCTOR-011")
	assertFail(t, c)
}

// ─── RuntimeChecks ────────────────────────────────────────────────────────────

func TestRuntimeCheck_012_DirExists_Pass(t *testing.T) {
	rc := newRuntimeChecks("/var/run/dso")
	rc.stat = func(_ string) (os.FileInfo, error) {
		info, _ := os.Stat(".")
		return info, nil
	}
	rc.glob = func(_ string) ([]string, error) { return nil, nil }

	c := rc.checkRuntimeDir()

	assertCheckID(t, c, "DSO-DOCTOR-012")
	assertPass(t, c)
}

func TestRuntimeCheck_012_DirMissing_Info(t *testing.T) {
	rc := newRuntimeChecks("/var/run/dso")
	rc.stat = func(_ string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	rc.glob = func(_ string) ([]string, error) { return nil, nil }

	c := rc.checkRuntimeDir()

	assertCheckID(t, c, "DSO-DOCTOR-012")
	assertInfo(t, c)
}

func TestRuntimeCheck_012_PathIsFile_Fail(t *testing.T) {
	// Simulate path existing as a regular file (not a directory).
	rc := newRuntimeChecks("/var/run/dso")
	rc.stat = func(_ string) (os.FileInfo, error) {
		// Use a real file (this go file) as a stand-in for "exists but is a file"
		return os.Stat("doctor_test.go")
	}
	rc.glob = func(_ string) ([]string, error) { return nil, nil }

	c := rc.checkRuntimeDir()

	assertCheckID(t, c, "DSO-DOCTOR-012")
	assertFail(t, c)
}

func TestRuntimeCheck_013_NoLocks_Pass(t *testing.T) {
	rc := newRuntimeChecks("/var/run/dso")
	rc.stat = func(_ string) (os.FileInfo, error) { return os.Stat(".") }
	rc.glob = func(_ string) ([]string, error) { return nil, nil }

	c := rc.checkNoStaleLocks()

	assertCheckID(t, c, "DSO-DOCTOR-013")
	assertPass(t, c)
}

func TestRuntimeCheck_013_StaleLocks_Warn(t *testing.T) {
	rc := newRuntimeChecks("/var/run/dso")
	rc.stat = func(_ string) (os.FileInfo, error) { return os.Stat(".") }
	rc.glob = func(_ string) ([]string, error) {
		return []string{"/var/run/dso/dso.lock", "/var/run/dso/agent.lock"}, nil
	}

	c := rc.checkNoStaleLocks()

	assertCheckID(t, c, "DSO-DOCTOR-013")
	assertWarn(t, c)
	if !strings.Contains(c.Detail, "2 lock file") {
		t.Errorf("expected detail to mention 2 lock files, got: %q", c.Detail)
	}
}

// ─── ServiceChecks ────────────────────────────────────────────────────────────

func TestServiceCheck_014_AgentBinaryFound_Pass(t *testing.T) {
	sc := newServiceChecks()
	sc.lookupBinary = func(_ string) (string, error) { return "/usr/local/bin/dso-agent", nil }
	sc.statUnitFile = noopStat

	c := sc.checkAgentBinary()

	assertCheckID(t, c, "DSO-DOCTOR-014")
	assertPass(t, c)
}

func TestServiceCheck_014_AgentBinaryMissing_Fail(t *testing.T) {
	sc := newServiceChecks()
	sc.lookupBinary = func(_ string) (string, error) { return "", errors.New("not found") }

	c := sc.checkAgentBinary()

	assertCheckID(t, c, "DSO-DOCTOR-014")
	assertFail(t, c)
	if c.Severity != DoctorCritical {
		t.Errorf("want DoctorCritical, got %q", c.Severity)
	}
}

func TestServiceCheck_015_UnitFileExists_Pass(t *testing.T) {
	sc := newServiceChecks()
	sc.statUnitFile = func(_ string) (os.FileInfo, error) { return os.Stat(".") }

	c := sc.checkUnitFile()

	assertCheckID(t, c, "DSO-DOCTOR-015")
	assertPass(t, c)
}

func TestServiceCheck_015_UnitFileMissing_Fail(t *testing.T) {
	sc := newServiceChecks()
	sc.statUnitFile = func(_ string) (os.FileInfo, error) { return nil, os.ErrNotExist }

	c := sc.checkUnitFile()

	assertCheckID(t, c, "DSO-DOCTOR-015")
	assertFail(t, c)
}

func TestServiceCheck_016_ServiceEnabled_Pass(t *testing.T) {
	sc := newServiceChecks()
	sc.isEnabled = func(_ string) (bool, error) { return true, nil }

	c := sc.checkServiceEnabled()

	assertCheckID(t, c, "DSO-DOCTOR-016")
	assertPass(t, c)
}

func TestServiceCheck_016_ServiceNotEnabled_Warn(t *testing.T) {
	sc := newServiceChecks()
	sc.isEnabled = func(_ string) (bool, error) { return false, nil }

	c := sc.checkServiceEnabled()

	assertCheckID(t, c, "DSO-DOCTOR-016")
	assertWarn(t, c)
}

func TestServiceCheck_016_SystemctlUnavailable_Info(t *testing.T) {
	sc := newServiceChecks()
	sc.isEnabled = func(_ string) (bool, error) { return false, errors.New("systemctl not found") }

	c := sc.checkServiceEnabled()

	assertCheckID(t, c, "DSO-DOCTOR-016")
	assertInfo(t, c)
}

func TestServiceCheck_017_ServiceActive_Pass(t *testing.T) {
	sc := newServiceChecks()
	sc.isActive = func(_ string) (bool, error) { return true, nil }

	c := sc.checkServiceActive()

	assertCheckID(t, c, "DSO-DOCTOR-017")
	assertPass(t, c)
}

func TestServiceCheck_017_ServiceInactive_Fail(t *testing.T) {
	sc := newServiceChecks()
	sc.isActive = func(_ string) (bool, error) { return false, nil }

	c := sc.checkServiceActive()

	assertCheckID(t, c, "DSO-DOCTOR-017")
	assertFail(t, c)
}

func TestServiceCheck_017_SystemctlUnavailable_Info(t *testing.T) {
	sc := newServiceChecks()
	sc.isActive = func(_ string) (bool, error) { return false, errors.New("systemctl not found") }

	c := sc.checkServiceActive()

	assertCheckID(t, c, "DSO-DOCTOR-017")
	assertInfo(t, c)
}

// ─── Doctor engine (doctor.go) ────────────────────────────────────────────────

func TestDoctor_Run_ReturnsNonNilResult(t *testing.T) {
	d := testDoctor()
	result := d.Run(context.Background())
	if result == nil {
		t.Fatal("expected non-nil DoctorResult")
	}
}

func TestDoctor_Run_TimestampSet(t *testing.T) {
	d := testDoctor()
	result := d.Run(context.Background())
	if result.Timestamp.IsZero() {
		t.Error("expected Timestamp to be set")
	}
}

func TestDoctor_Run_SummaryTotalMatchesChecks(t *testing.T) {
	d := testDoctor()
	result := d.Run(context.Background())
	if result.Summary.Total != len(result.Checks) {
		t.Errorf("summary.Total=%d does not match len(checks)=%d", result.Summary.Total, len(result.Checks))
	}
}

func TestDoctor_Run_SummaryCountsCorrect(t *testing.T) {
	d := testDoctor()
	result := d.Run(context.Background())

	counted := DoctorSummary{}
	for _, c := range result.Checks {
		switch c.Status {
		case DoctorPass:
			counted.Passed++
		case DoctorWarn:
			counted.Warnings++
		case DoctorFail:
			counted.Failures++
		case DoctorInfo:
			counted.Infos++
		}
	}
	if counted.Passed != result.Summary.Passed {
		t.Errorf("passed: want %d, got %d", counted.Passed, result.Summary.Passed)
	}
	if counted.Warnings != result.Summary.Warnings {
		t.Errorf("warnings: want %d, got %d", counted.Warnings, result.Summary.Warnings)
	}
	if counted.Failures != result.Summary.Failures {
		t.Errorf("failures: want %d, got %d", counted.Failures, result.Summary.Failures)
	}
}

func TestDoctor_OverallStatus_AllPass(t *testing.T) {
	checks := []DoctorCheck{
		{Status: DoctorPass},
		{Status: DoctorPass},
		{Status: DoctorInfo},
	}
	if s := doctorComputeOverall(checks); s != DoctorPass {
		t.Errorf("want DoctorPass, got %q", s)
	}
}

func TestDoctor_OverallStatus_AnyWarn(t *testing.T) {
	checks := []DoctorCheck{
		{Status: DoctorPass},
		{Status: DoctorWarn},
	}
	if s := doctorComputeOverall(checks); s != DoctorWarn {
		t.Errorf("want DoctorWarn, got %q", s)
	}
}

func TestDoctor_OverallStatus_FailWinsOverWarn(t *testing.T) {
	checks := []DoctorCheck{
		{Status: DoctorWarn},
		{Status: DoctorFail},
		{Status: DoctorPass},
	}
	if s := doctorComputeOverall(checks); s != DoctorFail {
		t.Errorf("want DoctorFail, got %q", s)
	}
}

func TestDoctor_OverallStatus_Empty_Pass(t *testing.T) {
	if s := doctorComputeOverall(nil); s != DoctorPass {
		t.Errorf("want DoctorPass for empty checks, got %q", s)
	}
}

// ─── Rendering ────────────────────────────────────────────────────────────────

func TestDoctorResult_RenderTerminal_ContainsDivider(t *testing.T) {
	result := allPassDoctorResult()
	out := result.RenderTerminal(false)
	if !strings.Contains(out, "─") {
		t.Error("expected terminal output to contain divider")
	}
}

func TestDoctorResult_RenderTerminal_ContainsCheckIDs(t *testing.T) {
	result := allPassDoctorResult()
	out := result.RenderTerminal(false)
	if !strings.Contains(out, "DSO-DOCTOR-001") {
		t.Errorf("expected terminal output to contain check ID, got:\n%s", out)
	}
}

func TestDoctorResult_RenderTerminal_ContainsOverallStatus(t *testing.T) {
	result := allPassDoctorResult()
	out := result.RenderTerminal(false)
	if !strings.Contains(out, "PASS") {
		t.Errorf("expected terminal output to contain PASS, got:\n%s", out)
	}
}

func TestDoctorResult_RenderTerminal_VerboseShowsRootCause(t *testing.T) {
	result := &DoctorResult{
		OverallStatus: DoctorFail,
		Timestamp:     testTime(),
		Checks: []DoctorCheck{
			{
				ID: "DSO-DOCTOR-001", Status: DoctorFail, Name: "Docker",
				RootCause: "Docker is not installed",
				Recovery:  []string{"Install Docker"},
			},
		},
	}
	result.Summary = doctorComputeSummary(result.Checks)
	out := result.RenderTerminal(true)
	if !strings.Contains(out, "Docker is not installed") {
		t.Errorf("verbose output missing RootCause, got:\n%s", out)
	}
}

func TestDoctorResult_RenderTerminal_NonVerboseOmitsRootCause(t *testing.T) {
	result := &DoctorResult{
		OverallStatus: DoctorFail,
		Timestamp:     testTime(),
		Checks: []DoctorCheck{
			{
				ID: "DSO-DOCTOR-001", Status: DoctorFail, Name: "Docker",
				RootCause: "Docker is not installed",
			},
		},
	}
	result.Summary = doctorComputeSummary(result.Checks)
	out := result.RenderTerminal(false)
	if strings.Contains(out, "Docker is not installed") {
		t.Error("non-verbose output must not include RootCause")
	}
}

func TestDoctorResult_RenderJSON_ValidJSON(t *testing.T) {
	result := allPassDoctorResult()
	out, err := result.RenderJSON()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("expected JSON output to start with '{', got: %s", out[:truncLen(out, 20)])
	}
}

func TestDoctorResult_RenderJSON_ContainsOverallStatus(t *testing.T) {
	result := allPassDoctorResult()
	out, _ := result.RenderJSON()
	if !strings.Contains(out, `"overall_status"`) {
		t.Errorf("expected JSON to contain overall_status field, got:\n%s", out)
	}
}

func TestDoctorResult_RenderJSON_RecoveryIsArrayNotNull(t *testing.T) {
	result := allPassDoctorResult()
	out, _ := result.RenderJSON()
	if strings.Contains(out, `"recovery":null`) {
		t.Error("recovery field must be [] not null in JSON output")
	}
}

func TestDoctorResult_RenderJSON_TimestampISO8601(t *testing.T) {
	result := allPassDoctorResult()
	out, _ := result.RenderJSON()
	if !strings.Contains(out, `"timestamp"`) || !strings.Contains(out, "2026-01-01T00:00:00Z") {
		t.Errorf("expected ISO-8601 timestamp in JSON output, got:\n%s", out)
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// testDoctor returns a Doctor with all OS hooks replaced by no-ops so tests
// run without Docker, systemd, or any real config files.
func testDoctor() *Doctor {
	opts := DoctorOptions{
		Mode:         ModeLocal,
		Provider:     "local",
		DockerSocket: "/var/run/docker.sock",
		ConfigPath:   "/etc/dso/dso.yaml",
		RuntimeDir:   "/var/run/dso",
	}
	d := NewDoctor(opts)

	// Docker — all pass
	d.docker.lookupBinary = func(_ string) (string, error) { return "/usr/bin/docker", nil }
	d.docker.runVersion = func(_ context.Context) error { return nil }
	d.docker.statSocket = func(_ string) (os.FileInfo, error) { return os.Stat(".") }

	// Permissions
	d.perms.statSocket = fakeStatWithMode(0660)
	d.perms.statConfig = func(_ string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	d.perms.currentUID = func() int { return 0 }

	// Configuration — file not present (INFO)
	d.config.stat = func(_ string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	d.config.readFile = func(_ string) ([]byte, error) { return nil, os.ErrNotExist }

	// Provider — local, no credentials required
	d.provider.lookupEnv = func(_ string) string { return "" }

	// Runtime — dir not present (INFO)
	d.runtime.stat = func(_ string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	d.runtime.glob = func(_ string) ([]string, error) { return nil, nil }

	// Service — systemctl unavailable (INFO)
	d.service.lookupBinary = func(_ string) (string, error) { return "", errors.New("not found") }
	d.service.statUnitFile = func(_ string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	d.service.isEnabled = func(_ string) (bool, error) { return false, errors.New("no systemctl") }
	d.service.isActive = func(_ string) (bool, error) { return false, errors.New("no systemctl") }

	return d
}

// allPassDoctorResult returns a DoctorResult with one passing check for render tests.
func allPassDoctorResult() *DoctorResult {
	checks := []DoctorCheck{
		{
			ID: "DSO-DOCTOR-001", Category: DoctorCatDocker, Severity: DoctorLow,
			Status: DoctorPass, Name: "Docker binary", Description: "Docker CLI binary",
			Detail: "docker found", Recovery: []string{},
		},
	}
	return &DoctorResult{
		OverallStatus: DoctorPass,
		Checks:        checks,
		Summary:       doctorComputeSummary(checks),
		Timestamp:     testTime(),
	}
}

// fakeStatWithMode returns a stat function that reports a specific file mode.
// It returns a real FileInfo from "." but with the mode override applied via
// the DoctorCheck tests — for mode-specific assertions we use os.FileMode
// directly on the DoctorCheck.Detail string in assertions.
//
// For the mode-based assertions in the permission tests, we need the actual
// mode to be returned by the stat function. Since os.FileInfo.Mode() is an
// interface method, we use a real file's stat but the tests assert on
// DoctorCheck.Status rather than the raw mode.
func fakeStatWithMode(mode os.FileMode) func(string) (os.FileInfo, error) {
	return func(_ string) (os.FileInfo, error) {
		return &fakeFileInfo{mode: mode}, nil
	}
}

// fakeFileInfo implements os.FileInfo with an overridable mode.
type fakeFileInfo struct {
	mode os.FileMode
}

func (f *fakeFileInfo) Name() string      { return "fake" }
func (f *fakeFileInfo) Size() int64       { return 0 }
func (f *fakeFileInfo) Mode() os.FileMode { return f.mode }
func (f *fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f *fakeFileInfo) IsDir() bool       { return f.mode.IsDir() }
func (f *fakeFileInfo) Sys() interface{}  { return nil }

// Test assertion helpers.

func assertCheckID(t *testing.T, c DoctorCheck, want string) {
	t.Helper()
	if c.ID != want {
		t.Errorf("check ID: want %q, got %q", want, c.ID)
	}
}

func assertPass(t *testing.T, c DoctorCheck) {
	t.Helper()
	if c.Status != DoctorPass {
		t.Errorf("want DoctorPass, got %q (detail: %s)", c.Status, c.Detail)
	}
}

func assertFail(t *testing.T, c DoctorCheck) {
	t.Helper()
	if c.Status != DoctorFail {
		t.Errorf("want DoctorFail, got %q (detail: %s)", c.Status, c.Detail)
	}
}

func assertWarn(t *testing.T, c DoctorCheck) {
	t.Helper()
	if c.Status != DoctorWarn {
		t.Errorf("want DoctorWarn, got %q (detail: %s)", c.Status, c.Detail)
	}
}

func assertInfo(t *testing.T, c DoctorCheck) {
	t.Helper()
	if c.Status != DoctorInfo {
		t.Errorf("want DoctorInfo, got %q (detail: %s)", c.Status, c.Detail)
	}
}

func assertHasRecovery(t *testing.T, c DoctorCheck) {
	t.Helper()
	if len(c.Recovery) == 0 {
		t.Errorf("expected non-empty Recovery steps for check %s (status: %s)", c.ID, c.Status)
	}
}

// truncLen returns n or len(s), whichever is smaller.
func truncLen(s string, n int) int {
	if len(s) < n {
		return len(s)
	}
	return n
}

// testTime returns a fixed deterministic time for use in render tests.
func testTime() time.Time {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
}
