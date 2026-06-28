package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// ─── Status and classification ────────────────────────────────────────────────

// DoctorStatus is the result of a single check.
type DoctorStatus string

const (
	DoctorPass DoctorStatus = "PASS"
	DoctorWarn DoctorStatus = "WARN"
	DoctorFail DoctorStatus = "FAIL"
	DoctorInfo DoctorStatus = "INFO"
)

// DoctorSeverity describes the impact of a failing check.
// Only meaningful when Status == DoctorFail or DoctorWarn.
type DoctorSeverity string

const (
	DoctorCritical DoctorSeverity = "critical"
	DoctorHigh     DoctorSeverity = "high"
	DoctorMedium   DoctorSeverity = "medium"
	DoctorLow      DoctorSeverity = "low"
)

// DoctorCategory groups related checks for filtering and Repair routing.
type DoctorCategory string

const (
	DoctorCatDocker        DoctorCategory = "docker"
	DoctorCatPermissions   DoctorCategory = "permissions"
	DoctorCatSecurity      DoctorCategory = "security"
	DoctorCatConfiguration DoctorCategory = "configuration"
	DoctorCatProvider      DoctorCategory = "provider"
	DoctorCatRuntime       DoctorCategory = "runtime"
	DoctorCatService       DoctorCategory = "service"
)

// ─── Result model ─────────────────────────────────────────────────────────────

// DoctorCheck is the result of a single diagnostic check.
// IDs are stable — referenced by documentation, Repair, and support responses.
type DoctorCheck struct {
	ID          string         // e.g. "DSO-DOCTOR-001" — never changes between releases
	Category    DoctorCategory
	Severity    DoctorSeverity // only meaningful for WARN / FAIL
	Status      DoctorStatus
	Name        string   // short human-readable name
	Description string   // what was checked
	Detail      string   // what was found
	RootCause   string   // why it failed (populated for WARN/FAIL)
	Recovery    []string // ordered fix steps (populated for WARN/FAIL)
}

// DoctorSummary aggregates check counts for quick display.
type DoctorSummary struct {
	Total    int
	Passed   int
	Warnings int
	Failures int
	Infos    int
}

// DoctorResult is the complete output of a Doctor run.
// It is the input to the Repair engine (Phase 9).
type DoctorResult struct {
	OverallStatus DoctorStatus
	Checks        []DoctorCheck
	Summary       DoctorSummary
	Timestamp     time.Time
}

// DoctorOptions controls which checks are run and how results are presented.
type DoctorOptions struct {
	Mode     SetupMode
	Provider string
	Verbose  bool
	Format   string // "terminal" | "json"

	// Override production defaults for test injection.
	ConfigPath   string
	RuntimeDir   string
	DockerSocket string
}

// ─── Engine ──────────────────────────────────────────────────────────────────

// Doctor runs independent diagnostic checks against the live system and
// produces a structured report. It never modifies system state.
type Doctor struct {
	opts     DoctorOptions
	docker   *DockerChecks
	perms    *PermissionChecks
	config   *ConfigurationChecks
	provider *ProviderChecks
	runtime  *RuntimeChecks
	service  *ServiceChecks
}

// NewDoctor constructs a Doctor wired to real OS hooks.
func NewDoctor(opts DoctorOptions) *Doctor {
	if opts.DockerSocket == "" {
		opts.DockerSocket = "/var/run/docker.sock"
	}
	if opts.ConfigPath == "" {
		if opts.Mode == ModeAgent {
			opts.ConfigPath = "/etc/dso/dso.yaml"
		} else {
			opts.ConfigPath = doctorExpandHome("~/.dso/dso.yaml")
		}
	}
	if opts.RuntimeDir == "" {
		if opts.Mode == ModeAgent {
			opts.RuntimeDir = "/var/run/dso"
		} else {
			opts.RuntimeDir = doctorExpandHome("~/.dso/run")
		}
	}
	return &Doctor{
		opts:     opts,
		docker:   newDockerChecks(opts.DockerSocket),
		perms:    newPermissionChecks(opts.DockerSocket, opts.ConfigPath),
		config:   newConfigurationChecks(opts.ConfigPath),
		provider: newProviderChecks(opts.Provider),
		runtime:  newRuntimeChecks(opts.RuntimeDir),
		service:  newServiceChecks(),
	}
}

// Run executes all diagnostic checks and returns the aggregated result.
// Checks are independent — no check depends on the result of another.
func (d *Doctor) Run(ctx context.Context) *DoctorResult {
	var checks []DoctorCheck
	checks = append(checks, d.docker.run(ctx)...)
	checks = append(checks, d.perms.run(ctx)...)
	checks = append(checks, d.config.run(ctx)...)
	checks = append(checks, d.provider.run(ctx)...)
	checks = append(checks, d.runtime.run(ctx)...)
	checks = append(checks, d.service.run(ctx)...)

	return &DoctorResult{
		OverallStatus: doctorComputeOverall(checks),
		Checks:        checks,
		Summary:       doctorComputeSummary(checks),
		Timestamp:     time.Now(),
	}
}

// ─── Rendering ────────────────────────────────────────────────────────────────

const doctorDivider = "─────────────────────────────────────────────────"

// RenderTerminal produces a human-readable diagnostic report.
// When verbose is true, WARN and FAIL checks include RootCause and Recovery.
func (r *DoctorResult) RenderTerminal(verbose bool) string {
	var b strings.Builder

	b.WriteString(doctorDivider + "\n")
	fmt.Fprintf(&b, "DSO Doctor  —  %s\n", r.Timestamp.Format("2006-01-02 15:04:05"))
	b.WriteString(doctorDivider + "\n\n")

	for _, c := range r.Checks {
		statusTag := fmt.Sprintf("[%s]", string(c.Status))
		fmt.Fprintf(&b, "  %-6s %-16s  %s\n", statusTag, c.ID, c.Name)
		if verbose && (c.Status == DoctorFail || c.Status == DoctorWarn) {
			if c.Detail != "" {
				fmt.Fprintf(&b, "           Detail:     %s\n", c.Detail)
			}
			if c.RootCause != "" {
				fmt.Fprintf(&b, "           Root cause: %s\n", c.RootCause)
			}
			for i, step := range c.Recovery {
				fmt.Fprintf(&b, "           Fix %d:      %s\n", i+1, step)
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\n" + doctorDivider + "\n")
	s := r.Summary
	fmt.Fprintf(&b, "  Overall: %-5s   %d checks   %d passed   %d warnings   %d failures\n",
		string(r.OverallStatus), s.Total, s.Passed, s.Warnings, s.Failures)
	b.WriteString(doctorDivider + "\n")

	return b.String()
}

// RenderJSON produces deterministic JSON output suitable for Repair consumption.
func (r *DoctorResult) RenderJSON() (string, error) {
	type jsonCheck struct {
		ID          string   `json:"id"`
		Category    string   `json:"category"`
		Severity    string   `json:"severity"`
		Status      string   `json:"status"`
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Detail      string   `json:"detail,omitempty"`
		RootCause   string   `json:"root_cause,omitempty"`
		Recovery    []string `json:"recovery,omitempty"`
	}
	type jsonSummary struct {
		Total    int `json:"total"`
		Passed   int `json:"passed"`
		Warnings int `json:"warnings"`
		Failures int `json:"failures"`
		Infos    int `json:"infos"`
	}
	type jsonResult struct {
		OverallStatus string      `json:"overall_status"`
		Timestamp     string      `json:"timestamp"`
		Summary       jsonSummary `json:"summary"`
		Checks        []jsonCheck `json:"checks"`
	}

	checks := make([]jsonCheck, len(r.Checks))
	for i, c := range r.Checks {
		recovery := c.Recovery
		if recovery == nil {
			recovery = []string{}
		}
		checks[i] = jsonCheck{
			ID:          c.ID,
			Category:    string(c.Category),
			Severity:    string(c.Severity),
			Status:      string(c.Status),
			Name:        c.Name,
			Description: c.Description,
			Detail:      c.Detail,
			RootCause:   c.RootCause,
			Recovery:    recovery,
		}
	}

	doc := jsonResult{
		OverallStatus: string(r.OverallStatus),
		Timestamp:     r.Timestamp.UTC().Format("2006-01-02T15:04:05Z"),
		Summary: jsonSummary{
			Total:    r.Summary.Total,
			Passed:   r.Summary.Passed,
			Warnings: r.Summary.Warnings,
			Failures: r.Summary.Failures,
			Infos:    r.Summary.Infos,
		},
		Checks: checks,
	}

	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal doctor result: %w", err)
	}
	return string(b), nil
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

func doctorComputeOverall(checks []DoctorCheck) DoctorStatus {
	hasFail, hasWarn := false, false
	for _, c := range checks {
		switch c.Status {
		case DoctorFail:
			hasFail = true
		case DoctorWarn:
			hasWarn = true
		}
	}
	if hasFail {
		return DoctorFail
	}
	if hasWarn {
		return DoctorWarn
	}
	return DoctorPass
}

func doctorComputeSummary(checks []DoctorCheck) DoctorSummary {
	s := DoctorSummary{Total: len(checks)}
	for _, c := range checks {
		switch c.Status {
		case DoctorPass:
			s.Passed++
		case DoctorWarn:
			s.Warnings++
		case DoctorFail:
			s.Failures++
		case DoctorInfo:
			s.Infos++
		}
	}
	return s
}

func doctorExpandHome(path string) string {
	if len(path) < 2 || path[:2] != "~/" {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return home + path[1:]
}

// passCheck is a convenience builder for passing checks.
func passCheck(id, name, description, detail string, cat DoctorCategory) DoctorCheck {
	return DoctorCheck{
		ID:          id,
		Category:    cat,
		Severity:    DoctorLow,
		Status:      DoctorPass,
		Name:        name,
		Description: description,
		Detail:      detail,
	}
}

// failCheck is a convenience builder for failing checks.
func failCheck(id, name, description, detail, rootCause string, severity DoctorSeverity, cat DoctorCategory, recovery ...string) DoctorCheck {
	return DoctorCheck{
		ID:          id,
		Category:    cat,
		Severity:    severity,
		Status:      DoctorFail,
		Name:        name,
		Description: description,
		Detail:      detail,
		RootCause:   rootCause,
		Recovery:    recovery,
	}
}

// warnCheck is a convenience builder for warning checks.
func warnCheck(id, name, description, detail, rootCause string, cat DoctorCategory, recovery ...string) DoctorCheck {
	return DoctorCheck{
		ID:          id,
		Category:    cat,
		Severity:    DoctorMedium,
		Status:      DoctorWarn,
		Name:        name,
		Description: description,
		Detail:      detail,
		RootCause:   rootCause,
		Recovery:    recovery,
	}
}

// infoCheck is a convenience builder for informational checks.
func infoCheck(id, name, description, detail string, cat DoctorCategory) DoctorCheck {
	return DoctorCheck{
		ID:          id,
		Category:    cat,
		Severity:    DoctorLow,
		Status:      DoctorInfo,
		Name:        name,
		Description: description,
		Detail:      detail,
	}
}
