package setup

import "context"

// ValidatorConfig holds injectable dependencies for the Validator.
// Production wires real HTTP probes; tests supply mocks or nil (skip the check).
type ValidatorConfig struct {
	// Provider connectivity probes. If nil, the check is skipped.
	// These are the ONLY network calls the Validator is permitted to make.
	CheckAWSConnectivity   func(ctx context.Context, region string) error
	CheckVaultConnectivity func(ctx context.Context, addr string) error
	CheckAzureConnectivity func(ctx context.Context) error
}

// Validator answers one question: "Can this environment successfully run the
// requested setup?" It consumes Environment and SetupOptions only; it never
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
// A non-nil error signals an unexpected internal failure; all validation
// findings are expressed as Issues inside the returned ValidationResult.
func (v *Validator) Validate(ctx context.Context, env *Environment, opts SetupOptions) (*ValidationResult, error) {
	result := &ValidationResult{}

	// Detection warnings first — they may be promoted or silently dropped.
	v.promoteDetectionWarnings(env, result)

	result.Issues = append(result.Issues, validateDocker(*env, opts)...)
	result.Issues = append(result.Issues, validatePermissions(*env, opts)...)

	provIssues, err := v.validateProvider(ctx, *env, opts)
	if err != nil {
		return nil, err
	}
	result.Issues = append(result.Issues, provIssues...)
	result.Issues = append(result.Issues, validateExisting(*env, opts)...)

	result.Valid = len(result.Errors()) == 0
	return result, nil
}

// promoteDetectionWarnings converts detection warnings into validation-level
// issues. Codes already covered by a dedicated validator are silently dropped
// to avoid duplicating the same finding at different levels of detail.
func (v *Validator) promoteDetectionWarnings(env *Environment, result *ValidationResult) {
	for _, dw := range env.DetectionWarnings {
		switch dw.Code {
		case CodeDockerDaemonUnreachable:
			// validateDocker emits a richer, categorised error — skip.
		case "os_release_read_failed":
			// Informational; has no bearing on whether setup can proceed.
		default:
			result.Issues = append(result.Issues, ValidationIssue{
				Severity: SeverityWarning,
				Category: CategoryConfiguration,
				Code:     dw.Code,
				Message:  "detection warning: " + dw.Message,
			})
		}
	}
}
