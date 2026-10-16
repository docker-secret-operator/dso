package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/internal/securefs/securefstest"

	"github.com/spf13/cobra"
)

type centralEnv struct {
	dir, db, key, pass string
}

func newCentralEnv(t *testing.T) *centralEnv {
	t.Helper()
	d := securefstest.TempDir(t)
	return &centralEnv{dir: d, db: filepath.Join(d, "data", "c.db"), key: filepath.Join(d, "keys", "root.key"), pass: filepath.Join(d, "pass.txt")}
}

func (e *centralEnv) run(t *testing.T, stdin string, args ...string) (string, string, error) {
	t.Helper()
	cmd := NewCentralCmd()
	var out, errb bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errb)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return out.String(), errb.String(), err
}

func (e *centralEnv) file(extra ...string) []string {
	return append([]string{"--db", e.db, "--key-provider", "file", "--key-file", e.key}, extra...)
}

func TestCentralCLIEndToEnd(t *testing.T) {
	e := newCentralEnv(t)
	const secret = "CLI-PLAINTEXT-value-42"
	if out, _, err := e.run(t, "", append([]string{"init"}, e.file()...)...); err != nil || !strings.Contains(out, "Initialised") {
		t.Fatalf("init: %v %s", err, out)
	}
	steps := [][]string{
		{"project", "create", "app"},
		{"environment", "create", "app", "prod"},
	}
	for _, s := range steps {
		if _, _, err := e.run(t, "", append(s, e.file()...)...); err != nil {
			t.Fatalf("%v: %v", s, err)
		}
	}
	if _, _, err := e.run(t, secret+"\n", append([]string{"secret", "create", "app", "prod", "db/pass", "--value-stdin"}, e.file()...)...); err != nil {
		t.Fatal(err)
	}
	out, _, err := e.run(t, "v2\n", append([]string{"secret", "add-version", "app", "prod", "db/pass", "--value-stdin", "--comment", "rotate"}, e.file()...)...)
	if err != nil || !strings.Contains(out, "version 2") {
		t.Fatalf("%v %s", err, out)
	}

	// metadata commands never print values
	for _, a := range [][]string{{"secret", "list", "app", "prod"}, {"secret", "versions", "app", "prod", "db/pass"}, {"project", "list"}, {"environment", "list", "app"}} {
		out, errOut, err := e.run(t, "", append(a, e.file()...)...)
		if err != nil {
			t.Fatalf("%v: %v", a, err)
		}
		if strings.Contains(out+errOut, "PLAINTEXT") || strings.Contains(out+errOut, "v2") && a[1] == "list" {
			t.Fatalf("%v leaked a value: %s", a, out)
		}
	}
	// get requires --reveal
	if out, _, err := e.run(t, "", append([]string{"secret", "get", "app", "prod", "db/pass"}, e.file()...)...); err == nil || strings.Contains(out, "v2") {
		t.Fatalf("get without --reveal: %v %q", err, out)
	}
	out, _, err = e.run(t, "", append([]string{"secret", "get", "app", "prod", "db/pass", "--reveal", "--version", "1"}, e.file()...)...)
	if err != nil || out != secret {
		t.Fatalf("v1 = %q, %v", out, err)
	}
	out, _, _ = e.run(t, "", append([]string{"secret", "get", "app", "prod", "db/pass", "--reveal"}, e.file()...)...)
	if out != "v2" {
		t.Fatalf("latest = %q", out)
	}
	if _, _, err := e.run(t, "", append([]string{"secret", "disable-version", "app", "prod", "db/pass", "2"}, e.file()...)...); err != nil {
		t.Fatal(err)
	}
	out, _, err = e.run(t, "", append([]string{"secret", "get", "app", "prod", "db/pass", "--reveal"}, e.file()...)...)
	if err == nil || out != "" || !strings.Contains(err.Error(), "latest version is disabled") {
		t.Fatalf("latest after disabling the newest version must fail, got out=%q err=%v", out, err)
	}
	out, _, _ = e.run(t, "", append([]string{"secret", "get", "app", "prod", "db/pass", "--reveal", "--version", "1"}, e.file()...)...)
	if out != secret {
		t.Fatalf("explicit v1 after disable = %q", out)
	}
	if _, _, err := e.run(t, "", append([]string{"secret", "delete", "app", "prod", "db/pass"}, e.file()...)...); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.run(t, "", append([]string{"secret", "get", "app", "prod", "db/pass", "--reveal"}, e.file()...)...); err == nil {
		t.Fatal("deleted secret readable")
	}
}

func TestCentralCLIHasNoValueFlag(t *testing.T) {
	// values must never be accepted via argv (shell history / ps)
	cmd := NewCentralCmd()
	var check func(c *cobra.Command)
	check = func(c *cobra.Command) {
		for _, name := range []string{"value", "secret-value", "password", "passphrase"} {
			if c.Flags().Lookup(name) != nil || c.PersistentFlags().Lookup(name) != nil {
				t.Fatalf("%s has a --%s flag", c.CommandPath(), name)
			}
		}
		for _, sub := range c.Commands() {
			check(sub)
		}
	}
	check(cmd)
}

func TestCentralCLIInitSafetyChecks(t *testing.T) {
	e := newCentralEnv(t)
	if _, _, err := e.run(t, "", "init", "--db", e.db, "--key-file", e.key); err == nil || !strings.Contains(err.Error(), "key-provider") {
		t.Fatalf("no explicit provider accepted: %v", err)
	}
	// key colocated with the database
	colo := []string{"init", "--db", filepath.Join(e.dir, "x.db"), "--key-provider", "file", "--key-file", filepath.Join(e.dir, "x.key")}
	if _, _, err := e.run(t, "", colo...); err == nil || !strings.Contains(err.Error(), "same directory") {
		t.Fatalf("colocated key accepted: %v", err)
	}
	if _, _, err := e.run(t, "", append(colo, "--allow-colocated-key")...); err != nil {
		t.Fatal(err)
	}
	// never re-initialise over an existing database
	if _, _, err := e.run(t, "", append(colo, "--allow-colocated-key")...); err == nil {
		t.Fatal("re-init succeeded")
	}
}

func TestCentralCLIFailsClosedOnBadKeyMaterial(t *testing.T) {
	e := newCentralEnv(t)
	e.run(t, "", append([]string{"init"}, e.file()...)...)
	// loosen the key's permissions: every operation must refuse before touching secrets
	if err := os.Chmod(e.key, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.run(t, "", append([]string{"project", "create", "app"}, e.file()...)...); err == nil || !strings.Contains(err.Error(), "chmod") {
		t.Fatalf("loose key accepted: %v", err)
	}
	os.Chmod(e.key, 0o400)
	os.Remove(e.key)
	if _, _, err := e.run(t, "", append([]string{"project", "list"}, e.file()...)...); err == nil {
		t.Fatal("missing key accepted")
	}
}

func TestCentralCLIPassphraseProvider(t *testing.T) {
	e := newCentralEnv(t)
	os.WriteFile(e.pass, []byte("a long enough passphrase\n"), 0o600)
	args := func(extra ...string) []string {
		return append([]string{"--db", e.db, "--key-provider", "passphrase", "--key-file", e.key, "--passphrase-file", e.pass}, extra...)
	}
	if _, _, err := e.run(t, "", append([]string{"init"}, args()...)...); err != nil {
		t.Fatal(err)
	}
	e.run(t, "", append([]string{"project", "create", "app"}, args()...)...)
	e.run(t, "", append([]string{"environment", "create", "app", "prod"}, args()...)...)
	if _, _, err := e.run(t, "pv", append([]string{"secret", "create", "app", "prod", "K", "--value-stdin"}, args()...)...); err != nil {
		t.Fatal(err)
	}
	out, _, err := e.run(t, "", append([]string{"secret", "get", "app", "prod", "K", "--reveal"}, args()...)...)
	if err != nil || out != "pv" {
		t.Fatalf("%q %v", out, err)
	}
	// wrong passphrase
	os.WriteFile(e.pass, []byte("not the right passphrase\n"), 0o600)
	if _, _, err := e.run(t, "", append([]string{"secret", "get", "app", "prod", "K", "--reveal"}, args()...)...); err == nil {
		t.Fatal("wrong passphrase accepted")
	}
	// passphrase file with loose perms is refused
	os.Chmod(e.pass, 0o644)
	if _, _, err := e.run(t, "", append([]string{"project", "list"}, args()...)...); err == nil || !strings.Contains(err.Error(), "accessible by group") {
		t.Fatalf("loose passphrase file accepted: %v", err)
	}
	// no terminal and no file: error, not a hang
	if _, _, err := e.run(t, "", "project", "list", "--db", e.db, "--key-provider", "passphrase", "--key-file", e.key); err == nil {
		t.Fatal("expected error without a passphrase source")
	}
}

func TestCentralCommandIsRegisteredOnRoot(t *testing.T) {
	root := NewRootCmd()
	c, _, err := root.Find([]string{"central"})
	if err != nil || c == nil || c.Name() != "central" {
		t.Fatalf("central not registered: %v", err)
	}
}
