package sqlite

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/docker-secret-operator/dso/internal/securefs/securefstest"
)

func v2(sql string) []Migration {
	emb, _ := EmbeddedMigrations()
	return append(append([]Migration{}, emb...), Migration{Version: curV + 1, SQL: sql})
}

func backups(t *testing.T, path string) []string {
	t.Helper()
	m, _ := filepath.Glob(path + ".pre-migration-*.bak")
	return m
}

// Regression for review finding M1-1: after a failed migration and further
// writes, a later successful migration reused the OLD backup, so the "pre-
// migration snapshot" silently lacked everything written in between.
func TestMigrationBackup_EveryAttemptGetsAFreshCompleteSnapshot(t *testing.T) {
	path := filepath.Join(securefstest.TempDir(t), "m.db")
	s, _ := Open(ctx, path, Options{})
	seed(t, s)
	s.CreateSecret(ctx, newSecret("early", "EARLY"))
	s.Close()

	// attempt 1 fails -> snapshot A taken before the failing change
	if _, err := Open(ctx, path, Options{Migrations: v2(`INSERT INTO no_such_table VALUES (1)`)}); err == nil {
		t.Fatal("expected migration failure")
	}
	bs := backups(t, path)
	if len(bs) != 1 {
		t.Fatalf("after failed attempt: %d backups", len(bs))
	}
	snapA := bs[0]

	// the operator keeps working on the still-valid v1 database
	s, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	s.CreateSecret(ctx, newSecret("late", "LATE"))
	s.Close()

	// attempt 2 succeeds -> must take snapshot B that includes LATE
	s, err = Open(ctx, path, Options{Migrations: v2(`ALTER TABLE projects ADD COLUMN x TEXT`)})
	if err != nil {
		t.Fatal(err)
	}
	snapB := s.LastMigrationBackup()
	s.Close()
	if snapB == "" || snapB == snapA {
		t.Fatalf("second migration reused backup %q", snapA)
	}
	if got := backups(t, path); len(got) != 2 {
		t.Fatalf("want 2 distinct backups, have %v", got)
	}

	has := func(bak, name string) bool {
		b, err := Open(ctx, bak, Options{})
		if err != nil {
			t.Fatalf("backup %s unusable: %v", bak, err)
		}
		defer b.Close()
		_, err = b.GetSecret(ctx, "e1", name)
		return err == nil
	}
	if !has(snapA, "EARLY") || has(snapA, "LATE") {
		t.Error("snapshot A should contain EARLY only")
	}
	if !has(snapB, "EARLY") || !has(snapB, "LATE") {
		t.Error("snapshot B (the real pre-migration state) is missing data written between the attempts")
	}
}

func TestMigrationBackup_IsPrivateCompleteAndMadeBeforeSchemaChange(t *testing.T) {
	path := filepath.Join(securefstest.TempDir(t), "m.db")
	s, _ := Open(ctx, path, Options{})
	seed(t, s)
	s.Close()
	s, err := Open(ctx, path, Options{Migrations: v2(`ALTER TABLE projects ADD COLUMN x TEXT`)})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	bak := s.LastMigrationBackup()
	fi, err := os.Stat(bak)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode %04o", fi.Mode().Perm())
	}
	// The backup must be at the OLD schema (taken before the change) and intact.
	b, err := Open(ctx, bak, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if v, _ := b.SchemaVersion(ctx); v != curV {
		t.Fatalf("backup schema %d, want the pre-migration 1", v)
	}
	if _, err := b.db.Exec(`SELECT x FROM projects`); err == nil {
		t.Fatal("backup already contains the migration's column")
	}
	if _, err := b.GetProjectBySlug(ctx, "proj"); err != nil {
		t.Fatalf("backup lost data: %v", err)
	}
}

// Restoration semantics: copying the pre-migration snapshot over the database
// returns to the old schema and data, and the migration can run again.
func TestMigrationBackup_RestoreThenMigrateAgain(t *testing.T) {
	dir := securefstest.TempDir(t)
	path := filepath.Join(dir, "m.db")
	s, _ := Open(ctx, path, Options{})
	seed(t, s)
	s.CreateSecret(ctx, newSecret("s1", "A"))
	s.Close()
	good := v2(`ALTER TABLE projects ADD COLUMN x TEXT`)
	s, err := Open(ctx, path, Options{Migrations: good})
	if err != nil {
		t.Fatal(err)
	}
	bak := s.LastMigrationBackup()
	s.Close()

	data, err := os.ReadFile(bak)
	if err != nil {
		t.Fatal(err)
	}
	// operator restores: remove db + WAL companions, install the snapshot (0600)
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		os.Remove(p)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path, Options{}) // old binary: schema 1 is what it understands
	if err != nil {
		t.Fatalf("restored database does not open: %v", err)
	}
	if v, _ := s.SchemaVersion(ctx); v != curV {
		t.Fatalf("restored schema %d", v)
	}
	if _, err := s.GetSecret(ctx, "e1", "A"); err != nil {
		t.Fatalf("restored data missing: %v", err)
	}
	s.Close()
	s, err = Open(ctx, path, Options{Migrations: good}) // and it migrates again
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if v, _ := s.SchemaVersion(ctx); v != curV+1 {
		t.Fatalf("re-migrated schema %d", v)
	}
	if len(backups(t, path)) != 2 {
		t.Fatalf("re-migration should have taken its own backup, have %v", backups(t, path))
	}
}

func TestMigrationBackup_FreshDatabaseNeedsNoBackup(t *testing.T) {
	path := filepath.Join(securefstest.TempDir(t), "fresh.db")
	s, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.LastMigrationBackup() != "" || len(backups(t, path)) != 0 {
		t.Fatal("a brand-new database should not produce a backup")
	}
}

func TestMigrationBackup_FailureToBackUpPreventsMigration(t *testing.T) {
	dir := securefstest.TempDir(t)
	path := filepath.Join(dir, "m.db")
	s, _ := Open(ctx, path, Options{})
	seed(t, s)
	s.Close()
	// Make the directory read-only so the snapshot file cannot be created.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	_, err := Open(ctx, path, Options{Migrations: v2(`ALTER TABLE projects ADD COLUMN x TEXT`)})
	if err == nil {
		t.Fatal("migration proceeded without a backup")
	}
	os.Chmod(dir, 0o700)
	s2, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if v, _ := s2.SchemaVersion(ctx); v != curV {
		t.Fatalf("schema changed to %d despite the missing backup", v)
	}
}
