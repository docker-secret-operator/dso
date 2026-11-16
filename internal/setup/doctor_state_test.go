package setup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/docker-secret-operator/dso/internal/bootstrap"
	"github.com/docker-secret-operator/dso/internal/securefs"
	"github.com/docker-secret-operator/dso/pkg/api"
)

// State-directory (DSO-DOCTOR-025 / REPAIR-STATE-001) and ledger availability
// (DSO-DOCTOR-024) tests. Temporary directories only; an injectable owner stands
// in for root so nothing needs privileges. Root-only behaviour (a real chown to
// root) is covered in internal/bootstrap and skips visibly there.

func stateChecks(t *testing.T, dir string, rootOwned bool) *StateDirChecks {
	t.Helper()
	sc := newStateDirChecks(ModeAgent, dir)
	sc.lookupGID = func() (int, bool) { return os.Getgid(), true }
	if rootOwned {
		sc.statOwnerF = func(fi os.FileInfo) (uint32, uint32, bool) {
			_, gid, ok := statOwner(fi)
			return 0, gid, ok // pretend root owns it; the group is the real one
		}
	}
	return sc
}

func newStateDir(t *testing.T, mode os.FileMode) string {
	t.Helper()
	d, err := os.MkdirTemp("", "dso-state-")
	if err != nil {
		t.Skipf("no temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	if err := os.Chmod(d, 0o700); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(d, "lib")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, mode); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestStateDirCheck_025(t *testing.T) {
	t.Run("0770 warns with actual and expected", func(t *testing.T) {
		c := stateChecks(t, newStateDir(t, 0o770), true).checkStateDir()
		assertCheckID(t, c, "DSO-DOCTOR-025")
		assertWarn(t, c)
		for _, want := range []string{"0770", "writable by group or others", "expected root:dso, mode 0750"} {
			if !strings.Contains(c.Detail, want) {
				t.Errorf("detail %q lacks %q", c.Detail, want)
			}
		}
		if !strings.Contains(c.RootCause, "ledger") {
			t.Errorf("root cause must name the ledger consequence: %q", c.RootCause)
		}
		joined := strings.Join(c.Recovery, "\n")
		if !strings.Contains(joined, "bootstrap agent") || !strings.Contains(joined, "never -R") || strings.Contains(joined, "chmod -R") {
			t.Errorf("recovery must name the supported path and never advise recursion: %q", joined)
		}
	})
	t.Run("0750 root passes", func(t *testing.T) {
		assertPass(t, stateChecks(t, newStateDir(t, 0o750), true).checkStateDir())
	})
	t.Run("0700 root passes (stricter)", func(t *testing.T) {
		assertPass(t, stateChecks(t, newStateDir(t, 0o700), true).checkStateDir())
	})
	t.Run("0755 warns (others access)", func(t *testing.T) {
		assertWarn(t, stateChecks(t, newStateDir(t, 0o755), true).checkStateDir())
	})
	t.Run("foreign owner warns", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("SKIPPED (non-root only): the real owner is root here")
		}
		c := stateChecks(t, newStateDir(t, 0o750), false).checkStateDir()
		assertWarn(t, c)
		if !strings.Contains(c.Detail, "expected root") {
			t.Errorf("detail %q", c.Detail)
		}
	})
	t.Run("missing is info", func(t *testing.T) {
		assertInfo(t, stateChecks(t, filepath.Join(os.TempDir(), "dso-state-missing-xyz"), true).checkStateDir())
	})
	t.Run("local mode not applicable", func(t *testing.T) {
		sc := stateChecks(t, newStateDir(t, 0o770), true)
		sc.mode = ModeLocal
		assertInfo(t, sc.checkStateDir())
	})
	t.Run("symlink fails", func(t *testing.T) {
		dir := newStateDir(t, 0o750)
		link := filepath.Join(filepath.Dir(dir), "lnk")
		if err := os.Symlink(dir, link); err != nil {
			t.Fatal(err)
		}
		assertFail(t, stateChecks(t, link, true).checkStateDir())
	})
}

func statSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil || p == root {
			return err
		}
		st := fi.Sys().(*syscall.Stat_t)
		out[p] = strings.Join([]string{fi.Mode().String(), string(rune(st.Uid)), string(rune(st.Gid))}, "|")
		return nil
	})
	return out
}

func populateChildren(t *testing.T, dir string) {
	t.Helper()
	for rel, mode := range map[string]os.FileMode{
		"plugins/p": 0o755, "state/rotations.json": 0o600, "vault.db": 0o666, "ledger/ledger.key": 0o400,
	} {
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
}

func repairFor(t *testing.T, dir string, calls *[]string) (*Repair, *StateDirChecks) {
	t.Helper()
	sc := stateChecks(t, dir, true)
	r := NewRepair(RepairOptions{Mode: ModeAgent, StateDir: dir})
	r.state.lookupGID = sc.lookupGID
	// The owner target is the test user (a non-root test cannot chown to root).
	r.state.apply = func(p string, perm os.FileMode, _, gid int) (bool, error) {
		*calls = append(*calls, p)
		return bootstrapApply(p, perm, os.Getuid(), gid)
	}
	r.runDoctor = func(ctx context.Context) *DoctorResult {
		return &DoctorResult{Checks: sc.run(ctx)}
	}
	return r, sc
}

func TestRepairState_025_OnlyTheDirectoryChanges_Idempotent(t *testing.T) {
	dir := newStateDir(t, 0o700)
	populateChildren(t, dir)
	if err := os.Chmod(dir, 0o770); err != nil {
		t.Fatal(err)
	}
	before := statSnapshot(t, dir)
	var calls []string
	r, sc := repairFor(t, dir, &calls)

	doctor := &DoctorResult{Checks: sc.run(context.Background())}
	plan := r.Plan(context.Background(), doctor)
	if len(plan.Issues) != 1 || plan.Issues[0].ID != "REPAIR-STATE-001" || !plan.Issues[0].RequiresConfirmation {
		t.Fatalf("plan %+v", plan.Issues)
	}
	res := r.Execute(context.Background(), plan, AlwaysConfirm)
	if res.Summary.Applied != 1 || res.Summary.Failed != 0 {
		t.Fatalf("summary %+v failed %+v", res.Summary, res.Failed)
	}
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o750 {
		t.Fatalf("mode %04o", fi.Mode().Perm())
	}
	if len(calls) != 1 || calls[0] != dir {
		t.Fatalf("apply calls %v: exactly the directory, never a child", calls)
	}
	after := statSnapshot(t, dir)
	for p, b := range before {
		if after[p] != b {
			t.Errorf("%s was touched: %s -> %s", p, b, after[p])
		}
	}
	for _, c := range res.Verification.Checks {
		if c.ID == "DSO-DOCTOR-025" && c.Status != DoctorPass {
			t.Fatalf("verification after repair: %+v", c)
		}
	}

	// Second run: nothing to plan, nothing applied.
	calls = nil
	plan = r.Plan(context.Background(), &DoctorResult{Checks: sc.run(context.Background())})
	if len(plan.Issues) != 0 {
		t.Fatalf("a repaired directory still plans work: %+v", plan.Issues)
	}
	if err := r.state.repairStateDir(); err != nil { // forced re-apply is a no-op
		t.Fatal(err)
	}
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o750 {
		t.Fatalf("mode %04o after a forced second apply", fi.Mode().Perm())
	}
}

func TestRepairState_025_DeclinedLeavesEverything(t *testing.T) {
	dir := newStateDir(t, 0o770)
	var calls []string
	r, sc := repairFor(t, dir, &calls)
	plan := r.Plan(context.Background(), &DoctorResult{Checks: sc.run(context.Background())})
	res := r.Execute(context.Background(), plan, NeverConfirm)
	if res.Summary.Declined != 1 || len(calls) != 0 {
		t.Fatalf("summary %+v calls %v", res.Summary, calls)
	}
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o770 {
		t.Fatalf("a declined repair changed the mode to %04o", fi.Mode().Perm())
	}
}

func TestRepairState_025_SymlinkRefused(t *testing.T) {
	real := newStateDir(t, 0o770)
	link := filepath.Join(filepath.Dir(real), "lnk")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	rs := newRepairState(link)
	rs.lookupGID = func() (int, bool) { return os.Getgid(), true }
	if err := rs.repairStateDir(); err == nil {
		t.Fatal("a symlinked state directory was repaired")
	}
	if fi, _ := os.Stat(real); fi.Mode().Perm() != 0o770 {
		t.Fatalf("the symlink target was changed to %04o", fi.Mode().Perm())
	}
}

func TestRepairState_025_LedgerDirChainAcceptsTheRepairedParent(t *testing.T) {
	if !securefs.Supported {
		t.Skip("securefs unsupported")
	}
	dir := newStateDir(t, 0o700)
	ldir := filepath.Join(dir, "ledger")
	if err := os.Mkdir(ldir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o770); err != nil {
		t.Fatal(err)
	}
	if securefs.CheckDirChain(ldir) == nil {
		t.Fatal("a group-writable parent was accepted before the repair")
	}
	var calls []string
	r, _ := repairFor(t, dir, &calls)
	if err := r.state.repairStateDir(); err != nil {
		t.Fatal(err)
	}
	if err := securefs.CheckDirChain(ldir); err != nil {
		t.Fatalf("the repaired chain is refused: %v", err)
	}
}

func TestRotationCheck_024_Ledger(t *testing.T) {
	rc := newTestRotationChecks()
	t.Run("active", func(t *testing.T) {
		c := rc.checkLedger(&api.StatusResponse{Ledger: api.LedgerStatusInfo{State: api.LedgerActive}}, nil)
		assertCheckID(t, c, "DSO-DOCTOR-024")
		assertPass(t, c)
	})
	t.Run("disabled warns with code, cause, degraded behaviour and fix", func(t *testing.T) {
		c := rc.checkLedger(&api.StatusResponse{Ledger: api.LedgerStatusInfo{
			State: api.LedgerDisabled, Code: "unsafe_permissions", Message: "/var/lib/dso: writable by group or others",
			Remediation: "chmod 0750 /var/lib/dso", Degraded: "no mass rotation",
		}}, nil)
		assertWarn(t, c)
		if !strings.Contains(c.Detail, "unsafe_permissions") || !strings.Contains(c.RootCause, "no mass rotation") {
			t.Errorf("%+v", c)
		}
		if len(c.Recovery) == 0 || c.Recovery[0] != "chmod 0750 /var/lib/dso" {
			t.Errorf("recovery %v", c.Recovery)
		}
	})
	t.Run("not configured is info", func(t *testing.T) {
		assertInfo(t, rc.checkLedger(&api.StatusResponse{Ledger: api.LedgerStatusInfo{State: api.LedgerNotConfigured}}, nil))
	})
	t.Run("older agent is info", func(t *testing.T) {
		assertInfo(t, rc.checkLedger(&api.StatusResponse{}, nil))
	})
	t.Run("agent unreachable is info", func(t *testing.T) {
		assertInfo(t, rc.checkLedger(nil, os.ErrNotExist))
	})
}

// bootstrapApply is the production primitive under its test-local name.
var bootstrapApply = bootstrap.ApplyDirectoryNoFollow
