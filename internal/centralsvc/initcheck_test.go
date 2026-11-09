package centralsvc

// Tests for CheckInitialized (see initcheck.go). Fixtures come from the real
// sqlite.Open, Service/Bind and store code wherever possible; crash states come
// from helper processes that SIGKILL themselves so the WAL is left exactly as an
// interrupted process leaves it. Every refusal asserts its exact error category
// and that the files were not modified.

import (
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

	"github.com/docker-secret-operator/dso/internal/crypto"
	"github.com/docker-secret-operator/dso/internal/securefs/securefstest"
	"github.com/docker-secret-operator/dso/internal/store"
	"github.com/docker-secret-operator/dso/internal/store/sqlite"
)

// ---- helper process ------------------------------------------------------------

// TestHelperProcessInitCheck is not a real test: it runs only when
// DSO_TEST_INITCHECK_DB is set. open-kill runs the real sqlite.Open (schema
// applied, nothing else) and bind-kill additionally runs Service.Bind; both then
// SIGKILL themselves without Close or checkpoint.
func TestHelperProcessInitCheck(t *testing.T) {
	path := os.Getenv("DSO_TEST_INITCHECK_DB")
	if path == "" {
		t.Skip("helper process")
	}
	st, err := sqlite.Open(ctx, path, sqlite.Options{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "initcheck helper: open:", err)
		os.Exit(2)
	}
	if os.Getenv("DSO_TEST_INITCHECK_MODE") == "bind-kill" {
		kp, err := crypto.NewFileKeyProvider(os.Getenv("DSO_TEST_INITCHECK_KEY"))
		if err != nil {
			fmt.Fprintln(os.Stderr, "initcheck helper: key:", err)
			os.Exit(2)
		}
		if err := New(st, kp, nil).Bind(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "initcheck helper: bind:", err)
			os.Exit(2)
		}
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
	select {}
}

// icCrash builds a crash-state database with the real code and returns its path.
func icCrash(t *testing.T, mode string) string {
	t.Helper()
	dir := securefstest.TempDir(t)
	path := filepath.Join(dir, "c.db")
	keyPath := filepath.Join(securefstest.TempDir(t), "root.key")
	if _, err := crypto.InstallKeyFile(keyPath); err != nil {
		t.Fatal(err)
	}
	c := exec.Command(os.Args[0], "-test.run=^TestHelperProcessInitCheck$") // #nosec G204 -- this test binary
	c.Env = append(os.Environ(), "DSO_TEST_INITCHECK_DB="+path, "DSO_TEST_INITCHECK_MODE="+mode, "DSO_TEST_INITCHECK_KEY="+keyPath)
	c.Stderr = os.Stderr
	err := c.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("helper %s: expected it to be killed, got %v", mode, err)
	}
	if ws, ok := ee.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
		t.Fatalf("helper %s: expected SIGKILL, got %v", mode, err)
	}
	for _, sfx := range []string{"", "-wal", "-shm"} {
		if _, err := os.Lstat(path + sfx); err != nil {
			t.Fatalf("crash fixture is missing %s%s: %v", path, sfx, err)
		}
	}
	if fi, _ := os.Stat(path); fi.Size() >= 16384 {
		t.Fatalf("main file is %d bytes; the schema should be WAL-resident", fi.Size())
	}
	return path
}

// ---- fixtures ---------------------------------------------------------------------

// icUnbound: the real Open ran and closed cleanly; the DSO schema is there, no
// binding, no data, no sidecars.
func icUnbound(t *testing.T) string {
	t.Helper()
	e := setup(t)
	if err := e.st.Close(); err != nil {
		t.Fatal(err)
	}
	icNoSidecars(t, e.dbPath)
	return e.dbPath
}

// icBound: the real Bind (and optionally a project with an environment) ran and
// the store was closed cleanly.
func icBound(t *testing.T, withProject bool) *env {
	t.Helper()
	e := setup(t)
	if withProject {
		e.project(t, "app", "prod")
	} else if err := e.svc.Bind(ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.st.Close(); err != nil {
		t.Fatal(err)
	}
	icNoSidecars(t, e.dbPath)
	return e
}

func icNoSidecars(t *testing.T, path string) {
	t.Helper()
	for _, sfx := range []string{"-wal", "-shm"} {
		if _, err := os.Lstat(path + sfx); !os.IsNotExist(err) {
			t.Fatalf("%s%s must not exist (%v)", path, sfx, err)
		}
	}
}

// icEdit opens path read-write, runs stmts, checkpoints and closes (sidecars
// vanish), simulating an attacker or an earlier tool with write access.
func icEdit(t *testing.T, path string, stmts ...string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%q: %v", s, err)
		}
	}
	if _, err := db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

const icDropMetaTriggers1 = `DROP TRIGGER store_meta_no_update`
const icDropMetaTriggers2 = `DROP TRIGGER store_meta_no_delete`

// icForeign is a non-DSO SQLite file (rollback journal, mode 0600).
func icForeign(t *testing.T, stmts ...string) string {
	t.Helper()
	path := filepath.Join(securefstest.TempDir(t), "c.db")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%q: %v", s, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func icHashes(t *testing.T, dir string) map[string]string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range ents {
		if e.Type()&os.ModeSymlink != 0 {
			tgt, _ := os.Readlink(filepath.Join(dir, e.Name()))
			out[e.Name()] = "link->" + tgt
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

func icFileHash(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%d:%s", len(b), hex.EncodeToString(sum[:6]))
}

func icSameDir(a, b map[string]string) bool {
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

// icCheckNoMutation runs CheckInitialized and fails if any file in the database
// directory changed (used where no sidecars exist, so nothing may be touched).
func icCheck(t *testing.T, path string) error {
	t.Helper()
	before := icHashes(t, filepath.Dir(path))
	err := CheckInitialized(path)
	if after := icHashes(t, filepath.Dir(path)); !icSameDir(before, after) {
		t.Fatalf("CheckInitialized changed files:\nbefore %v\nafter  %v", before, after)
	}
	return err
}

// ---- assertions ---------------------------------------------------------------------

func wantNotInit(t *testing.T, err error) *NotInitializedError {
	t.Helper()
	var ni *NotInitializedError
	if !errors.As(err, &ni) {
		t.Fatalf("want *NotInitializedError, got %T: %v", err, err)
	}
	if !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("errors.Is(err, ErrNotInitialized) must hold: %v", err)
	}
	if errors.Is(err, sqlite.ErrInspectionFailed) || errors.Is(err, ErrBindingCorrupt) || errors.Is(err, store.ErrSchemaTooNew) {
		t.Fatalf("a not-initialized finding must not match any other category: %v", err)
	}
	return ni
}

func wantInconclusive(t *testing.T, err error) *InconclusiveError {
	t.Helper()
	var ic *InconclusiveError
	if !errors.As(err, &ic) {
		t.Fatalf("want *InconclusiveError, got %T: %v", err, err)
	}
	if !errors.Is(err, sqlite.ErrInspectionFailed) {
		t.Fatalf("errors.Is(err, sqlite.ErrInspectionFailed) must hold: %v", err)
	}
	if errors.Is(err, ErrNotInitialized) {
		t.Fatalf("an inconclusive inspection must NEVER match ErrNotInitialized: %v", err)
	}
	return ic
}

func wantNeither(t *testing.T, err error) {
	t.Helper()
	var ni *NotInitializedError
	var ic *InconclusiveError
	if errors.As(err, &ni) || errors.As(err, &ic) || errors.Is(err, ErrNotInitialized) || errors.Is(err, sqlite.ErrInspectionFailed) {
		t.Fatalf("this outcome must be returned directly, not as a not-initialized or inconclusive error: %v", err)
	}
}

// wantPreserveFirst checks the exact shape of the recovery guidance.
func wantPreserveFirst(t *testing.T, guidance []string, path string) {
	t.Helper()
	if len(guidance) == 0 {
		t.Fatal("expected preserve-first recovery guidance")
	}
	text := strings.Join(guidance, "\n")
	last := -1
	for _, want := range []string{"never deletes or modifies", "1. Stop every process", "2. Copy " + path + ", " + path + "-wal, " + path + "-shm", "3. Verify the copies", "4. Only then"} {
		i := strings.Index(text, want)
		if i < 0 || i < last {
			t.Fatalf("guidance is missing %q, or out of order:\n%s", want, text)
		}
		last = i
	}
	if !strings.Contains(text, "--use-existing-key") {
		t.Fatalf("guidance must name the manual re-init command:\n%s", text)
	}
}

// ---- files that are not databases -----------------------------------------------------

func TestCheckInitialized_ZeroByteFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(securefstest.TempDir(t), "c.db")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ni := wantNotInit(t, icCheck(t, path))
	if ni.Reason != ReasonEmptyFile || !errors.Is(ni, sqlite.ErrEmptyDatabase) || len(ni.Guidance) != 0 {
		t.Fatalf("unexpected: %+v", ni)
	}
}

func TestCheckInitialized_GarbageFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(securefstest.TempDir(t), "c.db")
	if err := os.WriteFile(path, []byte(strings.Repeat("not a database\n", 30)), 0o600); err != nil {
		t.Fatal(err)
	}
	ni := wantNotInit(t, icCheck(t, path))
	if ni.Reason != ReasonNotSQLite || !errors.Is(ni, sqlite.ErrNotSQLite) || len(ni.Guidance) != 0 {
		t.Fatalf("unexpected: %+v", ni)
	}
}

func TestCheckInitialized_NoDSOSchema_Variants(t *testing.T) {
	t.Parallel()
	migTable := `CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`
	cases := map[string][]string{
		"empty-sqlite":           {`CREATE TABLE x(a)`, `DROP TABLE x`},
		"foreign-sqlite":         {`CREATE TABLE t(a)`, `INSERT INTO t VALUES(1)`},
		"migrations-table-only":  {migTable, `INSERT INTO schema_migrations VALUES(1, 0)`},
		"foreign-projects-table": {migTable, `CREATE TABLE projects(x)`, `CREATE TABLE environments(a)`, `CREATE TABLE secrets(a)`, `CREATE TABLE secret_versions(a)`},
	}
	for name, stmts := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ni := wantNotInit(t, icCheck(t, icForeign(t, stmts...)))
			if ni.Reason != ReasonNoSchema || len(ni.Guidance) != 0 {
				t.Fatalf("unexpected: %+v", ni)
			}
			if !ni.Retry {
				t.Fatal("a no-sidecar finding comes from the immutable view and must carry retry guidance")
			}
			if !strings.Contains(ni.Error(), "retry") {
				t.Fatalf("the message must tell the caller to retry: %q", ni.Error())
			}
		})
	}
}

func TestCheckInitialized_SecurefsRefusalsPassThroughUnchanged(t *testing.T) {
	t.Parallel()
	t.Run("symlink", func(t *testing.T) {
		path := icUnbound(t)
		link := filepath.Join(filepath.Dir(path), "link.db")
		if err := os.Symlink(path, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		err := CheckInitialized(link)
		if err == nil || !strings.Contains(err.Error(), "symbolic link") {
			t.Fatalf("want the securefs symlink refusal, got %v", err)
		}
		wantNeither(t, err)
	})
	t.Run("unsafe-mode", func(t *testing.T) {
		path := icUnbound(t)
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		err := CheckInitialized(path)
		if err == nil || !strings.Contains(err.Error(), "not safe to use") {
			t.Fatalf("want the securefs refusal, got %v", err)
		}
		wantNeither(t, err)
	})
}

// ---- initialized and adoptable ---------------------------------------------------------

func TestCheckInitialized_CleanBoundDatabase_NoSideEffects(t *testing.T) {
	t.Parallel()
	e := icBound(t, false)
	if err := icCheck(t, e.dbPath); err != nil {
		t.Fatalf("a bound database must be initialized: %v", err)
	}
}

func TestCheckInitialized_BoundWithProject(t *testing.T) {
	t.Parallel()
	e := icBound(t, true)
	if err := icCheck(t, e.dbPath); err != nil {
		t.Fatal(err)
	}
}

func TestCheckInitialized_LiveWAL_CompleteBindingFoundInWAL_IsInitialized(t *testing.T) {
	t.Parallel()
	e := setup(t)
	if err := e.svc.Bind(ctx); err != nil { // the store stays open: its sidecars are live
		t.Fatal(err)
	}
	if _, err := os.Lstat(e.dbPath + "-wal"); err != nil {
		t.Fatalf("precondition: a live WAL must exist: %v", err)
	}
	if err := CheckInitialized(e.dbPath); err != nil {
		t.Fatalf("a binding that exists only in a live WAL is still initialized: %v", err)
	}
}

func TestCheckInitialized_CrashAfterBind_BindingOnlyInWAL_IsInitialized(t *testing.T) {
	t.Parallel()
	path := icCrash(t, "bind-kill")
	mainBefore, walBefore := icFileHash(t, path), icFileHash(t, path+"-wal")
	if err := CheckInitialized(path); err != nil {
		t.Fatalf("a crash after Bind leaves a complete binding in the WAL; that is initialized: %v", err)
	}
	if icFileHash(t, path) != mainBefore || icFileHash(t, path+"-wal") != walBefore {
		t.Fatal("the main file or the -wal changed")
	}
}

func TestCheckInitialized_LegacySchema1WithProject_IsAdoptable(t *testing.T) {
	t.Parallel()
	dir := securefstest.TempDir(t)
	dbPath := filepath.Join(dir, "old.db")
	keyPath := filepath.Join(securefstest.TempDir(t), "root.key")
	if _, err := crypto.InstallKeyFile(keyPath); err != nil {
		t.Fatal(err)
	}
	kp, _ := crypto.NewFileKeyProvider(keyPath)
	all, _ := sqlite.EmbeddedMigrations()
	old, err := sqlite.Open(ctx, dbPath, sqlite.Options{Migrations: all[:1]}) // schema 1 only
	if err != nil {
		t.Fatal(err)
	}
	dek, _ := crypto.GenerateKey()
	wrapped, _ := kp.Wrap(ctx, dek, mustAAD(t)(crypto.DEKAAD("p-old", kp.ID(), 1)))
	if err := old.CreateProject(ctx, store.Project{ID: "p-old", Slug: "legacy", WrappedDEK: wrapped, DEKKeyID: kp.ID()}); err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	if err := icCheck(t, dbPath); err != nil {
		t.Fatalf("a legacy schema-1 database with a project is adoptable: %v", err)
	}
	// Ownership is Bind's job: a foreign key is refused there, not here.
	st, err := sqlite.Open(ctx, dbPath, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := New(st, otherKey(t), nil).ListProjects(ctx); !errors.Is(err, ErrWrongRootKey) {
		t.Fatalf("Bind must refuse a foreign key: %v", err)
	}
}

func TestCheckInitialized_LegacySchema1WithoutProjects_NoGuidance(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(securefstest.TempDir(t), "old.db")
	all, _ := sqlite.EmbeddedMigrations()
	old, err := sqlite.Open(ctx, dbPath, sqlite.Options{Migrations: all[:1]})
	if err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	ni := wantNotInit(t, icCheck(t, dbPath))
	if ni.Reason != ReasonNoBindingNoProjects || len(ni.Guidance) != 0 {
		t.Fatalf("an incomplete (schema 1) table set must not get recovery guidance: %+v", ni)
	}
}

func TestCheckInitialized_WipedBindingWithProject_IsAdoptable_BindDecidesOwnership(t *testing.T) {
	t.Parallel()
	e := icBound(t, true)
	icEdit(t, e.dbPath, icDropMetaTriggers1, icDropMetaTriggers2, `DELETE FROM store_meta WHERE key LIKE 'binding.%'`)
	if err := icCheck(t, e.dbPath); err != nil {
		t.Fatalf("a wiped binding with a project is adoptable: %v", err)
	}
	e.reopen(t)
	if _, err := New(e.st, otherKey(t), nil).ListProjects(ctx); !errors.Is(err, ErrWrongRootKey) {
		t.Fatalf("the wrong key must be refused by Bind: %v", err)
	}
	if _, err := e.svc.ListProjects(ctx); err != nil {
		t.Fatalf("the owner key must re-establish the binding: %v", err)
	}
}

func TestCheckInitialized_MalformedProjectDEK_CheckPasses_BindRefuses(t *testing.T) {
	t.Parallel()
	e := icBound(t, true)
	icEdit(t, e.dbPath, icDropMetaTriggers1, icDropMetaTriggers2,
		`DELETE FROM store_meta WHERE key LIKE 'binding.%'`, `UPDATE projects SET wrapped_dek = randomblob(60)`)
	if err := icCheck(t, e.dbPath); err != nil {
		t.Fatalf("the check does not verify DEKs (non-empty key data is well formed): %v", err)
	}
	e.reopen(t)
	if _, err := e.svc.ListProjects(ctx); !errors.Is(err, ErrWrongRootKey) {
		t.Fatalf("Bind must refuse a project whose key data does not unwrap: %v", err)
	}
	if _, err := e.st.GetMeta(ctx, metaBindingCanary); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a refused Bind must leave no binding: %v", err)
	}
}

// ---- corrupt binding and newer schema: returned directly --------------------------------

func TestCheckInitialized_PartialOrInvalidBinding_IsErrBindingCorrupt(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		"one-of-three-rows":  {`DELETE FROM store_meta WHERE key IN ('binding.canary', 'binding.root_key_id')`},
		"two-of-three-rows":  {`DELETE FROM store_meta WHERE key = 'binding.canary'`},
		"invalid-version":    {`UPDATE store_meta SET value = x'32' WHERE key = 'binding.version'`},
		"empty-version":      {`UPDATE store_meta SET value = x'' WHERE key = 'binding.version'`},
		"two-rows-no-canary": {`DELETE FROM store_meta WHERE key = 'binding.root_key_id'`},
	}
	for name, stmts := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := icBound(t, true)
			icEdit(t, e.dbPath, append([]string{icDropMetaTriggers1, icDropMetaTriggers2}, stmts...)...)
			err := icCheck(t, e.dbPath)
			if !errors.Is(err, ErrBindingCorrupt) {
				t.Fatalf("want ErrBindingCorrupt, got %v", err)
			}
			wantNeither(t, err) // classified after evidence collection, not through the probe
		})
	}
}

func TestCheckInitialized_NewerSchema_IsErrSchemaTooNew(t *testing.T) {
	t.Parallel()
	e := icBound(t, false)
	icEdit(t, e.dbPath, `INSERT INTO schema_migrations(version, applied_at) VALUES (99, 0)`)
	err := icCheck(t, e.dbPath)
	if !errors.Is(err, store.ErrSchemaTooNew) {
		t.Fatalf("want store.ErrSchemaTooNew, got %v", err)
	}
	wantNeither(t, err)
}

// ---- unbound, empty: the interrupted-initialization family ---------------------------------

func TestCheckInitialized_UnboundEmpty_NoWAL_ImmutableNegative_PreserveFirstGuidance(t *testing.T) {
	t.Parallel()
	path := icUnbound(t)
	ni := wantNotInit(t, icCheck(t, path))
	if ni.Reason != ReasonNoBindingNoProjects {
		t.Fatalf("reason = %q", ni.Reason)
	}
	if !ni.Retry || !strings.Contains(ni.Error(), "retry") {
		t.Fatalf("an immutable negative must carry retry guidance: %q", ni.Error())
	}
	wantPreserveFirst(t, ni.Guidance, path)
}

func TestCheckInitialized_UnboundEmpty_WALAwareNoWAL_IsDefinitive(t *testing.T) {
	t.Parallel()
	path := icUnbound(t)
	// A stray -shm forces the WAL-aware view; SQLite then creates an EMPTY -wal,
	// which must not count as WAL evidence.
	if err := os.WriteFile(path+"-shm", make([]byte, 32768), 0o600); err != nil {
		t.Fatal(err)
	}
	mainBefore := icFileHash(t, path)
	ni := wantNotInit(t, CheckInitialized(path))
	if ni.Retry {
		t.Fatal("a WAL-aware snapshot without a WAL is definitive: no retry guidance")
	}
	if fi, err := os.Stat(path + "-wal"); err != nil || fi.Size() != 0 {
		t.Fatalf("precondition: SQLite should have created an empty -wal (%v)", err)
	}
	wantPreserveFirst(t, ni.Guidance, path)
	if icFileHash(t, path) != mainBefore {
		t.Fatal("the main database file changed")
	}
}

func TestCheckInitialized_InterruptedInit_WALPresent_IsInconclusiveWithPreserveFirstGuidance(t *testing.T) {
	t.Parallel()
	path := icCrash(t, "open-kill")
	mainBefore, walBefore := icFileHash(t, path), icFileHash(t, path+"-wal")
	err := CheckInitialized(path)
	ic := wantInconclusive(t, err)
	if !ic.WALPresent {
		t.Fatal("a non-empty -wal was present")
	}
	wantPreserveFirst(t, ic.Guidance, path)
	if !strings.Contains(strings.Join(ic.Guidance, "\n"), "damaged -wal") {
		t.Fatalf("the guidance must not present the database as disposable:\n%s", strings.Join(ic.Guidance, "\n"))
	}
	if icFileHash(t, path) != mainBefore || icFileHash(t, path+"-wal") != walBefore {
		t.Fatal("the main file or the -wal changed")
	}
}

func TestCheckInitialized_DamagedWALHidingSchema_IsInconclusiveWithoutGuidance(t *testing.T) {
	t.Parallel()
	path := icCrash(t, "open-kill")
	fi, _ := os.Stat(path + "-wal")
	junk := make([]byte, fi.Size())
	_, _ = rand.Read(junk)
	if err := os.WriteFile(path+"-wal", junk, 0o600); err != nil {
		t.Fatal(err)
	}
	err := CheckInitialized(path)
	ic := wantInconclusive(t, err)
	if !ic.WALPresent || len(ic.Guidance) != 0 {
		t.Fatalf("a damaged WAL that hides the schema fails the emptiness proof, so there must be no guidance: %+v", ic)
	}
}

func TestCheckInitialized_ForeignTableOrData_PreventsGuidance(t *testing.T) {
	t.Parallel()
	t.Run("foreign-table", func(t *testing.T) {
		t.Parallel()
		path := icUnbound(t)
		icEdit(t, path, `CREATE TABLE extra(a)`)
		ni := wantNotInit(t, icCheck(t, path))
		if ni.Reason != ReasonNoBindingNoProjects || len(ni.Guidance) != 0 {
			t.Fatalf("a foreign table must disable recovery guidance: %+v", ni)
		}
	})
	t.Run("non-empty-audit-table", func(t *testing.T) {
		t.Parallel()
		e := setup(t)
		if _, err := e.st.AppendAuditEvent(ctx, store.AuditEvent{ID: "a1", EventType: store.AuditInit, Outcome: store.AuditSuccess}); err != nil {
			t.Fatal(err)
		}
		if err := e.st.Close(); err != nil {
			t.Fatal(err)
		}
		ni := wantNotInit(t, icCheck(t, e.dbPath))
		if len(ni.Guidance) != 0 {
			t.Fatalf("a non-empty table must disable recovery guidance: %+v", ni)
		}
	})
	t.Run("unexpected-migration-version", func(t *testing.T) {
		t.Parallel()
		path := icUnbound(t)
		icEdit(t, path, `INSERT INTO schema_migrations(version, applied_at) VALUES (2000, 0)`)
		// Version 2000 > latest => ErrSchemaTooNew, not guidance.
		if err := icCheck(t, path); !errors.Is(err, store.ErrSchemaTooNew) {
			t.Fatalf("want store.ErrSchemaTooNew, got %v", err)
		}
	})
	t.Run("version-gap-not-embedded", func(t *testing.T) {
		t.Parallel()
		path := icUnbound(t)
		icEdit(t, path, `DELETE FROM schema_migrations WHERE version = 2`, `INSERT INTO schema_migrations(version, applied_at) VALUES (0, 0)`)
		ni := wantNotInit(t, icCheck(t, path))
		if len(ni.Guidance) != 0 {
			t.Fatalf("a non-embedded migration version must disable recovery guidance: %+v", ni)
		}
	})
}

// ---- inspection failures never become "not initialized" -------------------------------------

func TestCheckInitialized_ProbeSQLFailure_IsInconclusive_AndChangesNothing(t *testing.T) {
	t.Parallel()
	e := icBound(t, false)
	// store_meta exists but is not the DSO table: reading it fails inside the probe.
	icEdit(t, e.dbPath, `DROP TABLE store_meta`, `CREATE TABLE store_meta(x)`)
	// Before this fix the failed probe made InspectDatabase fall back to the
	// WAL-aware view, which created -wal and -shm. Now nothing may change.
	err := icCheck(t, e.dbPath) // fails if ANY file in the directory changes
	ic := wantInconclusive(t, err)
	if ic.WALPresent || len(ic.Guidance) != 0 {
		t.Fatalf("no WAL existed and no guidance may be offered: %+v", ic)
	}
	icNoSidecars(t, e.dbPath)
	if !strings.Contains(ic.Error(), "reading the database failed") {
		t.Fatalf("the reason should say the read failed: %q", ic.Error())
	}
}

func TestCheckInitialized_ProbeSQLFailure_WithLiveWAL_IsInconclusive(t *testing.T) {
	t.Parallel()
	e := setup(t)
	if err := e.svc.Bind(ctx); err != nil { // the store stays open: sidecars are live
		t.Fatal(err)
	}
	db := e.rawDB(t)
	if _, err := db.Exec(`DROP TABLE store_meta`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE store_meta(x)`); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(e.dbPath + "-wal"); err != nil || fi.Size() == 0 {
		t.Fatalf("precondition: a non-empty WAL must exist (%v)", err)
	}
	mainBefore := icFileHash(t, e.dbPath)
	err := CheckInitialized(e.dbPath)
	ic := wantInconclusive(t, err)
	if !ic.WALPresent {
		t.Fatalf("a non-empty -wal was present: %+v", ic)
	}
	if icFileHash(t, e.dbPath) != mainBefore {
		t.Fatal("the main database file changed")
	}
}

func TestCheckInitialized_LockedDatabase_IsInconclusive(t *testing.T) {
	t.Parallel()
	e := icBound(t, false)
	w, err := sql.Open("sqlite", "file:"+e.dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	w.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = w.Close() })
	if _, err := w.Exec(`PRAGMA locking_mode=EXCLUSIVE`); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Exec(`INSERT INTO store_meta(key, value) VALUES ('zz.lock-test', x'01')`); err != nil { // keeps the exclusive lock
		t.Fatal(err)
	}
	start := time.Now()
	err = CheckInitialized(e.dbPath)
	ic := wantInconclusive(t, err)
	elapsed := time.Since(start)
	if elapsed > 30*time.Second {
		t.Fatal("the inspection was not bounded")
	}
	// A busy error must be left to InspectDatabase so its bounded retry runs:
	// up to three attempts, each waiting out (most of) the 1.5 s busy timeout.
	// If the probe swallowed the error there would be one wait of about 1.5 s,
	// so a lower bound well above that proves a retry happened. It is not 4.5 s
	// because SQLite may return early from a wait under load (observed: 3.8 s).
	if elapsed < 2200*time.Millisecond {
		t.Fatalf("only %v elapsed: the transient-lock retries were bypassed", elapsed)
	}
	_ = ic
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := CheckInitialized(e.dbPath); err != nil {
		t.Fatalf("after the lock is released the bound database must be initialized: %v", err)
	}
}

func TestCheckInitialized_InspectionErrorMapping(t *testing.T) {
	t.Parallel()
	failed := func(error) inspectFunc {
		return func(string, func(sqlite.Querier) error) (sqlite.InspectionResult, error) {
			return sqlite.InspectionResult{}, fmt.Errorf("%w: boom", sqlite.ErrInspectionFailed)
		}
	}
	t.Run("inspection-failed-is-inconclusive", func(t *testing.T) {
		wantInconclusive(t, checkInitializedWith(failed(nil), "/x/c.db"))
	})
	t.Run("securefs-style-error-passes-through-identically", func(t *testing.T) {
		sentinel := errors.New("securefs: /x/c.db is a symbolic link; refusing to follow it")
		got := checkInitializedWith(func(string, func(sqlite.Querier) error) (sqlite.InspectionResult, error) {
			return sqlite.InspectionResult{}, sentinel
		}, "/x/c.db")
		if got != sentinel {
			t.Fatalf("a securefs refusal must be returned unchanged, got %#v", got)
		}
	})
	t.Run("empty-and-not-sqlite-keep-their-cause", func(t *testing.T) {
		for _, c := range []struct {
			err    error
			reason InitReason
		}{{sqlite.ErrEmptyDatabase, ReasonEmptyFile}, {sqlite.ErrNotSQLite, ReasonNotSQLite}} {
			got := checkInitializedWith(func(string, func(sqlite.Querier) error) (sqlite.InspectionResult, error) {
				return sqlite.InspectionResult{}, fmt.Errorf("%w: /x/c.db", c.err)
			}, "/x/c.db")
			ni := wantNotInit(t, got)
			if ni.Reason != c.reason || !errors.Is(got, c.err) {
				t.Fatalf("unexpected %+v for %v", ni, c.err)
			}
		}
	})
}

// ---- WAL rule, exercised with synthetic inspection results ------------------------------------

func TestCheckInitialized_NegativeWithWALPresent_NeverNotInitialized(t *testing.T) {
	t.Parallel()
	// A probe result that shows no DSO schema, reported with and without a WAL.
	emptyProbe := func(res sqlite.InspectionResult) inspectFunc {
		return func(path string, probe func(sqlite.Querier) error) (sqlite.InspectionResult, error) {
			db, err := sql.Open("sqlite", "file::memory:")
			if err != nil {
				return sqlite.InspectionResult{}, err
			}
			defer db.Close()
			db.SetMaxOpenConns(1)
			tx, err := db.Begin()
			if err != nil {
				return sqlite.InspectionResult{}, err
			}
			defer tx.Rollback()
			if err := probe(tx); err != nil {
				return sqlite.InspectionResult{}, fmt.Errorf("%w: %w", sqlite.ErrInspectionFailed, err)
			}
			return res, nil
		}
	}
	for name, c := range map[string]struct {
		res      sqlite.InspectionResult
		inconcl  bool
		wantView bool // retry guidance expected
	}{
		"immutable-no-wal":      {sqlite.InspectionResult{View: sqlite.ViewImmutable}, false, true},
		"wal-aware-no-wal":      {sqlite.InspectionResult{View: sqlite.ViewWALAware}, false, false},
		"wal-aware-shm-only":    {sqlite.InspectionResult{View: sqlite.ViewWALAware, SHMPresent: true}, false, false},
		"wal-aware-wal-present": {sqlite.InspectionResult{View: sqlite.ViewWALAware, WALPresent: true, SHMPresent: true}, true, false},
		"immutable-wal-flag":    {sqlite.InspectionResult{View: sqlite.ViewImmutable, WALPresent: true}, true, false},
	} {
		t.Run(name, func(t *testing.T) {
			err := checkInitializedWith(emptyProbe(c.res), "/x/c.db")
			if c.inconcl {
				wantInconclusive(t, err)
				return
			}
			ni := wantNotInit(t, err)
			if ni.Retry != c.wantView {
				t.Fatalf("Retry = %v, want %v", ni.Retry, c.wantView)
			}
		})
	}
}

// ---- sentinel semantics and drift guard ----------------------------------------------------------

func TestErrorTypes_IsSemantics(t *testing.T) {
	t.Parallel()
	ni := error(&NotInitializedError{Path: "p", Reason: ReasonNoSchema, cause: sqlite.ErrNotSQLite})
	ic := error(&InconclusiveError{Path: "p", Reason: "r", cause: errors.New("c")})
	for _, c := range []struct {
		name string
		got  bool
		want bool
	}{
		{"NotInitialized is ErrNotInitialized", errors.Is(ni, ErrNotInitialized), true},
		{"NotInitialized keeps its file-level cause", errors.Is(ni, sqlite.ErrNotSQLite), true},
		{"NotInitialized is not ErrInspectionFailed", errors.Is(ni, sqlite.ErrInspectionFailed), false},
		{"Inconclusive is ErrInspectionFailed", errors.Is(ic, sqlite.ErrInspectionFailed), true},
		{"Inconclusive is NOT ErrNotInitialized", errors.Is(ic, ErrNotInitialized), false},
		{"Inconclusive is not ErrBindingCorrupt", errors.Is(ic, ErrBindingCorrupt), false},
	} {
		if c.got != c.want {
			t.Errorf("%s: got %v", c.name, c.got)
		}
	}
	var gotNI *NotInitializedError
	var gotIC *InconclusiveError
	if !errors.As(fmt.Errorf("wrapped: %w", ni), &gotNI) || !errors.As(fmt.Errorf("wrapped: %w", ic), &gotIC) {
		t.Error("errors.As must find both typed errors through wrapping")
	}
}

func TestDSOTables_MatchAFreshlyMigratedDatabase(t *testing.T) {
	t.Parallel()
	path := filepath.Join(securefstest.TempDir(t), "fresh.db")
	st, err := sqlite.Open(ctx, path, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table'`)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		got = append(got, n)
	}
	rows.Close()
	want := append(append([]string{}, dsoTables...), sqliteSequenceTable)
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("dsoTables has drifted from the migrations:\n got  %v\n want %v", got, want)
	}
	migs, err := sqlite.EmbeddedMigrations()
	if err != nil || len(migs) == 0 {
		t.Fatalf("embedded migrations: %v", err)
	}
}

func TestBindingKeysMatchTheBindingConstants(t *testing.T) {
	t.Parallel()
	want := map[string]bool{metaBindingVersion: true, metaBindingKeyID: true, metaBindingCanary: true}
	if len(bindingKeys) != len(want) {
		t.Fatalf("bindingKeys = %v", bindingKeys)
	}
	for _, k := range bindingKeys {
		if !want[k] {
			t.Fatalf("unexpected binding key %q", k)
		}
	}
}

// ---- probe-error capture: reset, no contamination, classifier -------------------------

// icQuerier returns a read-only transaction on path, standing in for one
// attempt of sqlite.InspectDatabase.
func icQuerier(t *testing.T, path string) sqlite.Querier {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&immutable=1")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(); _ = db.Close() })
	return tx
}

// icAttempts replays the probe once per path, like an inspection that discards
// attempts and runs the probe again; the LAST invocation is the winning one.
func icAttempts(t *testing.T, paths ...string) inspectFunc {
	return func(_ string, probe func(sqlite.Querier) error) (sqlite.InspectionResult, error) {
		for _, p := range paths {
			if err := probe(icQuerier(t, p)); err != nil {
				return sqlite.InspectionResult{}, fmt.Errorf("%w: %w", sqlite.ErrInspectionFailed, err)
			}
		}
		return sqlite.InspectionResult{View: sqlite.ViewWALAware}, nil
	}
}

func TestCheckInitialized_FailedAttemptThenSuccess_DoesNotLeakTheEarlierError(t *testing.T) {
	t.Parallel()
	broken := icBound(t, false)
	icEdit(t, broken.dbPath, `DROP TABLE store_meta`, `CREATE TABLE store_meta(x)`)
	good := icBound(t, false)
	// attempt 1 hits a query failure; attempt 2 (the winner) reads a bound database.
	if err := checkInitializedWith(icAttempts(t, broken.dbPath, good.dbPath), "/x/c.db"); err != nil {
		t.Fatalf("the earlier attempt's error and evidence leaked into the winning attempt: %v", err)
	}
}

func TestCheckInitialized_DiscardedGoodAttempt_DoesNotContaminateTheFailedWinner(t *testing.T) {
	t.Parallel()
	good := icBound(t, false)
	broken := icBound(t, false)
	icEdit(t, broken.dbPath, `DROP TABLE store_meta`, `CREATE TABLE store_meta(x)`)
	// attempt 1 read a complete binding; attempt 2 (the winner) fails to read.
	err := checkInitializedWith(icAttempts(t, good.dbPath, broken.dbPath), "/x/c.db")
	wantInconclusive(t, err) // not nil: the discarded attempt's evidence must not stand in
}

func TestCheckInitialized_FailedAttemptThenNegative_ReportsOnlyTheWinner(t *testing.T) {
	t.Parallel()
	broken := icBound(t, false)
	icEdit(t, broken.dbPath, `DROP TABLE store_meta`, `CREATE TABLE store_meta(x)`)
	unbound := icUnbound(t)
	// The winner is a plain negative: it must be reported as not initialized,
	// not as the earlier attempt's inconclusive read failure.
	ni := wantNotInit(t, checkInitializedWith(icAttempts(t, broken.dbPath, unbound), "/x/c.db"))
	if ni.Reason != ReasonNoBindingNoProjects {
		t.Fatalf("reason = %q", ni.Reason)
	}
}

type codedError struct{ code int }

func (e codedError) Error() string { return fmt.Sprintf("sqlite error %d", e.code) }
func (e codedError) Code() int     { return e.code }

func TestLeaveToInspection_Classifier(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		err   error
		leave bool
	}{
		{"SQLITE_ERROR is captured by the probe", codedError{1}, false},
		{"extended SQLITE_ERROR is captured", codedError{1 | 1<<8}, false},
		{"non-SQLite error (scan mismatch) is captured", errors.New("sql: Scan error on column index 0: converting NULL to int is unsupported"), false},
		{"SQLITE_BUSY is left to the bounded retry", codedError{5}, true},
		{"extended SQLITE_BUSY_RECOVERY is left to the bounded retry", codedError{5 | 1<<8}, true},
		{"SQLITE_LOCKED is left to the bounded retry", codedError{6}, true},
		{"SQLITE_CORRUPT keeps the immutable-to-WAL-aware fallback", codedError{11}, true},
		{"SQLITE_NOTADB is left to the inspection", codedError{26}, true},
		{"SQLITE_IOERR is left to the inspection", codedError{10}, true},
		{"SQLITE_CANTOPEN is left to the inspection", codedError{14}, true},
		{"wrapped SQLITE_BUSY is still left to the retry", fmt.Errorf("query: %w", codedError{5}), true},
	} {
		if got := leaveToInspection(c.err); got != c.leave {
			t.Errorf("%s: leaveToInspection = %v, want %v", c.name, got, c.leave)
		}
	}
}

// A real SQLite "no such table" error must be classified as capturable, and a
// real busy error must not be, so the classifier matches the driver's actual
// error values and not just the fakes above.
func TestLeaveToInspection_RealDriverErrors(t *testing.T) {
	t.Parallel()
	path := icUnbound(t)
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&immutable=1")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, qerr := db.Query(`SELECT * FROM no_such_table`)
	if qerr == nil || leaveToInspection(qerr) {
		t.Fatalf("a real 'no such table' error must be captured by the probe: %v", qerr)
	}

	bound := icBound(t, false)
	w, err := sql.Open("sqlite", "file:"+bound.dbPath+"?_pragma=busy_timeout(100)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	w.SetMaxOpenConns(1)
	defer w.Close()
	if _, err := w.Exec(`PRAGMA locking_mode=EXCLUSIVE`); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Exec(`INSERT INTO store_meta(key, value) VALUES ('zz.x', x'01')`); err != nil {
		t.Fatal(err)
	}
	r, err := sql.Open("sqlite", "file:"+bound.dbPath+"?mode=ro&_pragma=busy_timeout(100)")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	_, berr := r.Query(`SELECT count(*) FROM store_meta`)
	if berr == nil || !leaveToInspection(berr) {
		t.Fatalf("a real busy error must be left to InspectDatabase's retry: %v", berr)
	}
}
