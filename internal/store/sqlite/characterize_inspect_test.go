//go:build unix

package sqlite

// Phase B-prime characterization tests (Restore remediation, Phase 0 follow-up).
//
// These tests OBSERVE what SQLite and the DSO driver actually do to the files of
// a database during read-only inspection and during Open. They change no
// production code and are not meant to make an expected result come true: where
// the behaviour is certain they assert it strictly; where it is a prediction they
// log what was observed and fail loudly ("PREDICTION CONTRADICTED") if reality
// differs, so the result is reported rather than hidden.
//
// Strict invariants (must always hold): inspection never changes the main file or
// a non-empty -wal (content AND stat signature), and never removes anything.
// Permitted advisory changes: the -shm may be rewritten, and SQLite may create an
// EMPTY -wal beside an -shm-only database.

import (
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"syscall"
	"testing"

	"github.com/docker-secret-operator/dso/internal/securefs/securefstest"
)

// chFile is everything about one file that an inspection must leave alone.
type chFile struct {
	sig  statSig
	hash string
}

// chSnap snapshots every file in dir except the central lock sidecar.
func chSnap(t *testing.T, dir string) map[string]chFile {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]chFile{}
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".central.lock") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		fi, err := os.Lstat(p)
		if err != nil {
			t.Fatal(err)
		}
		f := chFile{sig: statSigOf(fi)}
		if fi.Mode().IsRegular() {
			f.hash = fileHash(t, p)
		}
		out[e.Name()] = f
	}
	return out
}

// chDiff names what differs between two snapshots, sorted.
func chDiff(before, after map[string]chFile) (changed, added, removed []string) {
	for n, b := range before {
		a, ok := after[n]
		switch {
		case !ok:
			removed = append(removed, n)
		case a != b:
			changed = append(changed, n)
		}
	}
	for n := range after {
		if _, ok := before[n]; !ok {
			added = append(added, n)
		}
	}
	sort.Strings(changed)
	sort.Strings(added)
	sort.Strings(removed)
	return changed, added, removed
}

// chCrashWAL leaves a real, non-empty -wal (200 committed rows in a table "t",
// none checkpointed) by running the preserve helper in wal-kill mode.
func chCrashWAL(t *testing.T) (dir, path string) {
	t.Helper()
	dir = pvDir(t)
	path = filepath.Join(dir, "c.db")
	err := pvHelper(t, "wal-kill", path).Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("the helper should have been killed: %v", err)
	}
	ws, ok := ee.Sys().(syscall.WaitStatus)
	if !ok || !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
		t.Fatalf("expected SIGKILL, got %v", err)
	}
	if fi, err := os.Stat(path + "-wal"); err != nil || fi.Size() == 0 {
		t.Fatalf("the crash must leave a non-empty -wal (%v)", err)
	}
	return dir, path
}

// ---- B1 / B2 / B8: a non-empty -wal with an -shm ------------------------------------

func TestCharacterize_InspectDatabase_NonEmptyWAL_PermittedChangesOnly(t *testing.T) {
	t.Parallel()
	dir, path := crashAfterOpen(t)
	if fi, err := os.Stat(path + "-wal"); err != nil || fi.Size() == 0 {
		t.Fatalf("precondition: a non-empty -wal (%v)", err)
	}
	if _, err := os.Lstat(path + "-shm"); err != nil {
		t.Fatalf("precondition: an -shm (%v)", err)
	}
	before := chSnap(t, dir)
	res, _ := mustInspect(t, fast(), path)
	after := chSnap(t, dir)
	changed, added, removed := chDiff(before, after)
	t.Logf("OBSERVED view=%v changed=%v added=%v removed=%v", res.View, changed, added, removed)
	if len(added) != 0 || len(removed) != 0 {
		t.Fatalf("inspection added %v / removed %v", added, removed)
	}
	for _, n := range changed {
		if n != "c.db-shm" {
			t.Errorf("%s changed (content or stat signature): before %+v after %+v", n, before[n], after[n])
		}
	}
}

// ---- B8: the immutable view creates nothing and leaves every signature alone --------

func TestCharacterize_InspectDatabase_ImmutableView_CreatesNothing_SignatureStable(t *testing.T) {
	t.Parallel()
	dir, path := cleanInspectDB(t, true)
	before := chSnap(t, dir)
	var f facts
	res, err := InspectDatabase(path, factsProbe(&f))
	if err != nil {
		t.Fatal(err)
	}
	if res.View != ViewImmutable {
		t.Fatalf("view = %v, want immutable", res.View)
	}
	changed, added, removed := chDiff(before, chSnap(t, dir))
	t.Logf("OBSERVED changed=%v added=%v removed=%v", changed, added, removed)
	if len(changed)+len(added)+len(removed) != 0 {
		t.Fatalf("the immutable view changed %v, added %v, removed %v", changed, added, removed)
	}
}

// ---- B5: -shm only and -wal only: the exact post-state ------------------------------

func TestCharacterize_InspectDatabase_SHMOnly_And_WALOnly_ExactPostState(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		build func(t *testing.T) (dir, path string)
	}{
		{"shm-only", func(t *testing.T) (string, string) {
			dir, path := cleanInspectDB(t, true)
			writeFile0600(t, path+"-shm", make([]byte, 32768))
			return dir, path
		}},
		{"wal-only", func(t *testing.T) (string, string) {
			dir, path := crashAfterOpen(t)
			if err := os.Remove(path + "-shm"); err != nil {
				t.Fatal(err)
			}
			return dir, path
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir, path := c.build(t)
			before := chSnap(t, dir)
			res, _ := mustInspect(t, fast(), path)
			after := chSnap(t, dir)
			changed, added, removed := chDiff(before, after)
			sizes := map[string]int64{}
			for _, n := range added {
				sizes[n] = -1
				if fi, err := os.Lstat(filepath.Join(dir, n)); err == nil {
					sizes[n] = fi.Size()
				}
			}
			t.Logf("OBSERVED view=%v changed=%v added=%v (sizes %v) removed=%v", res.View, changed, added, sizes, removed)
			if len(removed) != 0 {
				t.Fatalf("removed %v", removed)
			}
			if b, a := before["c.db"], after["c.db"]; a != b {
				t.Fatalf("the main file changed: before %+v after %+v", b, a)
			}
			if b, ok := before["c.db-wal"]; ok && b.sig.size > 0 && after["c.db-wal"] != b {
				t.Fatalf("the existing non-empty -wal changed")
			}
			for _, n := range added {
				if n != "c.db-shm" && sizes[n] != 0 {
					t.Errorf("an added file is neither the -shm nor empty: %s (%d bytes)", n, sizes[n])
				}
			}
		})
	}
}

// ---- B6: Open on a missing or zero-byte main file with orphan companions --------------

func TestCharacterize_Open_ZeroByteMain_OrphanWALAndJournal(t *testing.T) {
	t.Parallel()
	for _, mainState := range []string{"missing-main", "zero-byte-main"} {
		t.Run(mainState, func(t *testing.T) {
			t.Parallel()
			_, src := chCrashWAL(t)
			orphanWAL, err := os.ReadFile(src + "-wal")
			if err != nil {
				t.Fatal(err)
			}
			orphanJournal := pvBytes(3000) // not a valid journal: SQLite's size check should come first
			dir := securefstest.TempDir(t)
			path := filepath.Join(dir, "c.db")
			if mainState == "zero-byte-main" {
				writeFile0600(t, path, nil)
			}
			writeFile0600(t, path+"-wal", orphanWAL)
			writeFile0600(t, path+"-journal", orphanJournal)
			walSum, jSum := fileHash(t, path+"-wal"), fileHash(t, path+"-journal")

			s, err := Open(ctx, path, Options{})
			if err != nil {
				t.Logf("OBSERVED Open refused: %v", err)
				if fileHash(t, path+"-wal") != walSum || fileHash(t, path+"-journal") != jSum {
					t.Errorf("Open refused but still changed an orphan companion")
				}
				return
			}
			var rowsT int
			errT := s.db.QueryRow(`SELECT count(*) FROM t`).Scan(&rowsT)
			var tables int
			_ = s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table'`).Scan(&tables)
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			walSurvived, journalSurvived := false, false
			walExists, journalExists := false, false
			if _, err := os.Lstat(path + "-wal"); err == nil {
				walExists = true
				walSurvived = fileHash(t, path+"-wal") == walSum
			}
			if _, err := os.Lstat(path + "-journal"); err == nil {
				journalExists = true
				journalSurvived = fileHash(t, path+"-journal") == jSum
			}
			t.Logf("OBSERVED after Open+Close: table t visible=%v (rows %d, err %v), tables=%d, -wal exists=%v identical-to-orphan=%v, -journal exists=%v identical-to-orphan=%v",
				errT == nil, rowsT, errT, tables, walExists, walSurvived, journalExists, journalSurvived)
			// Safety-relevant: the orphan WAL's data must not be resurrected.
			if errT == nil {
				t.Errorf("PREDICTION CONTRADICTED: the orphan WAL's table t is visible after Open (%d rows): orphan frames were applied to the new database", rowsT)
			}
			// Prediction (from SQLite's pager): both orphans are discarded, not kept.
			if walSurvived {
				t.Errorf("PREDICTION CONTRADICTED: the orphan -wal survived byte-identical")
			}
			if journalSurvived {
				t.Errorf("PREDICTION CONTRADICTED: the orphan -journal survived byte-identical")
			}
		})
	}
}

// ---- B9: pin the driver ---------------------------------------------------------------

func TestCharacterize_Driver_Version(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var v string
	if err := db.QueryRow(`select sqlite_version()`).Scan(&v); err != nil || v == "" {
		t.Fatalf("sqlite_version(): %q, %v", v, err)
	}
	t.Logf("SQLITE_VERSION=%s", v)
	if bi, ok := debug.ReadBuildInfo(); ok {
		found := false
		for _, d := range bi.Deps {
			if strings.HasPrefix(d.Path, "modernc.org/") {
				found = true
				t.Logf("MODULE %s %s", d.Path, d.Version)
			}
		}
		if !found {
			t.Log("build info lists no modernc.org modules")
		}
	} else {
		t.Log("no build info available in this test binary")
	}
}
