package cli

import (
	"context"
	"fmt"

	"github.com/docker-secret-operator/dso/internal/setup"
	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/spf13/cobra"
)

// validateSections groups validate's checks for terminal display, reusing
// the same doctorSection/renderSectionedResult machinery doctor.go
// established -- Configuration and Provider checks are the *same*
// setup.NewDoctor()-backed checks doctor already runs (reused wholesale,
// not reimplemented); Compose/References/Secrets are validate's own,
// deeper, apply-time checks.
var validateSections = []doctorSection{
	{title: "Compose", categories: map[setup.DoctorCategory]bool{validateCatCompose: true}},
	{title: "References", categories: map[setup.DoctorCategory]bool{validateCatReferences: true}},
	{title: "Secrets", categories: map[setup.DoctorCategory]bool{validateCatSecrets: true}},
	{title: "Configuration", categories: map[setup.DoctorCategory]bool{setup.DoctorCatConfiguration: true}},
	{title: "Provider", categories: map[setup.DoctorCategory]bool{setup.DoctorCatProvider: true}},
}

// NewValidateCmd creates `docker dso validate`: the authoritative,
// read-only validation engine for a DSO-managed Compose project. It is
// the single validation path shared by the local CLI and (per Phase 1's
// plan) the eventual CI/GitHub Action wrapper -- deliberately not
// duplicated inside `migrate` or anywhere else.
func NewValidateCmd() *cobra.Command {
	var (
		jsonFlag    bool
		composeFlag string
	)

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate a DSO-managed Compose project (read-only, safe for CI)",
		Long: `Validate a DSO-managed Compose project.

Checks Compose syntax and structure, the syntax of every dso:// / dsofile://
reference, whether referenced secrets exist in the local vault, and the
configured provider's credentials -- without modifying any file, importing
any secret, or printing any secret value.

validate never prompts, never mutates project or vault state, and is safe
to run in CI. Exit code 0 means valid; non-zero means at least one check
failed.

"Valid" describes configuration correctness as far as these checks can
determine -- it does not guarantee the project will deploy successfully
(e.g. Docker/network conditions at deploy time are out of scope; see
'docker dso doctor' for environment-level checks).

Examples:
  docker dso validate              # human-readable report
  docker dso validate --json       # machine-readable, for CI/scripts`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runValidate(cmd.Context(), jsonFlag, composeFlag)
		},
	}

	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Output as JSON")
	cmd.Flags().StringVar(&composeFlag, "file", "", "Path to the Compose file (default: auto-detected)")

	return cmd
}

func runValidate(ctx context.Context, jsonOut bool, composeFlag string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	cfgPath := ResolveConfig()
	mode, _ := detectMode("", cfgPath)
	setupMode := setup.ModeLocal
	if mode == "cloud" {
		setupMode = setup.ModeAgent
	}
	providerNames := configuredProviderNames(cfgPath)

	// Configuration + Provider checks are the *same* setup.NewDoctor()
	// engine doctor.go already wires up -- reused directly rather than
	// re-validated with separate logic. Docker/runtime/permissions/service
	// checks are deliberately excluded: those are doctor's environment
	// concerns, not this project's configuration correctness.
	var checks []setup.DoctorCheck
	for _, c := range runEnvironmentChecks(ctx, setupMode, cfgPath, providerNames) {
		if c.Category == setup.DoctorCatConfiguration || c.Category == setup.DoctorCatProvider {
			checks = append(checks, c)
		}
	}

	composePath := composeFlag
	if composePath == "" {
		found, ok := findComposeFile()
		if !ok {
			checks = append(checks, setup.DoctorCheck{
				ID: "DSO-VALIDATE-001", Category: validateCatCompose, Status: setup.DoctorFail, Severity: setup.DoctorCritical,
				Name: "Compose file readable", Description: "The Compose file must exist and be readable",
				Detail: "no docker-compose.yml/.yaml found in the current directory",
			})
			return renderValidateResult(checks, jsonOut)
		}
		composePath = found
	}
	if _, err := config.IsSafePath("", composePath); err != nil {
		checks = append(checks, setup.DoctorCheck{
			ID: "DSO-VALIDATE-001", Category: validateCatCompose, Status: setup.DoctorFail, Severity: setup.DoctorCritical,
			Name: "Compose file readable", Description: "The Compose file must exist and be readable",
			Detail: fmt.Sprintf("invalid compose file path: %v", err),
		})
		return renderValidateResult(checks, jsonOut)
	}

	root, composeChecks := checkComposeStructure(composePath)
	checks = append(checks, composeChecks...)

	if root != nil {
		project := getProjectName(nil)
		refs, refChecks := collectDSOReferences(root, project)
		checks = append(checks, refChecks...)
		checks = append(checks, checkSecretExistence(refs)...)
	}

	return renderValidateResult(checks, jsonOut)
}

func renderValidateResult(checks []setup.DoctorCheck, jsonOut bool) error {
	result := buildDoctorResult(checks)

	if jsonOut {
		out, err := result.RenderJSON()
		if err != nil {
			return err
		}
		fmt.Println(out)
	} else {
		fmt.Println(renderSectionedResult("DSO Validate", result, validateSections, true))
	}

	if result.OverallStatus == setup.DoctorFail {
		return fmt.Errorf("validation failed")
	}
	return nil
}
