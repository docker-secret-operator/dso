package centralsvc

import (
	"errors"
	"reflect"
	"testing"

	"github.com/docker-secret-operator/dso/internal/store"
)

// TestRollbackSecret_CreatesNewVersionWithOldValue is the core happy path: a
// rollback to an older version appends a brand-new version carrying that
// version's plaintext, and never modifies any existing version's ciphertext
// (versions are immutable).
func TestRollbackSecret_CreatesNewVersionWithOldValue(t *testing.T) {
	e := setup(t)
	e.project(t, "p1", "env1")
	if err := e.svc.CreateSecret(ctx, "p1", "env1", "s1", []byte("a"), "alice", "first"); err != nil {
		t.Fatal(err)
	}
	if v, err := e.svc.AddVersion(ctx, "p1", "env1", "s1", []byte("b"), "alice", "second"); err != nil || v != 2 {
		t.Fatalf("AddVersion: v=%d err=%v", v, err)
	}

	newVersion, err := e.svc.RollbackSecret(ctx, "p1", "env1", "s1", 1, "bob", "rollback to v1")
	if err != nil {
		t.Fatalf("RollbackSecret: %v", err)
	}
	if newVersion != 3 {
		t.Fatalf("expected new version 3, got %d", newVersion)
	}

	if got := mustGet(t, e, "p1", "env1", "s1", 3); got != "a" {
		t.Fatalf("new version 3 = %q, want %q", got, "a")
	}
	if got := mustGet(t, e, "p1", "env1", "s1", 1); got != "a" {
		t.Fatalf("version 1 changed after rollback: %q", got)
	}
	if got := mustGet(t, e, "p1", "env1", "s1", 2); got != "b" {
		t.Fatalf("version 2 changed after rollback: %q", got)
	}

	vs, err := e.svc.ListVersions(ctx, "p1", "env1", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 3 {
		t.Fatalf("expected 3 versions, got %d", len(vs))
	}
	if vs[2].Version != 3 || vs[2].CreatedBy != "bob" || vs[2].Comment != "rollback to v1" {
		t.Fatalf("new version metadata: %+v", vs[2])
	}
}

// TestRollbackSecret_FromDisabledVersionRejected is Review Focus #1: rolling
// back from a disabled source version must fail, and must append nothing.
func TestRollbackSecret_FromDisabledVersionRejected(t *testing.T) {
	e := setup(t)
	e.project(t, "p1", "env1")
	if err := e.svc.CreateSecret(ctx, "p1", "env1", "s1", []byte("a"), "alice", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.AddVersion(ctx, "p1", "env1", "s1", []byte("b"), "alice", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.DisableVersion(ctx, "p1", "env1", "s1", 1, "alice"); err != nil {
		t.Fatal(err)
	}

	before, err := e.svc.ListVersions(ctx, "p1", "env1", "s1")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := e.svc.RollbackSecret(ctx, "p1", "env1", "s1", 1, "bob", ""); err == nil {
		t.Fatal("expected RollbackSecret from a disabled source version to fail")
	} else if !errors.Is(err, store.ErrVersionDisabled) {
		t.Fatalf("expected store.ErrVersionDisabled, got %v", err)
	}

	after, err := e.svc.ListVersions(ctx, "p1", "env1", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("version count changed after rejected rollback: before=%d after=%d", len(before), len(after))
	}
}

// TestRollbackSecret_FromCurrentLatestSucceeds is Review Focus #2: rolling
// back from the current latest (enabled) version must succeed, appending a
// new version with the same plaintext as the latest.
func TestRollbackSecret_FromCurrentLatestSucceeds(t *testing.T) {
	e := setup(t)
	e.project(t, "p1", "env1")
	if err := e.svc.CreateSecret(ctx, "p1", "env1", "s1", []byte("a"), "alice", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.AddVersion(ctx, "p1", "env1", "s1", []byte("b"), "alice", ""); err != nil {
		t.Fatal(err)
	}

	newVersion, err := e.svc.RollbackSecret(ctx, "p1", "env1", "s1", 2, "bob", "")
	if err != nil {
		t.Fatalf("RollbackSecret from latest: %v", err)
	}
	if newVersion != 3 {
		t.Fatalf("expected new version 3, got %d", newVersion)
	}
	if got := mustGet(t, e, "p1", "env1", "s1", 3); got != "b" {
		t.Fatalf("new version 3 = %q, want %q", got, "b")
	}
	if got := mustGet(t, e, "p1", "env1", "s1", 0); got != "b" {
		t.Fatalf("latest after rollback = %q, want %q", got, "b")
	}
}

func TestRollbackSecret_MissingProject(t *testing.T) {
	e := setup(t)
	if _, err := e.svc.RollbackSecret(ctx, "no-such-project", "env1", "s1", 1, "bob", ""); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRollbackSecret_MissingEnvironment(t *testing.T) {
	e := setup(t)
	e.project(t, "p1")
	if _, err := e.svc.RollbackSecret(ctx, "p1", "no-such-env", "s1", 1, "bob", ""); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRollbackSecret_MissingSecret(t *testing.T) {
	e := setup(t)
	e.project(t, "p1", "env1")
	if _, err := e.svc.RollbackSecret(ctx, "p1", "env1", "no-such-secret", 1, "bob", ""); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRollbackSecret_DeletedSecretRejected(t *testing.T) {
	e := setup(t)
	e.project(t, "p1", "env1")
	if err := e.svc.CreateSecret(ctx, "p1", "env1", "s1", []byte("a"), "alice", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.DeleteSecret(ctx, "p1", "env1", "s1", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.RollbackSecret(ctx, "p1", "env1", "s1", 1, "bob", ""); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for a deleted secret, got %v", err)
	}
}

func TestRollbackSecret_MissingVersionRejected(t *testing.T) {
	e := setup(t)
	e.project(t, "p1", "env1")
	if err := e.svc.CreateSecret(ctx, "p1", "env1", "s1", []byte("a"), "alice", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.RollbackSecret(ctx, "p1", "env1", "s1", 99, "bob", ""); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for a nonexistent version, got %v", err)
	}
}

// TestRollbackSecret_AppendsAuditEvent checks that exactly one
// AuditSecretRollback success event is recorded, carrying the actor (as
// ActorLabel) and the new version number -- never a secret value. The
// comment is recorded on the new version row itself (ListVersions), exactly
// like every other mutator in this service: no existing AuditEvent field
// carries a free-text comment.
func TestRollbackSecret_AppendsAuditEvent(t *testing.T) {
	e := setup(t)
	e.project(t, "p1", "env1")
	if err := e.svc.CreateSecret(ctx, "p1", "env1", "s1", []byte("a"), "alice", ""); err != nil {
		t.Fatal(err)
	}
	newVersion, err := e.svc.RollbackSecret(ctx, "p1", "env1", "s1", 1, "bob", "rollback comment")
	if err != nil {
		t.Fatal(err)
	}

	got := eventsOfType(t, e.st, store.AuditSecretRollback)
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 AuditSecretRollback event, got %d", len(got))
	}
	ev := got[0]
	if ev.Outcome != store.AuditSuccess {
		t.Fatalf("expected outcome=success, got %q", ev.Outcome)
	}
	if ev.ActorLabel != "bob" {
		t.Fatalf("expected actor label %q, got %q", "bob", ev.ActorLabel)
	}
	if ev.Version != newVersion {
		t.Fatalf("expected audit event version %d (the new version), got %d", newVersion, ev.Version)
	}
	if ev.ProjectSlug != "p1" || ev.EnvironmentSlug != "env1" || ev.SecretName != "s1" {
		t.Fatalf("unexpected scope on audit event: %+v", ev)
	}

	vs, err := e.svc.ListVersions(ctx, "p1", "env1", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if vs[len(vs)-1].Comment != "rollback comment" {
		t.Fatalf("expected the new version to carry the rollback comment, got %+v", vs[len(vs)-1])
	}
}

// TestRollbackSecret_NeverReturnsPlaintext is a type-level check: the method
// signature itself must be exactly (int, error) with no way for a plaintext
// value to travel through the return path.
func TestRollbackSecret_NeverReturnsPlaintext(t *testing.T) {
	var s *Service
	m, ok := reflect.TypeOf(s).MethodByName("RollbackSecret")
	if !ok {
		t.Fatal("RollbackSecret method not found")
	}
	// Method signature: func(*Service, context.Context, string, string, string, int, string, string) (int, error)
	ft := m.Type
	if ft.NumOut() != 2 {
		t.Fatalf("expected 2 return values, got %d", ft.NumOut())
	}
	if ft.Out(0).Kind() != reflect.Int {
		t.Fatalf("expected first return value to be int, got %s", ft.Out(0))
	}
	errType := reflect.TypeOf((*error)(nil)).Elem()
	if !ft.Out(1).Implements(errType) || ft.Out(1) != errType {
		t.Fatalf("expected second return value to be exactly the error interface, got %s", ft.Out(1))
	}
}
