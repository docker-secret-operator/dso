package setup

import (
	"context"
	"strings"
	"testing"
)

// ─── computeRecommendation ────────────────────────────────────────────────────

func TestComputeRecommendation_LocalModeWhenAgentNotSupported(t *testing.T) {
	env := &Environment{Capabilities: Capabilities{SupportsAgentMode: false}}
	mode, _ := computeRecommendation(env)
	if mode != ModeLocal {
		t.Errorf("want ModeLocal, got %q", mode)
	}
}

func TestComputeRecommendation_AgentModeWhenSupported(t *testing.T) {
	env := &Environment{Capabilities: Capabilities{SupportsAgentMode: true}}
	mode, _ := computeRecommendation(env)
	if mode != ModeAgent {
		t.Errorf("want ModeAgent, got %q", mode)
	}
}

func TestComputeRecommendation_DefaultProviderIsLocal(t *testing.T) {
	env := &Environment{}
	_, provider := computeRecommendation(env)
	if provider != "local" {
		t.Errorf("want 'local', got %q", provider)
	}
}

func TestComputeRecommendation_AWSBeatsAllOthers(t *testing.T) {
	env := &Environment{
		Providers: DetectedProviders{
			AWS:   AWSInfo{Detected: true},
			Azure: AzureInfo{Detected: true},
			Vault: VaultInfo{Detected: true},
		},
	}
	_, provider := computeRecommendation(env)
	if provider != "aws" {
		t.Errorf("want 'aws', got %q", provider)
	}
}

func TestComputeRecommendation_AzureBeatsVault(t *testing.T) {
	env := &Environment{
		Providers: DetectedProviders{
			Azure: AzureInfo{Detected: true},
			Vault: VaultInfo{Detected: true},
		},
	}
	_, provider := computeRecommendation(env)
	if provider != "azure" {
		t.Errorf("want 'azure', got %q", provider)
	}
}

func TestComputeRecommendation_VaultOnlyProvider(t *testing.T) {
	env := &Environment{
		Providers: DetectedProviders{
			Vault: VaultInfo{Detected: true},
		},
	}
	_, provider := computeRecommendation(env)
	if provider != "vault" {
		t.Errorf("want 'vault', got %q", provider)
	}
}

// ─── opCounter ───────────────────────────────────────────────────────────────

func TestOpCounter_SequentialIDs(t *testing.T) {
	ctr := &opCounter{}

	if got := ctr.nextDir(); got != "DIR-001" {
		t.Errorf("want DIR-001, got %q", got)
	}
	if got := ctr.nextDir(); got != "DIR-002" {
		t.Errorf("want DIR-002, got %q", got)
	}
	if got := ctr.nextFile(); got != "FILE-001" {
		t.Errorf("want FILE-001, got %q", got)
	}
	if got := ctr.nextFile(); got != "FILE-002" {
		t.Errorf("want FILE-002, got %q", got)
	}
	if got := ctr.nextService(); got != "SERVICE-001" {
		t.Errorf("want SERVICE-001, got %q", got)
	}
	if got := ctr.nextService(); got != "SERVICE-002" {
		t.Errorf("want SERVICE-002, got %q", got)
	}
	if got := ctr.nextPerm(); got != "PERM-001" {
		t.Errorf("want PERM-001, got %q", got)
	}
	if got := ctr.nextGroup(); got != "GROUP-001" {
		t.Errorf("want GROUP-001, got %q", got)
	}
}

// ─── Planner.Plan — local mode ────────────────────────────────────────────────

func TestPlanner_LocalMode_HasExactlyOneDirAndOneFile(t *testing.T) {
	p := newPlanner()
	plan, err := p.Plan(context.Background(), &Environment{}, &ValidationResult{}, SetupOptions{
		Mode:     ModeLocal,
		Provider: "local",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Directories) != 1 {
		t.Errorf("want 1 directory op, got %d", len(plan.Directories))
	}
	if len(plan.Files) != 1 {
		t.Errorf("want 1 file op, got %d", len(plan.Files))
	}
}

func TestPlanner_LocalMode_NoServicesOrGroups(t *testing.T) {
	p := newPlanner()
	plan, _ := p.Plan(context.Background(), &Environment{}, &ValidationResult{}, SetupOptions{
		Mode:     ModeLocal,
		Provider: "local",
	})
	if len(plan.Services) != 0 {
		t.Errorf("want no service ops in local mode, got %d", len(plan.Services))
	}
	if len(plan.Groups) != 0 {
		t.Errorf("want no group ops in local mode, got %d", len(plan.Groups))
	}
}

func TestPlanner_LocalMode_DirIDisDIR001(t *testing.T) {
	p := newPlanner()
	plan, _ := p.Plan(context.Background(), &Environment{}, &ValidationResult{}, SetupOptions{
		Mode:     ModeLocal,
		Provider: "local",
	})
	if plan.Directories[0].ID != "DIR-001" {
		t.Errorf("want DIR-001, got %q", plan.Directories[0].ID)
	}
}

func TestPlanner_LocalMode_FileIDisFILE001(t *testing.T) {
	p := newPlanner()
	plan, _ := p.Plan(context.Background(), &Environment{}, &ValidationResult{}, SetupOptions{
		Mode:     ModeLocal,
		Provider: "local",
	})
	if plan.Files[0].ID != "FILE-001" {
		t.Errorf("want FILE-001, got %q", plan.Files[0].ID)
	}
}

func TestPlanner_LocalMode_NonRootConfigDir(t *testing.T) {
	p := newPlanner()
	env := &Environment{User: UserInfo{HomeDir: "/home/alice", IsRoot: false}}
	plan, _ := p.Plan(context.Background(), env, &ValidationResult{}, SetupOptions{
		Mode:     ModeLocal,
		Provider: "local",
	})
	wantDir := "/home/alice/.dso"
	if plan.Directories[0].Path != wantDir {
		t.Errorf("config dir: want %q, got %q", wantDir, plan.Directories[0].Path)
	}
	if !strings.HasPrefix(plan.Files[0].Path, wantDir) {
		t.Errorf("config file path should be under %q, got %q", wantDir, plan.Files[0].Path)
	}
}

func TestPlanner_LocalMode_ConfigYAMLContainsModeAndProvider(t *testing.T) {
	p := newPlanner()
	plan, _ := p.Plan(context.Background(), &Environment{}, &ValidationResult{}, SetupOptions{
		Mode:     ModeLocal,
		Provider: "aws",
	})
	if !strings.Contains(plan.ConfigYAML, "mode: local") {
		t.Errorf("ConfigYAML should contain 'mode: local', got:\n%s", plan.ConfigYAML)
	}
	if !strings.Contains(plan.ConfigYAML, "provider: aws") {
		t.Errorf("ConfigYAML should contain 'provider: aws', got:\n%s", plan.ConfigYAML)
	}
}

func TestPlanner_LocalMode_FileOperationIsCreate(t *testing.T) {
	p := newPlanner()
	plan, _ := p.Plan(context.Background(), &Environment{}, &ValidationResult{}, SetupOptions{
		Mode:     ModeLocal,
		Provider: "local",
	})
	if plan.Files[0].Operation != "create" {
		t.Errorf("want 'create', got %q", plan.Files[0].Operation)
	}
}

// ─── Planner.Plan — agent mode ────────────────────────────────────────────────

func TestPlanner_AgentMode_HasServiceOps(t *testing.T) {
	p := newPlanner()
	plan, err := p.Plan(context.Background(), &Environment{User: UserInfo{IsRoot: true}}, &ValidationResult{}, SetupOptions{
		Mode:     ModeAgent,
		Provider: "local",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// service unit file + enable + start
	if len(plan.Services) < 2 {
		t.Errorf("want at least 2 service ops (enable+start), got %d", len(plan.Services))
	}
}

func TestPlanner_AgentMode_ServiceIDsAreSequential(t *testing.T) {
	p := newPlanner()
	plan, _ := p.Plan(context.Background(), &Environment{}, &ValidationResult{}, SetupOptions{
		Mode:     ModeAgent,
		Provider: "local",
	})
	if plan.Services[0].ID != "SERVICE-001" {
		t.Errorf("want SERVICE-001, got %q", plan.Services[0].ID)
	}
	if plan.Services[1].ID != "SERVICE-002" {
		t.Errorf("want SERVICE-002, got %q", plan.Services[1].ID)
	}
}

func TestPlanner_AgentMode_ServiceFileIsInFiles(t *testing.T) {
	p := newPlanner()
	plan, _ := p.Plan(context.Background(), &Environment{}, &ValidationResult{}, SetupOptions{
		Mode:     ModeAgent,
		Provider: "local",
	})
	found := false
	for _, f := range plan.Files {
		if strings.Contains(f.Path, "systemd") || strings.HasSuffix(f.Path, ".service") {
			found = true
		}
	}
	if !found {
		t.Error("expected a systemd unit file in plan.Files for agent mode")
	}
}

func TestPlanner_AgentMode_RootCreatesDockerGroup(t *testing.T) {
	p := newPlanner()
	plan, _ := p.Plan(context.Background(), &Environment{User: UserInfo{IsRoot: true}}, &ValidationResult{}, SetupOptions{
		Mode:     ModeAgent,
		Provider: "local",
	})
	if len(plan.Groups) == 0 {
		t.Error("expected a group op when running as root in agent mode")
	}
	if plan.Groups[0].Name != "dso" {
		t.Errorf("want group 'dso', got %q", plan.Groups[0].Name)
	}
}

func TestPlanner_AgentMode_NonRootSkipsGroup(t *testing.T) {
	p := newPlanner()
	plan, _ := p.Plan(context.Background(), &Environment{User: UserInfo{IsRoot: false}}, &ValidationResult{}, SetupOptions{
		Mode:     ModeAgent,
		Provider: "local",
	})
	if len(plan.Groups) != 0 {
		t.Errorf("want no group ops for non-root agent mode, got %d", len(plan.Groups))
	}
}

func TestPlanner_AgentMode_ConfigDirIsEtcDso(t *testing.T) {
	p := newPlanner()
	plan, _ := p.Plan(context.Background(), &Environment{}, &ValidationResult{}, SetupOptions{
		Mode:     ModeAgent,
		Provider: "local",
	})
	if plan.Directories[0].Path != "/etc/dso" {
		t.Errorf("want '/etc/dso', got %q", plan.Directories[0].Path)
	}
}

// ─── Planner.Plan — metadata and identity ────────────────────────────────────

func TestPlanner_PlanHasNonEmptyID(t *testing.T) {
	p := newPlanner()
	plan, _ := p.Plan(context.Background(), &Environment{}, &ValidationResult{}, SetupOptions{
		Mode:     ModeLocal,
		Provider: "local",
	})
	if plan.ID == "" {
		t.Error("expected non-empty plan ID")
	}
}

func TestPlanner_PlanIDHasPlanPrefix(t *testing.T) {
	p := newPlanner()
	plan, _ := p.Plan(context.Background(), &Environment{}, &ValidationResult{}, SetupOptions{
		Mode:     ModeLocal,
		Provider: "local",
	})
	if !strings.HasPrefix(plan.ID, "plan-") {
		t.Errorf("want plan ID to start with 'plan-', got %q", plan.ID)
	}
}

func TestPlanner_PlanTimestampIsSet(t *testing.T) {
	p := newPlanner()
	plan, _ := p.Plan(context.Background(), &Environment{}, &ValidationResult{}, SetupOptions{
		Mode:     ModeLocal,
		Provider: "local",
	})
	if plan.Timestamp.IsZero() {
		t.Error("expected plan Timestamp to be set")
	}
}

func TestPlanner_PlanModeAndProviderPropagated(t *testing.T) {
	p := newPlanner()
	plan, _ := p.Plan(context.Background(), &Environment{}, &ValidationResult{}, SetupOptions{
		Mode:     ModeAgent,
		Provider: "vault",
		DryRun:   true,
	})
	if plan.Mode != ModeAgent {
		t.Errorf("mode: want 'agent', got %q", plan.Mode)
	}
	if plan.Provider != "vault" {
		t.Errorf("provider: want 'vault', got %q", plan.Provider)
	}
	if !plan.DryRun {
		t.Error("DryRun flag should propagate to plan")
	}
}

// ─── Planner.Plan — mode/provider fallback ───────────────────────────────────

func TestPlanner_FallsBackToCapabilitiesWhenModeEmpty(t *testing.T) {
	p := newPlanner()
	env := &Environment{
		Capabilities: Capabilities{SupportsAgentMode: false, SupportsLocalMode: true},
	}
	plan, _ := p.Plan(context.Background(), env, &ValidationResult{}, SetupOptions{})
	if plan.Mode != ModeLocal {
		t.Errorf("want ModeLocal from capabilities, got %q", plan.Mode)
	}
}

func TestPlanner_FallsBackToDetectedProviderWhenProviderEmpty(t *testing.T) {
	p := newPlanner()
	env := &Environment{
		Providers: DetectedProviders{AWS: AWSInfo{Detected: true}},
	}
	plan, _ := p.Plan(context.Background(), env, &ValidationResult{}, SetupOptions{})
	if plan.Provider != "aws" {
		t.Errorf("want 'aws' from detected providers, got %q", plan.Provider)
	}
}

func TestPlanner_ExplicitOptsOverrideCapabilities(t *testing.T) {
	p := newPlanner()
	env := &Environment{
		Capabilities: Capabilities{SupportsAgentMode: true},
		Providers:    DetectedProviders{AWS: AWSInfo{Detected: true}},
	}
	plan, _ := p.Plan(context.Background(), env, &ValidationResult{}, SetupOptions{
		Mode:     ModeLocal,
		Provider: "vault",
	})
	if plan.Mode != ModeLocal {
		t.Errorf("explicit mode should win: want 'local', got %q", plan.Mode)
	}
	if plan.Provider != "vault" {
		t.Errorf("explicit provider should win: want 'vault', got %q", plan.Provider)
	}
}

// ─── Planner.Plan — upgrade detection ────────────────────────────────────────

func TestPlanner_ExistingInstallationProducesModifyOperation(t *testing.T) {
	p := newPlanner()
	env := &Environment{User: UserInfo{HomeDir: "/home/alice"}}

	configPath := "/home/alice/.dso/dso.yaml"
	vr := &ValidationResult{
		Issues: []ValidationIssue{
			{
				Severity: SeverityInfo,
				Category: CategoryConfiguration,
				Code:     CodeExistingInstallationFound,
				Message:  configPath,
			},
		},
	}

	plan, _ := p.Plan(context.Background(), env, vr, SetupOptions{
		Mode:     ModeLocal,
		Provider: "local",
	})

	if plan.Files[0].Operation != "modify" {
		t.Errorf("want 'modify' for existing install, got %q", plan.Files[0].Operation)
	}
}
