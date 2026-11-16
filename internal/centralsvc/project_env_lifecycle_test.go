package centralsvc

import (
	"errors"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/internal/store"
)

// ---- RenameProject ----------------------------------------------------------

func TestRenameProject_Service_Success(t *testing.T) {
	e := setup(t)
	e.project(t, "p1")

	if err := e.svc.RenameProject(ctx, "p1", "p1-renamed", "P1 Renamed", "alice"); err != nil {
		t.Fatalf("RenameProject: %v", err)
	}

	if _, err := e.st.GetProjectBySlug(ctx, "p1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("old slug should no longer resolve, got %v", err)
	}
	got, err := e.st.GetProjectBySlug(ctx, "p1-renamed")
	if err != nil {
		t.Fatalf("new slug should resolve: %v", err)
	}
	if got.Name != "P1 Renamed" {
		t.Fatalf("expected renamed project name, got %q", got.Name)
	}

	events := eventsOfType(t, e.st, store.AuditProjectRename)
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 AuditProjectRename event, got %d", len(events))
	}
	ev := events[0]
	if ev.Outcome != store.AuditSuccess {
		t.Fatalf("expected outcome=success, got %q", ev.Outcome)
	}
	if ev.ActorLabel != "alice" {
		t.Fatalf("expected actor label %q, got %q", "alice", ev.ActorLabel)
	}
	if ev.ProjectSlug != "p1" {
		t.Fatalf("expected audit event to carry the request's slug %q, got %q", "p1", ev.ProjectSlug)
	}
}

func TestRenameProject_Service_NotFound(t *testing.T) {
	e := setup(t)

	if err := e.svc.RenameProject(ctx, "no-such-project", "new-slug", "New Name", "alice"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	events := eventsOfType(t, e.st, store.AuditProjectRename)
	if len(events) != 1 || events[0].Outcome != store.AuditFailure {
		t.Fatalf("expected exactly 1 failure AuditProjectRename event, got %+v", events)
	}
	if events[0].ReasonCategory != "not_found" {
		t.Fatalf("expected reason category not_found, got %q", events[0].ReasonCategory)
	}
}

// TestRenameProject_Service_SlugCollisionIsCleanConflict is Review Focus #4:
// the store's raw SQLite unique-constraint error must never reach the caller;
// it must surface as store.ErrConflict via the service's existing "conflict"
// reasonCategory, with no SQLite-flavored text in the error.
func TestRenameProject_Service_SlugCollisionIsCleanConflict(t *testing.T) {
	e := setup(t)
	e.project(t, "p1")
	e.project(t, "p2")

	err := e.svc.RenameProject(ctx, "p2", "p1", "P2 As P1", "alice")
	if err == nil {
		t.Fatal("expected slug collision to fail")
	}
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected store.ErrConflict, got %v", err)
	}
	for _, needle := range []string{"UNIQUE", "SQLITE", "constraint failed", "sqlite3"} {
		if strings.Contains(strings.ToUpper(err.Error()), strings.ToUpper(needle)) {
			t.Fatalf("raw SQLite error text leaked to caller: %v", err)
		}
	}

	events := eventsOfType(t, e.st, store.AuditProjectRename)
	if len(events) != 1 || events[0].Outcome != store.AuditFailure {
		t.Fatalf("expected exactly 1 failure AuditProjectRename event, got %+v", events)
	}
	if events[0].ReasonCategory != "conflict" {
		t.Fatalf("expected reason category conflict, got %q", events[0].ReasonCategory)
	}

	// p2 must be entirely unchanged: a rejected rename is not a partial one.
	p2, err := e.st.GetProjectBySlug(ctx, "p2")
	if err != nil {
		t.Fatalf("p2 should still resolve by its original slug: %v", err)
	}
	if p2.Name != "p2" {
		t.Fatalf("p2 name must be unchanged after rejected rename, got %q", p2.Name)
	}
}

// ---- RenameEnvironment -------------------------------------------------------

func TestRenameEnvironment_Service_Success(t *testing.T) {
	e := setup(t)
	e.project(t, "p1", "env1")

	if err := e.svc.RenameEnvironment(ctx, "p1", "env1", "env1-renamed", "Env1 Renamed", "alice"); err != nil {
		t.Fatalf("RenameEnvironment: %v", err)
	}

	p, err := e.st.GetProjectBySlug(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.GetEnvironmentBySlug(ctx, p.ID, "env1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("old env slug should no longer resolve, got %v", err)
	}
	got, err := e.st.GetEnvironmentBySlug(ctx, p.ID, "env1-renamed")
	if err != nil {
		t.Fatalf("new env slug should resolve: %v", err)
	}
	if got.Name != "Env1 Renamed" {
		t.Fatalf("expected renamed environment name, got %q", got.Name)
	}

	events := eventsOfType(t, e.st, store.AuditEnvironmentRename)
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 AuditEnvironmentRename event, got %d", len(events))
	}
	ev := events[0]
	if ev.Outcome != store.AuditSuccess {
		t.Fatalf("expected outcome=success, got %q", ev.Outcome)
	}
	if ev.ActorLabel != "alice" {
		t.Fatalf("expected actor label %q, got %q", "alice", ev.ActorLabel)
	}
	if ev.ProjectSlug != "p1" || ev.EnvironmentSlug != "env1" {
		t.Fatalf("expected audit event to carry the request's scope p1/env1, got %q/%q", ev.ProjectSlug, ev.EnvironmentSlug)
	}
}

func TestRenameEnvironment_Service_NotFound(t *testing.T) {
	e := setup(t)
	e.project(t, "p1")

	if err := e.svc.RenameEnvironment(ctx, "p1", "no-such-env", "new-slug", "New Name", "alice"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	events := eventsOfType(t, e.st, store.AuditEnvironmentRename)
	if len(events) != 1 || events[0].Outcome != store.AuditFailure {
		t.Fatalf("expected exactly 1 failure AuditEnvironmentRename event, got %+v", events)
	}
	if events[0].ReasonCategory != "not_found" {
		t.Fatalf("expected reason category not_found, got %q", events[0].ReasonCategory)
	}
}

func TestRenameEnvironment_Service_SlugCollisionIsCleanConflict(t *testing.T) {
	e := setup(t)
	e.project(t, "p1", "env1", "env2")

	err := e.svc.RenameEnvironment(ctx, "p1", "env2", "env1", "Env2 As Env1", "alice")
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected store.ErrConflict, got %v", err)
	}
	for _, needle := range []string{"UNIQUE", "SQLITE", "constraint failed", "sqlite3"} {
		if strings.Contains(strings.ToUpper(err.Error()), strings.ToUpper(needle)) {
			t.Fatalf("raw SQLite error text leaked to caller: %v", err)
		}
	}

	events := eventsOfType(t, e.st, store.AuditEnvironmentRename)
	if len(events) != 1 || events[0].Outcome != store.AuditFailure || events[0].ReasonCategory != "conflict" {
		t.Fatalf("expected exactly 1 failure/conflict AuditEnvironmentRename event, got %+v", events)
	}
}

// ---- DeleteProject ------------------------------------------------------------

func TestDeleteProject_Service_EmptySucceedsWithAudit(t *testing.T) {
	e := setup(t)
	e.project(t, "p1")

	if err := e.svc.DeleteProject(ctx, "p1", "alice"); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	if _, err := e.st.GetProjectBySlug(ctx, "p1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected project gone, got %v", err)
	}

	events := eventsOfType(t, e.st, store.AuditProjectDelete)
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 AuditProjectDelete event, got %d", len(events))
	}
	ev := events[0]
	if ev.Outcome != store.AuditSuccess {
		t.Fatalf("expected outcome=success, got %q", ev.Outcome)
	}
	if ev.ActorLabel != "alice" || ev.ProjectSlug != "p1" {
		t.Fatalf("unexpected audit event: %+v", ev)
	}
}

func TestDeleteProject_Service_NotFound(t *testing.T) {
	e := setup(t)

	if err := e.svc.DeleteProject(ctx, "no-such-project", "alice"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	events := eventsOfType(t, e.st, store.AuditProjectDelete)
	if len(events) != 1 || events[0].Outcome != store.AuditFailure || events[0].ReasonCategory != "not_found" {
		t.Fatalf("expected exactly 1 failure/not_found AuditProjectDelete event, got %+v", events)
	}
}

// TestDeleteProject_Service_NonEmptyRejectedWithFailureAudit proves the
// store's non-empty-project ErrConflict is wired all the way through to a
// FAILURE audit event, and that the project (and its environment) survive
// the rejected attempt untouched.
func TestDeleteProject_Service_NonEmptyRejectedWithFailureAudit(t *testing.T) {
	e := setup(t)
	e.project(t, "p1", "env1")

	err := e.svc.DeleteProject(ctx, "p1", "alice")
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected store.ErrConflict for a non-empty project, got %v", err)
	}

	if _, err := e.st.GetProjectBySlug(ctx, "p1"); err != nil {
		t.Fatalf("project must survive a rejected delete: %v", err)
	}

	events := eventsOfType(t, e.st, store.AuditProjectDelete)
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 AuditProjectDelete event, got %d", len(events))
	}
	ev := events[0]
	if ev.Outcome != store.AuditFailure {
		t.Fatalf("expected outcome=failure, got %q", ev.Outcome)
	}
	if ev.ReasonCategory != "conflict" {
		t.Fatalf("expected reason category conflict, got %q", ev.ReasonCategory)
	}
	if ev.ActorLabel != "alice" || ev.ProjectSlug != "p1" {
		t.Fatalf("unexpected audit event: %+v", ev)
	}
}

// ---- DeleteEnvironment --------------------------------------------------------

func TestDeleteEnvironment_Service_EmptySucceedsWithAudit(t *testing.T) {
	e := setup(t)
	e.project(t, "p1", "env1")

	if err := e.svc.DeleteEnvironment(ctx, "p1", "env1", "alice"); err != nil {
		t.Fatalf("DeleteEnvironment: %v", err)
	}
	p, err := e.st.GetProjectBySlug(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.GetEnvironmentBySlug(ctx, p.ID, "env1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected environment gone, got %v", err)
	}

	events := eventsOfType(t, e.st, store.AuditEnvironmentDelete)
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 AuditEnvironmentDelete event, got %d", len(events))
	}
	ev := events[0]
	if ev.Outcome != store.AuditSuccess {
		t.Fatalf("expected outcome=success, got %q", ev.Outcome)
	}
	if ev.ActorLabel != "alice" || ev.ProjectSlug != "p1" || ev.EnvironmentSlug != "env1" {
		t.Fatalf("unexpected audit event: %+v", ev)
	}
}

func TestDeleteEnvironment_Service_NotFound(t *testing.T) {
	e := setup(t)
	e.project(t, "p1")

	if err := e.svc.DeleteEnvironment(ctx, "p1", "no-such-env", "alice"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	events := eventsOfType(t, e.st, store.AuditEnvironmentDelete)
	if len(events) != 1 || events[0].Outcome != store.AuditFailure || events[0].ReasonCategory != "not_found" {
		t.Fatalf("expected exactly 1 failure/not_found AuditEnvironmentDelete event, got %+v", events)
	}
}

// TestDeleteEnvironment_Service_NonEmptyRejectedWithFailureAudit proves the
// store's non-empty-environment ErrConflict (including soft-deleted secrets
// still referenced by it) is wired through to a FAILURE audit event, and
// that the environment survives the rejected attempt untouched.
func TestDeleteEnvironment_Service_NonEmptyRejectedWithFailureAudit(t *testing.T) {
	e := setup(t)
	e.project(t, "p1", "env1")
	if err := e.svc.CreateSecret(ctx, "p1", "env1", "s1", []byte("a"), "alice", ""); err != nil {
		t.Fatal(err)
	}

	err := e.svc.DeleteEnvironment(ctx, "p1", "env1", "alice")
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected store.ErrConflict for a non-empty environment, got %v", err)
	}

	p, err := e.st.GetProjectBySlug(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.GetEnvironmentBySlug(ctx, p.ID, "env1"); err != nil {
		t.Fatalf("environment must survive a rejected delete: %v", err)
	}

	events := eventsOfType(t, e.st, store.AuditEnvironmentDelete)
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 AuditEnvironmentDelete event, got %d", len(events))
	}
	ev := events[0]
	if ev.Outcome != store.AuditFailure {
		t.Fatalf("expected outcome=failure, got %q", ev.Outcome)
	}
	if ev.ReasonCategory != "conflict" {
		t.Fatalf("expected reason category conflict, got %q", ev.ReasonCategory)
	}
	if ev.ActorLabel != "alice" || ev.ProjectSlug != "p1" || ev.EnvironmentSlug != "env1" {
		t.Fatalf("unexpected audit event: %+v", ev)
	}
}
