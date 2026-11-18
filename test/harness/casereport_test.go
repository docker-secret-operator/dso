package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteCaseReport_PersistsEachCaseImmediately(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "report")
	pass := CaseReport{ID: "restart/restart-after-intent-start", Strategy: Restart, Point: "restart:after_intent_start",
		FaultFired: true, PIDVerified: true, CrashJournal: "intent_start_replacement/in_progress",
		FinalStatus: "critical_error", FinalOutcome: "rolled_back", Duration: 41 * time.Second}
	fail := CaseReport{ID: "rolling/state-after-persist-rename#4", FaultFired: true, PIDVerified: true,
		Violations: []Violation{{Rule: "service-reachable", Detail: "x"}}}
	unreached := CaseReport{ID: "signal/signal-during-readiness", NotExecuted: "point not reached within 2m30s"}

	// Each write is visible on disk before the next case starts: a timeout after case N
	// leaves cases 1..N intact.
	for i, c := range []CaseReport{pass, fail, unreached} {
		if err := WriteCaseReport(dir, c); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, CaseFileName(c.ID))); err != nil {
			t.Fatalf("case %d not on disk right after its write: %v", i, err)
		}
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(matches) != 0 {
		t.Fatalf("temp files left behind: %v", matches)
	}
	var got CaseReport
	b, _ := os.ReadFile(filepath.Join(dir, CaseFileName(pass.ID)))
	if err := json.Unmarshal(b, &got); err != nil || got.CrashJournal != pass.CrashJournal || got.ID != pass.ID {
		t.Fatalf("round trip failed: %v %+v", err, got)
	}
	idx, _ := os.ReadFile(filepath.Join(dir, "index.tsv"))
	lines := strings.Split(strings.TrimSpace(string(idx)), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], "\tPASS\t") || !strings.Contains(lines[1], "\tFAIL\t") || !strings.Contains(lines[2], "\tNOT-EXECUTED\t") {
		t.Fatalf("index statuses wrong:\n%s", idx)
	}
}

func TestCaseFileName_IsASinglePathElement(t *testing.T) {
	for _, id := range []string{"restart/state-after-persist-rename#4", "rolling/recovery-start", "signal/signal-after-spec-0-published"} {
		n := CaseFileName(id)
		if strings.ContainsAny(n, "/#") || filepath.Base(n) != n {
			t.Errorf("%q -> %q is not a single safe path element", id, n)
		}
	}
	if CaseFileName("restart/x#4") == CaseFileName("restart/x#5") {
		t.Error("distinct Nth-hit cases must not collide")
	}
}

func TestCaseStatus_UnreachedIsNeverAPass(t *testing.T) {
	if got := CaseStatus(CaseReport{ID: "x", NotExecuted: "never reached", FaultFired: true, PIDVerified: true}); got != "NOT-EXECUTED" {
		t.Fatalf("an unreached case reported %q", got)
	}
	if got := CaseStatus(CaseReport{ID: "x", FaultFired: true, PIDVerified: true}); got != "PASS" {
		t.Fatalf("a clean executed case reported %q", got)
	}
	if got := CaseStatus(CaseReport{ID: "x", FaultFired: true}); got != "FAIL" {
		t.Fatalf("an unverified pid reported %q", got)
	}
}

func TestLayoutOf_ShowsTheServiceAndItsArtifactsOnly(t *testing.T) {
	s := Snapshot{Containers: []CView{
		{ID: "aaaaaaaaaaaaaaaa", Name: "svc", Running: true},
		{ID: "bbbbbbbbbbbbbbbb", Name: "svc_dso_backup_1700", Running: false},
		{ID: "cccccccccccccccc", Name: "svc_dso_new_1700", Running: true},
		{ID: "dddddddddddddddd", Name: "svc_old_1699", Running: false},
		{ID: "eeeeeeeeeeeeeeee", Name: "other", Running: true},
		{ID: "ffffffffffffffff", Name: "other_dso_backup_1", Running: true},
	}}
	got := LayoutOf(s, "svc")
	want := []string{"svc aaaaaaaaaaaa running", "svc_dso_backup_1700 bbbbbbbbbbbb stopped", "svc_dso_new_1700 cccccccccccc running", "svc_old_1699 dddddddddddd stopped"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("layout = %v, want %v", got, want)
	}
}

func TestCaseLogLine_IsOneLineOfValidJSONWithThePrefix(t *testing.T) {
	c := CaseReport{ID: "signal/signal-after-spec-0-published", FaultFired: true, PIDVerified: true,
		CrashJournal: "intent_signal_publish/in_progress", FinalStatus: "critical_error",
		CrashLayout: []string{"svc aaaaaaaaaaaa running"}, SecondPass: "unchanged",
		Notes: []string{"line one\nstill one JSON line"}}
	line := CaseLogLine(c)
	if !strings.HasPrefix(line, CaseLogPrefix) || strings.ContainsAny(line, "\n\r") {
		t.Fatalf("must be a single prefixed line, got %q", line)
	}
	var back CaseReport
	if err := json.Unmarshal([]byte(strings.TrimPrefix(line, CaseLogPrefix)), &back); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if back.ID != c.ID || back.CrashJournal != c.CrashJournal || back.SecondPass != "unchanged" || len(back.CrashLayout) != 1 {
		t.Fatalf("round trip lost evidence: %+v", back)
	}
}
