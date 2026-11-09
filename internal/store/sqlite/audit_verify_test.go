package sqlite

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/internal/securefs/securefstest"
	"github.com/docker-secret-operator/dso/internal/store"
)

// tamper runs stmts directly against s's database after dropping the
// append-only triggers, simulating someone with write access to the file.
func tamper(t *testing.T, s *Store, stmts ...string) {
	t.Helper()
	for _, q := range append([]string{
		`DROP TRIGGER audit_events_no_update`, `DROP TRIGGER audit_events_no_delete`}, stmts...) {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

func fileState(t *testing.T, path string) map[string]string {
	t.Helper()
	m := map[string]string{}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		b, err := os.ReadFile(path + suffix)
		if os.IsNotExist(err) {
			m[suffix] = "<absent>"
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		m[suffix] = string(b)
	}
	return m
}

func TestVerifyAuditLog_ValidChain(t *testing.T) {
	s, path := open(t)
	seedWithAudit(t, s)
	res := VerifyAuditLog(ctx, path)
	if res.Status != AuditVerified || res.EventCount != 2 || res.TipEventID != "ev2" {
		t.Fatalf("expected verified/2/ev2, got %+v", res)
	}
}

func TestVerifyAuditLog_EmptyChainIsVerified(t *testing.T) {
	s, path := open(t)
	_ = s
	res := VerifyAuditLog(ctx, path)
	if res.Status != AuditVerified || res.EventCount != 0 || res.TipEventID != "" {
		t.Fatalf("an empty chain verifies, as VerifyAuditChain defines it; got %+v", res)
	}
}

func TestVerifyAuditLog_DetectsTampering(t *testing.T) {
	cases := []struct {
		name  string
		stmt  string
		index int
	}{
		{"modified field", `UPDATE audit_events SET actor_label = 'mallory' WHERE id = 'ev2'`, 1},
		{"broken link", `UPDATE audit_events SET prev_hash = X'00' WHERE id = 'ev2'`, 1},
		{"empty hash", `UPDATE audit_events SET event_hash = X'' WHERE id = 'ev1'`, 0},
		{"invalid hash", `UPDATE audit_events SET event_hash = X'0102' WHERE id = 'ev2'`, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, path := open(t)
			seedWithAudit(t, s)
			tamper(t, s, c.stmt)
			res := VerifyAuditLog(ctx, path)
			if res.Status != AuditIntegrityFailure {
				t.Fatalf("expected integrity failure, got %+v", res)
			}
			if res.FailureIndex != c.index || res.FailureReason == "" || res.FailureEventID == "" {
				t.Fatalf("expected index %d with a reason and event id, got %+v", c.index, res)
			}
		})
	}
}

func TestVerifyAuditLog_UnsupportedAuditSchemaVersionIsUnavailableNotTampering(t *testing.T) {
	s, path := open(t)
	seedWithAudit(t, s)
	tamper(t, s, `UPDATE audit_events SET schema_version = 99 WHERE id = 'ev2'`)
	res := VerifyAuditLog(ctx, path)
	if res.Status != AuditUnavailable || res.Err == nil || !strings.Contains(res.Reason, "version 99") {
		t.Fatalf("expected unavailable naming version 99, got %+v", res)
	}
}

func TestVerifyAuditLog_NewerDatabaseSchemaIsUnavailable(t *testing.T) {
	s, path := open(t)
	seedWithAudit(t, s)
	if _, err := s.db.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES (9999, 0)`); err != nil {
		t.Skipf("cannot fabricate a newer schema row with this layout: %v", err)
	}
	res := VerifyAuditLog(ctx, path)
	if res.Status != AuditUnavailable || !strings.Contains(res.Reason, "newer than this binary") {
		t.Fatalf("expected unavailable (newer schema), got %+v", res)
	}
}

func TestVerifyAuditLog_OperationalFailuresAreUnavailable(t *testing.T) {
	dir := securefstest.TempDir(t)

	missing := VerifyAuditLog(ctx, filepath.Join(dir, "nope.db"))
	if missing.Status != AuditUnavailable {
		t.Fatalf("missing database must be unavailable, got %+v", missing)
	}
	if _, err := os.Stat(filepath.Join(dir, "nope.db")); !os.IsNotExist(err) {
		t.Fatal("verification must not create a missing database")
	}

	garbage := filepath.Join(dir, "garbage.db")
	if err := os.WriteFile(garbage, []byte("this is definitely not a sqlite database file, just bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if res := VerifyAuditLog(ctx, garbage); res.Status != AuditUnavailable {
		t.Fatalf("a non-database file must be unavailable, got %+v", res)
	}

	bare := filepath.Join(dir, "bare.db")
	raw, err := sql.Open("sqlite", "file:"+bare)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE schema_migrations(version INTEGER NOT NULL); INSERT INTO schema_migrations VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	raw.Close()
	if err := os.Chmod(bare, 0o600); err != nil {
		t.Fatal(err)
	}
	before := fileState(t, bare)
	res := VerifyAuditLog(ctx, bare)
	if res.Status != AuditUnavailable || !strings.Contains(res.Reason, "no audit log") {
		t.Fatalf("a database without audit_events must be unavailable, got %+v", res)
	}
	after := fileState(t, bare)
	for k := range before {
		if before[k] != after[k] {
			t.Fatalf("verification modified %q of a database without an audit table (no schema may be created)", "bare.db"+k)
		}
	}
}

func TestVerifyAuditLog_DoesNotModifyDatabaseOrAppendEvents(t *testing.T) {
	s, path := open(t)
	seedWithAudit(t, s)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	before := fileState(t, path)
	res := VerifyAuditLog(ctx, path)
	if res.Status != AuditVerified {
		t.Fatalf("got %+v", res)
	}
	after := fileState(t, path)
	for k := range before {
		if before[k] != after[k] {
			t.Fatalf("file %q changed during verification", "db"+k)
		}
	}
	reopened, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	evs, err := reopened.ListAuditEvents(ctx)
	if err != nil || len(evs) != 2 {
		t.Fatalf("verification must not append events: %d events, err %v", len(evs), err)
	}
}

// TestVerifyAuditLog_WALModeSidecarsUntouched uses a live WAL-mode database
// whose most recent events exist only in the -wal file, and checks that the
// verifier sees them and leaves the main file, -wal and -shm byte-identical.
// (The guarantee is that the verifier never opens the live files with SQLite;
// the test cannot prove anything about other processes touching them.)
func TestVerifyAuditLog_WALModeSidecarsUntouched(t *testing.T) {
	s, path := open(t)
	seed(t, s)
	if _, err := s.db.Exec(`PRAGMA wal_autocheckpoint=0`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendAuditEvent(ctx, ev("w1", store.AuditProjectCreate)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendAuditEvent(ctx, ev("w2", store.AuditSecretCreate)); err != nil {
		t.Fatal(err)
	}
	before := fileState(t, path)
	if before["-wal"] == "<absent>" || before["-wal"] == "" {
		t.Fatal("test setup failed: no pending WAL frames")
	}
	res := VerifyAuditLog(ctx, path)
	if res.Status != AuditVerified || res.EventCount != 2 || res.TipEventID != "w2" {
		t.Fatalf("expected the WAL-resident events to be verified, got %+v", res)
	}
	after := fileState(t, path)
	for k := range before {
		if before[k] != after[k] {
			t.Fatalf("file %q changed during verification", "db"+k)
		}
	}
}

// TestVerifyAuditLog_TailDeletionIsNotDetected documents the known limit of
// the unkeyed chain: removing the newest events leaves a chain that verifies.
func TestVerifyAuditLog_TailDeletionIsNotDetected(t *testing.T) {
	s, path := open(t)
	seedWithAudit(t, s)
	tamper(t, s, `DELETE FROM audit_events WHERE id = 'ev2'`)
	res := VerifyAuditLog(ctx, path)
	if res.Status != AuditVerified || res.EventCount != 1 {
		t.Fatalf("documented limitation: tail deletion passes verification; got %+v", res)
	}
}
