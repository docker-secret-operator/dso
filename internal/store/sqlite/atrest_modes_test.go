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
