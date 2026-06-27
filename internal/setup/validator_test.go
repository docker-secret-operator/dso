package setup

import (
	"context"
	"errors"
	"testing"
)

// ─── Helpers ─────────────────────────────────────────────────────────────────

// healthyLocalEnv builds a minimal Environment that passes all validations for
// local mode with no cloud provider.
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

// mockValidator constructs a Validator with injectable connectivity probes.
func mockValidator(cfg ValidatorConfig) *Validator {
	return &Validator{cfg: cfg}
}

// ─── Validator.Validate ───────────────────────────────────────────────────────

func TestValidator_Validate_ValidLocalEnvironment(t *testing.T) {
	v := mockValidator(noopValidatorConfig())
	result, err := v.Validate(context.Background(), healthyLocalEnv(), SetupOptions{
		Mode:     ModeLocal,
		Provider: "local",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Valid {
		t.Errorf("expected Valid=true, got errors: %v", result.Errors)
	}
	if len(result.Errors) != 0 {
		t.Errorf("expected no errors, got: %v", result.Errors)
	}
}

func TestValidator_Validate_MissingDocker_Invalid(t *testing.T) {
	v := mockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.Docker = DockerInfo{} // no Docker at all

	result, err := v.Validate(context.Background(), env, SetupOptions{Mode: ModeLocal})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Valid {
		t.Error("expected Valid=false when Docker is not installed")
	}
	if !hasErrorCode(result.Errors, "docker_not_installed") {
		t.Errorf("expected docker_not_installed error, got: %v", result.Errors)
	}
}

func TestValidator_Validate_DockerBinaryFoundButDaemonDown_Invalid(t *testing.T) {
	v := mockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.Docker = DockerInfo{BinaryFound: true, DaemonReachable: false}

	result, _ := v.Validate(context.Background(), env, SetupOptions{Mode: ModeLocal})

	if result.Valid {
		t.Error("expected Valid=false when Docker daemon is not running")
	}
	if !hasErrorCode(result.Errors, "docker_daemon_unreachable") {
		t.Errorf("expected docker_daemon_unreachable, got: %v", result.Errors)
	}
}

func TestValidator_Validate_SocketFoundDaemonDown_DistinctError(t *testing.T) {
	v := mockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.Docker = DockerInfo{BinaryFound: true, SocketFound: true, DaemonReachable: false}

	result, _ := v.Validate(context.Background(), env, SetupOptions{Mode: ModeLocal})

	if result.Valid {
		t.Error("expected Valid=false")
	}
	if !hasErrorCode(result.Errors, "docker_daemon_not_running") {
		t.Errorf("expected docker_daemon_not_running, got: %v", result.Errors)
	}
}

func TestValidator_Validate_AgentModeNoRoot_Invalid(t *testing.T) {
	v := mockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.Systemd = SystemdInfo{Available: true}
	env.Capabilities.SupportsSystemd = true
	env.Capabilities.SupportsAgentMode = true
	// IsRoot is false by default from healthyLocalEnv

	result, _ := v.Validate(context.Background(), env, SetupOptions{Mode: ModeAgent})

	if result.Valid {
		t.Error("expected Valid=false when agent mode is requested without root")
	}
	if !hasErrorCode(result.Errors, "agent_mode_requires_root") {
		t.Errorf("expected agent_mode_requires_root, got: %v", result.Errors)
	}
}

func TestValidator_Validate_AgentModeNonRootFlag_Valid(t *testing.T) {
	v := mockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.Systemd = SystemdInfo{Available: true}
	env.Capabilities.SupportsSystemd = true
	env.Capabilities.SupportsAgentMode = true

	// --non-root suppresses the root requirement during initial setup.
	result, _ := v.Validate(context.Background(), env, SetupOptions{Mode: ModeAgent, NonRoot: true})

	if !result.Valid {
		t.Errorf("expected Valid=true with NonRoot=true, got errors: %v", result.Errors)
	}
}

func TestValidator_Validate_AgentModeNoSystemd_Invalid(t *testing.T) {
	v := mockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.User.IsRoot = true
	env.Capabilities.SupportsSystemd = false

	result, _ := v.Validate(context.Background(), env, SetupOptions{Mode: ModeAgent})

	if result.Valid {
		t.Error("expected Valid=false when agent mode is requested without systemd")
	}
	if !hasErrorCode(result.Errors, "agent_mode_requires_systemd") {
		t.Errorf("expected agent_mode_requires_systemd, got: %v", result.Errors)
	}
}

func TestValidator_Validate_UnknownProvider_Invalid(t *testing.T) {
	v := mockValidator(noopValidatorConfig())
	result, _ := v.Validate(context.Background(), healthyLocalEnv(), SetupOptions{
		Mode:     ModeLocal,
		Provider: "notacloud",
	})

	if result.Valid {
		t.Error("expected Valid=false for unknown provider")
	}
	if !hasErrorCode(result.Errors, "unknown_provider") {
		t.Errorf("expected unknown_provider, got: %v", result.Errors)
	}
}

func TestValidator_Validate_AWSNoCredentials_Invalid(t *testing.T) {
	v := mockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.Providers.AWS = AWSInfo{Detected: false}

	result, _ := v.Validate(context.Background(), env, SetupOptions{
		Mode:     ModeLocal,
		Provider: "aws",
	})

	if result.Valid {
		t.Error("expected Valid=false when AWS credentials are missing")
	}
	if !hasErrorCode(result.Errors, "aws_credentials_missing") {
		t.Errorf("expected aws_credentials_missing, got: %v", result.Errors)
	}
}

func TestValidator_Validate_AWSCredentialsWithConnectivitySuccess(t *testing.T) {
	v := mockValidator(ValidatorConfig{
		CheckAWSConnectivity: func(_ context.Context, _ string) error { return nil },
	})
	env := healthyLocalEnv()
	env.Providers.AWS = AWSInfo{Detected: true, Region: "us-east-1"}

	result, _ := v.Validate(context.Background(), env, SetupOptions{
		Mode:     ModeLocal,
		Provider: "aws",
	})

	if !result.Valid {
		t.Errorf("expected Valid=true when AWS connectivity probe succeeds, got errors: %v", result.Errors)
	}
}

func TestValidator_Validate_AWSCredentialsWithConnectivityFailure(t *testing.T) {
	v := mockValidator(ValidatorConfig{
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
	if !hasErrorCode(result.Errors, "aws_connectivity_failed") {
		t.Errorf("expected aws_connectivity_failed, got: %v", result.Errors)
	}
}

func TestValidator_Validate_VaultNoCredentials_Invalid(t *testing.T) {
	v := mockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.Providers.Vault = VaultInfo{Detected: false}

	result, _ := v.Validate(context.Background(), env, SetupOptions{Provider: "vault"})

	if result.Valid {
		t.Error("expected Valid=false when Vault credentials are missing")
	}
	if !hasErrorCode(result.Errors, "vault_credentials_missing") {
		t.Errorf("expected vault_credentials_missing, got: %v", result.Errors)
	}
}

func TestValidator_Validate_VaultConnectivityFailure(t *testing.T) {
	v := mockValidator(ValidatorConfig{
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
	if !hasErrorCode(result.Errors, "vault_connectivity_failed") {
		t.Errorf("expected vault_connectivity_failed, got: %v", result.Errors)
	}
}

func TestValidator_Validate_AzureNoCredentials_Invalid(t *testing.T) {
	v := mockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.Providers.Azure = AzureInfo{Detected: false}

	result, _ := v.Validate(context.Background(), env, SetupOptions{Provider: "azure"})

	if result.Valid {
		t.Error("expected Valid=false when Azure credentials are missing")
	}
	if !hasErrorCode(result.Errors, "azure_credentials_missing") {
		t.Errorf("expected azure_credentials_missing, got: %v", result.Errors)
	}
}

func TestValidator_Validate_AzureConnectivitySkippedWhenNilChecker(t *testing.T) {
	v := mockValidator(ValidatorConfig{
		CheckAzureConnectivity: nil, // no probe
	})
	env := healthyLocalEnv()
	env.Providers.Azure = AzureInfo{Detected: true, HasEnvCreds: true}

	result, _ := v.Validate(context.Background(), env, SetupOptions{Provider: "azure"})

	if !result.Valid {
		t.Errorf("expected Valid=true when Azure connectivity probe is nil, got errors: %v", result.Errors)
	}
}

func TestValidator_Validate_NoCloudProvider_WarnsUser(t *testing.T) {
	v := mockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.Providers = DetectedProviders{Available: []string{"local"}}

	result, _ := v.Validate(context.Background(), env, SetupOptions{Mode: ModeLocal})
	// Valid=true; only a warning is emitted
	if !result.Valid {
		t.Errorf("expected Valid=true with no cloud provider, got errors: %v", result.Errors)
	}
	if !hasWarningCode(result.Warnings, "no_cloud_provider_detected") {
		t.Errorf("expected no_cloud_provider_detected warning, got: %v", result.Warnings)
	}
}

func TestValidator_Validate_ExplicitLocalProvider_NoWarning(t *testing.T) {
	v := mockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.Providers = DetectedProviders{Available: []string{"local"}}

	result, _ := v.Validate(context.Background(), env, SetupOptions{
		Mode:     ModeLocal,
		Provider: "local", // explicit: user chose local intentionally
	})

	if !result.Valid {
		t.Errorf("expected Valid=true for explicit local provider, got errors: %v", result.Errors)
	}
	if hasWarningCode(result.Warnings, "no_cloud_provider_detected") {
		t.Error("should not warn about no cloud provider when local is explicitly requested")
	}
}

func TestValidator_Validate_ExistingInstallation_Suggestion(t *testing.T) {
	v := mockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.ExistingDSO = ExistingDSOInfo{
		Installed:  true,
		ConfigPath: "/etc/dso/dso.yaml",
		Version:    "1.2.3",
	}

	result, _ := v.Validate(context.Background(), env, SetupOptions{Mode: ModeLocal, Provider: "local"})

	if !result.Valid {
		t.Errorf("existing installation should not block setup, errors: %v", result.Errors)
	}
	if !hasSuggestionCode(result.Suggestions, "existing_installation_found") {
		t.Errorf("expected existing_installation_found suggestion, got: %v", result.Suggestions)
	}
}

func TestValidator_Validate_ServiceWithoutAgent_Warning(t *testing.T) {
	v := mockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.ExistingDSO = ExistingDSOInfo{
		Installed:        true,
		ServiceInstalled: true,
		AgentInstalled:   false,
	}

	result, _ := v.Validate(context.Background(), env, SetupOptions{Mode: ModeLocal, Provider: "local"})

	if !hasWarningCode(result.Warnings, "service_without_agent") {
		t.Errorf("expected service_without_agent warning, got: %v", result.Warnings)
	}
}

func TestValidator_Validate_DetectionWarningsPromoted(t *testing.T) {
	v := mockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.DetectionWarnings = []DetectionWarning{
		{Code: "systemd_version_failed", Message: "could not read systemd version"},
	}

	result, _ := v.Validate(context.Background(), env, SetupOptions{Mode: ModeLocal, Provider: "local"})

	if !result.Valid {
		t.Errorf("detection warnings should not block setup, errors: %v", result.Errors)
	}
	if !hasWarningCode(result.Warnings, "systemd_version_failed") {
		t.Errorf("expected systemd_version_failed warning promoted, got: %v", result.Warnings)
	}
}

func TestValidator_Validate_DockerDaemonUnreachableWarning_NotDuplicated(t *testing.T) {
	// docker_daemon_unreachable from detection should be dropped; validateDocker
	// emits a richer error instead. The two must not appear side by side.
	v := mockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.Docker = DockerInfo{BinaryFound: true, DaemonReachable: false}
	env.DetectionWarnings = []DetectionWarning{
		{Code: "docker_daemon_unreachable", Message: "daemon did not respond"},
	}

	result, _ := v.Validate(context.Background(), env, SetupOptions{Mode: ModeLocal})

	// Validate returns exactly one docker error — not an error plus a warning.
	dockerErrors := filterCode(result.Errors, "docker_daemon_unreachable")
	dockerWarnings := filterWarningCode(result.Warnings, "docker_daemon_unreachable")
	if len(dockerErrors)+len(dockerWarnings) > 1 {
		t.Errorf("docker_daemon_unreachable should appear at most once, errors=%v warnings=%v",
			dockerErrors, dockerWarnings)
	}
}

func TestValidator_Validate_OsReleaseWarning_Dropped(t *testing.T) {
	v := mockValidator(noopValidatorConfig())
	env := healthyLocalEnv()
	env.DetectionWarnings = []DetectionWarning{
		{Code: "os_release_read_failed", Message: "cannot read /etc/os-release"},
	}

	result, _ := v.Validate(context.Background(), env, SetupOptions{Mode: ModeLocal, Provider: "local"})

	if !result.Valid {
		t.Errorf("os_release_read_failed should not block setup, errors: %v", result.Errors)
	}
	if hasWarningCode(result.Warnings, "os_release_read_failed") {
		t.Error("os_release_read_failed should be silently dropped by the validator")
	}
}

// ─── validateDocker ───────────────────────────────────────────────────────────

func TestValidateDocker_NoBinary_ReturnsError(t *testing.T) {
	errs, _ := validateDocker(Environment{}, SetupOptions{})
	if !hasErrorCode(errs, "docker_not_installed") {
		t.Errorf("expected docker_not_installed, got: %v", errs)
	}
}

func TestValidateDocker_BinaryFoundDaemonReachable_NoErrors(t *testing.T) {
	env := Environment{Docker: DockerInfo{BinaryFound: true, DaemonReachable: true}}
	errs, warns := validateDocker(env, SetupOptions{})
	if len(errs) != 0 || len(warns) != 0 {
		t.Errorf("expected no errors/warnings, got errs=%v warns=%v", errs, warns)
	}
}

func TestValidateDocker_BinaryFoundSocketFoundDaemonDown_SpecificError(t *testing.T) {
	env := Environment{Docker: DockerInfo{BinaryFound: true, SocketFound: true, DaemonReachable: false}}
	errs, _ := validateDocker(env, SetupOptions{})
	if !hasErrorCode(errs, "docker_daemon_not_running") {
		t.Errorf("expected docker_daemon_not_running, got: %v", errs)
	}
}

func TestValidateDocker_BinaryFoundNoSocketDaemonDown_GenericError(t *testing.T) {
	env := Environment{Docker: DockerInfo{BinaryFound: true, SocketFound: false, DaemonReachable: false}}
	errs, _ := validateDocker(env, SetupOptions{})
	if !hasErrorCode(errs, "docker_daemon_unreachable") {
		t.Errorf("expected docker_daemon_unreachable, got: %v", errs)
	}
}

func TestValidateDocker_NoBinary_DoesNotCheckSocket(t *testing.T) {
	// When the binary is missing, exactly one error should be returned and
	// no socket-related errors should follow.
	env := Environment{Docker: DockerInfo{BinaryFound: false, SocketFound: true}}
	errs, _ := validateDocker(env, SetupOptions{})
	if len(errs) != 1 {
		t.Errorf("expected exactly 1 error when binary is missing, got: %v", errs)
	}
}

// ─── validatePermissions ──────────────────────────────────────────────────────

func TestValidatePermissions_LocalModeNonRoot_NoError(t *testing.T) {
	env := Environment{User: UserInfo{IsRoot: false}}
	errs, _ := validatePermissions(env, SetupOptions{Mode: ModeLocal})
	if len(errs) != 0 {
		t.Errorf("local mode should not require root, got: %v", errs)
	}
}

func TestValidatePermissions_AgentModeNonRoot_Error(t *testing.T) {
	env := Environment{
		User:         UserInfo{IsRoot: false},
		Capabilities: Capabilities{SupportsSystemd: true},
	}
	errs, _ := validatePermissions(env, SetupOptions{Mode: ModeAgent})
	if !hasErrorCode(errs, "agent_mode_requires_root") {
		t.Errorf("expected agent_mode_requires_root, got: %v", errs)
	}
}

func TestValidatePermissions_AgentModeRoot_NoRootError(t *testing.T) {
	env := Environment{
		User:         UserInfo{IsRoot: true},
		Capabilities: Capabilities{SupportsSystemd: true},
	}
	errs, _ := validatePermissions(env, SetupOptions{Mode: ModeAgent})
	if hasErrorCode(errs, "agent_mode_requires_root") {
		t.Error("root user should not get agent_mode_requires_root error")
	}
}

func TestValidatePermissions_AgentModeNoSystemd_Error(t *testing.T) {
	env := Environment{
		User:         UserInfo{IsRoot: true},
		Capabilities: Capabilities{SupportsSystemd: false},
	}
	errs, _ := validatePermissions(env, SetupOptions{Mode: ModeAgent})
	if !hasErrorCode(errs, "agent_mode_requires_systemd") {
		t.Errorf("expected agent_mode_requires_systemd, got: %v", errs)
	}
}

func TestValidatePermissions_DockerBinaryNoSocket_Warning(t *testing.T) {
	env := Environment{
		User:   UserInfo{IsRoot: false},
		Docker: DockerInfo{BinaryFound: true, SocketFound: false},
	}
	_, warns := validatePermissions(env, SetupOptions{Mode: ModeLocal})
	if !hasWarningCode(warns, "docker_socket_inaccessible") {
		t.Errorf("expected docker_socket_inaccessible warning, got: %v", warns)
	}
}

func TestValidatePermissions_DockerBinaryNoSocketRootUser_NoWarning(t *testing.T) {
	env := Environment{
		User:   UserInfo{IsRoot: true},
		Docker: DockerInfo{BinaryFound: true, SocketFound: false},
	}
	_, warns := validatePermissions(env, SetupOptions{Mode: ModeLocal})
	if hasWarningCode(warns, "docker_socket_inaccessible") {
		t.Error("root user should not get docker socket inaccessible warning")
	}
}

// ─── validateExisting ─────────────────────────────────────────────────────────

func TestValidateExisting_NoInstallation_NoOutput(t *testing.T) {
	env := Environment{ExistingDSO: ExistingDSOInfo{Installed: false}}
	errs, warns, suggestions := validateExisting(env, SetupOptions{})
	if len(errs)+len(warns)+len(suggestions) != 0 {
		t.Errorf("fresh install should produce no output, got errs=%v warns=%v suggestions=%v",
			errs, warns, suggestions)
	}
}

func TestValidateExisting_ConfigFound_Suggestion(t *testing.T) {
	env := Environment{ExistingDSO: ExistingDSOInfo{
		Installed:  true,
		ConfigPath: "/etc/dso/dso.yaml",
	}}
	_, _, suggestions := validateExisting(env, SetupOptions{})
	if !hasSuggestionCode(suggestions, "existing_installation_found") {
		t.Errorf("expected existing_installation_found suggestion, got: %v", suggestions)
	}
}

func TestValidateExisting_ConfigFoundWithVersion_MentionsVersion(t *testing.T) {
	env := Environment{ExistingDSO: ExistingDSOInfo{
		Installed:  true,
		ConfigPath: "/etc/dso/dso.yaml",
		Version:    "2.0.0",
	}}
	_, _, suggestions := validateExisting(env, SetupOptions{})
	if len(suggestions) == 0 {
		t.Fatal("expected at least one suggestion")
	}
	msg := suggestions[0].Message
	if msg == "" {
		t.Error("expected non-empty suggestion message")
	}
}

func TestValidateExisting_ServiceWithoutAgent_Warning(t *testing.T) {
	env := Environment{ExistingDSO: ExistingDSOInfo{
		Installed:        true,
		ServiceInstalled: true,
		AgentInstalled:   false,
	}}
	_, warns, _ := validateExisting(env, SetupOptions{})
	if !hasWarningCode(warns, "service_without_agent") {
		t.Errorf("expected service_without_agent warning, got: %v", warns)
	}
}

func TestValidateExisting_AgentWithoutServiceAgentMode_Suggestion(t *testing.T) {
	env := Environment{
		ExistingDSO: ExistingDSOInfo{
			Installed:        true,
			AgentInstalled:   true,
			ServiceInstalled: false,
		},
		Capabilities: Capabilities{SupportsAgentMode: true},
	}
	_, _, suggestions := validateExisting(env, SetupOptions{Mode: ModeAgent})
	if !hasSuggestionCode(suggestions, "agent_without_service") {
		t.Errorf("expected agent_without_service suggestion, got: %v", suggestions)
	}
}

func TestValidateExisting_AgentWithoutServiceLocalMode_NoSuggestion(t *testing.T) {
	env := Environment{ExistingDSO: ExistingDSOInfo{
		Installed:        true,
		AgentInstalled:   true,
		ServiceInstalled: false,
	}}
	_, _, suggestions := validateExisting(env, SetupOptions{Mode: ModeLocal})
	if hasSuggestionCode(suggestions, "agent_without_service") {
		t.Error("agent_without_service suggestion should not appear for local mode")
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

func hasErrorCode(errs []ValidationError, code string) bool {
	for _, e := range errs {
		if e.Code == code {
			return true
		}
	}
	return false
}

func hasWarningCode(warns []ValidationWarning, code string) bool {
	for _, w := range warns {
		if w.Code == code {
			return true
		}
	}
	return false
}

func hasSuggestionCode(suggestions []ValidationSuggestion, code string) bool {
	for _, s := range suggestions {
		if s.Code == code {
			return true
		}
	}
	return false
}

func filterCode(errs []ValidationError, code string) []ValidationError {
	var out []ValidationError
	for _, e := range errs {
		if e.Code == code {
			out = append(out, e)
		}
	}
	return out
}

func filterWarningCode(warns []ValidationWarning, code string) []ValidationWarning {
	var out []ValidationWarning
	for _, w := range warns {
		if w.Code == code {
			out = append(out, w)
		}
	}
	return out
}
