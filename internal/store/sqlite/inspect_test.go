package sqlite

// Tests for InspectDatabase (see inspect.go for the algorithm, the caller
// contract and the documented limits). Crash-state fixtures come from helper
// processes that run the real Open / CreateProject code and then SIGKILL
// themselves, so the WAL is left exactly as an interrupted process leaves it.
// All databases, sidecars and synchronization files live in test temporary
// directories.
//
// Every test builds its own *inspector, so nothing here mutates shared state
// and the tests are safe to run in parallel. A test never treats an inspection
// error as an acceptable "not initialized" answer: every failure case asserts
// its exact error category.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/securefs/securefstest"
	"github.com/docker-secret-operator/dso/internal/store"
)

var bg = context.Background()

// ---- helper process ------------------------------------------------------------

// TestHelperProcessInspect is not a real test: it only runs when
// DSO_TEST_INSPECT_DB is set by one of the tests below.
//
// Modes (DSO_TEST_INSPECT_MODE):
//
//	open-kill  - Open (migrates), then SIGKILL without Close or checkpoint
//	seed-kill  - Open, CreateProject, then SIGKILL
//	writer     - long-lived raw writer, checkpoint(TRUNCATE) every 20 inserts
//	cycle      - raw writer that opens, writes, checkpoints and CLOSES in a loop
//	inspect    - run InspectDatabase and print "RESULT:..." (read-only mount tests)
func TestHelperProcessInspect(t *testing.T) {
	path := os.Getenv("DSO_TEST_INSPECT_DB")
	if path == "" {
		t.Skip("helper process")
	}
	switch mode := os.Getenv("DSO_TEST_INSPECT_MODE"); mode {
	case "open-kill", "seed-kill":
		s, err := Open(ctx, path, Options{})
		if err != nil {
			fmt.Fprintln(os.Stderr, "inspect helper: open:", err)
			os.Exit(2)
		}
		if mode == "seed-kill" {
			if err := s.CreateProject(ctx, helperProject("pk")); err != nil {
				fmt.Fprintln(os.Stderr, "inspect helper: seed:", err)
				os.Exit(2)
			}
		}
		_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
		select {} // not reached
	case "writer", "cycle":
		helperWriter(path, mode == "cycle", os.Getenv("DSO_TEST_INSPECT_SYNC"))
	case "inspect":
		in := newInspector()
		if os.Getenv("DSO_TEST_INSPECT_NO_SECUREFS") == "1" {
			// Inside a user namespace root-owned parents look unowned and the
			// directory-chain check would reject the path; everything after
			// the file gate (sidecars, views, retries) is what this exercises.
			in.openPrivate = func(p string) (*os.File, error) { return os.Open(p) }
		}
		var f facts
		res, err := in.inspect(path, factsProbe(&f))
		view := res.View
		switch {
		case err == nil:
			fmt.Printf("RESULT:OK view=%s tables=%d\n", view, f.tables)
		case errors.Is(err, ErrEmptyDatabase):
			fmt.Printf("RESULT:EMPTY %v\n", err)
		case errors.Is(err, ErrNotSQLite):
			fmt.Printf("RESULT:NOTSQLITE %v\n", err)
		case errors.Is(err, ErrInspectionFailed):
			fmt.Printf("RESULT:FAILED %v\n", err)
		default:
			fmt.Printf("RESULT:OTHER %v\n", err)
		}
	default:
		fmt.Fprintln(os.Stderr, "inspect helper: unknown mode", mode)
		os.Exit(2)
	}
}

func helperProject(id string) store.Project {
	return store.Project{ID: id, Slug: id, Name: id, WrappedDEK: []byte{1}, DEKKeyID: "k", DEKVersion: 1, CreatedAt: time.Now()}
}

func helperWriter(path string, cycle bool, syncDir string) {
	deadline := time.Now().Add(25 * time.Second)
	dsn := "file:" + escapePath(path) + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	mk := func() *sql.DB {
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			fmt.Fprintln(os.Stderr, "inspect writer: open:", err)
			os.Exit(2)
		}
		db.SetMaxOpenConns(1)
		return db
	}
	db := mk()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS zz(a BLOB)`); err != nil {
		fmt.Fprintln(os.Stderr, "inspect writer: create:", err)
		os.Exit(2)
	}
	for i := 0; time.Now().Before(deadline); i++ {
		if _, err := db.Exec(`INSERT INTO zz VALUES (randomblob(2000))`); err != nil {
			fmt.Fprintln(os.Stderr, "inspect writer: insert:", err)
			os.Exit(2)
		}
		if cycle || i%20 == 19 {
			if _, err := db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
				fmt.Fprintln(os.Stderr, "inspect writer: checkpoint:", err)
				os.Exit(2)
			}
		}
		if cycle {
			_ = db.Close()
			db = mk()
		}
		if i == 0 {
			_ = os.WriteFile(filepath.Join(syncDir, "ready"), []byte("ready"), 0o600)
		}
		if _, err := os.Stat(filepath.Join(syncDir, "stop")); err == nil {
			break
		}
	}
	_ = db.Close()
}

// ---- probes, fixtures and assertions ---------------------------------------------

// facts is what factsProbe observed on its LAST invocation (the probe is
// idempotent: every call starts from a clean slate, as the contract requires).
type facts struct {
	calls         int
	tables        int
	names         string // sorted, comma-joined table names
	hasProjects   bool
	projects      int
	marker        int  // tables named "marker"
	writeRefused  bool // an attempted write through the Querier failed
	writeAccepted bool
}

func factsProbe(out *facts) func(Querier) error {
	return func(q Querier) error {
		calls := out.calls + 1
		*out = facts{calls: calls}
		rows, err := q.QueryContext(bg, `SELECT name FROM sqlite_master WHERE type='table' ORDER BY name`)
		if err != nil {
			return err
		}
		var names []string
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				rows.Close()
				return err
			}
			names = append(names, n)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		sort.Strings(names)
		out.tables, out.names = len(names), strings.Join(names, ",")
		for _, n := range names {
			switch n {
			case "projects":
				out.hasProjects = true
			case "marker":
				out.marker++
			}
		}
		if out.hasProjects {
			if err := q.QueryRowContext(bg, `SELECT count(*) FROM projects`).Scan(&out.projects); err != nil {
				return err
			}
		}
		if ex, ok := q.(interface {
			ExecContext(context.Context, string, ...any) (sql.Result, error)
		}); ok {
			if _, werr := ex.ExecContext(bg, `CREATE TABLE zz_inspect_must_fail(a)`); werr != nil {
				out.writeRefused = true
			} else {
				out.writeAccepted = true
			}
		}
		return nil
	}
}

// fast returns an inspector with short pauses; everything else is production.
func fast() *inspector {
	in := newInspector()
	in.retryDelay = 5 * time.Millisecond
	return in
}

// cleanInspectDB builds a cleanly closed database through the real Open/Close
// (fully checkpointed, no -wal/-shm). With seedProject it holds one project.
func cleanInspectDB(t *testing.T, seedProject bool) (dir, path string) {
	t.Helper()
	dir = securefstest.TempDir(t)
	path = filepath.Join(dir, "c.db")
	s, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if seedProject {
		if err := s.CreateProject(ctx, helperProject("p1")); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	assertNoSidecars(t, path)
	return dir, path
}

// foreignDB is a non-DSO SQLite file (rollback journal) with one table "t" and
// one row, created with mode 0600 like DSO's own files.
func foreignDB(t *testing.T) string {
	t.Helper()
	dir := securefstest.TempDir(t)
	path := filepath.Join(dir, "c.db")
	writeFile0600(t, path, nil)
	db, err := sql.Open("sqlite", "file:"+escapePath(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE t(a)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO t VALUES('xxxxxxxxxx')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// otherWriter opens path read-write in WAL mode, runs stmts, checkpoints and
// closes (the sidecars vanish), the way a DSO process that finishes would.
func otherWriter(t *testing.T, path string, stmts ...string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+escapePath(path)+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Error(err)
		return
	}
	db.SetMaxOpenConns(1)
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Errorf("writer %q: %v", s, err)
		}
	}
	if _, err := db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Errorf("writer checkpoint: %v", err)
	}
	_ = db.Close()
}

func assertNoSidecars(t *testing.T, path string) {
	t.Helper()
	for _, sfx := range []string{"-wal", "-shm"} {
		if _, err := os.Lstat(path + sfx); !os.IsNotExist(err) {
			t.Fatalf("%s%s must not exist (%v)", path, sfx, err)
		}
	}
}

func snapDir(t *testing.T, dir string) map[string]string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range ents {
		if e.Type()&os.ModeSymlink != 0 {
			target, _ := os.Readlink(filepath.Join(dir, e.Name()))
			out[e.Name()] = "link->" + target
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(b)
		out[e.Name()] = fmt.Sprintf("%d:%s", len(b), hex.EncodeToString(sum[:6]))
	}
	return out
}

func sameSnap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func fileHash(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%d:%s", len(b), hex.EncodeToString(sum[:6]))
}

// writeFile0600 creates a file the way DSO's own files are created.
func writeFile0600(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func runInspectHelper(t *testing.T, path, mode string, extra ...string) *exec.Cmd {
	t.Helper()
	c := exec.Command(os.Args[0], "-test.run=^TestHelperProcessInspect$") // #nosec G204 -- this test binary
	c.Env = append(os.Environ(), "DSO_TEST_INSPECT_DB="+path, "DSO_TEST_INSPECT_MODE="+mode)
	c.Env = append(c.Env, extra...)
	c.Stderr = os.Stderr
	return c
}

// crashHelper runs a kill-mode helper and requires that it died by SIGKILL.
func crashHelper(t *testing.T, path, mode string) {
	t.Helper()
	c := runInspectHelper(t, path, mode)
	err := c.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("helper %s: expected it to be killed, got %v", mode, err)
	}
	ws, ok := ee.Sys().(syscall.WaitStatus)
	if !ok || !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
		t.Fatalf("helper %s: expected SIGKILL, got %v", mode, err)
	}
}

// crashAfterOpen leaves schema (and nothing else) only in the WAL: the real Open
// ran, then the process was killed without Close or checkpoint.
func crashAfterOpen(t *testing.T) (dir, path string) {
	t.Helper()
	dir = securefstest.TempDir(t)
	path = filepath.Join(dir, "c.db")
	crashHelper(t, path, "open-kill")
	for _, sfx := range []string{"", "-wal", "-shm"} {
		if _, err := os.Lstat(path + sfx); err != nil {
			t.Fatalf("crash fixture is missing %s%s: %v", path, sfx, err)
		}
	}
	if fi, _ := os.Stat(path); fi.Size() >= 16384 {
		t.Fatalf("fixture main file is %d bytes; the schema should be WAL-resident", fi.Size())
	}
	return dir, path
}

// rawWriter is an independent read-write connection in WAL mode (not a Store).
func rawWriter(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+escapePath(path)+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func insertProjectSQL(id string) string {
	return fmt.Sprintf(`INSERT INTO projects(id,slug,name,wrapped_dek,dek_key_id,dek_version,created_at)
	VALUES('%s','%s','%s',x'01','k',1,0)`, id, id, id)
}

// mustInspect runs the inspection and requires success.
func mustInspect(t *testing.T, in *inspector, path string) (InspectionResult, facts) {
	t.Helper()
	var f facts
	res, err := in.inspect(path, factsProbe(&f))
	if err != nil {
		t.Fatalf("inspection failed: %v", err)
	}
	if !f.writeRefused || f.writeAccepted {
		t.Fatalf("the inspection transaction accepted a write (refused=%v accepted=%v)", f.writeRefused, f.writeAccepted)
	}
	return res, f
}

// exactly asserts err wraps want and none of the other categories.
func exactly(t *testing.T, err, want error) {
	t.Helper()
	for _, c := range []error{ErrEmptyDatabase, ErrNotSQLite, ErrInspectionFailed} {
		if errors.Is(err, c) != (c == want) {
			t.Fatalf("want exactly %v, got %v", want, err)
		}
	}
}

// ---- file-level categories ---------------------------------------------------------

func TestInspect_NilProbe_Rejected(t *testing.T) {
	t.Parallel()
	_, path := cleanInspectDB(t, false)
	if _, err := InspectDatabase(path, nil); err == nil {
		t.Fatal("a nil probe must be rejected")
	}
}

func TestInspect_EmptyFile_ErrEmptyDatabase(t *testing.T) {
	t.Parallel()
	dir := securefstest.TempDir(t)
	path := filepath.Join(dir, "c.db")
	writeFile0600(t, path, nil)
	before := snapDir(t, dir)
	var f facts
	_, err := InspectDatabase(path, factsProbe(&f))
	exactly(t, err, ErrEmptyDatabase)
	if f.calls != 0 {
		t.Fatalf("the probe ran %d times against an empty file", f.calls)
	}
	if !sameSnap(before, snapDir(t, dir)) {
		t.Fatal("inspecting an empty file changed the directory")
	}
}

func TestInspect_NotSQLite_ErrNotSQLite(t *testing.T) {
	t.Parallel()
	for name, content := range map[string][]byte{
		"text":            []byte(strings.Repeat("this is not a database\n", 20)),
		"short":           []byte("SQLite"),
		"magic-minus-nul": []byte("SQLite format 3" + strings.Repeat("x", 200)),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := securefstest.TempDir(t)
			path := filepath.Join(dir, "c.db")
			writeFile0600(t, path, content)
			before := snapDir(t, dir)
			var f facts
			_, err := InspectDatabase(path, factsProbe(&f))
			exactly(t, err, ErrNotSQLite)
			if f.calls != 0 {
				t.Fatalf("the probe ran %d times against a non-SQLite file", f.calls)
			}
			if !sameSnap(before, snapDir(t, dir)) {
				t.Fatal("inspecting a non-SQLite file changed the directory")
			}
		})
	}
}

func TestInspect_FinalComponentSymlinkRejected(t *testing.T) {
	t.Parallel()
	_, real := cleanInspectDB(t, false)
	dir := filepath.Dir(real)
	for name, target := range map[string]string{"valid-target": real, "dangling": filepath.Join(dir, "nothing.db")} {
		t.Run(name, func(t *testing.T) {
			link := filepath.Join(dir, name+".db")
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			var f facts
			_, err := InspectDatabase(link, factsProbe(&f))
			if err == nil || !strings.Contains(err.Error(), "symbolic link") {
				t.Fatalf("want a symbolic-link refusal, got %v", err)
			}
			for _, bad := range []error{ErrEmptyDatabase, ErrNotSQLite, ErrInspectionFailed} {
				if errors.Is(err, bad) {
					t.Fatalf("a symlink refusal must not be categorised as %v", bad)
				}
			}
			if f.calls != 0 {
				t.Fatal("the probe ran against a symlink")
			}
			assertNoSidecars(t, real)
		})
	}
}

func TestInspect_UnsafeFileMode_Refused(t *testing.T) {
	t.Parallel()
	_, path := cleanInspectDB(t, false)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	var f facts
	_, err := InspectDatabase(path, factsProbe(&f))
	if err == nil || !strings.Contains(err.Error(), "not safe to use") {
		t.Fatalf("want the private-file refusal, got %v", err)
	}
	if f.calls != 0 {
		t.Fatal("the probe ran against an unsafe file")
	}
	assertNoSidecars(t, path)
}

func TestInspect_DefaultInspectorKeepsTheSecurityGate(t *testing.T) {
	t.Parallel()
	in := newInspector()
	if in.openPrivate == nil || in.restarts < 1 || in.attempts < 1 || in.busyTimeout <= 0 {
		t.Fatalf("production inspector is not fully configured: %+v", in)
	}
	// Behavioural proof that the production gate is securefs: it refuses a
	// group-readable file, which a plain os.Open would not.
	_, path := cleanInspectDB(t, false)
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	var f facts
	if _, err := in.inspect(path, factsProbe(&f)); err == nil || !strings.Contains(err.Error(), "not safe to use") {
		t.Fatalf("the production gate accepted an unsafe file: %v", err)
	}
}

// ---- views ---------------------------------------------------------------------------

func TestInspect_CleanDatabase_NoSidecars_ImmutableAndCreatesNothing(t *testing.T) {
	t.Parallel()
	dir, path := cleanInspectDB(t, true)
	before := snapDir(t, dir)
	var f facts
	res, err := InspectDatabase(path, factsProbe(&f)) // the public entry point
	view := res.View
	if err != nil {
		t.Fatal(err)
	}
	if view != ViewImmutable {
		t.Fatalf("view = %v, want immutable", view)
	}
	if res.WALPresent || res.SHMPresent {
		t.Fatalf("a clean database has no sidecars, got %+v", res)
	}
	if !f.hasProjects || f.projects != 1 || !f.writeRefused || f.writeAccepted {
		t.Fatalf("unexpected facts: %+v", f)
	}
	assertNoSidecars(t, path)
	if after := snapDir(t, dir); !sameSnap(before, after) {
		t.Fatalf("inspection of a clean database changed files:\nbefore %v\nafter  %v", before, after)
	}
}

func TestInspect_WALAndSHM_NoWriter_SeesWALResidentSchema(t *testing.T) {
	t.Parallel()
	_, path := crashAfterOpen(t)
	// Contrast: an immutable view of this exact state cannot see the schema.
	imm, err := sql.Open("sqlite", inspectionDSN(path, true, 0))
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := imm.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("precondition: immutable view should see no tables, saw %d (%v)", n, err)
	}
	_ = imm.Close()

	mainBefore, walBefore := fileHash(t, path), fileHash(t, path+"-wal")
	res, f := mustInspect(t, fast(), path)
	view := res.View
	if view != ViewWALAware {
		t.Fatalf("view = %v, want wal-aware", view)
	}
	if !res.WALPresent || !res.SHMPresent {
		t.Fatalf("a crash state has a non-empty -wal and an -shm, got %+v", res)
	}
	if f.tables < 5 {
		t.Fatalf("the WAL-aware view saw %d tables; the WAL-resident schema must be visible", f.tables)
	}
	if fileHash(t, path) != mainBefore {
		t.Fatal("the main database file changed")
	}
	if fileHash(t, path+"-wal") != walBefore {
		t.Fatal("the -wal file changed")
	}
}

func TestInspect_WALResidentRow_SeenOnlyByWALAwareView(t *testing.T) {
	t.Parallel()
	_, path := cleanInspectDB(t, false)
	crashHelper(t, path, "seed-kill")
	imm, err := sql.Open("sqlite", inspectionDSN(path, true, 0))
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := imm.QueryRow(`SELECT count(*) FROM projects`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("precondition: immutable view should not see the WAL-resident project, saw %d (%v)", n, err)
	}
	_ = imm.Close()
	mainBefore := fileHash(t, path)
	res, f := mustInspect(t, fast(), path)
	view := res.View
	if view != ViewWALAware || f.projects != 1 {
		t.Fatalf("view=%v projects=%d, want wal-aware with the WAL-resident project", view, f.projects)
	}
	if fileHash(t, path) != mainBefore {
		t.Fatal("the main database file changed")
	}
}

func TestInspect_WALOnly_SHMMissing(t *testing.T) {
	t.Parallel()
	_, path := crashAfterOpen(t)
	if err := os.Remove(path + "-shm"); err != nil {
		t.Fatal(err)
	}
	mainBefore, walBefore := fileHash(t, path), fileHash(t, path+"-wal")
	res, f := mustInspect(t, fast(), path)
	view := res.View
	if view != ViewWALAware || f.tables < 5 {
		t.Fatalf("view=%v tables=%d: a WAL without an -shm must still be inspectable", view, f.tables)
	}
	// The -shm SQLite recreates for this read is not evidence of an earlier one.
	if !res.WALPresent || res.SHMPresent {
		t.Fatalf("WAL-only state: want WALPresent && !SHMPresent, got %+v", res)
	}
	// Documented side effect: SQLite recreates the -shm. The main file and the
	// existing -wal must be untouched.
	if fileHash(t, path) != mainBefore || fileHash(t, path+"-wal") != walBefore {
		t.Fatal("the main file or the -wal changed")
	}
}

func TestInspect_SHMOnly_WALMissing(t *testing.T) {
	t.Parallel()
	_, path := cleanInspectDB(t, true)
	writeFile0600(t, path+"-shm", make([]byte, 32768))
	mainBefore := fileHash(t, path)
	res, f := mustInspect(t, fast(), path)
	view := res.View
	if view != ViewWALAware || f.projects != 1 {
		t.Fatalf("view=%v projects=%d: a stray -shm must not make a clean database uninspectable", view, f.projects)
	}
	if res.WALPresent || !res.SHMPresent {
		t.Fatalf("SHM-only state: want !WALPresent && SHMPresent, got %+v", res)
	}
	// Documented side effect: SQLite may create the missing -wal. The main
	// file must be untouched.
	if fileHash(t, path) != mainBefore {
		t.Fatal("the main database file changed")
	}
}

func TestInspect_DamagedWAL_NeverBecomesNotSQLiteOrEmpty(t *testing.T) {
	t.Parallel()
	_, fullPath := crashAfterOpen(t)
	_, full := mustInspect(t, fast(), fullPath)

	damage := map[string]func(t *testing.T, wal string){
		"truncated-to-half": func(t *testing.T, wal string) {
			fi, _ := os.Stat(wal)
			if err := os.Truncate(wal, fi.Size()/2); err != nil {
				t.Fatal(err)
			}
		},
		"truncated-to-three-frames": func(t *testing.T, wal string) {
			if err := os.Truncate(wal, 32+3*(24+4096)); err != nil {
				t.Fatal(err)
			}
		},
		"garbage-after-valid-frames": func(t *testing.T, wal string) {
			f, err := os.OpenFile(wal, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			junk := make([]byte, 5000)
			_, _ = rand.Read(junk)
			_, _ = f.Write(junk)
			_ = f.Close()
		},
		"header-zeroed": func(t *testing.T, wal string) {
			f, err := os.OpenFile(wal, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = f.WriteAt(make([]byte, 32), 0)
			_ = f.Close()
		},
		"random-bytes": func(t *testing.T, wal string) {
			fi, _ := os.Stat(wal)
			b := make([]byte, fi.Size())
			_, _ = rand.Read(b)
			if err := os.WriteFile(wal, b, 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	names := make([]string, 0, len(damage))
	for n := range damage {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, path := crashAfterOpen(t)
			damage[name](t, path+"-wal")
			mainBefore := fileHash(t, path)
			var f facts
			res, err := fast().inspect(path, factsProbe(&f))
			view := res.View
			if err != nil {
				// An error is acceptable only as an inspection failure.
				exactly(t, err, ErrInspectionFailed)
				return
			}
			// SQLite ignores frames that fail their checksums: the damage
			// shows up as missing data (never as extra data), which a caller
			// must treat as inconclusive while a -wal exists.
			if !res.WALPresent {
				t.Fatalf("a damaged -wal was present but not reported (%+v)", res)
			}
			if view != ViewWALAware || f.tables > full.tables {
				t.Fatalf("view=%v tables=%d (intact state: %d)", view, f.tables, full.tables)
			}
			if fileHash(t, path) != mainBefore {
				t.Fatal("the main database file changed")
			}
		})
	}
}

// ---- stale and torn views (the race that blocked the first Step 2) -------------------

// A writer that opens, writes, checkpoints and closes entirely between the
// immutable read and the verification used to leave a handle whose cached
// schema was stale. The result must now be discarded and the attempt restarted,
// so the LAST probe invocation sees the writer's table.
func TestInspect_StaleNegative_WriterCompletesDuringImmutableProbe_Restarts(t *testing.T) {
	t.Parallel()
	path := foreignDB(t)
	in := fast()
	fired := 0
	in.afterImmutableProbe = func() {
		if fired == 0 {
			fired++
			otherWriter(t, path, `CREATE TABLE marker(a)`, `INSERT INTO marker VALUES(1)`)
		}
	}
	var f facts
	res, err := in.inspect(path, factsProbe(&f))
	view := res.View
	if err != nil {
		t.Fatal(err)
	}
	if fired != 1 {
		t.Fatalf("hook fired %d times", fired)
	}
	if f.marker != 1 {
		t.Fatalf("STALE NEGATIVE: the returned result missed the writer's table (names=%q, probe calls=%d)", f.names, f.calls)
	}
	if f.calls < 2 {
		t.Fatalf("the stale first result must have been discarded by a restart; probe calls=%d", f.calls)
	}
	if view != ViewImmutable {
		t.Fatalf("after the writer finished the file is quiescent again; view = %v", view)
	}
}

// Same, but the file size does not change (a table rename), so only the
// modification time can reveal the write.
func TestInspect_StaleNegative_SameSizeSchemaChange_Restarts(t *testing.T) {
	t.Parallel()
	path := foreignDB(t)
	in := fast()
	fired := 0
	in.afterImmutableProbe = func() {
		if fired == 0 {
			fired++
			otherWriter(t, path, `ALTER TABLE t RENAME TO u`)
		}
	}
	var f facts
	if _, err := in.inspect(path, factsProbe(&f)); err != nil {
		t.Fatal(err)
	}
	if f.names != "u" || f.calls < 2 {
		t.Fatalf("STALE NEGATIVE: tables=%q (want \"u\"), probe calls=%d", f.names, f.calls)
	}
}

// If the file never stops changing, the result is an inspection failure, never
// a stale answer and never a not-SQLite/empty classification.
func TestInspect_FileKeepsChanging_FailsWithBoundedRestarts(t *testing.T) {
	t.Parallel()
	path := foreignDB(t)
	in := fast()
	in.restarts = 3
	in.afterImmutableProbe = func() { otherWriter(t, path, `CREATE TABLE IF NOT EXISTS zz(a)`, `INSERT INTO zz VALUES(1)`) }
	var f facts
	res, err := in.inspect(path, factsProbe(&f))
	view := res.View
	exactly(t, err, ErrInspectionFailed)
	if view != 0 {
		t.Fatalf("a failed inspection must not report a view, got %v", view)
	}
	if f.calls != 3 {
		t.Fatalf("probe calls = %d, want exactly the 3 bounded attempts", f.calls)
	}
	if !strings.Contains(err.Error(), "kept changing") {
		t.Fatalf("the error should say the file kept changing: %v", err)
	}
}

func TestInspect_SidecarsAppearDuringImmutableProbe_RestartsAsWALAware(t *testing.T) {
	t.Parallel()
	_, path := cleanInspectDB(t, false)
	in := fast()
	fired := 0
	in.afterImmutableProbe = func() {
		if fired == 0 {
			fired++
			w := rawWriter(t, path) // stays open: its sidecars persist
			if _, err := w.Exec(insertProjectSQL("pw")); err != nil {
				t.Error(err)
			}
		}
	}
	res, f := mustInspect(t, in, path)
	view := res.View
	if view != ViewWALAware || f.projects != 1 {
		t.Fatalf("view=%v projects=%d: the discarded immutable result must be replaced by the WAL-aware one", view, f.projects)
	}
	if !res.WALPresent || !res.SHMPresent {
		t.Fatalf("the winning (WAL-aware) attempt saw the writer's sidecars, got %+v", res)
	}
}

func TestInspect_SidecarsAppearBeforeImmutableRead_UsesWALAware(t *testing.T) {
	t.Parallel()
	_, path := cleanInspectDB(t, false)
	in := fast()
	fired := 0
	in.afterSidecarCheck = func() { // a writer starts after the "no sidecars" decision
		if fired == 0 {
			fired++
			w := rawWriter(t, path)
			if _, err := w.Exec(insertProjectSQL("pw")); err != nil {
				t.Error(err)
			}
		}
	}
	res, f := mustInspect(t, in, path)
	view := res.View
	if view != ViewWALAware || f.projects != 1 {
		t.Fatalf("view=%v projects=%d, want the WAL-resident row via the WAL-aware view", view, f.projects)
	}
	if !res.WALPresent || !res.SHMPresent {
		t.Fatalf("the writer's sidecars were present before the database was opened, got %+v", res)
	}
}

func TestInspect_SidecarsDisappearDuringInspection(t *testing.T) {
	t.Parallel()
	_, path := cleanInspectDB(t, false)
	w := rawWriter(t, path)
	if _, err := w.Exec(insertProjectSQL("pw")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path + "-wal"); err != nil {
		t.Fatalf("precondition: writer sidecars should exist: %v", err)
	}
	in := fast()
	in.afterSidecarCheck = func() { _ = w.Close() } // last connection closes: checkpoint, sidecars removed
	res, f := mustInspect(t, in, path)
	view := res.View
	if view != ViewWALAware || f.projects != 1 {
		t.Fatalf("view=%v projects=%d, want the row the writer committed before closing", view, f.projects)
	}
	// The sidecars were gone by the end, but they were observed when the
	// winning attempt began: that evidence must be kept.
	if !res.WALPresent || !res.SHMPresent {
		t.Fatalf("sidecars observed earlier in the attempt were dropped from the result: %+v", res)
	}
}

// In the WAL-aware view the whole probe is one read transaction: a commit made
// by another connection in the middle of the probe must not be visible to it.
func TestInspect_WALAware_ProbeSeesOneSnapshot(t *testing.T) {
	t.Parallel()
	_, path := cleanInspectDB(t, false)
	w := rawWriter(t, path)
	if _, err := w.Exec(insertProjectSQL("p-first")); err != nil {
		t.Fatal(err)
	}
	var first, second int
	res, err := fast().inspect(path, func(q Querier) error {
		if err := q.QueryRowContext(bg, `SELECT count(*) FROM projects`).Scan(&first); err != nil {
			return err
		}
		if _, err := w.Exec(insertProjectSQL("p-during")); err != nil { // commits mid-probe
			return err
		}
		return q.QueryRowContext(bg, `SELECT count(*) FROM projects`).Scan(&second)
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.View != ViewWALAware {
		t.Fatalf("view = %v", res.View)
	}
	if first != second || first != 1 {
		t.Fatalf("not a snapshot: first read %d, second read %d (want both 1)", first, second)
	}
}

func TestInspect_PathReplacedAfterHeaderCheck_RestartsAndReadsTheNewFile(t *testing.T) {
	t.Parallel()
	_, path := cleanInspectDB(t, false)
	_, other := cleanInspectDB(t, true)
	in := fast()
	fired := 0
	in.afterHeader = func() {
		if fired == 0 {
			fired++
			if err := os.Rename(other, path); err != nil {
				t.Error(err)
			}
		}
	}
	var f facts
	res, err := in.inspect(path, factsProbe(&f))
	view := res.View
	if err != nil {
		t.Fatal(err)
	}
	if view != ViewImmutable || f.projects != 1 || f.calls < 2 {
		t.Fatalf("view=%v projects=%d calls=%d: the replaced path must be re-read from the file gate", view, f.projects, f.calls)
	}
}

func TestInspect_PathKeepsBeingReplaced_FailsAsInspectionFailed(t *testing.T) {
	t.Parallel()
	_, path := cleanInspectDB(t, false)
	spares := make([]string, 4)
	for i := range spares {
		_, spares[i] = cleanInspectDB(t, true)
	}
	in := fast()
	in.restarts = 3
	n := 0
	in.afterHeader = func() {
		if n < len(spares) {
			if err := os.Rename(spares[n], path); err != nil {
				t.Error(err)
			}
			n++
		}
	}
	var f facts
	_, err := in.inspect(path, factsProbe(&f))
	exactly(t, err, ErrInspectionFailed)
	if !strings.Contains(err.Error(), "kept changing") {
		t.Fatalf("the error should say the file kept changing: %v", err)
	}
}

// ---- probe errors and concurrency ------------------------------------------------------

var errProbeSentinel = errors.New("probe sentinel")

func TestInspect_ProbeError_IsInspectionFailedAndKeepsTheCause(t *testing.T) {
	t.Parallel()
	_, path := cleanInspectDB(t, false)
	calls := 0
	_, err := fast().inspect(path, func(Querier) error { calls++; return errProbeSentinel })
	if !errors.Is(err, errProbeSentinel) {
		t.Fatalf("the probe's own error must stay reachable: %v", err)
	}
	exactly(t, err, ErrInspectionFailed)
	// An immutable-view probe error falls back to the WAL-aware view, so the
	// probe ran in both (documented side effect: that view may create sidecars).
	if calls != 2 {
		t.Fatalf("probe calls = %d, want 2 (immutable, then WAL-aware)", calls)
	}
}

// runConcurrentInspections inspects a database while a separate process writes
// to it and checkpoints. Every call must either return a correct view or fail
// with exactly ErrInspectionFailed; a wrong answer or any other category fails.
func runConcurrentInspections(t *testing.T, mode string, rounds int) {
	_, path := cleanInspectDB(t, true)
	syncDir := t.TempDir()
	c := runInspectHelper(t, path, mode, "DSO_TEST_INSPECT_SYNC="+syncDir)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	t.Cleanup(func() {
		_ = os.WriteFile(filepath.Join(syncDir, "stop"), []byte("stop"), 0o600)
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			_ = c.Process.Kill()
			<-done
		}
	})
	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(syncDir, "ready")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("writer helper never became ready")
		}
		time.Sleep(5 * time.Millisecond)
	}
	ok, failed := 0, 0
	byView := map[InspectionView]int{}
	for i := 0; i < rounds; i++ {
		var f facts
		res, err := newInspector().inspect(path, factsProbe(&f))
		view := res.View
		if err != nil {
			exactly(t, err, ErrInspectionFailed)
			failed++
			continue
		}
		if !f.hasProjects || f.projects != 1 {
			t.Fatalf("round %d: WRONG ANSWER (view %v): hasProjects=%v projects=%d, want the projects table with 1 row", i, view, f.hasProjects, f.projects)
		}
		byView[view]++
		ok++
	}
	t.Logf("%s writer: %d inspections succeeded (immutable=%d wal-aware=%d), %d failed with ErrInspectionFailed",
		mode, ok, byView[ViewImmutable], byView[ViewWALAware], failed)
	if ok == 0 {
		t.Fatal("no inspection succeeded")
	}
}

func TestInspect_ConcurrentLongLivedWriterWithCheckpoints(t *testing.T) {
	t.Parallel()
	runConcurrentInspections(t, "writer", 400)
}

func TestInspect_ConcurrentWriterOpeningAndClosing(t *testing.T) {
	t.Parallel()
	runConcurrentInspections(t, "cycle", 400)
}

// ---- bounded locking -------------------------------------------------------------------

// Not parallel: it asserts a wall-clock window.
func TestInspect_LockedDatabase_FailsClosedWithinBoundedTime(t *testing.T) {
	_, path := cleanInspectDB(t, true)
	w := rawWriter(t, path)
	if _, err := w.Exec(`PRAGMA locking_mode=EXCLUSIVE`); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Exec(insertProjectSQL("pw")); err != nil { // takes and keeps the exclusive lock
		t.Fatal(err)
	}
	in := newInspector()
	in.busyTimeout, in.attempts, in.retryDelay = 40*time.Millisecond, 3, 30*time.Millisecond
	start := time.Now()
	var f facts
	_, err := in.inspect(path, factsProbe(&f))
	elapsed := time.Since(start)
	exactly(t, err, ErrInspectionFailed)
	// 3 attempts: at least the two inter-attempt delays, and well under a
	// generous bound for the three busy timeouts.
	if elapsed < 60*time.Millisecond || elapsed > 5*time.Second {
		t.Fatalf("elapsed %v is outside the bounded retry window", elapsed)
	}
	// Releasing the lock makes the same call succeed.
	_ = w.Close()
	_, f2 := mustInspect(t, fast(), path)
	if f2.projects != 2 {
		t.Fatalf("projects = %d, want 2 after the lock was released", f2.projects)
	}
}

func TestInspect_TransientClassification(t *testing.T) {
	t.Parallel()
	if isTransientInspectError(errors.New("disk image is malformed")) {
		t.Fatal("a malformed-image error must not be treated as transient")
	}
	if !isTransientInspectError(errors.New("database is locked (5) (SQLITE_BUSY)")) {
		t.Fatal("a busy error must be treated as transient")
	}
}

// ---- read-only filesystem --------------------------------------------------------------

// inspectOnReadOnlyMount runs the inspection inside a user+mount namespace in
// which dir is remounted read-only (the securefs file gate is replaced there; see
// the "inspect" helper mode). Where such a namespace cannot be created the test
// is skipped with the reason (an environmental limitation, not a pass).
func inspectOnReadOnlyMount(t *testing.T, dir, path string) string {
	t.Helper()
	if _, err := exec.LookPath("unshare"); err != nil {
		t.Skip("read-only filesystem test needs unshare(1): not available")
	}
	script := fmt.Sprintf("mount --bind '%s' '%s' && mount -o remount,ro,bind,nosuid,nodev '%s' '%s' && exec '%s' -test.run='^TestHelperProcessInspect$'",
		dir, dir, dir, dir, os.Args[0])
	c := exec.Command("unshare", "-Urm", "sh", "-c", script) // #nosec G204 -- fixed program, test-controlled paths
	c.Env = append(os.Environ(), "DSO_TEST_INSPECT_DB="+path, "DSO_TEST_INSPECT_MODE=inspect", "DSO_TEST_INSPECT_NO_SECUREFS=1")
	out, _ := c.CombinedOutput()
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "RESULT:") {
			return strings.TrimPrefix(line, "RESULT:")
		}
	}
	t.Skipf("cannot create a read-only mount namespace in this environment: %s", strings.TrimSpace(string(out)))
	return ""
}

func TestInspect_ReadOnlyFilesystem(t *testing.T) {
	t.Parallel()
	t.Run("clean-no-sidecars-immutable-works", func(t *testing.T) {
		t.Parallel()
		dir, path := cleanInspectDB(t, true)
		before := snapDir(t, dir)
		got := inspectOnReadOnlyMount(t, dir, path)
		if !strings.HasPrefix(got, "OK view=immutable ") {
			t.Fatalf("want a successful immutable inspection, got %q", got)
		}
		if !sameSnap(before, snapDir(t, dir)) {
			t.Fatal("files changed")
		}
	})
	t.Run("both-sidecars-wal-aware-view-works", func(t *testing.T) {
		t.Parallel()
		dir, path := crashAfterOpen(t)
		before := snapDir(t, dir)
		got := inspectOnReadOnlyMount(t, dir, path)
		if !strings.HasPrefix(got, "OK view=wal-aware ") {
			t.Fatalf("want a successful WAL-aware inspection, got %q", got)
		}
		if !sameSnap(before, snapDir(t, dir)) {
			t.Fatal("files changed")
		}
	})
	t.Run("only-wal-fails-closed", func(t *testing.T) {
		t.Parallel()
		dir, path := crashAfterOpen(t)
		if err := os.Remove(path + "-shm"); err != nil {
			t.Fatal(err)
		}
		before := snapDir(t, dir)
		got := inspectOnReadOnlyMount(t, dir, path)
		if !strings.HasPrefix(got, "FAILED ") {
			t.Fatalf("want ErrInspectionFailed (SQLite cannot create -shm on a read-only mount), got %q", got)
		}
		if !sameSnap(before, snapDir(t, dir)) {
			t.Fatal("files changed")
		}
	})
}

// ---- sidecar flags and the signature (InspectionResult contract) ----------------------

// SQLite creates an empty -wal when a read-only connection opens a WAL database
// that has none. That file must not be reported as evidence of a WAL that
// existed before the inspection.
func TestInspect_EmptyWALCreatedByInspection_IsNotPreexistingEvidence(t *testing.T) {
	t.Parallel()
	_, path := cleanInspectDB(t, true)
	writeFile0600(t, path+"-shm", make([]byte, 32768))
	if _, err := os.Lstat(path + "-wal"); !os.IsNotExist(err) {
		t.Fatalf("precondition: no -wal may exist yet (%v)", err)
	}
	res, _ := mustInspect(t, fast(), path)
	fi, err := os.Lstat(path + "-wal")
	if err != nil {
		t.Fatalf("precondition: SQLite should have created an empty -wal during the read: %v", err)
	}
	if fi.Size() != 0 {
		t.Fatalf("the -wal SQLite created is %d bytes, expected an empty file", fi.Size())
	}
	if res.WALPresent {
		t.Fatalf("an empty -wal created by the inspection itself was reported as pre-existing WAL evidence: %+v", res)
	}
	if res.View != ViewWALAware || !res.SHMPresent {
		t.Fatalf("unexpected result %+v", res)
	}
}

// An immutable probe that fails without any observed change falls back to the
// WAL-aware view. The sidecars that read creates are not earlier evidence.
func TestInspect_ImmutableProbeFailure_FallsBackWithoutPreexistingSidecars(t *testing.T) {
	t.Parallel()
	_, path := cleanInspectDB(t, true)
	calls := 0
	res, err := fast().inspect(path, func(q Querier) error {
		calls++
		if calls == 1 {
			return errProbeSentinel // the immutable attempt fails
		}
		var n int
		return q.QueryRowContext(bg, `SELECT count(*) FROM projects`).Scan(&n)
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || res.View != ViewWALAware {
		t.Fatalf("calls=%d view=%v: want the immutable attempt, then the WAL-aware fallback", calls, res.View)
	}
	if res.WALPresent || res.SHMPresent {
		t.Fatalf("no sidecar existed before the inspection, got %+v", res)
	}
	if _, err := os.Lstat(path + "-shm"); err != nil {
		t.Fatalf("precondition: the WAL-aware read should have created an -shm that the flags ignore: %v", err)
	}
}

// Sidecar observations belong to the attempt that produced the result. An
// attempt that is discarded (sidecars appeared during its probe) must not leak
// its flags into the winning attempt, which here finds no sidecars at all.
func TestInspect_DiscardedAttemptSidecarFlags_DoNotLeak(t *testing.T) {
	t.Parallel()
	_, path := cleanInspectDB(t, false)
	in := fast()
	var w *sql.DB
	headers, fired := 0, 0
	in.afterImmutableProbe = func() {
		if fired == 0 {
			fired++
			w = rawWriter(t, path)
			if _, err := w.Exec(insertProjectSQL("pw")); err != nil {
				t.Error(err)
			}
		}
	}
	in.afterHeader = func() {
		headers++
		if headers == 2 && w != nil {
			if err := w.Close(); err != nil { // checkpoint: the sidecars vanish
				t.Error(err)
			}
		}
	}
	res, f := mustInspect(t, in, path)
	if headers < 2 {
		t.Fatalf("the first attempt was not restarted (headers=%d)", headers)
	}
	if res.View != ViewImmutable || res.WALPresent || res.SHMPresent {
		t.Fatalf("flags from the discarded attempt leaked into the winning one: %+v", res)
	}
	if f.projects != 1 {
		t.Fatalf("projects = %d, want the writer's committed row", f.projects)
	}
}

// A damaged -wal that hides the expected schema must be flagged, so a caller
// can treat the missing schema as inconclusive.
func TestInspect_WALPresentWithExpectedSchemaMissing_IsFlagged(t *testing.T) {
	t.Parallel()
	_, path := crashAfterOpen(t)
	fi, _ := os.Stat(path + "-wal")
	junk := make([]byte, fi.Size())
	_, _ = rand.Read(junk)
	if err := os.WriteFile(path+"-wal", junk, 0o600); err != nil {
		t.Fatal(err)
	}
	res, f := mustInspect(t, fast(), path)
	if f.hasProjects {
		t.Fatal("precondition: the expected schema must be missing")
	}
	if res.View != ViewWALAware || !res.WALPresent {
		t.Fatalf("a missing schema with a -wal present must be flagged WALPresent, got %+v", res)
	}
}

// Same size, schema renamed, mtime restored: only ctime moves.
func TestInspect_StaleNegative_SameSizeRestoredMtime_DetectedByCtime(t *testing.T) {
	t.Parallel()
	if !ctimeAvailable {
		t.Skip("ctime is not available on this platform (see inspect_ctime_other.go): the immutable signature falls back to size and modification time")
	}
	path := foreignDB(t)
	in := fast()
	fired := 0
	var before, after statSig
	in.afterImmutableProbe = func() {
		if fired != 0 {
			return
		}
		fired++
		fi, err := os.Lstat(path)
		if err != nil {
			t.Error(err)
			return
		}
		before = statSigOf(fi)
		otherWriter(t, path, `ALTER TABLE t RENAME TO u`)
		if err := os.Chtimes(path, fi.ModTime(), fi.ModTime()); err != nil { // restore the original mtime
			t.Error(err)
		}
		fi2, err := os.Lstat(path)
		if err != nil {
			t.Error(err)
			return
		}
		after = statSigOf(fi2)
	}
	var f facts
	res, err := in.inspect(path, factsProbe(&f))
	if err != nil {
		t.Fatal(err)
	}
	if before.size != after.size || before.mtimeNs != after.mtimeNs {
		t.Fatalf("precondition: size and mtime must be identical (before %+v, after %+v)", before, after)
	}
	if before.ctimeNs == after.ctimeNs {
		t.Fatalf("precondition: ctime must have moved (before %+v, after %+v)", before, after)
	}
	if f.names != "u" || f.calls < 2 {
		t.Fatalf("STALE NEGATIVE: tables=%q (want \"u\"), probe calls=%d: ctime did not cause a restart", f.names, f.calls)
	}
	if res.View != ViewImmutable {
		t.Fatalf("view = %v", res.View)
	}
}

func TestInspect_StatSignatureFields(t *testing.T) {
	t.Parallel()
	_, path := cleanInspectDB(t, false)
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	sig := statSigOf(fi)
	if sig.size != fi.Size() || sig.mtimeNs != fi.ModTime().UnixNano() {
		t.Fatalf("size/mtime not captured: %+v", sig)
	}
	if ctimeAvailable {
		if !sig.platform || sig.dev == 0 || sig.ino == 0 || sig.ctimeNs == 0 {
			t.Fatalf("device, inode and ctime must be captured where available: %+v", sig)
		}
	} else if sig.platform || sig.ctimeNs != 0 {
		t.Fatalf("platform fields must be absent off Linux: %+v", sig)
	}
}
