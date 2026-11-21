//go:build linux

package ledger

import (
	"os"
	"path/filepath"
	"testing"
)

// The persistent-state directory contract as the key provider sees it: a
// root-owned 0750 /var/lib/dso (the parent) is accepted, the pre-upgrade 0770 is
// refused. Ownership is injected (the provider's ownerUID/euid seams) so no root
// is needed: the leaf and the key are "root-owned" while the test user is not
// root, exactly like the production agent running as root.
func TestKey_StateDirectoryContract_Parent0750Accepted_0770Refused(t *testing.T) {
	real := os.Geteuid()
	rootOwned := func(d *keyDeps) {
		d.ownerUID = func(os.FileInfo) (uint32, bool) { return 0, true } // root-owned
		d.euid = func() int { return real + 4242 }                       // the test user is "not" the owner
	}
	build := func(t *testing.T, parentMode os.FileMode) (parent, ledgerDir string) {
		t.Helper()
		parent = filepath.Join(safeTemp(t), "lib")
		ledgerDir = filepath.Join(parent, "ledger")
		if err := os.Mkdir(parent, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(ledgerDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(parent, parentMode); err != nil {
			t.Fatal(err)
		}
		return parent, ledgerDir
	}

	t.Run("0750 accepted", func(t *testing.T) {
		_, dir := build(t, 0o750)
		p := newKP(t, dir, rootOwned)
		if _, err := p.EnsureKey(); err != nil {
			t.Fatalf("a root-owned 0750 parent was refused: %v", err)
		}
		if _, err := p.LoadKey(); err != nil {
			t.Fatal(err)
		}
		if fi, _ := os.Stat(filepath.Join(dir, keyFileName)); fi.Mode().Perm()&^0o600 != 0 {
			t.Fatalf("key file mode %04o", fi.Mode().Perm())
		}
	})
	t.Run("0770 refused and nothing written", func(t *testing.T) {
		_, dir := build(t, 0o770)
		p := newKP(t, dir, rootOwned)
		_, err := p.EnsureKey()
		mustKind(t, err, ErrKeyUnsafe)
		if names := keyNames(t, dir); len(names) != 0 {
			t.Fatalf("files written under a group-writable parent: %v", names)
		}
	})
}
