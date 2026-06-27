package setup

import (
	"context"
	"errors"
	"testing"
)

// ─── Fixtures ─────────────────────────────────────────────────────────────────

// healthyLocalEnv returns a minimal Environment that passes all validations
// for local mode with no cloud provider.
func healthyLocalEnv() *Environment {
	return &Environment{
		Docker: DockerInfo{
			BinaryFound:     true,
			SocketFound:     true,
			DaemonReachable: true,
		},
		User: UserInfo{Username: "alice", IsRoot: false},
		Capabilities: Capabilities{
			SupportsDocker:    true,
			SupportsLocalMode: true,
		},
	}
}

// noopValidatorConfig returns a ValidatorConfig that skips all network probes.
func noopValidatorConfig() ValidatorConfig {
	return ValidatorConfig{} // nil functions == no probes
}

// newMockValidator constructs a Validator with injectable connectivity probes.
func newMockValidator(cfg ValidatorConfig) *Validator {
	return &Validator{cfg: cfg}
}

// ─── Validator.Validate ───────────────────────────────────────────────────────

func TestValidator_Validate_ValidLocalEnvironment(t *testing.T) {
	v := newMockValidator(noopValidatorConfig())
	result, err := v.Validate(context.Background(), healthyLocalEnv(), SetupOptions{
		Mode:     ModeLocal,
		Provider: "local",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Valid {
		t.Errorf("expected Valid=true, got issues: %v", result.Errors())
	}
}

func TestValidator_Validate_MissingDocker_Invalid(t *testing.T) {
	v := newMockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.Docker = DockerInfo{}

	result, err := v.Validate(context.Background(), env, SetupOptions{Mode: ModeLocal})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Valid {
		t.Error("expected Valid=false when Docker is not installed")
	}
	if !hasCode(result, SeverityError, CodeDockerNotInstalled) {
		t.Errorf("expected %s, got issues: %v", CodeDockerNotInstalled, result.Issues)
	}
}

func TestValidator_Validate_DockerBinaryFoundDaemonDown_Invalid(t *testing.T) {
	v := newMockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.Docker = DockerInfo{BinaryFound: true, DaemonReachable: false}

	result, _ := v.Validate(context.Background(), env, SetupOptions{Mode: ModeLocal})

	if result.Valid {
		t.Error("expected Valid=false when Docker daemon is not running")
	}
	if !hasCode(result, SeverityError, CodeDockerDaemonUnreachable) {
		t.Errorf("expected %s, got issues: %v", CodeDockerDaemonUnreachable, result.Issues)
	}
}

func TestValidator_Validate_SocketFoundDaemonDown_DistinctError(t *testing.T) {
	v := newMockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.Docker = DockerInfo{BinaryFound: true, SocketFound: true, DaemonReachable: false}

	result, _ := v.Validate(context.Background(), env, SetupOptions{Mode: ModeLocal})

	if result.Valid {
		t.Error("expected Valid=false")
	}
	if !hasCode(result, SeverityError, CodeDockerDaemonNotRunning) {
		t.Errorf("expected %s, got issues: %v", CodeDockerDaemonNotRunning, result.Issues)
	}
}

func TestValidator_Validate_AgentModeNoRoot_Invalid(t *testing.T) {
	v := newMockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.Systemd = SystemdInfo{Available: true}
	env.Capabilities.SupportsSystemd = true
	env.Capabilities.SupportsAgentMode = true

	result, _ := v.Validate(context.Background(), env, SetupOptions{Mode: ModeAgent})

	if result.Valid {
		t.Error("expected Valid=false when agent mode requested without root")
	}
	if !hasCode(result, SeverityError, CodeAgentModeRequiresRoot) {
		t.Errorf("expected %s, got issues: %v", CodeAgentModeRequiresRoot, result.Issues)
	}
}

func TestValidator_Validate_AgentModeNonRootFlag_Valid(t *testing.T) {
	v := newMockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.Systemd = SystemdInfo{Available: true}
	env.Capabilities.SupportsSystemd = true
	env.Capabilities.SupportsAgentMode = true

	result, _ := v.Validate(context.Background(), env, SetupOptions{Mode: ModeAgent, NonRoot: true})

	if !result.Valid {
		t.Errorf("expected Valid=true with NonRoot=true, got issues: %v", result.Errors())
	}
}

func TestValidator_Validate_AgentModeNoSystemd_Invalid(t *testing.T) {
	v := newMockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.User.IsRoot = true
	env.Capabilities.SupportsSystemd = false

	result, _ := v.Validate(context.Background(), env, SetupOptions{Mode: ModeAgent})

	if result.Valid {
		t.Error("expected Valid=false when agent mode requested without systemd")
	}
	if !hasCode(result, SeverityError, CodeAgentModeRequiresSystemd) {
		t.Errorf("expected %s, got issues: %v", CodeAgentModeRequiresSystemd, result.Issues)
	}
}

func TestValidator_Validate_UnknownProvider_Invalid(t *testing.T) {
	v := newMockValidator(noopValidatorConfig())
	result, _ := v.Validate(context.Background(), healthyLocalEnv(), SetupOptions{
		Mode:     ModeLocal,
		Provider: "notacloud",
	})

	if result.Valid {
		t.Error("expected Valid=false for unknown provider")
	}
	if !hasCode(result, SeverityError, CodeUnknownProvider) {
		t.Errorf("expected %s, got issues: %v", CodeUnknownProvider, result.Issues)
	}
}

func TestValidator_Validate_AWSNoCredentials_Invalid(t *testing.T) {
	v := newMockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.Providers.AWS = AWSInfo{Detected: false}

	result, _ := v.Validate(context.Background(), env, SetupOptions{
		Mode:     ModeLocal,
		Provider: "aws",
	})

	if result.Valid {
		t.Error("expected Valid=false when AWS credentials are missing")
	}
	if !hasCode(result, SeverityError, CodeAWSCredentialsMissing) {
		t.Errorf("expected %s, got issues: %v", CodeAWSCredentialsMissing, result.Issues)
	}
}

func TestValidator_Validate_AWSConnectivitySuccess(t *testing.T) {
	v := newMockValidator(ValidatorConfig{
		CheckAWSConnectivity: func(_ context.Context, _ string) error { return nil },
	})
	env := healthyLocalEnv()
	env.Providers.AWS = AWSInfo{Detected: true, Region: "us-east-1"}

	result, _ := v.Validate(context.Background(), env, SetupOptions{
		Mode:     ModeLocal,
		Provider: "aws",
	})

	if !result.Valid {
		t.Errorf("expected Valid=true when AWS connectivity probe succeeds, got issues: %v", result.Errors())
	}
}

func TestValidator_Validate_AWSConnectivityFailure(t *testing.T) {
	v := newMockValidator(ValidatorConfig{
		CheckAWSConnectivity: func(_ context.Context, _ string) error {
			return errors.New("connection refused")
		},
	})
	env := healthyLocalEnv()
	env.Providers.AWS = AWSInfo{Detected: true}

	result, _ := v.Validate(context.Background(), env, SetupOptions{
		Mode:     ModeLocal,
		Provider: "aws",
	})

	if result.Valid {
		t.Error("expected Valid=false when AWS connectivity probe fails")
	}
	if !hasCode(result, SeverityError, CodeAWSConnectivityFailed) {
		t.Errorf("expected %s, got issues: %v", CodeAWSConnectivityFailed, result.Issues)
	}
}

func TestValidator_Validate_VaultNoCredentials_Invalid(t *testing.T) {
	v := newMockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.Providers.Vault = VaultInfo{Detected: false}

	result, _ := v.Validate(context.Background(), env, SetupOptions{Provider: "vault"})

	if result.Valid {
		t.Error("expected Valid=false when Vault credentials are missing")
	}
	if !hasCode(result, SeverityError, CodeVaultCredentialsMissing) {
		t.Errorf("expected %s, got issues: %v", CodeVaultCredentialsMissing, result.Issues)
	}
}

func TestValidator_Validate_VaultConnectivityFailure(t *testing.T) {
	v := newMockValidator(ValidatorConfig{
		CheckVaultConnectivity: func(_ context.Context, _ string) error {
			return errors.New("dial tcp: connection refused")
		},
	})
	env := healthyLocalEnv()
	env.Providers.Vault = VaultInfo{Detected: true, Address: "http://vault:8200"}

	result, _ := v.Validate(context.Background(), env, SetupOptions{Provider: "vault"})

	if result.Valid {
		t.Error("expected Valid=false when Vault is unreachable")
	}
	if !hasCode(result, SeverityError, CodeVaultConnectivityFailed) {
		t.Errorf("expected %s, got issues: %v", CodeVaultConnectivityFailed, result.Issues)
	}
}

func TestValidator_Validate_AzureNoCredentials_Invalid(t *testing.T) {
	v := newMockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.Providers.Azure = AzureInfo{Detected: false}

	result, _ := v.Validate(context.Background(), env, SetupOptions{Provider: "azure"})

	if result.Valid {
		t.Error("expected Valid=false when Azure credentials are missing")
	}
	if !hasCode(result, SeverityError, CodeAzureCredentialsMissing) {
		t.Errorf("expected %s, got issues: %v", CodeAzureCredentialsMissing, result.Issues)
	}
}

func TestValidator_Validate_AzureConnectivitySkippedWhenNilChecker(t *testing.T) {
	v := newMockValidator(ValidatorConfig{CheckAzureConnectivity: nil})
	env := healthyLocalEnv()
	env.Providers.Azure = AzureInfo{Detected: true, HasEnvCreds: true}

	result, _ := v.Validate(context.Background(), env, SetupOptions{Provider: "azure"})

	if !result.Valid {
		t.Errorf("expected Valid=true when Azure connectivity probe is nil, got issues: %v", result.Errors())
	}
}

func TestValidator_Validate_NoCloudProvider_WarnsUser(t *testing.T) {
	v := newMockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.Providers = DetectedProviders{Available: []string{"local"}}

	result, _ := v.Validate(context.Background(), env, SetupOptions{Mode: ModeLocal})

	if !result.Valid {
		t.Errorf("expected Valid=true with no cloud provider, got issues: %v", result.Errors())
	}
	if !hasCode(result, SeverityWarning, CodeNoCloudProviderDetected) {
		t.Errorf("expected %s warning, got: %v", CodeNoCloudProviderDetected, result.Warnings())
	}
}

func TestValidator_Validate_ExplicitLocalProvider_NoCloudWarning(t *testing.T) {
	v := newMockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.Providers = DetectedProviders{Available: []string{"local"}}

	result, _ := v.Validate(context.Background(), env, SetupOptions{
		Mode:     ModeLocal,
		Provider: "local",
	})

	if !result.Valid {
		t.Errorf("expected Valid=true for explicit local provider, got issues: %v", result.Errors())
	}
	if hasCode(result, SeverityWarning, CodeNoCloudProviderDetected) {
		t.Error("should not warn about no cloud provider when local is explicitly requested")
	}
}

func TestValidator_Validate_ExistingInstallation_InfoIssue(t *testing.T) {
	v := newMockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.ExistingDSO = ExistingDSOInfo{
		Installed:  true,
		ConfigPath: "/etc/dso/dso.yaml",
		Version:    "1.2.3",
	}

	result, _ := v.Validate(context.Background(), env, SetupOptions{Mode: ModeLocal, Provider: "local"})

	if !result.Valid {
		t.Errorf("existing installation should not block setup, errors: %v", result.Errors())
	}
	if !hasCode(result, SeverityInfo, CodeExistingInstallationFound) {
		t.Errorf("expected %s info issue, got: %v", CodeExistingInstallationFound, result.Info())
	}
}

func TestValidator_Validate_ServiceWithoutAgent_Warning(t *testing.T) {
	v := newMockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.ExistingDSO = ExistingDSOInfo{
		Installed:        true,
		ServiceInstalled: true,
		AgentInstalled:   false,
	}

	result, _ := v.Validate(context.Background(), env, SetupOptions{Mode: ModeLocal, Provider: "local"})

	if !hasCode(result, SeverityWarning, CodeServiceWithoutAgent) {
		t.Errorf("expected %s warning, got: %v", CodeServiceWithoutAgent, result.Warnings())
	}
}

func TestValidator_Validate_DetectionWarningsPromoted(t *testing.T) {
	v := newMockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.DetectionWarnings = []DetectionWarning{
		{Code: "systemd_version_failed", Message: "could not read systemd version"},
	}

	result, _ := v.Validate(context.Background(), env, SetupOptions{Mode: ModeLocal, Provider: "local"})

	if !result.Valid {
		t.Errorf("detection warnings should not block setup, errors: %v", result.Errors())
	}
	if !hasCode(result, SeverityWarning, "systemd_version_failed") {
		t.Errorf("expected systemd_version_failed warning promoted, got: %v", result.Warnings())
	}
}

func TestValidator_Validate_DockerDaemonUnreachableWarning_NotDuplicated(t *testing.T) {
	// docker_daemon_unreachable from detection must be dropped; validateDocker
	// emits a richer error. The two must not appear simultaneously.
	v := newMockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.Docker = DockerInfo{BinaryFound: true, DaemonReachable: false}
	env.DetectionWarnings = []DetectionWarning{
		{Code: CodeDockerDaemonUnreachable, Message: "daemon did not respond"},
	}

	result, _ := v.Validate(context.Background(), env, SetupOptions{Mode: ModeLocal})

	count := countCode(result, CodeDockerDaemonUnreachable)
	if count > 1 {
		t.Errorf("docker_daemon_unreachable should appear at most once, found %d: %v", count, result.Issues)
	}
}

func TestValidator_Validate_OsReleaseWarning_Dropped(t *testing.T) {
	v := newMockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.DetectionWarnings = []DetectionWarning{
		{Code: "os_release_read_failed", Message: "cannot read /etc/os-release"},
	}

	result, _ := v.Validate(context.Background(), env, SetupOptions{Mode: ModeLocal, Provider: "local"})

	if !result.Valid {
		t.Errorf("os_release_read_failed should not block setup, errors: %v", result.Errors())
	}
	if hasCode(result, SeverityWarning, "os_release_read_failed") {
		t.Error("os_release_read_failed should be silently dropped by the validator")
	}
}

func TestValidator_Validate_InCategory_Docker(t *testing.T) {
	v := newMockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.Docker = DockerInfo{} // triggers docker_not_installed

	result, _ := v.Validate(context.Background(), env, SetupOptions{Mode: ModeLocal})

	dockerIssues := result.InCategory(CategoryDocker)
	if len(dockerIssues) == 0 {
		t.Error("expected Docker category issues when Docker is not installed")
	}
	for _, i := range dockerIssues {
		if i.Category != CategoryDocker {
			t.Errorf("expected CategoryDocker, got %q", i.Category)
		}
	}
}

// ─── validateDocker ───────────────────────────────────────────────────────────

func TestValidateDocker_NoBinary_ReturnsError(t *testing.T) {
	issues := validateDocker(Environment{}, SetupOptions{})
	if !issueHasCode(issues, SeverityError, CodeDockerNotInstalled) {
		t.Errorf("expected %s, got: %v", CodeDockerNotInstalled, issues)
	}
}

func TestValidateDocker_BinaryFoundDaemonReachable_NoIssues(t *testing.T) {
	env := Environment{Docker: DockerInfo{BinaryFound: true, DaemonReachable: true}}
	issues := validateDocker(env, SetupOptions{})
	if len(issues) != 0 {
		t.Errorf("expected no issues, got: %v", issues)
	}
}

func TestValidateDocker_SocketFoundDaemonDown_SpecificError(t *testing.T) {
	env := Environment{Docker: DockerInfo{BinaryFound: true, SocketFound: true, DaemonReachable: false}}
	issues := validateDocker(env, SetupOptions{})
	if !issueHasCode(issues, SeverityError, CodeDockerDaemonNotRunning) {
		t.Errorf("expected %s, got: %v", CodeDockerDaemonNotRunning, issues)
	}
}

func TestValidateDocker_NoSocketDaemonDown_GenericError(t *testing.T) {
	env := Environment{Docker: DockerInfo{BinaryFound: true, SocketFound: false, DaemonReachable: false}}
	issues := validateDocker(env, SetupOptions{})
	if !issueHasCode(issues, SeverityError, CodeDockerDaemonUnreachable) {
		t.Errorf("expected %s, got: %v", CodeDockerDaemonUnreachable, issues)
	}
}

func TestValidateDocker_NoBinary_ExactlyOneError(t *testing.T) {
	env := Environment{Docker: DockerInfo{BinaryFound: false, SocketFound: true}}
	issues := validateDocker(env, SetupOptions{})
	if len(issues) != 1 {
		t.Errorf("expected exactly 1 error when binary is missing, got: %v", issues)
	}
}

// ─── validatePermissions ──────────────────────────────────────────────────────

func TestValidatePermissions_LocalModeNonRoot_NoError(t *testing.T) {
	env := Environment{User: UserInfo{IsRoot: false}}
	issues := validatePermissions(env, SetupOptions{Mode: ModeLocal})
	if len(issues) != 0 {
		t.Errorf("local mode should not require root, got: %v", issues)
	}
}

func TestValidatePermissions_AgentModeNonRoot_Error(t *testing.T) {
	env := Environment{
		User:         UserInfo{IsRoot: false},
		Capabilities: Capabilities{SupportsSystemd: true},
	}
	issues := validatePermissions(env, SetupOptions{Mode: ModeAgent})
	if !issueHasCode(issues, SeverityError, CodeAgentModeRequiresRoot) {
		t.Errorf("expected %s, got: %v", CodeAgentModeRequiresRoot, issues)
	}
}

func TestValidatePermissions_AgentModeRoot_NoRootError(t *testing.T) {
	env := Environment{
		User:         UserInfo{IsRoot: true},
		Capabilities: Capabilities{SupportsSystemd: true},
	}
	issues := validatePermissions(env, SetupOptions{Mode: ModeAgent})
	if issueHasCode(issues, SeverityError, CodeAgentModeRequiresRoot) {
		t.Error("root user should not get agent_mode_requires_root error")
	}
}

func TestValidatePermissions_AgentModeNoSystemd_Error(t *testing.T) {
	env := Environment{
		User:         UserInfo{IsRoot: true},
		Capabilities: Capabilities{SupportsSystemd: false},
	}
	issues := validatePermissions(env, SetupOptions{Mode: ModeAgent})
	if !issueHasCode(issues, SeverityError, CodeAgentModeRequiresSystemd) {
		t.Errorf("expected %s, got: %v", CodeAgentModeRequiresSystemd, issues)
	}
}

func TestValidatePermissions_DockerBinaryNoSocket_Warning(t *testing.T) {
	env := Environment{
		User:   UserInfo{IsRoot: false},
		Docker: DockerInfo{BinaryFound: true, SocketFound: false},
	}
	issues := validatePermissions(env, SetupOptions{Mode: ModeLocal})
	if !issueHasCode(issues, SeverityWarning, CodeDockerSocketInaccessible) {
		t.Errorf("expected %s warning, got: %v", CodeDockerSocketInaccessible, issues)
	}
}

func TestValidatePermissions_DockerBinaryNoSocketRootUser_NoWarning(t *testing.T) {
	env := Environment{
		User:   UserInfo{IsRoot: true},
		Docker: DockerInfo{BinaryFound: true, SocketFound: false},
	}
	issues := validatePermissions(env, SetupOptions{Mode: ModeLocal})
	if issueHasCode(issues, SeverityWarning, CodeDockerSocketInaccessible) {
		t.Error("root user should not get docker_socket_inaccessible warning")
	}
}

// ─── validateExisting ─────────────────────────────────────────────────────────

func TestValidateExisting_NoInstallation_NoOutput(t *testing.T) {
	env := Environment{ExistingDSO: ExistingDSOInfo{Installed: false}}
	issues := validateExisting(env, SetupOptions{})
	if len(issues) != 0 {
		t.Errorf("fresh install should produce no issues, got: %v", issues)
	}
}

func TestValidateExisting_ConfigFound_InfoIssue(t *testing.T) {
	env := Environment{ExistingDSO: ExistingDSOInfo{
		Installed:  true,
		ConfigPath: "/etc/dso/dso.yaml",
	}}
	issues := validateExisting(env, SetupOptions{})
	if !issueHasCode(issues, SeverityInfo, CodeExistingInstallationFound) {
		t.Errorf("expected %s info issue, got: %v", CodeExistingInstallationFound, issues)
	}
}

func TestValidateExisting_ServiceWithoutAgent_Warning(t *testing.T) {
	env := Environment{ExistingDSO: ExistingDSOInfo{
		Installed:        true,
		ServiceInstalled: true,
		AgentInstalled:   false,
	}}
	issues := validateExisting(env, SetupOptions{})
	if !issueHasCode(issues, SeverityWarning, CodeServiceWithoutAgent) {
		t.Errorf("expected %s warning, got: %v", CodeServiceWithoutAgent, issues)
	}
}

func TestValidateExisting_AgentWithoutServiceAgentMode_InfoIssue(t *testing.T) {
	env := Environment{
		ExistingDSO: ExistingDSOInfo{
			Installed:        true,
			AgentInstalled:   true,
			ServiceInstalled: false,
		},
		Capabilities: Capabilities{SupportsAgentMode: true},
	}
	issues := validateExisting(env, SetupOptions{Mode: ModeAgent})
	if !issueHasCode(issues, SeverityInfo, CodeAgentWithoutService) {
		t.Errorf("expected %s info issue, got: %v", CodeAgentWithoutService, issues)
	}
}

func TestValidateExisting_AgentWithoutServiceLocalMode_NoIssue(t *testing.T) {
	env := Environment{ExistingDSO: ExistingDSOInfo{
		Installed:        true,
		AgentInstalled:   true,
		ServiceInstalled: false,
	}}
	issues := validateExisting(env, SetupOptions{Mode: ModeLocal})
	if issueHasCode(issues, SeverityInfo, CodeAgentWithoutService) {
		t.Error("agent_without_service should not appear for local mode")
	}
}

// ─── effectiveMode ────────────────────────────────────────────────────────────

func TestEffectiveMode_ExplicitModeWins(t *testing.T) {
	env := Environment{Capabilities: Capabilities{SupportsAgentMode: true}}
	mode := effectiveMode(env, SetupOptions{Mode: ModeLocal})
	if mode != ModeLocal {
		t.Errorf("explicit mode should win over capabilities, got %q", mode)
	}
}

func TestEffectiveMode_FallsBackToAgentWhenSupported(t *testing.T) {
	env := Environment{Capabilities: Capabilities{SupportsAgentMode: true}}
	mode := effectiveMode(env, SetupOptions{})
	if mode != ModeAgent {
		t.Errorf("expected ModeAgent from capabilities, got %q", mode)
	}
}

func TestEffectiveMode_FallsBackToLocalWhenNoAgent(t *testing.T) {
	env := Environment{Capabilities: Capabilities{SupportsAgentMode: false}}
	mode := effectiveMode(env, SetupOptions{})
	if mode != ModeLocal {
		t.Errorf("expected ModeLocal when agent is not supported, got %q", mode)
	}
}

// ─── Test helpers ─────────────────────────────────────────────────────────────

// hasCode reports whether result contains an issue with the given severity and code.
func hasCode(result *ValidationResult, severity ValidationSeverity, code string) bool {
	return issueHasCode(result.Issues, severity, code)
}

// issueHasCode reports whether the slice contains an issue with the given severity and code.
func issueHasCode(issues []ValidationIssue, severity ValidationSeverity, code string) bool {
	for _, i := range issues {
		if i.Severity == severity && i.Code == code {
			return true
		}
	}
	return false
}

// countCode returns the number of issues with the given code (any severity).
func countCode(result *ValidationResult, code string) int {
	n := 0
	for _, i := range result.Issues {
		if i.Code == code {
			n++
		}
	}
	return n
}
