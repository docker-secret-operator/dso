package sqlite

// Tests for RenameProject / DeleteProject (M2: central store lifecycle, Task 2).

import (
	"errors"
	"testing"

	"github.com/docker-secret-operator/dso/internal/store"
)

func TestRenameProject_Success(t *testing.T) {
	s, _ := open(t)
	proj := store.Project{ID: "p1", Slug: "old-slug", Name: "Old Name", WrappedDEK: []byte("wrapped"), DEKKeyID: "k1"}
	if err := s.CreateProject(ctx, proj); err != nil {
		t.Fatal(err)
	}

	if err := s.RenameProject(ctx, "p1", "new-slug", "New Name"); err != nil {
		t.Fatalf("RenameProject: %v", err)
	}

	got, err := s.GetProject(ctx, "p1")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Slug != "new-slug" || got.Name != "New Name" {
		t.Fatalf("got slug=%q name=%q, want slug=new-slug name=New Name", got.Slug, got.Name)
	}

	if _, err := s.GetProjectBySlug(ctx, "old-slug"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetProjectBySlug(old-slug) = %v, want ErrNotFound", err)
	}
}

func TestRenameProject_SlugCollision(t *testing.T) {
	s, _ := open(t)
	p1 := store.Project{ID: "p1", Slug: "proj-one", Name: "One", WrappedDEK: []byte("wrapped"), DEKKeyID: "k1"}
	p2 := store.Project{ID: "p2", Slug: "proj-two", Name: "Two", WrappedDEK: []byte("wrapped"), DEKKeyID: "k1"}
	if err := s.CreateProject(ctx, p1); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateProject(ctx, p2); err != nil {
		t.Fatal(err)
	}

	err := s.RenameProject(ctx, "p1", "proj-two", "One")
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("RenameProject to colliding slug = %v, want ErrConflict", err)
	}
}

func TestRenameProject_NotFound(t *testing.T) {
	s, _ := open(t)
	err := s.RenameProject(ctx, "unknown-id", "whatever-slug", "Whatever")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("RenameProject(unknown) = %v, want ErrNotFound", err)
	}
}

func TestDeleteProject_EmptySucceeds(t *testing.T) {
	s, _ := open(t)
	proj := store.Project{ID: "p1", Slug: "proj", Name: "Proj", WrappedDEK: []byte("wrapped"), DEKKeyID: "k1"}
	if err := s.CreateProject(ctx, proj); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteProject(ctx, "p1"); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}

	if _, err := s.GetProject(ctx, "p1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetProject after delete = %v, want ErrNotFound", err)
	}
}

func TestDeleteProject_NonEmptyRejected(t *testing.T) {
	s, _ := open(t)
	seed(t, s) // creates project p1 and environment e1

	err := s.DeleteProject(ctx, "p1")
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("DeleteProject(non-empty) = %v, want ErrConflict", err)
	}

	if _, err := s.GetProject(ctx, "p1"); err != nil {
		t.Fatalf("project should still be present: %v", err)
	}
}

func TestDeleteProject_NotFound(t *testing.T) {
	s, _ := open(t)
	err := s.DeleteProject(ctx, "unknown-id")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("DeleteProject(unknown) = %v, want ErrNotFound", err)
	}
}
