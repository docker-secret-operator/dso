package bootstrap

import "fmt"

// BootstrapError is the base error type for bootstrap operations
type BootstrapError struct {
	Code    string
	Message string
	Phase   string
	Cause   error
}

func (e *BootstrapError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %s (%v)", e.Phase, e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s: %s", e.Phase, e.Code, e.Message)
}

func (e *BootstrapError) Unwrap() error {
	return e.Cause
}

// Error code constants
const (
	ErrCodeDockerUnavailable     = "DOCKER_UNAVAILABLE"
	ErrCodeDockerUnresponsive    = "DOCKER_UNRESPONSIVE"
	ErrCodeDockerVersionIncompat = "DOCKER_VERSION_INCOMPATIBLE"
	ErrCodeDockerRuntimeUnavail  = "DOCKER_RUNTIME_UNAVAILABLE"
	ErrCodePermissionDenied      = "PERMISSION_DENIED"
	ErrCodeInvalidProvider       = "INVALID_PROVIDER"
	ErrCodeSymlinkDetected       = "SYMLINK_DETECTED"
	ErrCodePathTraversal         = "PATH_TRAVERSAL"
	ErrCodePathValidation        = "PATH_VALIDATION"
	ErrCodeFileWrite             = "FILE_WRITE_ERROR"
	ErrCodeConfigValidation      = "CONFIG_VALIDATION"
	ErrCodeSystemdUnavailable    = "SYSTEMD_UNAVAILABLE"
	ErrCodeSystemdVersion        = "SYSTEMD_VERSION_INCOMPATIBLE"
	ErrCodeLockAcquisition       = "LOCK_ACQUISITION_FAILED"
	ErrCodeMetadataFetch         = "METADATA_FETCH_FAILED"
	ErrCodeCloudDetection        = "CLOUD_DETECTION_FAILED"
	ErrCodeProviderConfig        = "PROVIDER_CONFIG_INVALID"
	ErrCodeGroupManagement       = "GROUP_MANAGEMENT_FAILED"
	ErrCodeUserValidation        = "USER_VALIDATION_FAILED"
	ErrCodeRollback              = "ROLLBACK_FAILED"
	ErrCodeInteractivePrompt     = "INTERACTIVE_PROMPT_FAILED"
	ErrCodeYAMLGeneration        = "YAML_GENERATION_FAILED"
)

// NewBootstrapError creates a new bootstrap error
func NewBootstrapError(code, phase, message string, cause error) *BootstrapError {
	return &BootstrapError{
		Code:    code,
		Message: message,
		Phase:   phase,
		Cause:   cause,
	}
}

// Factory functions for common errors
func ErrDockerUnavailable(phase string, cause error) *BootstrapError {
	return NewBootstrapError(
		ErrCodeDockerUnavailable,
		phase,
		"Docker daemon is not available. Ensure Docker is installed and running.",
		cause,
	)
}

func ErrDockerUnresponsive(phase string, cause error) *BootstrapError {
	return NewBootstrapError(
		ErrCodeDockerUnresponsive,
		phase,
		"Docker daemon is not responding. Check daemon status and socket permissions.",
		cause,
	)
}

func ErrDockerVersionIncompat(phase string, version string, cause error) *BootstrapError {
	return NewBootstrapError(
		ErrCodeDockerVersionIncompat,
		phase,
		fmt.Sprintf("Docker version %s is not compatible. Minimum: 20.10.0", version),
		cause,
	)
}

func ErrPermissionDenied(phase, resource string) *BootstrapError {
	return NewBootstrapError(
		ErrCodePermissionDenied,
		phase,
		fmt.Sprintf("Permission denied accessing %s. Run with appropriate privileges.", resource),
		nil,
	)
}

func ErrInvalidProvider(phase, provider string) *BootstrapError {
	return NewBootstrapError(
		ErrCodeInvalidProvider,
		phase,
		fmt.Sprintf("Invalid provider: %s. Supported: aws, azure, huawei, vault", provider),
		nil,
	)
}

func ErrSymlinkDetected(phase, path string) *BootstrapError {
	return NewBootstrapError(
		ErrCodeSymlinkDetected,
		phase,
		fmt.Sprintf("Symlink detected at %s. This is a security risk.", path),
		nil,
	)
}

func ErrPathTraversal(phase, path string) *BootstrapError {
	return NewBootstrapError(
		ErrCodePathTraversal,
		phase,
		fmt.Sprintf("Path traversal attempt detected: %s", path),
		nil,
	)
}

func ErrPathValidation(phase, path, reason string) *BootstrapError {
	return NewBootstrapError(
		ErrCodePathValidation,
		phase,
		fmt.Sprintf("Path validation failed for %s: %s", path, reason),
		nil,
	)
}

func ErrFileWrite(phase, path string, cause error) *BootstrapError {
	return NewBootstrapError(
		ErrCodeFileWrite,
		phase,
		fmt.Sprintf("Failed to write file %s", path),
		cause,
	)
}

func ErrConfigValidation(phase, reason string) *BootstrapError {
	return NewBootstrapError(
		ErrCodeConfigValidation,
		phase,
		fmt.Sprintf("Configuration validation failed: %s", reason),
		nil,
	)
}

func ErrSystemdUnavailable(phase string, cause error) *BootstrapError {
	return NewBootstrapError(
		ErrCodeSystemdUnavailable,
		phase,
		"systemd is not available. This is required for agent mode.",
		cause,
	)
}

func ErrLockAcquisition(phase string, cause error) *BootstrapError {
	return NewBootstrapError(
		ErrCodeLockAcquisition,
		phase,
		"Could not acquire bootstrap lock. Another bootstrap may be in progress.",
		cause,
	)
}

func ErrMetadataFetch(phase, provider string, cause error) *BootstrapError {
	return NewBootstrapError(
		ErrCodeMetadataFetch,
		phase,
		fmt.Sprintf("Failed to fetch metadata from %s", provider),
		cause,
	)
}

func ErrProviderConfig(phase, provider, reason string) *BootstrapError {
	return NewBootstrapError(
		ErrCodeProviderConfig,
		phase,
		fmt.Sprintf("Invalid %s provider configuration: %s", provider, reason),
		nil,
	)
}

func ErrGroupManagement(phase, operation string, cause error) *BootstrapError {
	return NewBootstrapError(
		ErrCodeGroupManagement,
		phase,
		fmt.Sprintf("Failed to manage group for %s", operation),
		cause,
	)
}

func ErrUserValidation(phase, username string) *BootstrapError {
	return NewBootstrapError(
		ErrCodeUserValidation,
		phase,
		fmt.Sprintf("Failed to validate user %s or retrieve user information", username),
		nil,
	)
}

func ErrRollback(phase, operation string, cause error) *BootstrapError {
	return NewBootstrapError(
		ErrCodeRollback,
		phase,
		fmt.Sprintf("Rollback of %s failed. Manual cleanup may be required.", operation),
		cause,
	)
}

func ErrInteractivePrompt(phase string, cause error) *BootstrapError {
	return NewBootstrapError(
		ErrCodeInteractivePrompt,
		phase,
		"Interactive prompt failed. Run with --non-interactive for automated setup.",
		cause,
	)
}

func ErrYAMLGeneration(phase string, cause error) *BootstrapError {
	return NewBootstrapError(
		ErrCodeYAMLGeneration,
		phase,
		"Failed to generate valid YAML configuration",
		cause,
	)
}
