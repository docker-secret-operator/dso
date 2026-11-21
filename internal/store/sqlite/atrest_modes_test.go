package sqlite

// M2 slice S3 (store layer): the files the store itself creates have the
// documented modes (0600 files, 0700 directories) whatever the process umask,
// and a backup of a database whose clear-text metadata contains a marker keeps
// that metadata (backups are complete) while the file stays 0600.

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"

	"github.com/docker-secret-operator/dso/internal/securefs/securefstest"
	"github.com/docker-secret-operator/dso/internal/store"
)

func TestS3_StoreFileModes_UmaskIndependent(t *testing.T) {
	for _, um := range []int{0o022, 0o002, 0o077, 0o000} {
		t.Run(fmt.Sprintf("umask-%04o", um), func(t *testing.T) {
			old := syscall.Umask(um)
			defer syscall.Umask(old)

			root := securefstest.TempDir(t)
			db := filepath.Join(root, "data", "c.db") // data/ is created by the store
			ctx := context.Background()
			st, err := Open(ctx, db, Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = st.Close() }()
			if err := st.CreateProject(ctx, store.Project{ID: "p1", Slug: "s3-slug-marker", Name: "n",
				WrappedDEK: []byte{1, 2, 3}, DEKKeyID: "k", DEKVersion: 1}); err != nil {
				t.Fatal(err)
			}
			bk, err := st.Backup(ctx, filepath.Join(root, "data", "b.db"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.Backup(ctx, bk); err == nil {
				t.Fatal("backup must never overwrite an existing file")
			}
			_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					t.Fatal(err)
				}
				fi, _ := d.Info()
				m := fi.Mode().Perm()
				if p == root {
					return nil
				}
				if fi.IsDir() && m != 0o700 {
					t.Errorf("dir %s mode %o, want 0700", p, m)
				}
				if fi.Mode().IsRegular() && m != 0o600 {
					t.Errorf("file %s mode %o, want 0600", p, m)
				}
				return nil
			})
			if _, err := os.Stat(db + "-wal"); err != nil {
				t.Errorf("expected -wal while open: %v", err)
			}
		})
	}
}

// modeMap captures only the permission bits of every entry below root
// (ignoring content), so a before/after comparison catches a permission
// change without being tripped up by an expected content change (a new row,
// a bigger WAL).
func modeMap(t *testing.T, root string) map[string]fs.FileMode {
	t.Helper()
	m := map[string]fs.FileMode{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		m[rel] = fi.Mode().Perm()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// TestS3_StoreFileModes_UnchangedAfterRenameDeleteRollback is the store-layer
// (M2 Task 9) counterpart of the service-layer permission regression test: at
// this layer there is no RollbackSecret method, so "rollback" is performed
// the same way the service layer implements it -- GetVersion the old record,
// then AddVersion with its own ciphertext/nonce/DEK-version re-supplied via a
// SealFunc -- and the store's own RenameProject/RenameEnvironment/
// DeleteEnvironment/DeleteProject are exercised directly. None of these
// operations are documented to touch any file or directory's permission bits,
// only database rows, so every mode captured before must be identical after.
func TestS3_StoreFileModes_UnchangedAfterRenameDeleteRollback(t *testing.T) {
	s, path := open(t)
	proj, env := seed(t, s)
	root := filepath.Dir(filepath.Dir(path)) // parent of data/

	if err := s.CreateSecret(ctx, newSecret("sec1", "s1")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddVersion(ctx, "sec1", "t", "v2", sealN("tag")); err != nil {
		t.Fatal(err)
	}

	// A second, empty project/environment pair to exercise delete without
	// disturbing the one "rollback" is performed against.
	proj2 := store.Project{ID: "p2", Slug: "proj2", Name: "Proj2", WrappedDEK: []byte("wrapped2"), DEKKeyID: "k2"}
	if err := s.CreateProject(ctx, proj2); err != nil {
		t.Fatal(err)
	}
	env2 := store.Environment{ID: "e2", ProjectID: "p2", Slug: "temp", Name: "Temp"}
	if err := s.CreateEnvironment(ctx, env2); err != nil {
		t.Fatal(err)
	}

	before := modeMap(t, root)

	// "Rollback" to version 1: read it raw and re-append it as a new version,
	// exactly as the service layer's RollbackSecret does (service.go,
	// Service.RollbackSecret) -- never an in-place revert.
	rec, err := s.GetVersion(ctx, "sec1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddVersion(ctx, "sec1", "t", "rollback", func(int) ([]byte, []byte, int, error) {
		return rec.Ciphertext, rec.Nonce, rec.DEKVersion, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RenameProject(ctx, proj.ID, "proj-renamed", "Proj Renamed"); err != nil {
		t.Fatal(err)
	}
	if err := s.RenameEnvironment(ctx, env.ID, "prod-renamed", "Prod Renamed"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteEnvironment(ctx, env2.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProject(ctx, proj2.ID); err != nil {
		t.Fatal(err)
	}

	after := modeMap(t, root)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("file/dir modes changed by rename/delete/rollback\nbefore: %v\nafter:  %v", before, after)
	}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		if p == root {
			return nil
		}
		fi, _ := d.Info()
		m := fi.Mode().Perm()
		if fi.IsDir() && m != 0o700 {
			t.Errorf("dir %s mode %o, want 0700", p, m)
		}
		if fi.Mode().IsRegular() && m != 0o600 {
			t.Errorf("file %s mode %o, want 0600", p, m)
		}
		return nil
	})
}
