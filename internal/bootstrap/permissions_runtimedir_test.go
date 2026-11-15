package bootstrap

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/internal/runtimedir"
)

// These tests use temporary directories only; the real /var/run/dso is never
// touched.

func TestDsoDirectoryPlan_RuntimeDirectoryIs0750AndOthersAreUnchanged(t *testing.T) {
	plan := dsoDirectoryPlan()
	want := map[string]struct {
		perm   os.FileMode
		strict bool
	}{
		"/etc/dso":     {0o775, false},
		"/var/lib/dso": {0o770, false},
		"/var/run/dso": {0o750, true},
		"/var/log/dso": {0o770, false},
	}
	if len(plan) != len(want) {
		t.Fatalf("the plan has %d entries, want %d", len(plan), len(want))
	}
	for _, d := range plan {
		w, ok := want[d.path]
		if !ok {
			t.Fatalf("unexpected directory in the plan: %s", d.path)
		}
		if d.perm != w.perm || d.strict != w.strict {
			t.Errorf("%s: perm %04o strict=%v, want %04o strict=%v", d.path, d.perm, d.strict, w.perm, w.strict)
		}
	}
	if runtimedir.DirMode != 0o750 {
		t.Fatalf("runtimedir.DirMode = %04o", runtimedir.DirMode)
	}
	for _, d := range plan {
		if d.path == "/var/run/dso" && d.perm&0o022 != 0 {
			t.Fatalf("the runtime directory plan grants group/other write: %04o", d.perm)
		}
	}
}

func TestDocumentPermissionModel_DescribesTheRuntimeDirectoryAs0750(t *testing.T) {
	doc := NewPermissionManager(&MockLogger{}, false).DocumentPermissionModel()
	if !strings.Contains(doc, "/var/run/dso (/run/dso): 0750 (root:dso)") {
		t.Error("the model must state the runtime directory is 0750 root:dso")
	}
	if strings.Contains(doc, "/var/run/dso: 0775") || strings.Contains(doc, "Owner/group read/write, others can execute") {
		t.Error("the stale 0775 description is still present")
	}
}

func specFor(path string, perm os.FileMode, strict bool) []dsoDirSpec {
	return []dsoDirSpec{{path: path, perm: perm, strict: strict}}
}

func mkdirMode(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Mkdir(path, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func asViolations(t *testing.T, err error) *permissionViolations {
	t.Helper()
	var v *permissionViolations
	if !errors.As(err, &v) {
		t.Fatalf("want *permissionViolations, got %T: %v", err, err)
	}
	return v
}

func TestVerifyDirectories_StrictRuntimeDirectoryViolationsFail(t *testing.T) {
	pm := NewPermissionManager(&MockLogger{}, false)
	uid, gid := uint32(os.Getuid()), os.Getgid()
	root := t.TempDir()

	ok := filepath.Join(root, "ok")
	mkdirMode(t, ok, 0o750)
	if err := pm.verifyDirectories(specFor(ok, 0o750, true), gid, uid); err != nil {
		t.Fatalf("a root:dso 0750 directory must verify: %v", err)
	}

	wide := filepath.Join(root, "wide")
	mkdirMode(t, wide, 0o775)
	err := pm.verifyDirectories(specFor(wide, 0o750, true), gid, uid)
	if err == nil {
		t.Fatal("0775 on the runtime directory must FAIL verification, not warn")
	}
	if v := asViolations(t, err); !strings.Contains(v.Error(), "0775") || !strings.Contains(v.Error(), "0750") {
		t.Fatalf("the violation should name both modes: %v", v)
	}

	// owner mismatch
	err = pm.verifyDirectories(specFor(ok, 0o750, true), gid, uid+1)
	asViolations(t, err)

	// group mismatch
	err = pm.verifyDirectories(specFor(ok, 0o750, true), gid+1, uid)
	asViolations(t, err)
}

func TestVerifyDirectories_NoDsoGroupMeansRootGroup(t *testing.T) {
	if os.Getgid() == 0 {
		t.Skip("running with gid 0")
	}
	pm := NewPermissionManager(&MockLogger{}, false)
	d := filepath.Join(t.TempDir(), "run")
	mkdirMode(t, d, 0o750)
	// dsoGID < 0 (the dso group does not exist): the invariant is group root.
	err := pm.verifyDirectories(specFor(d, 0o750, true), -1, uint32(os.Getuid()))
	asViolations(t, err)
}

func TestVerifyDirectories_SymlinkedRuntimeDirectoryFails(t *testing.T) {
	pm := NewPermissionManager(&MockLogger{}, false)
	root := t.TempDir()
	real := filepath.Join(root, "real")
	mkdirMode(t, real, 0o750)
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	err := pm.verifyDirectories(specFor(link, 0o750, true), os.Getgid(), uint32(os.Getuid()))
	if v := asViolations(t, err); !strings.Contains(v.Error(), "symbolic link") {
		t.Fatalf("want a symlink violation, got %v", v)
	}
}

func TestVerifyDirectories_NonStrictDeviationsOnlyWarn(t *testing.T) {
	logger := &MockLogger{}
	pm := NewPermissionManager(logger, false)
	d := filepath.Join(t.TempDir(), "etc")
	mkdirMode(t, d, 0o755) // plan says 0775, not strict
	if err := pm.verifyDirectories(specFor(d, 0o775, false), os.Getgid()+1, uint32(os.Getuid())+1); err != nil {
		t.Fatalf("a non-strict directory must only warn, got %v", err)
	}
	warned := false
	for _, m := range logger.messages {
		if strings.Contains(m, "permissions differ") {
			warned = true
		}
	}
	if !warned {
		t.Fatal("the deviation must still be reported")
	}
}

func TestVerifyDirectories_MissingDirectoryIsSkippedAndHardErrorsStillStopEarly(t *testing.T) {
	pm := NewPermissionManager(&MockLogger{}, false)
	if err := pm.verifyDirectories(specFor(filepath.Join(t.TempDir(), "absent"), 0o750, true), 0, 0); err != nil {
		t.Fatalf("a missing directory is skipped (as before), got %v", err)
	}
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err := pm.verifyDirectories(specFor(f, 0o750, true), 0, 0)
	if err == nil || strings.Contains(err.Error(), "permission violations") {
		t.Fatalf("a non-directory is a hard error, got %v", err)
	}
}

func TestVerifyDirectories_AllDirectoriesAreReportedBeforeFailing(t *testing.T) {
	logger := &MockLogger{}
	pm := NewPermissionManager(logger, false)
	uid, gid := uint32(os.Getuid()), os.Getgid()
	root := t.TempDir()
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	mkdirMode(t, a, 0o775)
	mkdirMode(t, b, 0o750)
	plan := []dsoDirSpec{{a, 0o750, true}, {b, 0o750, true}}
	err := pm.verifyDirectories(plan, gid, uid)
	asViolations(t, err)
	verified := 0
	for _, m := range logger.messages {
		if m == "Directory verified" {
			verified++
		}
	}
	if verified != 1 {
		t.Fatalf("the compliant directory should still be reported as verified, got %d", verified)
	}
}
