package sqlite

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/internal/securefs/securefstest"
)

// Review finding M1-3: only the database FILE was checked; in a directory other
// accounts can write to they could rename it away and substitute their own.
func TestOpen_RefusesUnsafeDirectories(t *testing.T) {
	root := securefstest.TempDir(t)
	for _, mode := range []os.FileMode{0o777, 0o775, 0o770, 0o757} {
		d := filepath.Join(root, "d"+strings.ReplaceAll(mode.String(), "-", ""))
		os.Mkdir(d, 0o700)
		os.Chmod(d, mode)
		if _, err := Open(ctx, filepath.Join(d, "c.db"), Options{}); err == nil || !strings.Contains(err.Error(), "writable by group or others") {
			t.Errorf("directory mode %04o accepted: %v", mode, err)
		}
		if _, err := os.Stat(filepath.Join(d, "c.db")); err == nil {
			t.Errorf("mode %04o: a database file was created in an unsafe directory", mode)
		}
	}
}

func TestOpen_UnsafeAncestorIsRefused(t *testing.T) {
	root := securefstest.TempDir(t)
	top := filepath.Join(root, "top")
	os.Mkdir(top, 0o700)
	os.Chmod(top, 0o777)
	if _, err := Open(ctx, filepath.Join(top, "inner", "c.db"), Options{}); err == nil {
		t.Fatal("database created beneath a world-writable ancestor")
	}
}

func TestOpen_DatabaseDirectoryBecomingUnsafeIsDetectedOnNextOpen(t *testing.T) {
	dir := securefstest.TempDir(t)
	p := filepath.Join(dir, "c.db")
	s, err := Open(ctx, p, Options{})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	os.Chmod(dir, 0o777)
	defer os.Chmod(dir, 0o700)
	if _, err := Open(ctx, p, Options{}); err == nil {
		t.Fatal("reopened a database whose directory became world-writable")
	}
}

func TestOpen_SymlinkedDatabasePathIsRefused(t *testing.T) {
	dir := securefstest.TempDir(t)
	real := filepath.Join(dir, "real.db")
	s, _ := Open(ctx, real, Options{})
	s.Close()
	link := filepath.Join(dir, "link.db")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	if _, err := Open(ctx, link, Options{}); err == nil {
		t.Fatal("symlinked database path accepted")
	}
	// a dangling link planted where the database should be created must not be followed
	dangling := filepath.Join(dir, "dangling.db")
	os.Symlink(filepath.Join(dir, "victim"), dangling)
	if _, err := Open(ctx, dangling, Options{}); err == nil {
		t.Fatal("dangling symlink followed")
	}
	if _, err := os.Stat(filepath.Join(dir, "victim")); err == nil {
		t.Fatal("a file was created through the planted symlink")
	}
}

func TestOpen_DatabaseUnderSafeSymlinkedDirectoryWorks(t *testing.T) {
	root := securefstest.TempDir(t)
	real := filepath.Join(root, "real")
	os.Mkdir(real, 0o700)
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	s, err := Open(ctx, filepath.Join(link, "c.db"), Options{})
	if err != nil {
		t.Fatalf("safe symlinked directory rejected: %v", err)
	}
	s.Close()
}
