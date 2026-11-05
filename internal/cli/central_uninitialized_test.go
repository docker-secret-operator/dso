package cli

// Step 4: every command routed through centralFlags.open requires an
// INITIALIZED database, not merely an existing file. These tests drive the real
// CLI against the real fixtures (sqlite.Open, `init`, helper processes that
// SIGKILL themselves) and assert, for every refusal: the exact error category,
// the message, exit status 1, that the error is returned unchanged from
// centralsvc.CheckInitialized, that nothing on disk changed, and that no key
// provider (and therefore no prompt) was reached.

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/centralsvc"
	"github.com/docker-secret-operator/dso/internal/store"
	"github.com/docker-secret-operator/dso/internal/store/sqlite"
)

// ---- helper process ------------------------------------------------------------

// TestHelperProcessCLIUninit is not a real test: with DSO_TEST_CLI_UNINIT_DB set
// it runs the real sqlite.Open (schema applied, nothing else) and then SIGKILLs
// itself without Close or checkpoint, leaving the WAL as an interrupted
// `dso central init` leaves it.
func TestHelperProcessCLIUninit(t *testing.T) {
	path := os.Getenv("DSO_TEST_CLI_UNINIT_DB")
	if path == "" {
		t.Skip("helper process")
	}
	if _, err := sqlite.Open(t.Context(), path, sqlite.Options{}); err != nil {
		fmt.Fprintln(os.Stderr, "cli uninit helper: open:", err)
		os.Exit(2)
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
	select {}
}

// ---- fixtures ---------------------------------------------------------------------

// unEnv is an environment with a valid root key on disk (from a real `init`) and
// no database file, in an existing 0700 data directory.
func unEnv(t *testing.T) *centralEnv { return keyedEnv(t, false) }

func unWrite(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func unForeign(t *testing.T, path string, stmts ...string) {
	t.Helper()
	unWrite(t, path, nil)
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
}

// unEdit runs stmts on an existing database, checkpoints and closes it so no
// sidecars remain (simulating an earlier tool or an attacker with write access).
func unEdit(t *testing.T, path string, stmts ...string) {
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

// unOpenOnly: the real sqlite.Open ran and closed cleanly: the DSO schema, no
// binding, no data, no sidecars (provably empty).
func unOpenOnly(t *testing.T, path string, appendAudit bool) {
	t.Helper()
	st, err := sqlite.Open(t.Context(), path, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if appendAudit {
		if _, err := st.AppendAuditEvent(t.Context(), store.AuditEvent{ID: "a1", EventType: store.AuditInit, Outcome: store.AuditSuccess}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
}

// unCrash leaves the schema only in a non-empty WAL, as a SIGKILL during init does.
func unCrash(t *testing.T, path string) {
	t.Helper()
	c := exec.Command(os.Args[0], "-test.run=^TestHelperProcessCLIUninit$") // #nosec G204 -- this test binary
	c.Env = append(os.Environ(), "DSO_TEST_CLI_UNINIT_DB="+path)
	c.Stderr = os.Stderr
	err := c.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("expected the helper to be killed, got %v", err)
	}
	if ws, ok := ee.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
		t.Fatalf("expected SIGKILL, got %v", err)
	}
	for _, sfx := range []string{"", "-wal", "-shm"} {
		if _, err := os.Lstat(path + sfx); err != nil {
			t.Fatalf("crash fixture is missing %s%s: %v", path, sfx, err)
		}
	}
	if fi, _ := os.Stat(path); fi.Size() >= 16384 {
		t.Fatalf("main file is %d bytes; the schema should be WAL-resident", fi.Size())
	}
}

// unSnap hashes every file under dir (recursively; symlinks by target).
func unSnap(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		switch {
		case d.IsDir():
			out[rel] = "dir"
		case d.Type()&fs.ModeSymlink != 0:
			tgt, _ := os.Readlink(p)
			out[rel] = "link->" + tgt
		default:
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(b)
			out[rel] = fmt.Sprintf("%d:%s", len(b), hex.EncodeToString(sum[:6]))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func unSame(a, b map[string]string) bool {
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

func unFileHash(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%d:%s", len(b), hex.EncodeToString(sum[:6]))
}

// unList runs `project list` against e.db.
func unList(t *testing.T, e *centralEnv) (string, error) {
	t.Helper()
	out, _, err := e.run(t, "", append([]string{"project", "list"}, e.file()...)...)
	return out, err
}

// unRefusal runs `project list` and checks the properties common to every
// refusal; it returns the error. If sidecarFree is set the whole environment
// directory must be byte-identical afterwards, otherwise only the main database
// file and an existing -wal must be (SQLite may rewrite -shm for a WAL read).
func unRefusal(t *testing.T, e *centralEnv, sidecarFree bool) error {
	t.Helper()
	before := unSnap(t, e.dir)
	mainBefore := unFileHash(t, e.db)
	walBefore := ""
	if _, err := os.Stat(e.db + "-wal"); err == nil {
		walBefore = unFileHash(t, e.db+"-wal")
	}
	out, err := unList(t, e)
	if err == nil {
		t.Fatalf("expected a refusal, got success: %q", out)
	}
	if out != "" {
		t.Fatalf("a refused command must print nothing, got %q", out)
	}
	if exitCodeFor(err) != 1 {
		t.Fatalf("exit status = %d, want 1", exitCodeFor(err))
	}
	if sidecarFree {
		if after := unSnap(t, e.dir); !unSame(before, after) {
			t.Fatalf("a refused command changed files:\nbefore %v\nafter  %v", before, after)
		}
	} else {
		if unFileHash(t, e.db) != mainBefore {
			t.Fatal("the main database file changed")
		}
		if walBefore != "" && unFileHash(t, e.db+"-wal") != walBefore {
			t.Fatal("the -wal file changed")
		}
	}
	// The error must be exactly what CheckInitialized returns: unchanged.
	if want := centralsvc.CheckInitialized(e.db); want == nil || want.Error() != err.Error() {
		t.Fatalf("the CLI altered the check's error:\n cli:   %v\n check: %v", err, want)
	}
	return err
}

func unNotInit(t *testing.T, err error, reason centralsvc.InitReason) *centralsvc.NotInitializedError {
	t.Helper()
	var ni *centralsvc.NotInitializedError
	if !errors.As(err, &ni) || !errors.Is(err, centralsvc.ErrNotInitialized) {
		t.Fatalf("want *NotInitializedError, got %T: %v", err, err)
	}
	if errors.Is(err, sqlite.ErrInspectionFailed) || errors.Is(err, centralsvc.ErrBindingCorrupt) {
		t.Fatalf("a not-initialized refusal must not match another category: %v", err)
	}
	if ni.Reason != reason {
		t.Fatalf("reason = %q, want %q", ni.Reason, reason)
	}
	if !strings.Contains(err.Error(), "is not an initialized DSO Central database: "+string(reason)) {
		t.Fatalf("unexpected message: %q", err.Error())
	}
	return ni
}

func unPreserveFirst(t *testing.T, text, dbPath string) {
	t.Helper()
	last := -1
	for _, want := range []string{"never deletes or modifies", "1. Stop every process", "2. Copy " + dbPath, "3. Verify the copies", "4. Only then", "--use-existing-key"} {
		i := strings.Index(text, want)
		if i < 0 || i < last {
			t.Fatalf("recovery guidance is missing %q, or out of order:\n%s", want, text)
		}
		last = i
	}
}

// ---- state matrix (project list) -----------------------------------------------------

func TestCentralUninitialized_StateMatrix(t *testing.T) {
	t.Parallel()

	t.Run("zero-byte-file", func(t *testing.T) {
		t.Parallel()
		e := unEnv(t)
		unWrite(t, e.db, nil)
		err := unRefusal(t, e, true)
		ni := unNotInit(t, err, centralsvc.ReasonEmptyFile)
		if !errors.Is(err, sqlite.ErrEmptyDatabase) || len(ni.Guidance) != 0 {
			t.Fatalf("unexpected: %+v", ni)
		}
	})
	t.Run("garbage-file", func(t *testing.T) {
		t.Parallel()
		e := unEnv(t)
		unWrite(t, e.db, []byte(strings.Repeat("not a database\n", 30)))
		err := unRefusal(t, e, true)
		ni := unNotInit(t, err, centralsvc.ReasonNotSQLite)
		if !errors.Is(err, sqlite.ErrNotSQLite) || len(ni.Guidance) != 0 {
			t.Fatalf("unexpected: %+v", ni)
		}
	})
	t.Run("foreign-sqlite", func(t *testing.T) {
		t.Parallel()
		e := unEnv(t)
		unForeign(t, e.db, `CREATE TABLE t(a)`, `INSERT INTO t VALUES(1)`)
		err := unRefusal(t, e, true)
		ni := unNotInit(t, err, centralsvc.ReasonNoSchema)
		if len(ni.Guidance) != 0 || !strings.Contains(err.Error(), "retry") {
			t.Fatalf("an immutable negative must carry retry guidance and no recovery guidance: %+v", ni)
		}
	})
	t.Run("unbound-empty-schema-provably-empty", func(t *testing.T) {
		t.Parallel()
		e := unEnv(t)
		unOpenOnly(t, e.db, false)
		err := unRefusal(t, e, true)
		ni := unNotInit(t, err, centralsvc.ReasonNoBindingNoProjects)
		unPreserveFirst(t, strings.Join(ni.Guidance, "\n"), e.db)
		if !strings.Contains(err.Error(), "retry") {
			t.Fatalf("an immutable negative must say to retry: %q", err.Error())
		}
	})
	t.Run("unbound-with-audit-row-no-guidance", func(t *testing.T) {
		t.Parallel()
		e := unEnv(t)
		unOpenOnly(t, e.db, true)
		ni := unNotInit(t, unRefusal(t, e, true), centralsvc.ReasonNoBindingNoProjects)
		if len(ni.Guidance) != 0 {
			t.Fatalf("a non-empty table must disable recovery guidance: %+v", ni)
		}
	})
	t.Run("partial-binding", func(t *testing.T) {
		t.Parallel()
		e := unEnv(t)
		// A real init gives a complete binding; remove one row.
		if _, _, err := e.run(t, "", append([]string{"init", "--use-existing-key"}, e.file()...)...); err != nil {
			t.Fatal(err)
		}
		unEdit(t, e.db, `DROP TRIGGER store_meta_no_delete`, `DELETE FROM store_meta WHERE key = 'binding.canary'`)
		err := unRefusal(t, e, true)
		if !errors.Is(err, centralsvc.ErrBindingCorrupt) || !strings.Contains(err.Error(), "2 of 3 binding entries") {
			t.Fatalf("want ErrBindingCorrupt (2 of 3), got %v", err)
		}
		var ni *centralsvc.NotInitializedError
		var ic *centralsvc.InconclusiveError
		if errors.As(err, &ni) || errors.As(err, &ic) {
			t.Fatalf("a corrupt binding is returned directly: %v", err)
		}
	})
	t.Run("invalid-binding-version", func(t *testing.T) {
		t.Parallel()
		e := unEnv(t)
		if _, _, err := e.run(t, "", append([]string{"init", "--use-existing-key"}, e.file()...)...); err != nil {
			t.Fatal(err)
		}
		unEdit(t, e.db, `DROP TRIGGER store_meta_no_update`, `UPDATE store_meta SET value = x'32' WHERE key = 'binding.version'`)
		err := unRefusal(t, e, true)
		if !errors.Is(err, centralsvc.ErrBindingCorrupt) || !strings.Contains(err.Error(), "unsupported binding format") {
			t.Fatalf("want ErrBindingCorrupt (unsupported format), got %v", err)
		}
	})
	t.Run("newer-schema", func(t *testing.T) {
		t.Parallel()
		e := unEnv(t)
		if _, _, err := e.run(t, "", append([]string{"init", "--use-existing-key"}, e.file()...)...); err != nil {
			t.Fatal(err)
		}
		unEdit(t, e.db, `INSERT INTO schema_migrations(version, applied_at) VALUES (99, 0)`)
		err := unRefusal(t, e, true)
		if !errors.Is(err, store.ErrSchemaTooNew) {
			t.Fatalf("want store.ErrSchemaTooNew, got %v", err)
		}
	})
	t.Run("interrupted-init-wal-present", func(t *testing.T) {
		t.Parallel()
		e := unEnv(t)
		unCrash(t, e.db)
		err := unRefusal(t, e, false)
		var ic *centralsvc.InconclusiveError
		if !errors.As(err, &ic) || !errors.Is(err, sqlite.ErrInspectionFailed) || errors.Is(err, centralsvc.ErrNotInitialized) {
			t.Fatalf("want *InconclusiveError, got %T: %v", err, err)
		}
		if !ic.WALPresent {
			t.Fatal("a non-empty -wal was present")
		}
		unPreserveFirst(t, strings.Join(ic.Guidance, "\n"), e.db)
		if !strings.Contains(err.Error(), "Do not treat this database as disposable") {
			t.Fatalf("the guidance must not call the database disposable: %q", err.Error())
		}
	})
	t.Run("damaged-wal-hiding-the-schema", func(t *testing.T) {
		t.Parallel()
		e := unEnv(t)
		unCrash(t, e.db)
		fi, _ := os.Stat(e.db + "-wal")
		junk := make([]byte, fi.Size())
		_, _ = rand.Read(junk)
		unWrite(t, e.db+"-wal", junk)
		err := unRefusal(t, e, false)
		var ic *centralsvc.InconclusiveError
		if !errors.As(err, &ic) || errors.Is(err, centralsvc.ErrNotInitialized) {
			t.Fatalf("want *InconclusiveError, got %T: %v", err, err)
		}
		if !ic.WALPresent || len(ic.Guidance) != 0 {
			t.Fatalf("a damaged WAL that hides the schema fails the emptiness proof: %+v", ic)
		}
	})
}

// ---- every command is guarded ---------------------------------------------------------

func TestCentralUninitialized_AllElevenCommandsAreGuarded(t *testing.T) {
	t.Parallel()
	for _, c := range openCmds {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			e := unEnv(t)
			unForeign(t, e.db, `CREATE TABLE t(a)`)
			before := unSnap(t, e.dir)
			stdout, _, err := e.run(t, c.stdin, append(append([]string{}, c.args...), e.file()...)...)
			if err == nil {
				t.Fatalf("%s succeeded against an uninitialized database: %q", c.name, stdout)
			}
			unNotInit(t, err, centralsvc.ReasonNoSchema)
			if stdout != "" || exitCodeFor(err) != 1 {
				t.Fatalf("stdout=%q exit=%d", stdout, exitCodeFor(err))
			}
			if after := unSnap(t, e.dir); !unSame(before, after) {
				t.Fatalf("%s changed files:\nbefore %v\nafter  %v", c.name, before, after)
			}
		})
	}
}

// The check runs before key-provider construction: with the passphrase provider
// and no passphrase file, reaching the provider would fail with a "no terminal"
// prompt error.
func TestCentralUninitialized_CheckPrecedesKeyProviderAndPrompts(t *testing.T) {
	t.Parallel()
	for name, build := range map[string]func(t *testing.T, e *centralEnv){
		"zero-byte":       func(t *testing.T, e *centralEnv) { unWrite(t, e.db, nil) },
		"foreign-sqlite":  func(t *testing.T, e *centralEnv) { unForeign(t, e.db, `CREATE TABLE t(a)`) },
		"unbound-schema":  func(t *testing.T, e *centralEnv) { unOpenOnly(t, e.db, false) },
		"interrupted-wal": func(t *testing.T, e *centralEnv) { unCrash(t, e.db) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := unEnv(t)
			build(t, e)
			_, _, err := e.run(t, "", "project", "list", "--db", e.db, "--key-provider", "passphrase", "--key-file", e.key)
			if err == nil {
				t.Fatal("expected a refusal")
			}
			if strings.Contains(err.Error(), "no terminal") || strings.Contains(err.Error(), "passphrase") {
				t.Fatalf("the key provider was reached before the check: %v", err)
			}
			var ni *centralsvc.NotInitializedError
			var ic *centralsvc.InconclusiveError
			if !errors.As(err, &ni) && !errors.As(err, &ic) {
				t.Fatalf("want a not-initialized or inconclusive refusal, got %T: %v", err, err)
			}
		})
	}
}

// Not parallel with the others' timing assumptions: asserts a lower bound on
// elapsed time that shows the inspection's bounded lock retries still run.
func TestCentralUninitialized_LockedDatabaseIsInconclusive(t *testing.T) {
	t.Parallel()
	e := unEnv(t)
	if _, _, err := e.run(t, "", append([]string{"init", "--use-existing-key"}, e.file()...)...); err != nil {
		t.Fatal(err)
	}
	w, err := sql.Open("sqlite", "file:"+e.db+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	w.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = w.Close() })
	if _, err := w.Exec(`PRAGMA locking_mode=EXCLUSIVE`); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Exec(`INSERT INTO store_meta(key, value) VALUES ('zz.lock-test', x'01')`); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = unList(t, e)
	elapsed := time.Since(start)
	var ic *centralsvc.InconclusiveError
	if !errors.As(err, &ic) || !errors.Is(err, sqlite.ErrInspectionFailed) || errors.Is(err, centralsvc.ErrNotInitialized) {
		t.Fatalf("a locked database must be inconclusive, got %T: %v", err, err)
	}
	if exitCodeFor(err) != 1 {
		t.Fatalf("exit status = %d, want 1", exitCodeFor(err))
	}
	if elapsed < 2200*time.Millisecond || elapsed > 30*time.Second {
		t.Fatalf("elapsed %v is outside the bounded retry window", elapsed)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := unList(t, e); err != nil {
		t.Fatalf("after the lock is released the initialized database must work: %v", err)
	}
}

// ---- initialized controls -----------------------------------------------------------------

func TestCentralInitialized_ControlsStillWork(t *testing.T) {
	t.Parallel()
	t.Run("after-init", func(t *testing.T) {
		t.Parallel()
		e := newCentralEnv(t)
		if _, _, err := e.run(t, "", append([]string{"init"}, e.file()...)...); err != nil {
			t.Fatal(err)
		}
		if _, err := unList(t, e); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("wiped-binding-with-project-is-adopted-by-its-key", func(t *testing.T) {
		t.Parallel()
		e := newCentralEnv(t)
		for _, a := range [][]string{{"init"}, {"project", "create", "app"}} {
			if _, _, err := e.run(t, "", append(a, e.file()...)...); err != nil {
				t.Fatal(err)
			}
		}
		unEdit(t, e.db, `DROP TRIGGER store_meta_no_update`, `DROP TRIGGER store_meta_no_delete`, `DELETE FROM store_meta WHERE key LIKE 'binding.%'`)
		// A different key: the check passes (a project exists) and Bind refuses it.
		other := newCentralEnv(t)
		if _, _, err := other.run(t, "", append([]string{"init"}, other.file()...)...); err != nil {
			t.Fatal(err)
		}
		_, _, err := e.run(t, "", "project", "list", "--db", e.db, "--key-provider", "file", "--key-file", other.key)
		if !errors.Is(err, centralsvc.ErrWrongRootKey) {
			t.Fatalf("Bind must refuse the wrong key: %v", err)
		}
		var ni *centralsvc.NotInitializedError
		if errors.As(err, &ni) {
			t.Fatalf("an adoptable database must not be reported as not initialized: %v", err)
		}
		// The owner key re-establishes the binding.
		out, err := unList(t, e)
		if err != nil || !strings.Contains(out, "app") {
			t.Fatalf("the owner key could not use its database: %v %q", err, out)
		}
	})
}

// ---- init and the other commands are unaffected ----------------------------------------------

func TestCentralInit_ReusesAZeroByteLeftover(t *testing.T) {
	t.Parallel()
	e := newCentralEnv(t)
	if err := os.Mkdir(filepath.Dir(e.db), 0o700); err != nil {
		t.Fatal(err)
	}
	unWrite(t, e.db, nil)
	out, _, err := e.run(t, "", append([]string{"init"}, e.file()...)...)
	if err != nil || !strings.Contains(out, "Initialised") {
		t.Fatalf("init must reuse an empty leftover: %v %q", err, out)
	}
	if _, err := unList(t, e); err != nil {
		t.Fatalf("the database init created must pass the check: %v", err)
	}
}

func TestCentralInit_StillRefusesANonEmptyUninitializedFile(t *testing.T) {
	t.Parallel()
	e := newCentralEnv(t)
	if err := os.Mkdir(filepath.Dir(e.db), 0o700); err != nil {
		t.Fatal(err)
	}
	unForeign(t, e.db, `CREATE TABLE t(a)`)
	_, _, err := e.run(t, "", append([]string{"init"}, e.file()...)...)
	if err == nil || !strings.Contains(err.Error(), "already exists; refusing to initialise over it") {
		t.Fatalf("init must still refuse a non-empty file: %v", err)
	}
}

func TestCentralAuditVerify_ZeroByteFile_StaysUnavailableExit11(t *testing.T) {
	t.Parallel()
	e := newCentralEnv(t)
	if err := os.Mkdir(filepath.Dir(e.db), 0o700); err != nil {
		t.Fatal(err)
	}
	unWrite(t, e.db, nil)
	before := unSnap(t, filepath.Dir(e.db))
	_, _, err := e.run(t, "", "audit", "verify", "--db", e.db)
	if exitCodeFor(err) != exitAuditUnavailable {
		t.Fatalf("exit = %d, want %d: %v", exitCodeFor(err), exitAuditUnavailable, err)
	}
	if after := unSnap(t, filepath.Dir(e.db)); !unSame(before, after) {
		t.Fatal("audit verify changed files")
	}
}

// The documented preserve-first recovery for an interrupted init works end to end.
func TestCentralUninitialized_DocumentedRecoveryWorks(t *testing.T) {
	t.Parallel()
	e := unEnv(t)
	unCrash(t, e.db)
	_, err := unList(t, e)
	var ic *centralsvc.InconclusiveError
	if !errors.As(err, &ic) {
		t.Fatalf("precondition: want an inconclusive refusal, got %v", err)
	}
	// 1-3: (nothing runs) copy the files, verify the copies.
	saved := filepath.Join(e.dir, "saved")
	if err := os.Mkdir(saved, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, sfx := range []string{"", "-wal", "-shm"} {
		b, err := os.ReadFile(e.db + sfx)
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(saved, "c.db"+sfx)
		unWrite(t, dst, b)
		if got, _ := os.ReadFile(dst); string(got) != string(b) {
			t.Fatalf("copy of %s is not identical", sfx)
		}
	}
	// 4: remove the three files (keep the lock sidecar and the key) and re-init.
	for _, sfx := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(e.db + sfx); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(e.db + ".central.lock"); err != nil {
		t.Fatalf("the lock sidecar must have been kept: %v", err)
	}
	out, _, err := e.run(t, "", append([]string{"init", "--use-existing-key"}, e.file()...)...)
	if err != nil || !strings.Contains(out, "Initialised") {
		t.Fatalf("init --use-existing-key after the manual recovery: %v %q", err, out)
	}
	if _, err := unList(t, e); err != nil {
		t.Fatalf("the recovered database must pass the check: %v", err)
	}
}
