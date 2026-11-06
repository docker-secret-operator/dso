//go:build unix

package sqlite

// Tests for displacement (displace.go): fault injection at every boundary, real
// kernel no-replace renames, real SIGKILLs at every step, and the lock guard.
// Files are ordinary random bytes: displacement never opens a database through
// SQLite, so no SQLite fixture is needed. Everything lives in test temp dirs.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// ---- fixtures -------------------------------------------------------------------------

func dsSfx(name string) string {
	switch name {
	case "main":
		return ""
	case "wal":
		return "-wal"
	case "journal":
		return "-journal"
	case "shm":
		return "-shm"
	}
	return "?"
}

type dsEnv struct {
	t    *testing.T
	dir  string
	db   string
	cand string
	lock *RestoreLock
	orig map[string][]byte // suffix -> original bytes
}

func newDS(t *testing.T, wal, shm, journal bool) *dsEnv {
	t.Helper()
	dir, db := pvDB(t, wal, shm, journal)
	cand := filepath.Join(pvDir(t), "cand.bin")
	pvWrite(t, cand, pvBytes(5000))
	lock, err := AcquireExclusiveCentralLock(db, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Close() })
	e := &dsEnv{t: t, dir: dir, db: db, cand: cand, lock: lock, orig: map[string][]byte{}}
	for _, sfx := range []string{"", "-wal", "-shm", "-journal"} {
		if b, err := os.ReadFile(db + sfx); err == nil {
			e.orig[sfx] = b
		}
	}
	return e
}

func (e *dsEnv) candBytes() []byte {
	b, err := os.ReadFile(e.cand)
	if err != nil {
		e.t.Fatal(err)
	}
	return b
}

func (e *dsEnv) prep(d *displacer) *Replacement {
	e.t.Helper()
	r, err := d.prepare(e.db, e.cand)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = r.Abandon() })
	return r
}

func (e *dsEnv) base() *Baseline {
	e.t.Helper()
	b, err := e.lock.BaselineAfterClose(e.db)
	if err != nil {
		e.t.Fatal(err)
	}
	return b
}

func (e *dsEnv) asides() []string {
	m, _ := filepath.Glob(e.db + ".replaced-*")
	return m
}

func (e *dsEnv) temps() []string {
	m, _ := filepath.Glob(e.db + ".restoring-*")
	return m
}

// assertAllInPlace: every original is back at its original path with its bytes,
// and no aside directory remains.
func (e *dsEnv) assertAllInPlace() {
	e.t.Helper()
	for sfx, want := range e.orig {
		got, err := os.ReadFile(e.db + sfx)
		if err != nil || !bytes.Equal(got, want) {
			e.t.Fatalf("%q is not back in place with its original bytes (%v)", sfx, err)
		}
	}
	// Nothing may sit at an original path that did not hold a file originally.
	for _, sfx := range []string{"", "-wal", "-shm", "-journal"} {
		if _, had := e.orig[sfx]; had {
			continue
		}
		if _, err := os.Lstat(e.db + sfx); !os.IsNotExist(err) {
			e.t.Fatalf("%q must not exist: it held no file originally (%v)", sfx, err)
		}
	}
	if a := e.asides(); len(a) != 0 {
		e.t.Fatalf("an aside directory remains: %v", a)
	}
}

// assertRetained: aside/<name> holds the original bytes for each name.
func (e *dsEnv) assertRetained(aside string, names ...string) {
	e.t.Helper()
	for _, n := range names {
		got, err := os.ReadFile(filepath.Join(aside, n))
		if err != nil || !bytes.Equal(got, e.orig[dsSfx(n)]) {
			e.t.Fatalf("%s is not retained in %s with its original bytes (%v)", n, aside, err)
		}
	}
}

func (e *dsEnv) assertDBIsCandidate() {
	e.t.Helper()
	got, err := os.ReadFile(e.db)
	if err != nil || !bytes.Equal(got, e.candBytes()) {
		e.t.Fatalf("the database path does not hold the candidate (%v)", err)
	}
}

type dsCalls struct{ list [][2]string }

// renamer records every move and fails the ones fail() selects BEFORE they take
// effect; otherwise it performs the real no-replace rename.
func (c *dsCalls) renamer(fail func(n int, oldp, newp string) error) func(string, string) error {
	return func(a, b string) error {
		c.list = append(c.list, [2]string{a, b})
		if fail != nil {
			if err := fail(len(c.list), a, b); err != nil {
				return err
			}
		}
		return moveNoReplace(a, b)
	}
}

// ---- explicit seams: files and directories are selected by NAME, not by position ----------------------------
//
// Tests that depend on a sequence (a failure, then the rollback, then the
// compensation) select each step by what is being moved and in which direction,
// not by its index in the call sequence, so adding or reordering an unrelated
// operation cannot silently change what a test exercises.

// isAside reports whether p is inside this environment's aside directory.
func (e *dsEnv) isAside(p string) bool { return strings.HasPrefix(p, e.db+".replaced-") }

// fwd: the forward displacement of the file with suffix sfx into the aside directory.
func (e *dsEnv) fwd(a, b, sfx string) bool { return a == e.db+sfx && e.isAside(b) }

// back: the rollback moving the retained file with suffix sfx back to its original path.
func (e *dsEnv) back(a, b, sfx string) bool { return e.isAside(a) && b == e.db+sfx }

// syncFailing fails the nth (1-based) directory sync of one class of directory: "parent"
// (the database's directory) or "aside" (the displaced-files directory). In a forward run
// the parent is synced after the mkdir (1st), before the installation (2nd) and after it
// (3rd); the aside directory is synced once, before the installation.
func (e *dsEnv) syncFailing(class string, nth int, err error) func(string) error {
	counts := map[string]int{}
	return func(dir string) error {
		c := ""
		switch {
		case dir == e.dir:
			c = "parent"
		case e.isAside(dir):
			c = "aside"
		}
		counts[c]++
		if c == class && counts[c] == nth {
			return err
		}
		return fsyncDir(dir)
	}
}

var errInjected = errors.New("injected failure")

func asDisplacement(t *testing.T, err error) *DisplacementError {
	t.Helper()
	var de *DisplacementError
	if !errors.As(err, &de) {
		t.Fatalf("want *DisplacementError, got %T: %v", err, err)
	}
	return de
}

// ---- the happy path and NoActive -----------------------------------------------------------

func TestDisplace_HappyPath_EveryOriginalFileIsDisplacedAndRetained(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, true, true)
	d := newDisplacer()
	repl := e.prep(d)
	res, err := d.replace(e.db, repl, e.base())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Installed || res.Dir == "" || len(res.Files) != 4 || len(res.Vanished) != 0 {
		t.Fatalf("result: %+v", res)
	}
	e.assertDBIsCandidate()
	for _, sfx := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(e.db + sfx); !os.IsNotExist(err) {
			t.Fatalf("%s must be gone from the original path (%v)", sfx, err)
		}
	}
	e.assertRetained(res.Dir, "main", "wal", "journal", "shm")
	if fi, err := os.Stat(res.Dir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("aside directory mode: %v %v", fi, err)
	}
	if len(e.temps()) != 0 {
		t.Fatalf("the temporary file must have become the database: %v", e.temps())
	}
}

func TestDisplace_NoActiveDatabase_InstallsWithNoAsideDirectory(t *testing.T) {
	t.Parallel()
	e := newDS(t, false, false, false)
	if err := os.Remove(e.db); err != nil {
		t.Fatal(err)
	}
	delete(e.orig, "")
	b, err := e.lock.BaselineNoActive(e.db)
	if err != nil {
		t.Fatal(err)
	}
	d := newDisplacer()
	res, err := d.replace(e.db, e.prep(d), b)
	if err != nil || !res.Installed || res.Dir != "" || len(e.asides()) != 0 {
		t.Fatalf("res %+v err %v asides %v", res, err, e.asides())
	}
	e.assertDBIsCandidate()
}

func TestDisplace_NoActiveDatabase_EmptyCompanionIsDisplacedNotDeleted(t *testing.T) {
	t.Parallel()
	e := newDS(t, false, false, false)
	if err := os.Remove(e.db); err != nil {
		t.Fatal(err)
	}
	pvWrite(t, e.db+"-wal", nil)
	b, err := e.lock.BaselineNoActive(e.db)
	if err != nil {
		t.Fatal(err)
	}
	d := newDisplacer()
	res, err := d.replace(e.db, e.prep(d), b)
	if err != nil || !res.Installed || len(res.Files) != 1 || res.Files[0].Name != "wal" {
		t.Fatalf("res %+v err %v", res, err)
	}
	if _, err := os.Lstat(res.Files[0].Retained); err != nil {
		t.Fatalf("the empty -wal must be retained, not deleted: %v", err)
	}
}

func TestDisplace_BaselineNoActive_RefusesWhenAFileAppeared(t *testing.T) {
	t.Parallel()
	e := newDS(t, false, false, false)
	if _, err := e.lock.BaselineNoActive(e.db); !errors.Is(err, ErrDisplaceBaselineChanged) {
		t.Fatalf("a main file exists, want ErrDisplaceBaselineChanged, got %v", err)
	}
}

func TestDisplace_NoActive_InstallBlockedByANewFile_NeverOverwrites(t *testing.T) {
	t.Parallel()
	e := newDS(t, false, false, false)
	if err := os.Remove(e.db); err != nil {
		t.Fatal(err)
	}
	b, err := e.lock.BaselineNoActive(e.db)
	if err != nil {
		t.Fatal(err)
	}
	d := newDisplacer()
	repl := e.prep(d)
	d.beforeInstall = func() { pvWrite(t, e.db, []byte("someone else's database")) }
	res, err := d.replace(e.db, repl, b)
	if res.Installed || !errors.Is(err, ErrDisplaceBlocked) {
		t.Fatalf("res %+v err %v", res, err)
	}
	if got, _ := os.ReadFile(e.db); string(got) != "someone else's database" {
		t.Fatal("the file that appeared was overwritten")
	}
	if !asDisplacement(t, err).RolledBack {
		t.Fatal("nothing was moved, so nothing needs rolling back")
	}
}

// ---- temporary file failures ---------------------------------------------------------------

func TestDisplace_TempFileFailures_LeaveOriginalsUntouched(t *testing.T) {
	t.Parallel()
	failing := func(after int) func(io.Writer) io.Writer {
		return func(w io.Writer) io.Writer { return &pvFailingWriter{w: w, after: after, err: errInjected} }
	}
	cases := map[string]func(d *displacer){
		"create": func(d *displacer) {
			d.createFile = func(string, os.FileMode) (*os.File, error) { return nil, errInjected }
		},
		"copy":  func(d *displacer) { d.wrapWriter = failing(100) },
		"sync":  func(d *displacer) { d.syncFile = func(*os.File) error { return errInjected } },
		"close": func(d *displacer) { d.closeFile = func(f *os.File) error { _ = f.Close(); return errInjected } },
	}
	for name, mod := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newDS(t, true, true, true)
			before := pvStates(t, e.db)
			d := newDisplacer()
			mod(d)
			if _, err := d.prepare(e.db, e.cand); !errors.Is(err, errInjected) {
				t.Fatalf("want the injected error, got %v", err)
			}
			pvSame(t, before, pvStates(t, e.db))
			if len(e.temps()) != 0 || len(e.asides()) != 0 {
				t.Fatalf("leftovers: temps %v asides %v", e.temps(), e.asides())
			}
		})
	}
}

func TestDisplace_MissingCandidate_LeavesEverythingUntouched(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, true, true)
	before := pvStates(t, e.db)
	if _, err := e.lock.PrepareReplacement(e.db, filepath.Join(e.dir, "does-not-exist")); err == nil {
		t.Fatal("a missing candidate must fail")
	}
	pvSame(t, before, pvStates(t, e.db))
	e.assertAllInPlace()
}

// ---- aside directory failures ----------------------------------------------------------------

func TestDisplace_AsideDirectoryFailures(t *testing.T) {
	t.Parallel()
	t.Run("mkdir fails", func(t *testing.T) {
		t.Parallel()
		e := newDS(t, true, true, true)
		d := newDisplacer()
		repl := e.prep(d)
		d.mkdir = func(string, os.FileMode) error { return errInjected }
		_, err := d.replace(e.db, repl, e.base())
		if !errors.Is(err, errInjected) || !asDisplacement(t, err).RolledBack {
			t.Fatalf("got %v", err)
		}
		e.assertAllInPlace()
	})
	t.Run("parent sync fails: the empty directory this call created is removed", func(t *testing.T) {
		t.Parallel()
		e := newDS(t, true, true, true)
		d := newDisplacer()
		repl := e.prep(d)
		d.syncDir = e.syncFailing("parent", 1, errInjected)
		_, err := d.replace(e.db, repl, e.base())
		if !errors.Is(err, errInjected) || !asDisplacement(t, err).RolledBack {
			t.Fatalf("got %v", err)
		}
		e.assertAllInPlace()
	})
	t.Run("an existing directory of the same name is never touched", func(t *testing.T) {
		t.Parallel()
		e := newDS(t, true, false, false)
		d := newDisplacer()
		repl := e.prep(d)
		d.random = func(b []byte) (int, error) { return copy(b, []byte{1, 2, 3, 4}), nil }
		d.now = func() time.Time { return time.Unix(1700000000, 0) }
		name := fmt.Sprintf("%s.replaced-%s-%s", e.db, "20231114T221320Z", "01020304")
		if err := os.Mkdir(name, 0o700); err != nil {
			t.Fatal(err)
		}
		pvWrite(t, filepath.Join(name, "keep"), []byte("precious"))
		_, err := d.replace(e.db, repl, e.base())
		if err == nil {
			t.Fatal("must refuse")
		}
		if got, _ := os.ReadFile(filepath.Join(name, "keep")); string(got) != "precious" {
			t.Fatal("the existing directory was touched")
		}
		for sfx, want := range e.orig {
			if got, _ := os.ReadFile(e.db + sfx); !bytes.Equal(got, want) {
				t.Fatalf("original %q changed", sfx)
			}
		}
	})
}

// ---- move failures and reverse-order rollback ------------------------------------------------------

func TestDisplace_MainMoveFails_NothingMoved(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, true, true)
	d := newDisplacer()
	repl := e.prep(d)
	var calls dsCalls
	d.rename = calls.renamer(func(n int, _, _ string) error {
		if n == 1 {
			return errInjected
		}
		return nil
	})
	_, err := d.replace(e.db, repl, e.base())
	if !errors.Is(err, errInjected) || !asDisplacement(t, err).RolledBack || len(calls.list) != 1 {
		t.Fatalf("err %v calls %v", err, calls.list)
	}
	e.assertAllInPlace()
}

func TestDisplace_CompanionMoveFails_RollsBackInReverseOrder(t *testing.T) {
	t.Parallel()
	// The failing move is selected by NAME. The approved displacement order is main, then
	// -wal, -journal, -shm; what is asserted is derived from what actually happened.
	for _, failing := range []string{"-wal", "-journal", "-shm"} {
		t.Run("the move of "+failing+" fails", func(t *testing.T) {
			t.Parallel()
			e := newDS(t, true, true, true)
			d := newDisplacer()
			repl := e.prep(d)
			var calls dsCalls
			d.rename = calls.renamer(func(_ int, a, b string) error {
				if e.fwd(a, b, failing) {
					return errInjected
				}
				return nil
			})
			_, err := d.replace(e.db, repl, e.base())
			if !errors.Is(err, errInjected) || !asDisplacement(t, err).RolledBack {
				t.Fatalf("got %v", err)
			}
			e.assertAllInPlace()
			// The forward moves that completed, in order, and the moves back. The moves back
			// must be exactly the reverse of the completed forward moves, so the main file,
			// which moved first, is the last to return.
			var forwardDone, back []string
			for _, c := range calls.list {
				switch {
				case e.isAside(c[1]) && c[0] != e.db+failing:
					forwardDone = append(forwardDone, c[0])
				case e.isAside(c[0]):
					back = append(back, c[1])
				}
			}
			var want []string
			for i := len(forwardDone) - 1; i >= 0; i-- {
				want = append(want, forwardDone[i])
			}
			if len(forwardDone) == 0 || forwardDone[0] != e.db || len(back) == 0 || back[len(back)-1] != e.db {
				t.Fatalf("the main file must move first and return last: forward %v back %v", forwardDone, back)
			}
			if fmt.Sprint(back) != fmt.Sprint(want) {
				t.Fatalf("rollback order %v, want the reverse of %v", back, forwardDone)
			}
		})
	}
}

func TestDisplace_CompanionVanishedBeforeItsMove_IsReported(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, true, true)
	d := newDisplacer()
	repl := e.prep(d)
	var calls dsCalls
	d.rename = calls.renamer(func(_ int, oldp, _ string) error {
		if oldp == e.db+"-wal" {
			_ = os.Remove(oldp) // gone between the examination and the move
		}
		return nil
	})
	res, err := d.replace(e.db, repl, e.base())
	if err != nil || !res.Installed {
		t.Fatalf("res %+v err %v", res, err)
	}
	if len(res.Vanished) != 1 || res.Vanished[0] != "wal" || len(res.Files) != 3 {
		t.Fatalf("vanished %v files %v", res.Vanished, res.Files)
	}
	e.assertRetained(res.Dir, "main", "journal", "shm")
}

func TestDisplace_NonRegularSource_IsRefusedBeforeAnythingMoves(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, false, false)
	d := newDisplacer()
	repl := e.prep(d)
	b := e.base()
	if err := os.Symlink("/etc/hostname", e.db+"-shm"); err != nil { // absent from the baseline
		t.Fatal(err)
	}
	_, err := d.replace(e.db, repl, b)
	if !errors.Is(err, ErrSidecarNotRegular) || !asDisplacement(t, err).RolledBack || len(e.asides()) != 0 {
		t.Fatalf("got %v asides %v", err, e.asides())
	}
	if got, _ := os.ReadFile(e.db); !bytes.Equal(got, e.orig[""]) {
		t.Fatal("the main file changed")
	}
}

// ---- directory sync failures ---------------------------------------------------------------------------

func TestDisplace_SyncFailureBeforeInstall_RollsBackAndInstallsNothing(t *testing.T) {
	t.Parallel()
	// Selected by directory class and by which sync of that directory, not by a global index.
	for _, c := range []struct {
		name  string
		class string
		nth   int
	}{
		{"the aside directory's sync", "aside", 1},
		{"the parent's second sync (just before the installation)", "parent", 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			e := newDS(t, true, true, true)
			d := newDisplacer()
			repl := e.prep(d)
			d.syncDir = e.syncFailing(c.class, c.nth, errInjected)
			res, err := d.replace(e.db, repl, e.base())
			if res.Installed || !errors.Is(err, errInjected) || !asDisplacement(t, err).RolledBack {
				t.Fatalf("res %+v err %v", res, err)
			}
			e.assertAllInPlace()
			if _, statErr := os.Lstat(repl.path); statErr != nil {
				t.Fatalf("the prepared replacement must be untouched: %v", statErr)
			}
		})
	}
}

func TestDisplace_SyncFailureAfterInstall_IsTypedAndTheReplacementIsInstalled(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, true, true)
	d := newDisplacer()
	repl := e.prep(d)
	d.syncDir = e.syncFailing("parent", 3, errInjected) // the parent's third sync: after the installation
	res, err := d.replace(e.db, repl, e.base())
	var ide *InstallDurabilityError
	if !errors.As(err, &ide) || !errors.Is(err, errInjected) {
		t.Fatalf("want *InstallDurabilityError, got %T %v", err, err)
	}
	var de *DisplacementError
	if errors.As(err, &de) {
		t.Fatal("a durability failure after installation must not be reported as an install failure")
	}
	if !res.Installed {
		t.Fatal("the replacement is installed")
	}
	e.assertDBIsCandidate()
	e.assertRetained(res.Dir, "main", "wal", "journal", "shm")
}

// ---- install blocked, rollback blocked, compensation -----------------------------------------------------------

func TestDisplace_InstallBlockedByANewFile_RollbackIsRefusedAndEverythingIsRetained(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, true, true)
	d := newDisplacer()
	repl := e.prep(d)
	d.beforeInstall = func() { pvWrite(t, e.db, []byte("someone else's database")) }
	res, err := d.replace(e.db, repl, e.base())
	if res.Installed || !errors.Is(err, ErrDisplaceBlocked) || !errors.Is(err, ErrRollbackIncomplete) {
		t.Fatalf("res %+v err %v", res, err)
	}
	de := asDisplacement(t, err)
	if de.RolledBack || len(de.Retained) != 4 || de.Dir == "" {
		t.Fatalf("%+v", de)
	}
	if got, _ := os.ReadFile(e.db); string(got) != "someone else's database" {
		t.Fatal("the newcomer was overwritten")
	}
	for _, sfx := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(e.db + sfx); !os.IsNotExist(err) {
			t.Fatalf("an old %s was put back beside a database it does not belong to", sfx)
		}
	}
	e.assertRetained(de.Dir, "main", "wal", "journal", "shm")
	for _, want := range []string{de.Dir, "main", "wal", "journal", "shm"} {
		if !bytes.Contains([]byte(err.Error()), []byte(want)) {
			t.Fatalf("the error must name every retained location, missing %q: %v", want, err)
		}
	}
}

func TestDisplace_RollbackCompensation_WhenTheMainPathIsTakenDuringRollback(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, true, true)
	d := newDisplacer()
	repl := e.prep(d)
	var calls dsCalls
	// The -shm move fails, so the rollback begins: the journal and the -wal go back, and
	// when the main file is about to return a newcomer takes its path first, so that
	// restore is refused and the companions already put back are displaced again.
	d.rename = calls.renamer(func(_ int, a, b string) error {
		switch {
		case e.fwd(a, b, "-shm"):
			return errInjected
		case e.back(a, b, ""):
			pvWrite(t, e.db, []byte("someone else's database"))
		}
		return nil
	})
	_, err := d.replace(e.db, repl, e.base())
	if !errors.Is(err, errInjected) || !errors.Is(err, ErrRollbackIncomplete) {
		t.Fatalf("got %v", err)
	}
	de := asDisplacement(t, err)
	if de.RolledBack {
		t.Fatal("the rollback did not complete")
	}
	if got, _ := os.ReadFile(e.db); string(got) != "someone else's database" {
		t.Fatal("the newcomer was overwritten")
	}
	for _, sfx := range []string{"-wal", "-journal"} {
		if _, err := os.Lstat(e.db + sfx); !os.IsNotExist(err) {
			t.Fatalf("the companion %s was left beside the newcomer (compensation failed)", sfx)
		}
	}
	e.assertRetained(de.Dir, "main", "wal", "journal")
	if len(de.NotMoved) != 1 || de.NotMoved[0] != e.db+"-shm" {
		t.Fatalf("the never-moved -shm must be reported: %v", de.NotMoved)
	}
}

// ---- tripwire and post-move verification --------------------------------------------------------------------------

func TestDisplace_PreInstallTripwire_DetectsAChangedMainOrCompanion(t *testing.T) {
	t.Parallel()
	cases := map[string]func(e *dsEnv){
		"main appended":      func(e *dsEnv) { pvAppend(t, e.db, []byte("late")) },
		"wal appended":       func(e *dsEnv) { pvAppend(t, e.db+"-wal", []byte("late")) },
		"journal appended":   func(e *dsEnv) { pvAppend(t, e.db+"-journal", []byte("late")) },
		"main swapped":       func(e *dsEnv) { swap := e.db + ".other"; pvWrite(t, swap, e.orig[""]); _ = os.Rename(swap, e.db) },
		"journal appeared":   nil,
		"wal removed":        func(e *dsEnv) { _ = os.Remove(e.db + "-wal") },
		"main became a link": func(e *dsEnv) { _ = os.Remove(e.db); _ = os.Symlink("/etc/hostname", e.db) },
	}
	for name, mod := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			hasJournal := name != "journal appeared"
			e := newDS(t, true, true, hasJournal)
			d := newDisplacer()
			repl := e.prep(d)
			b := e.base()
			if name == "journal appeared" {
				pvWrite(t, e.db+"-journal", pvBytes(100))
			} else {
				mod(e)
			}
			res, err := d.replace(e.db, repl, b)
			if res.Installed || !errors.Is(err, ErrDisplaceBaselineChanged) {
				t.Fatalf("res %+v err %v", res, err)
			}
			if len(e.asides()) != 0 {
				t.Fatalf("nothing may have moved: %v", e.asides())
			}
			if _, statErr := os.Lstat(e.db + "-wal"); name != "wal removed" && statErr != nil {
				t.Fatalf("the -wal moved: %v", statErr)
			}
		})
	}
}

func TestDisplace_PostMoveVerification_RollsBackAndKeepsTheLateWrite(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, true, true)
	d := newDisplacer()
	repl := e.prep(d)
	d.hook = func(step string) {
		if step == "main-moved" { // a writer with the old file still open
			pvAppend(t, filepath.Join(e.asides()[0], "main"), []byte("late write"))
		}
	}
	res, err := d.replace(e.db, repl, e.base())
	if res.Installed || !errors.Is(err, ErrDisplaceBaselineChanged) || !asDisplacement(t, err).RolledBack {
		t.Fatalf("res %+v err %v", res, err)
	}
	got, _ := os.ReadFile(e.db)
	if !bytes.Equal(got, append(append([]byte{}, e.orig[""]...), []byte("late write")...)) {
		t.Fatal("the late write must be kept in the restored original, not lost")
	}
	for _, sfx := range []string{"-wal", "-shm", "-journal"} {
		if got, _ := os.ReadFile(e.db + sfx); !bytes.Equal(got, e.orig[sfx]) {
			t.Fatalf("%s is not back in place", sfx)
		}
	}
}

func TestDisplace_Baseline_SignatureHashSignature_DetectsAWriteDuringTheHash(t *testing.T) {
	t.Parallel()
	_, db := pvDB(t, false, false, false)
	bl := &baseliner{afterHash: func() { pvAppend(t, db, []byte("x")) }}
	if _, err := bl.file(db, true); !errors.Is(err, ErrDisplaceBaselineChanged) {
		t.Fatalf("a write between the hash and the second signature must fail the capture, got %v", err)
	}
	if bf, err := (&baseliner{}).file(db, true); err != nil || !bf.exists || bf.hash == "" {
		t.Fatalf("an undisturbed capture must succeed: %+v %v", bf, err)
	}
}

func TestDisplace_VerifiedBaseline_MatchesThePreservationExactly(t *testing.T) {
	t.Parallel()
	t.Run("matches", func(t *testing.T) {
		t.Parallel()
		_, db := pvDB(t, true, true, false)
		p := pvMustPreserve(t, pvTest(), db)
		if _, err := p.VerifiedBaseline(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("main changed", func(t *testing.T) {
		t.Parallel()
		_, db := pvDB(t, true, false, false)
		p := pvMustPreserve(t, pvTest(), db)
		pvAppend(t, db, []byte("late"))
		if _, err := p.VerifiedBaseline(); !errors.Is(err, ErrPreserveSourceChanged) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("a wal appeared", func(t *testing.T) {
		t.Parallel()
		_, db := pvDB(t, false, false, false)
		p := pvMustPreserve(t, pvTest(), db)
		pvWrite(t, db+"-wal", pvBytes(50))
		if _, err := p.VerifiedBaseline(); !errors.Is(err, ErrPreserveSourceChanged) {
			t.Fatalf("got %v", err)
		}
	})
}

// ---- unsupported rename ---------------------------------------------------------------------------------------------

func TestDisplace_UnsupportedRename_FailsClosedWithNoOverwriteFallback(t *testing.T) {
	t.Parallel()
	unsupported := func(a, b string) error {
		return moveWith(func(string, string) error { return syscall.EINVAL }, a, b)
	}
	t.Run("on the first move", func(t *testing.T) {
		t.Parallel()
		e := newDS(t, true, true, true)
		d := newDisplacer()
		repl := e.prep(d)
		d.rename = unsupported
		_, err := d.replace(e.db, repl, e.base())
		if !errors.Is(err, ErrDisplaceNoReplaceUnsupported) || !asDisplacement(t, err).RolledBack {
			t.Fatalf("got %v", err)
		}
		e.assertAllInPlace()
	})
	t.Run("on the installation", func(t *testing.T) {
		t.Parallel()
		e := newDS(t, false, false, false)
		if err := os.Remove(e.db); err != nil {
			t.Fatal(err)
		}
		b, err := e.lock.BaselineNoActive(e.db)
		if err != nil {
			t.Fatal(err)
		}
		d := newDisplacer()
		repl := e.prep(d)
		d.rename = unsupported
		if _, err := d.replace(e.db, repl, b); !errors.Is(err, ErrDisplaceNoReplaceUnsupported) {
			t.Fatalf("got %v", err)
		}
		if _, err := os.Lstat(e.db); !os.IsNotExist(err) {
			t.Fatalf("nothing may have been installed by an overwriting fallback (%v)", err)
		}
	})
}

// ---- effects reported as errors, unknown locations, accurate reporting (PA-1, PA-2) ----------------------

// renamerAfter performs the real no-replace move and THEN lets after() return an
// error: the move took effect although the caller is told that it failed.
func (c *dsCalls) renamerAfter(after func(n int, oldp, newp string) error) func(string, string) error {
	return func(a, b string) error {
		c.list = append(c.list, [2]string{a, b})
		if err := moveNoReplace(a, b); err != nil {
			return err
		}
		return after(len(c.list), a, b)
	}
}

func TestDisplace_MoveTookEffectButReportedAnError_IsFoundAndRolledBackCompletely(t *testing.T) {
	t.Parallel()
	for _, failAt := range []int{1, 2} { // 1: the main file, 2: the -wal
		t.Run(fmt.Sprintf("move %d", failAt), func(t *testing.T) {
			t.Parallel()
			e := newDS(t, true, true, true)
			d := newDisplacer()
			repl := e.prep(d)
			var calls dsCalls
			d.rename = calls.renamerAfter(func(n int, _, _ string) error {
				if n == failAt {
					return errInjected
				}
				return nil
			})
			res, err := d.replace(e.db, repl, e.base())
			de := asDisplacement(t, err)
			if !errors.Is(err, errInjected) || res.Installed {
				t.Fatalf("res %+v err %v", res, err)
			}
			if !de.RolledBack || len(de.Unknown) != 0 || len(de.Retained) != 0 || len(de.RestoredInPlace) != 0 || de.Dir != "" || res.Dir != "" {
				t.Fatalf("the moved file must be found and rolled back completely: %+v", de)
			}
			e.assertAllInPlace()
		})
	}
}

func TestDisplace_InstallTookEffectButReportedAnError_IsTreatedAsInstalled(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, true, true)
	d := newDisplacer()
	repl := e.prep(d)
	var calls dsCalls
	d.rename = calls.renamerAfter(func(n int, _, _ string) error {
		if n == 5 { // main, wal, journal, shm, then the installation
			return errInjected
		}
		return nil
	})
	res, err := d.replace(e.db, repl, e.base())
	if err != nil || !res.Installed {
		t.Fatalf("an installation that took effect is an installation: res %+v err %v", res, err)
	}
	e.assertDBIsCandidate()
	e.assertRetained(res.Dir, "main", "wal", "journal", "shm")
}

func TestDisplace_FileInNeitherPlace_IsReportedAndNeverCalledRolledBack(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		failAt int
		sfx    string
	}{{"the main file", 1, ""}, {"the -wal", 2, "-wal"}} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			e := newDS(t, true, true, true)
			d := newDisplacer()
			repl := e.prep(d)
			third := filepath.Join(e.dir, "third-place")
			var calls dsCalls
			d.rename = func(a, b string) error {
				calls.list = append(calls.list, [2]string{a, b})
				if len(calls.list) == c.failAt { // the file ends up somewhere unexpected
					if err := os.Rename(a, third); err != nil {
						t.Error(err)
					}
					return errInjected
				}
				return moveNoReplace(a, b)
			}
			res, err := d.replace(e.db, repl, e.base())
			de := asDisplacement(t, err)
			orig := e.db + c.sfx
			if res.Installed || de.RolledBack || len(de.Unknown) != 1 || de.Unknown[0] != orig {
				t.Fatalf("an item at neither path must be reported as unknown and must not be called rolled back: %+v", de)
			}
			if de.Dir != "" || res.Dir != "" || len(e.asides()) != 0 {
				t.Fatalf("no retained directory exists: dir %q asides %v", de.Dir, e.asides())
			}
			if got, _ := os.ReadFile(third); !bytes.Equal(got, e.orig[c.sfx]) {
				t.Fatal("the bytes of the file in the third place are not the original's")
			}
			if !bytes.Contains([]byte(err.Error()), []byte(orig)) {
				t.Fatalf("the error must name the file whose location is unknown: %v", err)
			}
			if _, statErr := os.Lstat(orig); !os.IsNotExist(statErr) {
				t.Fatalf("the original path should be empty: %v", statErr)
			}
			for sfx, want := range e.orig {
				if sfx == c.sfx {
					continue
				}
				if got, _ := os.ReadFile(e.db + sfx); !bytes.Equal(got, want) {
					t.Fatalf("%q is not back in place with its original bytes", sfx)
				}
			}
		})
	}
}

func TestDisplace_OutcomeUnknownRename_NotPerformed_RollsBackCleanly(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, true, true)
	d := newDisplacer()
	repl := e.prep(d)
	var calls dsCalls
	d.rename = calls.renamer(func(n int, _, _ string) error {
		if n == 2 { // not performed: the file is still at its original path
			return fmt.Errorf("%w: injected", ErrDisplaceOutcomeUnknown)
		}
		return nil
	})
	_, err := d.replace(e.db, repl, e.base())
	if !errors.Is(err, ErrDisplaceOutcomeUnknown) || !asDisplacement(t, err).RolledBack {
		t.Fatalf("got %v", err)
	}
	e.assertAllInPlace()
}

func TestDisplace_CompensationFails_ReportsEveryFileAccurately(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, true, true)
	d := newDisplacer()
	repl := e.prep(d)
	var calls dsCalls
	// The -shm move fails and the rollback begins; the main file's return is refused by a
	// newcomer; in the compensation that follows, displacing the -wal again FAILS and
	// displacing the journal again succeeds.
	rolling := false
	d.rename = calls.renamer(func(_ int, a, b string) error {
		if e.isAside(a) {
			rolling = true
		}
		switch {
		case e.fwd(a, b, "-shm") && !rolling:
			return errInjected
		case e.back(a, b, ""):
			pvWrite(t, e.db, []byte("someone else's database"))
		case rolling && e.fwd(a, b, "-wal"):
			return errors.New("compensation failed")
		}
		return nil
	})
	res, err := d.replace(e.db, repl, e.base())
	de := asDisplacement(t, err)
	if res.Installed || de.RolledBack || !errors.Is(err, ErrRollbackIncomplete) {
		t.Fatalf("res %+v err %v", res, err)
	}
	retained := map[string]bool{}
	for _, f := range de.Retained {
		retained[f.Name] = true
	}
	if len(de.Retained) != 2 || !retained["main"] || !retained["journal"] {
		t.Fatalf("retained %+v", de.Retained)
	}
	if len(de.RestoredInPlace) != 1 || de.RestoredInPlace[0] != e.db+"-wal" {
		t.Fatalf("the -wal was restored and could not be displaced again, so it must be reported: %+v", de.RestoredInPlace)
	}
	if len(de.NotMoved) != 1 || de.NotMoved[0] != e.db+"-shm" || len(de.Unknown) != 0 {
		t.Fatalf("notMoved %v unknown %v", de.NotMoved, de.Unknown)
	}
	if got, _ := os.ReadFile(e.db); string(got) != "someone else's database" {
		t.Fatal("the newcomer was overwritten")
	}
	if got, _ := os.ReadFile(e.db + "-wal"); !bytes.Equal(got, e.orig["-wal"]) {
		t.Fatal("the -wal's bytes were lost")
	}
	if de.Dir == "" || res.Dir != de.Dir {
		t.Fatalf("the retained directory must be reported: %q %q", de.Dir, res.Dir)
	}
	e.assertRetained(de.Dir, "main", "journal")
	for _, want := range []string{"could not displace", e.db + "-wal", "still at its original path"} {
		if !bytes.Contains([]byte(err.Error()), []byte(want)) {
			t.Fatalf("the error must say %q: %v", want, err)
		}
	}
}

func TestDisplace_RollbackBlockedByANewCompanion_EverythingIsRetainedAndTheNewcomerIsUntouched(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, true, true)
	d := newDisplacer()
	repl := e.prep(d)
	var calls dsCalls
	// The -shm move fails and the rollback begins: the journal goes back, then the -wal's
	// return finds a newcomer -wal at its original path and is refused, and the
	// compensation displaces the journal again.
	d.rename = calls.renamer(func(_ int, a, b string) error {
		switch {
		case e.fwd(a, b, "-shm"):
			return errInjected
		case e.back(a, b, "-wal"):
			pvWrite(t, e.db+"-wal", []byte("a newcomer's wal"))
		}
		return nil
	})
	res, err := d.replace(e.db, repl, e.base())
	de := asDisplacement(t, err)
	if res.Installed || de.RolledBack || !errors.Is(err, ErrRollbackIncomplete) {
		t.Fatalf("res %+v err %v", res, err)
	}
	if len(de.Retained) != 3 || len(de.RestoredInPlace) != 0 || len(de.Unknown) != 0 {
		t.Fatalf("retained %+v restoredInPlace %v unknown %v", de.Retained, de.RestoredInPlace, de.Unknown)
	}
	if got, _ := os.ReadFile(e.db + "-wal"); string(got) != "a newcomer's wal" {
		t.Fatal("the newcomer -wal was overwritten")
	}
	for _, sfx := range []string{"", "-journal"} {
		if _, statErr := os.Lstat(e.db + sfx); !os.IsNotExist(statErr) {
			t.Fatalf("%q must not be at its original path (%v)", sfx, statErr)
		}
	}
	if got, _ := os.ReadFile(e.db + "-shm"); !bytes.Equal(got, e.orig["-shm"]) || len(de.NotMoved) != 1 || de.NotMoved[0] != e.db+"-shm" {
		t.Fatalf("the never-moved -shm must still be in place and reported: %v", de.NotMoved)
	}
	e.assertRetained(de.Dir, "main", "wal", "journal")
}

func TestDisplace_SwapBetweenPrescanAndMove_RollsBackCompletelyAndIsReportedAccurately(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, true, true)
	d := newDisplacer()
	repl := e.prep(d)
	b := e.base()
	swapped := []byte("a different main file")
	d.hook = func(step string) {
		if step == "verified-baseline" { // after the examination, before the moves
			swap := e.db + ".swap"
			pvWrite(t, swap, swapped)
			if err := os.Rename(swap, e.db); err != nil {
				t.Error(err)
			}
		}
	}
	res, err := d.replace(e.db, repl, b)
	de := asDisplacement(t, err)
	if res.Installed || !errors.Is(err, ErrDisplaceBaselineChanged) {
		t.Fatalf("res %+v err %v", res, err)
	}
	if !de.RolledBack || len(de.Unknown) != 0 || len(de.Retained) != 0 || len(de.RestoredInPlace) != 0 || de.Dir != "" || res.Dir != "" {
		t.Fatalf("the rollback was complete and must be reported as such: %+v", de)
	}
	if got, _ := os.ReadFile(e.db); !bytes.Equal(got, swapped) {
		t.Fatal("the file that was swapped in must be kept at the original path")
	}
	for _, sfx := range []string{"-wal", "-shm", "-journal"} {
		if got, _ := os.ReadFile(e.db + sfx); !bytes.Equal(got, e.orig[sfx]) {
			t.Fatalf("%s is not back in place", sfx)
		}
	}
	if a := e.asides(); len(a) != 0 {
		t.Fatalf("an aside directory remains: %v", a)
	}
}

// ---- the prepared replacement is re-checked before the installation (PA-4) ---------------------------------

func TestDisplace_PreparedReplacementChangedBeforeInstall_IsRefusedAndNothingIsInstalled(t *testing.T) {
	t.Parallel()
	changes := map[string]struct {
		do    func(t *testing.T, path string)
		check func(t *testing.T, path string)
	}{
		"modified": {
			do: func(t *testing.T, path string) { pvAppend(t, path, []byte("tampered")) },
			check: func(t *testing.T, path string) {
				if got, err := os.ReadFile(path); err != nil || !bytes.HasSuffix(got, []byte("tampered")) {
					t.Fatalf("the modified file must be left in place (%v)", err)
				}
			},
		},
		"substituted": {
			do: func(t *testing.T, path string) {
				other := path + ".other"
				pvWrite(t, other, []byte("a substitute"))
				if err := os.Rename(other, path); err != nil {
					t.Fatal(err)
				}
			},
			check: func(t *testing.T, path string) {
				if got, _ := os.ReadFile(path); string(got) != "a substitute" {
					t.Fatal("the substituted file must not be deleted or replaced")
				}
			},
		},
		"removed": {
			do: func(t *testing.T, path string) {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			},
			check: func(t *testing.T, path string) {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatalf("the path should be empty (%v)", err)
				}
			},
		},
	}
	for _, flow := range []string{"active database", "no active database"} {
		for name, ch := range changes {
			t.Run(flow+"/"+name, func(t *testing.T) {
				t.Parallel()
				e := newDS(t, true, true, true)
				var b *Baseline
				if flow == "no active database" {
					for sfx := range e.orig {
						if err := os.Remove(e.db + sfx); err != nil {
							t.Fatal(err)
						}
					}
					var err error
					if b, err = e.lock.BaselineNoActive(e.db); err != nil {
						t.Fatal(err)
					}
				} else {
					b = e.base()
				}
				d := newDisplacer()
				repl := e.prep(d)
				d.beforeInstall = func() { ch.do(t, repl.path) }
				res, err := d.replace(e.db, repl, b)
				if res.Installed || !errors.Is(err, ErrDisplaceReplacementChanged) {
					t.Fatalf("res %+v err %v", res, err)
				}
				de := asDisplacement(t, err)
				if !de.RolledBack || len(de.Retained) != 0 || len(de.Unknown) != 0 || de.Dir != "" {
					t.Fatalf("everything displaced must be back: %+v", de)
				}
				ch.check(t, repl.path)
				if flow == "active database" {
					e.assertAllInPlace() // the original main file and every companion, byte for byte
				} else {
					if _, statErr := os.Lstat(e.db); !os.IsNotExist(statErr) {
						t.Fatalf("nothing may be installed after the check fails (%v)", statErr)
					}
					if a := e.asides(); len(a) != 0 {
						t.Fatalf("an aside directory remains: %v", a)
					}
				}
			})
		}
	}
}

// ---- the operator-facing text states only what the structured result proves (AF-1) ----------------------

func dsHas(s, sub string) bool { return bytes.Contains([]byte(s), []byte(sub)) }

func TestDisplacementError_Text_StatesOnlyWhatTheStructuredResultProves(t *testing.T) {
	t.Parallel()
	type tc struct {
		name         string
		de           *DisplacementError
		inPlace      bool
		want, wantNo []string
	}
	cases := []tc{
		{
			name: "main back, a companion's location unknown: no claim that anything is retained",
			de: &DisplacementError{Stage: "move-wal", Err: errInjected, Main: MainAtOriginal,
				Unknown: []string{"/d/c.db-wal"}, UnknownDetail: []string{"/d/c.db-wal (at its original path: nothing there; at /d/aside/wal: nothing there)"}},
			want:   []string{"NOT rolled back", "the original main database file is back at its original path", "location could not be established: /d/c.db-wal", "nothing was deleted or overwritten"},
			wantNo: []string{"retained", "rolled back: every original"},
		},
		{
			name: "main of unknown location: it is not said to be back or retained",
			de: &DisplacementError{Stage: "move-main", Err: errInjected, Main: MainUnknown,
				Unknown: []string{"/d/c.db"}, UnknownDetail: []string{"/d/c.db (at its original path: nothing there; at /d/aside/main: nothing there)"}},
			want:   []string{"the location of the original main database file could not be established", "at its original path: nothing there"},
			wantNo: []string{"back at its original path", "retained"},
		},
		{
			name: "files retained in the aside directory",
			de: &DisplacementError{Stage: "install", Err: errInjected, Main: MainRetained, Dir: "/d/aside",
				Retained: []DisplacedFile{{Name: "main", Original: "/d/c.db", Retained: "/d/aside/main"}, {Name: "wal", Original: "/d/c.db-wal", Retained: "/d/aside/wal"}}},
			want: []string{"the original main database file is NOT at its original path", "retained in /d/aside", "main: /d/c.db -> /d/aside/main", "wal: /d/c.db-wal -> /d/aside/wal"},
		},
		{
			name:    "fully rolled back",
			de:      &DisplacementError{Stage: "install", Err: errInjected, Main: MainAtOriginal, RolledBack: true},
			inPlace: true,
			want:    []string{"rolled back: every original file is back at its original path"},
			wantNo:  []string{"NOT rolled back"},
		},
		{
			name: "mixed: retained main, a companion restored and stuck, one unknown, one vanished, one never moved",
			de: &DisplacementError{Stage: "move-shm", Err: errInjected, Main: MainRetained, Dir: "/d/aside",
				Retained:        []DisplacedFile{{Name: "main", Original: "/d/c.db", Retained: "/d/aside/main"}},
				RestoredInPlace: []string{"/d/c.db-wal"}, Unknown: []string{"/d/c.db-journal"},
				Vanished: []string{"/d/c.db-shm"}, NotMoved: []string{"/d/other"}},
			want: []string{"retained in /d/aside", "still at its original path /d/c.db-wal", "location could not be established: /d/c.db-journal",
				"vanished before it could be moved, so it is neither retained nor restored: /d/c.db-shm", "never moved, still at /d/other"},
		},
		{
			name:    "a vanished companion alone: the main file is in place, but it is not called rolled back",
			de:      &DisplacementError{Stage: "move-shm", Err: errInjected, Main: MainAtOriginal, Vanished: []string{"/d/c.db-wal"}},
			inPlace: true,
			want:    []string{"NOT rolled back", "vanished before it could be moved", "/d/c.db-wal"},
			wantNo:  []string{"rolled back: every original"},
		},
		{
			name:   "the zero value is never taken to be in place, and never prints an empty clause",
			de:     &DisplacementError{Stage: "x", Err: errInjected},
			want:   []string{"NOT rolled back: the state of the original files is not known"},
			wantNo: []string{"rolled back: every original", "NOT rolled back: ;"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			text := c.de.Error()
			for _, w := range c.want {
				if !dsHas(text, w) {
					t.Errorf("the text must contain %q:\n%s", w, text)
				}
			}
			for _, w := range c.wantNo {
				if dsHas(text, w) {
					t.Errorf("the text must NOT contain %q:\n%s", w, text)
				}
			}
			if got := c.de.OriginalInPlace(); got != c.inPlace {
				t.Errorf("OriginalInPlace = %v, want %v", got, c.inPlace)
			}
		})
	}
}

// ---- a swap between the examination and the move (AF-3) ------------------------------------------------------

// swapMain keeps the original main file (its bytes and identity) at keptOriginal
// and puts a different file, with different content, at the main path: the file a
// third party swapped in.
func (e *dsEnv) swapMain(keptOriginal string, swapped []byte) {
	e.t.Helper()
	if err := os.Rename(e.db, keptOriginal); err != nil {
		e.t.Error(err)
		return
	}
	pvWrite(e.t, e.db, swapped)
}

func (e *dsEnv) assertSwapKept(keptOriginal string, swapped []byte) {
	e.t.Helper()
	if got, _ := os.ReadFile(keptOriginal); !bytes.Equal(got, e.orig[""]) {
		e.t.Fatal("the original main file's bytes were lost")
	}
	kfi, err1 := os.Lstat(keptOriginal)
	dfi, err2 := os.Lstat(e.db)
	if err1 != nil || err2 != nil || os.SameFile(kfi, dfi) {
		e.t.Fatalf("the original and the swapped-in file must be distinct files (%v, %v)", err1, err2)
	}
	if got, _ := os.ReadFile(e.db); !bytes.Equal(got, swapped) {
		e.t.Fatal("the swapped-in file must be at the main path, untouched")
	}
}

func TestDisplace_SwapBeforeRename_RenameSucceeds_TheSwappedFileIsRestoredAndTheOriginalIsKept(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, true, true)
	d := newDisplacer()
	repl := e.prep(d)
	b := e.base()
	kept := filepath.Join(e.dir, "kept-original")
	swapped := []byte("a different main file")
	d.hook = func(step string) {
		if step == "verified-baseline" {
			e.swapMain(kept, swapped)
		}
	}
	res, err := d.replace(e.db, repl, b)
	de := asDisplacement(t, err)
	if res.Installed || !errors.Is(err, ErrDisplaceBaselineChanged) {
		t.Fatalf("res %+v err %v", res, err)
	}
	if !de.RolledBack || de.Main != MainAtOriginal || len(de.Unknown) != 0 || len(de.Retained) != 0 || len(de.Vanished) != 0 || len(de.RestoredInPlace) != 0 || de.Dir != "" {
		t.Fatalf("%+v", de)
	}
	e.assertSwapKept(kept, swapped)
	for _, sfx := range []string{"-wal", "-shm", "-journal"} {
		if got, _ := os.ReadFile(e.db + sfx); !bytes.Equal(got, e.orig[sfx]) {
			t.Fatalf("%s is not back in place", sfx)
		}
	}
	if a := e.asides(); len(a) != 0 {
		t.Fatalf("an aside directory remains: %v", a)
	}
}

func TestDisplace_SwapAfterTheLastLook_RenameReportsAnErrorAfterTakingEffect_FollowsTheFileThatMoved(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, true, true)
	d := newDisplacer()
	repl := e.prep(d)
	kept := filepath.Join(e.dir, "kept-original")
	swapped := []byte("a different main file")
	var calls dsCalls
	d.rename = func(a, b string) error {
		calls.list = append(calls.list, [2]string{a, b})
		if len(calls.list) == 1 { // the main move: swapped after the last look, then it takes effect, then it reports an error
			e.swapMain(kept, swapped)
			if err := moveNoReplace(a, b); err != nil {
				t.Error(err)
			}
			return errInjected
		}
		return moveNoReplace(a, b)
	}
	res, err := d.replace(e.db, repl, e.base())
	de := asDisplacement(t, err)
	if res.Installed || !errors.Is(err, errInjected) {
		t.Fatalf("res %+v err %v", res, err)
	}
	if !de.RolledBack || de.Main != MainAtOriginal || len(de.Unknown) != 0 || len(de.Retained) != 0 || de.Dir != "" || res.Dir != "" {
		t.Fatalf("the file that actually moved must be followed and put back, not reported as unknown: %+v", de)
	}
	e.assertSwapKept(kept, swapped)
	if a := e.asides(); len(a) != 0 {
		t.Fatalf("an aside directory remains: %v", a)
	}
}

func TestDisplace_SwapAfterTheLastLook_RenameReportsAnErrorWithoutTakingEffect_IsUnknownAndFailsClosed(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, true, true)
	d := newDisplacer()
	repl := e.prep(d)
	kept := filepath.Join(e.dir, "kept-original")
	swapped := []byte("a different main file")
	var calls dsCalls
	d.rename = func(a, b string) error {
		calls.list = append(calls.list, [2]string{a, b})
		if len(calls.list) == 1 {
			e.swapMain(kept, swapped) // swapped after the last look; the rename then fails without moving anything
			return errInjected
		}
		return moveNoReplace(a, b)
	}
	res, err := d.replace(e.db, repl, e.base())
	de := asDisplacement(t, err)
	if res.Installed || !errors.Is(err, errInjected) {
		t.Fatalf("res %+v err %v", res, err)
	}
	if de.RolledBack || de.Main != MainUnknown || len(de.Unknown) != 1 || de.Unknown[0] != e.db || len(de.UnknownDetail) != 1 {
		t.Fatalf("a file that cannot be identified must stay unknown and must not be called rolled back: %+v", de)
	}
	for _, want := range []string{"at its original path: a different file than the one examined", "nothing there"} {
		if !dsHas(de.UnknownDetail[0], want) {
			t.Fatalf("the candidate paths must be described (%q): %s", want, de.UnknownDetail[0])
		}
	}
	e.assertSwapKept(kept, swapped)
	for _, sfx := range []string{"-wal", "-shm", "-journal"} {
		if got, _ := os.ReadFile(e.db + sfx); !bytes.Equal(got, e.orig[sfx]) {
			t.Fatalf("%s must not have been touched", sfx)
		}
	}
}

func TestDisplace_SwapBeforeTheLastLook_RenameFailsWithoutTakingEffect_NothingMovedAndNothingIsTouched(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, true, true)
	d := newDisplacer()
	repl := e.prep(d)
	b := e.base()
	kept := filepath.Join(e.dir, "kept-original")
	swapped := []byte("a different main file")
	d.hook = func(step string) {
		if step == "verified-baseline" {
			e.swapMain(kept, swapped)
		}
	}
	var calls dsCalls
	d.rename = calls.renamer(func(n int, _, _ string) error {
		if n == 1 {
			return errInjected
		}
		return nil
	})
	_, err := d.replace(e.db, repl, b)
	de := asDisplacement(t, err)
	if !errors.Is(err, errInjected) || !de.RolledBack || de.Main != MainUntouched || len(de.Unknown) != 0 || len(de.Retained) != 0 {
		t.Fatalf("this operation moved nothing, so it touched nothing: %+v", de)
	}
	e.assertSwapKept(kept, swapped)
}

// ---- the prepared replacement: each compared property is checked (AF-4) --------------------------------------------

func TestDisplace_PreparedReplacement_ByteIdenticalInodeSubstitution_IsRefused(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, true, true)
	d := newDisplacer()
	repl := e.prep(d)
	b := e.base()
	var substituted os.FileInfo
	var content []byte
	d.beforeInstall = func() {
		fi, err := os.Lstat(repl.path)
		if err != nil {
			t.Error(err)
			return
		}
		if content, err = os.ReadFile(repl.path); err != nil {
			t.Error(err)
			return
		}
		other := repl.path + ".other"
		pvWrite(t, other, content)                                            // the same bytes and the same size ...
		if err := os.Chtimes(other, fi.ModTime(), fi.ModTime()); err != nil { // ... and the same mtime
			t.Error(err)
		}
		if err := os.Rename(other, repl.path); err != nil { // a different inode
			t.Error(err)
		}
		substituted, _ = os.Lstat(repl.path)
	}
	res, err := d.replace(e.db, repl, b)
	if res.Installed || !errors.Is(err, ErrDisplaceReplacementChanged) {
		t.Fatalf("a substituted inode must be refused even with identical content, size and mtime: res %+v err %v", res, err)
	}
	if de := asDisplacement(t, err); !de.RolledBack || de.Main != MainAtOriginal {
		t.Fatalf("%+v", de)
	}
	if fi, statErr := os.Lstat(repl.path); statErr != nil || !os.SameFile(fi, substituted) {
		t.Fatalf("the substituted file must not be deleted or replaced (%v)", statErr)
	}
	if got, _ := os.ReadFile(repl.path); !bytes.Equal(got, content) {
		t.Fatal("the substituted file's content changed")
	}
	e.assertAllInPlace() // nothing was installed: the original main file and companions are in place
}

func TestDisplace_PreparedReplacement_SameSizeModificationTimeChange_IsRefused(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, true, true)
	d := newDisplacer()
	repl := e.prep(d)
	b := e.base()
	var before os.FileInfo
	d.beforeInstall = func() {
		fi, err := os.Lstat(repl.path)
		if err != nil {
			t.Error(err)
			return
		}
		before = fi
		// One hour later: far beyond any filesystem's timestamp resolution, same size, same inode.
		later := fi.ModTime().Add(time.Hour)
		if err := os.Chtimes(repl.path, later, later); err != nil {
			t.Error(err)
		}
	}
	res, err := d.replace(e.db, repl, b)
	if res.Installed || !errors.Is(err, ErrDisplaceReplacementChanged) {
		t.Fatalf("a changed modification time must be refused: res %+v err %v", res, err)
	}
	fi, statErr := os.Lstat(repl.path)
	if statErr != nil || !os.SameFile(fi, before) || fi.Size() != before.Size() {
		t.Fatalf("the file must be left in place, same inode and size (%v)", statErr)
	}
	e.assertAllInPlace()
}

// TestDisplace_ReplacementVerify_EveryComparedPropertyIsChecked isolates each
// property, so none can be dropped unnoticed. It needs no timestamp resolution and
// no way to set a ctime (os.Chtimes cannot: the kernel sets ctime itself): the
// recorded signature is altered, one field at a time, for an unchanged file.
func TestDisplace_ReplacementVerify_EveryComparedPropertyIsChecked(t *testing.T) {
	t.Parallel()
	e := newDS(t, false, false, false)
	repl := e.prep(newDisplacer())
	if err := repl.verify(); err != nil {
		t.Fatalf("an untouched replacement must verify: %v", err)
	}
	mutations := map[string]func(s *statSig){
		"size":   func(s *statSig) { s.size++ },
		"mtime":  func(s *statSig) { s.mtimeNs += int64(time.Hour) },
		"ctime":  func(s *statSig) { s.ctimeNs += int64(time.Hour) },
		"inode":  func(s *statSig) { s.ino++ },
		"device": func(s *statSig) { s.dev++ },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			clone := *repl
			mutate(&clone.sig)
			if err := clone.verify(); !errors.Is(err, ErrDisplaceReplacementChanged) {
				t.Fatalf("a changed %s must be refused, got %v", name, err)
			}
		})
	}
	t.Run("identity alone, with an identical signature", func(t *testing.T) {
		other := filepath.Join(e.dir, "other.bin")
		pvWrite(t, other, pvBytes(100))
		ofi, err := os.Lstat(other)
		if err != nil {
			t.Fatal(err)
		}
		clone := *repl // the recorded identity is the replacement's, the path and signature are the other file's
		clone.path, clone.sig = other, statSigOf(ofi)
		if err := clone.verify(); !errors.Is(err, ErrDisplaceReplacementChanged) {
			t.Fatalf("a different file must be refused even when its signature matches, got %v", err)
		}
	})
	t.Run("not a regular file", func(t *testing.T) {
		link := filepath.Join(e.dir, "link.bin")
		if err := os.Symlink(repl.path, link); err != nil {
			t.Fatal(err)
		}
		clone := *repl
		clone.path = link
		if err := clone.verify(); !errors.Is(err, ErrDisplaceReplacementChanged) {
			t.Fatalf("a symbolic link must be refused, got %v", err)
		}
	})
	t.Run("missing", func(t *testing.T) {
		clone := *repl
		clone.path = filepath.Join(e.dir, "absent.bin")
		if err := clone.verify(); !errors.Is(err, ErrDisplaceReplacementChanged) {
			t.Fatalf("a missing file must be refused, got %v", err)
		}
	})
}

// ---- vanished files are carried into the error (AF-5) ---------------------------------------------------------------

func TestDisplace_CompanionVanishes_ThenALaterFailure_IsCarriedIntoTheError(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, true, true)
	d := newDisplacer()
	repl := e.prep(d)
	var calls dsCalls
	// main(1), wal(2) vanishes, journal(3), shm(4) fails.
	d.rename = calls.renamer(func(n int, oldp, _ string) error {
		switch {
		case oldp == e.db+"-wal":
			_ = os.Remove(oldp)
		case n == 4:
			return errInjected
		}
		return nil
	})
	res, err := d.replace(e.db, repl, e.base())
	de := asDisplacement(t, err)
	if res.Installed || !errors.Is(err, errInjected) {
		t.Fatalf("res %+v err %v", res, err)
	}
	if len(de.Vanished) != 1 || de.Vanished[0] != e.db+"-wal" || len(res.Vanished) != 1 || res.Vanished[0] != "wal" {
		t.Fatalf("the vanished companion must be reported: error %v result %v", de.Vanished, res.Vanished)
	}
	if de.RolledBack {
		t.Fatal("a vanished file means the original state is not completely restored: RolledBack must be false")
	}
	if de.Main != MainAtOriginal || len(de.Retained) != 0 || len(de.Unknown) != 0 || len(de.RestoredInPlace) != 0 || !de.OriginalInPlace() {
		t.Fatalf("the vanished file must not be classified as retained, unknown or restored: %+v", de)
	}
	for _, want := range []string{"vanished before it could be moved, so it is neither retained nor restored", e.db + "-wal"} {
		if !dsHas(err.Error(), want) {
			t.Fatalf("the error must say %q: %v", want, err)
		}
	}
	if _, statErr := os.Lstat(e.db + "-wal"); !os.IsNotExist(statErr) {
		t.Fatalf("the vanished -wal must still be absent (%v)", statErr)
	}
	for _, sfx := range []string{"", "-journal", "-shm"} {
		if got, _ := os.ReadFile(e.db + sfx); !bytes.Equal(got, e.orig[sfx]) {
			t.Fatalf("%q is not back in place", sfx)
		}
	}
	if a := e.asides(); len(a) != 0 {
		t.Fatalf("an aside directory remains: %v", a)
	}
}

func TestDisplace_MainFileVanishes_IsReportedAsVanishedAndNothingIsMoved(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, true, true)
	d := newDisplacer()
	repl := e.prep(d)
	var calls dsCalls
	d.rename = calls.renamer(func(n int, oldp, _ string) error {
		if n == 1 {
			_ = os.Remove(oldp) // the main file disappears before it can be moved
		}
		return nil
	})
	res, err := d.replace(e.db, repl, e.base())
	de := asDisplacement(t, err)
	if res.Installed {
		t.Fatal("nothing may be installed")
	}
	if de.Main != MainVanished || len(de.Vanished) != 1 || de.Vanished[0] != e.db || de.RolledBack || de.OriginalInPlace() {
		t.Fatalf("the main file vanished and must be reported as such, not as unknown or restored: %+v", de)
	}
	if len(de.Unknown) != 0 || len(de.Retained) != 0 {
		t.Fatalf("%+v", de)
	}
	if len(de.NotMoved) != 3 {
		t.Fatalf("the three companions were never moved and must be reported: %v", de.NotMoved)
	}
	for _, sfx := range []string{"-wal", "-shm", "-journal"} {
		if got, _ := os.ReadFile(e.db + sfx); !bytes.Equal(got, e.orig[sfx]) {
			t.Fatalf("%s must be untouched", sfx)
		}
	}
	if !dsHas(err.Error(), "vanished before it could be moved") {
		t.Fatalf("the error must say that the main file vanished: %v", err)
	}
	if a := e.asides(); len(a) != 0 {
		t.Fatalf("an aside directory remains: %v", a)
	}
}

// ---- a file already gone when the call is made is never called rolled back ----------------------------------

func TestDisplace_BaselineStage_FileGoneBeforeTheCall_IsNeverCalledRolledBack(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		remove       string
		wantMain     MainState
		wantInPlace  bool
		wantVanished string
	}{
		{"the main file", "", MainVanished, false, ""},
		{"the -wal", "-wal", MainUntouched, true, "-wal"},
		{"the -journal", "-journal", MainUntouched, true, "-journal"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			e := newDS(t, true, true, true)
			d := newDisplacer()
			repl := e.prep(d)
			b := e.base()
			if err := os.Remove(e.db + c.remove); err != nil { // gone after the baseline, before the call
				t.Fatal(err)
			}
			res, err := d.replace(e.db, repl, b)
			de := asDisplacement(t, err)
			if res.Installed || !errors.Is(err, ErrDisplaceBaselineChanged) {
				t.Fatalf("res %+v err %v", res, err)
			}
			if de.RolledBack || de.Main != c.wantMain || de.OriginalInPlace() != c.wantInPlace {
				t.Fatalf("a file that is gone must not be reported as rolled back or in place: %+v", de)
			}
			if len(de.Vanished) != 1 || de.Vanished[0] != e.db+c.remove {
				t.Fatalf("the vanished file must be reported: %v", de.Vanished)
			}
			if dsHas(err.Error(), "rolled back: every original") || !dsHas(err.Error(), "vanished before it could be moved") {
				t.Fatalf("the text must say that a file vanished: %v", err)
			}
			if a := e.asides(); len(a) != 0 {
				t.Fatalf("nothing was moved, so no aside directory may exist: %v", a)
			}
			for sfx, want := range e.orig {
				if sfx == c.remove {
					continue
				}
				if got, _ := os.ReadFile(e.db + sfx); !bytes.Equal(got, want) {
					t.Fatalf("%q must be untouched", sfx)
				}
			}
		})
	}
}

// ---- a -wal or -journal appearing after the displacement (reviewer finding) ----------------------------

func TestDisplace_CompanionAppearsBeforeInstall_IsRefusedAndNeverInstalledBesideIt(t *testing.T) {
	t.Parallel()
	for _, sfx := range []string{"-wal", "-journal"} {
		t.Run("active database/"+sfx, func(t *testing.T) {
			t.Parallel()
			e := newDS(t, true, true, true)
			d := newDisplacer()
			repl := e.prep(d)
			b := e.base()
			d.beforeInstall = func() { pvWrite(t, e.db+sfx, []byte("a stray companion")) }
			res, err := d.replace(e.db, repl, b)
			de := asDisplacement(t, err)
			if res.Installed || !errors.Is(err, ErrDisplaceCompanionAppeared) {
				t.Fatalf("res %+v err %v", res, err)
			}
			if got, _ := os.ReadFile(e.db + sfx); string(got) != "a stray companion" {
				t.Fatal("the stray companion was overwritten or removed")
			}
			if _, statErr := os.Lstat(e.db); !os.IsNotExist(statErr) {
				t.Fatalf("nothing may be installed beside it (%v)", statErr)
			}
			if de.RolledBack || de.Main != MainRetained || len(de.Retained) != 4 || len(de.Unknown) != 0 || len(de.RestoredInPlace) != 0 {
				t.Fatalf("every original must be retained, none put back beside the stray file: %+v", de)
			}
			e.assertRetained(de.Dir, "main", "wal", "journal", "shm")
		})
	}
	t.Run("no active database", func(t *testing.T) {
		t.Parallel()
		e := newDS(t, true, true, true)
		for sfx := range e.orig {
			if err := os.Remove(e.db + sfx); err != nil {
				t.Fatal(err)
			}
		}
		b, err := e.lock.BaselineNoActive(e.db)
		if err != nil {
			t.Fatal(err)
		}
		d := newDisplacer()
		repl := e.prep(d)
		d.beforeInstall = func() { pvWrite(t, e.db+"-wal", []byte("a stray companion")) }
		res, err := d.replace(e.db, repl, b)
		if res.Installed || !errors.Is(err, ErrDisplaceCompanionAppeared) {
			t.Fatalf("res %+v err %v", res, err)
		}
		if got, _ := os.ReadFile(e.db + "-wal"); string(got) != "a stray companion" {
			t.Fatal("the stray companion was overwritten or removed")
		}
		if _, statErr := os.Lstat(e.db); !os.IsNotExist(statErr) {
			t.Fatalf("nothing may be installed (%v)", statErr)
		}
	})
	t.Run("a stray -shm is advisory and does not block", func(t *testing.T) {
		t.Parallel()
		e := newDS(t, true, false, false)
		d := newDisplacer()
		repl := e.prep(d)
		d.beforeInstall = func() { pvWrite(t, e.db+"-shm", []byte("advisory")) }
		res, err := d.replace(e.db, repl, e.base())
		if err != nil || !res.Installed {
			t.Fatalf("res %+v err %v", res, err)
		}
	})
}

// ---- verifyDisplaced compares exactly the properties a rename leaves alone ----------------------------------

func TestDisplace_VerifyDisplaced_EveryComparedPropertyIsChecked(t *testing.T) {
	t.Parallel()
	e := newDS(t, false, false, false)
	b := e.base()
	retained := filepath.Join(e.dir, "retained-main")
	if err := os.Rename(e.db, retained); err != nil {
		t.Fatal(err)
	}
	it := &dItem{name: "main", orig: e.db, retained: retained, info: b.main.info}
	if err := b.verifyDisplaced(it); err != nil {
		t.Fatalf("a file moved with no change must verify (the rename changed only its ctime): %v", err)
	}
	for name, mutate := range map[string]func(s *statSig){
		"size":   func(s *statSig) { s.size++ },
		"mtime":  func(s *statSig) { s.mtimeNs += int64(time.Hour) },
		"inode":  func(s *statSig) { s.ino++ },
		"device": func(s *statSig) { s.dev++ },
	} {
		t.Run(name, func(t *testing.T) {
			clone := *b
			mutate(&clone.main.sig)
			if err := clone.verifyDisplaced(it); !errors.Is(err, ErrDisplaceBaselineChanged) {
				t.Fatalf("a changed %s must be refused, got %v", name, err)
			}
		})
	}
	t.Run("ctime is deliberately not compared", func(t *testing.T) {
		clone := *b
		clone.main.sig.ctimeNs += int64(time.Hour)
		if err := clone.verifyDisplaced(it); err != nil {
			t.Fatalf("a rename updates ctime, so it must not be compared after the move: %v", err)
		}
	})
	t.Run("identity", func(t *testing.T) {
		other := filepath.Join(e.dir, "other.bin")
		pvWrite(t, other, pvBytes(10))
		ofi, err := os.Lstat(other)
		if err != nil {
			t.Fatal(err)
		}
		clone := *b
		clone.main.info = ofi
		if err := clone.verifyDisplaced(it); !errors.Is(err, ErrDisplaceBaselineChanged) {
			t.Fatalf("a different file must be refused, got %v", err)
		}
	})
}

// ---- a rename whose outcome is unknown but which took effect (AF-6) -------------------------------------------------

func TestDisplace_OutcomeUnknownRename_ThatTookEffect_IsFoundAndHandled(t *testing.T) {
	t.Parallel()
	unknown := func(n, at int) error {
		if n == at {
			return fmt.Errorf("%w: injected", ErrDisplaceOutcomeUnknown)
		}
		return nil
	}
	t.Run("a move: the file is found at its retained path and rolled back", func(t *testing.T) {
		t.Parallel()
		e := newDS(t, true, true, true)
		d := newDisplacer()
		repl := e.prep(d)
		var calls dsCalls
		d.rename = calls.renamerAfter(func(n int, _, _ string) error { return unknown(n, 2) })
		_, err := d.replace(e.db, repl, e.base())
		if !errors.Is(err, ErrDisplaceOutcomeUnknown) || !asDisplacement(t, err).RolledBack {
			t.Fatalf("got %v", err)
		}
		e.assertAllInPlace()
	})
	t.Run("the installation: the replacement is found in place and treated as installed", func(t *testing.T) {
		t.Parallel()
		e := newDS(t, true, true, true)
		d := newDisplacer()
		repl := e.prep(d)
		var calls dsCalls
		d.rename = calls.renamerAfter(func(n int, _, _ string) error { return unknown(n, 5) })
		res, err := d.replace(e.db, repl, e.base())
		if err != nil || !res.Installed {
			t.Fatalf("res %+v err %v", res, err)
		}
		e.assertDBIsCandidate()
		e.assertRetained(res.Dir, "main", "wal", "journal", "shm")
	})
}

// ---- the rollback's durability and leftovers are reported, never swallowed ------------------------------------

// rollbackScenario: the -shm move fails, so the journal, the -wal and the main file
// are put back. "rolling" becomes true as soon as the rollback starts.
func rollbackScenario(t *testing.T) (*dsEnv, *displacer, *Replacement, *bool) {
	t.Helper()
	e := newDS(t, true, true, true)
	d := newDisplacer()
	repl := e.prep(d)
	rolling := false
	d.rename = func(a, b string) error {
		if e.isAside(a) {
			rolling = true
		}
		if e.fwd(a, b, "-shm") {
			return errInjected
		}
		return moveNoReplace(a, b)
	}
	return e, d, repl, &rolling
}

func TestDisplace_RollbackSyncFailure_IsReportedAndNotCalledDurable(t *testing.T) {
	t.Parallel()
	for _, nth := range []int{1, 2} { // the sync after the files go back, and the one after the aside directory is removed
		t.Run(fmt.Sprintf("rollback sync %d fails", nth), func(t *testing.T) {
			t.Parallel()
			e, d, repl, rolling := rollbackScenario(t)
			seen := 0
			d.syncDir = func(dir string) error {
				if dir == e.dir && *rolling {
					seen++
					if seen == nth {
						return errInjected
					}
				}
				return fsyncDir(dir)
			}
			res, err := d.replace(e.db, repl, e.base())
			de := asDisplacement(t, err)
			if res.Installed || !errors.Is(err, errInjected) {
				t.Fatalf("res %+v err %v", res, err)
			}
			// Where the files are is unchanged: every original is back.
			if !de.RolledBack || de.Main != MainAtOriginal {
				t.Fatalf("%+v", de)
			}
			e.assertAllInPlace()
			// But the operator must be told that the rollback is not confirmed durable.
			if len(de.Notes) != 1 || !dsHas(de.Notes[0], "not confirmed to be crash-durable") || !dsHas(de.Notes[0], e.dir) {
				t.Fatalf("the failed sync must be reported: %v", de.Notes)
			}
			if !dsHas(err.Error(), "rolled back: every original file is back at its original path; however, syncing") {
				t.Fatalf("the text must qualify \"rolled back\": %v", err)
			}
		})
	}
}

func TestDisplace_RollbackCannotRemoveTheAsideDirectory_IsReportedAndTheDirectoryIsLeftAlone(t *testing.T) {
	t.Parallel()
	t.Run("after a complete rollback", func(t *testing.T) {
		t.Parallel()
		e, d, repl, _ := rollbackScenario(t)
		d.removeDir = func(string) error { return errInjected }
		_, err := d.replace(e.db, repl, e.base())
		de := asDisplacement(t, err)
		if !de.RolledBack {
			t.Fatalf("%+v", de)
		}
		asides := e.asides()
		if len(asides) != 1 {
			t.Fatalf("the directory could not be removed, so it must still be there: %v", asides)
		}
		if ents, _ := os.ReadDir(asides[0]); len(ents) != 0 {
			t.Fatalf("it must be empty: %v", ents)
		}
		if len(de.Notes) != 1 || !dsHas(de.Notes[0], asides[0]) || !dsHas(de.Notes[0], "could not be removed and is still there") {
			t.Fatalf("the leftover directory must be reported: %v", de.Notes)
		}
		if !dsHas(err.Error(), asides[0]) {
			t.Fatalf("the text must name the leftover directory: %v", err)
		}
		for sfx, want := range e.orig {
			if got, _ := os.ReadFile(e.db + sfx); !bytes.Equal(got, want) {
				t.Fatalf("%q is not back in place", sfx)
			}
		}
	})
	t.Run("when nothing had been moved", func(t *testing.T) {
		t.Parallel()
		e := newDS(t, true, true, true)
		d := newDisplacer()
		repl := e.prep(d)
		d.rename = func(a, b string) error {
			if e.fwd(a, b, "") {
				return errInjected // the main move fails: nothing moves
			}
			return moveNoReplace(a, b)
		}
		d.removeDir = func(string) error { return errInjected }
		_, err := d.replace(e.db, repl, e.base())
		de := asDisplacement(t, err)
		if !de.RolledBack || len(de.Notes) != 1 || !dsHas(de.Notes[0], "could not be removed and is still there") {
			t.Fatalf("%+v", de)
		}
		if len(e.asides()) != 1 {
			t.Fatalf("the empty directory this call created must still be there: %v", e.asides())
		}
	})
}

// ---- a real crash during the rollback ------------------------------------------------------------------------

// TestHelperProcessDisplaceRollback is not a real test: it runs only when
// DSO_TEST_RBK_MODE is set. The -shm move fails, so the rollback begins (journal,
// then -wal, then main go back); the process SIGKILLs itself before or after the
// Nth move back.
func TestHelperProcessDisplaceRollback(t *testing.T) {
	mode := os.Getenv("DSO_TEST_RBK_MODE")
	if mode == "" {
		t.Skip("helper process")
	}
	db, cand := os.Getenv("DSO_TEST_RBK_DB"), os.Getenv("DSO_TEST_RBK_CAND")
	n, _ := strconv.Atoi(os.Getenv("DSO_TEST_RBK_N"))
	lock, err := AcquireExclusiveCentralLock(db, 3*time.Second)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	d := newDisplacer()
	repl, err := d.prepare(db, cand)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	b, err := lock.BaselineAfterClose(db)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	aside := func(p string) bool { return strings.HasPrefix(p, db+".replaced-") }
	kill := func() {
		_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
		select {}
	}
	backs := 0
	d.rename = func(a, bp string) error {
		if aside(bp) && a == db+"-shm" {
			return errors.New("injected failure")
		}
		if aside(a) {
			backs++
			if mode == "before" && backs == n {
				kill()
			}
		}
		err := moveNoReplace(a, bp)
		if aside(a) && mode == "after" && backs == n {
			kill()
		}
		return err
	}
	_, err = d.replace(db, repl, b)
	fmt.Fprintln(os.Stderr, "the child was supposed to be killed but finished:", err)
	os.Exit(3)
}

func TestDisplace_CrashDuringRollback_NeverLosesAnOriginalByte_AndEveryStateIsDistinguishable(t *testing.T) {
	t.Parallel()
	// The rollback puts back, in this order: the journal, the -wal, the main file.
	restoreOrder := []string{"-journal", "-wal", ""}
	nameOf := map[string]string{"": "main", "-wal": "wal", "-journal": "journal", "-shm": "shm"}
	for _, c := range []struct {
		mode     string
		n        int
		restored int // how many have been put back when the process dies
	}{{"before", 1, 0}, {"after", 1, 1}, {"before", 2, 1}, {"after", 2, 2}, {"before", 3, 2}, {"after", 3, 3}} {
		t.Run(fmt.Sprintf("killed %s move back %d", c.mode, c.n), func(t *testing.T) {
			t.Parallel()
			_, db := pvDB(t, true, true, true)
			cand := filepath.Join(pvDir(t), "cand.bin")
			pvWrite(t, cand, pvBytes(5000))
			orig := map[string][]byte{}
			for _, sfx := range []string{"", "-wal", "-shm", "-journal"} {
				b, err := os.ReadFile(db + sfx)
				if err != nil {
					t.Fatal(err)
				}
				orig[sfx] = b
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcessDisplaceRollback$") // #nosec G204 -- this test binary
			cmd.Env = append(os.Environ(), "DSO_TEST_RBK_MODE="+c.mode, "DSO_TEST_RBK_N="+strconv.Itoa(c.n), "DSO_TEST_RBK_DB="+db, "DSO_TEST_RBK_CAND="+cand)
			cmd.Stderr = os.Stderr
			err := cmd.Run()
			var ee *exec.ExitError
			if !errors.As(err, &ee) {
				t.Fatalf("expected the child to be killed, got %v", err)
			}
			if ws, ok := ee.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
				t.Fatalf("expected SIGKILL, got %v", err)
			}
			asides, _ := filepath.Glob(db + ".replaced-*")
			if len(asides) != 1 {
				t.Fatalf("one aside directory expected after a crash: %v", asides)
			}
			restored := map[string]bool{}
			for _, sfx := range restoreOrder[:c.restored] {
				restored[sfx] = true
			}
			for sfx, want := range orig {
				atOrig, _ := os.ReadFile(db + sfx)
				atAside, _ := os.ReadFile(filepath.Join(asides[0], nameOf[sfx]))
				inOrig, inAside := bytes.Equal(atOrig, want), bytes.Equal(atAside, want)
				if inOrig == inAside { // exactly one place
					t.Fatalf("%q: its original bytes must exist in exactly one place (original path %v, aside %v)", sfx, inOrig, inAside)
				}
				// -shm was never moved; the others are at their original path exactly when put back.
				wantOrig := sfx == "-shm" || restored[sfx]
				if inOrig != wantOrig {
					t.Fatalf("%q: at the original path = %v, want %v", sfx, inOrig, wantOrig)
				}
			}
			// What a later restore can see. Before the main file is back there is no main file and
			// the companions at their original names are reported as orphans (it refuses); once it is
			// back, the original database is whole again.
			sc, err := ActiveSidecars(db)
			if err != nil {
				t.Fatal(err)
			}
			if c.restored == 3 {
				if !sc.MainExists || len(sc.OrphanNonEmpty()) != 0 {
					t.Fatalf("the original database is whole again: %+v", sc)
				}
				return
			}
			var wantOrphans []string
			if restored["-wal"] {
				wantOrphans = append(wantOrphans, "-wal")
			}
			if restored["-journal"] {
				wantOrphans = append(wantOrphans, "-journal")
			}
			wantOrphans = append(wantOrphans, "-shm")
			if sc.MainExists || fmt.Sprint(sc.OrphanNonEmpty()) != fmt.Sprint(wantOrphans) {
				t.Fatalf("main exists %v, orphans %v, want none and %v", sc.MainExists, sc.OrphanNonEmpty(), wantOrphans)
			}
		})
	}
}

// ---- temporary file ownership --------------------------------------------------------------------------------------

func TestDisplace_Abandon_RemovesOnlyTheFileThisOperationCreated(t *testing.T) {
	t.Parallel()
	t.Run("removes its own file", func(t *testing.T) {
		t.Parallel()
		e := newDS(t, false, false, false)
		repl, err := newDisplacer().prepare(e.db, e.cand)
		if err != nil {
			t.Fatal(err)
		}
		if err := repl.Abandon(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(repl.path); !os.IsNotExist(err) {
			t.Fatalf("still there: %v", err)
		}
	})
	t.Run("leaves a different file at the same path", func(t *testing.T) {
		t.Parallel()
		e := newDS(t, false, false, false)
		repl, err := newDisplacer().prepare(e.db, e.cand)
		if err != nil {
			t.Fatal(err)
		}
		other := repl.path + ".other"
		pvWrite(t, other, []byte("not ours"))
		if err := os.Rename(other, repl.path); err != nil {
			t.Fatal(err)
		}
		if err := repl.Abandon(); err == nil {
			t.Fatal("must report that it left the file")
		}
		if got, _ := os.ReadFile(repl.path); string(got) != "not ours" {
			t.Fatal("a file this operation did not create was removed")
		}
	})
	t.Run("does nothing after installation", func(t *testing.T) {
		t.Parallel()
		e := newDS(t, false, false, false)
		d := newDisplacer()
		repl := e.prep(d)
		path := repl.path
		if _, err := d.replace(e.db, repl, e.base()); err != nil {
			t.Fatal(err)
		}
		pvWrite(t, path, []byte("created later by someone else"))
		if err := repl.Abandon(); err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(path); string(got) != "created later by someone else" {
			t.Fatal("Abandon removed a file after the replacement was installed")
		}
	})
}

// ---- the lock guard ---------------------------------------------------------------------------------------------------

func TestDisplace_EveryMethodRequiresAHeldLockForThisPath(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, false, false)
	repl := e.prep(newDisplacer())
	b := e.base()
	released, err := AcquireExclusiveCentralLock(filepath.Join(pvDir(t), "x.db"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = released.Close()
	otherDB := filepath.Join(pvDir(t), "other.db")
	pvWrite(t, otherDB, pvBytes(10))
	wrongPath, err := AcquireExclusiveCentralLock(otherDB, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer wrongPath.Close()
	var nilLock *RestoreLock
	before := pvStates(t, e.db)
	for name, lock := range map[string]*RestoreLock{"nil": nilLock, "released": released, "wrong path": wrongPath} {
		if _, err := lock.PrepareReplacement(e.db, e.cand); err == nil {
			t.Errorf("%s: PrepareReplacement accepted", name)
		}
		if _, err := lock.BaselineAfterClose(e.db); err == nil {
			t.Errorf("%s: BaselineAfterClose accepted", name)
		}
		if _, err := lock.BaselineNoActive(e.db); err == nil {
			t.Errorf("%s: BaselineNoActive accepted", name)
		}
		if res, err := lock.ReplaceActiveDatabase(e.db, repl, b); err == nil || res.Installed {
			t.Errorf("%s: ReplaceActiveDatabase accepted", name)
		}
	}
	pvSame(t, before, pvStates(t, e.db))
	if len(e.asides()) != 0 {
		t.Fatalf("a refused call moved files: %v", e.asides())
	}
	// And the right lock works.
	if _, err := e.lock.ReplaceActiveDatabase(e.db, repl, b); err != nil {
		t.Fatal(err)
	}
}

// ---- real crashes at every step ------------------------------------------------------------------------------------------

// TestHelperProcessDisplace is not a real test: it runs only when
// DSO_TEST_DISPLACE_KILL is set. It displaces and installs, and SIGKILLs itself
// at the named step.
func TestHelperProcessDisplace(t *testing.T) {
	step := os.Getenv("DSO_TEST_DISPLACE_KILL")
	if step == "" {
		t.Skip("helper process")
	}
	db, cand := os.Getenv("DSO_TEST_DISPLACE_DB"), os.Getenv("DSO_TEST_DISPLACE_CAND")
	lock, err := AcquireExclusiveCentralLock(db, 3*time.Second)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	d := newDisplacer()
	repl, err := d.prepare(db, cand)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	b, err := lock.BaselineAfterClose(db)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	d.hook = func(s string) {
		if s == step {
			_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
			select {}
		}
	}
	_, err = d.replace(db, repl, b)
	fmt.Fprintln(os.Stderr, "the child was supposed to be killed at", step, "but finished:", err)
	os.Exit(3)
}

func TestDisplace_CrashAtEveryStep_NeverLosesAnOriginalByte_AndIsDetectable(t *testing.T) {
	t.Parallel()
	steps := []string{"verified-baseline", "prepared-verified", "aside-created", "aside-parent-synced", "main-moved", "wal-moved",
		"journal-moved", "shm-moved", "verified-displaced", "synced-before-install", "before-install", "installed", "install-verified", "final-synced", "final-rechecked"}
	mainMovedAt := map[string]bool{}
	for _, n := range []string{"main-moved", "wal-moved", "journal-moved", "shm-moved", "verified-displaced", "synced-before-install", "before-install"} {
		mainMovedAt[n] = true // main-moved .. before-install: the path holds no main file
	}
	for _, step := range steps {
		t.Run(step, func(t *testing.T) {
			t.Parallel()
			dir, db := pvDB(t, true, true, true)
			cand := filepath.Join(pvDir(t), "cand.bin")
			candBytes := pvBytes(5000)
			pvWrite(t, cand, candBytes)
			orig := map[string][]byte{}
			for _, sfx := range []string{"", "-wal", "-shm", "-journal"} {
				b, err := os.ReadFile(db + sfx)
				if err != nil {
					t.Fatal(err)
				}
				orig[sfx] = b
			}
			c := exec.Command(os.Args[0], "-test.run=^TestHelperProcessDisplace$") // #nosec G204 -- this test binary
			c.Env = append(os.Environ(), "DSO_TEST_DISPLACE_KILL="+step, "DSO_TEST_DISPLACE_DB="+db, "DSO_TEST_DISPLACE_CAND="+cand)
			c.Stderr = os.Stderr
			err := c.Run()
			var ee *exec.ExitError
			if !errors.As(err, &ee) {
				t.Fatalf("expected the child to be killed, got %v", err)
			}
			if ws, ok := ee.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
				t.Fatalf("expected SIGKILL, got %v", err)
			}

			// Every original file's bytes exist exactly once, at its path or in the aside directory.
			var files []string
			ents, _ := os.ReadDir(dir)
			for _, en := range ents {
				if en.Type().IsRegular() {
					files = append(files, filepath.Join(dir, en.Name()))
				}
			}
			asides, _ := filepath.Glob(db + ".replaced-*")
			for _, a := range asides {
				ae, _ := os.ReadDir(a)
				for _, en := range ae {
					files = append(files, filepath.Join(a, en.Name()))
				}
			}
			for sfx, want := range orig {
				found := 0
				for _, f := range files {
					if got, err := os.ReadFile(f); err == nil && bytes.Equal(got, want) {
						found++
					}
				}
				if found != 1 {
					t.Fatalf("original %q: its bytes were found %d times, want exactly once (files %v)", sfx, found, files)
				}
			}

			got, mainErr := os.ReadFile(db)
			switch {
			case step == "installed" || step == "install-verified" || step == "final-synced" || step == "final-rechecked":
				if mainErr != nil || !bytes.Equal(got, candBytes) {
					t.Fatalf("the replacement must be installed (%v)", mainErr)
				}
			case mainMovedAt[step]:
				if !os.IsNotExist(mainErr) {
					t.Fatalf("the main file has moved, the path must be empty (%v)", mainErr)
				}
				// A state a later restore can detect and refuse: no main file, and the
				// companions still at their original names are reported as orphans.
				sc, err := ActiveSidecars(db)
				if err != nil {
					t.Fatal(err)
				}
				var want []string
				if step == "main-moved" {
					want = []string{"-wal", "-journal", "-shm"}
				} else if step == "wal-moved" {
					want = []string{"-journal", "-shm"}
				} else if step == "journal-moved" {
					want = []string{"-shm"}
				}
				if fmt.Sprint(sc.OrphanNonEmpty()) != fmt.Sprint(want) && !(len(want) == 0 && len(sc.OrphanNonEmpty()) == 0) {
					t.Fatalf("orphans %v, want %v", sc.OrphanNonEmpty(), want)
				}
			default:
				if mainErr != nil || !bytes.Equal(got, orig[""]) {
					t.Fatalf("before the main file moves it must be untouched (%v)", mainErr)
				}
			}
		})
	}
}

// ---- Phase C: post-install verification and unknown install outcomes ------------------------------------

type instFlipWriter struct {
	w    io.Writer
	done bool
}

func (f *instFlipWriter) Write(p []byte) (int, error) {
	if !f.done && len(p) > 0 {
		f.done = true
		q := append([]byte(nil), p...)
		q[0] ^= 0xff
		return f.w.Write(q)
	}
	return f.w.Write(p)
}

type instFlipReader struct {
	r    io.Reader
	done bool
}

func (f *instFlipReader) Read(p []byte) (int, error) {
	n, err := f.r.Read(p)
	if !f.done && n > 0 {
		f.done = true
		p[0] ^= 0xff
	}
	return n, err
}

type instEOFReader struct {
	r     io.Reader
	onEOF func()
}

func (f *instEOFReader) Read(p []byte) (int, error) {
	n, err := f.r.Read(p)
	if err == io.EOF && f.onEOF != nil {
		f.onEOF()
		f.onEOF = nil
	}
	return n, err
}

func instAfterInstall(d *displacer, f func()) {
	d.hook = func(step string) {
		if step == "installed" {
			f()
		}
	}
}

func instAppend(t *testing.T, path string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
}

// instMoveAway moves the file at path aside (it keeps its inode) and returns where.
func instMoveAway(t *testing.T, path string) string {
	t.Helper()
	to := path + ".taken"
	if err := os.Rename(path, to); err != nil {
		t.Fatal(err)
	}
	return to
}

func TestDisplace_Prepare_RecordsTheSHA256AndLengthOfTheBytesWritten(t *testing.T) {
	t.Parallel()
	e := newDS(t, false, false, false)
	repl := e.prep(newDisplacer())
	sum := sha256.Sum256(e.candBytes())
	if repl.hash != hex.EncodeToString(sum[:]) || repl.size != int64(len(e.candBytes())) {
		t.Fatalf("recorded hash %s size %d", repl.hash, repl.size)
	}
}

func TestDisplace_InstalledAndVerified_IsReportedVerified(t *testing.T) {
	t.Parallel()
	for _, active := range []bool{true, false} {
		t.Run(fmt.Sprintf("active=%v", active), func(t *testing.T) {
			t.Parallel()
			e := newDS(t, active, active, active)
			if !active {
				for _, sfx := range []string{"", "-wal", "-shm", "-journal"} {
					_ = os.Remove(e.db + sfx)
				}
			}
			d := newDisplacer()
			repl := e.prep(d)
			var b *Baseline
			if active {
				b = e.base()
			} else {
				var err error
				if b, err = e.lock.BaselineNoActive(e.db); err != nil {
					t.Fatal(err)
				}
			}
			var steps []string
			d.hook = func(s string) { steps = append(steps, s) }
			res, err := d.replace(e.db, repl, b)
			if err != nil || !res.Installed || !res.Verified {
				t.Fatalf("res %+v err %v", res, err)
			}
			e.assertDBIsCandidate()
			tail := steps[len(steps)-4:]
			if fmt.Sprint(tail) != "[installed install-verified final-synced final-rechecked]" {
				t.Fatalf("steps %v", steps)
			}
		})
	}
}

// Every way the file at the active path can fail the post-install check: the
// replacement stays installed, nothing is rolled back (the path is occupied and the
// originals stay in the aside directory), and the error is typed and says which.
func TestDisplace_PostInstallVerification_EveryFailureIsTypedAndNeverRolledBack(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		kind     InstalledFileKind
		mismatch bool
		setup    func(t *testing.T, e *dsEnv, d *displacer)
	}{
		{"byte flipped in place (same size)", InstalledContentChanged, true, func(t *testing.T, e *dsEnv, d *displacer) {
			instAfterInstall(d, func() {
				f, err := os.OpenFile(e.db, os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				if _, err := f.WriteAt([]byte{0xAB}, 100); err != nil {
					t.Fatal(err)
				}
			})
		}},
		{"truncated", InstalledContentChanged, true, func(t *testing.T, e *dsEnv, d *displacer) {
			instAfterInstall(d, func() {
				if err := os.Truncate(e.db, 10); err != nil {
					t.Fatal(err)
				}
			})
		}},
		{"appended to", InstalledContentChanged, true, func(t *testing.T, e *dsEnv, d *displacer) {
			instAfterInstall(d, func() { instAppend(t, e.db) })
		}},
		{"read back differently", InstalledContentChanged, true, func(t *testing.T, e *dsEnv, d *displacer) {
			d.wrapReader = func(r io.Reader) io.Reader { return &instFlipReader{r: r} }
		}},
		{"replaced by a different file", InstalledReplaced, true, func(t *testing.T, e *dsEnv, d *displacer) {
			instAfterInstall(d, func() {
				instMoveAway(t, e.db)
				pvWrite(t, e.db, []byte("some other database"))
			})
		}},
		{"replaced by a byte-identical copy", InstalledReplaced, true, func(t *testing.T, e *dsEnv, d *displacer) {
			instAfterInstall(d, func() {
				instMoveAway(t, e.db)
				pvWrite(t, e.db, e.candBytes())
			})
		}},
		{"removed", InstalledGone, true, func(t *testing.T, e *dsEnv, d *displacer) {
			instAfterInstall(d, func() {
				if err := os.Remove(e.db); err != nil {
					t.Fatal(err)
				}
			})
		}},
		{"replaced by a symlink", InstalledNotRegular, true, func(t *testing.T, e *dsEnv, d *displacer) {
			instAfterInstall(d, func() {
				to := instMoveAway(t, e.db)
				if err := os.Symlink(to, e.db); err != nil {
					t.Fatal(err)
				}
			})
		}},
		{"replaced by a directory", InstalledNotRegular, true, func(t *testing.T, e *dsEnv, d *displacer) {
			instAfterInstall(d, func() {
				instMoveAway(t, e.db)
				if err := os.Mkdir(e.db, 0o700); err != nil {
					t.Fatal(err)
				}
			})
		}},
		{"changes while it is being read", InstalledUnstable, true, func(t *testing.T, e *dsEnv, d *displacer) {
			d.wrapReader = func(r io.Reader) io.Reader {
				return &instEOFReader{r: r, onEOF: func() { instAppend(t, e.db) }}
			}
		}},
		{"changes while the directory is synced", InstalledUnstable, true, func(t *testing.T, e *dsEnv, d *displacer) {
			armed := false
			d.hook = func(step string) {
				if step == "install-verified" {
					armed = true
				}
			}
			d.syncDir = func(dir string) error {
				if armed && dir == e.dir {
					if err := os.Chtimes(e.db, time.Unix(1, 0), time.Unix(1, 0)); err != nil {
						t.Fatal(err)
					}
				}
				return fsyncDir(dir)
			}
		}},
		{"swapped while the directory is synced", InstalledReplaced, true, func(t *testing.T, e *dsEnv, d *displacer) {
			armed := false
			d.hook = func(step string) {
				if step == "install-verified" {
					armed = true
				}
			}
			d.syncDir = func(dir string) error {
				if armed && dir == e.dir {
					armed = false
					instMoveAway(t, e.db)
					pvWrite(t, e.db, e.candBytes())
				}
				return fsyncDir(dir)
			}
		}},
		{"cannot be opened (unverified, not a mismatch)", InstalledUnreadable, false, func(t *testing.T, e *dsEnv, d *displacer) {
			d.openInstalled = func(string) (*os.File, error) {
				return nil, &os.PathError{Op: "open", Path: e.db, Err: syscall.EACCES}
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newDS(t, true, true, true)
			d := newDisplacer()
			var calls dsCalls
			d.rename = calls.renamer(nil)
			tc.setup(t, e, d)
			repl := e.prep(d)
			res, err := d.replace(e.db, repl, e.base())
			var ice *InstalledFileChangedError
			if !errors.As(err, &ice) {
				t.Fatalf("want *InstalledFileChangedError, got %T %v", err, err)
			}
			var de *DisplacementError
			if errors.As(err, &de) {
				t.Fatal("an installed file that fails verification is not a displacement failure")
			}
			if ice.Kind != tc.kind || ice.Mismatch() != tc.mismatch || ice.Path != e.db {
				t.Fatalf("kind %q mismatch %v path %s: %v", ice.Kind, ice.Mismatch(), ice.Path, err)
			}
			if !res.Installed || res.Verified {
				t.Fatalf("installed but not verified: %+v", res)
			}
			// No rollback of any kind: the originals are all still in the aside directory.
			for _, c := range calls.list {
				for _, sfx := range []string{"", "-wal", "-shm", "-journal"} {
					if e.back(c[0], c[1], sfx) {
						t.Fatalf("a rollback move was issued: %v", c)
					}
				}
			}
			e.assertRetained(res.Dir, "main", "wal", "journal", "shm")
			for _, sfx := range []string{"-wal", "-shm", "-journal"} {
				if _, err := os.Lstat(e.db + sfx); !os.IsNotExist(err) {
					t.Fatalf("an original %s was put back", sfx)
				}
			}
			if !repl.installed {
				t.Fatal("the replacement was installed")
			}
		})
	}
}

func TestDisplace_PostInstallVerification_ParentSyncFailureIsCarriedAlongsideIt(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, true, true)
	d := newDisplacer()
	instAfterInstall(d, func() { _ = os.Remove(e.db) })
	d.syncDir = e.syncFailing("parent", 3, errInjected)
	repl := e.prep(d)
	res, err := d.replace(e.db, repl, e.base())
	var ice *InstalledFileChangedError
	if !errors.As(err, &ice) || ice.Kind != InstalledGone || !errors.Is(ice.SyncErr, errInjected) {
		t.Fatalf("%T %v", err, err)
	}
	if !dsHas(err.Error(), "syncing the parent directory also failed") || res.Verified || !res.Installed {
		t.Fatalf("res %+v err %v", res, err)
	}
}

func TestDisplace_InstalledAndVerified_ButParentSyncFailed_IsVerifiedWithADurabilityError(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, true, true)
	d := newDisplacer()
	repl := e.prep(d)
	d.syncDir = e.syncFailing("parent", 3, errInjected)
	res, err := d.replace(e.db, repl, e.base())
	var ide *InstallDurabilityError
	if !errors.As(err, &ide) || !res.Installed || !res.Verified {
		t.Fatalf("res %+v err %T %v", res, err, err)
	}
	e.assertDBIsCandidate()
}

// The install rename reports an error: the filesystem decides between installed,
// definitely not installed, and unknown.
func TestDisplace_InstallRenameError_UnknownOutcome_IsNeverRolledBackAndNeverGuessed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		setup func(t *testing.T, e *dsEnv, d *displacer, calls *dsCalls)
	}{
		{"took effect, then the file vanished", func(t *testing.T, e *dsEnv, d *displacer, calls *dsCalls) {
			d.rename = calls.renamerAfter(func(n int, a, b string) error {
				if b == e.db {
					_ = os.Remove(e.db)
					return errInjected
				}
				return nil
			})
		}},
		{"took effect, then another file took the path", func(t *testing.T, e *dsEnv, d *displacer, calls *dsCalls) {
			d.rename = calls.renamerAfter(func(n int, a, b string) error {
				if b == e.db {
					_ = os.Remove(e.db)
					pvWrite(t, e.db, []byte("someone else"))
					return errInjected
				}
				return nil
			})
		}},
		{"failed, and the active path cannot be examined", func(t *testing.T, e *dsEnv, d *displacer, calls *dsCalls) {
			d.rename = calls.renamer(func(n int, a, b string) error {
				if b == e.db {
					return errInjected
				}
				return nil
			})
			d.statInstalled = func(path string) (os.FileInfo, error) {
				if path == e.db {
					return nil, &os.PathError{Op: "lstat", Path: path, Err: syscall.EIO}
				}
				return os.Lstat(path)
			}
		}},
		{"failed, and the prepared file is gone from its path", func(t *testing.T, e *dsEnv, d *displacer, calls *dsCalls) {
			d.rename = calls.renamer(func(n int, a, b string) error {
				if b == e.db {
					_ = os.Remove(a)
					return errInjected
				}
				return nil
			})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newDS(t, true, true, true)
			d := newDisplacer()
			var calls dsCalls
			tc.setup(t, e, d, &calls)
			repl := e.prep(d)
			res, err := d.replace(e.db, repl, e.base())
			de := asDisplacement(t, err)
			if de.Install != InstallUnknown || !errors.Is(err, ErrDisplaceInstallOutcomeUnknown) || !errors.Is(err, errInjected) {
				t.Fatalf("install %q: %v", de.Install, err)
			}
			if de.RolledBack || de.OriginalInPlace() || res.Installed || res.Verified {
				t.Fatalf("an unknown outcome is not a rollback and not an installation: %+v res %+v", de, res)
			}
			for _, c := range calls.list {
				for _, sfx := range []string{"", "-wal", "-shm", "-journal"} {
					if e.back(c[0], c[1], sfx) {
						t.Fatalf("a rollback move was issued although the install outcome is unknown: %v", c)
					}
				}
			}
			if len(de.Retained) != 4 || de.Dir == "" {
				t.Fatalf("the originals are retained and reported: %+v", de)
			}
			e.assertRetained(de.Dir, "main", "wal", "journal", "shm")
			if !repl.finished || repl.Abandon() != nil {
				t.Fatal("the temporary file must be left alone (it may be the installed database)")
			}
			for _, want := range []string{"NOT rolled back", "may be installed", de.Dir} {
				if !dsHas(err.Error(), want) {
					t.Fatalf("the error must say %q: %v", want, err)
				}
			}
			if dsHas(err.Error(), "rolled back: every original") {
				t.Fatalf("must not claim a rollback: %v", err)
			}
		})
	}
}

func TestDisplace_InstallRenameError_NotInstalled_IsRolledBackAndSaysSo(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, true, true)
	d := newDisplacer()
	var calls dsCalls
	var repl *Replacement
	d.rename = calls.renamer(func(n int, a, b string) error {
		if repl != nil && a == repl.path {
			return errInjected // before any effect: the prepared file stays where it is
		}
		return nil
	})
	repl = e.prep(d)
	res, err := d.replace(e.db, repl, e.base())
	de := asDisplacement(t, err)
	if de.Install != InstallNotInstalled || !de.RolledBack || res.Installed {
		t.Fatalf("%+v res %+v", de, res)
	}
	e.assertAllInPlace()
}

func TestDisplace_InstallBlocked_ByAForeignFile_IsNotInstalledEvenThoughTheRollbackIsRefused(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, true, true)
	d := newDisplacer()
	repl := e.prep(d)
	d.beforeInstall = func() { pvWrite(t, e.db, []byte("someone else's database")) }
	_, err := d.replace(e.db, repl, e.base())
	de := asDisplacement(t, err)
	if de.Install != InstallNotInstalled || de.RolledBack {
		t.Fatalf("%+v", de)
	}
}

func TestDisplace_InstallRenameError_ThatTookEffect_IsVerifiedLikeAnyInstall(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, true, true)
	d := newDisplacer()
	repl := e.prep(d)
	var calls dsCalls
	d.rename = calls.renamerAfter(func(n int, a, b string) error {
		if b == e.db {
			return errInjected
		}
		return nil
	})
	res, err := d.replace(e.db, repl, e.base())
	if err != nil || !res.Installed || !res.Verified {
		t.Fatalf("res %+v err %v", res, err)
	}
}

// ---- Phase C.1 ---------------------------------------------------------------------------------------

// F1: with no active database there is nothing displaced, so Main is MainNone and
// nothing is retained. When the install outcome is unknown that must still never
// read as "the original is in place".
func TestDisplace_InstallOutcomeUnknown_NoActiveDatabase_OriginalIsNeverReportedInPlace(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, true, true)
	for sfx := range e.orig {
		if err := os.Remove(e.db + sfx); err != nil {
			t.Fatal(err)
		}
	}
	b, err := e.lock.BaselineNoActive(e.db)
	if err != nil {
		t.Fatal(err)
	}
	d := newDisplacer()
	repl := e.prep(d)
	var calls dsCalls
	d.rename = calls.renamerAfter(func(n int, a, bb string) error {
		if bb == e.db {
			_ = os.Remove(e.db) // took effect, then the file vanished: not knowable
			return errInjected
		}
		return nil
	})
	res, err := d.replace(e.db, repl, b)
	de := asDisplacement(t, err)
	if de.Install != InstallUnknown || de.Main != MainNone || len(de.Retained) != 0 || res.Installed {
		t.Fatalf("setup: %+v res %+v", de, res)
	}
	if de.OriginalInPlace() {
		t.Fatal("an unknown install outcome must never be reported as 'the original is in place'")
	}
	if de.RolledBack {
		t.Fatal("an unknown install outcome is never a rollback")
	}
}

func TestDisplacementError_OriginalInPlace_OnlyForConfirmedStates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		de   DisplacementError
		want bool
	}{
		{"untouched", DisplacementError{Main: MainUntouched, Install: InstallNotInstalled}, true},
		{"back at original", DisplacementError{Main: MainAtOriginal, Install: InstallNotInstalled}, true},
		{"no main file", DisplacementError{Main: MainNone, Install: InstallNotInstalled}, true},
		{"zero value (not from a displacement)", DisplacementError{}, false},
		{"unknown outcome, no main file", DisplacementError{Main: MainNone, Install: InstallUnknown}, false},
		{"unknown outcome, main untouched", DisplacementError{Main: MainUntouched, Install: InstallUnknown}, false},
		{"unknown outcome, main at original", DisplacementError{Main: MainAtOriginal, Install: InstallUnknown}, false},
		{"main retained", DisplacementError{Main: MainRetained, Install: InstallNotInstalled}, false},
		{"main unknown", DisplacementError{Main: MainUnknown}, false},
		{"main vanished", DisplacementError{Main: MainVanished}, false},
		{"companion retained", DisplacementError{Main: MainAtOriginal, Retained: []DisplacedFile{{Name: "wal"}}}, false},
		{"companion unknown", DisplacementError{Main: MainAtOriginal, Unknown: []string{"x"}}, false},
		{"companion restored in place", DisplacementError{Main: MainAtOriginal, RestoredInPlace: []string{"x"}}, false},
	}
	for _, tc := range cases {
		if got := tc.de.OriginalInPlace(); got != tc.want {
			t.Errorf("%s: OriginalInPlace() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// ---- pre-displacement verification of the prepared replacement (F2) --------------------------------------

type instFailReader struct{ err error }

func (f *instFailReader) Read([]byte) (int, error) { return 0, f.err }

// Every way the prepared replacement can be bad BEFORE anything is moved: nothing is
// displaced, no rename of any kind is issued, the original database and every
// companion stay byte for byte where they were, an unexpected file at the temporary
// path is neither deleted nor overwritten, and the error is typed and discoverable.
func TestDisplace_PreparedReplacementBad_IsRefusedBeforeAnythingIsMoved(t *testing.T) {
	t.Parallel()
	foreign := []byte(foreignBody)
	cases := []struct {
		name    string
		active  bool
		setup   func(t *testing.T, e *dsEnv, d *displacer, repl *Replacement)
		foreign bool // an unexpected file sits at the temporary path afterwards
		want    string
	}{
		{"same-size corruption with the mtime restored", true, func(t *testing.T, e *dsEnv, d *displacer, repl *Replacement) {
			before, err := os.Stat(repl.path)
			if err != nil {
				t.Fatal(err)
			}
			f, err := os.OpenFile(repl.path, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteAt([]byte{0xAB, 0xCD}, 100); err != nil {
				t.Fatal(err)
			}
			_ = f.Close()
			if err := os.Chtimes(repl.path, before.ModTime(), before.ModTime()); err != nil {
				t.Fatal(err)
			}
		}, false, "stat signature differs"},
		{"content differs but the file looks untouched (hash only)", true, func(t *testing.T, e *dsEnv, d *displacer, repl *Replacement) {
			d.wrapPreparedReader = func(r io.Reader) io.Reader { return &instFlipReader{r: r} }
		}, false, "is not what was written"},
		{"write altered on its way to the file during preparation", true, func(t *testing.T, e *dsEnv, d *displacer, repl *Replacement) {
			// handled below: the corruption is injected before prepare
		}, false, "is not what was written"},
		{"substituted by a different file", true, func(t *testing.T, e *dsEnv, d *displacer, repl *Replacement) {
			other := repl.path + ".other"
			pvWrite(t, other, foreign)
			if err := os.Rename(other, repl.path); err != nil {
				t.Fatal(err)
			}
		}, true, "is not the file that was prepared"},
		{"substituted by a byte-identical copy", true, func(t *testing.T, e *dsEnv, d *displacer, repl *Replacement) {
			other := repl.path + ".other"
			pvWrite(t, other, e.candBytes())
			if err := os.Rename(other, repl.path); err != nil {
				t.Fatal(err)
			}
		}, true, "is not the file that was prepared"},
		{"replaced by a symlink", true, func(t *testing.T, e *dsEnv, d *displacer, repl *Replacement) {
			target := repl.path + ".target"
			pvWrite(t, target, foreign)
			if err := os.Remove(repl.path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, repl.path); err != nil {
				t.Fatal(err)
			}
		}, false, "not a regular file"},
		{"removed", true, func(t *testing.T, e *dsEnv, d *displacer, repl *Replacement) {
			if err := os.Remove(repl.path); err != nil {
				t.Fatal(err)
			}
		}, false, "no longer exists"},
		{"mode widened (group/other access)", true, func(t *testing.T, e *dsEnv, d *displacer, repl *Replacement) {
			if err := os.Chmod(repl.path, 0o644); err != nil {
				t.Fatal(err)
			}
		}, false, "could not be opened securely"},
		{"cannot be read", true, func(t *testing.T, e *dsEnv, d *displacer, repl *Replacement) {
			d.wrapPreparedReader = func(io.Reader) io.Reader { return &instFailReader{err: syscall.EIO} }
		}, false, "could not be read"},
		{"no active database: substituted", false, func(t *testing.T, e *dsEnv, d *displacer, repl *Replacement) {
			other := repl.path + ".other"
			pvWrite(t, other, foreign)
			if err := os.Rename(other, repl.path); err != nil {
				t.Fatal(err)
			}
		}, true, "is not the file that was prepared"},
		{"no active database: removed", false, func(t *testing.T, e *dsEnv, d *displacer, repl *Replacement) {
			if err := os.Remove(repl.path); err != nil {
				t.Fatal(err)
			}
		}, false, "no longer exists"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newDS(t, true, true, true)
			d := newDisplacer()
			if tc.name == "write altered on its way to the file during preparation" {
				d.wrapWriter = func(w io.Writer) io.Writer { return &instFlipWriter{w: w} }
			}
			repl := e.prep(d)
			var b *Baseline
			if tc.active {
				b = e.base()
			} else {
				for sfx := range e.orig {
					if err := os.Remove(e.db + sfx); err != nil {
						t.Fatal(err)
					}
				}
				var err error
				if b, err = e.lock.BaselineNoActive(e.db); err != nil {
					t.Fatal(err)
				}
			}
			tc.setup(t, e, d, repl)
			var calls dsCalls
			d.rename = calls.renamer(nil)
			var syncs []string
			d.syncDir = func(dir string) error { syncs = append(syncs, dir); return fsyncDir(dir) }
			var steps []string
			d.hook = func(s string) { steps = append(steps, s) }

			res, err := d.replace(e.db, repl, b)
			if err == nil || res.Installed || res.Verified {
				t.Fatalf("a bad prepared replacement must be refused: res %+v err %v", res, err)
			}
			if !errors.Is(err, ErrDisplaceReplacementChanged) {
				t.Fatalf("errors.Is(ErrDisplaceReplacementChanged) must hold: %v", err)
			}
			de := asDisplacement(t, err)
			if de.Stage != "prepared-replacement" || de.Install != InstallNotInstalled {
				t.Fatalf("stage %q install %q", de.Stage, de.Install)
			}
			if !dsHas(err.Error(), tc.want) || !dsHas(err.Error(), "untouched") {
				t.Fatalf("the error must say %q and that the originals are untouched: %v", tc.want, err)
			}
			// Nothing was displaced: no rename, no sync, no aside directory, no step past the check.
			if len(calls.list) != 0 || len(syncs) != 0 || len(e.asides()) != 0 || len(de.Retained) != 0 || res.Dir != "" || len(res.Files) != 0 {
				t.Fatalf("nothing may be moved before the check passes: renames %v syncs %v asides %v", calls.list, syncs, e.asides())
			}
			for _, s := range steps {
				if s == "prepared-verified" || s == "aside-created" || s == "main-moved" {
					t.Fatalf("a step after the check was reached: %v", steps)
				}
			}
			if tc.active {
				e.assertAllInPlace()
				if !de.RolledBack || de.Main != MainUntouched {
					t.Fatalf("%+v", de)
				}
			} else if _, statErr := os.Lstat(e.db); !os.IsNotExist(statErr) {
				t.Fatalf("nothing may be installed (%v)", statErr)
			}
			if tc.foreign {
				if got, rerr := os.ReadFile(repl.path); rerr != nil || !bytes.Equal(got, foreignWant(tc.name, e)) {
					t.Fatalf("the unexpected file at the temporary path must not be deleted or overwritten (%v)", rerr)
				}
			}
			// Abandon never removes a file this operation did not create.
			if err := repl.Abandon(); tc.foreign && err == nil {
				t.Fatal("Abandon must refuse to remove a substituted file")
			}
			if tc.foreign {
				if _, serr := os.Lstat(repl.path); serr != nil {
					t.Fatalf("the unexpected file was removed by Abandon: %v", serr)
				}
			}
		})
	}
}

// foreignWant is the content the unexpected file at the temporary path must keep.
func foreignWant(name string, e *dsEnv) []byte {
	if name == "substituted by a byte-identical copy" {
		return e.candBytes()
	}
	return []byte(foreignBody)
}

const foreignBody = "a foreign file that must survive"

// The check passes for an untouched prepared replacement and sits between the
// baseline check and the first displacement.
func TestDisplace_PreparedReplacementVerified_IsTheStepBeforeAnyDisplacement(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, true, true)
	d := newDisplacer()
	repl := e.prep(d)
	var steps []string
	d.hook = func(s string) { steps = append(steps, s) }
	res, err := d.replace(e.db, repl, e.base())
	if err != nil || !res.Verified {
		t.Fatalf("res %+v err %v", res, err)
	}
	idx := func(name string) int {
		for i, s := range steps {
			if s == name {
				return i
			}
		}
		return -1
	}
	if !(idx("prepared-verified") >= 0 && idx("prepared-verified") < idx("aside-created") && idx("aside-created") < idx("main-moved") &&
		idx("main-moved") < idx("installed") && idx("installed") < idx("install-verified") && idx("install-verified") < idx("final-rechecked")) {
		t.Fatalf("unexpected order: %v", steps)
	}
}

// The post-install verification is unchanged: a prepared file that passes the new
// check but changes afterwards is still caught after the installation.
func TestDisplace_PostInstallVerification_StillCatchesCorruptionAfterThePreparedCheckPassed(t *testing.T) {
	t.Parallel()
	e := newDS(t, true, true, true)
	d := newDisplacer()
	repl := e.prep(d)
	d.wrapReader = func(r io.Reader) io.Reader { return &instFlipReader{r: r} } // only the installed read is altered
	res, err := d.replace(e.db, repl, e.base())
	var ice *InstalledFileChangedError
	if !errors.As(err, &ice) || ice.Kind != InstalledContentChanged || !res.Installed || res.Verified {
		t.Fatalf("res %+v err %T %v", res, err, err)
	}
}
