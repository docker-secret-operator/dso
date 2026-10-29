package sqlite

import (
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/docker-secret-operator/dso/internal/securefs/securefstest"
	"github.com/docker-secret-operator/dso/internal/store"
)

func latestSchema(t *testing.T) int {
	t.Helper()
	v, err := LatestSchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestValidateCandidate_ValidCandidateAccepted(t *testing.T) {
	s, _ := open(t)
	seedWithAudit(t, s)
	dst := filepath.Join(securefstest.TempDir(t), "cand.db")
	path, err := s.Backup(ctx, dst)
	if err != nil {
		t.Fatal(err)
	}

	info, err := ValidateCandidate(ctx, path, latestSchema(t))
	if err != nil {
		t.Fatalf("expected a valid candidate to be accepted: %v", err)
	}
	if info.EventCount != 2 {
		t.Fatalf("expected 2 audit events in the candidate info, got %d", info.EventCount)
	}
	if info.LastEvent == nil || info.LastEvent.ID != "ev2" {
		t.Fatalf("expected the candidate's last event to be ev2, got %+v", info.LastEvent)
	}
}

func TestValidateCandidate_InvalidSQLiteDatabaseRejected(t *testing.T) {
	dir := securefstest.TempDir(t)
	path := filepath.Join(dir, "not-a-db.bak")
	if err := os.WriteFile(path, []byte("this is not a sqlite file at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateCandidate(ctx, path, 99); err == nil {
		t.Fatal("expected a non-SQLite file to be rejected")
	}
}

func TestValidateCandidate_EmptyFileRejected(t *testing.T) {
	dir := securefstest.TempDir(t)
	path := filepath.Join(dir, "empty.db")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateCandidate(ctx, path, 99); err == nil {
		t.Fatal("expected an empty file to be rejected")
	}
}

func TestValidateCandidate_CorruptedAuditChainRejected(t *testing.T) {
	s, _ := open(t)
	if _, err := s.AppendAuditEvent(ctx, ev("e1", store.AuditInit)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendAuditEvent(ctx, ev("e2", store.AuditProjectCreate)); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(securefstest.TempDir(t), "cand.db")
	path, err := s.Backup(ctx, dst)
	if err != nil {
		t.Fatal(err)
	}

	// Tamper with the candidate's audit chain directly via a plain
	// read-write SQL connection (outside any Store API) to simulate a
	// broken chain, then validate the resulting file.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DROP TRIGGER IF EXISTS audit_events_no_update`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE audit_events SET event_hash = X'DEADBEEF' WHERE id = 'e2'`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	if _, err := ValidateCandidate(ctx, path, latestSchema(t)); err == nil {
		t.Fatal("expected a tampered audit chain to be rejected")
	}
}

func TestValidateCandidate_UnsupportedNewerSchemaRejected(t *testing.T) {
	s, _ := open(t)
	dst := filepath.Join(securefstest.TempDir(t), "cand.db")
	path, err := s.Backup(ctx, dst)
	if err != nil {
		t.Fatal(err)
	}
	// maxSupportedSchema set below the candidate's own (real) schema version
	// simulates a candidate newer than this binary supports.
	_, err = ValidateCandidate(ctx, path, 0)
	if err == nil {
		t.Fatal("expected a candidate newer than the supported schema to be rejected")
	}
	if !isStoreErrSchemaTooNew(err) {
		t.Fatalf("expected store.ErrSchemaTooNew, got %v", err)
	}
}

// TestValidateCandidate_WALModeCandidateWithPendingFramesIsNeverModified is a
// regression test for a confirmed defect found during the Step 5.4 audit
// (F3): opening a WAL-mode candidate with genuinely pending (uncheckpointed)
// frames through a plain PRAGMA query_only connection measurably checkpoints
// and deletes its -wal/-shm companions -- even though nothing in that
// connection ever executes a write statement. This test constructs exactly
// that candidate (a live store with wal_autocheckpoint disabled and real
// uncommitted-to-main-file writes, copied out while still open) and asserts
// its -wal/-shm bytes are byte-for-byte unchanged after ValidateCandidate.
func TestValidateCandidate_WALModeCandidateWithPendingFramesIsNeverModified(t *testing.T) {
	s, livePath := open(t)
	seed(t, s)
	if _, err := s.db.Exec(`PRAGMA wal_autocheckpoint=0`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		id := "wal-env-" + strconv.Itoa(i)
		if err := s.CreateEnvironment(ctx, store.Environment{ID: id, ProjectID: "p1", Slug: "wal-slug-" + strconv.Itoa(i), Name: "n"}); err != nil {
			t.Fatal(err)
		}
	}

	candDir := securefstest.TempDir(t)
	cand := filepath.Join(candDir, "candidate.db")
	var walBefore, shmBefore []byte
	for _, suffix := range []string{"", "-wal", "-shm"} {
		b, err := os.ReadFile(livePath + suffix)
		if err != nil {
			t.Fatalf("expected a real %s companion before checkpoint: %v", suffix, err)
		}
		if err := os.WriteFile(cand+suffix, b, 0o600); err != nil {
			t.Fatal(err)
		}
		switch suffix {
		case "-wal":
			walBefore = b
		case "-shm":
			shmBefore = b
		}
	}
	if len(walBefore) == 0 {
		t.Fatal("test setup failed to produce a non-empty -wal file to validate against")
	}
	s.db.Close()

	if _, err := ValidateCandidate(ctx, cand, latestSchema(t)); err != nil {
		t.Fatalf("expected the WAL-mode candidate to validate successfully: %v", err)
	}

	walAfter, err := os.ReadFile(cand + "-wal")
	if err != nil {
		t.Fatalf("expected the candidate's -wal file to still exist after validation, got: %v", err)
	}
	if string(walAfter) != string(walBefore) {
		t.Fatal("ValidateCandidate must not modify the candidate's -wal file")
	}
	shmAfter, err := os.ReadFile(cand + "-shm")
	if err != nil {
		t.Fatalf("expected the candidate's -shm file to still exist after validation, got: %v", err)
	}
	if string(shmAfter) != string(shmBefore) {
		t.Fatal("ValidateCandidate must not modify the candidate's -shm file")
	}
}

func isStoreErrSchemaTooNew(err error) bool {
	for e := err; e != nil; {
		if e == store.ErrSchemaTooNew {
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}
