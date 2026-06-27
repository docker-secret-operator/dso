package setup

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// ─── Test fixtures ────────────────────────────────────────────────────────────

func localPlanFixture() InstallPlan {
	return InstallPlan{
		ID:        "plan-20260101-120000",
		Version:   1,
		Timestamp: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		Mode:      ModeLocal,
		Provider:  "local",
		DryRun:    true,
		Directories: []DirectoryChange{
			{ID: "DIR-001", Path: "/home/alice/.dso", Mode: 0700, Operation: "create"},
		},
		Files: []FileChange{
			{ID: "FILE-001", Path: "/home/alice/.dso/dso.yaml", Mode: 0600, Content: []byte("version: \"1\"\n"), Operation: "create"},
		},
		Summary: PlanSummary{
			TotalOperations: 2,
			CreateCount:     2,
			RequiresRoot:    false,
			EstimatedTime:   4 * time.Second,
		},
		Metadata: map[string]string{"schema": "v1"},
	}
}

func agentPlanFixture() InstallPlan {
	return InstallPlan{
		ID:        "plan-20260101-130000",
		Version:   1,
		Timestamp: time.Date(2026, 1, 1, 13, 0, 0, 0, time.UTC),
		Mode:      ModeAgent,
		Provider:  "aws",
		DryRun:    false,
		Directories: []DirectoryChange{
			{ID: "DIR-001", Path: "/etc/dso", Mode: 0750, Owner: "root:dso", Operation: "create"},
		},
		Files: []FileChange{
			{ID: "FILE-001", Path: "/etc/dso/dso.yaml", Mode: 0640, Owner: "root:dso", Operation: "create"},
			{ID: "FILE-002", Path: "/etc/systemd/system/dso-agent.service", Mode: 0644, Owner: "root:root", Content: []byte("[Unit]\nDescription=DSO\n"), Operation: "create"},
		},
		Permissions: []PermissionChange{
			{ID: "PERM-001", Path: "/etc/dso", Current: 0755, Target: 0750, Owner: "root:dso"},
		},
		Services: []ServiceChange{
			{ID: "SERVICE-001", Name: "dso-agent.service", Operation: "enable"},
			{ID: "SERVICE-002", Name: "dso-agent.service", Operation: "start"},
		},
		Groups: []GroupChange{
			{ID: "GROUP-001", Name: "dso", Operation: "create"},
		},
		Summary: PlanSummary{
			TotalOperations: 7,
			CreateCount:     6,
			ModifyCount:     1,
			RequiresRoot:    true,
			EstimatedTime:   14 * time.Second,
		},
	}
}

// ─── TerminalRenderer ─────────────────────────────────────────────────────────

func TestTerminalRenderer_EmptyPlan_DoesNotPanic(t *testing.T) {
	r := &TerminalRenderer{}
	out, err := r.Render(InstallPlan{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == "" {
		t.Error("expected non-empty output even for empty plan")
	}
}

func TestTerminalRenderer_ContainsDSOSetupPlanHeader(t *testing.T) {
	r := &TerminalRenderer{}
	out, _ := r.Render(localPlanFixture())
	if !strings.Contains(out, "DSO Setup Plan") {
		t.Errorf("expected 'DSO Setup Plan' header, got:\n%s", out)
	}
}

func TestTerminalRenderer_ContainsPlanID(t *testing.T) {
	r := &TerminalRenderer{}
	out, _ := r.Render(localPlanFixture())
	if !strings.Contains(out, "plan-20260101-120000") {
		t.Errorf("expected plan ID in output, got:\n%s", out)
	}
}

func TestTerminalRenderer_ContainsMode(t *testing.T) {
	r := &TerminalRenderer{}
	out, _ := r.Render(localPlanFixture())
	if !strings.Contains(out, "local") {
		t.Errorf("expected mode 'local' in output, got:\n%s", out)
	}
}

func TestTerminalRenderer_ContainsProvider(t *testing.T) {
	r := &TerminalRenderer{}
	out, _ := r.Render(agentPlanFixture())
	if !strings.Contains(out, "aws") {
		t.Errorf("expected provider 'aws' in output, got:\n%s", out)
	}
}

func TestTerminalRenderer_ContainsDirID(t *testing.T) {
	r := &TerminalRenderer{}
	out, _ := r.Render(localPlanFixture())
	if !strings.Contains(out, "DIR-001") {
		t.Errorf("expected DIR-001 in output, got:\n%s", out)
	}
}

func TestTerminalRenderer_ContainsDirPath(t *testing.T) {
	r := &TerminalRenderer{}
	out, _ := r.Render(localPlanFixture())
	if !strings.Contains(out, "/home/alice/.dso") {
		t.Errorf("expected dir path in output, got:\n%s", out)
	}
}

func TestTerminalRenderer_ContainsFileID(t *testing.T) {
	r := &TerminalRenderer{}
	out, _ := r.Render(localPlanFixture())
	if !strings.Contains(out, "FILE-001") {
		t.Errorf("expected FILE-001 in output, got:\n%s", out)
	}
}

func TestTerminalRenderer_ContainsFilePath(t *testing.T) {
	r := &TerminalRenderer{}
	out, _ := r.Render(localPlanFixture())
	if !strings.Contains(out, "dso.yaml") {
		t.Errorf("expected dso.yaml in output, got:\n%s", out)
	}
}

func TestTerminalRenderer_CreatePrefixIsPlus(t *testing.T) {
	r := &TerminalRenderer{}
	out, _ := r.Render(localPlanFixture())
	if !strings.Contains(out, "+ DIR-001") {
		t.Errorf("expected '+ DIR-001' (create prefix), got:\n%s", out)
	}
}

func TestTerminalRenderer_ModifyPrefixIsTilde(t *testing.T) {
	r := &TerminalRenderer{}
	plan := InstallPlan{
		Permissions: []PermissionChange{
			{ID: "PERM-001", Path: "/etc/dso", Current: 0755, Target: 0750},
		},
	}
	out, _ := r.Render(plan)
	if !strings.Contains(out, "~ PERM-001") {
		t.Errorf("expected '~ PERM-001' (modify prefix), got:\n%s", out)
	}
}

func TestTerminalRenderer_DeletePrefixIsMinus(t *testing.T) {
	r := &TerminalRenderer{}
	plan := InstallPlan{
		Services: []ServiceChange{
			{ID: "SERVICE-001", Name: "dso-agent.service", Operation: "stop"},
		},
	}
	out, _ := r.Render(plan)
	if !strings.Contains(out, "- SERVICE-001") {
		t.Errorf("expected '- SERVICE-001' (delete prefix), got:\n%s", out)
	}
}

func TestTerminalRenderer_DryRunFooterWhenDryRunTrue(t *testing.T) {
	r := &TerminalRenderer{}
	plan := localPlanFixture()
	plan.DryRun = true
	out, _ := r.Render(plan)
	if !strings.Contains(out, "No changes have been applied") {
		t.Errorf("expected dry-run footer, got:\n%s", out)
	}
}

func TestTerminalRenderer_NoDryRunFooterWhenDryRunFalse(t *testing.T) {
	r := &TerminalRenderer{}
	plan := localPlanFixture()
	plan.DryRun = false
	out, _ := r.Render(plan)
	if strings.Contains(out, "No changes have been applied") {
		t.Errorf("dry-run footer must not appear when DryRun=false, got:\n%s", out)
	}
}

func TestTerminalRenderer_AgentMode_ContainsServiceID(t *testing.T) {
	r := &TerminalRenderer{}
	out, _ := r.Render(agentPlanFixture())
	if !strings.Contains(out, "SERVICE-001") {
		t.Errorf("expected SERVICE-001 in output, got:\n%s", out)
	}
}

func TestTerminalRenderer_AgentMode_ContainsGroupID(t *testing.T) {
	r := &TerminalRenderer{}
	out, _ := r.Render(agentPlanFixture())
	if !strings.Contains(out, "GROUP-001") {
		t.Errorf("expected GROUP-001 in output, got:\n%s", out)
	}
}

func TestTerminalRenderer_AgentMode_ContainsPermissionID(t *testing.T) {
	r := &TerminalRenderer{}
	out, _ := r.Render(agentPlanFixture())
	if !strings.Contains(out, "PERM-001") {
		t.Errorf("expected PERM-001 in output, got:\n%s", out)
	}
}

func TestTerminalRenderer_SummaryShowsOperationCounts(t *testing.T) {
	r := &TerminalRenderer{}
	out, _ := r.Render(localPlanFixture())
	if !strings.Contains(out, "Summary") {
		t.Errorf("expected 'Summary' section, got:\n%s", out)
	}
}

func TestTerminalRenderer_RequiresRootShownForAgentPlan(t *testing.T) {
	r := &TerminalRenderer{}
	out, _ := r.Render(agentPlanFixture())
	if !strings.Contains(out, "root") {
		t.Errorf("expected 'root' in output for agent plan, got:\n%s", out)
	}
}

func TestTerminalRenderer_RequiresRootNotShownForLocalPlan(t *testing.T) {
	r := &TerminalRenderer{}
	out, _ := r.Render(localPlanFixture())
	if strings.Contains(out, "Requires:") {
		t.Errorf("should not show 'Requires:' for non-root local plan, got:\n%s", out)
	}
}

func TestTerminalRenderer_TimestampIncluded(t *testing.T) {
	r := &TerminalRenderer{}
	out, _ := r.Render(localPlanFixture())
	if !strings.Contains(out, "2026-01-01") {
		t.Errorf("expected timestamp in output, got:\n%s", out)
	}
}

func TestTerminalRenderer_IsDeterministic(t *testing.T) {
	r := &TerminalRenderer{}
	plan := agentPlanFixture()
	out1, _ := r.Render(plan)
	out2, _ := r.Render(plan)
	if out1 != out2 {
		t.Error("terminal renderer must be deterministic")
	}
}

func TestTerminalRenderer_NoEnvironmentInSignature(t *testing.T) {
	// Compile-time contract check: Render accepts only InstallPlan.
	// If this compiles, the interface is correct.
	r := &TerminalRenderer{}
	_, err := r.Render(InstallPlan{Mode: ModeLocal})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTerminalRenderer_FileSizePresentWhenContentNonEmpty(t *testing.T) {
	r := &TerminalRenderer{}
	out, _ := r.Render(localPlanFixture())
	if !strings.Contains(out, "bytes") {
		t.Errorf("expected file size in output for non-empty content, got:\n%s", out)
	}
}

func TestTerminalRenderer_OwnerPresentInAgentPlan(t *testing.T) {
	r := &TerminalRenderer{}
	out, _ := r.Render(agentPlanFixture())
	if !strings.Contains(out, "root:dso") {
		t.Errorf("expected owner 'root:dso' in output, got:\n%s", out)
	}
}

// ─── JSONRenderer ─────────────────────────────────────────────────────────────

func TestJSONRenderer_ProducesValidJSON(t *testing.T) {
	r := &JSONRenderer{}
	out, err := r.Render(localPlanFixture())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var v interface{}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
}

func TestJSONRenderer_TopLevelPlanKey(t *testing.T) {
	r := &JSONRenderer{}
	out, _ := r.Render(localPlanFixture())
	var doc map[string]interface{}
	_ = json.Unmarshal([]byte(out), &doc)
	if _, ok := doc["plan"]; !ok {
		t.Errorf("expected top-level 'plan' key, got keys: %v", doc)
	}
}

func TestJSONRenderer_ContainsPlanID(t *testing.T) {
	r := &JSONRenderer{}
	out, _ := r.Render(localPlanFixture())
	if !strings.Contains(out, "plan-20260101-120000") {
		t.Errorf("expected plan ID in JSON, got:\n%s", out)
	}
}

func TestJSONRenderer_ModeIsString(t *testing.T) {
	r := &JSONRenderer{}
	out, _ := r.Render(localPlanFixture())
	if !strings.Contains(out, `"mode": "local"`) {
		t.Errorf("expected mode as string, got:\n%s", out)
	}
}

func TestJSONRenderer_ProviderIsPresent(t *testing.T) {
	r := &JSONRenderer{}
	out, _ := r.Render(localPlanFixture())
	if !strings.Contains(out, `"provider"`) {
		t.Errorf("expected 'provider' key in JSON, got:\n%s", out)
	}
}

func TestJSONRenderer_DirectoriesArrayPresent(t *testing.T) {
	r := &JSONRenderer{}
	out, _ := r.Render(localPlanFixture())
	if !strings.Contains(out, `"directories"`) {
		t.Errorf("expected 'directories' array in JSON, got:\n%s", out)
	}
}

func TestJSONRenderer_FilesArrayPresent(t *testing.T) {
	r := &JSONRenderer{}
	out, _ := r.Render(localPlanFixture())
	if !strings.Contains(out, `"files"`) {
		t.Errorf("expected 'files' array in JSON, got:\n%s", out)
	}
}

func TestJSONRenderer_SummaryKeyPresent(t *testing.T) {
	r := &JSONRenderer{}
	out, _ := r.Render(localPlanFixture())
	if !strings.Contains(out, `"summary"`) {
		t.Errorf("expected 'summary' key in JSON, got:\n%s", out)
	}
}

func TestJSONRenderer_EmptyPlan_ArraysAreEmptyNotNull(t *testing.T) {
	r := &JSONRenderer{}
	out, err := r.Render(InstallPlan{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, field := range []string{"directories", "files", "permissions", "services", "groups"} {
		if strings.Contains(out, `"`+field+`": null`) {
			t.Errorf("%s should be [] not null in empty plan JSON, got:\n%s", field, out)
		}
	}
}

func TestJSONRenderer_GeneratedAtIsISO8601(t *testing.T) {
	r := &JSONRenderer{}
	out, _ := r.Render(localPlanFixture())
	if !strings.Contains(out, "2026-01-01T12:00:00Z") {
		t.Errorf("expected ISO-8601 generated_at, got:\n%s", out)
	}
}

func TestJSONRenderer_AgentMode_ServicesPresent(t *testing.T) {
	r := &JSONRenderer{}
	out, _ := r.Render(agentPlanFixture())
	if !strings.Contains(out, "SERVICE-001") {
		t.Errorf("expected SERVICE-001 in JSON, got:\n%s", out)
	}
}

func TestJSONRenderer_AgentMode_GroupsPresent(t *testing.T) {
	r := &JSONRenderer{}
	out, _ := r.Render(agentPlanFixture())
	if !strings.Contains(out, "GROUP-001") {
		t.Errorf("expected GROUP-001 in JSON, got:\n%s", out)
	}
}

func TestJSONRenderer_AgentMode_PermissionsPresent(t *testing.T) {
	r := &JSONRenderer{}
	out, _ := r.Render(agentPlanFixture())
	if !strings.Contains(out, "PERM-001") {
		t.Errorf("expected PERM-001 in JSON, got:\n%s", out)
	}
}

func TestJSONRenderer_SummaryTotalOperations(t *testing.T) {
	r := &JSONRenderer{}
	out, _ := r.Render(localPlanFixture())
	if !strings.Contains(out, `"total_operations"`) {
		t.Errorf("expected 'total_operations' in JSON summary, got:\n%s", out)
	}
}

func TestJSONRenderer_DryRunFieldPresent(t *testing.T) {
	r := &JSONRenderer{}
	out, _ := r.Render(localPlanFixture())
	if !strings.Contains(out, `"dry_run"`) {
		t.Errorf("expected 'dry_run' field in JSON, got:\n%s", out)
	}
}

func TestJSONRenderer_ModeOctalInDirectories(t *testing.T) {
	r := &JSONRenderer{}
	out, _ := r.Render(localPlanFixture())
	// 0700 → "0700"
	if !strings.Contains(out, `"0700"`) {
		t.Errorf("expected octal mode '0700' as string in JSON, got:\n%s", out)
	}
}

func TestJSONRenderer_IsDeterministic(t *testing.T) {
	r := &JSONRenderer{}
	plan := agentPlanFixture()
	out1, _ := r.Render(plan)
	out2, _ := r.Render(plan)
	if out1 != out2 {
		t.Error("JSON renderer must be deterministic")
	}
}

func TestJSONRenderer_VersionField(t *testing.T) {
	r := &JSONRenderer{}
	out, _ := r.Render(localPlanFixture())
	if !strings.Contains(out, `"version": 1`) {
		t.Errorf("expected version=1 in JSON, got:\n%s", out)
	}
}

// ─── newRenderer ─────────────────────────────────────────────────────────────

func TestNewRenderer_EmptyStringGivesTerminal(t *testing.T) {
	if _, ok := newRenderer("").(*TerminalRenderer); !ok {
		t.Error("expected TerminalRenderer for empty format string")
	}
}

func TestNewRenderer_TerminalStringGivesTerminal(t *testing.T) {
	if _, ok := newRenderer("terminal").(*TerminalRenderer); !ok {
		t.Error("expected TerminalRenderer for 'terminal'")
	}
}

func TestNewRenderer_JSONStringGivesJSON(t *testing.T) {
	if _, ok := newRenderer("json").(*JSONRenderer); !ok {
		t.Error("expected JSONRenderer for 'json'")
	}
}

func TestNewRenderer_UnknownStringFallsBackToTerminal(t *testing.T) {
	if _, ok := newRenderer("html").(*TerminalRenderer); !ok {
		t.Error("unknown format should fall back to TerminalRenderer")
	}
}

// ─── PreviewEngine ────────────────────────────────────────────────────────────

func TestPreviewEngine_DelegatesTerminalRender(t *testing.T) {
	pe := NewPreviewEngine(&TerminalRenderer{})
	out, err := pe.Render(localPlanFixture())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "DSO Setup Plan") {
		t.Errorf("expected terminal header, got:\n%s", out)
	}
}

func TestPreviewEngine_DelegatesJSONRender(t *testing.T) {
	pe := NewPreviewEngine(&JSONRenderer{})
	out, err := pe.Render(localPlanFixture())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var v interface{}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("PreviewEngine with JSONRenderer produced invalid JSON: %v", err)
	}
}

func TestPreviewEngine_DoesNotModifyPlan(t *testing.T) {
	pe := NewPreviewEngine(&TerminalRenderer{})
	plan := localPlanFixture()
	idBefore := plan.ID
	_, _ = pe.Render(plan)
	if plan.ID != idBefore {
		t.Error("PreviewEngine must not mutate the InstallPlan")
	}
}

// ─── opPrefix ─────────────────────────────────────────────────────────────────

func TestOpPrefix_Create(t *testing.T) {
	if got := opPrefix("create"); got != "+" {
		t.Errorf("want '+', got %q", got)
	}
}

func TestOpPrefix_Enable(t *testing.T) {
	if got := opPrefix("enable"); got != "+" {
		t.Errorf("want '+', got %q", got)
	}
}

func TestOpPrefix_Start(t *testing.T) {
	if got := opPrefix("start"); got != "+" {
		t.Errorf("want '+', got %q", got)
	}
}

func TestOpPrefix_Modify(t *testing.T) {
	if got := opPrefix("modify"); got != "~" {
		t.Errorf("want '~', got %q", got)
	}
}

func TestOpPrefix_Delete(t *testing.T) {
	if got := opPrefix("delete"); got != "-" {
		t.Errorf("want '-', got %q", got)
	}
}

func TestOpPrefix_Stop(t *testing.T) {
	if got := opPrefix("stop"); got != "-" {
		t.Errorf("want '-', got %q", got)
	}
}

func TestOpPrefix_Unknown(t *testing.T) {
	if got := opPrefix("frobnicate"); got != "+" {
		t.Errorf("unknown op should default to '+', got %q", got)
	}
}
