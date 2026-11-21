package setup

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/internal/audit"
)

func auditFileFixture(t *testing.T, n int) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "audit.jsonl")
	d, err := audit.OpenDurable(p)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		_ = d.Append("evt", "operator", "", "s", "", "success")
	}
	_ = d.Close()
	return p
}

func TestAuditFileCheck_Outcomes(t *testing.T) {
	mk := func(path string) DoctorCheck { return newAuditFileChecks(ModeAgent, path).checkAuditFile() }

	if c := newAuditFileChecks(ModeLocal, "/x").checkAuditFile(); c.Status != DoctorInfo || c.ID != "DSO-DOCTOR-032" {
		t.Fatalf("local mode: %+v", c)
	}
	if c := mk(filepath.Join(t.TempDir(), "absent")); c.Status != DoctorInfo || !strings.Contains(c.Detail, "not present") {
		t.Fatalf("absent: %+v", c)
	}
	good := auditFileFixture(t, 3)
	if c := mk(good); c.Status != DoctorPass || !strings.Contains(c.Detail, "3 records") || !strings.Contains(c.Detail, "unkeyed") {
		t.Fatalf("good: %+v", c)
	}

	bad := auditFileFixture(t, 3)
	b, _ := os.ReadFile(bad)
	_ = os.WriteFile(bad, []byte(strings.Replace(string(b), `"user":"operator"`, `"user":"x"`, 1)), 0o600)
	if c := mk(bad); c.Status != DoctorFail || c.Severity != DoctorHigh || len(c.Recovery) == 0 {
		t.Fatalf("tampered: %+v", c)
	}

	open := auditFileFixture(t, 1)
	_ = os.Chmod(open, 0o644)
	if c := mk(open); c.Status != DoctorWarn || !strings.Contains(c.Detail, "0644") {
		t.Fatalf("world-readable: %+v", c)
	}

	link := filepath.Join(t.TempDir(), "l.jsonl")
	if err := os.Symlink(good, link); err == nil {
		if c := mk(link); c.Status != DoctorWarn {
			t.Fatalf("symlink: %+v", c)
		}
	}
}

func TestAuditFileCheck_UnreadableIsInfoNotFailure(t *testing.T) {
	ac := newAuditFileChecks(ModeAgent, auditFileFixture(t, 1))
	ac.read = func(string) ([]audit.Record, error) { return nil, fs.ErrPermission }
	if c := ac.checkAuditFile(); c.Status != DoctorInfo || !strings.Contains(c.Detail, "cannot be read") {
		t.Fatalf("%+v", c)
	}
	ac.read = func(string) ([]audit.Record, error) { return nil, errors.New("boom") }
	if c := ac.checkAuditFile(); c.Status != DoctorInfo {
		t.Fatalf("%+v", c)
	}
}
