package cli

import (
	"bytes"
	"encoding/json"
	"github.com/docker-secret-operator/dso/internal/agent"
	"github.com/docker-secret-operator/dso/internal/setup"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/internal/audit"
)

func runAuditVerify(t *testing.T, args ...string) (string, int) {
	t.Helper()
	cmd := NewAuditCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetArgs(append([]string{"verify"}, args...))
	err := cmd.Execute()
	code := 0
	if err != nil {
		code = exitCodeFor(err)
	}
	return out.String(), code
}

func writeAudit(t *testing.T, n int) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "audit.jsonl")
	d, err := audit.OpenDurable(p)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		if err := d.Append("evt", "operator", "", "s", "", "success"); err != nil {
			t.Fatal(err)
		}
	}
	_ = d.Close()
	return p
}

func TestAuditVerify_VerifiedAndEmpty(t *testing.T) {
	out, code := runAuditVerify(t, "--file", writeAudit(t, 3))
	if code != 0 || !strings.Contains(out, "VERIFIED (3 records)") || !strings.Contains(out, "unkeyed") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if _, code := runAuditVerify(t, "--file", writeAudit(t, 0)); code != 0 {
		t.Fatalf("empty file must verify, code=%d", code)
	}
}

func TestAuditVerify_TamperIsExit10_MissingIsExit11_JSONStable(t *testing.T) {
	p := writeAudit(t, 3)
	b, _ := os.ReadFile(p)
	_ = os.WriteFile(p, []byte(strings.Replace(string(b), `"user":"operator"`, `"user":"x"`, 1)), 0o600)
	out, code := runAuditVerify(t, "--file", p, "--json")
	var doc auditFileVerifyJSON
	if err := json.Unmarshal([]byte(out), &doc); err != nil || code != exitAuditIntegrityFailure || doc.Status != "integrity_failure" {
		t.Fatalf("code=%d doc=%+v err=%v", code, doc, err)
	}
	out, code = runAuditVerify(t, "--file", filepath.Join(t.TempDir(), "nope"), "--json")
	if json.Unmarshal([]byte(out), &doc) != nil || code != exitAuditUnavailable || doc.Status != "unavailable" || strings.Contains(out, "nope") {
		t.Fatalf("code=%d out=%q (reason must not leak the path)", code, out)
	}
}

func TestAuditVerify_NeverModifiesTheFile_AndRefusesSymlink(t *testing.T) {
	p := writeAudit(t, 2)
	// An unterminated tail is ignored by verify and must be left on disk untouched.
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString(`{"seq":3,"par`)
	_ = f.Close()
	before, _ := os.ReadFile(p)
	if _, code := runAuditVerify(t, "--file", p); code != 0 {
		t.Fatalf("code=%d", code)
	}
	after, _ := os.ReadFile(p)
	if !bytes.Equal(before, after) {
		t.Fatal("verify modified the audit file")
	}
	link := filepath.Join(t.TempDir(), "link.jsonl")
	if err := os.Symlink(p, link); err != nil {
		t.Skip("symlinks unsupported")
	}
	if _, code := runAuditVerify(t, "--file", link); code != exitAuditUnavailable {
		t.Fatalf("symlink must be unavailable, code=%d", code)
	}
}

func TestDoctorAuditFileDefaultMatchesAgentPath(t *testing.T) {
	if setup.DefaultAuditFilePath != agent.DurableAuditPath() {
		t.Fatalf("doctor default %q != agent path %q", setup.DefaultAuditFilePath, agent.DurableAuditPath())
	}
}

func TestStartDurableAudit_BestEffortOnFailureAndInstallsOnSuccess(t *testing.T) {
	audit.SetDurable(nil)
	core, logs := observer.New(zap.ErrorLevel)
	logger := zap.New(core)

	// Failure: a group/world-accessible file is refused; the agent must go on.
	bad := filepath.Join(t.TempDir(), "audit.jsonl")
	if err := os.WriteFile(bad, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if closeFn := startDurableAudit(logger, bad); closeFn != nil {
		t.Fatal("failure must return no teardown")
	}
	if audit.DurablePath() != "" || audit.CurrentDurableStatus().Enabled {
		t.Fatal("no sink may be installed after a failed open")
	}
	if logs.FilterMessageSnippet("NOT being persisted").Len() != 1 {
		t.Fatalf("failure must be logged loudly, got %d entries", logs.Len())
	}

	// Success: sink installed, status enabled, teardown detaches it.
	good := filepath.Join(t.TempDir(), "state", "audit.jsonl")
	closeFn := startDurableAudit(logger, good)
	if closeFn == nil || audit.DurablePath() != good || !audit.CurrentDurableStatus().Enabled {
		t.Fatal("sink not installed")
	}
	closeFn()
	if audit.CurrentDurableStatus().Enabled {
		t.Fatal("teardown must detach the sink")
	}
}

func runAuditExport(t *testing.T, args ...string) (string, int) {
	t.Helper()
	cmd := NewAuditCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetArgs(append([]string{"export"}, args...))
	err := cmd.Execute()
	code := 0
	if err != nil {
		code = exitCodeFor(err)
	}
	return out.String(), code
}

func TestAuditExport_VerifiedFileEmitsOneJSONObjectPerRecord(t *testing.T) {
	p := writeAudit(t, 3)
	before, _ := os.ReadFile(p)
	out, code := runAuditExport(t, "--file", p)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != 0 || len(lines) != 3 {
		t.Fatalf("code=%d lines=%d out=%q", code, len(lines), out)
	}
	for i, l := range lines {
		var r audit.Record
		if err := json.Unmarshal([]byte(l), &r); err != nil || r.Seq != uint64(i+1) {
			t.Fatalf("line %d: %v seq=%d", i, err, r.Seq)
		}
	}
	if after, _ := os.ReadFile(p); !bytes.Equal(before, after) {
		t.Fatal("export modified the audit file")
	}
}

func TestAuditExport_TamperedOrMissingWritesNothing(t *testing.T) {
	p := writeAudit(t, 3)
	b, _ := os.ReadFile(p)
	if err := os.WriteFile(p, bytes.Replace(b, []byte("operator"), []byte("0perator"), 1), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, code := runAuditExport(t, "--file", p); code != 10 || out != "" {
		t.Fatalf("tampered: code=%d out=%q", code, out)
	}
	if out, code := runAuditExport(t, "--file", filepath.Join(t.TempDir(), "none")); code != 11 || out != "" {
		t.Fatalf("missing: code=%d out=%q", code, out)
	}
}

func TestAuditExport_OutputFileIs0600AndNeverOverwritten(t *testing.T) {
	p := writeAudit(t, 2)
	dst := filepath.Join(t.TempDir(), "out.ndjson")
	if out, code := runAuditExport(t, "--file", p, "--output", dst); code != 0 || out != "" {
		t.Fatalf("code=%d out=%q", code, out)
	}
	st, err := os.Stat(dst)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("stat=%v err=%v", st, err)
	}
	if _, code := runAuditExport(t, "--file", p, "--output", dst); code == 0 {
		t.Fatal("existing output file must not be overwritten")
	}
	// a failed export must not leave an output file behind
	bad := filepath.Join(t.TempDir(), "never.ndjson")
	if _, code := runAuditExport(t, "--file", filepath.Join(t.TempDir(), "none"), "--output", bad); code != 11 {
		t.Fatalf("code=%d", code)
	}
	if _, err := os.Stat(bad); err == nil {
		t.Fatal("failed export created an output file")
	}
}
