package setup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Planner generates an InstallPlan from environment facts, validation results,
// and user options. It has one responsibility: describe what apply() must do.
//
// The Planner must never:
//   - execute filesystem operations
//   - perform validation
//   - detect environment facts
//   - print or prompt
//
// Everything after the Planner — preview, apply, rollback, resume, Doctor —
// must consume only InstallPlan. They must never inspect Environment directly.
type Planner struct{}

func newPlanner() *Planner { return &Planner{} }

// Plan builds the complete, immutable InstallPlan for the given environment
// and options. The ValidationResult is consumed for routing decisions (e.g.
// whether an existing config requires an upgrade path) but is never re-evaluated.
func (p *Planner) Plan(_ context.Context, env *Environment, vr *ValidationResult, opts SetupOptions) (*InstallPlan, error) {
	mode, provider := resolveEffective(env, opts)

	plan := &InstallPlan{
		ID:        generatePlanID(),
		Version:   1,
		Timestamp: time.Now(),
		Mode:      mode,
		Provider:  provider,
		DryRun:    opts.DryRun,
		Metadata:  map[string]string{"schema": "v1"},
	}

	ctr := &opCounter{}

	p.planDirectories(plan, env, mode, ctr)
	p.planConfigFile(plan, env, mode, provider, vr, ctr)

	if mode == ModeAgent {
		p.planService(plan, ctr)
		p.planDockerGroup(plan, env, ctr)
	}

	plan.Summary = computeSummary(plan)
	return plan, nil
}

// ─── Directory planning ───────────────────────────────────────────────────────

func (p *Planner) planDirectories(plan *InstallPlan, env *Environment, mode SetupMode, ctr *opCounter) {
	configDir := configDirectory(env, mode)

	plan.Directories = append(plan.Directories, DirectoryChange{
		ID:        ctr.nextDir(),
		Path:      configDir,
		Mode:      configDirMode(mode),
		Owner:     configDirOwner(mode),
		Operation: "create",
	})
}

// ─── Config file planning ─────────────────────────────────────────────────────

func (p *Planner) planConfigFile(
	plan *InstallPlan,
	env *Environment,
	mode SetupMode,
	provider string,
	vr *ValidationResult,
	ctr *opCounter,
) {
	configDir := configDirectory(env, mode)
	configPath := filepath.Join(configDir, "dso.yaml")

	operation := "create"
	if isUpgrade(vr, configPath) {
		operation = "modify"
	}

	yaml := renderConfigYAML(mode, provider)
	plan.ConfigYAML = yaml

	plan.Files = append(plan.Files, FileChange{
		ID:        ctr.nextFile(),
		Path:      configPath,
		Content:   []byte(yaml),
		Mode:      configFileMode(mode),
		Owner:     configDirOwner(mode),
		Operation: operation,
	})
}

// ─── Service planning ─────────────────────────────────────────────────────────

const (
	dsoServiceName = "dso-agent.service"
	dsoServicePath = "/etc/systemd/system/" + dsoServiceName
)

var dsoServiceUnit = []byte(`[Unit]
Description=Docker Secret Operator Agent
Documentation=https://github.com/docker-secret-operator/dso
After=network.target docker.service
Requires=docker.service

[Service]
Type=simple
ExecStart=/usr/local/bin/dso agent
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
`)

func (p *Planner) planService(plan *InstallPlan, ctr *opCounter) {
	plan.Files = append(plan.Files, FileChange{
		ID:        ctr.nextFile(),
		Path:      dsoServicePath,
		Content:   dsoServiceUnit,
		Mode:      0644,
		Owner:     "root:root",
		Operation: "create",
	})
	plan.Services = append(plan.Services,
		ServiceChange{
			ID:        ctr.nextService(),
			Name:      dsoServiceName,
			Operation: "enable",
		},
		ServiceChange{
			ID:        ctr.nextService(),
			Name:      dsoServiceName,
			Operation: "start",
		},
	)
}

// ─── Group planning ───────────────────────────────────────────────────────────

func (p *Planner) planDockerGroup(plan *InstallPlan, env *Environment, ctr *opCounter) {
	// When running as root in agent mode, ensure the dso group exists so that
	// non-root access can be configured post-install.
	if env.User.IsRoot {
		plan.Groups = append(plan.Groups, GroupChange{
			ID:        ctr.nextGroup(),
			Name:      "dso",
			Operation: "create",
		})
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// resolveEffective returns the mode and provider that will be used, applying
// computeRecommendation as a fallback when the user left either unspecified.
func resolveEffective(env *Environment, opts SetupOptions) (SetupMode, string) {
	recMode, recProvider := computeRecommendation(env)

	mode := opts.Mode
	if mode == "" {
		mode = recMode
	}
	provider := opts.Provider
	if provider == "" {
		provider = recProvider
	}
	return mode, provider
}

// computeRecommendation derives the best mode and provider from detected
// capabilities. Provider priority: aws > azure > vault > local.
// Moved here from engine.go in Phase 4; Phase 5+ consume it through the plan.
func computeRecommendation(env *Environment) (SetupMode, string) {
	mode := ModeLocal
	if env.Capabilities.SupportsAgentMode {
		mode = ModeAgent
	}

	provider := "local"
	if env.Providers.Vault.Detected {
		provider = "vault"
	}
	if env.Providers.Azure.Detected {
		provider = "azure"
	}
	if env.Providers.AWS.Detected {
		provider = "aws"
	}

	return mode, provider
}

// configDirectory returns the directory where the DSO config file will live.
func configDirectory(env *Environment, mode SetupMode) string {
	if mode == ModeAgent || env.User.IsRoot {
		return "/etc/dso"
	}
	home := env.User.HomeDir
	if home == "" {
		home = "/root"
	}
	return filepath.Join(home, ".dso")
}

func configDirMode(mode SetupMode) os.FileMode {
	if mode == ModeAgent {
		return 0750
	}
	return 0700
}

func configDirOwner(mode SetupMode) string {
	if mode == ModeAgent {
		return "root:dso"
	}
	return ""
}

func configFileMode(mode SetupMode) os.FileMode {
	if mode == ModeAgent {
		return 0640
	}
	return 0600
}

// isUpgrade returns true when the ValidationResult contains an existing-config
// info issue for the given config path, indicating an upgrade rather than a
// fresh install.
func isUpgrade(vr *ValidationResult, configPath string) bool {
	if vr == nil {
		return false
	}
	for _, issue := range vr.InCategory(CategoryConfiguration) {
		if issue.Code == CodeExistingInstallationFound &&
			containsPath(issue.Message, configPath) {
			return true
		}
	}
	return false
}

func containsPath(msg, path string) bool {
	return len(path) > 0 && len(msg) > 0 &&
		(msg == path || // exact match unlikely but safe
			len(msg) > len(path) && msg[:len(msg)-len(path)+len(path)] != "" &&
			filepath.Base(msg) == filepath.Base(path))
}

// renderConfigYAML produces the DSO configuration content for the given mode
// and provider. This is a minimal schema; Phase 6 will expand it with full
// provider-specific fields sourced from user input.
func renderConfigYAML(mode SetupMode, provider string) string {
	return fmt.Sprintf(`# DSO configuration — generated by dso setup
# Edit this file or re-run: dso setup
version: "1"
mode: %s
provider: %s
`, mode, provider)
}

// generatePlanID produces a human-readable plan identifier based on the
// current timestamp. It is unique to the millisecond within a single host.
func generatePlanID() string {
	return fmt.Sprintf("plan-%s", time.Now().Format("20060102-150405"))
}

// ─── Summary computation ──────────────────────────────────────────────────────

// computeSummary derives aggregate counts from a fully-built plan.
// Called once at the end of Plan(); preview only renders, never recomputes.
func computeSummary(plan *InstallPlan) PlanSummary {
	s := PlanSummary{}

	for _, d := range plan.Directories {
		s.TotalOperations++
		switch d.Operation {
		case "create":
			s.CreateCount++
		case "modify":
			s.ModifyCount++
		case "delete":
			s.DeleteCount++
		}
	}
	for _, f := range plan.Files {
		s.TotalOperations++
		switch f.Operation {
		case "create":
			s.CreateCount++
		case "modify":
			s.ModifyCount++
		case "delete":
			s.DeleteCount++
		}
	}
	// Permissions are always modifications of existing state.
	for range plan.Permissions {
		s.TotalOperations++
		s.ModifyCount++
	}
	for _, svc := range plan.Services {
		s.TotalOperations++
		switch svc.Operation {
		case "create", "enable", "start":
			s.CreateCount++
		case "stop", "disable":
			s.DeleteCount++
		}
	}
	for _, g := range plan.Groups {
		s.TotalOperations++
		switch g.Operation {
		case "create":
			s.CreateCount++
		case "add-member":
			s.ModifyCount++
		case "delete":
			s.DeleteCount++
		}
	}

	s.RequiresRoot = plan.Mode == ModeAgent || len(plan.Services) > 0 || len(plan.Groups) > 0
	s.EstimatedTime = time.Duration(s.TotalOperations) * 2 * time.Second

	return s
}

// ─── Operation counter ────────────────────────────────────────────────────────

// opCounter issues sequential, type-prefixed IDs for plan operations.
// IDs appear in preview output, logs, and rollback traces.
type opCounter struct {
	dirs     int
	files    int
	services int
	perms    int
	groups   int
}

func (c *opCounter) nextDir() string     { c.dirs++; return fmt.Sprintf("DIR-%03d", c.dirs) }
func (c *opCounter) nextFile() string    { c.files++; return fmt.Sprintf("FILE-%03d", c.files) }
func (c *opCounter) nextService() string { c.services++; return fmt.Sprintf("SERVICE-%03d", c.services) }
func (c *opCounter) nextPerm() string    { c.perms++; return fmt.Sprintf("PERM-%03d", c.perms) }
func (c *opCounter) nextGroup() string   { c.groups++; return fmt.Sprintf("GROUP-%03d", c.groups) }
