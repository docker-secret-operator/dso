package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/docker-secret-operator/dso/internal/securefs/securefstest"
	"time"

	"github.com/docker-secret-operator/dso/internal/store"
)

var ctx = context.Background()

func open(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(securefstest.TempDir(t), "db", "central.db")
	s, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

// seed creates project p1 / env e1 and returns ids.
func seed(t *testing.T, s *Store) (proj store.Project, env store.Environment) {
	t.Helper()
	proj = store.Project{ID: "p1", Slug: "proj", Name: "Proj", WrappedDEK: []byte("wrapped"), DEKKeyID: "k1"}
	if err := s.CreateProject(ctx, proj); err != nil {
		t.Fatal(err)
	}
	env = store.Environment{ID: "e1", ProjectID: "p1", Slug: "prod", Name: "Prod"}
	if err := s.CreateEnvironment(ctx, env); err != nil {
		t.Fatal(err)
	}
	return
}

func newSecret(id, name string) store.NewSecret {
	return store.NewSecret{ID: id, ProjectID: "p1", EnvironmentID: "e1", Name: name, CreatedBy: "t", DEKVersion: 1,
		Ciphertext: []byte("ct-" + id), Nonce: make([]byte, 12)}
}

func sealN(tag string) store.SealFunc {
	return func(v int) ([]byte, []byte, int, error) {
		return []byte(fmt.Sprintf("%s-v%d", tag, v)), make([]byte, 12), 1, nil
	}
}

func TestSchemaInitialisation(t *testing.T) {
	s, path := open(t)
	if v, err := s.SchemaVersion(ctx); err != nil || v != curV {
		t.Fatalf("schema version %d, %v", v, err)
	}
	var mode string
	s.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode)
	if mode != "wal" {
		t.Fatalf("journal_mode %s", mode)
	}
	var fk int
	s.db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk)
	if fk != 1 {
		t.Fatal("foreign keys off")
	}
	var sync int
	s.db.QueryRow(`PRAGMA synchronous`).Scan(&sync)
	if sync != 2 { // FULL
		t.Fatalf("synchronous=%d, want FULL(2)", sync)
	}
	for _, tbl := range []string{"projects", "environments", "secrets", "secret_versions", "schema_migrations", "store_meta"} {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&n); err != nil || n != 1 {
			t.Fatalf("table %s missing", tbl)
		}
	}
	// db, and the WAL/SHM companions once a write has happened, are 0600 in a 0700 dir
	seed(t, s)
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if fi.Mode().Perm()&0o077 != 0 {
			t.Fatalf("%s mode %04o is accessible by group/others", p, fi.Mode().Perm())
		}
	}
	if di, _ := os.Stat(filepath.Dir(path)); di.Mode().Perm() != 0o700 {
		t.Fatalf("db dir mode %04o", di.Mode().Perm())
	}
}

func TestOpenRefusesInsecureOrInvalidFiles(t *testing.T) {
	d := securefstest.TempDir(t)
	loose := filepath.Join(d, "loose.db")
	os.WriteFile(loose, nil, 0o644)
	os.Chmod(loose, 0o644)
	if _, err := Open(ctx, loose, Options{}); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("world-readable db accepted: %v", err)
	}
	garbage := filepath.Join(d, "garbage.db")
	os.WriteFile(garbage, []byte(strings.Repeat("this is not a database ", 100)), 0o600)
	if _, err := Open(ctx, garbage, Options{}); err == nil {
		t.Fatal("garbage file accepted")
	}
	if _, err := Open(ctx, d, Options{}); err == nil {
		t.Fatal("directory accepted as database")
	}
	sym := filepath.Join(d, "sym.db")
	os.Symlink(garbage, sym)
	if _, err := Open(ctx, sym, Options{}); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestPathCannotInjectDSNOptions(t *testing.T) {
	d := filepath.Join(securefstest.TempDir(t), "a?b#c%d")
	path := filepath.Join(d, "x.db?mode=ro&_pragma=foreign_keys(0)")
	s, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	seed(t, s) // would fail on a read-only or FK-disabled-misparsed open
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database not created at the literal path: %v", err)
	}
	var fk int
	s.db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk)
	if fk != 1 {
		t.Fatal("path injected a pragma")
	}
}

func TestRestartPersistence(t *testing.T) {
	s, path := open(t)
	seed(t, s)
	if err := s.CreateSecret(ctx, newSecret("s1", "A")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddVersion(ctx, "s1", "t", "second", sealN("x")); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	m, err := s2.GetSecret(ctx, "e1", "A")
	if err != nil || m.LatestVersion != 2 || m.VersionCount != 2 {
		t.Fatalf("after restart: %+v %v", m, err)
	}
	rec, err := s2.GetVersion(ctx, "s1", 2)
	if err != nil || string(rec.Ciphertext) != "x-v2" || rec.Comment != "second" {
		t.Fatalf("version lost: %+v %v", rec, err)
	}
	if v, _ := s2.SchemaVersion(ctx); v != curV {
		t.Fatal("schema version not persisted")
	}
}

func TestCrashRecoveryUncheckpointedWAL(t *testing.T) {
	// Write, then abandon the handle without Close (no checkpoint), and reopen.
	path := filepath.Join(securefstest.TempDir(t), "c.db")
	s, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	seed(t, s)
	s.CreateSecret(ctx, newSecret("s1", "A"))
	s.db.Close() // closes connections but skips our checkpoint
	s2, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if _, err := s2.GetSecret(ctx, "e1", "A"); err != nil {
		t.Fatalf("committed data lost: %v", err)
	}
}

// ---- migrations -------------------------------------------------------------

func TestMigrationUpgradePreservesDataAndBacksUp(t *testing.T) {
	path := filepath.Join(securefstest.TempDir(t), "m.db")
	s, _ := Open(ctx, path, Options{})
	seed(t, s)
	s.CreateSecret(ctx, newSecret("s1", "A"))
	s.Close()

	embedded, _ := EmbeddedMigrations()
	more := append(append([]Migration{}, embedded...), Migration{Version: curV + 1, SQL: `ALTER TABLE projects ADD COLUMN note TEXT NOT NULL DEFAULT ''`})
	s2, err := Open(ctx, path, Options{Migrations: more})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if v, _ := s2.SchemaVersion(ctx); v != curV+1 {
		t.Fatalf("version %d", v)
	}
	if _, err := s2.GetSecret(ctx, "e1", "A"); err != nil {
		t.Fatalf("data lost in migration: %v", err)
	}
	bak := s2.LastMigrationBackup()
	if !strings.HasPrefix(bak, path+".pre-migration-v"+strconv.Itoa(curV)+"-") || !strings.HasSuffix(bak, ".bak") {
		t.Fatalf("unexpected backup name %q", bak)
	}
	fi, err := os.Stat(bak)
	if err != nil {
		t.Fatalf("no pre-migration backup: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode %04o", fi.Mode().Perm())
	}
	b, err := Open(ctx, bak, Options{})
	if err != nil {
		t.Fatalf("backup unusable: %v", err)
	}
	defer b.Close()
	if v, _ := b.SchemaVersion(ctx); v != curV {
		t.Fatalf("backup should be at the old schema, got %d", v)
	}
}

func TestFailedMigrationRollsBackCompletely(t *testing.T) {
	path := filepath.Join(securefstest.TempDir(t), "f.db")
	s, _ := Open(ctx, path, Options{})
	seed(t, s)
	s.Close()
	embedded, _ := EmbeddedMigrations()
	bad := append(append([]Migration{}, embedded...), Migration{Version: curV + 1, SQL: `
		ALTER TABLE projects ADD COLUMN half_applied TEXT;
		INSERT INTO no_such_table VALUES (1);`})
	if _, err := Open(ctx, path, Options{Migrations: bad}); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("migration %d", curV+1)) {
		t.Fatalf("expected migration failure naming version 2, got %v", err)
	}
	s2, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if v, _ := s2.SchemaVersion(ctx); v != curV {
		t.Fatalf("version advanced to %d after failed migration", v)
	}
	if _, err := s2.db.Exec(`SELECT half_applied FROM projects`); err == nil {
		t.Fatal("partial DDL from failed migration persisted")
	}
	if _, err := s2.GetProjectBySlug(ctx, "proj"); err != nil {
		t.Fatalf("existing data damaged: %v", err)
	}
}

func TestNewerSchemaIsRefusedAndUntouched(t *testing.T) {
	path := filepath.Join(securefstest.TempDir(t), "n.db")
	embedded, _ := EmbeddedMigrations()
	newer := append(append([]Migration{}, embedded...), Migration{Version: curV + 1, SQL: `CREATE TABLE future(x)`})
	s, err := Open(ctx, path, Options{Migrations: newer})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	_, err = Open(ctx, path, Options{}) // "old binary"
	if !errors.Is(err, store.ErrSchemaTooNew) {
		t.Fatalf("expected ErrSchemaTooNew, got %v", err)
	}
	if m, _ := filepath.Glob(path + ".pre-migration-*"); len(m) != 0 {
		t.Fatal("refusal created a backup / modified the database")
	}
}

func TestMigrationListValidation(t *testing.T) {
	p := func() string { return filepath.Join(securefstest.TempDir(t), "v.db") }
	for name, ms := range map[string][]Migration{
		"starts-at-2": {{Version: 2, SQL: "SELECT 1"}},
		"duplicate":   {{Version: 1, SQL: "SELECT 1"}, {Version: 1, SQL: "SELECT 1"}},
		"decreasing":  {{Version: 1, SQL: "SELECT 1"}, {Version: 3, SQL: "SELECT 1"}, {Version: 2, SQL: "SELECT 1"}},
	} {
		if _, err := Open(ctx, p(), Options{Migrations: ms}); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestEmbeddedMigrationsAreSequential(t *testing.T) {
	ms, err := EmbeddedMigrations()
	if err != nil || len(ms) == 0 {
		t.Fatal(err)
	}
	for i, m := range ms {
		if m.Version != i+1 {
			t.Fatalf("migration %d has version %d", i, m.Version)
		}
	}
}

// ---- constraints --------------------------------------------------------------

func TestUniquenessAndForeignKeys(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	if err := s.CreateProject(ctx, store.Project{ID: "p2", Slug: "proj", WrappedDEK: []byte("w"), DEKKeyID: "k"}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate slug: %v", err)
	}
	if err := s.CreateProject(ctx, store.Project{ID: "p1", Slug: "other", WrappedDEK: []byte("w"), DEKKeyID: "k"}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate id: %v", err)
	}
	if err := s.CreateEnvironment(ctx, store.Environment{ID: "e2", ProjectID: "p1", Slug: "prod"}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate env slug: %v", err)
	}
	if err := s.CreateEnvironment(ctx, store.Environment{ID: "e3", ProjectID: "nope", Slug: "x"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("env for missing project: %v", err)
	}
	if err := s.CreateSecret(ctx, newSecret("s1", "A")); err != nil {
		t.Fatal(err)
	}
	dup := newSecret("s2", "A")
	if err := s.CreateSecret(ctx, dup); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate live name: %v", err)
	}
	// a secret naming an environment that belongs to a different project is rejected by the composite FK
	s.CreateProject(ctx, store.Project{ID: "p2", Slug: "other", WrappedDEK: []byte("w"), DEKKeyID: "k"})
	cross := newSecret("s3", "B")
	cross.ProjectID = "p2"
	if err := s.CreateSecret(ctx, cross); err == nil {
		t.Fatal("secret accepted with environment from another project")
	}
	// invalid inputs
	for name, mut := range map[string]func(*store.NewSecret){
		"empty-ct": func(n *store.NewSecret) { n.Ciphertext = nil },
		"no-name":  func(n *store.NewSecret) { n.Name = "" },
		"bad-dek":  func(n *store.NewSecret) { n.DEKVersion = 0 },
	} {
		n := newSecret("sx-"+name, "X"+name)
		mut(&n)
		if err := s.CreateSecret(ctx, n); !errors.Is(err, store.ErrInvalid) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// short nonce is rejected by CHECK
	bad := newSecret("s4", "C")
	bad.Nonce = make([]byte, 5)
	if err := s.CreateSecret(ctx, bad); err == nil {
		t.Fatal("5-byte nonce accepted")
	}
	// parents cannot be deleted from under children
	if _, err := s.db.Exec(`DELETE FROM environments WHERE id='e1'`); err == nil {
		t.Fatal("environment with secrets deleted")
	}
	if _, err := s.db.Exec(`DELETE FROM projects WHERE id='p1'`); err == nil {
		t.Fatal("project with environments deleted")
	}
}

func TestSoftDeleteFreesNameAndKeepsVersions(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	s.CreateSecret(ctx, newSecret("s1", "A"))
	if err := s.DeleteSecret(ctx, "s1", "bob"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSecret(ctx, "e1", "A"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted secret still visible: %v", err)
	}
	if err := s.DeleteSecret(ctx, "s1", "bob"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("double delete: %v", err)
	}
	if _, err := s.GetLatestVersion(ctx, "s1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted secret readable by id: %v", err)
	}
	if _, err := s.AddVersion(ctx, "s1", "t", "", sealN("z")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("version added to deleted secret: %v", err)
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM secret_versions WHERE secret_id='s1'`).Scan(&n)
	if n != 1 {
		t.Fatal("versions not retained after soft delete")
	}
	if err := s.CreateSecret(ctx, newSecret("s2", "A")); err != nil {
		t.Fatalf("name not reusable after delete: %v", err)
	}
	if l, _ := s.ListSecrets(ctx, "e1"); len(l) != 1 || l[0].ID != "s2" {
		t.Fatalf("list: %+v", l)
	}
}

// ---- versions ---------------------------------------------------------------------

func TestVersionImmutabilityEnforcedByDatabase(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	s.CreateSecret(ctx, newSecret("s1", "A"))
	for _, stmt := range []string{
		`UPDATE secret_versions SET ciphertext = x'00' WHERE secret_id='s1'`,
		`UPDATE secret_versions SET nonce = x'000000000000000000000001' WHERE secret_id='s1'`,
		`UPDATE secret_versions SET version = 9 WHERE secret_id='s1'`,
		`UPDATE secret_versions SET comment = 'edited' WHERE secret_id='s1'`,
		`UPDATE secret_versions SET created_by = 'mallory' WHERE secret_id='s1'`,
		`UPDATE secret_versions SET dek_version = 2 WHERE secret_id='s1'`,
		`UPDATE secret_versions SET secret_id = 'other' WHERE secret_id='s1'`,
		`DELETE FROM secret_versions WHERE secret_id='s1'`,
	} {
		if _, err := s.db.Exec(stmt); err == nil {
			t.Fatalf("mutation allowed: %s", stmt)
		}
	}
	// the one permitted change is disabling; it is one-way and write-once
	if err := s.DisableVersion(ctx, "s1", 1, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE secret_versions SET disabled_at = NULL, disabled_by = NULL WHERE secret_id='s1'`); err == nil {
		t.Fatal("re-enabling a version allowed")
	}
	if _, err := s.db.Exec(`UPDATE secret_versions SET disabled_by = 'x' WHERE secret_id='s1'`); err == nil {
		t.Fatal("rewriting disabled_by allowed")
	}
	rec, err := s.ListVersions(ctx, "s1")
	if err != nil || rec[0].DisabledBy != "alice" || rec[0].DisabledAt == nil {
		t.Fatalf("%+v %v", rec, err)
	}
	if err := s.DisableVersion(ctx, "s1", 1, "bob"); err != nil { // idempotent, does not overwrite
		t.Fatal(err)
	}
	rec, _ = s.ListVersions(ctx, "s1")
	if rec[0].DisabledBy != "alice" {
		t.Fatal("second disable overwrote attribution")
	}
}

func TestVersionRetrievalAndDisable(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	s.CreateSecret(ctx, newSecret("s1", "A"))
	for i := 2; i <= 3; i++ {
		v, err := s.AddVersion(ctx, "s1", "t", "c"+strconv.Itoa(i), sealN("a"))
		if err != nil || v != i {
			t.Fatalf("version %d: %v", v, err)
		}
	}
	if r, err := s.GetLatestVersion(ctx, "s1"); err != nil || r.Version != 3 {
		t.Fatalf("latest: %+v %v", r, err)
	}
	if r, err := s.GetVersion(ctx, "s1", 1); err != nil || string(r.Ciphertext) != "ct-s1" {
		t.Fatalf("v1: %+v %v", r, err)
	}
	if _, err := s.GetVersion(ctx, "s1", 99); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing version: %v", err)
	}
	if err := s.DisableVersion(ctx, "s1", 3, "t"); err != nil {
		t.Fatal(err)
	}
	// Review finding M1-6: disabling the newest version must not silently serve
	// an older one (it may be the very credential being replaced).
	if r, err := s.GetLatestVersion(ctx, "s1"); !errors.Is(err, store.ErrLatestVersionDisabled) || len(r.Ciphertext) != 0 {
		t.Fatalf("latest with a disabled newest version must fail closed: %+v %v", r, err)
	}
	if r, err := s.GetVersion(ctx, "s1", 3); !errors.Is(err, store.ErrVersionDisabled) || len(r.Ciphertext) != 0 {
		t.Fatalf("disabled version returned data: %+v %v", r, err)
	}
	if m, _ := s.GetSecret(ctx, "e1", "A"); m.LatestVersion != 3 || !m.LatestVersionDisabled || m.VersionCount != 3 {
		t.Fatalf("meta %+v", m)
	}
	s.DisableVersion(ctx, "s1", 2, "t")
	s.DisableVersion(ctx, "s1", 1, "t")
	if _, err := s.GetLatestVersion(ctx, "s1"); !errors.Is(err, store.ErrLatestVersionDisabled) {
		t.Fatalf("all disabled: %v", err)
	}
	// a new version after all are disabled gets the next number, never reuses one
	if v, err := s.AddVersion(ctx, "s1", "t", "", sealN("a")); err != nil || v != 4 {
		t.Fatalf("version number reused or failed: %d %v", v, err)
	}
	// ...and becomes the readable latest again
	if r, err := s.GetLatestVersion(ctx, "s1"); err != nil || r.Version != 4 {
		t.Fatalf("new version not served as latest: %+v %v", r, err)
	}
	// an older ENABLED version never substitutes for a disabled newest one
	s.DisableVersion(ctx, "s1", 4, "t")
	if _, err := s.GetLatestVersion(ctx, "s1"); !errors.Is(err, store.ErrLatestVersionDisabled) {
		t.Fatalf("fell back after disabling v4: %v", err)
	}
	if err := s.DisableVersion(ctx, "s1", 42, "t"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("disable missing: %v", err)
	}
	if err := s.DisableVersion(ctx, "ghost", 1, "t"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("disable on missing secret: %v", err)
	}
}

// ---- transactions ---------------------------------------------------------------------

func TestTransactionRollbackOnFailure(t *testing.T) {
	s, _ := open(t)
	seed(t, s)

	// CreateSecret: the version insert fails (bad nonce size) after the secret
	// row was inserted -> neither row may survive.
	bad := newSecret("s1", "A")
	bad.Nonce = make([]byte, 3)
	if err := s.CreateSecret(ctx, bad); err == nil {
		t.Fatal("expected failure")
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM secrets`).Scan(&n)
	if n != 0 {
		t.Fatal("secret row survived a failed CreateSecret")
	}
	// ...and the name is still usable
	if err := s.CreateSecret(ctx, newSecret("s2", "A")); err != nil {
		t.Fatal(err)
	}

	// AddVersion: sealing fails -> no row, number not consumed
	boom := errors.New("seal exploded")
	if _, err := s.AddVersion(ctx, "s2", "t", "", func(int) ([]byte, []byte, int, error) { return nil, nil, 0, boom }); !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
	// AddVersion: row-level failure (bad nonce) -> rolled back
	if _, err := s.AddVersion(ctx, "s2", "t", "", func(int) ([]byte, []byte, int, error) { return []byte("c"), []byte("n"), 1, nil }); err == nil {
		t.Fatal("expected CHECK failure")
	}
	// AddVersion: seal panics -> transaction must not wedge the store
	func() {
		defer func() { recover() }()
		s.AddVersion(ctx, "s2", "t", "", func(int) ([]byte, []byte, int, error) { panic("boom") })
	}()
	if v, err := s.AddVersion(ctx, "s2", "t", "", sealN("ok")); err != nil || v != 2 {
		t.Fatalf("version after failures: %d %v (store wedged or number consumed)", v, err)
	}
	// cancelled context mid-flight
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.AddVersion(cctx, "s2", "t", "", sealN("x")); err == nil {
		t.Fatal("cancelled context succeeded")
	}
	vs, _ := s.ListVersions(ctx, "s2")
	if len(vs) != 2 {
		t.Fatalf("versions: %d", len(vs))
	}
}

// ---- concurrency --------------------------------------------------------------------------

func TestConcurrentWritersNoLostUpdate(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	s.CreateSecret(ctx, newSecret("s1", "A"))
	const writers, each = 16, 10
	var wg sync.WaitGroup
	var mu sync.Mutex
	got := map[int]bool{}
	errc := make(chan error, writers*each)
	stop := make(chan struct{})
	var rwg sync.WaitGroup
	for r := 0; r < 4; r++ { // readers run during the writes
		rwg.Add(1)
		go func() {
			defer rwg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := s.GetLatestVersion(ctx, "s1"); err != nil {
					errc <- fmt.Errorf("reader: %w", err)
					return
				}
				if _, err := s.ListSecrets(ctx, "e1"); err != nil {
					errc <- fmt.Errorf("reader list: %w", err)
					return
				}
			}
		}()
	}
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				v, err := s.AddVersion(ctx, "s1", "t", "", sealN("w"))
				if err != nil {
					errc <- err
					return
				}
				mu.Lock()
				if got[v] {
					errc <- fmt.Errorf("version %d assigned twice", v)
				}
				got[v] = true
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	close(stop)
	rwg.Wait()
	close(errc)
	for err := range errc {
		t.Fatal(err)
	}
	total := writers*each + 1
	if len(got) != writers*each {
		t.Fatalf("%d distinct versions, want %d", len(got), writers*each)
	}
	vs, _ := s.ListVersions(ctx, "s1")
	if len(vs) != total {
		t.Fatalf("%d rows, want %d", len(vs), total)
	}
	for i, v := range vs {
		if v.Version != i+1 {
			t.Fatalf("gap or reorder at %d: version %d", i, v.Version)
		}
	}
}

func TestConcurrentCreatesOfSameNameExactlyOneWins(t *testing.T) {
	s, _ := open(t)
	seed(t, s)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, conflict := 0, 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := s.CreateSecret(ctx, newSecret("id"+strconv.Itoa(i), "same"))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, store.ErrConflict):
				conflict++
			default:
				t.Errorf("unexpected: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if ok != 1 || conflict != 19 {
		t.Fatalf("ok=%d conflict=%d", ok, conflict)
	}
}

// Multi-process: separate OS processes (each with its own SQLite handle) hammer
// one database; then one process is SIGKILLed mid-write. No update may be lost
// or duplicated and the database must stay consistent.

func TestHelperProcessWriter(t *testing.T) {
	path := os.Getenv("DSO_TEST_DB")
	if path == "" {
		t.Skip("helper process")
	}
	n, _ := strconv.Atoi(os.Getenv("DSO_TEST_N"))
	s, err := Open(ctx, path, Options{BusyTimeout: 30 * time.Second})
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(2)
	}
	for i := 0; n == 0 || i < n; i++ {
		if _, err := s.AddVersion(ctx, "s1", "proc", "", sealN("p")); err != nil {
			fmt.Fprintln(os.Stderr, "add:", err)
			os.Exit(3)
		}
	}
	s.Close()
}

func helper(t *testing.T, path string, n int) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcessWriter$")
	cmd.Env = append(os.Environ(), "DSO_TEST_DB="+path, "DSO_TEST_N="+strconv.Itoa(n))
	cmd.Stderr = os.Stderr
	return cmd
}

func TestMultiProcessWritersNoLostUpdate(t *testing.T) {
	s, path := open(t)
	seed(t, s)
	s.CreateSecret(ctx, newSecret("s1", "A"))
	const procs, each = 4, 15
	var cmds []*exec.Cmd
	for i := 0; i < procs; i++ {
		c := helper(t, path, each)
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		cmds = append(cmds, c)
	}
	for _, c := range cmds {
		if err := c.Wait(); err != nil {
			t.Fatalf("helper failed: %v", err)
		}
	}
	vs, err := s.ListVersions(ctx, "s1")
	if err != nil || len(vs) != procs*each+1 {
		t.Fatalf("versions %d, want %d (%v)", len(vs), procs*each+1, err)
	}
	for i, v := range vs {
		if v.Version != i+1 {
			t.Fatalf("gap at %d: %d", i, v.Version)
		}
	}
}

func TestCrashDuringWritesLeavesConsistentDatabase(t *testing.T) {
	s, path := open(t)
	seed(t, s)
	s.CreateSecret(ctx, newSecret("s1", "A"))
	c := helper(t, path, 0) // writes until killed
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		m, _ := s.GetSecret(ctx, "e1", "A")
		if m.VersionCount > 20 {
			break
		}
		if time.Now().After(deadline) {
			c.Process.Kill()
			t.Fatal("helper made no progress")
		}
		time.Sleep(5 * time.Millisecond)
	}
	c.Process.Signal(syscall.SIGKILL)
	c.Wait()
	s.Close()

	s2, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatalf("reopen after crash: %v", err)
	}
	defer s2.Close()
	var res string
	if err := s2.db.QueryRow(`PRAGMA integrity_check`).Scan(&res); err != nil || res != "ok" {
		t.Fatalf("integrity_check: %q %v", res, err)
	}
	vs, _ := s2.ListVersions(ctx, "s1")
	if len(vs) < 21 {
		t.Fatalf("committed versions lost: %d", len(vs))
	}
	for i, v := range vs {
		if v.Version != i+1 {
			t.Fatalf("gap after crash at %d: %d", i, v.Version)
		}
	}
	if _, err := s2.AddVersion(ctx, "s1", "t", "", sealN("after")); err != nil {
		t.Fatalf("cannot write after crash: %v", err)
	}
}

func TestCloseIsCleanAndFurtherUseFails(t *testing.T) {
	s, path := open(t)
	seed(t, s)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListProjects(ctx); err == nil {
		t.Fatal("use after close succeeded")
	}
	// WAL is checkpointed on clean close: the -wal file is empty or gone
	if fi, err := os.Stat(path + "-wal"); err == nil && fi.Size() != 0 {
		t.Fatalf("WAL not truncated on close: %d bytes", fi.Size())
	}
	var _ sql.DB
}
