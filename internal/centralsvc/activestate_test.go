package centralsvc

// Tests for classifyActive (see initcheck.go). The classifier reuses
// CheckInitialized's inspection and evidence code, so these tests check the
// mapping from that evidence to an activeState, the precedence of binding and
// project evidence over the Foreign finding, that classification never writes
// the database, and that CheckInitialized's own results are unchanged.
// Fixtures come from the helpers in initcheck_test.go.
//
// Byte-identity: where a fixture has no sidecars the whole directory is
// compared before and after. Where a -wal exists the main file and -wal are
// compared; -shm is deliberately NOT required to be byte-identical, because a
// WAL-aware read may rewrite it (the documented exception of the inspection
// contract).

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/securefs/securefstest"
	"github.com/docker-secret-operator/dso/internal/store"
	"github.com/docker-secret-operator/dso/internal/store/sqlite"
)

// acClassify runs classifyActive and fails if any file in the directory changed
// (for fixtures without sidecars).
func acClassify(t *testing.T, path string) (activeState, error) {
	t.Helper()
	before := icHashes(t, filepath.Dir(path))
	st, err := classifyActive(path)
	if after := icHashes(t, filepath.Dir(path)); !icSameDir(before, after) {
		t.Fatalf("classifyActive changed files:\nbefore %v\nafter  %v", before, after)
	}
	return st, err
}

// acWant checks the state and that the returned error is exactly what
// CheckInitialized returns for the same file.
func acWant(t *testing.T, path string, want activeState) error {
	t.Helper()
	got, err := acClassify(t, path)
	if got != want {
		t.Fatalf("state = %v (err %v), want %v", got, err, want)
	}
	legacy := CheckInitialized(path)
	var legacyNI *NotInitializedError
	if got == stateInconclusive && errors.As(legacy, &legacyNI) {
		// An ambiguous file: CheckInitialized keeps its historical not-initialized
		// finding, while classifyActive reports Inconclusive and must not match it.
		if errors.Is(err, ErrNotInitialized) {
			t.Fatalf("an inconclusive classification must not match ErrNotInitialized: %v", err)
		}
		wantInconclusive(t, err)
		return err
	}
	if fmt.Sprint(legacy) != fmt.Sprint(err) {
		t.Fatalf("classifyActive's error differs from CheckInitialized's:\nclassifyActive:   %v\nCheckInitialized: %v", err, legacy)
	}
	return err
}

func acWipeBinding(t *testing.T, path string, more ...string) {
	t.Helper()
	icEdit(t, path, append([]string{icDropMetaTriggers1, icDropMetaTriggers2, `DELETE FROM store_meta WHERE key LIKE 'binding.%'`}, more...)...)
}

// icLockExclusive holds an exclusive SQLite lock on path until the returned DB
// is closed (the same technique as TestCheckInitialized_LockedDatabase_IsInconclusive).
func icLockExclusive(t *testing.T, path string) *sql.DB {
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

// ---- files that are not (yet) databases ------------------------------------------------------

func TestClassifyActive_Missing(t *testing.T) {
	t.Parallel()
	dir := securefstest.TempDir(t)
	path := filepath.Join(dir, "absent.db")
	before := icHashes(t, dir)
	st, err := classifyActive(path)
	if st != stateMissing || err != nil {
		t.Fatalf("got %v, %v", st, err)
	}
	if !icSameDir(before, icHashes(t, dir)) {
		t.Fatal("classifying a missing database created files")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("the path must still be absent: %v", err)
	}
}

func TestClassifyActive_ParentIsAFile_IsInconclusive(t *testing.T) {
	t.Parallel()
	dir := securefstest.TempDir(t)
	file := filepath.Join(dir, "plain")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := classifyActive(filepath.Join(file, "c.db"))
	if st != stateInconclusive || err == nil {
		t.Fatalf("a path that cannot be examined must fail closed, got %v, %v", st, err)
	}
}

func TestClassifyActive_ZeroByteFile_IsEmpty(t *testing.T) {
	t.Parallel()
	path := filepath.Join(securefstest.TempDir(t), "c.db")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ni := wantNotInit(t, acWant(t, path, stateEmpty))
	if ni.Reason != ReasonEmptyFile {
		t.Fatalf("reason = %q", ni.Reason)
	}
}

func TestClassifyActive_NonSQLiteContent_IsNonSQLite(t *testing.T) {
	t.Parallel()
	path := filepath.Join(securefstest.TempDir(t), "c.db")
	if err := os.WriteFile(path, []byte(strings.Repeat("not a database\n", 30)), 0o600); err != nil {
		t.Fatal(err)
	}
	ni := wantNotInit(t, acWant(t, path, stateNonSQLite))
	if ni.Reason != ReasonNotSQLite {
		t.Fatalf("reason = %q", ni.Reason)
	}
}

func TestClassifyActive_SecurefsRefusals_AreInconclusiveAndPassThrough(t *testing.T) {
	t.Parallel()
	t.Run("unsafe-mode", func(t *testing.T) {
		path := icUnbound(t)
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		err := acWant(t, path, stateInconclusive)
		if err == nil || !strings.Contains(err.Error(), "not safe to use") {
			t.Fatalf("want the securefs refusal, got %v", err)
		}
		wantNeither(t, err)
	})
	t.Run("symlink", func(t *testing.T) {
		path := icUnbound(t)
		link := filepath.Join(filepath.Dir(path), "link.db")
		if err := os.Symlink(path, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		err := acWant(t, link, stateInconclusive)
		if err == nil || !strings.Contains(err.Error(), "symbolic link") {
			t.Fatalf("want the securefs symlink refusal, got %v", err)
		}
	})
}

// ---- unbound DSO databases -----------------------------------------------------------------

func TestClassifyActive_EmptySQLite_IsDSOUnbound(t *testing.T) {
	t.Parallel()
	path := icForeign(t, `CREATE TABLE x(a)`, `DROP TABLE x`)
	ni := wantNotInit(t, acWant(t, path, stateDSOUnbound))
	if ni.Reason != ReasonNoSchema {
		t.Fatalf("reason = %q", ni.Reason)
	}
}

func TestClassifyActive_OnlyEmptySchemaMigrations_IsDSOUnbound_NotForeign(t *testing.T) {
	t.Parallel()
	path := icForeign(t, `CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`)
	wantNotInit(t, acWant(t, path, stateDSOUnbound))
}

func TestClassifyActive_PartialDSOSchema_WithExactStructures_IsDSOUnbound(t *testing.T) {
	t.Parallel()
	// A real DSO database with the core tables dropped: what remains has exactly
	// the DSO structure, so the evidence supports "unbound DSO fragment".
	path := icUnbound(t)
	icEdit(t, path, `DROP TABLE secret_versions`, `DROP TABLE secrets`, `DROP TABLE environments`)
	wantNotInit(t, acWant(t, path, stateDSOUnbound))
}

func TestClassifyActive_PartialDSOSchema_WithHandWrittenColumns_IsInconclusive(t *testing.T) {
	t.Parallel()
	// Right table names, but projects lacks most DSO columns: not DSO evidence.
	path := icForeign(t,
		`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`,
		`CREATE TABLE projects(id TEXT PRIMARY KEY, wrapped_dek BLOB, dek_key_id TEXT)`)
	acWant(t, path, stateInconclusive)
}

func TestClassifyActive_DSONamedTablesWithForeignColumns_AreInconclusive(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		"secrets(id,name)-only":     {`CREATE TABLE secrets(id, name)`},
		"environments(a)-only":      {`CREATE TABLE environments(a)`},
		"audit_events(a)-only":      {`CREATE TABLE audit_events(a)`},
		"secret_versions(a)-only":   {`CREATE TABLE secret_versions(a)`},
		"store_meta(k)-only":        {`CREATE TABLE store_meta(k)`},
		"projects(x)-only":          {`CREATE TABLE projects(x)`},
		"schema_migrations(v)-only": {`CREATE TABLE schema_migrations(v)`},
		"all-seven-foreign-columns": {
			`CREATE TABLE schema_migrations(v)`, `CREATE TABLE projects(x)`, `CREATE TABLE environments(a)`,
			`CREATE TABLE secrets(a)`, `CREATE TABLE secret_versions(a)`, `CREATE TABLE store_meta(k)`, `CREATE TABLE audit_events(a)`},
		"dso-schema_migrations-plus-foreign-secrets": {
			`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`,
			`CREATE TABLE secrets(id, name)`},
	}
	for name, stmts := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			acWant(t, icForeign(t, stmts...), stateInconclusive)
		})
	}
}

func TestClassifyActive_CompleteDSOSchemaWithAlteredTable_IsInconclusive(t *testing.T) {
	t.Parallel()
	for _, table := range []string{"projects", "environments", "secrets", "secret_versions", "store_meta", "audit_events", "schema_migrations"} {
		t.Run(table, func(t *testing.T) {
			t.Parallel()
			path := icUnbound(t)
			icEdit(t, path, `ALTER TABLE `+table+` ADD COLUMN extra_col TEXT`)
			acWant(t, path, stateInconclusive)
		})
	}
}

// ---- unexpected views ---------------------------------------------------------------------------

func TestClassifyActive_Views(t *testing.T) {
	t.Parallel()
	t.Run("view-only-foreign-database", func(t *testing.T) {
		t.Parallel()
		acWant(t, icForeign(t, `CREATE VIEW v AS SELECT 1`), stateInconclusive)
	})
	t.Run("view-only-with-dso-named-view", func(t *testing.T) {
		t.Parallel()
		acWant(t, icForeign(t, `CREATE VIEW projects AS SELECT 1 AS id`), stateInconclusive)
	})
	t.Run("complete-dso-schema-plus-view-no-binding", func(t *testing.T) {
		t.Parallel()
		path := icUnbound(t)
		icEdit(t, path, `CREATE VIEW v AS SELECT 1`)
		acWant(t, path, stateInconclusive)
	})
	t.Run("only-schema_migrations-plus-view", func(t *testing.T) {
		t.Parallel()
		acWant(t, icForeign(t,
			`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`,
			`CREATE VIEW v AS SELECT version FROM schema_migrations`), stateInconclusive)
	})
	t.Run("foreign-table-plus-view-is-still-foreign", func(t *testing.T) {
		t.Parallel()
		// D7 is about tables: a foreign table makes it Foreign whether or not a
		// view is also present. A view alone never does.
		acWant(t, icForeign(t, `CREATE TABLE t(a)`, `CREATE VIEW v AS SELECT a FROM t`), stateForeign)
	})
	t.Run("complete-dso-plus-extra-table-plus-view-is-foreign", func(t *testing.T) {
		t.Parallel()
		path := icUnbound(t)
		icEdit(t, path, `CREATE TABLE extra(a)`, `CREATE VIEW v AS SELECT 1`)
		acWant(t, path, stateForeign)
	})
	// Documented precedence: valid binding or well-formed projects decide first,
	// exactly as CheckInitialized does, so a view cannot turn an initialized or
	// adoptable database into anything else -- and is never Foreign.
	t.Run("valid-binding-plus-view-is-initialized", func(t *testing.T) {
		t.Parallel()
		e := icBound(t, false)
		icEdit(t, e.dbPath, `CREATE VIEW v AS SELECT 1`)
		if err := acWant(t, e.dbPath, stateInitialized); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("well-formed-projects-plus-view-is-initialized", func(t *testing.T) {
		t.Parallel()
		e := icBound(t, true)
		acWipeBinding(t, e.dbPath, `CREATE VIEW v AS SELECT 1`)
		if err := acWant(t, e.dbPath, stateInitialized); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("partial-binding-plus-view-is-binding-corrupt", func(t *testing.T) {
		t.Parallel()
		e := icBound(t, true)
		icEdit(t, e.dbPath, icDropMetaTriggers1, icDropMetaTriggers2,
			`DELETE FROM store_meta WHERE key = 'binding.canary'`, `CREATE VIEW v AS SELECT 1`)
		if err := acWant(t, e.dbPath, stateBindingCorrupt); !errors.Is(err, ErrBindingCorrupt) {
			t.Fatalf("want ErrBindingCorrupt, got %v", err)
		}
	})
	t.Run("newer-schema-plus-view-is-too-new", func(t *testing.T) {
		t.Parallel()
		e := icBound(t, false)
		icEdit(t, e.dbPath, `INSERT INTO schema_migrations(version, applied_at) VALUES (99, 0)`, `CREATE VIEW v AS SELECT 1`)
		acWant(t, e.dbPath, stateTooNew)
	})
}

func TestCollectEvidence_RecordsViewsAndStructures(t *testing.T) {
	t.Parallel()
	path := icUnbound(t)
	icEdit(t, path, `CREATE VIEW zz_view AS SELECT 1`, `CREATE VIEW aa_view AS SELECT 2`)
	ev, err := collectEvidence(icQuerier(t, path))
	if err != nil {
		t.Fatal(err)
	}
	if len(ev.views) != 2 || ev.views[0] != "aa_view" || ev.views[1] != "zz_view" {
		t.Fatalf("views must be collected (sorted), not discarded: %v", ev.views)
	}
	for _, tbl := range dsoTables {
		if ev.columns[tbl] == "" {
			t.Fatalf("no structure collected for %s", tbl)
		}
	}
}

// The expected structures are derived from the embedded migrations (plus the
// schema_migrations statement mirrored from (*Store).migrate); they must equal
// what a real sqlite.Open produces, or the derivation has drifted.
func TestDSOStructures_MatchAFreshlyOpenedDatabase(t *testing.T) {
	t.Parallel()
	want, err := dsoStructures()
	if err != nil {
		t.Fatal(err)
	}
	if len(want.latest) != len(dsoTables) {
		t.Fatalf("expected a structure for each of %d tables, got %d", len(dsoTables), len(want.latest))
	}
	q := icQuerier(t, icUnbound(t))
	realSM, err := tableSignature(q, "schema_migrations")
	if err != nil || want.schemaMigrations != realSM {
		t.Fatalf("schema_migrations structure drifted (%v):\nreal:    %s\nderived: %s", err, realSM, want.schemaMigrations)
	}
	migs, _ := sqlite.EmbeddedMigrations()
	if len(want.versions) != len(migs) {
		t.Fatalf("the embedded sequence has %d migrations, the derived schema %d", len(migs), len(want.versions))
	}
	for i, m := range migs {
		if want.versions[i] != m.Version {
			t.Fatalf("derived version sequence %v differs from the embedded one", want.versions)
		}
	}
	for _, tbl := range dsoTables {
		got, err := tableSignature(q, tbl)
		if err != nil {
			t.Fatal(err)
		}
		if got != want.latest[tbl] {
			t.Fatalf("%s: derived structure differs from a real database:\nreal:    %s\nderived: %s", tbl, got, want.latest[tbl])
		}
	}
}

func TestClassifyActive_BindingOrProjectsWinOverStructureDoubts_DocumentedPrecedence(t *testing.T) {
	t.Parallel()
	// Documented limitation, pinned: CheckInitialized (and so classifyActive)
	// decides Initialized from binding or project evidence alone, before any
	// table structure is considered. Only the unbound branches look at structure.
	e := icBound(t, false)
	icEdit(t, e.dbPath, `ALTER TABLE secrets ADD COLUMN extra_col TEXT`)
	if err := acWant(t, e.dbPath, stateInitialized); err != nil {
		t.Fatal(err)
	}
}

func TestClassifyActive_CleanUnboundDSO_IsDSOUnbound(t *testing.T) {
	t.Parallel()
	ni := wantNotInit(t, acWant(t, icUnbound(t), stateDSOUnbound))
	if ni.Reason != ReasonNoBindingNoProjects {
		t.Fatalf("reason = %q", ni.Reason)
	}
}

func TestClassifyActive_LegacySchema1WithoutProjects_IsDSOUnbound(t *testing.T) {
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
	wantNotInit(t, acWant(t, dbPath, stateDSOUnbound))
}

// ---- Foreign ---------------------------------------------------------------------------------

func TestClassifyActive_ForeignSQLite_IsForeign(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		"one-table-with-a-row": {`CREATE TABLE customer(id INTEGER PRIMARY KEY, name TEXT)`, `INSERT INTO customer(name) VALUES('alice')`},
		"empty-foreign-table":  {`CREATE TABLE t(a)`},
		"foreign-with-migrations-table": {
			`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`,
			`CREATE TABLE orders(id)`},
	}
	for name, stmts := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ni := wantNotInit(t, acWant(t, icForeign(t, stmts...), stateForeign))
			if ni.Reason != ReasonNoSchema || len(ni.Guidance) != 0 {
				t.Fatalf("unexpected: %+v", ni)
			}
		})
	}
}

func TestClassifyActive_CompleteDSOSchemaPlusExtraTableAndNoBinding_IsForeign(t *testing.T) {
	t.Parallel()
	path := icUnbound(t)
	icEdit(t, path, `CREATE TABLE extra(a)`)
	ni := wantNotInit(t, acWant(t, path, stateForeign))
	if ni.Reason != ReasonNoBindingNoProjects || len(ni.Guidance) != 0 {
		t.Fatalf("unexpected: %+v", ni)
	}
}

func TestClassifyActive_MigratedForeignDatabase_IsForeign(t *testing.T) {
	t.Parallel()
	// What the pre-fix restore left behind: a foreign file with the DSO schema
	// added by sqlite.Open.
	path := icForeign(t, `CREATE TABLE customer(id INTEGER PRIMARY KEY, name TEXT)`, `INSERT INTO customer(name) VALUES('alice')`)
	st, err := sqlite.Open(ctx, path, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	wantNotInit(t, acWant(t, path, stateForeign))
}

func TestClassifyActive_SQLiteInternalTables_NeverMakeAFile_Foreign(t *testing.T) {
	t.Parallel()
	hasTable := func(t *testing.T, path, name string) {
		t.Helper()
		before := icFileHash(t, path)
		var n int
		db := icQuerier(t, path)
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n); err != nil || n != 1 {
			t.Fatalf("fixture lacks %s (%d, %v)", name, n, err)
		}
		if icFileHash(t, path) != before {
			t.Fatal("fixture check changed the file")
		}
	}
	t.Run("unbound-with-sqlite_stat1", func(t *testing.T) {
		t.Parallel()
		path := icUnbound(t)
		icEdit(t, path, `ANALYZE`)
		hasTable(t, path, "sqlite_stat1")
		wantNotInit(t, acWant(t, path, stateDSOUnbound))
	})
	t.Run("bound-with-sqlite_stat1", func(t *testing.T) {
		t.Parallel()
		e := icBound(t, false)
		icEdit(t, e.dbPath, `ANALYZE`)
		hasTable(t, e.dbPath, "sqlite_stat1")
		if err := acWant(t, e.dbPath, stateInitialized); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("sqlite_sequence", func(t *testing.T) {
		t.Parallel()
		// audit_events is AUTOINCREMENT, so a normal DSO database always has it.
		path := icUnbound(t)
		hasTable(t, path, "sqlite_sequence")
		wantNotInit(t, acWant(t, path, stateDSOUnbound))
	})
}

// ---- precedence ----------------------------------------------------------------------------------

func TestClassifyActive_ValidBindingTriple_IsInitialized(t *testing.T) {
	t.Parallel()
	e := icBound(t, false)
	if err := acWant(t, e.dbPath, stateInitialized); err != nil {
		t.Fatal(err)
	}
}

func TestClassifyActive_Precedence_BindingBeatsForeign(t *testing.T) {
	t.Parallel()
	e := icBound(t, false)
	icEdit(t, e.dbPath, `CREATE TABLE extra(a)`, `INSERT INTO extra VALUES(1)`)
	if err := acWant(t, e.dbPath, stateInitialized); err != nil {
		t.Fatalf("foreign tables beside a valid binding are initialized: %v", err)
	}
}

func TestClassifyActive_Precedence_WellFormedProjectsBeatForeign(t *testing.T) {
	t.Parallel()
	e := icBound(t, true)
	acWipeBinding(t, e.dbPath, `CREATE TABLE extra(a)`)
	if err := acWant(t, e.dbPath, stateInitialized); err != nil {
		t.Fatalf("well-formed projects are adoptable even beside foreign tables: %v", err)
	}
}

func TestClassifyActive_Precedence_ForeignDoesNotWeakenBindingCorruption(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		"one-of-three-rows": {`DELETE FROM store_meta WHERE key IN ('binding.canary', 'binding.root_key_id')`},
		"two-of-three-rows": {`DELETE FROM store_meta WHERE key = 'binding.canary'`},
		"invalid-version":   {`UPDATE store_meta SET value = x'32' WHERE key = 'binding.version'`},
	}
	for name, stmts := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := icBound(t, true)
			icEdit(t, e.dbPath, append([]string{icDropMetaTriggers1, icDropMetaTriggers2, `CREATE TABLE extra(a)`}, stmts...)...)
			err := acWant(t, e.dbPath, stateBindingCorrupt)
			if !errors.Is(err, ErrBindingCorrupt) {
				t.Fatalf("want ErrBindingCorrupt, got %v", err)
			}
		})
	}
}

func TestClassifyActive_BindingCorrupt_WithoutForeignTables(t *testing.T) {
	t.Parallel()
	e := icBound(t, true)
	icEdit(t, e.dbPath, icDropMetaTriggers1, icDropMetaTriggers2, `DELETE FROM store_meta WHERE key = 'binding.canary'`)
	if err := acWant(t, e.dbPath, stateBindingCorrupt); !errors.Is(err, ErrBindingCorrupt) {
		t.Fatalf("want ErrBindingCorrupt, got %v", err)
	}
}

func TestClassifyActive_Adoptable_WipedBindingWithProject_IsInitialized(t *testing.T) {
	t.Parallel()
	e := icBound(t, true)
	acWipeBinding(t, e.dbPath)
	if err := acWant(t, e.dbPath, stateInitialized); err != nil {
		t.Fatal(err)
	}
}

func TestClassifyActive_NewerSchema_IsTooNew(t *testing.T) {
	t.Parallel()
	e := icBound(t, false)
	icEdit(t, e.dbPath, `INSERT INTO schema_migrations(version, applied_at) VALUES (99, 0)`)
	err := acWant(t, e.dbPath, stateTooNew)
	if !errors.Is(err, store.ErrSchemaTooNew) {
		t.Fatalf("want store.ErrSchemaTooNew, got %v", err)
	}
}

// ---- inspection that cannot decide ------------------------------------------------------------------

func TestClassifyActive_LockedDatabase_IsInconclusive(t *testing.T) {
	t.Parallel()
	e := icBound(t, false)
	w := icLockExclusive(t, e.dbPath)
	start := time.Now()
	st, err := classifyActive(e.dbPath)
	if st != stateInconclusive {
		t.Fatalf("state = %v (err %v)", st, err)
	}
	wantInconclusive(t, err)
	if time.Since(start) > 30*time.Second {
		t.Fatal("the inspection was not bounded")
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if st, err := classifyActive(e.dbPath); st != stateInitialized || err != nil {
		t.Fatalf("after the lock is released the database is initialized: %v, %v", st, err)
	}
}

func TestClassifyActive_InterruptedInit_WALPresent_IsInconclusive_AndReadOnly(t *testing.T) {
	t.Parallel()
	path := icCrash(t, "open-kill")
	mainBefore, walBefore := icFileHash(t, path), icFileHash(t, path+"-wal")
	st, err := classifyActive(path)
	if st != stateInconclusive {
		t.Fatalf("state = %v (err %v)", st, err)
	}
	if ic := wantInconclusive(t, err); !ic.WALPresent {
		t.Fatal("a non-empty -wal was present")
	}
	if icFileHash(t, path) != mainBefore || icFileHash(t, path+"-wal") != walBefore {
		t.Fatal("the main file or the -wal changed") // -shm is intentionally not compared
	}
	if _, err := os.Lstat(path + "-shm"); err != nil {
		t.Fatalf("-shm must still exist (it may be rewritten, never removed): %v", err)
	}
}

func TestClassifyActive_DamagedWAL_IsInconclusive_AndReadOnly(t *testing.T) {
	t.Parallel()
	path := icCrash(t, "open-kill")
	fi, _ := os.Stat(path + "-wal")
	junk := make([]byte, fi.Size())
	_, _ = rand.Read(junk)
	if err := os.WriteFile(path+"-wal", junk, 0o600); err != nil {
		t.Fatal(err)
	}
	mainBefore, walBefore := icFileHash(t, path), icFileHash(t, path+"-wal")
	st, err := classifyActive(path)
	if st != stateInconclusive {
		t.Fatalf("state = %v (err %v)", st, err)
	}
	wantInconclusive(t, err)
	if icFileHash(t, path) != mainBefore || icFileHash(t, path+"-wal") != walBefore {
		t.Fatal("the main file or the -wal changed")
	}
}

func TestClassifyActive_CrashAfterBind_WALHoldsBinding_IsInitialized(t *testing.T) {
	t.Parallel()
	path := icCrash(t, "bind-kill")
	mainBefore, walBefore := icFileHash(t, path), icFileHash(t, path+"-wal")
	st, err := classifyActive(path)
	if st != stateInitialized || err != nil {
		t.Fatalf("got %v, %v", st, err)
	}
	if icFileHash(t, path) != mainBefore || icFileHash(t, path+"-wal") != walBefore {
		t.Fatal("the main file or the -wal changed")
	}
}

func TestClassifyActive_InjectedInspectionErrors_FailClosed(t *testing.T) {
	t.Parallel()
	path := icUnbound(t) // exists, so the Lstat gate passes
	t.Run("inspection-failed", func(t *testing.T) {
		inspect := func(string, func(sqlite.Querier) error) (sqlite.InspectionResult, error) {
			return sqlite.InspectionResult{}, fmt.Errorf("%w: boom", sqlite.ErrInspectionFailed)
		}
		st, err := classifyActiveWith(inspect, path)
		if st != stateInconclusive {
			t.Fatalf("state = %v", st)
		}
		wantInconclusive(t, err)
	})
	t.Run("unrecognised-error", func(t *testing.T) {
		boom := errors.New("something else")
		inspect := func(string, func(sqlite.Querier) error) (sqlite.InspectionResult, error) {
			return sqlite.InspectionResult{}, boom
		}
		st, err := classifyActiveWith(inspect, path)
		if st != stateInconclusive || !errors.Is(err, boom) {
			t.Fatalf("got %v, %v", st, err)
		}
	})
	t.Run("deterministic-query-failure", func(t *testing.T) {
		broken := icBound(t, false)
		icEdit(t, broken.dbPath, `DROP TABLE store_meta`, `CREATE TABLE store_meta(k)`) // wrong columns
		st, err := classifyActive(broken.dbPath)
		if st != stateInconclusive {
			t.Fatalf("state = %v (err %v)", st, err)
		}
		wantInconclusive(t, err)
	})
}

// ---- the type itself ---------------------------------------------------------------------------------

func TestActiveState_ZeroValueFailsClosed_AndStringsAreDistinct(t *testing.T) {
	t.Parallel()
	var zero activeState
	if zero != stateInconclusive {
		t.Fatal("the zero value must be stateInconclusive")
	}
	seen := map[string]activeState{}
	for _, s := range []activeState{stateInconclusive, stateMissing, stateInitialized, stateEmpty, stateDSOUnbound,
		stateBindingCorrupt, stateForeign, stateTooNew, stateNonSQLite} {
		name := s.String()
		if strings.HasPrefix(name, "activeState(") {
			t.Fatalf("state %d has no name", int(s))
		}
		if prev, dup := seen[name]; dup {
			t.Fatalf("%v and %v share the name %q", prev, s, name)
		}
		seen[name] = s
	}
	if got := activeState(99).String(); !strings.HasPrefix(got, "activeState(") {
		t.Fatalf("an unknown state must be named as such: %q", got)
	}
}

// ---- CheckInitialized is unchanged by the classification fields -----------------------------------------

func TestCheckInitialized_ErrorTextAndMatchingUnchangedByClassification(t *testing.T) {
	t.Parallel()
	path := icUnbound(t)
	icEdit(t, path, `CREATE TABLE extra(a)`)
	ni := wantNotInit(t, icCheck(t, path))
	want := fmt.Sprintf("database %s is not an initialized DSO Central database: %s (if another dso command may be writing to it, wait and retry)",
		path, ReasonNoBindingNoProjects)
	if ni.Error() != want {
		t.Fatalf("the ordinary-command message changed:\n got %q\nwant %q", ni.Error(), want)
	}
	if ni.class != stateForeign {
		t.Fatalf("the class is internal evidence and should be Foreign here, got %v", ni.class)
	}
	if !errors.Is(ni, ErrNotInitialized) || errors.Is(ni, sqlite.ErrInspectionFailed) {
		t.Fatalf("matching changed: %v", ni)
	}
}

// ---- Phase 1.1: ownership and structural integrity are separate answers ------------------------------

// acAssess runs assessActive and fails if any file in the directory changed (for
// fixtures without sidecars).
func acAssess(t *testing.T, path string) activeAssessment {
	t.Helper()
	before := icHashes(t, filepath.Dir(path))
	a := assessActive(path)
	if after := icHashes(t, filepath.Dir(path)); !icSameDir(before, after) {
		t.Fatalf("assessActive changed files:\nbefore %v\nafter  %v", before, after)
	}
	return a
}

func acWantAssess(t *testing.T, path string, state activeState, structure structuralFinding) activeAssessment {
	t.Helper()
	a := acAssess(t, path)
	if a.State != state || a.Structure != structure {
		t.Fatalf("got state=%v structure=%v (reasons %v, err %v), want state=%v structure=%v",
			a.State, a.Structure, a.StructureReasons, a.Err, state, structure)
	}
	if structure == structureMismatch && len(a.StructureReasons) == 0 {
		t.Fatal("a structural mismatch must say why")
	}
	if structure == structureClean && len(a.StructureReasons) != 0 {
		t.Fatalf("a clean finding has no reasons: %v", a.StructureReasons)
	}
	return a
}

func TestAssessActive_OwnershipAndStructureAreIndependent(t *testing.T) {
	t.Parallel()
	all, _ := sqlite.EmbeddedMigrations()
	legacy := func(t *testing.T, n int) string {
		p := filepath.Join(securefstest.TempDir(t), "old.db")
		st, err := sqlite.Open(ctx, p, sqlite.Options{Migrations: all[:n]})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
		return p
	}
	bound := func(edits ...string) func(*testing.T) string {
		return func(t *testing.T) string {
			e := icBound(t, false)
			if len(edits) > 0 {
				icEdit(t, e.dbPath, edits...)
			}
			return e.dbPath
		}
	}
	adoptable := func(edits ...string) func(*testing.T) string {
		return func(t *testing.T) string {
			e := icBound(t, true)
			acWipeBinding(t, e.dbPath, edits...)
			return e.dbPath
		}
	}
	unbound := func(edits ...string) func(*testing.T) string {
		return func(t *testing.T) string {
			p := icUnbound(t)
			if len(edits) > 0 {
				icEdit(t, p, edits...)
			}
			return p
		}
	}
	foreign := func(stmts ...string) func(*testing.T) string {
		return func(t *testing.T) string { return icForeign(t, stmts...) }
	}
	cases := []struct {
		name      string
		build     func(*testing.T) string
		state     activeState
		structure structuralFinding
	}{
		{"binding, expected tables", bound(), stateInitialized, structureClean},
		{"binding, altered secrets", bound(`ALTER TABLE secrets ADD COLUMN extra_col TEXT`), stateInitialized, structureMismatch},
		{"binding, missing core tables", bound(`DROP TABLE secret_versions`, `DROP TABLE secrets`, `DROP TABLE environments`), stateInitialized, structureMismatch},
		{"binding, unexpected view", bound(`CREATE VIEW v AS SELECT 1`), stateInitialized, structureMismatch},
		{"binding, unexpected user table", bound(`CREATE TABLE extra(a)`), stateInitialized, structureMismatch},
		{"binding, sqlite_stat1 only", bound(`ANALYZE`), stateInitialized, structureClean},
		{"binding, generated column", bound(`ALTER TABLE secrets ADD COLUMN g INTEGER GENERATED ALWAYS AS (1) VIRTUAL`), stateInitialized, structureMismatch},
		{"projects, expected tables", adoptable(), stateInitialized, structureClean},
		{"projects, altered environments", adoptable(`ALTER TABLE environments ADD COLUMN extra_col TEXT`), stateInitialized, structureMismatch},
		{"projects, unexpected view", adoptable(`CREATE VIEW v AS SELECT 1`), stateInitialized, structureMismatch},
		{"unbound, expected tables", unbound(), stateDSOUnbound, structureClean},
		{"unbound, sqlite_stat1 only", unbound(`ANALYZE`), stateDSOUnbound, structureClean},
		{"unbound, schema 1 (per-version structure)", func(t *testing.T) string { return legacy(t, 1) }, stateDSOUnbound, structureClean},
		{"unbound, schema 2 (per-version structure)", func(t *testing.T) string { return legacy(t, 2) }, stateDSOUnbound, structureClean},
		{"unbound, dropped core tables", unbound(`DROP TABLE secret_versions`, `DROP TABLE secrets`, `DROP TABLE environments`), stateDSOUnbound, structureMismatch},
		{"unbound, view", unbound(`CREATE VIEW v AS SELECT 1`), stateInconclusive, structureMismatch},
		{"unbound, generated column", unbound(`ALTER TABLE environments ADD COLUMN g INTEGER GENERATED ALWAYS AS (1) VIRTUAL`), stateInconclusive, structureMismatch},
		{"unbound, altered table", unbound(`ALTER TABLE projects ADD COLUMN extra_col TEXT`), stateInconclusive, structureMismatch},
		{"unbound, extra user table", unbound(`CREATE TABLE extra(a)`), stateForeign, structureMismatch},
		{"foreign file", foreign(`CREATE TABLE customer(id)`), stateForeign, structureUnknown},
		{"migrated foreign file", func(t *testing.T) string {
			p := icForeign(t, `CREATE TABLE customer(id)`)
			st, err := sqlite.Open(ctx, p, sqlite.Options{})
			if err != nil {
				t.Fatal(err)
			}
			_ = st.Close()
			return p
		}, stateForeign, structureMismatch},
		{"DSO-named foreign table only", foreign(`CREATE TABLE secrets(id, name)`), stateInconclusive, structureUnknown},
		{"view-only foreign file", foreign(`CREATE VIEW v AS SELECT 1`), stateInconclusive, structureUnknown},
		{"partial binding, expected tables", bound(icDropMetaTriggers1, icDropMetaTriggers2, `DELETE FROM store_meta WHERE key = 'binding.canary'`), stateBindingCorrupt, structureClean},
		{"partial binding, altered table", bound(icDropMetaTriggers1, icDropMetaTriggers2, `DELETE FROM store_meta WHERE key = 'binding.canary'`, `ALTER TABLE secrets ADD COLUMN extra_col TEXT`), stateBindingCorrupt, structureMismatch},
		{"newer schema", bound(`INSERT INTO schema_migrations(version, applied_at) VALUES (99, 0)`), stateTooNew, structureUnknown},
		{"zero-byte file", func(t *testing.T) string {
			p := filepath.Join(securefstest.TempDir(t), "c.db")
			if err := os.WriteFile(p, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			return p
		}, stateEmpty, structureUnknown},
		{"non-SQLite file", func(t *testing.T) string {
			p := filepath.Join(securefstest.TempDir(t), "c.db")
			if err := os.WriteFile(p, []byte(strings.Repeat("not a database\n", 30)), 0o600); err != nil {
				t.Fatal(err)
			}
			return p
		}, stateNonSQLite, structureUnknown},
		{"missing file", func(t *testing.T) string { return filepath.Join(securefstest.TempDir(t), "absent.db") }, stateMissing, structureUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			acWantAssess(t, tc.build(t), tc.state, tc.structure)
		})
	}
}

func TestAssessActive_IncompleteInspection_StructureIsUnknown(t *testing.T) {
	t.Parallel()
	t.Run("locked", func(t *testing.T) {
		t.Parallel()
		e := icBound(t, false)
		w := icLockExclusive(t, e.dbPath)
		a := assessActive(e.dbPath)
		if a.State != stateInconclusive || a.Structure != structureUnknown {
			t.Fatalf("got %v / %v", a.State, a.Structure)
		}
		_ = w.Close()
	})
	t.Run("interrupted-init-wal", func(t *testing.T) {
		t.Parallel()
		path := icCrash(t, "open-kill")
		mainBefore, walBefore := icFileHash(t, path), icFileHash(t, path+"-wal")
		a := assessActive(path)
		if a.State != stateInconclusive || a.Structure != structureUnknown {
			t.Fatalf("got %v / %v", a.State, a.Structure)
		}
		if icFileHash(t, path) != mainBefore || icFileHash(t, path+"-wal") != walBefore {
			t.Fatal("the main file or the -wal changed")
		}
	})
	t.Run("injected-failure", func(t *testing.T) {
		t.Parallel()
		inspect := func(string, func(sqlite.Querier) error) (sqlite.InspectionResult, error) {
			return sqlite.InspectionResult{}, fmt.Errorf("%w: boom", sqlite.ErrInspectionFailed)
		}
		a := assessActiveWith(inspect, icUnbound(t))
		if a.State != stateInconclusive || a.Structure != structureUnknown {
			t.Fatalf("got %v / %v", a.State, a.Structure)
		}
	})
}

func TestAssessActive_CrashAfterBind_WALHoldsEverything_IsInitializedAndClean(t *testing.T) {
	t.Parallel()
	path := icCrash(t, "bind-kill")
	mainBefore, walBefore := icFileHash(t, path), icFileHash(t, path+"-wal")
	a := assessActive(path)
	if a.State != stateInitialized || a.Structure != structureClean {
		t.Fatalf("got %v / %v (%v)", a.State, a.Structure, a.StructureReasons)
	}
	if icFileHash(t, path) != mainBefore || icFileHash(t, path+"-wal") != walBefore {
		t.Fatal("the main file or the -wal changed") // -shm is intentionally not compared
	}
}

func TestAssessActive_StructureReasonsNameTheProblem(t *testing.T) {
	t.Parallel()
	e := icBound(t, false)
	icEdit(t, e.dbPath, `ALTER TABLE secrets ADD COLUMN extra_col TEXT`, `DROP TABLE secret_versions`, `CREATE TABLE extra(a)`, `CREATE VIEW v AS SELECT 1`)
	a := acWantAssess(t, e.dbPath, stateInitialized, structureMismatch)
	text := strings.Join(a.StructureReasons, "\n")
	for _, want := range []string{"secrets: columns differ", "secret_versions: table is missing", "unexpected table extra", "unexpected view v"} {
		if !strings.Contains(text, want) {
			t.Fatalf("reasons lack %q:\n%s", want, text)
		}
	}
}

// ---- generated columns -----------------------------------------------------------------------------

func TestTableSignature_IncludesGeneratedColumnMetadata(t *testing.T) {
	t.Parallel()
	sig := func(ddl string) string {
		q := icQuerier(t, icForeign(t, ddl))
		s, err := tableSignature(q, "t")
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	plain := sig(`CREATE TABLE t(a INTEGER, g INTEGER)`)
	virtual := sig(`CREATE TABLE t(a INTEGER, g INTEGER GENERATED ALWAYS AS (a) VIRTUAL)`)
	stored := sig(`CREATE TABLE t(a INTEGER, g INTEGER GENERATED ALWAYS AS (a) STORED)`)
	without := sig(`CREATE TABLE t(a INTEGER)`)
	all := map[string]string{"plain": plain, "virtual": virtual, "stored": stored, "without-column": without}
	seen := map[string]string{}
	for name, s := range all {
		if prev, dup := seen[s]; dup {
			t.Fatalf("%s and %s have the same signature %q", prev, name, s)
		}
		seen[s] = name
	}
}

func TestDSOStructures_HaveNoGeneratedOrHiddenColumns(t *testing.T) {
	t.Parallel()
	sch, err := dsoStructures()
	if err != nil {
		t.Fatal(err)
	}
	for ver, m := range sch.byVersion {
		for table, sig := range m {
			for _, col := range strings.Split(strings.TrimSuffix(sig, ";"), ";") {
				f := strings.Fields(col)
				if len(f) < 5 || f[len(f)-1] != "0" {
					t.Fatalf("schema %d %s: column %q is hidden or generated, but DSO's schema defines none", ver, table, col)
				}
			}
		}
	}
}

// ---- derived structures: per version and defensive copies ----------------------------------------------

func TestDSOStructures_AreDerivedPerSchemaVersion(t *testing.T) {
	t.Parallel()
	sch, err := dsoStructures()
	if err != nil {
		t.Fatal(err)
	}
	migs, _ := sqlite.EmbeddedMigrations()
	if len(sch.byVersion) != len(migs) {
		t.Fatalf("one structure set per embedded migration expected, got %d for %d", len(sch.byVersion), len(migs))
	}
	if !sameStrMap(sch.latest, sch.byVersion[migs[len(migs)-1].Version]) {
		t.Fatal("latest must be the newest version's structures")
	}
	if _, ok := sch.byVersion[1]["store_meta"]; ok {
		t.Fatal("schema 1 has no store_meta")
	}
	if _, ok := sch.byVersion[1]["schema_migrations"]; !ok {
		t.Fatal("schema_migrations exists from the start")
	}
}

func sameStrMap(a, b map[string]string) bool {
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

func TestDSOStructures_ReturnDefensiveCopies(t *testing.T) {
	t.Parallel()
	first, err := dsoStructures()
	if err != nil {
		t.Fatal(err)
	}
	want, _ := dsoStructures()
	// Corrupt everything reachable from the first result.
	for k := range first.latest {
		first.latest[k] = "tampered"
	}
	first.latest["injected"] = "x"
	first.schemaMigrations = "tampered"
	for i := range first.versions {
		first.versions[i] = 999
	}
	first.versions = append(first.versions, 1000)
	for ver, m := range first.byVersion {
		for k := range m {
			m[k] = "tampered"
		}
		m["injected"] = "x"
		delete(first.byVersion, ver)
	}
	again, err := dsoStructures()
	if err != nil {
		t.Fatal(err)
	}
	if again.schemaMigrations != want.schemaMigrations || fmt.Sprint(again.versions) != fmt.Sprint(want.versions) {
		t.Fatal("mutating the returned versions or schema_migrations structure affected a later caller")
	}
	if !sameStrMap(again.latest, want.latest) || len(again.byVersion) != len(want.byVersion) {
		t.Fatal("mutating a returned structure affected a later caller")
	}
	for ver, m := range want.byVersion {
		if !sameStrMap(again.byVersion[ver], m) {
			t.Fatalf("nested structures for schema %d were affected", ver)
		}
	}
	// And classification still works after the tampering.
	if a := assessActive(icUnbound(t)); a.State != stateDSOUnbound || a.Structure != structureClean {
		t.Fatalf("classification affected by tampering: %v / %v", a.State, a.Structure)
	}
}

func TestDSOStructures_ConcurrentCallersUnderRace(t *testing.T) {
	t.Parallel()
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				sch, err := dsoStructures()
				if err != nil {
					errs <- err
					return
				}
				for k := range sch.latest { // each caller owns its copy: writes must not race
					sch.latest[k] += "!"
				}
				for _, m := range sch.byVersion {
					m["x"] = "y"
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	fresh, _ := dsoStructures()
	for k, v := range fresh.latest {
		if strings.HasSuffix(v, "!") {
			t.Fatalf("%s: a concurrent caller's write reached the cache", k)
		}
	}
}

// ---- error identity ----------------------------------------------------------------------------------------

func TestClassifyActive_InconclusiveNeverMatchesErrNotInitialized(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		path     string
		contains string
	}{
		"dso-named-foreign-table": {icForeign(t, `CREATE TABLE secrets(id, name)`), "do not have its structure"},
		"view-only-file":          {icForeign(t, `CREATE VIEW v AS SELECT 1`), "views"},
		"dso-unbound-plus-view": {func() string {
			p := icUnbound(t)
			icEdit(t, p, `CREATE VIEW v AS SELECT 1`)
			return p
		}(), "views"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			a := acAssess(t, tc.path)
			if a.State != stateInconclusive {
				t.Fatalf("state = %v", a.State)
			}
			ic := wantInconclusive(t, a.Err) // matches ErrInspectionFailed, never ErrNotInitialized
			if errors.Is(a.Err, ErrNotInitialized) {
				t.Fatalf("an inconclusive classification must not match ErrNotInitialized: %v", a.Err)
			}
			var ni *NotInitializedError
			if errors.As(a.Err, &ni) {
				t.Fatalf("the not-initialized finding must not be reachable through the chain: %v", a.Err)
			}
			if !strings.Contains(a.Err.Error(), tc.contains) || ic.Path != tc.path {
				t.Fatalf("the diagnostic context was lost: %v", a.Err)
			}
		})
	}
}

func TestCheckInitialized_AmbiguousFiles_KeepTheirHistoricalErrorIdentity(t *testing.T) {
	t.Parallel()
	// CheckInitialized must not change: the same files still produce the
	// not-initialized finding, with the historical text.
	path := icForeign(t, `CREATE TABLE secrets(id, name)`)
	ni := wantNotInit(t, icCheck(t, path))
	want := fmt.Sprintf("database %s is not an initialized DSO Central database: %s (if another dso command may be writing to it, wait and retry)", path, ReasonNoSchema)
	if ni.Error() != want || !ni.Retry {
		t.Fatalf("CheckInitialized's finding changed:\n got %q (retry=%v)\nwant %q (retry=true)", ni.Error(), ni.Retry, want)
	}
	view := icForeign(t, `CREATE VIEW v AS SELECT 1`)
	if ni := wantNotInit(t, icCheck(t, view)); ni.Reason != ReasonNoSchema {
		t.Fatalf("reason = %q", ni.Reason)
	}
}

func TestClassifyActive_StateIsAuthoritativeNotTheErrorType(t *testing.T) {
	t.Parallel()
	// Each non-inconclusive finding still carries CheckInitialized's error, but
	// control flow must use the state: this pins the pairing so a change shows up.
	for name, tc := range map[string]struct {
		path  string
		state activeState
		isNI  bool
	}{
		"foreign":      {icForeign(t, `CREATE TABLE customer(id)`), stateForeign, true},
		"dso-unbound":  {icUnbound(t), stateDSOUnbound, true},
		"inconclusive": {icForeign(t, `CREATE TABLE secrets(id, name)`), stateInconclusive, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			a := acAssess(t, tc.path)
			if a.State != tc.state || errors.Is(a.Err, ErrNotInitialized) != tc.isNI {
				t.Fatalf("state=%v isNotInitialized=%v, want %v / %v (err %v)", a.State, errors.Is(a.Err, ErrNotInitialized), tc.state, tc.isNI, a.Err)
			}
		})
	}
}

// ---- Phase 1.2: migration-history continuity and version-aware ownership -----------------------------------

// acVersions rewrites schema_migrations on a fixture to exactly the given
// versions (the table itself is untouched).
func acVersions(versions ...int) []string {
	stmts := []string{`DELETE FROM schema_migrations`}
	for _, v := range versions {
		stmts = append(stmts, fmt.Sprintf(`INSERT INTO schema_migrations(version, applied_at) VALUES (%d, 0)`, v))
	}
	return stmts
}

func acReasons(a activeAssessment) string { return strings.Join(a.StructureReasons, "\n") }

func TestAssessActive_InterruptedInit_OnlySchemaMigrations_IsDSOUnbound(t *testing.T) {
	t.Parallel()
	const ddl = `CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`
	check := func(t *testing.T, path string) {
		t.Helper()
		a := acWantAssess(t, path, stateDSOUnbound, structureUnknown) // no recorded version: structure cannot be attributed
		ni := wantNotInit(t, a.Err)                                   // the historical not-initialized finding, not Inconclusive
		if ni.Reason != ReasonNoSchema {
			t.Fatalf("reason = %q", ni.Reason)
		}
		if err := CheckInitialized(path); !errors.Is(err, ErrNotInitialized) {
			t.Fatalf("CheckInitialized must keep reporting not-initialized: %v", err)
		}
	}
	t.Run("real-open-that-applied-no-migration", func(t *testing.T) {
		t.Parallel()
		// What an initialization killed right after creating schema_migrations
		// leaves: sqlite.Open with an empty (non-nil) migration set creates that
		// table and nothing else.
		path := filepath.Join(securefstest.TempDir(t), "c.db")
		st, err := sqlite.Open(ctx, path, sqlite.Options{Migrations: []sqlite.Migration{}})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := icQuerier(t, path).QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table'`).Scan(&n); err != nil || n != 1 {
			t.Fatalf("fixture should hold exactly schema_migrations (%d tables, %v)", n, err)
		}
		check(t, path)
	})
	t.Run("hand-written-empty", func(t *testing.T) {
		t.Parallel()
		check(t, icForeign(t, ddl))
	})
	t.Run("with-a-recorded-version-it-is-not-interrupted-init", func(t *testing.T) {
		t.Parallel()
		// An interrupted initialization leaves schema_migrations EMPTY; a row means
		// something else wrote it (see the zero-rows tests below).
		acWant(t, icForeign(t, ddl, `INSERT INTO schema_migrations VALUES (999, 0)`), stateInconclusive)
	})
	t.Run("altered-schema_migrations-is-inconclusive", func(t *testing.T) {
		t.Parallel()
		acWant(t, icForeign(t, `CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL, extra TEXT)`), stateInconclusive)
	})
}

func TestAssessActive_MissingMigrationHistory_IsAStructuralMismatch_NotAnOwnershipChange(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		versions []int
		mismatch bool
		want     []string // substrings every reason text must contain
		notWant  []string
	}{
		{"complete-history", []int{1, 2, 3}, false, nil, nil},
		{"only-the-newest", []int{3}, true, []string{"migration version 1 is not recorded", "migration version 2 is not recorded"}, nil},
		{"missing-middle", []int{1, 3}, true, []string{"migration version 2 is not recorded"}, []string{"migration version 1 is not recorded"}},
		{"missing-oldest", []int{2, 3}, true, []string{"migration version 1 is not recorded"}, []string{"migration version 2 is not recorded"}},
		{"unrecognised-extra-version", []int{0, 1, 2, 3}, true, []string{"recorded schema version 0 is not an embedded migration"}, []string{"is not recorded"}},
	}
	for _, tc := range cases {
		t.Run("bound/"+tc.name, func(t *testing.T) {
			t.Parallel()
			e := icBound(t, false)
			icEdit(t, e.dbPath, acVersions(tc.versions...)...)
			structure := structureClean
			if tc.mismatch {
				structure = structureMismatch
			}
			a := acWantAssess(t, e.dbPath, stateInitialized, structure) // ownership is unchanged
			for _, w := range tc.want {
				if !strings.Contains(acReasons(a), w) {
					t.Fatalf("reasons lack %q:\n%s", w, acReasons(a))
				}
			}
			for _, w := range tc.notWant {
				if strings.Contains(acReasons(a), w) {
					t.Fatalf("reasons must not contain %q:\n%s", w, acReasons(a))
				}
			}
		})
		t.Run("unbound/"+tc.name, func(t *testing.T) {
			t.Parallel()
			path := icUnbound(t)
			icEdit(t, path, acVersions(tc.versions...)...)
			structure := structureClean
			if tc.mismatch {
				structure = structureMismatch
			}
			acWantAssess(t, path, stateDSOUnbound, structure)
		})
	}
}

func TestAssessActive_ValidLegacyHistories_AreClean(t *testing.T) {
	t.Parallel()
	all, _ := sqlite.EmbeddedMigrations()
	for n := 1; n <= len(all); n++ {
		t.Run(fmt.Sprintf("schema-%d", n), func(t *testing.T) {
			t.Parallel()
			p := filepath.Join(securefstest.TempDir(t), "v.db")
			st, err := sqlite.Open(ctx, p, sqlite.Options{Migrations: all[:n]})
			if err != nil {
				t.Fatal(err)
			}
			if err := st.Close(); err != nil {
				t.Fatal(err)
			}
			acWantAssess(t, p, stateDSOUnbound, structureClean)
		})
	}
}

func TestAssessActive_UnknownVersionEvidence_MakesMigrationDefinedTablesInconclusive(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		"no-version-rows":      acVersions(),
		"version-zero-only":    acVersions(0),
		"no-schema_migrations": {`DROP TABLE schema_migrations`},
	}
	for name, stmts := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := icUnbound(t)
			icEdit(t, path, stmts...)
			a := acWantAssess(t, path, stateInconclusive, structureUnknown)
			wantInconclusive(t, a.Err)
			if !strings.Contains(a.Err.Error(), "do not have its structure") {
				t.Fatalf("the diagnostic should say why: %v", a.Err)
			}
		})
	}
	t.Run("version-newer-than-supported-is-too-new", func(t *testing.T) {
		t.Parallel()
		path := icUnbound(t)
		icEdit(t, path, acVersions(1, 2, 3, 99)...)
		acWantAssess(t, path, stateTooNew, structureUnknown)
	})
	t.Run("table-not-defined-at-its-own-version", func(t *testing.T) {
		t.Parallel()
		// A schema-3 database that records only version 1: store_meta and
		// audit_events do not exist at version 1, so their names prove nothing.
		path := icUnbound(t)
		icEdit(t, path, acVersions(1)...)
		a := acWantAssess(t, path, stateInconclusive, structureMismatch)
		wantInconclusive(t, a.Err)
		if !strings.Contains(a.Err.Error(), "store_meta") || !strings.Contains(acReasons(a), "does not belong to schema version 1") {
			t.Fatalf("unexpected diagnostics: %v / %s", a.Err, acReasons(a))
		}
	})
}

// A synthetic schema whose version 3 changes projects: the "hypothetical newer
// schema" an older, legitimate database must not be judged against.
func acNewerSchema(t *testing.T) dsoSchema {
	t.Helper()
	real, err := dsoStructures()
	if err != nil {
		t.Fatal(err)
	}
	s := real
	s.byVersion[3]["projects"] += "archived_at INTEGER 0 0 0;"
	s.latest = s.byVersion[3]
	return s
}

func acEvidence(t *testing.T, path string) initEvidence {
	t.Helper()
	ev, err := collectEvidence(icQuerier(t, path))
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func TestTablesWithoutDSOStructure_UsesTheDatabasesOwnVersion(t *testing.T) {
	t.Parallel()
	newer := acNewerSchema(t)
	all, _ := sqlite.EmbeddedMigrations()
	open := func(n int) string {
		p := filepath.Join(securefstest.TempDir(t), "v.db")
		st, err := sqlite.Open(ctx, p, sqlite.Options{Migrations: all[:n]})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
		return p
	}
	t.Run("older-database-is-not-judged-by-the-newer-schema", func(t *testing.T) {
		t.Parallel()
		ev := acEvidence(t, open(2))
		if ev.columns["projects"] == newer.latest["projects"] {
			t.Fatal("the fixture must differ from the hypothetical latest schema, or this test proves nothing")
		}
		if bad := ev.tablesWithoutDSOStructure(newer); len(bad) != 0 {
			t.Fatalf("a valid schema-2 database was judged by the newer schema: %v", bad)
		}
		if f, reasons := ev.structureAgainst(newer); f != structureClean {
			t.Fatalf("structure = %v (%v)", f, reasons)
		}
	})
	t.Run("a-database-at-the-newer-version-must-match-it", func(t *testing.T) {
		t.Parallel()
		ev := acEvidence(t, open(3))
		bad := ev.tablesWithoutDSOStructure(newer)
		if len(bad) != 1 || bad[0] != "projects" {
			t.Fatalf("bad = %v, want [projects]", bad)
		}
		if f, _ := ev.structureAgainst(newer); f != structureMismatch {
			t.Fatalf("structure = %v", f)
		}
	})
	t.Run("without-a-valid-version-nothing-falls-back-to-latest", func(t *testing.T) {
		t.Parallel()
		ev := acEvidence(t, open(3))
		ev.versions = nil
		if bad := ev.tablesWithoutDSOStructure(newer); len(bad) == 0 {
			t.Fatal("migration-defined tables must be ambiguous without a valid recorded version, even if they match the latest schema")
		}
		for _, v := range [][]int{{0}, {42}, {-1}} {
			ev.versions = v
			if bad := ev.tablesWithoutDSOStructure(newer); len(bad) == 0 {
				t.Fatalf("recorded versions %v are not valid evidence", v)
			}
		}
	})
	t.Run("schema_migrations-ignores-the-recorded-version", func(t *testing.T) {
		t.Parallel()
		ev := acEvidence(t, open(3))
		ev.versions = nil
		for _, bad := range ev.tablesWithoutDSOStructure(newer) {
			if bad == "schema_migrations" {
				t.Fatal("schema_migrations is version-independent")
			}
		}
	})
}

func TestStructureAgainst_UsesTheEmbeddedSequence_NotConsecutiveNumbers(t *testing.T) {
	t.Parallel()
	real, err := dsoStructures()
	if err != nil {
		t.Fatal(err)
	}
	// An embedded sequence 1, 5, 9: the same three structures under sparse numbers.
	sparse := dsoSchema{
		byVersion:        map[int]map[string]string{1: real.byVersion[1], 5: real.byVersion[2], 9: real.byVersion[3]},
		versions:         []int{1, 5, 9},
		latest:           real.byVersion[3],
		schemaMigrations: real.schemaMigrations,
	}
	e := icBound(t, false)
	base := acEvidence(t, e.dbPath)
	for _, tc := range []struct {
		versions []int
		want     structuralFinding
		missing  []int
	}{
		{[]int{1, 5, 9}, structureClean, nil},
		{[]int{1, 9}, structureMismatch, []int{5}},
		{[]int{9}, structureMismatch, []int{1, 5}},
		{[]int{5, 9}, structureMismatch, []int{1}},
	} {
		ev := base
		ev.versions = tc.versions
		got, reasons := ev.structureAgainst(sparse)
		if got != tc.want {
			t.Fatalf("versions %v: %v (%v), want %v", tc.versions, got, reasons, tc.want)
		}
		text := strings.Join(reasons, "\n")
		for _, m := range tc.missing {
			if !strings.Contains(text, fmt.Sprintf("migration version %d is not recorded", m)) {
				t.Fatalf("versions %v: reasons lack version %d:\n%s", tc.versions, m, text)
			}
		}
		if strings.Contains(text, "version 2 ") || strings.Contains(text, "version 3 ") {
			t.Fatalf("versions %v: consecutive numbering was assumed:\n%s", tc.versions, text)
		}
	}
}

// ---- Phase 1.2b: schema_migrations alone is ownership evidence only while it is empty ---------------------------

const acSMDDL = `CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`

func TestAssessActive_SchemaMigrationsOnly_ZeroRowsRule(t *testing.T) {
	t.Parallel()
	rows := func(vs ...int) []string {
		out := []string{acSMDDL}
		for _, v := range vs {
			out = append(out, fmt.Sprintf(`INSERT INTO schema_migrations VALUES (%d, 0)`, v))
		}
		return out
	}
	cases := []struct {
		name  string
		stmts []string
		state activeState
		rows  int // rows expected in the diagnostic when Inconclusive because of rows
	}{
		{"exact-ddl-zero-rows", rows(), stateDSOUnbound, 0},
		{"exact-ddl-one-row-version-1", rows(1), stateInconclusive, 1},
		{"exact-ddl-three-arbitrary-rows", rows(20240101, 20240102, 20240103), stateInconclusive, 3},
		{"exact-ddl-version-999", rows(999), stateInconclusive, 1},
		{"exact-ddl-version-zero", rows(0), stateInconclusive, 1},
		{"exact-ddl-negative-version", rows(-1), stateInconclusive, 1},
		{"exact-ddl-all-embedded-versions-but-no-tables", rows(1, 2, 3), stateInconclusive, 3},
		{"malformed-extra-column-zero-rows", []string{`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL, extra TEXT)`}, stateInconclusive, 0},
		{"malformed-no-primary-key-zero-rows", []string{`CREATE TABLE schema_migrations(version INTEGER, applied_at INTEGER NOT NULL)`}, stateInconclusive, 0},
		{"malformed-text-version-zero-rows", []string{`CREATE TABLE schema_migrations(version TEXT PRIMARY KEY, applied_at INTEGER NOT NULL)`}, stateInconclusive, 0},
		{"malformed-structure-with-rows", []string{`CREATE TABLE schema_migrations(version INTEGER, applied_at INTEGER)`, `INSERT INTO schema_migrations VALUES (1, 0)`}, stateInconclusive, 0},
		{"zero-rows-plus-foreign-table-is-foreign", append(rows(), `CREATE TABLE customer(id)`), stateForeign, 0},
		{"rows-plus-foreign-table-is-foreign", append(rows(1, 2, 3), `CREATE TABLE customer(id)`), stateForeign, 0},
		{"zero-rows-plus-view-is-inconclusive", append(rows(), `CREATE VIEW v AS SELECT version FROM schema_migrations`), stateInconclusive, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := icForeign(t, tc.stmts...)
			a := acAssess(t, path)
			if a.State != tc.state {
				t.Fatalf("state = %v (err %v), want %v", a.State, a.Err, tc.state)
			}
			switch tc.state {
			case stateDSOUnbound:
				if ni := wantNotInit(t, a.Err); ni.Reason != ReasonNoSchema {
					t.Fatalf("reason = %q", ni.Reason)
				}
			case stateInconclusive:
				wantInconclusive(t, a.Err)
				if errors.Is(a.Err, ErrNotInitialized) {
					t.Fatalf("an inconclusive classification must not match ErrNotInitialized: %v", a.Err)
				}
				if tc.rows > 0 && !strings.Contains(a.Err.Error(), fmt.Sprintf("holds %d row(s)", tc.rows)) {
					t.Fatalf("the diagnostic should say the table holds rows: %v", a.Err)
				}
			case stateForeign:
				wantNotInit(t, a.Err)
			}
			if a.Structure == structureClean {
				t.Fatalf("a schema_migrations-only file can never be structurally clean (%v)", a.StructureReasons)
			}
		})
	}
}

func TestAssessActive_SchemaMigrationsOnly_RealInterruptedInit_IsStillDSOUnbound(t *testing.T) {
	t.Parallel()
	// The genuine article: the real Open created schema_migrations and was killed
	// (here: applied no migration) before the first migration transaction.
	path := filepath.Join(securefstest.TempDir(t), "c.db")
	st, err := sqlite.Open(ctx, path, sqlite.Options{Migrations: []sqlite.Migration{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := icQuerier(t, path).QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("a real interrupted initialization leaves an empty schema_migrations (%d rows, %v)", n, err)
	}
	acWantAssess(t, path, stateDSOUnbound, structureUnknown)
}

func TestAssessActive_SchemaMigrationsRows_DoNotAffectDatabasesThatHaveTheirSchema(t *testing.T) {
	t.Parallel()
	// The rule is only about schema_migrations standing alone. A database with the
	// DSO tables keeps its version rows, and every earlier outcome.
	acWantAssess(t, icUnbound(t), stateDSOUnbound, structureClean)
	e := icBound(t, false)
	acWantAssess(t, e.dbPath, stateInitialized, structureClean)
	p := icUnbound(t)
	icEdit(t, p, acVersions()...) // full DSO schema, history deleted: still Inconclusive
	acWantAssess(t, p, stateInconclusive, structureUnknown)
	// And a file with no tables at all keeps its approved Phase 1 classification.
	acWantAssess(t, icForeign(t, `CREATE TABLE x(a)`, `DROP TABLE x`), stateDSOUnbound, structureUnknown)
}

func TestCheckInitialized_SchemaMigrationsOnlyFiles_KeepTheirHistoricalFinding(t *testing.T) {
	t.Parallel()
	// CheckInitialized does not apply the zero-rows rule: both files are still the
	// historical not-initialized finding, with the historical text.
	for name, stmts := range map[string][]string{
		"zero-rows": {acSMDDL},
		"with-rows": {acSMDDL, `INSERT INTO schema_migrations VALUES (999, 0)`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := icForeign(t, stmts...)
			ni := wantNotInit(t, icCheck(t, path))
			want := fmt.Sprintf("database %s is not an initialized DSO Central database: %s (if another dso command may be writing to it, wait and retry)", path, ReasonNoSchema)
			if ni.Error() != want || !ni.Retry {
				t.Fatalf("CheckInitialized's finding changed:\n got %q (retry=%v)\nwant %q", ni.Error(), ni.Retry, want)
			}
		})
	}
}
