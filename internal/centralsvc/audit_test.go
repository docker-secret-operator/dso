package centralsvc

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/docker-secret-operator/dso/internal/store"
	"github.com/docker-secret-operator/dso/internal/store/sqlite"
)

// failingAuditTx wraps a real store.Tx and makes AppendAuditEvent fail,
// while every other method (including the business mutation) passes through
// unchanged -- used to force the transactional-write failure path without
// touching anything else inside the shared WithTx callback.
type failingAuditTx struct {
	store.Tx
}

func (f failingAuditTx) AppendAuditEvent(ctx context.Context, ev store.AuditEvent) (store.AuditEvent, error) {
	return store.AuditEvent{}, errors.New("synthetic audit-store failure (test-injected)")
}

// failingAuditStore wraps a real store.Store. Standalone AppendAuditEvent
// calls (the best-effort path: GetValue's reveal events, and every
// recorded failure event) fail directly. WithTx-scoped calls (the atomic
// path: CreateProject/CreateEnvironment/CreateSecret/AddVersion/
// DisableVersion/DeleteSecret) pass through to the real store but with the
// Tx wrapped in failingAuditTx, so the business mutation inside still runs
// for real and only its paired AppendAuditEvent call fails.
type failingAuditStore struct {
	store.Store
}

func (f failingAuditStore) AppendAuditEvent(ctx context.Context, ev store.AuditEvent) (store.AuditEvent, error) {
	return store.AuditEvent{}, errors.New("synthetic audit-store failure (test-injected)")
}

func (f failingAuditStore) WithTx(ctx context.Context, fn func(ctx context.Context, tx store.Tx) error) error {
	return f.Store.WithTx(ctx, func(ctx context.Context, tx store.Tx) error {
		return fn(ctx, failingAuditTx{Tx: tx})
	})
}

// TestAudit_BestEffortWriteFailureIsLoggedAndDoesNotBlockTheRead forces the
// standalone (non-transactional) AppendAuditEvent path to fail -- the path
// GetValue uses -- and asserts (F5): the approved availability-first policy
// holds (the read still succeeds), the failure is logged at error level, and
// the log contains no secret plaintext or other sensitive value.
func TestAudit_BestEffortWriteFailureIsLoggedAndDoesNotBlockTheRead(t *testing.T) {
	e := setup(t)
	e.project(t, "p1", "env1")
	if err := e.svc.CreateSecret(ctx, "p1", "env1", "s1", []byte(plain), "alice", ""); err != nil {
		t.Fatal(err)
	}

	core, logs := observer.New(zap.DebugLevel)
	svc := New(failingAuditStore{Store: e.st}, e.svc.keys, zap.New(core))

	val, err := svc.GetValue(ctx, "p1", "env1", "s1", 0)
	if err != nil {
		t.Fatalf("availability-first policy: GetValue must still succeed when the audit write fails, got: %v", err)
	}
	if string(val.Plaintext) != plain {
		t.Fatal("GetValue returned the wrong value")
	}

	found := false
	for _, ent := range logs.All() {
		if ent.Level.String() == "error" && strings.Contains(ent.Message, "audit") {
			found = true
		}
		if strings.Contains(ent.Message, plain) {
			t.Fatalf("log entry contains the plaintext secret value: %q", ent.Message)
		}
		for _, f := range ent.Context {
			if strings.Contains(f.String, plain) {
				t.Fatalf("log field %q contains the plaintext secret value", f.Key)
			}
		}
	}
	if !found {
		t.Fatal("expected an error-level log entry reporting the audit-write failure, found none")
	}
}

// TestAudit_TransactionalWriteFailureRollsBackAndIsLogged forces the
// WithTx-scoped AppendAuditEvent path to fail -- the path
// CreateProject/CreateEnvironment/CreateSecret/AddVersion/DisableVersion/
// DeleteSecret all use -- and asserts (F5, without weakening the approved
// atomicity guarantee): the operation fails as a whole (the business
// mutation does NOT commit without its audit event), and the failure is
// still logged at error level via the separate, standalone best-effort
// failure-event attempt that runs after the rolled-back transaction.
func TestAudit_TransactionalWriteFailureRollsBackAndIsLogged(t *testing.T) {
	e := setup(t)

	core, logs := observer.New(zap.DebugLevel)
	svc := New(failingAuditStore{Store: e.st}, e.svc.keys, zap.New(core))

	if _, err := svc.CreateProject(ctx, "p-fail", "p-fail"); err == nil {
		t.Fatal("expected CreateProject to fail when its paired audit write fails")
	}
	// The approved atomicity guarantee: the business mutation must not have
	// committed without its audit event.
	if _, err := e.st.GetProjectBySlug(ctx, "p-fail"); err != store.ErrNotFound {
		t.Fatalf("business mutation must have rolled back with the failed audit write, got: %v", err)
	}

	found := false
	for _, ent := range logs.All() {
		if ent.Level.String() == "error" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected an error-level log entry reporting the audit-write failure, found none")
	}
}

// eventsOfType returns every event of the given type, in chain order.
func eventsOfType(t *testing.T, st *sqlite.Store, typ store.AuditEventType) []store.AuditEvent {
	t.Helper()
	all, err := st.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var out []store.AuditEvent
	for _, e := range all {
		if e.EventType == typ {
			out = append(out, e)
		}
	}
	return out
}

func TestAudit_EachOperationCreatesExactlyOneEvent(t *testing.T) {
	e := setup(t)

	if _, err := e.svc.CreateProject(ctx, "p1", "p1"); err != nil {
		t.Fatal(err)
	}
	if got := eventsOfType(t, e.st, store.AuditProjectCreate); len(got) != 1 {
		t.Fatalf("CreateProject: expected exactly 1 event, got %d", len(got))
	}

	if _, err := e.svc.CreateEnvironment(ctx, "p1", "env1", "env1"); err != nil {
		t.Fatal(err)
	}
	if got := eventsOfType(t, e.st, store.AuditEnvironmentCreate); len(got) != 1 {
		t.Fatalf("CreateEnvironment: expected exactly 1 event, got %d", len(got))
	}

	if err := e.svc.CreateSecret(ctx, "p1", "env1", "s1", []byte("v1"), "alice", ""); err != nil {
		t.Fatal(err)
	}
	if got := eventsOfType(t, e.st, store.AuditSecretCreate); len(got) != 1 {
		t.Fatalf("CreateSecret: expected exactly 1 event, got %d", len(got))
	}

	if _, err := e.svc.AddVersion(ctx, "p1", "env1", "s1", []byte("v2"), "alice", ""); err != nil {
		t.Fatal(err)
	}
	if got := eventsOfType(t, e.st, store.AuditSecretAddVersion); len(got) != 1 {
		t.Fatalf("AddVersion: expected exactly 1 event, got %d", len(got))
	}

	if _, err := e.svc.GetValue(ctx, "p1", "env1", "s1", 0); err != nil {
		t.Fatal(err)
	}
	if got := eventsOfType(t, e.st, store.AuditSecretGetReveal); len(got) != 1 {
		t.Fatalf("GetValue: expected exactly 1 event, got %d", len(got))
	}

	if err := e.svc.DisableVersion(ctx, "p1", "env1", "s1", 1, "alice"); err != nil {
		t.Fatal(err)
	}
	if got := eventsOfType(t, e.st, store.AuditSecretDisableVersion); len(got) != 1 {
		t.Fatalf("DisableVersion: expected exactly 1 event, got %d", len(got))
	}

	if err := e.svc.DeleteSecret(ctx, "p1", "env1", "s1", "alice"); err != nil {
		t.Fatal(err)
	}
	if got := eventsOfType(t, e.st, store.AuditSecretDelete); len(got) != 1 {
		t.Fatalf("DeleteSecret: expected exactly 1 event, got %d", len(got))
	}

	// The full chain must verify: every write above shares one consistent
	// hash chain, including the standalone GetValue reveal event interleaved
	// between transactional writes.
	all, err := e.st.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlite.VerifyAuditChain(all); err != nil {
		t.Fatalf("chain must verify cleanly across mixed transactional and standalone appends: %v", err)
	}
}

func TestAudit_ExcludedListOperationsCreateNoEvents(t *testing.T) {
	e := setup(t)
	e.project(t, "p1", "env1")
	if err := e.svc.CreateSecret(ctx, "p1", "env1", "s1", []byte("v1"), "alice", ""); err != nil {
		t.Fatal(err)
	}
	before, err := e.st.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := e.svc.ListProjects(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.ListEnvironments(ctx, "p1"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.ListSecrets(ctx, "p1", "env1"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.ListVersions(ctx, "p1", "env1", "s1"); err != nil {
		t.Fatal(err)
	}

	after, err := e.st.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("metadata-only list operations must not create audit events: %d before, %d after", len(before), len(after))
	}
}

func TestAudit_RevealEventNeverContainsPlaintext(t *testing.T) {
	e := setup(t)
	e.project(t, "p1", "env1")
	if err := e.svc.CreateSecret(ctx, "p1", "env1", "s1", []byte(plain), "alice", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.GetValue(ctx, "p1", "env1", "s1", 0); err != nil {
		t.Fatal(err)
	}
	all, err := e.st.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range all {
		for _, field := range []string{ev.ID, ev.ActorLabel, ev.ProjectSlug, ev.EnvironmentSlug, ev.SecretName, ev.ReasonCategory} {
			if strings.Contains(field, plain) {
				t.Fatalf("audit event field unexpectedly contains the plaintext secret value: %q", field)
			}
		}
		if bytes.Contains(ev.PrevHash, []byte(plain)) || bytes.Contains(ev.EventHash, []byte(plain)) {
			t.Fatal("audit event hash fields unexpectedly contain the plaintext secret value")
		}
	}
}

func TestAudit_FailureOutcomesAreRecordedWithoutRawErrorText(t *testing.T) {
	e := setup(t)
	e.project(t, "p1", "env1")

	// GetValue against a secret that does not exist: a failure event, not a
	// silently-dropped one, and no success event pretending otherwise.
	if _, err := e.svc.GetValue(ctx, "p1", "env1", "does-not-exist", 0); err == nil {
		t.Fatal("expected an error for a nonexistent secret")
	}
	reveals := eventsOfType(t, e.st, store.AuditSecretGetReveal)
	if len(reveals) != 1 {
		t.Fatalf("expected exactly 1 reveal event (the failure), got %d", len(reveals))
	}
	if reveals[0].Outcome != store.AuditFailure {
		t.Fatalf("expected outcome=failure, got %q", reveals[0].Outcome)
	}
	if reveals[0].ReasonCategory == "" {
		t.Fatal("expected a non-empty reason category for the failure")
	}
	// The category must be a closed value, never a raw Go error string
	// (which could echo implementation detail): confirm it matches one of
	// the fixed categories reasonCategory can produce.
	switch reveals[0].ReasonCategory {
	case "not_found", "conflict", "validation_error", "version_disabled", "integrity_error", "wrong_root_key", "db_error":
		// ok
	default:
		t.Fatalf("reason category %q is not one of the closed set -- looks like a raw error string leaked through", reveals[0].ReasonCategory)
	}
}

func TestAudit_BusinessFailureDoesNotLeaveASuccessEvent(t *testing.T) {
	e := setup(t)
	e.project(t, "p1", "env1")
	// A project slug collision: CreateProject fails inside its own WithTx, so
	// the attempted success event must have rolled back with it, and only
	// the separate, standalone failure event (recorded after rollback) exists.
	if _, err := e.svc.CreateProject(ctx, "p1", "p1 again"); err == nil {
		t.Fatal("expected a conflict error for a duplicate project slug")
	}
	got := eventsOfType(t, e.st, store.AuditProjectCreate)
	// One success event for the original e.project(t, "p1", "env1") call's
	// CreateProject, plus one failure event for the collision attempt -- never
	// a second success event for the attempt that actually failed.
	successes, failures := 0, 0
	for _, ev := range got {
		switch ev.Outcome {
		case store.AuditSuccess:
			successes++
		case store.AuditFailure:
			failures++
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("expected exactly 1 success and 1 failure event, got %d success, %d failure", successes, failures)
	}
}

func TestAudit_ActorLabelNeverConflatedWithVerifiedIdentity(t *testing.T) {
	e := setup(t)
	e.project(t, "p1", "env1")
	if err := e.svc.CreateSecret(ctx, "p1", "env1", "s1", []byte("v1"), "alice-the-caller-supplied-label", ""); err != nil {
		t.Fatal(err)
	}
	got := eventsOfType(t, e.st, store.AuditSecretCreate)
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].ActorLabel != "alice-the-caller-supplied-label" {
		t.Fatalf("expected the caller-supplied actor label to be recorded as-is, got %q", got[0].ActorLabel)
	}
	// No verified_identity field is exposed by store.AuditEvent in this
	// milestone (no Option B socket exists yet to populate one) -- there is
	// deliberately nothing here to assert "is populated", only that the
	// caller-supplied label is never promoted into something stronger than
	// what it is: attribution only, per the approved Step 5.2 design.
}
