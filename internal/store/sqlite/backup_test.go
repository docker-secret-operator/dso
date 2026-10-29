package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/securefs/securefstest"
	"github.com/docker-secret-operator/dso/internal/store"
)

// seedWithAudit creates a project/environment/secret and a couple of audit
// events, exercising every table a backup must cover.
func seedWithAudit(t *testing.T, s *Store) {
	t.Helper()
	proj, env := seed(t, s)
	if err := s.CreateSecret(ctx, store.NewSecret{
		ID: "sec1", ProjectID: proj.ID, EnvironmentID: env.ID, Name: "db-password",
		Ciphertext: []byte("ct"), Nonce: []byte("abcdefghijkl"), DEKVersion: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendAuditEvent(ctx, ev("ev1", store.AuditProjectCreate)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendAuditEvent(ctx, ev("ev2", store.AuditSecretCreate)); err != nil {
		t.Fatal(err)
	}
}

func TestBackup_SuccessfulBackupContainsEveryTableAndVerifies(t *testing.T) {
	s, _ := open(t)
	seedWithAudit(t, s)

	dst := filepath.Join(securefstest.TempDir(t), "out", "snapshot.db")
	path, err := s.Backup(ctx, dst)
	if err != nil {
		t.Fatal(err)
	}
	if path != dst {
		t.Fatalf("expected backup path %s, got %s", dst, path)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("expected backup mode 0600, got %04o", fi.Mode().Perm())
	}

	// Reopenability: the backup must itself open cleanly as a normal store,
	// and every seeded row must be present.
	restored, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatalf("backup did not reopen cleanly: %v", err)
	}
	defer restored.Close()

	if _, err := restored.GetProjectBySlug(ctx, "proj"); err != nil {
		t.Fatalf("project missing from backup: %v", err)
	}
	sec, err := restored.GetSecret(ctx, "e1", "db-password")
	if err != nil {
		t.Fatalf("secret missing from backup: %v", err)
	}
	if sec.ID != "sec1" {
		t.Fatalf("unexpected secret id %q", sec.ID)
	}
	events, err := restored.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected the complete audit chain (2 events) in the backup, got %d", len(events))
	}
	if err := VerifyAuditChain(events); err != nil {
		t.Fatalf("backup's audit chain must verify cleanly: %v", err)
	}
}

func TestBackup_EmptyStoreBackupSucceedsAndReopens(t *testing.T) {
	s, _ := open(t)
	dst := filepath.Join(securefstest.TempDir(t), "out", "empty.db")
	path, err := s.Backup(ctx, dst)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatalf("empty backup did not reopen cleanly: %v", err)
	}
	defer restored.Close()
	events, err := restored.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("expected no events, got %d", len(events))
	}
}

func TestBackup_DestinationAlreadyExists(t *testing.T) {
	s, _ := open(t)
	dir := securefstest.TempDir(t)
	dst := filepath.Join(dir, "existing.db")
	if err := os.WriteFile(dst, []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Backup(ctx, dst); !errors.Is(err, ErrBackupDestinationExists) {
		t.Fatalf("expected ErrBackupDestinationExists, got %v", err)
	}
	// The pre-existing file must be untouched.
	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "not a database" {
		t.Fatal("Backup must not overwrite an existing destination file")
	}
}

func TestBackup_UnsafeDestinationDirectoryRejected(t *testing.T) {
	s, _ := open(t)
	base := securefstest.TempDir(t)
	unsafe := filepath.Join(base, "unsafe")
	if err := os.Mkdir(unsafe, 0o777); err != nil { // #nosec G301 -- deliberately unsafe for this test
		t.Fatal(err)
	}
	// os.Mkdir's requested mode is reduced by the process umask; force the
	// exact mode so this test does not depend on the umask in effect.
	if err := os.Chmod(unsafe, 0o777); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(unsafe, "out.db")
	if _, err := s.Backup(ctx, dst); err == nil {
		t.Fatal("expected Backup to reject a group/world-writable destination directory")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("Backup must not have written anything into an unsafe directory")
	}
}

func TestBackup_FailureDuringVacuumLeavesNoPartialFile(t *testing.T) {
	s, _ := open(t)
	dir := securefstest.TempDir(t)
	dst := filepath.Join(dir, "conflict.db")
	// A cancelled context makes VACUUM INTO's ExecContext fail deterministically
	// after the destination has already been exclusively created, exercising
	// the cleanup path without relying on timing or real disk exhaustion.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.Backup(cctx, dst); err == nil {
		t.Fatal("expected Backup to fail with a cancelled context")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("a failed backup must not leave a partial file at the destination")
	}
}

func TestBackup_CleanupDoesNotDamageUnrelatedFiles(t *testing.T) {
	s, _ := open(t)
	dir := securefstest.TempDir(t)
	unrelated := filepath.Join(dir, "unrelated.txt")
	if err := os.WriteFile(unrelated, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	dst := filepath.Join(dir, "failed.db")
	if _, err := s.Backup(cctx, dst); err == nil {
		t.Fatal("expected failure")
	}
	b, err := os.ReadFile(unrelated)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "keep me" {
		t.Fatal("backup failure cleanup must not touch unrelated files")
	}
}

func TestBackup_ConsistentDuringInFlightWriteTransaction(t *testing.T) {
	s, _ := open(t)
	dst := filepath.Join(securefstest.TempDir(t), "concurrent.db")

	var wg sync.WaitGroup
	writeStarted := make(chan struct{})
	writeDone := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = s.WithTx(ctx, func(ctx context.Context, tx store.Tx) error {
			if err := tx.CreateProject(ctx, store.Project{ID: "pX", Slug: "concurrent", Name: "C", WrappedDEK: []byte("w"), DEKKeyID: "k1"}); err != nil {
				return err
			}
			close(writeStarted)
			<-writeDone
			return nil
		})
	}()
	<-writeStarted
	time.Sleep(20 * time.Millisecond)

	path2, err := s.Backup(ctx, dst)
	close(writeDone)
	wg.Wait()
	if err != nil {
		t.Fatalf("backup during an in-flight write transaction should still succeed: %v", err)
	}

	restored, err := Open(ctx, path2, Options{})
	if err != nil {
		t.Fatalf("backup produced during a concurrent write did not reopen cleanly: %v", err)
	}
	defer restored.Close()
	var integrity string
	if err := restored.db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("backup taken during a concurrent write transaction failed integrity_check: %q, %v", integrity, err)
	}
	// The concurrent writer's uncommitted project must not appear: VACUUM
	// INTO's own read transaction must see a single consistent point in time
	// that does not include a transaction still in flight.
	if _, err := restored.GetProjectBySlug(ctx, "concurrent"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected the uncommitted concurrent write to be absent from the backup, got err=%v", err)
	}
}

func TestSafetySnapshot_NamingAndReopenability(t *testing.T) {
	s, path := open(t)
	seedWithAudit(t, s)
	snap, err := s.SafetySnapshot(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(snap) != filepath.Dir(path) {
		t.Fatalf("expected safety snapshot beside the active database, got %s", snap)
	}
	restored, err := Open(ctx, snap, Options{})
	if err != nil {
		t.Fatalf("safety snapshot did not reopen cleanly: %v", err)
	}
	defer restored.Close()
	if _, err := restored.GetProjectBySlug(ctx, "proj"); err != nil {
		t.Fatalf("safety snapshot missing seeded data: %v", err)
	}
}
