package cli

// D3: `dso central backup` refuses every database that is not an initialized
// (or adoptable) DSO Central database, BEFORE sqlite.Open can migrate it, before
// an audit event can be appended to it, and before any destination is created.
// The fixtures and assertion helpers come from central_uninitialized_test.go.

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/centralsvc"
	"github.com/docker-secret-operator/dso/internal/crypto"
	"github.com/docker-secret-operator/dso/internal/store"
	"github.com/docker-secret-operator/dso/internal/store/sqlite"
)

// ubOut is a backup destination whose parent directory does not exist, so a
// refusal that created the destination directory would be visible.
func ubOut(e *centralEnv) string { return filepath.Join(e.dir, "backups", "new", "out.db") }

func ubBackup(t *testing.T, e *centralEnv, out string) (string, error) {
	t.Helper()
	stdout, _, err := e.run(t, "", append([]string{"backup", "--out", out}, e.file()...)...)
	return stdout, err
}

// ubRefusal runs `backup` and checks the properties common to every refusal.
// If sidecarFree is set the whole environment directory (including the lock
// sidecar: a refused backup must not create one) must be byte-identical
// afterwards; otherwise the main database file and an existing -wal must be, and
// the set of lock sidecars must not have changed.
func ubRefusal(t *testing.T, e *centralEnv, sidecarFree bool) error {
	t.Helper()
	out := ubOut(e)
	before := unSnap(t, e.dir)
	mainBefore := unFileHash(t, e.db)
	walBefore := ""
	if _, err := os.Stat(e.db + "-wal"); err == nil {
		walBefore = unFileHash(t, e.db+"-wal")
	}
	_, lockBefore := before[filepath.Join(filepath.Base(filepath.Dir(e.db)), filepath.Base(e.db)+".central.lock")]

	stdout, err := ubBackup(t, e, out)
	if err == nil {
		t.Fatalf("expected a refusal, got success: %q", stdout)
	}
	if stdout != "" {
		t.Fatalf("a refused backup must print nothing, got %q", stdout)
	}
	if exitCodeFor(err) != 1 {
		t.Fatalf("exit status = %d, want 1", exitCodeFor(err))
	}
	if _, statErr := os.Lstat(out); !os.IsNotExist(statErr) {
		t.Fatalf("a backup file was produced (%v)", statErr)
	}
	if _, statErr := os.Lstat(filepath.Join(e.dir, "backups")); !os.IsNotExist(statErr) {
		t.Fatalf("the destination directory was created (%v)", statErr)
	}
	if sidecarFree {
		if after := unSnap(t, e.dir); !unSame(before, after) {
			t.Fatalf("a refused backup changed files:\nbefore %v\nafter  %v", before, after)
		}
	} else {
		if unFileHash(t, e.db) != mainBefore {
			t.Fatal("the main database file changed")
		}
		if walBefore != "" && unFileHash(t, e.db+"-wal") != walBefore {
			t.Fatal("the -wal file changed")
		}
		if _, statErr := os.Lstat(e.db + ".central.lock"); (statErr == nil) != lockBefore {
			t.Fatalf("a refused backup changed the lock sidecar (existed before: %v)", lockBefore)
		}
	}
	// The error must be exactly what CheckInitialized returns: unchanged.
	if want := centralsvc.CheckInitialized(e.db); want == nil || want.Error() != err.Error() {
		t.Fatalf("the CLI altered the check's error:\n cli:   %v\n check: %v", err, want)
	}
	return err
}

// unExclusiveWriter holds an exclusive lock on the database until it is closed.
func unExclusiveWriter(t *testing.T, path string) *sql.DB {
	t.Helper()
	w, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
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
	return w
}

func ubInitialised(t *testing.T) *centralEnv {
	t.Helper()
	e := newCentralEnv(t)
	if _, _, err := e.run(t, "", append([]string{"init"}, e.file()...)...); err != nil {
		t.Fatal(err)
	}
	return e
}

// ---- state matrix -----------------------------------------------------------------------

func TestCentralBackupUninitialized_StateMatrix(t *testing.T) {
	t.Parallel()

	t.Run("zero-byte-database", func(t *testing.T) {
		t.Parallel()
		e := unEnv(t)
		unWrite(t, e.db, nil)
		err := ubRefusal(t, e, true)
		ni := unNotInit(t, err, centralsvc.ReasonEmptyFile)
		if !errors.Is(err, sqlite.ErrEmptyDatabase) || len(ni.Guidance) != 0 {
			t.Fatalf("unexpected: %+v", ni)
		}
	})
	t.Run("garbage-file", func(t *testing.T) {
		t.Parallel()
		e := unEnv(t)
		unWrite(t, e.db, []byte(strings.Repeat("not a database\n", 30)))
		err := ubRefusal(t, e, true) // before D3 this created a .central.lock
		unNotInit(t, err, centralsvc.ReasonNotSQLite)
		if !errors.Is(err, sqlite.ErrNotSQLite) {
			t.Fatalf("the file-level cause must stay reachable: %v", err)
		}
	})
	t.Run("foreign-sqlite-rollback-journal", func(t *testing.T) {
		t.Parallel()
		e := unEnv(t)
		unForeign(t, e.db, `CREATE TABLE t(a)`, `INSERT INTO t VALUES(1)`)
		unNotInit(t, ubRefusal(t, e, true), centralsvc.ReasonNoSchema)
	})
	t.Run("foreign-sqlite-already-wal", func(t *testing.T) {
		t.Parallel()
		e := unEnv(t)
		unForeign(t, e.db, `PRAGMA journal_mode=WAL`, `CREATE TABLE t(a)`, `INSERT INTO t VALUES(1)`)
		unNotInit(t, ubRefusal(t, e, true), centralsvc.ReasonNoSchema)
	})
	t.Run("foreign-sqlite-with-its-own-projects-table", func(t *testing.T) {
		t.Parallel()
		e := unEnv(t)
		unForeign(t, e.db, `CREATE TABLE projects(x)`)
		// Before D3 this exited 1 but still converted the file to WAL and added
		// a schema_migrations table.
		unNotInit(t, ubRefusal(t, e, true), centralsvc.ReasonNoSchema)
	})
	t.Run("uninitialized-dso-schema-without-audit-row", func(t *testing.T) {
		t.Parallel()
		e := unEnv(t)
		unOpenOnly(t, e.db, false)
		ni := unNotInit(t, ubRefusal(t, e, true), centralsvc.ReasonNoBindingNoProjects)
		unPreserveFirst(t, strings.Join(ni.Guidance, "\n"), e.db)
	})
	t.Run("uninitialized-dso-schema-with-audit-row", func(t *testing.T) {
		t.Parallel()
		e := unEnv(t)
		unOpenOnly(t, e.db, true)
		ni := unNotInit(t, ubRefusal(t, e, true), centralsvc.ReasonNoBindingNoProjects)
		if len(ni.Guidance) != 0 {
			t.Fatalf("a non-empty table must disable recovery guidance: %+v", ni)
		}
	})
	t.Run("partial-binding", func(t *testing.T) {
		t.Parallel()
		e := unEnv(t)
		if _, _, err := e.run(t, "", append([]string{"init", "--use-existing-key"}, e.file()...)...); err != nil {
			t.Fatal(err)
		}
		unEdit(t, e.db, `DROP TRIGGER store_meta_no_delete`, `DELETE FROM store_meta WHERE key = 'binding.canary'`)
		eventsBefore := len(auditEventsAt(t, e.db))
		err := ubRefusal(t, e, true)
		if !errors.Is(err, centralsvc.ErrBindingCorrupt) {
			t.Fatalf("want ErrBindingCorrupt, got %v", err)
		}
		var ni *centralsvc.NotInitializedError
		var ic *centralsvc.InconclusiveError
		if errors.As(err, &ni) || errors.As(err, &ic) {
			t.Fatalf("a corrupt binding is returned directly: %v", err)
		}
		// No exception for a corrupt binding: no audit event was appended to it.
		if got := len(auditEventsAt(t, e.db)); got != eventsBefore {
			t.Fatalf("an audit event was appended to the corrupt database (%d -> %d)", eventsBefore, got)
		}
	})
	t.Run("invalid-binding-version", func(t *testing.T) {
		t.Parallel()
		e := unEnv(t)
		if _, _, err := e.run(t, "", append([]string{"init", "--use-existing-key"}, e.file()...)...); err != nil {
			t.Fatal(err)
		}
		unEdit(t, e.db, `DROP TRIGGER store_meta_no_update`, `UPDATE store_meta SET value = x'32' WHERE key = 'binding.version'`)
		if err := ubRefusal(t, e, true); !errors.Is(err, centralsvc.ErrBindingCorrupt) {
			t.Fatalf("want ErrBindingCorrupt, got %v", err)
		}
	})
	t.Run("newer-schema", func(t *testing.T) {
		t.Parallel()
		e := unEnv(t)
		if _, _, err := e.run(t, "", append([]string{"init", "--use-existing-key"}, e.file()...)...); err != nil {
			t.Fatal(err)
		}
		unEdit(t, e.db, `INSERT INTO schema_migrations(version, applied_at) VALUES (99, 0)`)
		if err := ubRefusal(t, e, true); !errors.Is(err, store.ErrSchemaTooNew) {
			t.Fatalf("want store.ErrSchemaTooNew, got %v", err)
		}
	})
	t.Run("interrupted-init-wal-resident-schema", func(t *testing.T) {
		t.Parallel()
		e := unEnv(t)
		unCrash(t, e.db)
		err := ubRefusal(t, e, false)
		var ic *centralsvc.InconclusiveError
		if !errors.As(err, &ic) || !errors.Is(err, sqlite.ErrInspectionFailed) || errors.Is(err, centralsvc.ErrNotInitialized) {
			t.Fatalf("want *InconclusiveError, got %T: %v", err, err)
		}
		if !ic.WALPresent {
			t.Fatal("a non-empty -wal was present")
		}
		unPreserveFirst(t, strings.Join(ic.Guidance, "\n"), e.db)
	})
	t.Run("damaged-wal", func(t *testing.T) {
		t.Parallel()
		e := unEnv(t)
		unCrash(t, e.db)
		fi, _ := os.Stat(e.db + "-wal")
		junk := make([]byte, fi.Size())
		_, _ = rand.Read(junk)
		unWrite(t, e.db+"-wal", junk)
		err := ubRefusal(t, e, false)
		var ic *centralsvc.InconclusiveError
		if !errors.As(err, &ic) || errors.Is(err, centralsvc.ErrNotInitialized) || !ic.WALPresent || len(ic.Guidance) != 0 {
			t.Fatalf("a damaged WAL hiding the schema must be inconclusive without guidance, got %T: %v", err, err)
		}
	})
}

func TestCentralBackupUninitialized_LockedDatabaseIsInconclusive(t *testing.T) {
	t.Parallel()
	e := ubInitialised(t)
	w := unExclusiveWriter(t, e.db)
	start := time.Now()
	_, err := ubBackup(t, e, ubOut(e))
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
	if _, statErr := os.Lstat(filepath.Join(e.dir, "backups")); !os.IsNotExist(statErr) {
		t.Fatalf("the destination directory was created (%v)", statErr)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ubBackup(t, e, ubOut(e)); err != nil {
		t.Fatalf("after the lock is released the initialized database must back up: %v", err)
	}
}

// ---- positive controls ---------------------------------------------------------------------

func TestCentralBackupUninitialized_InitializedDatabaseStillBacksUp(t *testing.T) {
	t.Parallel()
	e := ubInitialised(t)
	out := filepath.Join(e.dir, "backups", "out.db")
	stdout, err := ubBackup(t, e, out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "Backup written to "+out) || !strings.Contains(stdout, "root key") {
		t.Fatalf("unexpected output: %q", stdout)
	}
	if _, statErr := os.Stat(out); statErr != nil {
		t.Fatal(statErr)
	}
	n := 0
	for _, ev := range auditEventsAt(t, e.db) {
		if ev.EventType == store.AuditBackup && ev.Outcome == store.AuditSuccess {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("expected exactly one backup success event in the active database, got %d", n)
	}
	for _, ev := range auditEventsAt(t, out) {
		if ev.EventType == store.AuditBackup {
			t.Fatal("the backup must not describe its own creation")
		}
	}
}

func TestCentralBackupUninitialized_LegacySchema1WithProject_StillMigratesAndBacksUp(t *testing.T) {
	t.Parallel()
	e := unEnv(t) // the key exists
	kp, err := crypto.NewFileKeyProvider(e.key)
	if err != nil {
		t.Fatal(err)
	}
	all, err := sqlite.EmbeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	old, err := sqlite.Open(t.Context(), e.db, sqlite.Options{Migrations: all[:1]}) // schema 1 only
	if err != nil {
		t.Fatal(err)
	}
	aad, err := crypto.DEKAAD("p-old", kp.ID(), 1)
	if err != nil {
		t.Fatal(err)
	}
	dek, _ := crypto.GenerateKey()
	wrapped, err := kp.Wrap(t.Context(), dek, aad)
	if err != nil {
		t.Fatal(err)
	}
	if err := old.CreateProject(t.Context(), store.Project{ID: "p-old", Slug: "legacy", WrappedDEK: wrapped, DEKKeyID: kp.ID()}); err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	if err := centralsvc.CheckInitialized(e.db); err != nil {
		t.Fatalf("precondition: a legacy database with a project is adoptable: %v", err)
	}
	out := filepath.Join(e.dir, "backups", "out.db")
	if _, err := ubBackup(t, e, out); err != nil {
		t.Fatalf("a valid legacy database must still back up: %v", err)
	}
	if _, statErr := os.Stat(out); statErr != nil {
		t.Fatal(statErr)
	}
	// Legacy migration is unchanged: the live database was migrated, with the
	// documented pre-migration copy.
	if m, _ := filepath.Glob(e.db + ".pre-migration-v1-*.bak"); len(m) != 1 {
		t.Fatalf("expected one pre-migration copy, got %v", m)
	}
	st, err := sqlite.Open(t.Context(), e.db, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if v, err := st.SchemaVersion(t.Context()); err != nil || v != len(all) {
		t.Fatalf("schema version = %d (%v), want %d", v, err, len(all))
	}
}

func TestCentralBackupUninitialized_WipedBindingWithProject_IsAllowedThroughTheCheck(t *testing.T) {
	t.Parallel()
	e := newCentralEnv(t)
	for _, a := range [][]string{{"init"}, {"project", "create", "app"}} {
		if _, _, err := e.run(t, "", append(a, e.file()...)...); err != nil {
			t.Fatal(err)
		}
	}
	unEdit(t, e.db, `DROP TRIGGER store_meta_no_update`, `DROP TRIGGER store_meta_no_delete`, `DELETE FROM store_meta WHERE key LIKE 'binding.%'`)
	out := filepath.Join(e.dir, "backups", "out.db")
	if _, err := ubBackup(t, e, out); err != nil {
		t.Fatalf("an adoptable database must still back up: %v", err)
	}
	if _, statErr := os.Stat(out); statErr != nil {
		t.Fatal(statErr)
	}
}

// ---- recovery regressions -----------------------------------------------------------------------

func TestCentralBackupUninitialized_ZeroByteLeftover_RefusedThenInitStillWorks(t *testing.T) {
	t.Parallel()
	e := newCentralEnv(t)
	if err := os.Mkdir(filepath.Dir(e.db), 0o700); err != nil {
		t.Fatal(err)
	}
	unWrite(t, e.db, nil)
	unNotInit(t, ubRefusal(t, e, true), centralsvc.ReasonEmptyFile)
	// Before D3 the backup would have turned this reusable leftover into a
	// non-empty file that init refuses.
	out, _, err := e.run(t, "", append([]string{"init"}, e.file()...)...)
	if err != nil || !strings.Contains(out, "Initialised") {
		t.Fatalf("init must still reuse the untouched leftover: %v %q", err, out)
	}
	if _, err := ubBackup(t, e, filepath.Join(e.dir, "backups", "after-init.db")); err != nil {
		t.Fatalf("the initialized database must back up: %v", err)
	}
}

func TestCentralBackupUninitialized_InterruptedInit_FilesPreservedAndRecoveryStillWorks(t *testing.T) {
	t.Parallel()
	e := unEnv(t)
	unCrash(t, e.db)
	orig := map[string][]byte{}
	for _, sfx := range []string{"", "-wal", "-shm"} {
		b, err := os.ReadFile(e.db + sfx)
		if err != nil {
			t.Fatal(err)
		}
		orig[sfx] = b
	}
	err := ubRefusal(t, e, false)
	var ic *centralsvc.InconclusiveError
	if !errors.As(err, &ic) {
		t.Fatalf("want an inconclusive refusal, got %T: %v", err, err)
	}
	// The three files all still exist, and the main file and the -wal (the
	// only places the schema lives) are byte-identical. The -shm is SQLite's
	// shared wal-index: the read-only inspection may legitimately rewrite it, so
	// it is required to exist but not to be identical (see inspect.go).
	for _, sfx := range []string{"", "-wal", "-shm"} {
		if _, statErr := os.Stat(e.db + sfx); statErr != nil {
			t.Fatalf("%s%s is gone: %v", filepath.Base(e.db), sfx, statErr)
		}
	}
	for _, sfx := range []string{"", "-wal"} {
		got, _ := os.ReadFile(e.db + sfx)
		if string(got) != string(orig[sfx]) {
			t.Fatalf("%s%s is no longer byte-identical", filepath.Base(e.db), sfx)
		}
	}
	// The documented preserve-first recovery still succeeds.
	saved := filepath.Join(e.dir, "saved")
	if err := os.Mkdir(saved, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, sfx := range []string{"", "-wal", "-shm"} {
		unWrite(t, filepath.Join(saved, "c.db"+sfx), orig[sfx])
	}
	for _, sfx := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(e.db + sfx); err != nil {
			t.Fatal(err)
		}
	}
	out, _, err := e.run(t, "", append([]string{"init", "--use-existing-key"}, e.file()...)...)
	if err != nil || !strings.Contains(out, "Initialised") {
		t.Fatalf("init --use-existing-key after the manual recovery: %v %q", err, out)
	}
	if _, err := ubBackup(t, e, filepath.Join(e.dir, "backups", "recovered.db")); err != nil {
		t.Fatalf("the recovered database must back up: %v", err)
	}
}
