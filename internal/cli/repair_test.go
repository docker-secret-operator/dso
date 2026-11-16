package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/internal/setup"
)

// `dso repair permissions` command tests: temporary directories only; the
// command is never run against the host.

type repairCLI struct {
	t      *testing.T
	layout setup.PermissionsLayout
	euid   int
}

func newRepairCLI(t *testing.T) *repairCLI {
	t.Helper()
	base := t.TempDir()
	rc := &repairCLI{t: t, euid: 0, layout: setup.PermissionsLayout{
		EtcDir:    filepath.Join(base, "etc"),
		StateDir:  filepath.Join(base, "lib"),
		RunDir:    filepath.Join(base, "run"),
		LedgerDir: filepath.Join(base, "lib", "ledger"),
		PluginDir: filepath.Join(base, "plugins"),
	}}
	orig := newPermissionsRepairer
	t.Cleanup(func() { newPermissionsRepairer = orig })
	newPermissionsRepairer = func() *setup.PermissionsRepairer {
		uid, gid := os.Getuid(), os.Getgid()
		return setup.NewPermissionsRepairerWith(setup.PermissionsRepairerOptions{
			Layout:    &rc.layout,
			OwnerUID:  &uid,
			LookupGID: func() (int, bool) { return gid, true },
			EUID:      func() int { return rc.euid },
		})
	}
	return rc
}

func (rc *repairCLI) mkdir(p string, mode os.FileMode) {
	rc.t.Helper()
	if err := os.MkdirAll(p, 0o700); err != nil {
		rc.t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		rc.t.Fatal(err)
	}
}

func (rc *repairCLI) loose() {
	rc.mkdir(rc.layout.EtcDir, 0o775)
	rc.mkdir(rc.layout.StateDir, 0o770)
	rc.mkdir(rc.layout.RunDir, 0o775)
	rc.mkdir(rc.layout.LedgerDir, 0o700)
	rc.mkdir(rc.layout.PluginDir, 0o775)
}

func runRepair(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := Run(append([]string{"repair", "permissions"}, args...), &out, &errb)
	return code, out.String(), errb.String()
}

func modeOf(t *testing.T, p string) os.FileMode {
	t.Helper()
	fi, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

func TestRepairPermissions_DryRunReportsAndExits12(t *testing.T) {
	rc := newRepairCLI(t)
	rc.loose()
	rc.euid = 1000 // a dry run needs no root
	code, out, errs := runRepair("--dry-run")
	if code != exitRepairChangesRequired {
		t.Fatalf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s", code, exitRepairChangesRequired, out, errs)
	}
	for _, want := range []string{"dry run: nothing is modified", "PATH", "CURRENT", "EXPECTED", "Changes required: 4",
		"0775", "0750", "Configuration files will not be rewritten", "Ledger keys will not be regenerated",
		"No recursive permission changes will be performed"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if modeOf(t, rc.layout.EtcDir) != 0o775 || modeOf(t, rc.layout.StateDir) != 0o770 {
		t.Fatal("a dry run changed a directory")
	}
	t.Logf("sample dry-run output:\n%s", out)
}

func TestRepairPermissions_ApplyThenIdempotentExits0(t *testing.T) {
	rc := newRepairCLI(t)
	rc.loose()
	code, out, errs := runRepair()
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errs)
	}
	if modeOf(t, rc.layout.EtcDir) != 0o750 || modeOf(t, rc.layout.StateDir) != 0o750 ||
		modeOf(t, rc.layout.RunDir) != 0o750 || modeOf(t, rc.layout.PluginDir) != 0o755 {
		t.Fatal("directories not corrected")
	}
	if !strings.Contains(out, "Changed: 4") || !strings.Contains(out, "Failed: 0") {
		t.Fatalf("summary missing:\n%s", out)
	}
	code, out, _ = runRepair()
	if code != 0 || !strings.Contains(out, "Changes required: 0") || !strings.Contains(out, "Changed: 0") {
		t.Fatalf("second run exit %d, not idempotent:\n%s", code, out)
	}
	// And a dry run on the repaired tree is clean (exit 0, not 12).
	if code, _, _ = runRepair("--dry-run"); code != 0 {
		t.Fatalf("dry run after repair exits %d", code)
	}
}

func TestRepairPermissions_NonRootApplyRefusedExit1(t *testing.T) {
	rc := newRepairCLI(t)
	rc.loose()
	rc.euid = 1000
	code, out, errs := runRepair()
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if out != "" || !strings.Contains(errs, "needs root") || !strings.Contains(errs, "sudo") {
		t.Fatalf("stdout %q stderr %q", out, errs)
	}
	if modeOf(t, rc.layout.EtcDir) != 0o775 {
		t.Fatal("a refused apply changed a directory")
	}
}

func TestRepairPermissions_ManualActionExits13(t *testing.T) {
	rc := newRepairCLI(t)
	real := filepath.Join(filepath.Dir(rc.layout.EtcDir), "real")
	rc.mkdir(real, 0o770)
	if err := os.Symlink(real, rc.layout.StateDir); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runRepair()
	if code != exitRepairManualAction {
		t.Fatalf("exit %d, want %d\n%s\n%s", code, exitRepairManualAction, out, errs)
	}
	if !strings.Contains(out, "manual") || modeOf(t, real) != 0o770 {
		t.Fatalf("output:\n%s", out)
	}
}

func TestRepairPermissions_PartialFailureExits1AndSaysWhich(t *testing.T) {
	rc := newRepairCLI(t)
	rc.loose()
	orig := newPermissionsRepairer
	newPermissionsRepairer = func() *setup.PermissionsRepairer {
		uid, gid := os.Getuid(), os.Getgid()
		return setup.NewPermissionsRepairerWith(setup.PermissionsRepairerOptions{
			Layout: &rc.layout, OwnerUID: &uid,
			LookupGID: func() (int, bool) { return gid, true },
			EUID:      func() int { return 0 },
			Apply: func(path string, perm os.FileMode, u, g int) (bool, error) {
				if path == rc.layout.RunDir {
					return false, os.ErrPermission
				}
				if err := os.Chmod(path, perm); err != nil {
					return false, err
				}
				return true, nil
			},
		})
	}
	defer func() { newPermissionsRepairer = orig }()
	code, out, errs := runRepair()
	if code != 1 || !strings.Contains(errs, "1 path(s) could not be repaired (3 changed)") {
		t.Fatalf("exit %d stderr %q\n%s", code, errs, out)
	}
	if !strings.Contains(out, "FAILED") || modeOf(t, rc.layout.RunDir) != 0o775 || modeOf(t, rc.layout.EtcDir) != 0o750 {
		t.Fatalf("partial failure not reported accurately:\n%s", out)
	}
}

func TestRepairPermissions_JSONHasNoKeyMaterial(t *testing.T) {
	rc := newRepairCLI(t)
	rc.loose()
	key := []byte("dso-ledger-key-v1\nk-0123456789abcdef\n" + (strings.Repeat("NOTAREALKEY", 5))[:44] + "\n")
	if err := os.WriteFile(filepath.Join(rc.layout.LedgerDir, "ledger.key"), key, 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runRepair("--dry-run", "--json")
	if code != exitRepairChangesRequired {
		t.Fatalf("exit %d", code)
	}
	var rep setup.PermissionsReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if !rep.DryRun || rep.ChangesRequired != 5 {
		t.Fatalf("%+v", rep)
	}
	if strings.Contains(out, "NOTAREALKEY") || strings.Contains(out, "k-0123456789abcdef") {
		t.Fatal("key material in the output")
	}
}

func TestRepairPermissions_RejectsArgsAndIsRegistered(t *testing.T) {
	newRepairCLI(t)
	if code, _, _ := runRepair("extra"); code == 0 {
		t.Fatal("a stray argument must be rejected")
	}
	root := NewRootCmd()
	c, _, err := root.Find([]string{"repair", "permissions"})
	if err != nil || c.Name() != "permissions" || c.Flags().Lookup("dry-run") == nil {
		t.Fatalf("command not registered: %v", err)
	}
}
