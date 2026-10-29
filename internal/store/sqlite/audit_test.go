package sqlite

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/securefs/securefstest"
	"github.com/docker-secret-operator/dso/internal/store"
)

func ev(id string, t store.AuditEventType) store.AuditEvent {
	return store.AuditEvent{ID: id, EventType: t, Outcome: store.AuditSuccess, ProjectSlug: "proj", SecretName: id}
}

func TestAuditChain_EmptyHistory(t *testing.T) {
	s, _ := open(t)
	events, err := s.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("expected empty history, got %d events", len(events))
	}
	if err := VerifyAuditChain(events); err != nil {
		t.Fatalf("empty chain must verify cleanly: %v", err)
	}
}

func TestAuditChain_FirstEventHasNilPrevHash(t *testing.T) {
	s, _ := open(t)
	got, err := s.AppendAuditEvent(ctx, ev("e1", store.AuditInit))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.PrevHash) != 0 {
		t.Fatalf("first event must have an empty prev_hash, got %x", got.PrevHash)
	}
	if len(got.EventHash) == 0 {
		t.Fatal("expected a computed event_hash")
	}
}

func TestAuditChain_SubsequentEventsLink(t *testing.T) {
	s, _ := open(t)
	e1, err := s.AppendAuditEvent(ctx, ev("e1", store.AuditInit))
	if err != nil {
		t.Fatal(err)
	}
	e2, err := s.AppendAuditEvent(ctx, ev("e2", store.AuditProjectCreate))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(e2.PrevHash, e1.EventHash) {
		t.Fatalf("second event's prev_hash %x does not match first event's event_hash %x", e2.PrevHash, e1.EventHash)
	}
	events, err := s.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if err := VerifyAuditChain(events); err != nil {
		t.Fatalf("chain must verify cleanly: %v", err)
	}
}

func TestAuditChain_DeterministicHashForEquivalentData(t *testing.T) {
	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a := store.AuditEvent{ID: "e1", SchemaVersion: 1, EventType: store.AuditSecretCreate, Outcome: store.AuditSuccess,
		ActorLabel: "alice", ProjectSlug: "p", EnvironmentSlug: "env", SecretName: "s", Version: 1, CreatedAt: fixed}
	b := a // identical copy
	h1 := computeEventHash(nil, a)
	h2 := computeEventHash(nil, b)
	if !bytes.Equal(h1, h2) {
		t.Fatal("identical canonical event data must hash identically")
	}
	b.Version = 2 // one field differs
	h3 := computeEventHash(nil, b)
	if bytes.Equal(h1, h3) {
		t.Fatal("different event data must not hash identically")
	}
}

func TestAuditChain_TamperedFieldDetected(t *testing.T) {
	s, _ := open(t)
	if _, err := s.AppendAuditEvent(ctx, ev("e1", store.AuditInit)); err != nil {
		t.Fatal(err)
	}
	events, err := s.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	events[0].SecretName = "tampered" // simulate an attacker editing a covered field in place
	var chainErr *ChainError
	if err := VerifyAuditChain(events); err == nil {
		t.Fatal("expected tampered field to be detected")
	} else if !asChainError(err, &chainErr) {
		t.Fatalf("expected a *ChainError, got %T: %v", err, err)
	}
}

func TestAuditChain_BrokenPrevHashLinkDetected(t *testing.T) {
	s, _ := open(t)
	if _, err := s.AppendAuditEvent(ctx, ev("e1", store.AuditInit)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendAuditEvent(ctx, ev("e2", store.AuditProjectCreate)); err != nil {
		t.Fatal(err)
	}
	events, err := s.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	events[1].PrevHash = []byte("not the real previous hash")
	if err := VerifyAuditChain(events); err == nil {
		t.Fatal("expected a broken prev_hash link to be detected")
	}
}

func TestAuditChain_MissingEventHashDetected(t *testing.T) {
	s, _ := open(t)
	if _, err := s.AppendAuditEvent(ctx, ev("e1", store.AuditInit)); err != nil {
		t.Fatal(err)
	}
	events, err := s.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	events[0].EventHash = nil // malformed/missing hash
	if err := VerifyAuditChain(events); err == nil {
		t.Fatal("expected a missing event_hash to be detected as a verification failure")
	}
}

// TestAuditChain_TailDeletionNotDetected is the explicit, required negative
// test (Phase 2 Step 5.3, approved design §6/§10): the chain's own
// verification, by itself, cannot tell that its most recent event(s) were
// deleted -- a chain missing its tail is, from this check's point of view,
// indistinguishable from a chain that simply hasn't grown further yet. This
// test exists specifically so no future change can silently start claiming a
// stronger guarantee than this design actually provides.
func TestAuditChain_TailDeletionNotDetected(t *testing.T) {
	s, _ := open(t)
	if _, err := s.AppendAuditEvent(ctx, ev("e1", store.AuditInit)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendAuditEvent(ctx, ev("e2", store.AuditProjectCreate)); err != nil {
		t.Fatal(err)
	}
	full, err := s.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyAuditChain(full); err != nil {
		t.Fatalf("full chain must verify cleanly: %v", err)
	}
	truncated := full[:1] // simulate the tail having been deleted
	if err := VerifyAuditChain(truncated); err != nil {
		t.Fatalf("a truncated chain must still verify cleanly -- tail deletion is NOT detectable by this chain alone "+
			"(explicit, approved limitation): %v", err)
	}
}

func asChainError(err error, target **ChainError) bool {
	ce, ok := err.(*ChainError)
	if ok {
		*target = ce
	}
	return ok
}

// TestAuditSchemaVersion_SupportedFutureAndInvalid (F4): the writer accepts
// only the supported version (defaulting 0 to it), and rejects both a future
// and a negative version outright, before anything is written. The verifier
// independently rejects an event claiming an unsupported version, so a
// future/invalid version can never be reported as "verified" either way.
func TestAuditSchemaVersion_SupportedFutureAndInvalid(t *testing.T) {
	s, _ := open(t)

	// Supported (explicit and defaulted) both succeed.
	supported := ev("e1", store.AuditInit)
	supported.SchemaVersion = supportedAuditEventSchemaVersion
	if _, err := s.AppendAuditEvent(ctx, supported); err != nil {
		t.Fatalf("explicit supported version must be accepted: %v", err)
	}
	defaulted := ev("e2", store.AuditProjectCreate) // SchemaVersion left at 0
	if got, err := s.AppendAuditEvent(ctx, defaulted); err != nil {
		t.Fatalf("defaulted (zero) version must be accepted: %v", err)
	} else if got.SchemaVersion != supportedAuditEventSchemaVersion {
		t.Fatalf("expected the defaulted version to resolve to %d, got %d", supportedAuditEventSchemaVersion, got.SchemaVersion)
	}

	// A future version is rejected by the writer.
	future := ev("e3", store.AuditSecretCreate)
	future.SchemaVersion = supportedAuditEventSchemaVersion + 1
	if _, err := s.AppendAuditEvent(ctx, future); err != store.ErrInvalid {
		t.Fatalf("a future schema version must be rejected with ErrInvalid, got %v", err)
	}

	// An invalid (negative) version is rejected by the writer.
	invalid := ev("e4", store.AuditSecretDelete)
	invalid.SchemaVersion = -1
	if _, err := s.AppendAuditEvent(ctx, invalid); err != store.ErrInvalid {
		t.Fatalf("a negative schema version must be rejected with ErrInvalid, got %v", err)
	}

	// The verifier independently rejects an event claiming an unsupported
	// version, even if one somehow reached it (e.g. a future binary's data
	// read by this one) -- it must not silently treat it as verified.
	events, err := s.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected exactly 2 persisted events (the rejected ones were never written), got %d", len(events))
	}
	tampered := append([]store.AuditEvent{}, events...)
	tampered[1].SchemaVersion = supportedAuditEventSchemaVersion + 1
	var chainErr *ChainError
	if err := VerifyAuditChain(tampered); err == nil {
		t.Fatal("expected verification to reject an event claiming an unsupported schema version")
	} else if !asChainError(err, &chainErr) {
		t.Fatalf("expected a *ChainError, got %T: %v", err, err)
	}
}

func TestAppendAuditEvent_RejectsInvalid(t *testing.T) {
	s, _ := open(t)
	cases := []store.AuditEvent{
		{ID: "", EventType: store.AuditInit, Outcome: store.AuditSuccess},
		{ID: "e1", EventType: "", Outcome: store.AuditSuccess},
		{ID: "e1", EventType: store.AuditInit, Outcome: ""},
		{ID: "e1", EventType: store.AuditInit, Outcome: "not-a-real-outcome"},
	}
	for i, c := range cases {
		if _, err := s.AppendAuditEvent(ctx, c); err != store.ErrInvalid {
			t.Errorf("case %d: expected ErrInvalid, got %v", i, err)
		}
	}
}

// TestAppendAuditEvent_ConcurrentStandaloneAppendsStayLinked guards against
// the race fixed in Store.AppendAuditEvent: two concurrent standalone
// (non-WithTx) appends must not both read the same "latest" hash and fork
// the chain.
func TestAppendAuditEvent_ConcurrentStandaloneAppendsStayLinked(t *testing.T) {
	s, _ := open(t)
	const n = 20
	errc := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			_, err := s.AppendAuditEvent(ctx, ev(string(rune('a'+i)), store.AuditSecretGetReveal))
			errc <- err
		}(i)
	}
	for i := 0; i < n; i++ {
		if err := <-errc; err != nil {
			t.Fatal(err)
		}
	}
	events, err := s.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != n {
		t.Fatalf("expected %d events, got %d", n, len(events))
	}
	if err := VerifyAuditChain(events); err != nil {
		t.Fatalf("concurrent standalone appends must still produce a single valid chain: %v", err)
	}
}

// TestWithTx_AuditInsertFailureRollsBackBusinessMutation is the direct proof
// of the approved Gate 2/§7.1 atomicity guarantee: if the audit-event insert
// inside a shared WithTx fails, the business mutation it was paired with must
// not be left committed.
func TestWithTx_AuditInsertFailureRollsBackBusinessMutation(t *testing.T) {
	s, _ := open(t)
	// Pre-seed a conflicting audit event id so the second insert inside the
	// transaction below is guaranteed to fail on the UNIQUE(id) constraint.
	if _, err := s.AppendAuditEvent(ctx, ev("dup-id", store.AuditInit)); err != nil {
		t.Fatal(err)
	}

	err := s.WithTx(ctx, func(ctx context.Context, tx store.Tx) error {
		if err := tx.CreateProject(ctx, store.Project{
			ID: "p-atomic", Slug: "atomic-test", WrappedDEK: []byte("w"), DEKKeyID: "k",
		}); err != nil {
			return err
		}
		_, err := tx.AppendAuditEvent(ctx, ev("dup-id", store.AuditProjectCreate)) // id collides -> fails
		return err
	})
	if err == nil {
		t.Fatal("expected the audit insert's UNIQUE(id) violation to fail the transaction")
	}

	if _, getErr := s.GetProjectBySlug(ctx, "atomic-test"); getErr != store.ErrNotFound {
		t.Fatalf("business mutation must have rolled back with the failed audit insert, but GetProjectBySlug returned: %v", getErr)
	}

	// F6: the audit history itself must also show no trace of the failed
	// attempt -- exactly the one pre-seeded event, unchanged, not two, and
	// not a partially-committed or corrupted row.
	events, err := s.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 audit event (the pre-seeded one) after the rolled-back attempt, got %d", len(events))
	}
	if events[0].ID != "dup-id" || events[0].EventType != store.AuditInit || events[0].Outcome != store.AuditSuccess {
		t.Fatalf("the surviving event must be exactly the pre-seeded one, unchanged; got %+v", events[0])
	}
	if err := VerifyAuditChain(events); err != nil {
		t.Fatalf("the surviving chain must still verify cleanly: %v", err)
	}
}

// TestAppendAuditEvent_ContextCancellationRollsBack follows the same
// deterministic pattern as TestWithTxContextCancellationRollsBackWithoutLeaks
// (sqlite_tx_test.go): cancel the context from inside the transaction itself,
// so there is no timing race -- the cancellation is guaranteed to have
// happened before the next statement runs.
func TestAppendAuditEvent_ContextCancellationRollsBack(t *testing.T) {
	s, _ := open(t)
	if _, err := s.AppendAuditEvent(ctx, ev("e1", store.AuditInit)); err != nil {
		t.Fatal(err)
	}
	cctx, cancel := context.WithCancel(ctx)
	err := s.WithTx(cctx, func(ctx context.Context, tx store.Tx) error {
		cancel()
		_, err := tx.AppendAuditEvent(ctx, ev("e2", store.AuditProjectCreate)) // must fail: context cancelled
		return err
	})
	cancel()
	if err == nil {
		t.Fatal("expected the cancelled-context append to fail")
	}
	events, err := s.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected only the pre-existing event to survive cancellation, got %d", len(events))
	}
}

// TestMigration0003_UpgradesExistingDatabaseAndIsUsable is a
// migration-3-specific test (reusing the existing generic migration test
// pattern, per §9 of the remediation task -- no new migration-testing
// framework): a database created with only migrations 1 and 2 (i.e. no
// audit_events table yet) must upgrade cleanly to include migration 3 and
// be immediately usable for AppendAuditEvent afterward.
func TestMigration0003_UpgradesExistingDatabaseAndIsUsable(t *testing.T) {
	path := filepath.Join(securefstest.TempDir(t), "m3.db")
	embedded, err := EmbeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	var upTo2 []Migration
	for _, m := range embedded {
		if m.Version <= 2 {
			upTo2 = append(upTo2, m)
		}
	}
	if len(upTo2) != 2 {
		t.Fatalf("expected exactly migrations 1 and 2, got %d", len(upTo2))
	}
	s, err := Open(ctx, path, Options{Migrations: upTo2})
	if err != nil {
		t.Fatal(err)
	}
	seed(t, s)
	s.Close()

	s2, err := Open(ctx, path, Options{}) // full embedded set, including migration 3
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if v, _ := s2.SchemaVersion(ctx); v < 3 {
		t.Fatalf("expected schema version >= 3 after upgrade, got %d", v)
	}
	if _, err := s2.GetProjectBySlug(ctx, "proj"); err != nil {
		t.Fatalf("data from before the upgrade must survive: %v", err)
	}
	if _, err := s2.AppendAuditEvent(ctx, ev("post-upgrade", store.AuditInit)); err != nil {
		t.Fatalf("audit_events table must be usable immediately after the migration-3 upgrade: %v", err)
	}
}

// TestMigration0003_FailureLeavesNoPartialAuditTable reuses
// TestFailedMigrationRollsBackCompletely's exact pattern (sqlite_test.go),
// applied specifically to migration 3: if it fails partway, the audit_events
// table must not exist at all afterward, not a partially-created one.
func TestMigration0003_FailureLeavesNoPartialAuditTable(t *testing.T) {
	path := filepath.Join(securefstest.TempDir(t), "m3-fail.db")
	embedded, err := EmbeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	var upTo2 []Migration
	for _, m := range embedded {
		if m.Version <= 2 {
			upTo2 = append(upTo2, m)
		} else if m.Version == 3 {
			// A broken variant of migration 3: creates the table, then fails
			// on a statement that references a nonexistent table -- the
			// whole migration transaction (sqlite.go's applyMigration) must
			// roll back the partially-created table along with it.
			upTo2 = append(upTo2, Migration{Version: 3, SQL: m.SQL + `
				INSERT INTO no_such_table VALUES (1);`})
		}
	}
	s, err := Open(ctx, path, Options{Migrations: upTo2[:2]})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	if _, err := Open(ctx, path, Options{Migrations: upTo2}); err == nil {
		t.Fatal("expected the broken migration 3 to fail")
	}

	s2, err := Open(ctx, path, Options{Migrations: upTo2[:2]})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if v, _ := s2.SchemaVersion(ctx); v != 2 {
		t.Fatalf("schema version advanced to %d despite the failed migration 3", v)
	}
	if _, err := s2.db.Exec(`SELECT 1 FROM audit_events LIMIT 1`); err == nil {
		t.Fatal("audit_events table must not exist after migration 3 failed and rolled back")
	}
}

func TestAuditEvents_AreImmutable(t *testing.T) {
	s, _ := open(t)
	if _, err := s.AppendAuditEvent(ctx, ev("e1", store.AuditInit)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE audit_events SET event_type = 'x' WHERE id = 'e1'`); err == nil {
		t.Fatal("expected the audit_events_no_update trigger to reject an UPDATE")
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM audit_events WHERE id = 'e1'`); err == nil {
		t.Fatal("expected the audit_events_no_delete trigger to reject a DELETE")
	}
}
