package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/docker-secret-operator/dso/internal/rotation"
)

// Migration of journals written before F-S6-1 (keys provider:secret:ids) to the
// operation-identity keys. The legacy files below are built exactly as the old
// persistState wrote them: a JSON object keyed by the old key.

func legacyRec(provider, secret, orig, rid, status string) *RotationState {
	now := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	return &RotationState{
		ProviderName: provider, SecretName: secret, OriginalContainerID: orig,
		OriginalContainerIDs: splitContainerIDs(orig), RotationID: rid, Phase: "rotation_requested",
		Status: status, StartTime: now, LastUpdate: now,
	}
}

func writeLegacy(t *testing.T, dir string, m map[string]*RotationState) []byte {
	t.Helper()
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rotations.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	return b
}

func legacyBackups(t *testing.T, dir string) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(dir, "rotations.json.legacy-*"))
	return m
}

func persistedKeys(t *testing.T, dir string) map[string]*RotationState {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "rotations.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]*RotationState{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("persisted journal unreadable: %v", err)
	}
	return m
}

func load(t *testing.T, dir string) *StateTracker {
	t.Helper()
	st, err := NewStateTracker(dir, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// Old format, one valid completed record: kept, addressable by its rotation id,
// the exact old bytes preserved aside, and a second startup changes nothing.
func TestMigration_LegacySingleValidRecord(t *testing.T) {
	dir := t.TempDir()
	old := writeLegacy(t, dir, map[string]*RotationState{
		"vault:db:o1": legacyRec("vault", "db", "o1", "rot-1", "completed"),
	})
	st := load(t, dir)
	r := st.recordByID("rot-1")
	if r == nil || r.Status != "completed" || r.ProviderName != "vault" || r.OriginalContainerID != "o1" {
		t.Fatalf("record lost or altered: %+v", r)
	}
	keys := persistedKeys(t, dir)
	if _, ok := keys["journal:rot-1"]; !ok || len(keys) != 1 {
		t.Fatalf("persisted keys %v", keys)
	}
	bk := legacyBackups(t, dir)
	if len(bk) != 1 {
		t.Fatalf("backups %v", bk)
	}
	if b, _ := os.ReadFile(bk[0]); string(b) != string(old) {
		t.Fatal("the backup is not the exact pre-migration file")
	}
	// Repeated startup: no new backup, no rewrite.
	before, _ := os.ReadFile(filepath.Join(dir, "rotations.json"))
	for i := 0; i < 3; i++ {
		st2 := load(t, dir)
		if st2.recordByID("rot-1") == nil {
			t.Fatal("lost on restart")
		}
	}
	after, _ := os.ReadFile(filepath.Join(dir, "rotations.json"))
	if string(before) != string(after) || len(legacyBackups(t, dir)) != 1 {
		t.Fatal("repeated startup is not a no-op")
	}
}

// Old format, unresolved records (in_progress inherited, critical_error, rollback):
// all kept with their status, the guard still blocks, recovery still sees them.
func TestMigration_LegacyUnresolvedRecordsPreserved(t *testing.T) {
	dir := t.TempDir()
	crit := legacyRec("vault", "api", "o2", "rot-2", "critical_error")
	crit.ServiceID, crit.ErrorMessage, crit.Strategy = "svc-api", "operator needed", "signal"
	inprog := legacyRec("vault", "db", "o1", "rot-1", "in_progress")
	inprog.ServiceID, inprog.Strategy, inprog.Seq, inprog.Transition = "svc-db", "rolling", 3, rotation.TrIntentHandoff
	rb := legacyRec("aws", "k", "o3", "rot-3", "rollback_required")
	writeLegacy(t, dir, map[string]*RotationState{"vault:api:o2": crit, "vault:db:o1": inprog, "aws:k:o3": rb})
	st := load(t, dir)
	for id, want := range map[string]string{"rot-1": "in_progress", "rot-2": "critical_error", "rot-3": "rollback_required"} {
		if got := statusOf(st, id); got != want {
			t.Errorf("%s = %s, want %s", id, got, want)
		}
	}
	g := newJournalGuard(st)
	for _, svc := range []string{"svc-api", "svc-db"} {
		if _, b := g.RotationBlocked(svc); !b {
			t.Errorf("%s no longer blocked after migration", svc)
		}
	}
	if len(st.GetUnfinishedRotationsAtStartup()) != 3 {
		t.Fatalf("unfinished = %v", ids(st.GetUnfinishedRotationsAtStartup()))
	}
	// The migrated in_progress record is still INHERITED: a new dispatch of the
	// same secret leaves it alone.
	_ = st.StartRotationWithID("rot-new", "vault", "db", "o1", "")
	_ = st.DeleteStateID("rot-new")
	if statusOf(st, "rot-1") != "in_progress" {
		t.Fatal("inherited record disturbed")
	}
	if _, b := g.RotationBlocked("svc-db"); !b {
		t.Fatal("inherited in_progress no longer blocks")
	}
}

// New-format journals (several scoped operations) load with no migration at all.
func TestMigration_NewFormatIsNotRewritten(t *testing.T) {
	dir := t.TempDir()
	st := load(t, dir)
	for _, id := range []string{"a", "b", "c"} {
		_ = st.StartRotationWithID(id, "vault", "db", "o", "")
	}
	_ = st.CompleteRotationID("b")
	before, _ := os.ReadFile(filepath.Join(dir, "rotations.json"))
	st2 := load(t, dir)
	if len(st2.snapshotRecords()) != 3 {
		t.Fatalf("records %d", len(st2.snapshotRecords()))
	}
	after, _ := os.ReadFile(filepath.Join(dir, "rotations.json"))
	if string(before) != string(after) || len(legacyBackups(t, dir)) != 0 {
		t.Fatal("a current-format journal was rewritten or backed up")
	}
	// The persisted schema is unchanged: a plain JSON object of the same record shape,
	// with no new fields (an older release still reads it).
	var generic map[string]map[string]any
	if err := json.Unmarshal(after, &generic); err != nil {
		t.Fatal(err)
	}
	for k, r := range generic {
		if !strings.HasPrefix(k, "journal:") {
			t.Fatalf("key %q", k)
		}
		if _, ok := r["key"]; ok {
			t.Fatal("the in-memory slot leaked into the file")
		}
	}
}

// Duplicate identities. Byte-identical duplicates are one record; DIFFERENT records
// claiming one rotation id are all preserved and none is addressable by that id.
func TestMigration_DuplicateIdentities(t *testing.T) {
	t.Run("identical", func(t *testing.T) {
		dir := t.TempDir()
		r := legacyRec("vault", "db", "o1", "rot-1", "completed")
		cp := *r
		writeLegacy(t, dir, map[string]*RotationState{"vault:db:o1": r, "journal:rot-1": &cp})
		st := load(t, dir)
		if len(st.snapshotRecords()) != 1 || st.recordByID("rot-1") == nil {
			t.Fatalf("records %v", ids(st.snapshotRecords()))
		}
		if len(legacyBackups(t, dir)) != 1 {
			t.Fatal("no backup of the pre-migration duplicate file")
		}
	})
	t.Run("conflicting", func(t *testing.T) {
		dir := t.TempDir()
		a := legacyRec("vault", "db", "o1", "rot-1", "critical_error")
		a.ServiceID, a.ErrorMessage = "svc", "operator needed"
		b := legacyRec("vault", "db", "o1", "rot-1", "completed")
		b.Seq = 4
		b.SecretVersion = "" // different content
		writeLegacy(t, dir, map[string]*RotationState{"vault:db:o1": a, "journal:rot-1": b})
		st := load(t, dir)
		if n := len(st.snapshotRecords()); n != 2 {
			t.Fatalf("both conflicting records must be preserved, got %d", n)
		}
		if st.recordByID("rot-1") != nil {
			t.Fatal("an ambiguous identity must not resolve to a record")
		}
		// Fail closed everywhere the id is used.
		if err := st.CompleteRotationID("rot-1"); !errors.Is(err, ErrRotationIdentityAmbiguous) {
			t.Fatalf("complete: %v", err)
		}
		if err := st.Record(context.Background(), rotation.JournalEvent{RotationID: "rot-1", Transition: rotation.TrCompleted}); !errors.Is(err, ErrRotationIdentityAmbiguous) {
			t.Fatalf("record: %v", err)
		}
		if err := st.StartRotationWithID("rot-1", "vault", "db", "o1", ""); !errors.Is(err, ErrRotationIdentityAmbiguous) {
			t.Fatalf("start: %v", err)
		}
		// The unresolved one still blocks its service; recovery can address each by slot.
		if _, blocked := newJournalGuard(st).RotationBlocked("svc"); !blocked {
			t.Fatal("the unresolved duplicate no longer blocks")
		}
		for _, r := range st.snapshotRecords() {
			if !strings.Contains(r.Detail, "operator review required") {
				t.Fatalf("not marked for operator review: %+v", r)
			}
			if r.Status == "critical_error" {
				if err := st.MarkRecoveredRecord(r); err != nil {
					t.Fatalf("recovery cannot resolve the preserved record by its own slot: %v", err)
				}
			}
		}
		// Idempotent: a restart neither re-annotates nor re-backs-up.
		before := persistedKeys(t, dir)
		_ = load(t, dir)
		if len(legacyBackups(t, dir)) != 1 || len(persistedKeys(t, dir)) != len(before) {
			t.Fatal("repeated startup changed the migrated journal")
		}
	})
}

// Ambiguous legacy identity: a key that contradicts its record is never
// reinterpreted as another operation; the record is preserved verbatim, marked, and
// only reachable by its own slot.
func TestMigration_AmbiguousLegacyIdentity(t *testing.T) {
	dir := t.TempDir()
	mismatchedKey := legacyRec("vault", "db", "o1", "rot-A", "in_progress") // key names rot-B
	mismatchedKey.ServiceID = "svc-x"
	wrongLegacy := legacyRec("vault", "db", "o9", "rot-C", "completed") // key does not match its content
	noID := legacyRec("vault", "api", "o2", "", "completed")            // pre-rotation-id release
	writeLegacy(t, dir, map[string]*RotationState{
		"journal:rot-B": mismatchedKey, "vault:db:o1": wrongLegacy, "vault:api:o2": noID,
	})
	st := load(t, dir)
	if st.recordByID("rot-A") != nil || st.recordByID("rot-B") != nil || st.recordByID("rot-C") != nil {
		t.Fatal("an ambiguous record resolved by rotation id")
	}
	keys := persistedKeys(t, dir)
	if _, ok := keys["ambiguous:journal:rot-B"]; !ok {
		t.Fatalf("keys %v", keys)
	}
	if _, ok := keys["ambiguous:vault:db:o1"]; !ok {
		t.Fatalf("keys %v", keys)
	}
	if _, ok := keys["vault:api:o2"]; !ok {
		t.Fatalf("an id-less legacy record is kept under its own key: %v", keys)
	}
	if r := keys["ambiguous:journal:rot-B"]; r.Status != "in_progress" || r.RotationID != "rot-A" || r.ServiceID != "svc-x" {
		t.Fatalf("content changed: %+v", r)
	}
	if _, b := newJournalGuard(st).RotationBlocked("svc-x"); !b {
		t.Fatal("the unresolved ambiguous record no longer blocks its service")
	}
	// Events for the claimed ids fail closed (they could attach to the wrong operation).
	if err := st.Record(context.Background(), rotation.JournalEvent{RotationID: "rot-A", Transition: rotation.TrCompleted}); !errors.Is(err, ErrRotationIdentityAmbiguous) {
		t.Fatalf("record: %v", err)
	}
	// An id-less legacy record stays resolvable by the deprecated triple.
	if err := st.MarkRecovered("vault", "api", "o2"); err != nil {
		t.Fatal(err)
	}
}

// Corrupt content: a null record is dropped (only after the old file is preserved);
// an unreadable file keeps the existing quarantine and fail-closed behaviour.
func TestMigration_CorruptRecords(t *testing.T) {
	dir := t.TempDir()
	old := []byte(`{"vault:db:o1": null, "vault:api:o2": {"provider_name":"vault","secret_name":"api","original_container_id":"o2","rotation_id":"rot-2","status":"completed"}}`)
	if err := os.WriteFile(filepath.Join(dir, "rotations.json"), old, 0o600); err != nil {
		t.Fatal(err)
	}
	st := load(t, dir)
	if st.recordByID("rot-2") == nil || len(st.snapshotRecords()) != 1 {
		t.Fatalf("records %v", ids(st.snapshotRecords()))
	}
	bk := legacyBackups(t, dir)
	if len(bk) != 1 {
		t.Fatalf("backups %v", bk)
	}
	if b, _ := os.ReadFile(bk[0]); string(b) != string(old) {
		t.Fatal("the dropped null entry is not preserved in the backup")
	}

	// Unparseable: quarantined, corrupt flag set, no migration attempted.
	dir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir2, "rotations.json"), []byte(`{"vault:db:o1": {"rotation_id": "r`), 0o600); err != nil {
		t.Fatal(err)
	}
	st2 := load(t, dir2)
	if c, _ := st2.Corrupt(); !c {
		t.Fatal("corrupt journal not flagged")
	}
	if _, b := newJournalGuard(st2).RotationBlocked("anything"); !b {
		t.Fatal("a corrupt journal must block")
	}
	if len(legacyBackups(t, dir2)) != 0 {
		t.Fatal("no migration backup for an unreadable file")
	}
}

// Interrupted migration: a crash after the backup, or after the new file was written
// but before it replaced the old one, leaves the legacy file intact; the next start
// completes the migration without a second backup, and loses nothing.
func TestMigration_InterruptedAndResumed(t *testing.T) {
	for _, point := range []string{"after_backup", "before_persist"} {
		t.Run(point, func(t *testing.T) {
			dir := t.TempDir()
			crit := legacyRec("vault", "api", "o2", "rot-2", "critical_error")
			crit.ServiceID = "svc-api"
			old := writeLegacy(t, dir, map[string]*RotationState{
				"vault:db:o1": legacyRec("vault", "db", "o1", "rot-1", "in_progress"), "vault:api:o2": crit,
			})
			migrationFault = func(p string) error {
				if p == point {
					return errors.New("injected crash")
				}
				return nil
			}
			st := load(t, dir)
			migrationFault = nil
			// The legacy file is untouched on disk.
			if b, _ := os.ReadFile(filepath.Join(dir, "rotations.json")); string(b) != string(old) {
				t.Fatal("the legacy file changed during an interrupted migration")
			}
			// While the migration is pending the tracker still serves a complete, correct
			// view in memory and keeps blocking.
			if statusOf(st, "rot-2") != "critical_error" {
				t.Fatal("in-memory view incomplete")
			}
			// Next start: completes, exactly one backup, nothing lost.
			st2 := load(t, dir)
			if len(st2.snapshotRecords()) != 2 || statusOf(st2, "rot-1") != "in_progress" || statusOf(st2, "rot-2") != "critical_error" {
				t.Fatalf("records after resume: %v", ids(st2.snapshotRecords()))
			}
			if bk := legacyBackups(t, dir); len(bk) != 1 {
				t.Fatalf("backups after an interrupted + resumed migration: %v", bk)
			}
			keys := persistedKeys(t, dir)
			if _, ok := keys["journal:rot-1"]; !ok || len(keys) != 2 {
				t.Fatalf("keys %v", keys)
			}
		})
	}
}

// A later journal write while the migration is pending retries the migration (never
// writes the new shape over an unpreserved legacy file).
func TestMigration_PendingMigrationNeverOverwritesLegacyWithoutBackup(t *testing.T) {
	dir := t.TempDir()
	old := writeLegacy(t, dir, map[string]*RotationState{"vault:db:o1": legacyRec("vault", "db", "o1", "rot-1", "completed")})
	migrationFault = func(p string) error { return errors.New("injected crash") }
	st := load(t, dir)
	if err := st.StartRotationWithID("rot-2", "vault", "db", "o1", ""); err == nil {
		t.Fatal("expected the journal write to fail while the migration is blocked")
	}
	migrationFault = nil
	if b, _ := os.ReadFile(filepath.Join(dir, "rotations.json")); string(b) != string(old) {
		t.Fatal("legacy file replaced")
	}
	// Fault cleared: the next write completes the migration, then succeeds.
	if err := st.StartRotationWithID("rot-3", "vault", "db", "o1", ""); err != nil {
		t.Fatal(err)
	}
	if len(legacyBackups(t, dir)) != 1 {
		t.Fatal("backup missing")
	}
	keys := persistedKeys(t, dir)
	if _, ok := keys["journal:rot-1"]; !ok {
		t.Fatalf("keys %v", keys)
	}
}

// If the backup cannot be written the legacy file is never replaced (fail closed).
func TestMigration_BackupFailureRefusesRewrite(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-based failure injection does not apply to root")
	}
	dir := t.TempDir()
	old := writeLegacy(t, dir, map[string]*RotationState{"vault:db:o1": legacyRec("vault", "db", "o1", "rot-1", "in_progress")})
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(dir, 0o700) }()
	st := load(t, dir)
	if b, _ := os.ReadFile(filepath.Join(dir, "rotations.json")); string(b) != string(old) {
		t.Fatal("legacy file replaced without a backup")
	}
	if statusOf(st, "rot-1") != "in_progress" {
		t.Fatal("in-memory records lost")
	}
}

// Old-format single record + a NEW dispatch of the same secret in the same process:
// the legacy record is not touched by the new operation's lifecycle.
func TestMigration_LegacyRecordNotTouchedByNewDispatchLifecycle(t *testing.T) {
	dir := t.TempDir()
	writeLegacy(t, dir, map[string]*RotationState{"vault:db:o1": legacyRec("vault", "db", "o1", "rot-old", "completed")})
	st := load(t, dir)
	_ = st.StartRotationWithID("rot-new", "vault", "db", "o1", "")
	_ = st.MarkRollbackID("rot-new")
	_ = st.DeleteStateID("rot-new")
	if r := st.recordByID("rot-old"); r == nil || r.Status != "completed" {
		t.Fatalf("legacy predecessor touched: %+v", r)
	}
}
