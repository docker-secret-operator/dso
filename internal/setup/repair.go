package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ─── Risk model ───────────────────────────────────────────────────────────────

// RepairRisk describes the impact level of a repair action.
type RepairRisk string

const (
	RepairRiskSafe        RepairRisk = "safe"
	RepairRiskModerate    RepairRisk = "moderate"
	RepairRiskDestructive RepairRisk = "destructive"
)

// ─── Action lifecycle ─────────────────────────────────────────────────────────

// RepairStatus tracks the outcome of a single repair action.
type RepairStatus string

const (
	RepairStatusPending  RepairStatus = "pending"
	RepairStatusApplied  RepairStatus = "applied"
	RepairStatusFailed   RepairStatus = "failed"
	RepairStatusDeclined RepairStatus = "declined"
	RepairStatusSkipped  RepairStatus = "skipped"
)

// ─── Plan model ───────────────────────────────────────────────────────────────

// RepairAction is a single remediation step generated from a DoctorCheck.
// IDs are stable — referenced by documentation and support responses.
type RepairAction struct {
	ID                  string
	IssueID             string         // DoctorCheck.ID this action addresses
	Category            DoctorCategory
	Description         string
	RiskLevel           RepairRisk
	RequiresConfirmation bool
	Status              RepairStatus
	Err                 error // non-nil when Status == RepairStatusFailed
}

// RepairPlan holds all repair actions derived from a DoctorResult.
// It is generated before any system state is modified.
type RepairPlan struct {
	Issues []RepairAction
}

// ─── Result model ─────────────────────────────────────────────────────────────

// RepairFailure records a repair action that failed during execution.
type RepairFailure struct {
	ActionID string
	IssueID  string
	Err      error
}

// RepairSummary aggregates repair outcome counts.
type RepairSummary struct {
	Total    int
	Applied  int
	Failed   int
	Declined int
}

// RepairResult is the structured output of a Repair execution.
// Verification holds the post-repair Doctor run for issue comparison.
type RepairResult struct {
	Plan         *RepairPlan
	Applied      []string // IssueIDs successfully applied
	Failed       []RepairFailure
	Declined     []string // IssueIDs declined by the user
	Summary      RepairSummary
	Verification *DoctorResult // post-repair Doctor run
	Timestamp    time.Time
}

// ─── Options and confirmation ─────────────────────────────────────────────────

// RepairOptions configures the repair engine.
type RepairOptions struct {
	Mode         SetupMode
	Provider     string
	ConfigPath   string
	RuntimeDir   string
	DockerSocket string
}

// ConfirmFunc is called before executing actions that require confirmation.
// Return true to proceed, false to decline.
type ConfirmFunc func(description string) bool

// AlwaysConfirm confirms every action without prompting (--yes mode and tests).
var AlwaysConfirm ConfirmFunc = func(_ string) bool { return true }

// NeverConfirm declines every confirmation (dry-run tests).
var NeverConfirm ConfirmFunc = func(_ string) bool { return false }

// ─── Engine ───────────────────────────────────────────────────────────────────

// Repair converts DoctorResult findings into concrete fix operations.
// It never performs its own diagnostics — all input comes from DoctorResult.
type Repair struct {
	opts      RepairOptions
	perms     *RepairPermissions
	config    *RepairConfiguration
	runtime   *RepairRuntime
	service   *RepairService
	provider  *RepairProvider
	runDoctor func(context.Context) *DoctorResult // injectable for tests
}

// NewRepair constructs a Repair engine wired to real OS hooks.
func NewRepair(opts RepairOptions) *Repair {
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
	r := &Repair{
		opts:     opts,
		perms:    newRepairPermissions(opts.DockerSocket, opts.ConfigPath),
		config:   newRepairConfiguration(opts.ConfigPath, opts.Provider),
		runtime:  newRepairRuntime(opts.RuntimeDir),
		service:  newRepairService(),
		provider: newRepairProvider(opts.Provider),
	}
	r.runDoctor = func(ctx context.Context) *DoctorResult {
		d := NewDoctor(DoctorOptions{
			Mode:         opts.Mode,
			Provider:     opts.Provider,
			ConfigPath:   opts.ConfigPath,
			RuntimeDir:   opts.RuntimeDir,
			DockerSocket: opts.DockerSocket,
		})
		return d.Run(ctx)
	}
	return r
}

// Plan generates a RepairPlan from a DoctorResult without modifying any system state.
func (r *Repair) Plan(_ context.Context, result *DoctorResult) *RepairPlan {
	var actions []RepairAction
	for _, check := range result.Checks {
		if check.Status != DoctorFail && check.Status != DoctorWarn {
			continue
		}
		action := r.planAction(check)
		if action == nil {
			continue
		}
		actions = append(actions, *action)
	}
	return &RepairPlan{Issues: actions}
}

// planAction routes a single DoctorCheck to the appropriate repair executor.
// Returns nil when no automatic repair is available for the check.
func (r *Repair) planAction(check DoctorCheck) *RepairAction {
	switch check.ID {
	case "DSO-DOCTOR-004", "DSO-DOCTOR-005", "DSO-DOCTOR-009":
		return r.perms.planForCheck(check)
	case "DSO-DOCTOR-007", "DSO-DOCTOR-008":
		return r.config.planForCheck(check)
	case "DSO-DOCTOR-012", "DSO-DOCTOR-013":
		return r.runtime.planForCheck(check)
	case "DSO-DOCTOR-015", "DSO-DOCTOR-016", "DSO-DOCTOR-017":
		return r.service.planForCheck(check)
	case "DSO-DOCTOR-010", "DSO-DOCTOR-011":
		return r.provider.planForCheck(check)
	default:
		// DSO-DOCTOR-001/002/003 (Docker infra), 006 (root INFO), 014 (binary missing):
		// no automatic repair is safe or feasible.
		return nil
	}
}

// Execute runs all actions in the RepairPlan, prompting for confirmation where
// required. After all actions complete it runs Doctor for verification.
func (r *Repair) Execute(ctx context.Context, plan *RepairPlan, confirm ConfirmFunc) *RepairResult {
	if confirm == nil {
		confirm = AlwaysConfirm
	}
	for i := range plan.Issues {
		action := &plan.Issues[i]
		if action.RequiresConfirmation && !confirm(action.Description) {
			action.Status = RepairStatusDeclined
			continue
		}
		if err := r.executeAction(ctx, action); err != nil {
			action.Status = RepairStatusFailed
			action.Err = err
		} else {
			action.Status = RepairStatusApplied
		}
	}

	verification := r.runDoctor(ctx)
	return r.buildResult(plan, verification)
}

// executeAction dispatches a single repair action to the appropriate executor.
func (r *Repair) executeAction(ctx context.Context, action *RepairAction) error {
	switch action.IssueID {
	case "DSO-DOCTOR-004":
		return r.perms.repairSocketPerms()
	case "DSO-DOCTOR-005", "DSO-DOCTOR-009":
		return r.perms.repairConfigPerms()
	case "DSO-DOCTOR-007":
		return r.config.createDefaultConfig()
	case "DSO-DOCTOR-008":
		return r.config.recreateEmptyConfig()
	case "DSO-DOCTOR-012":
		return r.runtime.createRuntimeDir()
	case "DSO-DOCTOR-013":
		return r.runtime.removeStaleLocks()
	case "DSO-DOCTOR-015":
		return r.service.writeUnitFile()
	case "DSO-DOCTOR-016":
		return r.service.enableService(ctx)
	case "DSO-DOCTOR-017":
		return r.service.startService(ctx)
	default:
		return fmt.Errorf("no executable repair for issue %s", action.IssueID)
	}
}

// buildResult assembles the RepairResult from the executed plan and verification.
func (r *Repair) buildResult(plan *RepairPlan, verification *DoctorResult) *RepairResult {
	result := &RepairResult{
		Plan:         plan,
		Verification: verification,
		Timestamp:    time.Now(),
	}
	for _, action := range plan.Issues {
		switch action.Status {
		case RepairStatusApplied:
			result.Applied = append(result.Applied, action.IssueID)
		case RepairStatusFailed:
			result.Failed = append(result.Failed, RepairFailure{
				ActionID: action.ID,
				IssueID:  action.IssueID,
				Err:      action.Err,
			})
		case RepairStatusDeclined:
			result.Declined = append(result.Declined, action.IssueID)
		}
	}
	result.Summary = RepairSummary{
		Total:    len(plan.Issues),
		Applied:  len(result.Applied),
		Failed:   len(result.Failed),
		Declined: len(result.Declined),
	}
	return result
}

// ─── Rendering ────────────────────────────────────────────────────────────────

const repairDivider = "─────────────────────────────────────────────────"

// RenderTerminal produces a human-readable repair report.
func (r *RepairResult) RenderTerminal() string {
	var b strings.Builder

	b.WriteString(repairDivider + "\n")
	fmt.Fprintf(&b, "DSO Repair  —  %s\n", r.Timestamp.Format("2006-01-02 15:04:05"))
	b.WriteString(repairDivider + "\n\n")

	if len(r.Plan.Issues) == 0 {
		b.WriteString("  No repairs needed — system is healthy.\n\n")
	} else {
		for _, action := range r.Plan.Issues {
			fmt.Fprintf(&b, "  %-10s %-16s  %s\n",
				repairStatusTag(action.Status), action.IssueID, action.Description)
			if action.Err != nil {
				fmt.Fprintf(&b, "             Error: %s\n", action.Err)
			}
		}
		b.WriteString("\n")
	}

	if r.Verification != nil {
		fmt.Fprintf(&b, "Verification: %s\n\n", string(r.Verification.OverallStatus))
	}

	b.WriteString(repairDivider + "\n")
	s := r.Summary
	fmt.Fprintf(&b, "  %d applied   %d failed   %d declined\n", s.Applied, s.Failed, s.Declined)
	b.WriteString(repairDivider + "\n")

	return b.String()
}

func repairStatusTag(s RepairStatus) string {
	switch s {
	case RepairStatusApplied:
		return "[APPLIED]"
	case RepairStatusFailed:
		return "[FAILED]"
	case RepairStatusDeclined:
		return "[DECLINED]"
	case RepairStatusSkipped:
		return "[SKIPPED]"
	default:
		return "[PENDING]"
	}
}

// RenderJSON produces structured JSON output for programmatic consumption.
func (r *RepairResult) RenderJSON() (string, error) {
	type jsonAction struct {
		ID                  string `json:"id"`
		IssueID             string `json:"issue_id"`
		Category            string `json:"category"`
		Description         string `json:"description"`
		RiskLevel           string `json:"risk_level"`
		RequiresConfirmation bool   `json:"requires_confirmation"`
		Status              string `json:"status"`
		Error               string `json:"error,omitempty"`
	}
	type jsonSummary struct {
		Total    int `json:"total"`
		Applied  int `json:"applied"`
		Failed   int `json:"failed"`
		Declined int `json:"declined"`
	}
	type jsonResult struct {
		Timestamp          string       `json:"timestamp"`
		Summary            jsonSummary  `json:"summary"`
		Actions            []jsonAction `json:"actions"`
		VerificationStatus string       `json:"verification_status,omitempty"`
	}

	actions := make([]jsonAction, len(r.Plan.Issues))
	for i, a := range r.Plan.Issues {
		errStr := ""
		if a.Err != nil {
			errStr = a.Err.Error()
		}
		actions[i] = jsonAction{
			ID:                   a.ID,
			IssueID:              a.IssueID,
			Category:             string(a.Category),
			Description:          a.Description,
			RiskLevel:            string(a.RiskLevel),
			RequiresConfirmation: a.RequiresConfirmation,
			Status:               string(a.Status),
			Error:                errStr,
		}
	}

	verStatus := ""
	if r.Verification != nil {
		verStatus = string(r.Verification.OverallStatus)
	}

	doc := jsonResult{
		Timestamp:          r.Timestamp.UTC().Format("2006-01-02T15:04:05Z"),
		Summary:            jsonSummary{Total: r.Summary.Total, Applied: r.Summary.Applied, Failed: r.Summary.Failed, Declined: r.Summary.Declined},
		Actions:            actions,
		VerificationStatus: verStatus,
	}

	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal repair result: %w", err)
	}
	return string(b), nil
}
