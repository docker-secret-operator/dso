package crypto

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/internal/securefs/securefstest"
)

// Review finding M1-3: a 0400 key file inside a directory another account can
// write to is replaceable by that account.
func TestKeyFile_UnsafeDirectoryIsRefusedForInstallAndLoad(t *testing.T) {
	root := securefstest.TempDir(t)
	good := filepath.Join(root, "good")
	if _, err := InstallKeyFile(filepath.Join(good, "root.key")); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileKeyProvider(filepath.Join(good, "root.key")); err != nil {
		t.Fatal(err)
	}
	// Loading: the same key becomes unusable once its directory is writable by others.
	if err := os.Chmod(good, 0o777); err != nil {
		t.Fatal(err)
	}
	_, err := NewFileKeyProvider(filepath.Join(good, "root.key"))
	if err == nil || !strings.Contains(err.Error(), "writable by group or others") {
		t.Fatalf("key in a world-writable directory accepted: %v", err)
	}
	// Installing: nothing is written into an unsafe directory.
	bad := filepath.Join(root, "bad")
	os.Mkdir(bad, 0o700)
	os.Chmod(bad, 0o777)
	if _, err := InstallKeyFile(filepath.Join(bad, "root.key")); err == nil {
		t.Fatal("key installed into an unsafe directory")
	}
	if ents, _ := os.ReadDir(bad); len(ents) != 0 {
		t.Fatalf("files left in the unsafe directory: %v", ents)
	}
	if _, err := InstallPassphraseKeyFile(filepath.Join(bad, "p.json"), []byte("a long enough passphrase")); err == nil {
		t.Fatal("passphrase parameters installed into an unsafe directory")
	}
}

func TestKeyFile_SubstitutedKeyInUnsafeDirectoryScenario(t *testing.T) {
	// The scenario from the review: a local user with write access to the
	// directory removes the real key and installs their own. With the directory
	// check, the loader refuses the whole directory instead of trusting
	// whatever file is there.
	root := securefstest.TempDir(t)
	d := filepath.Join(root, "keys")
	p := filepath.Join(d, "root.key")
	if _, err := InstallKeyFile(p); err != nil {
		t.Fatal(err)
	}
	os.Chmod(d, 0o770) // group-writable: the attacker is in the group
	os.Remove(p)
	attackerKey := filepath.Join(root, "attacker.key")
	if _, err := InstallKeyFile(attackerKey); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(attackerKey)
	os.WriteFile(p, data, 0o400)
	if _, err := NewFileKeyProvider(p); err == nil {
		t.Fatal("a key substituted in a group-writable directory was accepted")
	}
}

func TestStaleTempKeyFilesAreReportedNotDeleted(t *testing.T) {
	d := securefstest.TempDir(t)
	stale := filepath.Join(d, staleTempPrefix+"123")
	os.WriteFile(stale, []byte("key material"), 0o400)
	if got := StaleTempKeyFiles(d); len(got) != 1 || got[0] != stale {
		t.Fatalf("got %v", got)
	}
	if _, err := InstallKeyFile(filepath.Join(d, "root.key")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); err != nil {
		t.Fatal("a pre-existing temp key file was deleted")
	}
}
