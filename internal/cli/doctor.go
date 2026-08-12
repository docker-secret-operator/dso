package cli

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/docker-secret-operator/dso/internal/compose"
	"github.com/docker-secret-operator/dso/internal/setup"
	dsoConfig "github.com/docker-secret-operator/dso/pkg/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// doctorCatProject groups the project-level checks added on top of
// setup.NewDoctor's system/environment checks (compose/.env/DSO-reference
// detection). It deliberately lives in this package, not internal/setup:
// these checks are about the user's current project directory, not the
// host system, so they don't belong in the engine setup.Repair also drives.
const doctorCatProject setup.DoctorCategory = "project"

// doctorSection is a display grouping for terminal output. JSON output
// stays flat (by Category) since machine consumers care about the
// individual check, not how it's grouped on screen.
type doctorSection struct {
	title      string
	categories map[setup.DoctorCategory]bool
}

var doctorSections = []doctorSection{
	{
		title: "Environment",
		categories: map[setup.DoctorCategory]bool{
			setup.DoctorCatDocker:      true,
			setup.DoctorCatRuntime:     true,
			setup.DoctorCatPermissions: true,
			setup.DoctorCatService:     true,
		},
	},
	{title: "Configuration", categories: map[setup.DoctorCategory]bool{setup.DoctorCatConfiguration: true}},
	{title: "Provider", categories: map[setup.DoctorCategory]bool{setup.DoctorCatProvider: true}},
	{title: "Project", categories: map[setup.DoctorCategory]bool{doctorCatProject: true}},
}

// NewDoctorCmd creates the doctor diagnostics command.
func NewDoctorCmd() *cobra.Command {
	var (
		levelFlag string
		jsonFlag  bool
	)

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose the local DSO environment and current project",
		Long: `Diagnose the local DSO environment and current project.

Doctor performs safe, read-only checks: Docker connectivity, DSO
configuration validity, configured provider credentials, and (when run
inside a Compose project) whether the project has a compose file, a
plaintext .env file, and well-formed DSO secret references.

Doctor never prints secret values, tokens, or credentials, and never
modifies any files.

Examples:
  docker dso doctor              # Quick health check
  docker dso doctor --level full # Include recovery steps for failures
  docker dso doctor --json       # Machine-readable output`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(cmd.Context(), levelFlag, jsonFlag)
		},
	}

	cmd.Flags().StringVar(&levelFlag, "level", "default", "Diagnostic level: default, full")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Output as JSON")

	return cmd
}

func runDoctor(ctx context.Context, level string, jsonOut bool) error {
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

	checks := runEnvironmentChecks(ctx, setupMode, cfgPath, providerNames)
	checks = append(checks, runProjectChecks()...)

	result := buildDoctorResult(checks)

	if jsonOut {
		out, err := result.RenderJSON()
		if err != nil {
			return err
		}
		fmt.Println(out)
	} else {
		fmt.Println(renderSectionedResult("DSO Doctor", result, doctorSections, level == "full"))
	}

	if result.OverallStatus == setup.DoctorFail {
		return fmt.Errorf("doctor detected failing checks")
	}
	return nil
}

// configuredProviderNames returns the names of providers configured in
// dso.yaml, sorted for deterministic output. A load failure or absent
// config returns nil -- setup.NewDoctor's own configuration checks already
// surface that as a FAIL/WARN, so this just falls back to "no provider
// configured" rather than erroring twice.
func configuredProviderNames(cfgPath string) []string {
	cfg, err := dsoConfig.LoadConfig(cfgPath)
	if err != nil || cfg == nil || len(cfg.Providers) == 0 {
		return nil
	}
	names := make([]string, 0, len(cfg.Providers))
	for name := range cfg.Providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// runEnvironmentChecks runs setup.NewDoctor's full check suite (Docker,
// runtime, permissions, configuration, service, provider) -- the same
// engine internal/setup/repair.go uses. Reused wholesale rather than
// reimplemented: this is a read-only diagnostic call, no state mutation.
func runEnvironmentChecks(ctx context.Context, mode setup.SetupMode, cfgPath string, providerNames []string) []setup.DoctorCheck {
	if len(providerNames) == 0 {
		return setup.NewDoctor(setup.DoctorOptions{Mode: mode, ConfigPath: cfgPath}).Run(ctx).Checks
	}

	base := setup.NewDoctor(setup.DoctorOptions{Mode: mode, ConfigPath: cfgPath, Provider: providerNames[0]}).Run(ctx)
	checks := base.Checks

	// Multiple providers configured: the first run above already covers
	// Docker/runtime/permissions/configuration/service once, plus the first
	// provider. Re-running the full doctor per additional provider would
	// duplicate those non-provider checks in the output, so only their
	// provider-category checks are kept. Check IDs (DSO-DOCTOR-010/011) are
	// shared across providers by design in setup.ProviderChecks, so the
	// provider name is folded into Name here to keep multi-provider output
	// distinguishable.
	for _, name := range providerNames[1:] {
		extra := setup.NewDoctor(setup.DoctorOptions{Mode: mode, ConfigPath: cfgPath, Provider: name}).Run(ctx)
		for _, c := range extra.Checks {
			if c.Category != setup.DoctorCatProvider {
				continue
			}
			c.Name = fmt.Sprintf("%s (%s)", c.Name, name)
			checks = append(checks, c)
		}
	}
	return checks
}

// runProjectChecks inspects the current directory for a Compose project:
// whether a compose file exists, whether a plaintext .env sits alongside
// it, and whether any dso:// / dsofile:// references in the compose file
// are at least well-formed. This does not consult the vault or any
// provider -- confirming a referenced secret actually exists is
// `docker dso validate`'s job, not doctor's; doctor stays fast and
// read-only with zero network/provider calls.
func runProjectChecks() []setup.DoctorCheck {
	composePath, found := findComposeFile()
	if !found {
		return []setup.DoctorCheck{{
			ID: "DSO-PROJECT-001", Category: doctorCatProject, Status: setup.DoctorInfo,
			Name: "Compose file", Description: "A docker-compose.yml/.yaml in the current directory",
			Detail: "no compose file found in the current directory",
		}}
	}

	checks := []setup.DoctorCheck{{
		ID: "DSO-PROJECT-001", Category: doctorCatProject, Status: setup.DoctorPass,
		Name: "Compose file", Description: "A docker-compose.yml/.yaml in the current directory",
		Detail: "found " + composePath,
	}}

	checks = append(checks, checkDotEnvPresence())

	content, err := os.ReadFile(composePath) // #nosec G304 -- composePath comes from findComposeFile's fixed candidate list, not user input
	if err != nil {
		return append(checks, setup.DoctorCheck{
			ID: "DSO-PROJECT-003", Category: doctorCatProject, Status: setup.DoctorFail, Severity: setup.DoctorHigh,
			Name: "Compose syntax", Description: "Compose file must be readable and valid YAML",
			Detail: fmt.Sprintf("failed to read %s: %v", composePath, err),
		})
	}

	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		return append(checks, setup.DoctorCheck{
			ID: "DSO-PROJECT-003", Category: doctorCatProject, Status: setup.DoctorFail, Severity: setup.DoctorHigh,
			Name: "Compose syntax", Description: "Compose file must be readable and valid YAML",
			Detail:    fmt.Sprintf("YAML parse error: %v", err),
			RootCause: "Compose file contains invalid YAML",
			Recovery:  []string{"Validate YAML syntax and re-run doctor"},
		})
	}
	checks = append(checks, setup.DoctorCheck{
		ID: "DSO-PROJECT-003", Category: doctorCatProject, Status: setup.DoctorPass,
		Name: "Compose syntax", Description: "Compose file must be readable and valid YAML",
		Detail: "valid YAML",
	})

	checks = append(checks, checkDSOReferences(&root))
	return checks
}

func checkDotEnvPresence() setup.DoctorCheck {
	if _, err := os.Stat(".env"); err != nil {
		return setup.DoctorCheck{
			ID: "DSO-PROJECT-002", Category: doctorCatProject, Status: setup.DoctorInfo,
			Name: ".env file", Description: "Whether a plaintext .env file sits alongside the compose project",
			Detail: "no .env file found",
		}
	}
	return setup.DoctorCheck{
		ID: "DSO-PROJECT-002", Category: doctorCatProject, Status: setup.DoctorWarn,
		Name: ".env file", Description: "Whether a plaintext .env file sits alongside the compose project",
		Detail:    ".env file present",
		RootCause: "Plaintext secrets in .env are readable by any local process and are the exact risk DSO exists to remove",
		Recovery:  []string{"Run 'docker dso migrate' to move its secret values into the DSO vault"},
	}
}

// checkDSOReferences walks services.*.environment for dso:// / dsofile://
// values and reports how many were found and whether any are malformed
// (empty path after the prefix). This mirrors the URI-shape rule
// internal/resolver already enforces (project/path must both be
// non-empty) without importing resolver's unexported parser -- it is a
// two-line format check, not a re-implementation of secret resolution.
func checkDSOReferences(root *yaml.Node) setup.DoctorCheck {
	doc := root
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		doc = doc.Content[0]
	}
	servicesNode := compose.GetMapValue(doc, "services")

	var total int
	var malformed []string

	if servicesNode != nil && servicesNode.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(servicesNode.Content); i += 2 {
			serviceName := servicesNode.Content[i].Value
			serviceBody := servicesNode.Content[i+1]
			if serviceBody == nil || serviceBody.Kind != yaml.MappingNode {
				continue
			}
			envNode := compose.GetMapValue(serviceBody, "environment")
			if envNode == nil {
				continue
			}

			values := scalarEnvValues(envNode)
			for _, v := range values {
				uri, prefix := "", ""
				switch {
				case strings.HasPrefix(v, "dsofile://"):
					prefix, uri = "dsofile://", strings.TrimPrefix(v, "dsofile://")
				case strings.HasPrefix(v, "dso://"):
					prefix, uri = "dso://", strings.TrimPrefix(v, "dso://")
				default:
					continue
				}
				total++
				if strings.TrimSpace(uri) == "" {
					malformed = append(malformed, fmt.Sprintf("%s: %s<empty>", serviceName, prefix))
				}
			}
		}
	}

	switch {
	case total == 0:
		return setup.DoctorCheck{
			ID: "DSO-PROJECT-004", Category: doctorCatProject, Status: setup.DoctorInfo,
			Name: "DSO references", Description: "dso:// / dsofile:// references in the compose file",
			Detail: "none found -- this project is not yet DSO-managed; run 'docker dso migrate' to get started",
		}
	case len(malformed) > 0:
		return setup.DoctorCheck{
			ID: "DSO-PROJECT-004", Category: doctorCatProject, Status: setup.DoctorFail, Severity: setup.DoctorHigh,
			Name: "DSO references", Description: "dso:// / dsofile:// references in the compose file",
			Detail:    fmt.Sprintf("%d reference(s) found, %d malformed", total, len(malformed)),
			RootCause: "a dso:// or dsofile:// URI is missing its secret path (expected dso://[project/]path): " + strings.Join(malformed, ", "),
			Recovery:  []string{"Fix the malformed reference(s) listed above"},
		}
	default:
		return setup.DoctorCheck{
			ID: "DSO-PROJECT-004", Category: doctorCatProject, Status: setup.DoctorPass,
			Name: "DSO references", Description: "dso:// / dsofile:// references in the compose file",
			Detail: fmt.Sprintf("%d reference(s) found, all well-formed", total),
		}
	}
}

// scalarEnvValues extracts the raw values from an `environment:` node in
// either its mapping form (KEY: value) or its list form (KEY=value).
func scalarEnvValues(envNode *yaml.Node) []string {
	var out []string
	switch envNode.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(envNode.Content); i += 2 {
			val := envNode.Content[i+1]
			if val != nil && val.Kind == yaml.ScalarNode {
				out = append(out, val.Value)
			}
		}
	case yaml.SequenceNode:
		for _, item := range envNode.Content {
			if item.Kind != yaml.ScalarNode {
				continue
			}
			parts := strings.SplitN(item.Value, "=", 2)
			if len(parts) == 2 {
				out = append(out, parts[1])
			}
		}
	}
	return out
}

// findComposeFile looks for the standard compose filenames in the current
// directory, matching the same candidates `docker dso up` checks for.
func findComposeFile() (string, bool) {
	for _, name := range []string{"docker-compose.yml", "docker-compose.yaml"} {
		if _, err := os.Stat(name); err == nil {
			return name, true
		}
	}
	return "", false
}

// buildDoctorResult computes OverallStatus/Summary locally rather than via
// setup's unexported doctorComputeOverall/doctorComputeSummary -- those stay
// package-private to internal/setup, and the aggregation rule (any FAIL ->
// FAIL; else any WARN -> WARN; else PASS) is simple enough not to warrant
// exporting them just for this.
func buildDoctorResult(checks []setup.DoctorCheck) *setup.DoctorResult {
	summary := setup.DoctorSummary{Total: len(checks)}
	overall := setup.DoctorPass
	for _, c := range checks {
		switch c.Status {
		case setup.DoctorPass:
			summary.Passed++
		case setup.DoctorWarn:
			summary.Warnings++
			if overall != setup.DoctorFail {
				overall = setup.DoctorWarn
			}
		case setup.DoctorFail:
			summary.Failures++
			overall = setup.DoctorFail
		case setup.DoctorInfo:
			summary.Infos++
		}
	}
	return &setup.DoctorResult{
		OverallStatus: overall,
		Checks:        checks,
		Summary:       summary,
	}
}

// renderSectionedResult renders result's checks grouped into sections, for
// any command sharing this display shape (doctor, validate) -- both build
// on the same setup.DoctorCheck/DoctorResult types, so they share this one
// renderer rather than each formatting output independently.
func renderSectionedResult(heading string, result *setup.DoctorResult, sections []doctorSection, verbose bool) string {
	var b strings.Builder
	b.WriteString(heading + "\n")
	b.WriteString(strings.Repeat("─", 43) + "\n")

	for _, section := range sections {
		var inSection []setup.DoctorCheck
		for _, c := range result.Checks {
			if section.categories[c.Category] {
				inSection = append(inSection, c)
			}
		}
		if len(inSection) == 0 {
			continue
		}

		fmt.Fprintf(&b, "\n%s\n", section.title)
		for _, c := range inSection {
			fmt.Fprintf(&b, "  %s %s: %s\n", doctorStatusSymbol(c.Status), c.Name, c.Detail)
			if verbose && (c.Status == setup.DoctorFail || c.Status == setup.DoctorWarn) {
				if c.RootCause != "" {
					fmt.Fprintf(&b, "      Root cause: %s\n", c.RootCause)
				}
				for i, step := range c.Recovery {
					fmt.Fprintf(&b, "      Fix %d: %s\n", i+1, step)
				}
			}
		}
	}

	s := result.Summary
	b.WriteString("\nResult\n")
	fmt.Fprintf(&b, "  %d checks passed\n", s.Passed)
	if s.Warnings > 0 {
		fmt.Fprintf(&b, "  %d warning(s)\n", s.Warnings)
	}
	if s.Failures > 0 {
		fmt.Fprintf(&b, "  %d error(s)\n", s.Failures)
	}
	if s.Infos > 0 {
		fmt.Fprintf(&b, "  %d informational\n", s.Infos)
	}

	return b.String()
}

func doctorStatusSymbol(status setup.DoctorStatus) string {
	switch status {
	case setup.DoctorPass:
		return "✓"
	case setup.DoctorFail:
		return "✗"
	case setup.DoctorWarn:
		return "⚠"
	case setup.DoctorInfo:
		return "-"
	default:
		return "?"
	}
}
