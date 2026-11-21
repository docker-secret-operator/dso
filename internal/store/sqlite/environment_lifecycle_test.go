package sqlite

// Tests for RenameEnvironment / DeleteEnvironment (M2: central store lifecycle, Task 3).

import (
	"errors"
	"testing"

	"github.com/docker-secret-operator/dso/internal/store"
)

func TestRenameEnvironment_Success(t *testing.T) {
	s, _ := open(t)
	seed(t, s) // creates project p1 and environment e1 (slug "prod")

	if err := s.RenameEnvironment(ctx, "e1", "new-slug", "New Name"); err != nil {
		t.Fatalf("RenameEnvironment: %v", err)
	}

	got, err := s.GetEnvironmentBySlug(ctx, "p1", "new-slug")
	if err != nil {
		t.Fatalf("GetEnvironmentBySlug(new-slug): %v", err)
	}
	if got.Slug != "new-slug" || got.Name != "New Name" {
		t.Fatalf("got slug=%q name=%q, want slug=new-slug name=New Name", got.Slug, got.Name)
	}

	if _, err := s.GetEnvironmentBySlug(ctx, "p1", "prod"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetEnvironmentBySlug(prod) = %v, want ErrNotFound", err)
	}
}

func TestRenameEnvironment_SlugCollisionWithinProject(t *testing.T) {
	s, _ := open(t)
	seed(t, s) // creates project p1 and environment e1 (slug "prod")
	e2 := store.Environment{ID: "e2", ProjectID: "p1", Slug: "staging", Name: "Staging"}
	if err := s.CreateEnvironment(ctx, e2); err != nil {
		t.Fatal(err)
	}

	err := s.RenameEnvironment(ctx, "e1", "staging", "Prod Renamed")
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("RenameEnvironment to colliding slug = %v, want ErrConflict", err)
	}
}

func TestRenameEnvironment_SlugReuseAcrossProjectsAllowed(t *testing.T) {
	s, _ := open(t)
	seed(t, s) // creates project p1 and environment e1 (slug "prod")
	p2 := store.Project{ID: "p2", Slug: "proj-two", Name: "Two", WrappedDEK: []byte("wrapped"), DEKKeyID: "k1"}
	if err := s.CreateProject(ctx, p2); err != nil {
		t.Fatal(err)
	}
	e2 := store.Environment{ID: "e2", ProjectID: "p2", Slug: "staging", Name: "Staging"}
	if err := s.CreateEnvironment(ctx, e2); err != nil {
		t.Fatal(err)
	}

	// e1 is in p1; renaming it to "staging" must succeed even though p2/e2
	// already uses "staging", since uniqueness is scoped to (project_id, slug).
	if err := s.RenameEnvironment(ctx, "e1", "staging", "Prod Renamed"); err != nil {
		t.Fatalf("RenameEnvironment across-project slug reuse: %v", err)
	}

	got, err := s.GetEnvironmentBySlug(ctx, "p1", "staging")
	if err != nil {
		t.Fatalf("GetEnvironmentBySlug(p1, staging): %v", err)
	}
	if got.ID != "e1" {
		t.Fatalf("got environment id %q, want e1", got.ID)
	}
}

func TestRenameEnvironment_NotFound(t *testing.T) {
	s, _ := open(t)
	err := s.RenameEnvironment(ctx, "unknown-id", "whatever-slug", "Whatever")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("RenameEnvironment(unknown) = %v, want ErrNotFound", err)
	}
}

func TestDeleteEnvironment_EmptySucceeds(t *testing.T) {
	s, _ := open(t)
	seed(t, s) // creates project p1 and environment e1

	if err := s.DeleteEnvironment(ctx, "e1"); err != nil {
		t.Fatalf("DeleteEnvironment: %v", err)
	}

	if _, err := s.GetEnvironmentBySlug(ctx, "p1", "prod"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetEnvironmentBySlug after delete = %v, want ErrNotFound", err)
	}
}

func TestDeleteEnvironment_NonEmptyActiveSecretRejected(t *testing.T) {
	s, _ := open(t)
	seed(t, s) // creates project p1 and environment e1
	if err := s.CreateSecret(ctx, newSecret("sec1", "secret-one")); err != nil {
		t.Fatal(err)
	}

	err := s.DeleteEnvironment(ctx, "e1")
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("DeleteEnvironment(non-empty active) = %v, want ErrConflict", err)
	}

	if _, err := s.GetEnvironmentBySlug(ctx, "p1", "prod"); err != nil {
		t.Fatalf("environment should still be present: %v", err)
	}
}

func TestDeleteEnvironment_NonEmptySoftDeletedSecretRejected(t *testing.T) {
	s, _ := open(t)
	seed(t, s) // creates project p1 and environment e1
	if err := s.CreateSecret(ctx, newSecret("sec1", "secret-one")); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSecret(ctx, "sec1", "tester"); err != nil {
		t.Fatalf("DeleteSecret (soft delete): %v", err)
	}

	// The secret is now soft-deleted and invisible to ListSecrets, but its
	// versions remain and still reference the environment: deletion must
	// still be rejected (Review Focus #3).
	secrets, err := s.ListSecrets(ctx, "e1")
	if err != nil {
		t.Fatal(err)
	}
	if len(secrets) != 0 {
		t.Fatalf("ListSecrets after soft-delete = %d entries, want 0", len(secrets))
	}

	err = s.DeleteEnvironment(ctx, "e1")
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("DeleteEnvironment(non-empty, soft-deleted secret) = %v, want ErrConflict", err)
	}
}

func TestDeleteEnvironment_NotFound(t *testing.T) {
	s, _ := open(t)
	err := s.DeleteEnvironment(ctx, "unknown-id")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("DeleteEnvironment(unknown) = %v, want ErrNotFound", err)
	}
}
