package setup

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/docker-secret-operator/dso/internal/bootstrap"
)

// `dso repair permissions` tests. Temporary directories only: the real
// /etc/dso, /var/lib/dso, /run/dso and plugin directory are never touched, and
// no test needs root (the one root-only test skips visibly).

const (
	fixtureKeyHeader = "dso-ledger-key-v1\n"
	// An obviously fake key body: a marker the output must never contain.
	fixtureKeyBody = "NOTAREALKEYBODYNOTAREALKEYBODYNOTAREAL=="
)

// fixtureKey is a well-formed (size/header) but fake ledger key file.
func fixtureKey() []byte {
	body := fixtureKeyBody
	for len(body) < 44 {
		body += "A"
	}
	return []byte(fixtureKeyHeader + "k-0123456789abcdef\n" + body[:44] + "\n")
}

type permFixture struct {
	t      *testing.T
	base   string
	layout PermissionsLayout
	r      *PermissionsRepairer
}

func newPermFixture(t *testing.T) *permFixture {
	t.Helper()
	base := t.TempDir()
	f := &permFixture{t: t, base: base, layout: PermissionsLayout{
		EtcDir:    filepath.Join(base, "etc"),
		StateDir:  filepath.Join(base, "lib"),
		RunDir:    filepath.Join(base, "run"),
		LedgerDir: filepath.Join(base, "lib", "ledger"),
		PluginDir: filepath.Join(base, "plugins"),
	}}
	uid, gid := os.Getuid(), os.Getgid()
	f.r = NewPermissionsRepairerWith(PermissionsRepairerOptions{
		Layout:    &f.layout,
		OwnerUID:  &uid,
		LookupGID: func() (int, bool) { return gid, true },
		EUID:      func() int { return 0 }, // pretend root: the seams make the test unprivileged
	})
	return f
}

func (f *permFixture) mkdir(p string, mode os.FileMode) {
	f.t.Helper()
	if err := os.MkdirAll(p, 0o700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		f.t.Fatal(err)
	}
}

func (f *permFixture) write(p string, data []byte, mode os.FileMode) {
	f.t.Helper()
	if err := os.WriteFile(p, data, 0o600); err != nil {
		f.t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		f.t.Fatal(err)
	}
}

// installLoose creates the whole contract in its pre-repair state: every
// directory too open, children with loose modes, a config file and a key.
func (f *permFixture) installLoose() {
	f.mkdir(f.layout.EtcDir, 0o775)
	f.write(filepath.Join(f.layout.EtcDir, "dso.yaml"), []byte("provider: local\n# not rewritten\n"), 0o664)
	f.mkdir(filepath.Join(f.layout.EtcDir, "plugins"), 0o777)
	f.mkdir(f.layout.StateDir, 0o770)
	f.write(filepath.Join(f.layout.StateDir, "vault.db"), []byte("db"), 0o666)
	f.mkdir(filepath.Join(f.layout.StateDir, "state"), 0o777)
	f.mkdir(f.layout.RunDir, 0o775)
	f.write(filepath.Join(f.layout.RunDir, "dso.sock"), []byte("s"), 0o666)
	f.mkdir(f.layout.LedgerDir, 0o770)
	f.write(filepath.Join(f.layout.LedgerDir, "ledger.key"), fixtureKey(), 0o644)
	f.write(filepath.Join(f.layout.LedgerDir, "ledger.json"), []byte("{}"), 0o666)
	f.mkdir(f.layout.PluginDir, 0o775)
	f.write(filepath.Join(f.layout.PluginDir, "dso-provider-x"), []byte("bin"), 0o777)
}

type snap struct {
	mode os.FileMode
	uid  uint32
	gid  uint32
	size int64
	data string
}

func snapshotAll(t *testing.T, root string, skip map[string]bool) map[string]snap {
	t.Helper()
	out := map[string]snap{}
	err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if skip[p] {
			return nil
		}
		st := fi.Sys().(*syscall.Stat_t)
		s := snap{fi.Mode(), st.Uid, st.Gid, fi.Size(), ""}
		if fi.Mode().IsRegular() {
			b, _ := os.ReadFile(p) // #nosec G304 -- test path under TempDir
			s.data = string(b)
		}
		out[p] = s
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (f *permFixture) listed() map[string]bool {
	return map[string]bool{
		f.layout.EtcDir: true, f.layout.StateDir: true, f.layout.RunDir: true,
		f.layout.LedgerDir: true, filepath.Join(f.layout.LedgerDir, "ledger.key"): true, f.layout.PluginDir: true,
	}
}

func modeOfPath(t *testing.T, p string) os.FileMode {
	t.Helper()
	fi, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode() & (os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky)
}

func entryFor(t *testing.T, rep *PermissionsReport, path string) PermissionEntry {
	t.Helper()
	for _, e := range rep.Entries {
		if e.Path == path {
			return e
		}
	}
	t.Fatalf("no entry for %s in %+v", path, rep.Entries)
	return PermissionEntry{}
}

func TestPermissionsRepair_DryRunIsNonMutating(t *testing.T) {
	f := newPermFixture(t)
	f.installLoose()
	before := snapshotAll(t, f.base, nil)
	f.r.euid = func() int { return 4242 } // dry run needs no root

	rep, err := f.r.Repair(true)
	if err != nil {
		t.Fatal(err)
	}
	if after := snapshotAll(t, f.base, nil); fmt.Sprint(before) != fmt.Sprint(after) {
		t.Fatal("a dry run modified the tree")
	}
	if !rep.DryRun || rep.ChangesRequired != 6 || rep.Changed != 0 || rep.Failed != 0 {
		t.Fatalf("report %+v: want 6 corrections required (etc, lib, run, ledger dir, key, plugins)", rep)
	}
	for _, e := range rep.Entries {
		if e.Result != "" {
			t.Errorf("%s has result %q in a dry run", e.Path, e.Result)
		}
	}
	var out bytes.Buffer
	rep.WriteText(&out)
	for _, want := range []string{"PATH", "CURRENT", "EXPECTED", "Changes required: 6",
		"Configuration files will not be rewritten", "Ledger keys will not be regenerated", "No recursive permission changes will be performed"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

func TestPermissionsRepair_ApplyChangesOnlyListedDirectories(t *testing.T) {
	f := newPermFixture(t)
	f.installLoose()
	cfg := filepath.Join(f.layout.EtcDir, "dso.yaml")
	cfgBefore, _ := os.ReadFile(cfg) // #nosec G304 -- test path under TempDir
	before := snapshotAll(t, f.base, f.listed())

	rep, err := f.r.Repair(false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Failed != 0 || rep.Changed != 6 || rep.ChangesRequired != 6 {
		t.Fatalf("report: changed %d failed %d required %d\n%+v", rep.Changed, rep.Failed, rep.ChangesRequired, rep.Entries)
	}
	want := map[string]os.FileMode{
		f.layout.EtcDir: 0o750, f.layout.StateDir: 0o750, f.layout.RunDir: 0o750,
		f.layout.LedgerDir: 0o700, filepath.Join(f.layout.LedgerDir, "ledger.key"): 0o400, f.layout.PluginDir: 0o755,
	}
	for p, m := range want {
		if got := modeOfPath(t, p); got != m {
			t.Errorf("%s mode %04o, want %04o", p, got, m)
		}
	}
	// Children (modes, owners, sizes, contents) are untouched.
	after := snapshotAll(t, f.base, f.listed())
	if len(before) != len(after) {
		t.Fatalf("tree shape changed: %d -> %d entries", len(before), len(after))
	}
	for p, b := range before {
		if a := after[p]; a != b {
			t.Errorf("%s was touched: %+v -> %+v", p, b, a)
		}
	}
	if got, _ := os.ReadFile(cfg); !bytes.Equal(got, cfgBefore) { // #nosec G304 -- test path under TempDir
		t.Fatal("dso.yaml bytes changed")
	}
	if got := modeOfPath(t, cfg); got != 0o664 {
		t.Fatalf("dso.yaml mode changed to %04o", got)
	}
	if got := modeOfPath(t, filepath.Join(f.layout.StateDir, "vault.db")); got != 0o666 {
		t.Fatalf("a child was chmod'ed to %04o", got)
	}

	// Idempotent: a second run has nothing to do and reports success.
	rep2, err := f.r.Repair(false)
	if err != nil {
		t.Fatal(err)
	}
	if rep2.ChangesRequired != 0 || rep2.Changed != 0 || rep2.Failed != 0 {
		t.Fatalf("second run not idempotent: %+v", rep2)
	}
}

func TestPermissionsRepair_SymlinkRefused(t *testing.T) {
	f := newPermFixture(t)
	real := filepath.Join(f.base, "real")
	f.mkdir(real, 0o770)
	if err := os.Symlink(real, f.layout.StateDir); err != nil {
		t.Fatal(err)
	}
	rep, err := f.r.Repair(false)
	if err != nil {
		t.Fatal(err)
	}
	e := entryFor(t, rep, f.layout.StateDir)
	if e.Status != PermStatusManual || e.Result != "" || !strings.Contains(e.Detail, "symbolic link") {
		t.Fatalf("symlink entry %+v", e)
	}
	if got := modeOfPath(t, real); got != 0o770 {
		t.Fatalf("the symlink target was changed to %04o", got)
	}
	if rep.ManualRequired == 0 {
		t.Fatal("the symlink must need manual action")
	}
}

func TestPermissionsRepair_SymlinkSwappedInAfterInspectionIsRefusedAtTheDescriptor(t *testing.T) {
	f := newPermFixture(t)
	f.mkdir(f.layout.StateDir, 0o770)
	real := filepath.Join(f.base, "elsewhere")
	f.mkdir(real, 0o770)
	f.r.apply = func(path string, perm os.FileMode, uid, gid int) (bool, error) {
		if path == f.layout.StateDir { // the race: a link replaces the directory after Lstat
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(real, path); err != nil {
				t.Fatal(err)
			}
		}
		return bootstrap.ApplyDirectoryNoFollow(path, perm, uid, gid)
	}
	rep, err := f.r.Repair(false)
	if err != nil {
		t.Fatal(err)
	}
	e := entryFor(t, rep, f.layout.StateDir)
	if e.Result != PermResultFailed || e.Error == "" {
		t.Fatalf("expected a refused change, got %+v", e)
	}
	if got := modeOfPath(t, real); got != 0o770 {
		t.Fatalf("the redirected target was changed to %04o", got)
	}
}

func TestPermissionsRepair_RegularFileInsteadOfDirectoryRefused(t *testing.T) {
	f := newPermFixture(t)
	f.write(f.layout.EtcDir, []byte("not a directory"), 0o666)
	rep, err := f.r.Repair(false)
	if err != nil {
		t.Fatal(err)
	}
	e := entryFor(t, rep, f.layout.EtcDir)
	if e.Status != PermStatusManual || e.Current != "not a directory" {
		t.Fatalf("entry %+v", e)
	}
	if got := modeOfPath(t, f.layout.EtcDir); got != 0o666 {
		t.Fatalf("the file was chmod'ed to %04o", got)
	}
}

func TestPermissionsRepair_LedgerKeyNeverCreated(t *testing.T) {
	f := newPermFixture(t)
	f.mkdir(f.layout.LedgerDir, 0o700)
	keyPath := filepath.Join(f.layout.LedgerDir, "ledger.key")

	// First run (no key, no ledger state): informational, nothing created.
	rep, err := f.r.Repair(false)
	if err != nil {
		t.Fatal(err)
	}
	if e := entryFor(t, rep, keyPath); e.Status != PermStatusAbsent || !strings.Contains(e.Detail, "never by this command") {
		t.Fatalf("first-run entry %+v", e)
	}
	if _, err := os.Lstat(keyPath); !os.IsNotExist(err) {
		t.Fatalf("a key file exists after the repair: %v", err)
	}

	// Lost key (ledger state exists): a diagnostic, still nothing created.
	f.write(filepath.Join(f.layout.LedgerDir, "ledger.json"), []byte("{}"), 0o600)
	rep, err = f.r.Repair(false)
	if err != nil {
		t.Fatal(err)
	}
	e := entryFor(t, rep, keyPath)
	if e.Status != PermStatusManual || !strings.Contains(e.Detail, "MISSING") || !strings.Contains(e.Detail, "never regenerated") {
		t.Fatalf("lost-key entry %+v", e)
	}
	if _, err := os.Lstat(keyPath); !os.IsNotExist(err) {
		t.Fatalf("a key file was created: %v", err)
	}
	ents, _ := os.ReadDir(f.layout.LedgerDir)
	if len(ents) != 1 {
		t.Fatalf("the ledger directory gained files: %v", ents)
	}
}

func TestPermissionsRepair_LedgerKeyOnlyAVerifiedKeyIsChmodded(t *testing.T) {
	good := fixtureKey()
	cases := []struct {
		name   string
		data   []byte
		mode   os.FileMode
		mutate func(f *permFixture, key string)
		want   string // status
		after  os.FileMode
	}{
		{"loose verified key is tightened", good, 0o644, nil, PermStatusChange, 0o400},
		{"0600 is accepted untouched", good, 0o600, nil, PermStatusOK, 0o600},
		{"0400 is untouched", good, 0o400, nil, PermStatusOK, 0o400},
		{"wrong size is corrupt: untouched", []byte(strings.Repeat("x", 10)), 0o644, nil, PermStatusManual, 0o644},
		{"right size, no header: untouched", bytes.Repeat([]byte("z"), len(good)), 0o644, nil, PermStatusManual, 0o644},
		{"other owner: untouched", good, 0o644, func(f *permFixture, _ string) { f.r.ownerUID = os.Getuid() + 1 }, PermStatusManual, 0o644},
		{"hard-linked key: untouched", good, 0o644, func(f *permFixture, key string) {
			if err := os.Link(key, key+".extra"); err != nil {
				f.t.Skipf("hard links unavailable: %v", err)
			}
		}, PermStatusManual, 0o644},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newPermFixture(t)
			f.mkdir(f.layout.LedgerDir, 0o700)
			key := filepath.Join(f.layout.LedgerDir, "ledger.key")
			f.write(key, c.data, c.mode)
			if c.mutate != nil {
				c.mutate(f, key)
			}
			rep, err := f.r.Repair(false)
			if err != nil {
				t.Fatal(err)
			}
			e := entryFor(t, rep, key)
			if e.Status != c.want {
				t.Fatalf("status %q, want %q (%+v)", e.Status, c.want, e)
			}
			if got := modeOfPath(t, key); got != c.after {
				t.Fatalf("key mode %04o, want %04o", got, c.after)
			}
			if got, _ := os.ReadFile(key); !bytes.Equal(got, c.data) { // #nosec G304 -- test path under TempDir
				t.Fatal("key bytes changed")
			}
		})
	}
}

func TestPermissionsRepair_LedgerKeySymlinkRefused(t *testing.T) {
	f := newPermFixture(t)
	f.mkdir(f.layout.LedgerDir, 0o700)
	target := filepath.Join(f.base, "other.key")
	f.write(target, fixtureKey(), 0o644)
	key := filepath.Join(f.layout.LedgerDir, "ledger.key")
	if err := os.Symlink(target, key); err != nil {
		t.Fatal(err)
	}
	rep, err := f.r.Repair(false)
	if err != nil {
		t.Fatal(err)
	}
	if e := entryFor(t, rep, key); e.Status != PermStatusManual || e.Current != "symbolic link" {
		t.Fatalf("entry %+v", e)
	}
	if got := modeOfPath(t, target); got != 0o644 {
		t.Fatalf("the link target was changed to %04o", got)
	}
}

func TestPermissionsRepair_NonRootApplyRefused(t *testing.T) {
	f := newPermFixture(t)
	f.installLoose()
	before := snapshotAll(t, f.base, nil)
	f.r.euid = func() int { return 1000 }
	rep, err := f.r.Repair(false)
	if !errors.Is(err, ErrPermissionsNeedRoot) || rep != nil {
		t.Fatalf("rep=%v err=%v: a non-root apply must be refused", rep, err)
	}
	for _, want := range []string{"root", "sudo", "--dry-run"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q lacks %q", err, want)
		}
	}
	if after := snapshotAll(t, f.base, nil); fmt.Sprint(before) != fmt.Sprint(after) {
		t.Fatal("a refused apply changed something")
	}
}

func TestPermissionsRepair_PartialFailureIsReportedAccurately(t *testing.T) {
	f := newPermFixture(t)
	f.installLoose()
	f.r.apply = func(path string, perm os.FileMode, uid, gid int) (bool, error) {
		if path == f.layout.StateDir {
			return false, errors.New("injected chmod failure")
		}
		return bootstrap.ApplyDirectoryNoFollow(path, perm, uid, gid)
	}
	rep, err := f.r.Repair(false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Failed != 1 || rep.Changed != 5 {
		t.Fatalf("changed %d failed %d, want 5/1", rep.Changed, rep.Failed)
	}
	e := entryFor(t, rep, f.layout.StateDir)
	if e.Result != PermResultFailed || !strings.Contains(e.Error, "injected chmod failure") {
		t.Fatalf("failing entry %+v", e)
	}
	if got := modeOfPath(t, f.layout.StateDir); got != 0o770 {
		t.Fatalf("the failed directory is now %04o", got)
	}
	for _, p := range []string{f.layout.EtcDir, f.layout.RunDir, f.layout.LedgerDir, f.layout.PluginDir} {
		if entryFor(t, rep, p).Result != PermResultChanged {
			t.Errorf("%s not reported changed", p)
		}
	}
	var out bytes.Buffer
	rep.WriteText(&out)
	if !strings.Contains(out.String(), "FAILED: injected chmod failure") || !strings.Contains(out.String(), "Failed: 1") {
		t.Fatalf("output does not report the failure:\n%s", out.String())
	}
}

func TestPermissionsRepair_KeyChmodFailureIsReported(t *testing.T) {
	f := newPermFixture(t)
	f.mkdir(f.layout.LedgerDir, 0o700)
	key := filepath.Join(f.layout.LedgerDir, "ledger.key")
	f.write(key, fixtureKey(), 0o644)
	f.r.chmodKey = func(*os.File, os.FileMode) error { return errors.New("injected key chmod failure") }
	rep, err := f.r.Repair(false)
	if err != nil {
		t.Fatal(err)
	}
	if e := entryFor(t, rep, key); e.Result != PermResultFailed || rep.Failed != 1 {
		t.Fatalf("entry %+v failed=%d", e, rep.Failed)
	}
}

func TestPermissionsRepair_OutputNeverContainsKeyMaterial(t *testing.T) {
	f := newPermFixture(t)
	f.installLoose()
	for _, dry := range []bool{true, false} {
		rep, err := f.r.Repair(dry)
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		rep.WriteText(&out)
		for _, banned := range []string{fixtureKeyBody[:20], "k-0123456789abcdef", "NOTAREALKEY"} {
			if strings.Contains(out.String(), banned) {
				t.Fatalf("output contains key material %q:\n%s", banned, out.String())
			}
		}
	}
}

func TestPermissionsRepair_PluginDirectory(t *testing.T) {
	t.Run("write bits are removed, read and execute are kept", func(t *testing.T) {
		f := newPermFixture(t)
		f.mkdir(f.layout.PluginDir, 0o775)
		if _, err := f.r.Repair(false); err != nil {
			t.Fatal(err)
		}
		if got := modeOfPath(t, f.layout.PluginDir); got != 0o755 {
			t.Fatalf("mode %04o", got)
		}
	})
	t.Run("a stricter mode is left alone", func(t *testing.T) {
		f := newPermFixture(t)
		f.mkdir(f.layout.PluginDir, 0o700)
		rep, _ := f.r.Repair(false)
		if e := entryFor(t, rep, f.layout.PluginDir); e.Status != PermStatusOK {
			t.Fatalf("entry %+v", e)
		}
		if got := modeOfPath(t, f.layout.PluginDir); got != 0o700 {
			t.Fatalf("mode %04o", got)
		}
	})
	t.Run("not root-owned: reported, mode not changed", func(t *testing.T) {
		f := newPermFixture(t)
		f.mkdir(f.layout.PluginDir, 0o777)
		f.r.ownerUID = os.Getuid() + 1
		rep, _ := f.r.Repair(false)
		if e := entryFor(t, rep, f.layout.PluginDir); e.Status != PermStatusManual {
			t.Fatalf("entry %+v", e)
		}
		if got := modeOfPath(t, f.layout.PluginDir); got != 0o777 {
			t.Fatalf("mode %04o", got)
		}
	})
}

func TestPermissionsRepair_NoDSOGroupFallsBackToRoot(t *testing.T) {
	f := newPermFixture(t)
	f.mkdir(f.layout.StateDir, 0o750)
	gid := os.Getgid()
	f.r.lookupGID = func() (int, bool) { return 0, false }
	rep := f.r.Inspect()
	e := entryFor(t, rep, f.layout.StateDir)
	if !rep.NoDSOGroup || !strings.Contains(e.Expected, "root:root 0750") {
		t.Fatalf("expected root:root fallback, got %+v", e)
	}
	if gid != 0 && e.Status != PermStatusChange {
		t.Fatalf("a directory owned by gid %d must need the root group: %+v", gid, e)
	}
}

func TestPermissionsRepair_StricterModeIsNeverLoosened(t *testing.T) {
	f := newPermFixture(t)
	f.mkdir(f.layout.EtcDir, 0o700)
	rep, _ := f.r.Repair(false)
	e := entryFor(t, rep, f.layout.EtcDir)
	if e.Status != PermStatusOK || !strings.Contains(e.Detail, "stricter") {
		t.Fatalf("entry %+v", e)
	}
	if got := modeOfPath(t, f.layout.EtcDir); got != 0o700 {
		t.Fatalf("the directory was loosened to %04o", got)
	}
	// The strict runtime directory is exact: a stricter mode there is corrected.
	f.mkdir(f.layout.RunDir, 0o700)
	rep, _ = f.r.Repair(false)
	if e := entryFor(t, rep, f.layout.RunDir); e.Status != PermStatusChange {
		t.Fatalf("the strict runtime directory must be exactly 0750: %+v", e)
	}
}

func TestPermissionsRepair_AbsentPathsAreNotCreated(t *testing.T) {
	f := newPermFixture(t)
	rep, err := f.r.Repair(false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.ChangesRequired != 0 || rep.ManualRequired != 0 {
		t.Fatalf("%+v", rep)
	}
	ents, _ := os.ReadDir(f.base)
	if len(ents) != 0 {
		t.Fatalf("a missing path was created: %v", ents)
	}
}

// Bootstrap, the setup planner and the repair command share one contract.
func TestPermissionsContract_BootstrapPlannerAndRepairAgree(t *testing.T) {
	plan := map[string]bootstrap.DirectoryContractEntry{}
	for _, e := range bootstrap.DirectoryContract() {
		plan[e.Path] = e
	}
	for _, p := range permissionsRepairScope {
		if _, ok := plan[p]; !ok {
			t.Fatalf("repair scope path %s is not in the bootstrap directory plan", p)
		}
	}
	if got := plan["/etc/dso"].Mode; got != 0o750 || bootstrap.EtcDirMode != 0o750 {
		t.Fatalf("bootstrap /etc/dso mode %04o", got)
	}
	if got := configDirMode(ModeAgent); got != plan["/etc/dso"].Mode {
		t.Fatalf("setup planner config directory mode %04o != bootstrap %04o", got, plan["/etc/dso"].Mode)
	}
	if got := configDirOwner(ModeAgent); got != "root:dso" {
		t.Fatalf("planner owner %q", got)
	}
	// The repair expectations come from the same plan: a directory at the plan
	// mode passes, and one group-writable (the 0775 of earlier bootstraps) needs repair.
	for _, c := range []struct {
		path string
		dir  func(f *permFixture) string
	}{
		{"/etc/dso", func(f *permFixture) string { return f.layout.EtcDir }},
		{bootstrap.StateDirPath, func(f *permFixture) string { return f.layout.StateDir }},
		{"/var/run/dso", func(f *permFixture) string { return f.layout.RunDir }},
	} {
		f := newPermFixture(t)
		d := c.dir(f)
		f.mkdir(d, plan[c.path].Mode)
		if e := entryFor(t, f.r.Inspect(), d); e.Status != PermStatusOK {
			t.Errorf("%s at the plan mode %04o is not accepted: %+v", c.path, plan[c.path].Mode, e)
		}
		f.mkdir(d, 0o775)
		e := entryFor(t, f.r.Inspect(), d)
		if e.Status != PermStatusChange || !strings.Contains(e.Expected, fmt.Sprintf("%04o", plan[c.path].Mode)) {
			t.Errorf("%s at 0775 must need repair to %04o: %+v", c.path, plan[c.path].Mode, e)
		}
	}
	if def := DefaultPermissionsLayout(); def.EtcDir != "/etc/dso" || def.StateDir != "/var/lib/dso" || def.LedgerDir != "/var/lib/dso/ledger" {
		t.Fatalf("default layout drifted: %+v", def)
	}
}

func TestPermissionsRepair_RootOnlyOwnershipChange(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("SKIPPED (root-only): changing a directory's owner to root:<gid> needs root; owner comparison, dry-run, refusal and no-recursion rules run unprivileged in the other tests")
	}
	f := newPermFixture(t)
	f.mkdir(f.layout.EtcDir, 0o775)
	f.write(filepath.Join(f.layout.EtcDir, "dso.yaml"), []byte("x"), 0o600)
	if err := os.Chown(f.layout.EtcDir, 1234, 1234); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(filepath.Join(f.layout.EtcDir, "dso.yaml"), 1234, 1234); err != nil {
		t.Fatal(err)
	}
	prod := NewPermissionsRepairerWith(PermissionsRepairerOptions{Layout: &f.layout, LookupGID: func() (int, bool) { return 4321, true }})
	if _, err := prod.Repair(false); err != nil {
		t.Fatal(err)
	}
	st := mustStatT(t, f.layout.EtcDir)
	if st.Uid != 0 || st.Gid != 4321 || modeOfPath(t, f.layout.EtcDir) != 0o750 {
		t.Fatalf("directory is %d:%d %04o", st.Uid, st.Gid, modeOfPath(t, f.layout.EtcDir))
	}
	if c := mustStatT(t, filepath.Join(f.layout.EtcDir, "dso.yaml")); c.Uid != 1234 {
		t.Fatalf("a child was chowned to %d", c.Uid)
	}
}

func mustStatT(t *testing.T, p string) *syscall.Stat_t {
	t.Helper()
	fi, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Sys().(*syscall.Stat_t)
}
