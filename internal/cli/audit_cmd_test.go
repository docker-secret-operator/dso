package cli

import (
	"bytes"
	"encoding/json"
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
