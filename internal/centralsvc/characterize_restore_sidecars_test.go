//go:build unix

package centralsvc

// Phase B-prime characterization tests for Restore's pre-preservation assessment
// (see internal/store/sqlite/characterize_inspect_test.go for the same approach).
// They observe what assessActive / inspectActive do to the files of an active
// database that has real sidecars, change no production code, and assert strictly
// only the invariants Interpretation A relies on:
//
//   - the main file and a non-empty -wal are byte-, mtime- and inode-identical;
//   - a rollback journal is never consumed, changed or removed;
//   - nothing is removed, and the only file that may change is the -shm (an
//     added file must be empty);
//   - an Empty main file with orphan companions is not touched at all;
//   - inspect -> preserve -> reinspect -> Unchanged() holds for every combination.

import (
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/docker-secret-operator/dso/internal/securefs/securefstest"
	"github.com/docker-secret-operator/dso/internal/store/sqlite"
)

// chLiveWAL leaves a database with a real, non-empty -wal and an -shm: a live
// connection commits a tiny change with auto-checkpoint off, and the three files
// are copied while it is still open. The connection is then closed, which only
// affects the source.
func chLiveWAL(t *testing.T, src string) string {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+src+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=wal_autocheckpoint(0)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	// A header-only change: it commits a WAL frame without touching any table, so
	// the database's classification and structure are unaffected.
	if _, err := db.Exec(`PRAGMA user_version = 7`); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(src + "-wal"); err != nil || fi.Size() == 0 {
		t.Fatalf("fixture: expected a non-empty -wal (%v)", err)
	}
	if _, err := os.Stat(src + "-shm"); err != nil {
		t.Fatalf("fixture: expected an -shm (%v)", err)
	}
	dst := filepath.Join(securefstest.TempDir(t), "c.db")
	for _, sfx := range []string{"", "-wal", "-shm"} {
		b, err := os.ReadFile(src + sfx)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst+sfx, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}

func chDiffState(before, after map[string]rpFileState) (changed, added, removed []string) {
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

// chAssertPermitted fails unless the only differences are an -shm rewrite and
// added empty files.
func chAssertPermitted(t *testing.T, dir string, before, after map[string]rpFileState, mustNotChange ...string) {
	t.Helper()
	changed, added, removed := chDiffState(before, after)
	t.Logf("OBSERVED changed=%v added=%v removed=%v", changed, added, removed)
	if len(removed) != 0 {
		t.Fatalf("files were removed: %v", removed)
	}
	for _, n := range changed {
		if n != "c.db-shm" {
			t.Errorf("%s changed: before %+v after %+v", n, before[n], after[n])
		}
	}
	for _, n := range added {
		if after[n].size != 0 && n != "c.db-shm" {
			t.Errorf("an added file is neither the -shm nor empty: %s %+v", n, after[n])
		}
	}
	for _, n := range mustNotChange {
		if b, ok := before[n]; ok && after[n] != b {
			t.Errorf("%s must be untouched: before %+v after %+v", n, b, after[n])
		}
	}
}

// ---- B1 / B2: R1 and R2 fixtures with real non-empty WAL and SHM -------------------

func TestCharacterize_AssessActive_R1R2Fixtures_WithRealWALAndSHM(t *testing.T) {
	t.Parallel()
	fixtures := []struct {
		name  string
		build func(t *testing.T) string
	}{
		{"R1 initialized, structure mismatch", func(t *testing.T) string { return chLiveWAL(t, riMismatch(t)) }},
		{"R2 binding-corrupt", func(t *testing.T) string {
			e := icBound(t, true)
			icEdit(t, e.dbPath, icDropMetaTriggers1, icDropMetaTriggers2, `DELETE FROM store_meta WHERE key = 'binding.canary'`)
			return chLiveWAL(t, e.dbPath)
		}},
		{"R2 dso-unbound", func(t *testing.T) string { return chLiveWAL(t, icUnbound(t)) }},
		{"initialized, clean", func(t *testing.T) string { return chLiveWAL(t, riInitialized(t)) }},
	}
	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			t.Parallel()
			path := f.build(t)
			dir := filepath.Dir(path)
			before := riSnap(t, dir)
			a := assessActive(path)
			after := riSnap(t, dir)
			t.Logf("OBSERVED state=%v structure=%v versionKnown=%v version=%d err=%v", a.State, a.Structure, a.VersionKnown, a.RecordedVersion, a.Err)
			chAssertPermitted(t, dir, before, after, "c.db", "c.db-wal")
		})
	}
}

// ---- B7: R2 Empty with orphan companions ---------------------------------------------

func TestCharacterize_AssessActive_EmptyMainWithOrphans_TouchesNothing(t *testing.T) {
	t.Parallel()
	path := riEmptyFile(t)
	crash := icCrash(t, "open-kill")
	wal, err := os.ReadFile(crash + "-wal")
	if err != nil {
		t.Fatal(err)
	}
	_, journal := riEmptyWithHotJournal(t)
	for sfx, b := range map[string][]byte{"-wal": wal, "-shm": make([]byte, 32768), "-journal": journal} {
		if err := os.WriteFile(path+sfx, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Dir(path)
	before := riSnap(t, dir)
	a := assessActive(path)
	sc, err := sqlite.ActiveSidecars(path)
	if err != nil {
		t.Fatal(err)
	}
	latest, err := sqlite.LatestSchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	plan, perr := planRestore(planInput{Assessment: a, Sidecars: sc, LatestVersion: latest})
	after := riSnap(t, dir)
	t.Logf("OBSERVED state=%v planErr=%v plan=%+v", a.State, perr, plan)
	rpSameDir(t, before, after) // every file, -shm included, identical: no SQLite open happens
	if a.State != stateEmpty {
		t.Fatalf("state = %v, want Empty", a.State)
	}
	if perr != nil || plan.Flow != RestoreFlowReplaceWithoutOpen || !plan.Preserve || plan.OpenOriginal {
		t.Fatalf("an Empty main with orphans must be preserved and replaced without opening: plan %+v, err %v", plan, perr)
	}
}

// ---- B3 / B4: a genuine hot rollback journal, in both inspection views ------------------

func TestCharacterize_HotJournal_BothInspectionViews(t *testing.T) {
	t.Parallel()
	for _, view := range []string{"immutable view (no other sidecars)", "WAL-aware view (an -shm is present)"} {
		t.Run(view, func(t *testing.T) {
			t.Parallel()
			db := rpHotJournal(t)
			dir := filepath.Dir(db)
			if view[0] == 'W' {
				if err := os.WriteFile(db+"-shm", make([]byte, 32768), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			before := riSnap(t, dir)
			var projects int
			res, ierr := sqlite.InspectDatabase(db, func(q sqlite.Querier) error {
				return q.QueryRowContext(ctx, `SELECT count(*) FROM projects`).Scan(&projects)
			})
			a := assessActive(db)
			sc, _ := sqlite.ActiveSidecars(db)
			latest, _ := sqlite.LatestSchemaVersion()
			plan, perr := planRestore(planInput{Assessment: a, Sidecars: sc, LatestVersion: latest})
			after := riSnap(t, dir)
			t.Logf("OBSERVED inspect: view=%v projects=%d err=%v | assess: state=%v structure=%v err=%v | plan: flow=%v preserve=%v err=%v",
				res.View, projects, ierr, a.State, a.Structure, a.Err, plan.Flow, plan.Preserve, perr)
			if _, err := os.Lstat(db + "-journal"); err != nil {
				t.Fatalf("the hot journal must still exist: %v", err)
			}
			chAssertPermitted(t, dir, before, after, "c.db", "c.db-journal")
		})
	}
}

// ---- B11: inspect -> preserve -> reinspect -> Unchanged, for every sidecar combination --

func TestCharacterize_InspectPreserveReinspect_SidecarCombinations(t *testing.T) {
	t.Parallel()
	latest, err := sqlite.LatestSchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		build func(t *testing.T) string
	}{
		{"live -wal and -shm", func(t *testing.T) string { return chLiveWAL(t, riInitialized(t)) }},
		{"live -wal only", func(t *testing.T) string {
			p := chLiveWAL(t, riInitialized(t))
			if err := os.Remove(p + "-shm"); err != nil {
				t.Fatal(err)
			}
			return p
		}},
		{"-shm only", func(t *testing.T) string {
			p := riInitialized(t)
			if err := os.WriteFile(p+"-shm", make([]byte, 32768), 0o600); err != nil {
				t.Fatal(err)
			}
			return p
		}},
		{"hot journal", func(t *testing.T) string { return rpHotJournal(t) }},
		{"no sidecars", func(t *testing.T) string { return riInitialized(t) }},
		{"structure mismatch with live -wal and -shm", func(t *testing.T) string { return chLiveWAL(t, riMismatch(t)) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			path := c.build(t)
			insp, err := inspectActive(path, RestoreOptions{})
			if err != nil {
				t.Logf("OBSERVED the first inspection refused: %v", err)
				return
			}
			plan, perr := planRestore(planInput{Assessment: insp.assessment, Sidecars: insp.sidecars, LatestVersion: latest})
			pres, err := sqlite.PreserveActiveFiles(path, insp.id)
			if err != nil {
				t.Fatalf("preservation failed: %v", err)
			}
			re, err := inspectActive(path, RestoreOptions{})
			if err != nil {
				t.Fatalf("reassessment after preservation failed: %v", err)
			}
			if perr == nil {
				if err := plan.matchesReassessment(re.assessment, re.sidecars); err != nil {
					t.Errorf("the reassessment does not match the plan: %v", err)
				}
			} else {
				t.Logf("OBSERVED the planner refused: %v", perr)
			}
			if err := sameActiveFile(insp.fi, re.fi); err != nil {
				t.Errorf("file identity: %v", err)
			}
			if err := pres.Unchanged(); err != nil {
				t.Errorf("Unchanged() after the reassessment: %v", err)
			}
			t.Logf("OBSERVED state=%v planFlow=%v preserved=%d files", insp.assessment.State, plan.Flow, len(pres.Files()))
		})
	}
}
