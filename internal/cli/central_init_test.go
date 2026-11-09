package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/internal/crypto"
)

func keyBytes(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Review finding M1-4: a failure after the key was installed used to leave a
// key and no database, and re-running init was refused with no way forward.
func TestCentralInit_FailureAfterKeyKeepsKeyAndIsRecoverable(t *testing.T) {
	e := newCentralEnv(t)
	centralInitAfterKey = func() error { return errors.New("simulated disk-full while creating the database") }
	_, _, err := e.run(t, "", append([]string{"init"}, e.file()...)...)
	centralInitAfterKey = nil
	if err == nil {
		t.Fatal("expected the simulated failure")
	}
	for _, want := range []string{"NOT deleted or changed", "--use-existing-key", "simulated disk-full"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q:\n%v", want, err)
		}
	}
	original := keyBytes(t, e.key) // the key survived...
	if len(original) == 0 {
		t.Fatal("key missing")
	}
	for _, p := range []string{e.db, e.db + "-wal", e.db + "-shm"} { // ...and no half-made database did
		if _, err := os.Stat(p); err == nil {
			t.Errorf("leftover database file %s", p)
		}
	}

	// A plain retry must NOT overwrite the key, and explains the way forward.
	_, _, err = e.run(t, "", append([]string{"init"}, e.file()...)...)
	if err == nil || !strings.Contains(err.Error(), "--use-existing-key") || !strings.Contains(err.Error(), "will not be overwritten") {
		t.Fatalf("retry without the flag: %v", err)
	}
	if string(keyBytes(t, e.key)) != string(original) {
		t.Fatal("retry modified the key")
	}

	// Recovery: continue with the key that is already there.
	out, _, err := e.run(t, "", append([]string{"init", "--use-existing-key"}, e.file()...)...)
	if err != nil || !strings.Contains(out, "bound to root key id") {
		t.Fatalf("recovery failed: %v %s", err, out)
	}
	if string(keyBytes(t, e.key)) != string(original) {
		t.Fatal("recovery rewrote the key")
	}
	if _, _, err := e.run(t, "", append([]string{"project", "create", "app"}, e.file()...)...); err != nil {
		t.Fatalf("recovered database unusable: %v", err)
	}
	// ...and it is a normal initialised database now.
	if _, _, err := e.run(t, "", append([]string{"init", "--use-existing-key"}, e.file()...)...); err == nil {
		t.Fatal("re-initialised an existing database")
	}
}

func TestCentralInit_PreflightFailuresLeaveNoKeyBehind(t *testing.T) {
	e := newCentralEnv(t)
	// database directory that other accounts can write to
	bad := filepath.Join(e.dir, "baddb")
	os.Mkdir(bad, 0o700)
	os.Chmod(bad, 0o777)
	_, _, err := e.run(t, "", "init", "--db", filepath.Join(bad, "c.db"), "--key-provider", "file", "--key-file", e.key)
	if err == nil || !strings.Contains(err.Error(), "writable by group or others") {
		t.Fatalf("unsafe db directory: %v", err)
	}
	if _, err := os.Stat(e.key); err == nil {
		t.Fatal("a key was installed even though the database location was rejected")
	}
	// key directory that other accounts can write to
	badKeys := filepath.Join(e.dir, "badkeys")
	os.Mkdir(badKeys, 0o700)
	os.Chmod(badKeys, 0o770)
	_, _, err = e.run(t, "", "init", "--db", e.db, "--key-provider", "file", "--key-file", filepath.Join(badKeys, "root.key"))
	if err == nil || !strings.Contains(err.Error(), "writable by group or others") {
		t.Fatalf("unsafe key directory: %v", err)
	}
	if ents, _ := os.ReadDir(badKeys); len(ents) != 0 {
		t.Fatalf("key material written into an unsafe directory: %v", ents)
	}
	if _, err := os.Stat(e.db); err == nil {
		t.Fatal("a database was created although the key location was rejected")
	}
	// existing non-empty database
	e2 := newCentralEnv(t)
	os.MkdirAll(filepath.Dir(e2.db), 0o700)
	os.WriteFile(e2.db, []byte("not empty"), 0o600)
	if _, _, err := e2.run(t, "", append([]string{"init"}, e2.file()...)...); err == nil {
		t.Fatal("initialised over an existing database")
	}
	if _, err := os.Stat(e2.key); err == nil {
		t.Fatal("key installed despite an existing database")
	}
}

func TestCentralInit_UseExistingKeyRequiresAKeyAndNeverOverwrites(t *testing.T) {
	e := newCentralEnv(t)
	if _, _, err := e.run(t, "", append([]string{"init", "--use-existing-key"}, e.file()...)...); err == nil ||
		!strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("flag without a key: %v", err)
	}
	if _, err := crypto.InstallKeyFile(e.key); err != nil {
		t.Fatal(err)
	}
	original := keyBytes(t, e.key)
	if _, _, err := e.run(t, "", append([]string{"init", "--use-existing-key"}, e.file()...)...); err != nil {
		t.Fatal(err)
	}
	if string(keyBytes(t, e.key)) != string(original) {
		t.Fatal("existing key modified")
	}
	// an unusable existing key is reported, not replaced
	e3 := newCentralEnv(t)
	os.MkdirAll(filepath.Dir(e3.key), 0o700)
	os.WriteFile(e3.key, []byte("garbage"), 0o400)
	_, _, err := e3.run(t, "", append([]string{"init", "--use-existing-key"}, e3.file()...)...)
	if err == nil || !strings.Contains(err.Error(), "existing key cannot be used") {
		t.Fatalf("garbage key: %v", err)
	}
	if string(keyBytes(t, e3.key)) != "garbage" {
		t.Fatal("garbage key was overwritten")
	}
	if _, err := os.Stat(e3.db); err == nil {
		t.Fatal("database created from an unusable key")
	}
}

func TestCentralInit_SymlinkedDirectoriesCannotDefeatTheColocationCheck(t *testing.T) {
	e := newCentralEnv(t)
	real := filepath.Join(e.dir, "real")
	os.Mkdir(real, 0o700)
	alias := filepath.Join(e.dir, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Skip("symlinks unavailable")
	}
	_, _, err := e.run(t, "", "init", "--db", filepath.Join(real, "c.db"), "--key-provider", "file", "--key-file", filepath.Join(alias, "root.key"))
	if err == nil || !strings.Contains(err.Error(), "same directory") {
		t.Fatalf("alias bypassed the colocation check: %v", err)
	}
}

func TestCentralInit_WarnsAboutLeftoverTemporaryKeyFiles(t *testing.T) {
	e := newCentralEnv(t)
	os.MkdirAll(filepath.Dir(e.key), 0o700)
	stale := filepath.Join(filepath.Dir(e.key), ".dso-key-12345")
	os.WriteFile(stale, []byte("key material"), 0o400)
	_, errOut, err := e.run(t, "", append([]string{"init"}, e.file()...)...)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut, ".dso-key-12345") || !strings.Contains(errOut, "not removed automatically") {
		t.Errorf("no warning about the leftover:\n%s", errOut)
	}
	if _, err := os.Stat(stale); err != nil {
		t.Error("init deleted a leftover key file")
	}
}

func TestCentralInit_PassphraseRecovery(t *testing.T) {
	e := newCentralEnv(t)
	os.WriteFile(e.pass, []byte("a long enough passphrase\n"), 0o600)
	args := func(extra ...string) []string {
		return append([]string{"--db", e.db, "--key-provider", "passphrase", "--key-file", e.key, "--passphrase-file", e.pass}, extra...)
	}
	centralInitAfterKey = func() error { return errors.New("boom") }
	_, _, err := e.run(t, "", append([]string{"init"}, args()...)...)
	centralInitAfterKey = nil
	if err == nil {
		t.Fatal("expected failure")
	}
	original := keyBytes(t, e.key)
	// wrong passphrase on recovery is rejected without touching anything
	os.WriteFile(e.pass, []byte("the wrong passphrase here\n"), 0o600)
	if _, _, err := e.run(t, "", append([]string{"init", "--use-existing-key"}, args()...)...); err == nil {
		t.Fatal("recovery accepted a wrong passphrase")
	}
	os.WriteFile(e.pass, []byte("a long enough passphrase\n"), 0o600)
	if _, _, err := e.run(t, "", append([]string{"init", "--use-existing-key"}, args()...)...); err != nil {
		t.Fatalf("recovery: %v", err)
	}
	if string(keyBytes(t, e.key)) != string(original) {
		t.Fatal("parameter file modified")
	}
}

// ---- C6: --passphrase-file ----------------------------------------------------

func TestPassphraseFile_ValidationRules(t *testing.T) {
	e := newCentralEnv(t)
	os.WriteFile(e.pass, []byte("a long enough passphrase\n"), 0o600)
	args := []string{"--db", e.db, "--key-provider", "passphrase", "--key-file", e.key, "--passphrase-file", e.pass}
	if _, _, err := e.run(t, "", append([]string{"init"}, args...)...); err != nil {
		t.Fatal(err)
	}
	list := append([]string{"project", "list"}, args...)

	// group/other access
	os.Chmod(e.pass, 0o640)
	if _, _, err := e.run(t, "", list...); err == nil || !strings.Contains(err.Error(), "accessible by group or others") {
		t.Fatalf("0640 passphrase file accepted: %v", err)
	}
	os.Chmod(e.pass, 0o400)
	if _, _, err := e.run(t, "", list...); err != nil {
		t.Fatalf("0400 passphrase file rejected: %v", err)
	}

	// symlink to a perfectly good file
	link := filepath.Join(e.dir, "pass-link")
	if err := os.Symlink(e.pass, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	linkArgs := []string{"--db", e.db, "--key-provider", "passphrase", "--key-file", e.key, "--passphrase-file", link}
	if _, _, err := e.run(t, "", append([]string{"project", "list"}, linkArgs...)...); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("symlinked passphrase file followed: %v", err)
	}

	// directory others can write to: the file could be swapped
	open := filepath.Join(e.dir, "open")
	os.Mkdir(open, 0o700)
	os.Chmod(open, 0o777)
	inOpen := filepath.Join(open, "pass")
	os.WriteFile(inOpen, []byte("a long enough passphrase\n"), 0o600)
	openArgs := []string{"--db", e.db, "--key-provider", "passphrase", "--key-file", e.key, "--passphrase-file", inOpen}
	if _, _, err := e.run(t, "", append([]string{"project", "list"}, openArgs...)...); err == nil || !strings.Contains(err.Error(), "writable by group or others") {
		t.Fatalf("passphrase file in an unsafe directory accepted: %v", err)
	}

	// missing, directory, oversize
	if _, _, err := e.run(t, "", append([]string{"project", "list"}, "--db", e.db, "--key-provider", "passphrase", "--key-file", e.key, "--passphrase-file", filepath.Join(e.dir, "nope"))...); err == nil {
		t.Fatal("missing passphrase file accepted")
	}
	big := filepath.Join(e.dir, "big")
	os.WriteFile(big, []byte(strings.Repeat("x", 5000)), 0o600)
	if _, _, err := e.run(t, "", append([]string{"project", "list"}, "--db", e.db, "--key-provider", "passphrase", "--key-file", e.key, "--passphrase-file", big)...); err == nil {
		t.Fatal("oversize passphrase file accepted")
	}
}

// ---- binding visible through the CLI ------------------------------------------

func TestCentralCLI_AnotherKeyFileCannotOperateOnABoundDatabase(t *testing.T) {
	e := newCentralEnv(t)
	if _, _, err := e.run(t, "", append([]string{"init"}, e.file()...)...); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.run(t, "", append([]string{"project", "create", "app"}, e.file()...)...); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(e.dir, "otherkeys", "root.key")
	if _, err := crypto.InstallKeyFile(other); err != nil {
		t.Fatal(err)
	}
	args := func(sub ...string) []string {
		return append(sub, "--db", e.db, "--key-provider", "file", "--key-file", other)
	}
	for _, sub := range [][]string{{"project", "create", "intruder"}, {"project", "list"}, {"environment", "create", "app", "prod"}} {
		if _, _, err := e.run(t, "", args(sub...)...); err == nil || !strings.Contains(err.Error(), "root key") {
			t.Errorf("%v with another key: %v", sub, err)
		}
	}
}
