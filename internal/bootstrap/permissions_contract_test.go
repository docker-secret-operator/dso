package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/docker-secret-operator/dso/internal/runtimedir"
	"github.com/docker-secret-operator/dso/internal/securefs"
)

// Persistent-state directory contract tests. Temporary directories only: no
// real /var/lib/dso, no recursive chmod/chown, no root needed except where a
// test skips visibly.

type dirSnapshot struct {
	mode os.FileMode
	uid  uint32
	gid  uint32
	size int64
}

func snapshotTree(t *testing.T, root string) map[string]dirSnapshot {
	t.Helper()
	out := map[string]dirSnapshot{}
	err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		st := fi.Sys().(*syscall.Stat_t)
		out[p] = dirSnapshot{fi.Mode(), st.Uid, st.Gid, fi.Size()}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// populate builds an installation-like tree inside dir: a plugin, a database,
// a ledger directory with a key-like file and user content with loose modes.
func populate(t *testing.T, dir string) {
	t.Helper()
	files := map[string]os.FileMode{
		"plugins/dso-provider-x": 0o755,
		"state/rotations.json":   0o600,
		"vault.db":               0o666, // deliberately loose: repair must not tighten it
		"user/notes.txt":         0o644,
		"ledger/ledger.key":      0o400,
	}
	for rel, mode := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o777); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{"plugins", "state", "user", "ledger"} {
		if err := os.Chmod(filepath.Join(dir, d), 0o777&^0o000); err != nil {
			t.Fatal(err)
		}
	}
}

func dirModeOf(t *testing.T, p string) os.FileMode {
	t.Helper()
	fi, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

// repairManager is a PermissionManager whose owner target is the test user, so
// ownership logic runs for real without root.
func repairManager(calls *[]string, changed *[]bool) *PermissionManager {
	pm := NewPermissionManager(&MockLogger{}, false)
	pm.applyDir = func(path string, perm os.FileMode, _ /*root*/, gid int) (bool, error) {
		*calls = append(*calls, path)
		c, err := ApplyDirectoryNoFollow(path, perm, os.Getuid(), gid)
		*changed = append(*changed, c)
		return c, err
	}
	return pm
}

// safeBase returns a private temp directory directly under the system temp
// directory, so the ledger provider's directory-chain check can pass (t.TempDir
// can be group-writable under some umasks).
func safeBase(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "dso-contract-")
	if err != nil {
		t.Skipf("no temp directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	if err := os.Chmod(d, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := securefs.CheckDirChain(d); err != nil {
		t.Skipf("the temp base is not a safe directory chain here: %v", err)
	}
	return d
}

func TestDirectoryContract_PlanIsPinned(t *testing.T) {
	want := map[string]struct {
		perm   os.FileMode
		strict bool
	}{
		"/etc/dso":     {0o750, false},
		"/var/lib/dso": {0o750, false},
		"/var/run/dso": {0o750, true},
		"/var/log/dso": {0o770, false},
	}
	got := dsoDirectoryPlan()
	if len(got) != len(want) {
		t.Fatalf("plan has %d entries, want %d", len(got), len(want))
	}
	for _, d := range got {
		w, ok := want[d.path]
		if !ok || d.perm != w.perm || d.strict != w.strict {
			t.Errorf("%s: %04o strict=%v, want %+v", d.path, d.perm, d.strict, w)
		}
	}
	if StateDirPath != "/var/lib/dso" || StateDirMode != 0o750 {
		t.Fatalf("state directory constants drifted: %s %04o", StateDirPath, StateDirMode)
	}
	if StateDirMode&0o027 != 0 {
		t.Fatalf("the state directory mode %04o must not grant group write or any other access", StateDirMode)
	}
	if runtimedir.DirMode != 0o750 {
		t.Fatalf("runtimedir.DirMode %04o", runtimedir.DirMode)
	}
}

func TestDirectoryContract_NoStaleDocText(t *testing.T) {
	doc := NewPermissionManager(&MockLogger{}, false).DocumentPermissionModel()
	if !strings.Contains(doc, "/var/lib/dso: 0750 (root:dso)") {
		t.Error("the permission model must state /var/lib/dso is 0750 root:dso")
	}
	if strings.Contains(doc, "can write state") {
		t.Error("the stale claim that dso group members can write state is still present")
	}
}

func TestDirectoryValidator_UsesThePlanModes(t *testing.T) {
	base := t.TempDir()
	plan := []dsoDirSpec{{filepath.Join(base, "lib"), 0o750, false}, {filepath.Join(base, "log"), 0o770, false}}
	dv := &DirectoryValidator{logger: &MockLogger{}, dirs: plan}
	if err := dv.ValidateBootstrapDirectories("test"); err != nil {
		t.Fatal(err)
	}
	for _, d := range plan {
		if _, err := os.Stat(d.path); err != nil {
			t.Fatalf("%s not created: %v", d.path, err)
		}
	}
}

func TestRepairStateDirectory_OnlyTheDirectoryChanges(t *testing.T) {
	base := t.TempDir()
	lib := filepath.Join(base, "lib")
	if err := os.Mkdir(lib, 0o700); err != nil {
		t.Fatal(err)
	}
	populate(t, lib)
	if err := os.Chmod(lib, 0o770); err != nil { // the pre-upgrade state
		t.Fatal(err)
	}
	before := snapshotTree(t, lib)

	var calls []string
	var changed []bool
	pm := repairManager(&calls, &changed)
	plan := []dsoDirSpec{{lib, StateDirMode, false}}
	if err := pm.setupDirectories(plan, os.Getgid()); err != nil {
		t.Fatal(err)
	}
	if got := dirModeOf(t, lib); got != 0o750 {
		t.Fatalf("state directory mode %04o, want 0750", got)
	}
	if len(calls) != 1 || calls[0] != lib || !changed[0] {
		t.Fatalf("calls %v changed %v: exactly the directory must be applied", calls, changed)
	}
	after := snapshotTree(t, lib)
	if len(before) != len(after) {
		t.Fatalf("the tree changed shape: %d -> %d entries", len(before), len(after))
	}
	for p, b := range before {
		if a := after[p]; a != b {
			t.Errorf("%s was touched: %+v -> %+v", p, b, a)
		}
	}
	if got := dirModeOf(t, filepath.Join(lib, "vault.db")); got != 0o666 {
		t.Fatalf("a file inside was chmod'ed to %04o", got)
	}

	// Idempotent: a second run reports no change.
	calls, changed = nil, nil
	if err := pm.setupDirectories(plan, os.Getgid()); err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 || changed[0] {
		t.Fatalf("second run changed=%v, want no change", changed)
	}
}

func TestRepairStateDirectory_AppliesOnlyPlanPathsNeverChildren(t *testing.T) {
	base := t.TempDir()
	lib := filepath.Join(base, "lib")
	if err := os.Mkdir(lib, 0o770); err != nil {
		t.Fatal(err)
	}
	populate(t, lib)
	var applied, made []string
	pm := NewPermissionManager(&MockLogger{}, false)
	pm.mkdirAll = func(p string, _ os.FileMode) error { made = append(made, p); return os.MkdirAll(p, 0o750) }
	pm.applyDir = func(p string, _ os.FileMode, _, _ int) (bool, error) { applied = append(applied, p); return false, nil }
	if err := pm.setupDirectories([]dsoDirSpec{{lib, 0o750, false}}, os.Getgid()); err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || applied[0] != lib || len(made) != 1 || made[0] != lib {
		t.Fatalf("applied %v made %v: only the planned directory may be handled", applied, made)
	}
}

func TestRepairStateDirectory_SymlinkRefused(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.Mkdir(real, 0o770); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(real, 0o770); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "lib")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	var calls []string
	var changed []bool
	pm := repairManager(&calls, &changed)
	if err := pm.setupDirectories([]dsoDirSpec{{link, 0o750, false}}, os.Getgid()); err == nil {
		t.Fatal("a symlinked state directory must be refused")
	}
	if len(calls) != 0 {
		t.Fatalf("the apply step ran on a symlink: %v", calls)
	}
	if got := dirModeOf(t, real); got != 0o770 {
		t.Fatalf("the symlink target was changed to %04o", got)
	}
	// The descriptor-level guard refuses on its own as well (a race that swaps
	// the directory for a link after the Lstat cannot redirect the change).
	if _, err := ApplyDirectoryNoFollow(link, 0o750, -1, -1); err == nil {
		t.Fatal("ApplyDirectoryNoFollow followed a symlink")
	}
	if got := dirModeOf(t, real); got != 0o770 {
		t.Fatalf("the symlink target was changed to %04o by the descriptor guard", got)
	}
	// A regular file is not a directory.
	f := filepath.Join(base, "file")
	if err := os.WriteFile(f, []byte("x"), 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyDirectoryNoFollow(f, 0o750, -1, -1); err == nil {
		t.Fatal("a regular file was accepted as a directory")
	}
}

func TestRepairStateDirectory_ChownToRootOnlyTouchesTheDirectory(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("SKIPPED (root-only): changing a directory's owner to root:<gid> needs root; the ownership comparison and no-recursion rules run unprivileged in the other tests")
	}
	base := t.TempDir()
	lib := filepath.Join(base, "lib")
	if err := os.Mkdir(lib, 0o770); err != nil {
		t.Fatal(err)
	}
	populate(t, lib)
	if err := os.Chown(filepath.Join(lib, "vault.db"), 1234, 1234); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(lib, 1234, 1234); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyDirectoryNoFollow(lib, 0o750, 0, 4321); err != nil {
		t.Fatal(err)
	}
	st := mustStat(t, lib)
	if st.Uid != 0 || st.Gid != 4321 || dirModeOf(t, lib) != 0o750 {
		t.Fatalf("directory is %d:%d %04o", st.Uid, st.Gid, dirModeOf(t, lib))
	}
	if c := mustStat(t, filepath.Join(lib, "vault.db")); c.Uid != 1234 {
		t.Fatalf("a child was chowned to %d", c.Uid)
	}
}

func mustStat(t *testing.T, p string) *syscall.Stat_t {
	t.Helper()
	fi, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Sys().(*syscall.Stat_t)
}

// After the repair the directory-chain check the ledger key provider applies
// (securefs.CheckDirChain on the ledger directory) accepts the chain; at the
// pre-upgrade 0770 it refused it and the ledger opened disabled. (The provider
// itself is exercised in internal/ledger/key_contract_linux_test.go; only that
// package and the engine wiring may import internal/ledger.)
func TestRepairStateDirectory_LedgerDirChainAcceptsTheRepairedParent(t *testing.T) {
	base := safeBase(t)
	lib := filepath.Join(base, "lib")
	ldir := filepath.Join(lib, "ledger")
	if err := os.MkdirAll(ldir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(lib, 0o770); err != nil {
		t.Fatal(err)
	}
	if err := securefs.CheckDirChain(ldir); err == nil {
		t.Fatal("a group-writable parent was accepted before the repair")
	}
	var calls []string
	var changed []bool
	pm := repairManager(&calls, &changed)
	if err := pm.setupDirectories([]dsoDirSpec{{lib, StateDirMode, false}}, os.Getgid()); err != nil {
		t.Fatal(err)
	}
	if err := securefs.CheckDirChain(ldir); err != nil {
		t.Fatalf("the repaired directory chain is refused: %v", err)
	}
	if got := dirModeOf(t, ldir); got != 0o700 {
		t.Fatalf("the ledger directory mode changed to %04o", got)
	}
}

// /etc/dso is root:dso 0750 in every writer: the bootstrap plan, the rollback
// recreation and the documented permission model. (The setup planner and the
// repair command are checked against the same plan in internal/setup.)
func TestDirectoryContract_EtcDirIsRootDso0750EverywhereInBootstrap(t *testing.T) {
	if EtcDirPath != "/etc/dso" || EtcDirMode != 0o750 {
		t.Fatalf("etc constants drifted: %s %04o", EtcDirPath, EtcDirMode)
	}
	if EtcDirMode&0o027 != 0 {
		t.Fatalf("/etc/dso mode %04o must not grant group write or any other access", EtcDirMode)
	}
	found := false
	for _, d := range dsoDirectoryPlan() {
		if d.path == EtcDirPath {
			found = true
			if d.perm != EtcDirMode {
				t.Errorf("plan mode %04o != EtcDirMode %04o", d.perm, EtcDirMode)
			}
		}
	}
	if !found {
		t.Fatal("/etc/dso is not in the directory plan")
	}
	for _, c := range DirectoryContract() {
		if c.Path == EtcDirPath && c.Mode != EtcDirMode {
			t.Errorf("DirectoryContract %04o != EtcDirMode", c.Mode)
		}
	}
	// The rollback recreation (a hard-coded path in rollback.go) must use the
	// contract constants rather than a literal mode.
	src, err := os.ReadFile("rollback.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "SafeCreateDirectory(ctx, EtcDirPath, EtcDirMode,") || strings.Contains(string(src), "0775") {
		t.Error("rollback.go must recreate /etc/dso with EtcDirMode, not a literal 0775")
	}
	doc := NewPermissionManager(&MockLogger{}, false).DocumentPermissionModel()
	if !strings.Contains(doc, "/etc/dso: 0750 (root:dso)") || strings.Contains(doc, "0775") {
		t.Error("the permission model must state /etc/dso is 0750 root:dso and no 0775")
	}
}
