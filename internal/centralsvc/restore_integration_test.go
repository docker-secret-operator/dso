package centralsvc

// Integration tests for Restore's Phase 3b flow: classify and plan under the
// exclusive lock, refuse, confirm, raw-preserve, reassess, confirm again, pass
// the gates, then act. They use real SQLite files, genuine crash fixtures (a
// SIGKILLed process leaves a real -wal or a real hot rollback journal), and
// helper processes that kill the restore itself at each boundary. Test seams on
// RestoreOptions let a test change a file at an exact stage, fake a preservation
// failure, or fake a later assessment; they never replace the code under test.

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/crypto"
	"github.com/docker-secret-operator/dso/internal/securefs/securefstest"
	"github.com/docker-secret-operator/dso/internal/store/sqlite"
)

// ---- helper process: kill the restore at a stage ----------------------------------------------------

// TestHelperProcessRestoreIntegration is not a real test: it runs only when
// DSO_TEST_RI_KILL is set. It runs Restore (answering every confirmation yes) and
// SIGKILLs itself at the named stage.
func TestHelperProcessRestoreIntegration(t *testing.T) {
	kill := os.Getenv("DSO_TEST_RI_KILL")
	if kill == "" {
		t.Skip("helper process")
	}
	die := func() {
		_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
		select {}
	}
	kp, err := crypto.NewFileKeyProvider(os.Getenv("DSO_TEST_RI_KEYFILE"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	opts := RestoreOptions{
		CandidatePath: os.Getenv("DSO_TEST_RI_CAND"), DBPath: os.Getenv("DSO_TEST_RI_DB"), KeyProvider: kp,
		LockTimeout:              3 * time.Second,
		Confirm:                  func(sqlite.CandidateInfo) bool { return true },
		ConfirmAfterPreservation: func(RestorePlan, PreservationInfo) bool { return true },
		testHook: func(stage string) {
			if stage == kill {
				die()
			}
		},
	}
	if kill == "during-replace" {
		opts.testBeforeRename = die
	}
	_, err = Restore(ctx, opts)
	fmt.Fprintln(os.Stderr, "the child was supposed to be killed at", kill, "but finished:", err)
	os.Exit(3)
}

// ---- fixtures ---------------------------------------------------------------------------------------------------

// riCandidate returns a valid backup bound to its own root key.
func riCandidate(t *testing.T) (cand string, kp crypto.KeyProvider, keyFile string) {
	t.Helper()
	e := setup(t)
	e.project(t, "candproj", "prod")
	cand = backupActive(t, e, "cand.db")
	kp, err := crypto.NewFileKeyProvider(e.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	return cand, kp, e.keyPath
}

func riInitialized(t *testing.T) string { return icBound(t, true).dbPath }

func riMismatch(t *testing.T) string {
	p := riInitialized(t)
	icEdit(t, p, `ALTER TABLE secrets ADD COLUMN extra_col TEXT`)
	return p
}

// riOlderSchema is a genuine schema-2 database: exactly the tables of schema 2.
func riOlderSchema(t *testing.T) string {
	p := riInitialized(t)
	icEdit(t, p, `DROP TRIGGER audit_events_no_update`, `DROP TRIGGER audit_events_no_delete`,
		`DROP TABLE audit_events`, `DELETE FROM schema_migrations WHERE version = 3`)
	return p
}

// riColdJournal is an ordinary DSO database with a cold, non-empty -journal
// (journal_mode=PERSIST leaves it behind with a zeroed header).
func riColdJournal(t *testing.T) string {
	p := riInitialized(t)
	db, err := sql.Open("sqlite", "file:"+p)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	for _, stmt := range []string{`PRAGMA journal_mode=PERSIST`, `UPDATE projects SET name = name`} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(stmt, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(p + "-journal"); err != nil || fi.Size() == 0 {
		t.Fatalf("fixture: expected a non-empty -journal (%v)", err)
	}
	return p
}

// riEmptyWithHotJournal is a zero-byte main file with a genuine hot journal
// beside it: classified Empty (before sidecars are looked at), which is the one
// path on which a stale journal reaches the replacement.
func riEmptyWithHotJournal(t *testing.T) (db string, journal []byte) {
	t.Helper()
	hot := rpHotJournal(t)
	journal, err := os.ReadFile(hot + "-journal")
	if err != nil {
		t.Fatal(err)
	}
	dir := securefstest.TempDir(t)
	db = filepath.Join(dir, "c.db")
	if err := os.WriteFile(db, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(db+"-journal", journal, 0o600); err != nil {
		t.Fatal(err)
	}
	return db, journal
}

func riEmptyFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(securefstest.TempDir(t), "c.db")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func riMissing(t *testing.T) string { return filepath.Join(securefstest.TempDir(t), "c.db") }

// ---- running a restore ---------------------------------------------------------------------------------------------

type riRun struct {
	res         RestoreResult
	err         error
	stages      []string
	confirms    int
	secondCalls int
	plan        *RestorePlan
}

func (r *riRun) reached(stage string) bool {
	for _, s := range r.stages {
		if s == stage {
			return true
		}
	}
	return false
}

func (r *riRun) indexOf(stage string) int {
	for i, s := range r.stages {
		if s == stage {
			return i
		}
	}
	return -1
}

func riDo(t *testing.T, db, cand string, kp crypto.KeyProvider, mod func(o *RestoreOptions, r *riRun)) *riRun {
	t.Helper()
	r := &riRun{}
	o := RestoreOptions{
		CandidatePath: cand, DBPath: db, KeyProvider: kp, LockTimeout: 3 * time.Second,
		Confirm:  func(sqlite.CandidateInfo) bool { r.confirms++; return true },
		OnPlan:   func(p RestorePlan) { r.plan = &p },
		testHook: func(s string) { r.stages = append(r.stages, s) },
	}
	if mod != nil {
		mod(&o, r)
	}
	r.res, r.err = Restore(ctx, o)
	return r
}

// riSnap snapshots a directory, ignoring the central lock sidecar, which Restore
// creates by design as soon as it takes the lock.
func riSnap(t *testing.T, dir string) map[string]rpFileState {
	t.Helper()
	m := rpSnapshotDir(t, dir)
	for name := range m {
		if strings.HasSuffix(name, ".central.lock") {
			delete(m, name)
		}
	}
	return m
}

func riPreservations(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		if strings.Contains(e.Name(), ".pre-restore-raw-") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

func riSnapshots(t *testing.T, dir string) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(dir, "*.pre-restore-*.bak"))
	return m
}

// riAssertRestored checks that db is the candidate: it reopens, lists the
// candidate's project, and passes SQLite's own integrity check.
func riAssertRestored(t *testing.T, db string, kp crypto.KeyProvider) {
	t.Helper()
	raw, err := sql.Open("sqlite", "file:"+db)
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	var integrity string
	if err := raw.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity_check = %q (%v)", integrity, err)
	}
	var slugs []string
	rows, err := raw.Query(`SELECT slug FROM projects ORDER BY slug`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		slugs = append(slugs, s)
	}
	rows.Close()
	_ = raw.Close()
	if strings.Join(slugs, ",") != "candproj" {
		t.Fatalf("the restored database holds projects %v, want exactly the candidate's [candproj]", slugs)
	}
	st, err := sqlite.Open(ctx, db, sqlite.Options{})
	if err != nil {
		t.Fatalf("the restored database does not open: %v", err)
	}
	defer st.Close()
	if _, err := New(st, kp, nil).ListProjects(ctx); err != nil {
		t.Fatalf("the restored database is not usable with the candidate's key: %v", err)
	}
}

// ---- A. normal flows -------------------------------------------------------------------------------------------------

func TestRestoreIntegration_MissingActiveDatabase(t *testing.T) {
	t.Parallel()
	cand, kp, _ := riCandidate(t)
	t.Run("no companions", func(t *testing.T) {
		t.Parallel()
		db := riMissing(t)
		r := riDo(t, db, cand, kp, nil)
		if r.err != nil {
			t.Fatal(r.err)
		}
		if r.plan == nil || r.plan.Flow != RestoreFlowNoActive || r.plan.Preserve {
			t.Fatalf("plan: %+v", r.plan)
		}
		if strings.Join(r.stages, ",") != "planned,confirmed,gates-passed,pre-journal-gate,replaced" {
			t.Fatalf("stages: %v", r.stages)
		}
		if r.res.RawPreservation != "" || r.res.SafetySnapshot != "" || len(riPreservations(t, filepath.Dir(db))) != 0 {
			t.Fatalf("nothing existed to preserve or snapshot: %+v", r.res)
		}
		riAssertRestored(t, db, kp)
	})
	t.Run("empty companions are removed, not refused", func(t *testing.T) {
		t.Parallel()
		db := riMissing(t)
		for _, sfx := range []string{"-wal", "-journal", "-shm"} {
			if err := os.WriteFile(db+sfx, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		r := riDo(t, db, cand, kp, nil)
		if r.err != nil {
			t.Fatal(r.err)
		}
		for _, sfx := range []string{"-wal", "-journal", "-shm"} {
			if _, err := os.Lstat(db + sfx); !os.IsNotExist(err) {
				t.Fatalf("the empty %s should have been removed (%v)", sfx, err)
			}
		}
		riAssertRestored(t, db, kp)
	})
}

func TestRestoreIntegration_InitializedLatestClean_UnchangedSnapshotFlow(t *testing.T) {
	t.Parallel()
	cand, kp, _ := riCandidate(t)
	db := riInitialized(t)
	r := riDo(t, db, cand, kp, nil)
	if r.err != nil {
		t.Fatal(r.err)
	}
	if r.plan.Flow != RestoreFlowSnapshot || r.plan.Preserve || r.plan.SecondConfirmation || !r.plan.OpenOriginal {
		t.Fatalf("plan: %+v", r.plan)
	}
	if r.res.RawPreservation != "" || r.res.SafetySnapshot == "" || len(riPreservations(t, filepath.Dir(db))) != 0 {
		t.Fatalf("the plain flow takes the safety snapshot and no raw preservation: %+v", r.res)
	}
	if r.secondCalls != 0 || r.reached(stagePreserved) {
		t.Fatalf("stages %v", r.stages)
	}
	riAssertRestored(t, db, kp)
}

func TestRestoreIntegration_PreserveFirstThenSnapshotFlow(t *testing.T) {
	t.Parallel()
	cand, kp, _ := riCandidate(t)
	cases := map[string]struct {
		build   func(t *testing.T) string
		reason  string
		walCopy bool
		jrnCopy bool
	}{
		"older schema (R3)":           {riOlderSchema, "older than", false, false},
		"non-empty -wal (R4)":         {func(t *testing.T) string { return icCrash(t, "bind-kill") }, "-wal", true, false},
		"cold non-empty journal (R8)": {riColdJournal, "-journal", false, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db := tc.build(t)
			// What the original looked like, before anything of Restore touched it.
			mainBefore := icFileHash(t, db)
			var walBefore, jrnBefore string
			if tc.walCopy {
				walBefore = icFileHash(t, db+"-wal")
			}
			if tc.jrnCopy {
				jrnBefore = icFileHash(t, db+"-journal")
			}
			r := riDo(t, db, cand, kp, nil)
			if r.err != nil {
				t.Fatal(r.err)
			}
			if r.plan.Flow != RestoreFlowSnapshot || !r.plan.Preserve || r.plan.SecondConfirmation || !r.plan.OpenOriginal {
				t.Fatalf("plan: %+v", r.plan)
			}
			if !strings.Contains(strings.Join(r.plan.PreserveReasons, "\n"), tc.reason) {
				t.Fatalf("reasons: %v", r.plan.PreserveReasons)
			}
			// Preservation strictly precedes the first Open, and the preserved copy is the
			// ORIGINAL, byte for byte -- not what Open or the checkpoint left.
			if !(r.indexOf(stagePreserved) >= 0 && r.indexOf(stagePreserved) < r.indexOf(stageOpenOriginal)) {
				t.Fatalf("preservation must precede Open: %v", r.stages)
			}
			if r.res.RawPreservation == "" || r.res.SafetySnapshot == "" {
				t.Fatalf("both recovery copies are expected: %+v", r.res)
			}
			if icFileHash(t, filepath.Join(r.res.RawPreservation, "main")) != mainBefore {
				t.Fatal("the preserved main file is not the original")
			}
			if tc.walCopy && icFileHash(t, filepath.Join(r.res.RawPreservation, "wal")) != walBefore {
				t.Fatal("the preserved -wal is not the original")
			}
			if tc.jrnCopy && icFileHash(t, filepath.Join(r.res.RawPreservation, "journal")) != jrnBefore {
				t.Fatal("the preserved -journal is not the original")
			}
			if _, err := os.Lstat(filepath.Join(r.res.RawPreservation, "manifest.json")); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(db + "-journal"); !os.IsNotExist(err) {
				t.Fatalf("no old journal may remain beside the restored database (%v)", err)
			}
			riAssertRestored(t, db, kp)
			if l := riPreservations(t, filepath.Dir(db)); len(l) != 1 {
				t.Fatalf("the completed preservation must remain: %v", l)
			}
		})
	}
}

// ---- B. refusals ---------------------------------------------------------------------------------------------------------

func TestRestoreIntegration_Refusals_LeaveEverythingUntouched(t *testing.T) {
	t.Parallel()
	cand, kp, _ := riCandidate(t)
	journalOf := func(t *testing.T) []byte {
		b, err := os.ReadFile(rpHotJournal(t) + "-journal")
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	orphan := func(sfx string) func(t *testing.T) string {
		return func(t *testing.T) string {
			db := riMissing(t)
			if err := os.WriteFile(db+sfx, []byte("orphaned data"), 0o600); err != nil {
				t.Fatal(err)
			}
			return db
		}
	}
	cases := []struct {
		name  string
		build func(t *testing.T) string
		kind  error
		want  string // substring of the message
	}{
		{"foreign database", func(t *testing.T) string { return icForeign(t, `CREATE TABLE customer(id)`) }, ErrRestoreRefusedForeign, "not a DSO Central database"},
		{"schema too new", func(t *testing.T) string {
			p := riInitialized(t)
			icEdit(t, p, `INSERT INTO schema_migrations(version, applied_at) VALUES (99, 0)`)
			return p
		}, ErrRestoreRefusedTooNew, "newer version"},
		{"not a SQLite file", func(t *testing.T) string {
			p := riMissing(t)
			if err := os.WriteFile(p, []byte(strings.Repeat("not a database\n", 40)), 0o600); err != nil {
				t.Fatal(err)
			}
			return p
		}, ErrRestoreRefusedNotSQLite, "not a SQLite database"},
		{"genuine hot rollback journal", func(t *testing.T) string { return rpHotJournal(t) }, ErrRestoreRefusedInconclusive, "hot journal"},
		{"DSO-unbound beside a hot journal", func(t *testing.T) string {
			p := icUnbound(t)
			if err := os.WriteFile(p+"-journal", journalOf(t), 0o600); err != nil {
				t.Fatal(err)
			}
			return p
		}, ErrRestoreRefusedInconclusive, "hot journal"},
		{"BindingCorrupt beside a hot journal", func(t *testing.T) string {
			e := icBound(t, true)
			icEdit(t, e.dbPath, icDropMetaTriggers1, icDropMetaTriggers2, `DELETE FROM store_meta WHERE key = 'binding.canary'`)
			if err := os.WriteFile(e.dbPath+"-journal", journalOf(t), 0o600); err != nil {
				t.Fatal(err)
			}
			return e.dbPath
		}, ErrRestoreRefusedInconclusive, "hot journal"},
		{"missing main, orphan -wal", orphan("-wal"), ErrRestoreRefusedOrphans, "-wal"},
		{"missing main, orphan -journal", orphan("-journal"), ErrRestoreRefusedOrphans, "-journal"},
		{"missing main, orphan -shm", orphan("-shm"), ErrRestoreRefusedOrphans, "-shm"},
		{"active path is a symlink", func(t *testing.T) string {
			dir := securefstest.TempDir(t)
			real := filepath.Join(dir, "real.db")
			if err := os.WriteFile(real, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			p := filepath.Join(dir, "c.db")
			if err := os.Symlink(real, p); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			return p
		}, ErrRestoreRefusedInconclusive, "symbolic link"},
		{"active path is a directory", func(t *testing.T) string {
			p := riMissing(t)
			if err := os.Mkdir(p, 0o700); err != nil {
				t.Fatal(err)
			}
			return p
		}, ErrRestoreRefusedInconclusive, "directory"},
		{"companion is a symlink", func(t *testing.T) string {
			p := riInitialized(t)
			if err := os.Symlink(p, p+"-wal"); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			return p
		}, ErrRestoreRefusedInconclusive, "symbolic link"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := tc.build(t)
			dir := filepath.Dir(db)
			before := riSnap(t, dir)
			r := riDo(t, db, cand, kp, nil)
			if !errors.Is(r.err, ErrRestoreRefused) || !errors.Is(r.err, tc.kind) {
				t.Fatalf("want a %v refusal, got %v", tc.kind, r.err)
			}
			if !strings.Contains(r.err.Error(), tc.want) {
				t.Fatalf("the message lacks %q:\n%v", tc.want, r.err)
			}
			if r.confirms != 0 || len(r.stages) != 0 {
				t.Fatalf("a refusal must come before any prompt or stage: confirms=%d stages=%v", r.confirms, r.stages)
			}
			if r.res.RawPreservation != "" || r.res.SafetySnapshot != "" {
				t.Fatalf("nothing may be preserved or snapshotted: %+v", r.res)
			}
			rpSameDir(t, before, riSnap(t, dir))
			if l := riPreservations(t, dir); len(l) != 0 {
				t.Fatalf("a refusal must not create a preservation: %v", l)
			}
		})
	}
}

func TestRestoreIntegration_Refusal_HeldDatabaseIsInconclusive(t *testing.T) {
	t.Parallel()
	cand, kp, _ := riCandidate(t)
	e := icBound(t, false)
	w := icLockExclusive(t, e.dbPath)
	mainBefore := icFileHash(t, e.dbPath)
	r := riDo(t, e.dbPath, cand, kp, nil)
	mainAfter := icFileHash(t, e.dbPath)
	_ = w.Close()
	if !errors.Is(r.err, ErrRestoreRefusedInconclusive) || r.confirms != 0 || len(r.stages) != 0 {
		t.Fatalf("a database held by another process must be refused as inconclusive: %v (stages %v)", r.err, r.stages)
	}
	if mainAfter != mainBefore {
		t.Fatal("the main file changed")
	}
	if l := riPreservations(t, filepath.Dir(e.dbPath)); len(l) != 0 {
		t.Fatalf("no preservation: %v", l)
	}
}

func TestRestoreIntegration_Refusal_InspectionErrors_FailClosed(t *testing.T) {
	t.Parallel()
	cand, kp, _ := riCandidate(t)
	t.Run("a permission error from the inspection seam", func(t *testing.T) {
		t.Parallel()
		db := riInitialized(t)
		before := riSnap(t, filepath.Dir(db))
		r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
			o.testSidecars = func(string) (sqlite.ActiveSidecarsInfo, error) {
				return sqlite.ActiveSidecarsInfo{}, &os.PathError{Op: "lstat", Path: db, Err: syscall.EACCES}
			}
		})
		if !errors.Is(r.err, ErrRestoreRefusedInconclusive) || !errors.Is(r.err, syscall.EACCES) || errors.Is(r.err, ErrRestoreRefusedOrphans) {
			t.Fatalf("EACCES must be a refusal, never 'missing': %v", r.err)
		}
		if r.confirms != 0 || len(r.stages) != 0 {
			t.Fatalf("stages %v", r.stages)
		}
		rpSameDir(t, before, riSnap(t, filepath.Dir(db)))
	})
	t.Run("ENOTDIR is not 'missing'", func(t *testing.T) {
		t.Parallel()
		dir := securefstest.TempDir(t)
		file := filepath.Join(dir, "plain")
		if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		r := riDo(t, filepath.Join(file, "c.db"), cand, kp, nil)
		if r.err == nil || r.confirms != 0 || r.reached(stagePlanned) {
			t.Fatalf("got %v (stages %v)", r.err, r.stages)
		}
	})
	t.Run("a real directory that cannot be searched", func(t *testing.T) {
		t.Parallel()
		if os.Geteuid() == 0 {
			t.Skip("permissions do not bind root")
		}
		sub := filepath.Join(securefstest.TempDir(t), "sub")
		if err := os.Mkdir(sub, 0o700); err != nil {
			t.Fatal(err)
		}
		db := filepath.Join(sub, "c.db")
		if err := os.WriteFile(db, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(sub, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(sub, 0o700) })
		r := riDo(t, db, cand, kp, nil)
		if r.err == nil || r.confirms != 0 || r.reached(stagePlanned) || r.res.RawPreservation != "" {
			t.Fatalf("got %v (stages %v)", r.err, r.stages)
		}
	})
	t.Run("inconsistent seam inputs fail closed", func(t *testing.T) {
		t.Parallel()
		db := riInitialized(t)
		r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
			o.testAssess = func(string) activeAssessment { return activeAssessment{State: stateMissing} }
		})
		if !errors.Is(r.err, ErrRestoreRefusedInvalid) || r.confirms != 0 {
			t.Fatalf("got %v", r.err)
		}
	})
}

// A refusal is not a prompt: the confirmation callback (which is what --force
// answers) is never asked, so it cannot override one.
func TestRestoreIntegration_ConfirmationCannotOverrideARefusal(t *testing.T) {
	t.Parallel()
	cand, kp, _ := riCandidate(t)
	db := icForeign(t, `CREATE TABLE customer(id)`)
	asked := false
	r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
		o.Confirm = func(sqlite.CandidateInfo) bool { asked = true; return true }
		o.ConfirmAfterPreservation = func(RestorePlan, PreservationInfo) bool { asked = true; return true }
	})
	if !errors.Is(r.err, ErrRestoreRefusedForeign) || asked {
		t.Fatalf("err %v, a callback was asked: %v", r.err, asked)
	}
}

// ---- C. preservation behavior ---------------------------------------------------------------------------------------------

func TestRestoreIntegration_PreservationFailures_StopBeforeAnythingIsOpenedOrReplaced(t *testing.T) {
	t.Parallel()
	cand, kp, _ := riCandidate(t)
	cases := []struct {
		name string
		err  error
		is   error
		want string
		deny string
	}{
		{"ENOSPC", fmt.Errorf("%w: %v", sqlite.ErrPreserveNoSpace, syscall.ENOSPC), sqlite.ErrPreserveNoSpace, "not enough storage", "free space was checked"},
		{"EDQUOT", fmt.Errorf("%w: %v", sqlite.ErrPreserveNoSpace, syscall.EDQUOT), sqlite.ErrPreserveNoSpace, "not enough storage", "free space was checked"},
		{"unstable", fmt.Errorf("%w after 3 attempts", sqlite.ErrPreserveUnstable), sqlite.ErrPreserveUnstable, "kept changing", ""},
		{"identity", fmt.Errorf("%w: x", sqlite.ErrPreserveIdentityChanged), sqlite.ErrPreserveIdentityChanged, "not the one that was assessed", ""},
		{"verification", fmt.Errorf("%w: main", sqlite.ErrPreserveVerification), sqlite.ErrPreserveVerification, "read back identically", ""},
		{"unsupported platform", fmt.Errorf("%w (EINVAL)", sqlite.ErrPreserveNoReplaceUnsupported), sqlite.ErrPreserveNoReplaceUnsupported, "platform or filesystem", ""},
		{"generic", errors.New("something else"), nil, "preserving the active database failed", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := riEmptyFile(t)
			dir := filepath.Dir(db)
			before := riSnap(t, dir)
			r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
				o.testPreserve = func(string, sqlite.FileIdentity) (*sqlite.Preservation, error) { return nil, tc.err }
			})
			if r.err == nil || (tc.is != nil && !errors.Is(r.err, tc.is)) || !strings.Contains(r.err.Error(), tc.want) {
				t.Fatalf("got %v", r.err)
			}
			if tc.deny != "" && strings.Contains(r.err.Error(), tc.deny) {
				t.Fatalf("the message must not claim that space was checked in advance: %v", r.err)
			}
			if tc.name == "ENOSPC" && !strings.Contains(r.err.Error(), "not checked in advance") {
				t.Fatalf("the message must say space is not checked in advance: %v", r.err)
			}
			if r.reached(stageOpenOriginal) || r.reached(stageReplaced) || r.reached(stageGatesPassed) || r.res.RawPreservation != "" {
				t.Fatalf("stages %v, preservation %q", r.stages, r.res.RawPreservation)
			}
			rpSameDir(t, before, riSnap(t, dir))
		})
	}
	t.Run("a real identity change is detected by the real preservation", func(t *testing.T) {
		t.Parallel()
		db := riEmptyFile(t)
		r := riDo(t, db, cand, kp, func(o *RestoreOptions, r *riRun) {
			real := o.testHook
			o.testHook = func(s string) {
				real(s)
				if s == stageConfirmed { // between assessment and preservation
					other := db + ".other"
					if err := os.WriteFile(other, nil, 0o600); err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(other, db); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
		if !errors.Is(r.err, sqlite.ErrPreserveIdentityChanged) || r.reached(stageReplaced) || r.reached(stageOpenOriginal) {
			t.Fatalf("got %v (stages %v)", r.err, r.stages)
		}
	})
	t.Run("a durability error stops restore even though a preservation was published", func(t *testing.T) {
		t.Parallel()
		db := riEmptyFile(t)
		dir := filepath.Dir(db)
		r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
			o.testPreserve = func(p string, id sqlite.FileIdentity) (*sqlite.Preservation, error) {
				pres, err := sqlite.PreserveActiveFiles(p, id)
				if err != nil {
					t.Fatal(err)
				}
				return pres, errors.New("fsync of the parent directory failed")
			}
		})
		if r.err == nil || r.res.RawPreservation == "" || r.reached(stageReplaced) || r.reached(stageOpenOriginal) || r.reached(stageGatesPassed) {
			t.Fatalf("got %v, preservation %q, stages %v", r.err, r.res.RawPreservation, r.stages)
		}
		if !strings.Contains(r.err.Error(), r.res.RawPreservation) || !strings.Contains(r.err.Error(), "was kept") {
			t.Fatalf("the message should name the kept preservation: %v", r.err)
		}
		if l := riPreservations(t, dir); len(l) != 1 || l[0] != r.res.RawPreservation {
			t.Fatalf("the published preservation must remain: %v", l)
		}
		if fi, err := os.Stat(db); err != nil || fi.Size() != 0 {
			t.Fatalf("the original was changed (%v)", err)
		}
	})
}

func TestRestoreIntegration_DeclinedConfirmationCreatesNoPreservation(t *testing.T) {
	t.Parallel()
	cand, kp, _ := riCandidate(t)
	db := riEmptyFile(t)
	dir := filepath.Dir(db)
	before := riSnap(t, dir)
	r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
		o.Confirm = func(sqlite.CandidateInfo) bool { return false }
	})
	if !errors.Is(r.err, ErrRestoreNotConfirmed) || r.reached(stageConfirmed) || r.res.RawPreservation != "" {
		t.Fatalf("got %v (stages %v)", r.err, r.stages)
	}
	rpSameDir(t, before, riSnap(t, dir))
	if l := riPreservations(t, dir); len(l) != 0 {
		t.Fatalf("a declined restore must not write a preservation: %v", l)
	}
}

// ---- D. reassessment and the independent gates -------------------------------------------------------------------------------

func TestRestoreIntegration_Reassessment_AnyDifferenceAbortsAndKeepsThePreservation(t *testing.T) {
	t.Parallel()
	cand, kp, _ := riCandidate(t)
	type mut struct {
		name   string
		build  func(t *testing.T) string
		assess func(a *activeAssessment)
		sc     func(s *sqlite.ActiveSidecarsInfo)
	}
	nonEmpty := func(n int64) sqlite.SidecarFile { return sqlite.SidecarFile{Exists: true, Size: n} }
	muts := []mut{
		{"state changes", riEmptyFile, func(a *activeAssessment) { a.State = stateDSOUnbound }, nil},
		{"structure changes", riMismatch, func(a *activeAssessment) { a.Structure = structureClean }, nil},
		{"structure becomes unknown", riMismatch, func(a *activeAssessment) { a.Structure = structureUnknown }, nil},
		{"recorded version changes", riMismatch, func(a *activeAssessment) { a.RecordedVersion = 2 }, nil},
		{"recorded version becomes unknown", riMismatch, func(a *activeAssessment) { a.VersionKnown, a.RecordedVersion = false, 0 }, nil},
		{"wal empty to non-empty", riMismatch, nil, func(s *sqlite.ActiveSidecarsInfo) { s.WAL = nonEmpty(10) }},
		{"journal empty to non-empty", riMismatch, nil, func(s *sqlite.ActiveSidecarsInfo) { s.Journal = nonEmpty(10) }},
		{"wal non-empty to empty", func(t *testing.T) string { return icCrash(t, "bind-kill") }, nil, func(s *sqlite.ActiveSidecarsInfo) { s.WAL = sqlite.SidecarFile{Exists: true} }},
		{"wal non-empty to absent", func(t *testing.T) string { return icCrash(t, "bind-kill") }, nil, func(s *sqlite.ActiveSidecarsInfo) { s.WAL = sqlite.SidecarFile{} }},
		{"reassessment becomes inconclusive", riMismatch, func(a *activeAssessment) { a.State, a.Err = stateInconclusive, errors.New("held") }, nil},
	}
	for _, m := range muts {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			db := m.build(t)
			dir := filepath.Dir(db)
			var reassessing bool
			r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
				real := o.testHook
				o.testHook = func(s string) {
					real(s)
					if s == stagePreserved {
						reassessing = true
					}
				}
				o.testAssess = func(p string) activeAssessment {
					a := assessActive(p)
					if reassessing && m.assess != nil {
						m.assess(&a)
					}
					return a
				}
				o.testSidecars = func(p string) (sqlite.ActiveSidecarsInfo, error) {
					s, err := sqlite.ActiveSidecars(p)
					if reassessing && m.sc != nil {
						m.sc(&s)
					}
					return s, err
				}
				o.ConfirmAfterPreservation = func(RestorePlan, PreservationInfo) bool { return true }
			})
			if !errors.Is(r.err, ErrRestoreReassessmentMismatch) {
				t.Fatalf("want a reassessment mismatch, got %v (stages %v)", r.err, r.stages)
			}
			if r.res.RawPreservation == "" {
				t.Fatal("the completed preservation must be reported")
			}
			if _, err := os.Lstat(filepath.Join(r.res.RawPreservation, "manifest.json")); err != nil {
				t.Fatalf("the completed preservation must be kept intact: %v", err)
			}
			if !strings.Contains(r.err.Error(), r.res.RawPreservation) {
				t.Fatalf("the message should name the preservation: %v", r.err)
			}
			for _, s := range []string{stageReassessed, stageSecondPrompt, stageGatesPassed, stageOpenOriginal, stageSnapshot, stageReplaced} {
				if r.reached(s) {
					t.Fatalf("stage %s must not be reached after a mismatch: %v", s, r.stages)
				}
			}
			if l := riPreservations(t, dir); len(l) != 1 {
				t.Fatalf("exactly the completed preservation must exist: %v", l)
			}
		})
	}
	t.Run("a held database makes the real reassessment inconclusive", func(t *testing.T) {
		t.Parallel()
		db := riMismatch(t)
		var w *sql.DB
		r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
			real := o.testHook
			o.testHook = func(s string) {
				real(s)
				if s == stagePreserved {
					w = icLockExclusive(t, db)
				}
			}
		})
		if w != nil {
			_ = w.Close()
		}
		if !errors.Is(r.err, ErrRestoreReassessmentMismatch) || r.reached(stageReplaced) || r.reached(stageOpenOriginal) {
			t.Fatalf("got %v (stages %v)", r.err, r.stages)
		}
	})
}

func TestRestoreIntegration_IndependentGates(t *testing.T) {
	t.Parallel()
	cand, kp, _ := riCandidate(t)
	yes := func(o *RestoreOptions, _ *riRun) {
		o.ConfirmAfterPreservation = func(RestorePlan, PreservationInfo) bool { return true }
	}
	t.Run("the file identity gate catches a replaced file when nothing was preserved", func(t *testing.T) {
		t.Parallel()
		db := riInitialized(t)
		r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
			real := o.testHook
			o.testHook = func(s string) {
				real(s)
				if s == stageConfirmed { // plain flow: no preservation, so only the identity gate can notice
					b, _ := os.ReadFile(db)
					other := db + ".other"
					if err := os.WriteFile(other, b, 0o600); err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(other, db); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
		if !errors.Is(r.err, ErrRestoreActiveFileChanged) || r.reached(stageOpenOriginal) || r.reached(stageReplaced) {
			t.Fatalf("got %v (stages %v)", r.err, r.stages)
		}
	})
	t.Run("the identity gate also catches an identical-content replacement after a preservation", func(t *testing.T) {
		t.Parallel()
		db := riMismatch(t)
		r := riDo(t, db, cand, kp, func(o *RestoreOptions, r *riRun) {
			yes(o, r)
			real := o.testHook
			o.testHook = func(s string) {
				real(s)
				if s == stageReassessed {
					b, _ := os.ReadFile(db)
					other := db + ".other"
					if err := os.WriteFile(other, b, 0o600); err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(other, db); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
		if r.err == nil || r.reached(stageReplaced) || r.reached(stageOpenOriginal) || r.res.RawPreservation == "" {
			t.Fatalf("got %v (stages %v)", r.err, r.stages)
		}
		if !errors.Is(r.err, sqlite.ErrPreserveSourceChanged) && !errors.Is(r.err, ErrRestoreActiveFileChanged) {
			t.Fatalf("want a source-changed or identity error, got %v", r.err)
		}
	})
	t.Run("the reassessment itself notices a replaced file", func(t *testing.T) {
		t.Parallel()
		db := riMismatch(t)
		r := riDo(t, db, cand, kp, func(o *RestoreOptions, r *riRun) {
			yes(o, r)
			real := o.testHook
			o.testHook = func(s string) {
				real(s)
				if s == stagePreserved {
					b, _ := os.ReadFile(db)
					other := db + ".other"
					if err := os.WriteFile(other, b, 0o600); err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(other, db); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
		if !errors.Is(r.err, ErrRestoreActiveFileChanged) || r.reached(stageReassessed) || r.res.RawPreservation == "" {
			t.Fatalf("got %v (stages %v)", r.err, r.stages)
		}
	})
	t.Run("Unchanged() catches an in-place modification that keeps the file identity", func(t *testing.T) {
		t.Parallel()
		db := riMismatch(t)
		r := riDo(t, db, cand, kp, func(o *RestoreOptions, r *riRun) {
			yes(o, r)
			real := o.testHook
			o.testHook = func(s string) {
				real(s)
				if s == stageSecondConfirmed { // after the reassessment, while the operator "thought"
					f, err := os.OpenFile(db, os.O_WRONLY|os.O_APPEND, 0)
					if err != nil {
						t.Fatal(err)
					}
					_, _ = f.Write([]byte("late write"))
					_ = f.Close()
				}
			}
		})
		if !errors.Is(r.err, sqlite.ErrPreserveSourceChanged) || r.reached(stageGatesPassed) || r.reached(stageReplaced) {
			t.Fatalf("got %v (stages %v)", r.err, r.stages)
		}
		if !strings.Contains(r.err.Error(), r.res.RawPreservation) {
			t.Fatalf("the message should name the kept preservation: %v", r.err)
		}
		if _, err := os.Lstat(filepath.Join(r.res.RawPreservation, "manifest.json")); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("a file appearing at a missing database stops the restore", func(t *testing.T) {
		t.Parallel()
		db := riMissing(t)
		r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
			real := o.testHook
			o.testHook = func(s string) {
				real(s)
				if s == stageConfirmed {
					if err := os.WriteFile(db, []byte("someone else's database"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
		if !errors.Is(r.err, ErrRestoreActiveFileChanged) || r.reached(stageReplaced) {
			t.Fatalf("got %v (stages %v)", r.err, r.stages)
		}
		if b, _ := os.ReadFile(db); string(b) != "someone else's database" {
			t.Fatal("the file that appeared was overwritten")
		}
	})
	t.Run("an orphan appearing beside a missing database stops the restore and is not deleted", func(t *testing.T) {
		t.Parallel()
		db := riMissing(t)
		r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
			real := o.testHook
			o.testHook = func(s string) {
				real(s)
				if s == stageConfirmed {
					if err := os.WriteFile(db+"-wal", []byte("frames from nowhere"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
		if !errors.Is(r.err, ErrRestoreRefusedOrphans) || r.reached(stageReplaced) {
			t.Fatalf("got %v (stages %v)", r.err, r.stages)
		}
		if b, _ := os.ReadFile(db + "-wal"); string(b) != "frames from nowhere" {
			t.Fatal("the orphan was deleted or changed")
		}
	})
}

// ---- E. confirmations --------------------------------------------------------------------------------------------------------------

func TestSecondConfirmationAccepted(t *testing.T) {
	t.Parallel()
	yes := []string{"replace\n", "replace\r\n", "replace\nanything after the first line"}
	no := []string{"y\n", "yes\n", "Y\n", "Replace\n", "REPLACE\n", " replace\n", "replace \n", "\treplace\n", "replac\n", "replacex\n",
		"\n", "", "replace", "replace\x00\n", "no\n", "replace replace\n"}
	for _, in := range yes {
		if !SecondConfirmationAccepted(strings.NewReader(in)) {
			t.Fatalf("%q must be accepted", in)
		}
	}
	for _, in := range no {
		if SecondConfirmationAccepted(strings.NewReader(in)) {
			t.Fatalf("%q must be declined", in)
		}
	}
	if SecondConfirmationAccepted(nil) {
		t.Fatal("a nil reader must decline")
	}
	if SecondConfirmationAccepted(errReader{}) {
		t.Fatal("a failing reader must decline")
	}
	if SecondConfirmationAccepted(strings.NewReader(strings.Repeat("a", 5000) + "\nreplace\n")) {
		t.Fatal("an over-long first line is not the word")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestRestoreIntegration_SecondConfirmation(t *testing.T) {
	t.Parallel()
	cand, kp, _ := riCandidate(t)
	run := func(t *testing.T, second func(RestorePlan, PreservationInfo) bool) (*riRun, string) {
		db := riMismatch(t)
		r := riDo(t, db, cand, kp, func(o *RestoreOptions, r *riRun) {
			if second != nil {
				o.ConfirmAfterPreservation = func(p RestorePlan, i PreservationInfo) bool { r.secondCalls++; return second(p, i) }
			}
		})
		return r, db
	}
	declined := func(t *testing.T, r *riRun, db string, mainBefore string) {
		t.Helper()
		if !errors.Is(r.err, ErrRestoreSecondConfirmationDeclined) {
			t.Fatalf("got %v (stages %v)", r.err, r.stages)
		}
		if r.res.RawPreservation == "" || !strings.Contains(r.err.Error(), r.res.RawPreservation) {
			t.Fatalf("the preservation must be reported and kept: %v", r.err)
		}
		if _, err := os.Lstat(filepath.Join(r.res.RawPreservation, "manifest.json")); err != nil {
			t.Fatal(err)
		}
		for _, s := range []string{stageGatesPassed, stageOpenOriginal, stageSnapshot, stageReplaced} {
			if r.reached(s) {
				t.Fatalf("stage %s must not be reached when the second confirmation is not given: %v", s, r.stages)
			}
		}
		if icFileHash(t, db) != mainBefore {
			t.Fatal("the original was changed")
		}
		if len(riSnapshots(t, filepath.Dir(db))) != 0 {
			t.Fatal("no safety snapshot may be taken when the original is not opened")
		}
	}
	t.Run("the exact word proceeds, without opening the original", func(t *testing.T) {
		t.Parallel()
		db := riMismatch(t)
		mainBefore := icFileHash(t, db)
		var atReplace string
		r := riDo(t, db, cand, kp, func(o *RestoreOptions, r *riRun) {
			o.ConfirmAfterPreservation = func(RestorePlan, PreservationInfo) bool {
				r.secondCalls++
				return SecondConfirmationAccepted(strings.NewReader("replace\n"))
			}
			real := o.testHook
			o.testHook = func(s string) {
				real(s)
				if s == stagePreJournalGate {
					atReplace = icFileHash(t, db)
				}
			}
		})
		if r.err != nil {
			t.Fatal(r.err)
		}
		if r.secondCalls != 1 || r.plan.Flow != RestoreFlowReplaceWithoutOpen || !r.plan.SecondConfirmation || r.plan.OpenOriginal {
			t.Fatalf("calls %d, plan %+v", r.secondCalls, r.plan)
		}
		// The order of stages: first confirmation, preservation, reassessment, second
		// confirmation, gates, replacement -- and never an Open or a snapshot.
		want := "planned,confirmed,preserved,reassessed,second-prompt,second-confirmed,gates-passed,pre-journal-gate,replaced"
		if strings.Join(r.stages, ",") != want {
			t.Fatalf("stages:\n got %v\nwant %s", r.stages, want)
		}
		if atReplace != mainBefore {
			t.Fatal("the original changed before it was replaced: something opened or modified it")
		}
		if r.res.SafetySnapshot != "" || len(riSnapshots(t, filepath.Dir(db))) != 0 {
			t.Fatalf("no snapshot in a flow that never opens the original: %+v", r.res)
		}
		if icFileHash(t, filepath.Join(r.res.RawPreservation, "main")) != mainBefore {
			t.Fatal("the preservation is not the original")
		}
		riAssertRestored(t, db, kp)
	})
	for name, answer := range map[string]string{"y": "y\n", "yes": "yes\n", "another word": "no\n", "wrong case": "Replace\n", "padded": " replace\n", "EOF": "", "unterminated": "replace"} {
		t.Run("rejects "+name, func(t *testing.T) {
			t.Parallel()
			db := riMismatch(t)
			mainBefore := icFileHash(t, db)
			r := riDo(t, db, cand, kp, func(o *RestoreOptions, r *riRun) {
				o.ConfirmAfterPreservation = func(RestorePlan, PreservationInfo) bool {
					r.secondCalls++
					return SecondConfirmationAccepted(strings.NewReader(answer))
				}
			})
			declined(t, r, db, mainBefore)
		})
	}
	t.Run("an unavailable callback declines", func(t *testing.T) {
		t.Parallel()
		db := riMismatch(t)
		mainBefore := icFileHash(t, db)
		r := riDo(t, db, cand, kp, nil) // ConfirmAfterPreservation is nil
		declined(t, r, db, mainBefore)
	})
	t.Run("an answer to the first confirmation (what --force gives) is not the second", func(t *testing.T) {
		t.Parallel()
		db := riMismatch(t)
		mainBefore := icFileHash(t, db)
		r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
			o.Confirm = func(sqlite.CandidateInfo) bool { return true } // skips only the normal prompt
		})
		declined(t, r, db, mainBefore)
	})
	t.Run("the callback receives the plan and the preservation", func(t *testing.T) {
		t.Parallel()
		var gotPlan RestorePlan
		var gotInfo PreservationInfo
		r, _ := run(t, func(p RestorePlan, i PreservationInfo) bool { gotPlan, gotInfo = p, i; return false })
		if gotPlan.Flow != RestoreFlowReplaceWithoutOpen || gotInfo.Dir == "" || gotInfo.Dir != r.res.RawPreservation || len(gotInfo.Files) == 0 {
			t.Fatalf("plan %+v info %+v", gotPlan, gotInfo)
		}
	})
	t.Run("only the R1 plan asks the second question", func(t *testing.T) {
		t.Parallel()
		for name, build := range map[string]func(*testing.T) string{
			"plain initialized": riInitialized, "empty file": riEmptyFile, "missing": riMissing, "older schema": riOlderSchema,
		} {
			r := riDo(t, build(t), cand, kp, func(o *RestoreOptions, r *riRun) {
				o.ConfirmAfterPreservation = func(RestorePlan, PreservationInfo) bool { r.secondCalls++; return true }
			})
			if r.err != nil || r.secondCalls != 0 {
				t.Fatalf("%s: err %v, second confirmation asked %d times", name, r.err, r.secondCalls)
			}
		}
	})
}

func TestRestoreIntegration_FirstConfirmationOrder(t *testing.T) {
	t.Parallel()
	cand, kp, _ := riCandidate(t)
	db := riEmptyFile(t)
	var order []string
	r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
		o.OnPlan = func(RestorePlan) { order = append(order, "onplan") }
		o.Confirm = func(sqlite.CandidateInfo) bool { order = append(order, "confirm"); return true }
		real := o.testHook
		o.testHook = func(s string) { real(s); order = append(order, s) }
	})
	if r.err != nil {
		t.Fatal(r.err)
	}
	got := strings.Join(order, ",")
	if !strings.HasPrefix(got, "planned,onplan,confirm,confirmed,preserved,") {
		t.Fatalf("the plan is reported, then confirmed, then preserved: %s", got)
	}
}

// ---- F. replacement, the journal gate, and the stale-journal regression -------------------------------------------------------------

// A rollback journal left beside a replaced database is applied to it on the next
// open. Control: prove the fixture really can revert restored contents.
func TestRestoreIntegration_StaleHotJournal_ControlRevertsAndRestoreDoesNot(t *testing.T) {
	t.Parallel()
	cand, kp, _ := riCandidate(t)
	db, journal := riEmptyWithHotJournal(t)

	// Control: put the candidate's bytes where the database is, leave the journal
	// in place (what a replacement that forgot the journal would produce), and let
	// SQLite open it.
	control := securefstest.TempDir(t)
	b, err := os.ReadFile(cand)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(control, "c.db"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(control, "c.db-journal"), journal, 0o600); err != nil {
		t.Fatal(err)
	}
	cdb, err := sql.Open("sqlite", "file:"+filepath.Join(control, "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	cdb.SetMaxOpenConns(1)
	var integrity string
	ierr := cdb.QueryRow(`PRAGMA integrity_check`).Scan(&integrity)
	var slugs []string
	if rows, err := cdb.Query(`SELECT slug FROM projects ORDER BY slug`); err == nil {
		for rows.Next() {
			var s string
			_ = rows.Scan(&s)
			slugs = append(slugs, s)
		}
		rows.Close()
	}
	_ = cdb.Close()
	if ierr == nil && integrity == "ok" && strings.Join(slugs, ",") == "candproj" {
		t.Fatalf("the fixture cannot demonstrate the failure mode: the stale journal left the candidate intact (integrity %q, projects %v)", integrity, slugs)
	}
	t.Logf("control: with the stale journal left beside it, the candidate opens as projects=%v, integrity=%q (%v)", slugs, integrity, ierr)

	// The real restore removes the preserved journal first.
	r := riDo(t, db, cand, kp, nil)
	if r.err != nil {
		t.Fatal(r.err)
	}
	if r.plan.Flow != RestoreFlowReplaceWithoutOpen || !r.plan.Preserve || r.res.RawPreservation == "" {
		t.Fatalf("plan %+v, result %+v", r.plan, r.res)
	}
	if got, _ := os.ReadFile(filepath.Join(r.res.RawPreservation, "journal")); !bytes.Equal(got, journal) {
		t.Fatal("the journal must be preserved byte for byte before it is removed")
	}
	if _, err := os.Lstat(db + "-journal"); !os.IsNotExist(err) {
		t.Fatalf("the old journal must be gone before the restored database is opened (%v)", err)
	}
	riAssertRestored(t, db, kp)
}

func TestRestoreIntegration_JournalGate(t *testing.T) {
	t.Parallel()
	cand, kp, _ := riCandidate(t)
	t.Run("an unpreserved non-empty journal blocks replacement", func(t *testing.T) {
		t.Parallel()
		db := riInitialized(t)
		mainBefore := icFileHash(t, db)
		cold := bytes.Repeat([]byte{0}, 2048)
		r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
			real := o.testHook
			o.testHook = func(s string) {
				real(s)
				if s == stagePreJournalGate { // after Open and Close, nothing preserved
					if err := os.WriteFile(db+"-journal", cold, 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
		if !errors.Is(r.err, ErrRestoreJournalNotPreserved) || r.reached(stageReplaced) {
			t.Fatalf("got %v (stages %v)", r.err, r.stages)
		}
		if got, _ := os.ReadFile(db + "-journal"); !bytes.Equal(got, cold) {
			t.Fatal("the unpreserved journal was deleted or changed")
		}
		if icFileHash(t, db) == "" || mainBefore == "" {
			t.Fatal("unreachable")
		}
		if r.res.SafetySnapshot == "" || !strings.Contains(r.err.Error(), r.res.SafetySnapshot) {
			t.Fatalf("the safety snapshot should be named: %v", r.err)
		}
	})
	t.Run("a journal that no longer matches its preservation blocks replacement", func(t *testing.T) {
		t.Parallel()
		db, journal := riEmptyWithHotJournal(t)
		r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
			real := o.testHook
			o.testHook = func(s string) {
				real(s)
				if s == stagePreJournalGate {
					f, err := os.OpenFile(db+"-journal", os.O_WRONLY|os.O_APPEND, 0)
					if err != nil {
						t.Fatal(err)
					}
					_, _ = f.Write([]byte("more"))
					_ = f.Close()
				}
			}
		})
		if !errors.Is(r.err, ErrRestoreJournalNotPreserved) || r.reached(stageReplaced) {
			t.Fatalf("got %v (stages %v)", r.err, r.stages)
		}
		got, _ := os.ReadFile(db + "-journal")
		if !bytes.HasPrefix(got, journal) {
			t.Fatal("the journal was altered beyond the test's own append")
		}
		if got, _ := os.ReadFile(filepath.Join(r.res.RawPreservation, "journal")); !bytes.Equal(got, journal) {
			t.Fatal("the preserved journal must be intact")
		}
	})
	t.Run("the journal gate itself re-checks that the preservation still matches the original", func(t *testing.T) {
		t.Parallel()
		db, _ := riEmptyWithHotJournal(t)
		r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
			real := o.testHook
			o.testHook = func(s string) {
				real(s)
				if s == stagePreJournalGate { // after the earlier gates; the main file now differs from the preserved copy
					f, err := os.OpenFile(db, os.O_WRONLY|os.O_APPEND, 0)
					if err != nil {
						t.Fatal(err)
					}
					_, _ = f.Write([]byte("late"))
					_ = f.Close()
				}
			}
		})
		if !errors.Is(r.err, ErrRestoreJournalNotPreserved) || r.reached(stageReplaced) {
			t.Fatalf("got %v (stages %v)", r.err, r.stages)
		}
	})
	t.Run("a non-empty journal missing from the preservation blocks replacement", func(t *testing.T) {
		t.Parallel()
		db := riEmptyFile(t)
		r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
			real := o.testHook
			o.testHook = func(s string) {
				real(s)
				if s == stagePreserved { // a journal appears after the preservation was made
					if err := os.WriteFile(db+"-journal", bytes.Repeat([]byte{1}, 600), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
		if r.err == nil || r.reached(stageReplaced) {
			t.Fatalf("got %v (stages %v)", r.err, r.stages)
		}
		if got, _ := os.ReadFile(db + "-journal"); len(got) != 600 {
			t.Fatal("the late journal was deleted")
		}
	})
	t.Run("replaceDatabaseFile itself refuses a non-empty journal unless told it is preserved", func(t *testing.T) {
		t.Parallel()
		dir := securefstest.TempDir(t)
		db := filepath.Join(dir, "c.db")
		cand2 := filepath.Join(dir, "new.db")
		for p, c := range map[string]string{db: "old", cand2: "new", db + "-wal": "wal", db + "-shm": "shm", db + "-journal": "journal bytes"} {
			if err := os.WriteFile(p, []byte(c), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := replaceDatabaseFile(db, cand2); !errors.Is(err, ErrRestoreJournalNotPreserved) {
			t.Fatalf("got %v", err)
		}
		// Nothing was removed or replaced.
		for p, c := range map[string]string{db: "old", db + "-wal": "wal", db + "-shm": "shm", db + "-journal": "journal bytes"} {
			if b, _ := os.ReadFile(p); string(b) != c {
				t.Fatalf("%s changed", p)
			}
		}
		if err := replaceDatabaseFileOpts(db, cand2, true, nil); err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(db); string(b) != "new" {
			t.Fatal("not replaced")
		}
		for _, sfx := range []string{"-wal", "-shm", "-journal"} {
			if _, err := os.Lstat(db + sfx); !os.IsNotExist(err) {
				t.Fatalf("%s should be gone (%v)", sfx, err)
			}
		}
	})
	t.Run("replaceDatabaseFile removes an empty or absent journal and refuses a non-regular one", func(t *testing.T) {
		t.Parallel()
		dir := securefstest.TempDir(t)
		db := filepath.Join(dir, "c.db")
		cand2 := filepath.Join(dir, "new.db")
		if err := os.WriteFile(cand2, []byte("new"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(db+"-journal", nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := replaceDatabaseFile(db, cand2); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(db + "-journal"); !os.IsNotExist(err) {
			t.Fatal("the empty journal should be gone")
		}
		if err := os.Mkdir(db+"-journal", 0o700); err != nil {
			t.Fatal(err)
		}
		before, _ := os.ReadFile(db)
		if err := replaceDatabaseFileOpts(db, cand2, true, nil); err == nil {
			t.Fatal("a journal that is a directory must be refused")
		}
		after, _ := os.ReadFile(db)
		if !bytes.Equal(before, after) {
			t.Fatal("the database changed")
		}
	})
}

func TestRestoreIntegration_NoOpenFlows_NeverOpenTheOriginal(t *testing.T) {
	t.Parallel()
	cand, kp, _ := riCandidate(t)
	cases := map[string]func(t *testing.T) string{
		"initialized, structure mismatch (R1)": riMismatch,
		"empty file (R2)":                      riEmptyFile,
		"dso-unbound (R2)":                     func(t *testing.T) string { return icUnbound(t) },
		"binding-corrupt (R2)": func(t *testing.T) string {
			e := icBound(t, true)
			icEdit(t, e.dbPath, icDropMetaTriggers1, icDropMetaTriggers2, `DELETE FROM store_meta WHERE key = 'binding.canary'`)
			return e.dbPath
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db := build(t)
			dir := filepath.Dir(db)
			before := riSnap(t, dir)
			opened, snapshotted := 0, 0
			var atGates, atJournalGate map[string]rpFileState
			r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
				o.ConfirmAfterPreservation = func(RestorePlan, PreservationInfo) bool { return true }
				real := o.testHook
				o.testHook = func(s string) {
					real(s)
					switch s {
					case stageOpenOriginal:
						opened++
					case stageSnapshot:
						snapshotted++
					case stageGatesPassed:
						atGates = riSnap(t, dir)
					case stagePreJournalGate:
						atJournalGate = riSnap(t, dir)
					}
				}
				o.testSafetySnapshot = func(context_ context.Context, _ *sqlite.Store, _ string) (string, error) {
					snapshotted++
					return "", errors.New("a snapshot must not be taken")
				}
				o.testReopenOriginal = func(context_ context.Context, _ string) (*sqlite.Store, error) {
					opened++
					return nil, errors.New("the original must not be reopened")
				}
			})
			if r.err != nil {
				t.Fatal(r.err)
			}
			if opened != 0 || snapshotted != 0 || r.plan.OpenOriginal || r.res.SafetySnapshot != "" {
				t.Fatalf("opened=%d snapshotted=%d plan=%+v", opened, snapshotted, r.plan)
			}
			// Right up to the replacement the original's files are byte-, mtime- and inode-identical:
			// nothing opened, migrated, checkpointed or created beside them.
			rpSameDir(t, before, withoutPreservations(atGates, before))
			rpSameDir(t, before, withoutPreservations(atJournalGate, before))
			for name := range atJournalGate {
				if strings.HasSuffix(name, "-wal") || strings.HasSuffix(name, "-shm") {
					if _, existed := before[name]; !existed {
						t.Fatalf("%s was created beside the original", name)
					}
				}
			}
			if len(riSnapshots(t, dir)) != 0 {
				t.Fatal("no safety snapshot file may exist")
			}
			riAssertRestored(t, db, kp)
		})
	}
}

// withoutPreservations returns a copy of a restricted to the names in b, so a
// comparison ignores the preservation directory that appeared by design.
func withoutPreservations(a, b map[string]rpFileState) map[string]rpFileState {
	out := map[string]rpFileState{}
	for n, st := range a {
		if _, ok := b[n]; ok {
			out[n] = st
		}
	}
	return out
}

func TestRestoreIntegration_ReplacementAndReopenFailures(t *testing.T) {
	t.Parallel()
	cand, kp, _ := riCandidate(t)
	t.Run("a replacement failure in a no-Open flow names the preservation and writes no audit event", func(t *testing.T) {
		t.Parallel()
		db := riMismatch(t)
		eventsBefore := len(eventsAt(t, db))
		mainBefore := icFileHash(t, db)
		r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
			o.ConfirmAfterPreservation = func(RestorePlan, PreservationInfo) bool { return true }
			o.testReplaceDatabaseFile = func(string, string) error { return errors.New("disk exploded") }
		})
		if r.err == nil || !strings.Contains(r.err.Error(), "disk exploded") || r.res.RawPreservation == "" ||
			!strings.Contains(r.err.Error(), r.res.RawPreservation) || strings.Contains(r.err.Error(), "is not needed") {
			t.Fatalf("got %v", r.err)
		}
		if icFileHash(t, db) != mainBefore {
			t.Fatal("the original main file changed")
		}
		if got := len(eventsAt(t, db)); got != eventsBefore {
			t.Fatalf("a flow that never opened the original must not write an audit event there (%d -> %d)", eventsBefore, got)
		}
	})
	t.Run("a replacement failure in the snapshot flow keeps the existing behavior", func(t *testing.T) {
		t.Parallel()
		db := riInitialized(t)
		eventsBefore := len(eventsAt(t, db))
		r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
			o.testReplaceDatabaseFile = func(string, string) error { return errors.New("disk exploded") }
		})
		if r.err == nil || r.res.SafetySnapshot == "" || !strings.Contains(r.err.Error(), "is not needed") {
			t.Fatalf("got %v", r.err)
		}
		if got := len(eventsAt(t, db)); got != eventsBefore+1 {
			t.Fatalf("the original was opened, so the failure is audited there as before (%d -> %d)", eventsBefore, got)
		}
	})
	t.Run("a reopen failure in a no-Open flow names the raw preservation", func(t *testing.T) {
		t.Parallel()
		db := riEmptyFile(t)
		r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
			o.testReopen = func(context_ context.Context, _ string) (*sqlite.Store, error) {
				return nil, errors.New("cannot reopen")
			}
		})
		if r.err == nil || !strings.Contains(r.err.Error(), "WAS ACTIVATED") || !strings.Contains(r.err.Error(), r.res.RawPreservation) {
			t.Fatalf("got %v", r.err)
		}
	})
}

func TestRestoreIntegration_CompletedPreservationIsNeverDeleted(t *testing.T) {
	t.Parallel()
	cand, kp, _ := riCandidate(t)
	db := riEmptyFile(t)
	dir := filepath.Dir(db)
	first := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
		o.testReplaceDatabaseFile = func(string, string) error { return errors.New("fail after preserving") }
	})
	if first.err == nil || first.res.RawPreservation == "" {
		t.Fatalf("got %v", first.err)
	}
	keep := first.res.RawPreservation
	hash := icFileHash(t, filepath.Join(keep, "manifest.json"))
	// A later successful restore and a later failed one both leave it alone.
	second := riDo(t, db, cand, kp, nil)
	if second.err != nil {
		t.Fatal(second.err)
	}
	third := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
		o.testPreserve = func(string, sqlite.FileIdentity) (*sqlite.Preservation, error) {
			return nil, sqlite.ErrPreserveNoSpace
		}
	})
	_ = third
	if icFileHash(t, filepath.Join(keep, "manifest.json")) != hash {
		t.Fatal("a completed preservation changed")
	}
	if l := riPreservations(t, dir); len(l) < 1 {
		t.Fatalf("a completed preservation was deleted: %v", l)
	}
}

// ---- dry run and the lock -------------------------------------------------------------------------------------------------------------

func TestRestoreIntegration_DryRunNeverExaminesTheActiveDatabase(t *testing.T) {
	t.Parallel()
	cand, kp, _ := riCandidate(t)
	db := riMissing(t)
	r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
		o.DryRun = true
		o.testAssess = func(string) activeAssessment {
			t.Error("a dry run must not classify the active database")
			return activeAssessment{}
		}
		o.testSidecars = func(string) (sqlite.ActiveSidecarsInfo, error) {
			t.Error("a dry run must not read the active database's companions")
			return sqlite.ActiveSidecarsInfo{}, nil
		}
		o.OnPlan = func(RestorePlan) { t.Error("a dry run makes no plan") }
	})
	if r.err != nil || !r.res.DryRun || r.confirms != 0 || len(r.stages) != 0 {
		t.Fatalf("got %v %+v stages %v", r.err, r.res, r.stages)
	}
	if !strings.Contains(r.res.DryRunNotice, "not examined") || !strings.Contains(r.res.DryRunNotice, "may still be refused") {
		t.Fatalf("notice: %q", r.res.DryRunNotice)
	}
	if _, err := os.Lstat(db + ".central.lock"); !os.IsNotExist(err) {
		t.Fatalf("a dry run must not even take the lock (%v)", err)
	}
	if r.res.Plan.Flow != RestoreFlowRefuse || r.res.RawPreservation != "" {
		t.Fatalf("plan %+v", r.res.Plan)
	}
}

func TestRestoreIntegration_InspectionHappensUnderTheExclusiveLock(t *testing.T) {
	t.Parallel()
	cand, kp, _ := riCandidate(t)
	db := riInitialized(t)
	var lockErr error
	r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
		o.testAssess = func(p string) activeAssessment {
			// If Restore did not hold the exclusive lock yet, this would succeed.
			if l, err := sqlite.AcquireExclusiveCentralLock(db, 100*time.Millisecond); err == nil {
				_ = l.Close()
				lockErr = errors.New("the exclusive lock was not held while the active database was assessed")
			}
			return assessActive(p)
		}
	})
	if r.err != nil {
		t.Fatal(r.err)
	}
	if lockErr != nil {
		t.Fatal(lockErr)
	}
}

// ---- G. interruption at every boundary -----------------------------------------------------------------------------------------------------

func riKilledRestore(t *testing.T, db, cand, keyFile, stage string) {
	t.Helper()
	c := exec.Command(os.Args[0], "-test.run=^TestHelperProcessRestoreIntegration$") // #nosec G204 -- this test binary
	c.Env = append(os.Environ(), "DSO_TEST_RI_KILL="+stage, "DSO_TEST_RI_DB="+db, "DSO_TEST_RI_CAND="+cand, "DSO_TEST_RI_KEYFILE="+keyFile)
	c.Stderr = os.Stderr
	err := c.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("the restore should have been killed at %s: %v", stage, err)
	}
	if ws, ok := ee.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
		t.Fatalf("expected SIGKILL at %s, got %v", stage, err)
	}
}

func TestRestoreIntegration_KilledAtEveryBoundary_LeavesAnExplainableSafeState(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"confirmed", "preserved", "reassessed", "gates-passed", "pre-journal-gate", "during-replace"} {
		t.Run("no-Open flow, killed at "+stage, func(t *testing.T) {
			t.Parallel()
			cand, kp, keyFile := riCandidate(t)
			db, journal := riEmptyWithHotJournal(t)
			dir := filepath.Dir(db)
			riKilledRestore(t, db, cand, keyFile, stage)

			pres := riPreservations(t, dir)
			switch stage {
			case "confirmed": // immediately before the preservation is made
				if len(pres) != 0 {
					t.Fatalf("nothing was preserved yet: %v", pres)
				}
			default:
				if len(pres) != 1 || strings.HasSuffix(pres[0], ".partial") {
					t.Fatalf("one complete, published preservation expected: %v", pres)
				}
				if _, err := os.Lstat(filepath.Join(pres[0], "manifest.json")); err != nil {
					t.Fatalf("the preservation is not complete: %v", err)
				}
				if got, _ := os.ReadFile(filepath.Join(pres[0], "journal")); !bytes.Equal(got, journal) {
					t.Fatal("the preservation does not hold the original journal")
				}
			}
			// The original: untouched up to the moment of replacement. "during-replace" is
			// after the original files were MOVED ASIDE (nothing is deleted) and before the
			// replacement is installed: the original path is empty, and every original file
			// is retained, byte for byte, in the aside directory.
			var aside string
			if stage == "during-replace" {
				if _, err := os.Lstat(db); !os.IsNotExist(err) {
					t.Fatalf("the original main file has been moved aside, so the original path must be empty (%v)", err)
				}
				if _, err := os.Lstat(db + "-journal"); !os.IsNotExist(err) {
					t.Fatalf("the original journal has been moved aside, so the original path must hold none (%v)", err)
				}
				asides, _ := filepath.Glob(db + ".replaced-*")
				if len(asides) != 1 {
					t.Fatalf("one aside directory expected: %v", asides)
				}
				aside = asides[0]
				if fi, err := os.Stat(filepath.Join(aside, "main")); err != nil || fi.Size() != 0 {
					t.Fatalf("the original zero-byte main file must be retained in the aside directory (%v)", err)
				}
				if got, _ := os.ReadFile(filepath.Join(aside, "journal")); !bytes.Equal(got, journal) {
					t.Fatal("the original journal must be retained byte for byte in the aside directory")
				}
			} else {
				if fi, err := os.Stat(db); err != nil || fi.Size() != 0 {
					t.Fatalf("the original main file must still be the original zero-byte file (%v)", err)
				}
				if got, jerr := os.ReadFile(db + "-journal"); !bytes.Equal(got, journal) {
					t.Fatalf("the original journal must be untouched at %s (%v)", stage, jerr)
				}
			}
			// ... and the interrupted state is recoverable by running restore again.
			r := riDo(t, db, cand, kp, nil)
			if r.err != nil {
				t.Fatalf("a rerun after a kill at %s: %v", stage, r.err)
			}
			riAssertRestored(t, db, kp)
			for _, p := range pres {
				if _, err := os.Lstat(p); err != nil {
					t.Fatalf("a completed preservation was deleted by the rerun: %v", err)
				}
			}
			if aside != "" {
				// The rerun must not touch what the interrupted restore retained.
				if got, _ := os.ReadFile(filepath.Join(aside, "journal")); !bytes.Equal(got, journal) {
					t.Fatal("the retained original journal was changed or deleted by the rerun")
				}
				if _, err := os.Lstat(filepath.Join(aside, "main")); err != nil {
					t.Fatalf("the retained original main file was deleted by the rerun: %v", err)
				}
			}
		})
	}
	t.Run("R1 flow, killed before the second confirmation", func(t *testing.T) {
		t.Parallel()
		cand, kp, keyFile := riCandidate(t)
		db := riMismatch(t)
		dir := filepath.Dir(db)
		mainBefore := icFileHash(t, db)
		before := riSnap(t, dir)
		riKilledRestore(t, db, cand, keyFile, "second-prompt")
		pres := riPreservations(t, dir)
		if len(pres) != 1 {
			t.Fatalf("one preservation expected: %v", pres)
		}
		if _, err := os.Lstat(filepath.Join(pres[0], "manifest.json")); err != nil {
			t.Fatal(err)
		}
		if icFileHash(t, db) != mainBefore || len(riSnapshots(t, dir)) != 0 {
			t.Fatal("the original must be untouched and unopened")
		}
		rpSameDir(t, before, withoutPreservations(riSnap(t, dir), before))
		if icFileHash(t, filepath.Join(pres[0], "main")) != mainBefore {
			t.Fatal("the preservation is not the original")
		}
		// A rerun with the second confirmation completes.
		r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
			o.ConfirmAfterPreservation = func(RestorePlan, PreservationInfo) bool { return true }
		})
		if r.err != nil {
			t.Fatal(r.err)
		}
		riAssertRestored(t, db, kp)
	})
}
