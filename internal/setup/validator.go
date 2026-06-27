package setup

import "context"

// ValidatorConfig holds injectable dependencies for the Validator.
// In production these default to real HTTP probes; tests supply mocks or nil
// (which skips the connectivity check entirely).
type ValidatorConfig struct {
	// Provider connectivity probes. If nil, the check is skipped.
	// These are the ONLY network calls the Validator is permitted to make.
	CheckAWSConnectivity   func(ctx context.Context, region string) error
	CheckVaultConnectivity func(ctx context.Context, addr string) error
	CheckAzureConnectivity func(ctx context.Context) error
}

// Validator answers one question: "Can this environment successfully run the
// requested setup?" It consumes Environment and SetupOptions; it never
// rediscovers the environment — no LookPath, os.Stat, user.Current, or
// exec.Command calls live here. Those belong exclusively in the detector.
type Validator struct {
	cfg ValidatorConfig
}

// newValidator constructs a Validator wired to the real HTTP connectivity probes.
func newValidator() *Validator {
	return &Validator{
		cfg: ValidatorConfig{
			CheckAWSConnectivity:   checkAWSConnectivity,
			CheckVaultConnectivity: checkVaultConnectivity,
			CheckAzureConnectivity: checkAzureConnectivity,
		},
	}
}

// Validate checks the environment and returns a structured result.
// A non-nil error signals an unexpected internal failure; normal validation
// failures are expressed as Errors inside the returned ValidationResult.
func (v *Validator) Validate(ctx context.Context, env *Environment, opts SetupOptions) (*ValidationResult, error) {
	result := &ValidationResult{}

	// 1. Promote relevant detection warnings into validation-level warnings.
	v.promoteDetectionWarnings(env, result)

	// 2. Docker availability.
	dockerErrs, dockerWarns := validateDocker(*env, opts)
	result.Errors = append(result.Errors, dockerErrs...)
	result.Warnings = append(result.Warnings, dockerWarns...)

	// 3. User permissions for the requested mode.
	permErrs, permWarns := validatePermissions(*env, opts)
	result.Errors = append(result.Errors, permErrs...)
	result.Warnings = append(result.Warnings, permWarns...)

	// 4. Provider credentials and connectivity.
	provErrs, provWarns, err := v.validateProvider(ctx, *env, opts)
	if err != nil {
		return nil, err
	}
	result.Errors = append(result.Errors, provErrs...)
	result.Warnings = append(result.Warnings, provWarns...)

	// 5. Existing installation — determines install vs. upgrade vs. repair.
	existErrs, existWarns, existSuggestions := validateExisting(*env, opts)
	result.Errors = append(result.Errors, existErrs...)
	result.Warnings = append(result.Warnings, existWarns...)
	result.Suggestions = append(result.Suggestions, existSuggestions...)

	result.Valid = len(result.Errors) == 0
	return result, nil
}

// promoteDetectionWarnings converts detection warnings into validation-level
// warnings. Warnings that are already handled by a dedicated validator
// (e.g. docker_daemon_unreachable is covered by validateDocker) are silently
// dropped to avoid duplicates.
func (v *Validator) promoteDetectionWarnings(env *Environment, result *ValidationResult) {
	for _, dw := range env.DetectionWarnings {
		switch dw.Code {
		case "docker_daemon_unreachable":
			// Covered by validateDocker with richer context — skip.
		case "os_release_read_failed":
			// Informational only; has no bearing on setup correctness.
		default:
			result.Warnings = append(result.Warnings, ValidationWarning{
				Code:    dw.Code,
				Message: "detection warning: " + dw.Message,
			})
		}
	}
}
