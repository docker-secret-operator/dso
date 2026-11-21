package cli

// A2: every command routed through centralFlags.open requires an existing
// database. A mistyped --db must never create (and bind) a new Central Store.
// `init` and `restore` are the deliberate exceptions; `backup` has its own
// (B-2) check and `audit verify` never opens the live database.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// openCmds are the commands that go through centralFlags.open, each with
// arguments that get past flag validation so that the database check is what
// decides the outcome.
var openCmds = []struct {
	name  string
	args  []string
	stdin string
}{
	{"project list", []string{"project", "list"}, ""},
	{"project create", []string{"project", "create", "p"}, ""},
	{"environment list", []string{"environment", "list", "p"}, ""},
	{"environment create", []string{"environment", "create", "p", "e"}, ""},
	{"secret get", []string{"secret", "get", "p", "e", "n", "--reveal"}, ""},
	{"secret list", []string{"secret", "list", "p", "e"}, ""},
	{"secret versions", []string{"secret", "versions", "p", "e", "n"}, ""},
	{"secret create", []string{"secret", "create", "p", "e", "n", "--value-stdin"}, "v\n"},
	{"secret add-version", []string{"secret", "add-version", "p", "e", "n", "--value-stdin"}, "v\n"},
	{"secret disable-version", []string{"secret", "disable-version", "p", "e", "n", "1"}, ""},
	{"secret delete", []string{"secret", "delete", "p", "e", "n"}, ""},
}

// keyedEnv returns an environment with a valid root key on disk (created by a
// real `init`) whose database has then been removed, so the only thing that
// can refuse a command is the missing database. If removeParent is set the
// database's parent directory is removed too.
func keyedEnv(t *testing.T, removeParent bool) *centralEnv {
	t.Helper()
	e := newCentralEnv(t)
	if _, _, err := e.run(t, "", append([]string{"init"}, e.file()...)...); err != nil {
		t.Fatal(err)
	}
	if removeParent {
		if err := os.RemoveAll(filepath.Dir(e.db)); err != nil {
			t.Fatal(err)
		}
		return e
	}
	for _, sfx := range []string{"", "-wal", "-shm", ".central.lock"} {
		if err := os.Remove(e.db + sfx); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	return e
}

func assertUnchanged(t *testing.T, e *centralEnv, before []string, what string) {
	t.Helper()
	if after := treeEntries(t, e.dir); !sameEntries(before, after) {
		t.Fatalf("%s changed the filesystem:\nbefore: %v\nafter:  %v", what, before, after)
	}
}

func TestCentralOpenCmds_MissingDatabase_RefusedAndCreateNothing(t *testing.T) {
	for _, parentGone := range []bool{false, true} {
		variant := "parent exists"
		if parentGone {
			variant = "parent missing"
		}
		for _, c := range openCmds {
			t.Run(variant+"/"+c.name, func(t *testing.T) {
				e := keyedEnv(t, parentGone)
				before := treeEntries(t, e.dir)
				stdout, _, err := e.run(t, c.stdin, append(append([]string{}, c.args...), e.file()...)...)
				if err == nil {
					t.Fatalf("expected a refusal for a missing database, got success; stdout=%q", stdout)
				}
				if !strings.Contains(err.Error(), "does not exist") || !strings.Contains(err.Error(), "run dso central init first") {
					t.Fatalf("expected the missing-database error, got: %v", err)
				}
				if stdout != "" {
					t.Fatalf("a refused command must print nothing, got %q", stdout)
				}
				assertUnchanged(t, e, before, c.name)
				for _, p := range []string{e.db, e.db + ".central.lock", e.db + "-wal", e.db + "-shm"} {
					if _, statErr := os.Lstat(p); !os.IsNotExist(statErr) {
						t.Fatalf("%s must not exist after a refused command (%v)", p, statErr)
					}
				}
				if parentGone {
					if _, statErr := os.Lstat(filepath.Dir(e.db)); !os.IsNotExist(statErr) {
						t.Fatalf("the parent directory must not be created (%v)", statErr)
					}
				}
			})
		}
	}
}

// Before A2, `project list` against a missing database created an empty,
// bound database and exited 0 printing an empty listing.
func TestCentralProjectList_MissingDatabase_NoLongerSucceedsOrCreates(t *testing.T) {
	e := keyedEnv(t, false)
	before := treeEntries(t, e.dir)
	stdout, _, err := e.run(t, "", append([]string{"project", "list"}, e.file()...)...)
	if err == nil {
		t.Fatal("project list on a missing database must exit non-zero")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("expected the missing-database error, got: %v", err)
	}
	if strings.Contains(stdout, "SLUG") || strings.Contains(stdout, "NAME") || stdout != "" {
		t.Fatalf("no (empty) successful listing may be printed, got %q", stdout)
	}
	assertUnchanged(t, e, before, "project list")
}

// The database check runs before key-provider construction: with the
// passphrase provider and no passphrase file, building the provider would
// fail with a "no terminal" prompt error, so seeing the missing-database error
// proves the order.
func TestCentralOpenCmds_DatabaseCheckPrecedesKeyProvider(t *testing.T) {
	e := newCentralEnv(t)
	before := treeEntries(t, e.dir)
	_, _, err := e.run(t, "", "project", "list", "--db", e.db, "--key-provider", "passphrase", "--key-file", e.key)
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("expected the missing-database error before any key/passphrase handling, got: %v", err)
	}
	assertUnchanged(t, e, before, "project list (passphrase provider)")
}

func TestCentralOpenCmds_NonRegularDatabase_Rejected(t *testing.T) {
	kinds := []struct {
		name  string
		setup func(t *testing.T, e *centralEnv, real string) (db string)
	}{
		{"directory", func(t *testing.T, e *centralEnv, _ string) string {
			d := filepath.Join(e.dir, "dbdir")
			if err := os.Mkdir(d, 0o700); err != nil {
				t.Fatal(err)
			}
			return d
		}},
		{"symlink to a valid database", func(t *testing.T, e *centralEnv, real string) string {
			l := filepath.Join(e.dir, "link.db")
			if err := os.Symlink(real, l); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			return l
		}},
		{"dangling symlink", func(t *testing.T, e *centralEnv, _ string) string {
			l := filepath.Join(e.dir, "dangling.db")
			if err := os.Symlink(filepath.Join(e.dir, "nothing.db"), l); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			return l
		}},
	}
	for _, k := range kinds {
		for _, c := range openCmds {
			t.Run(k.name+"/"+c.name, func(t *testing.T) {
				e := newCentralEnv(t)
				if _, _, err := e.run(t, "", append([]string{"init"}, e.file()...)...); err != nil {
					t.Fatal(err)
				}
				db := k.setup(t, e, e.db)
				targetBefore, readErr := os.ReadFile(e.db)
				if readErr != nil {
					t.Fatal(readErr)
				}
				before := treeEntries(t, e.dir)
				args := append(append([]string{}, c.args...), "--db", db, "--key-provider", "file", "--key-file", e.key)
				stdout, _, err := e.run(t, c.stdin, args...)
				if err == nil || !strings.Contains(err.Error(), "not a regular file") ||
					!strings.Contains(err.Error(), "symlinks and directories are not accepted") {
					t.Fatalf("expected a not-a-regular-file refusal, got err=%v stdout=%q", err, stdout)
				}
				assertUnchanged(t, e, before, c.name)
				if after, _ := os.ReadFile(e.db); string(after) != string(targetBefore) {
					t.Fatal("the symlink target database was modified")
				}
			})
		}
	}
}

// ---- exceptions and compatibility ------------------------------------------

func TestCentralInit_StillCreatesMissingDatabase(t *testing.T) {
	e := newCentralEnv(t)
	if _, statErr := os.Lstat(e.db); !os.IsNotExist(statErr) {
		t.Fatal("precondition: database must not exist")
	}
	out, _, err := e.run(t, "", append([]string{"init"}, e.file()...)...)
	if err != nil || !strings.Contains(out, "Initialised") {
		t.Fatalf("init must create a missing database: %v %q", err, out)
	}
	if fi, statErr := os.Lstat(e.db); statErr != nil || !fi.Mode().IsRegular() {
		t.Fatalf("init did not create the database: %v", statErr)
	}
}

func TestCentralOpenCmds_ValidDatabase_AllCategoriesStillWork(t *testing.T) {
	e := newCentralEnv(t)
	steps := []struct {
		args  []string
		stdin string
	}{
		{[]string{"init"}, ""},
		{[]string{"project", "create", "p"}, ""},
		{[]string{"environment", "create", "p", "e"}, ""},
		{[]string{"secret", "create", "p", "e", "n", "--value-stdin"}, "v1\n"},
		{[]string{"secret", "add-version", "p", "e", "n", "--value-stdin"}, "v2\n"},
		{[]string{"project", "list"}, ""},
		{[]string{"environment", "list", "p"}, ""},
		{[]string{"secret", "list", "p", "e"}, ""},
		{[]string{"secret", "versions", "p", "e", "n"}, ""},
		{[]string{"secret", "get", "p", "e", "n", "--reveal"}, ""},
		{[]string{"secret", "disable-version", "p", "e", "n", "1"}, ""},
		{[]string{"secret", "delete", "p", "e", "n"}, ""},
	}
	for _, s := range steps {
		if _, _, err := e.run(t, s.stdin, append(append([]string{}, s.args...), e.file()...)...); err != nil {
			t.Fatalf("%v failed on a valid database: %v", s.args, err)
		}
	}
}

func TestCentralRestore_StillRestoresIntoMissingDatabase(t *testing.T) {
	e, cand := restoreFixture(t)
	for _, sfx := range []string{"", "-wal", "-shm", ".central.lock"} {
		if err := os.Remove(e.db + sfx); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	stdout, _, err := e.run(t, "", append([]string{"restore", "--from", cand, "--force"}, e.file()...)...)
	if err != nil || !strings.Contains(stdout, "Restore complete") {
		t.Fatalf("restore into a missing database must still work: %v %q", err, stdout)
	}
	list, _, err := e.run(t, "", append([]string{"project", "list"}, e.file()...)...)
	if err != nil || !strings.Contains(list, "app") {
		t.Fatalf("restored database is not usable: %v %q", err, list)
	}
}

func TestCentralAuditVerify_MissingDatabase_StaysNonCreatingExit11(t *testing.T) {
	e := keyedEnv(t, false)
	before := treeEntries(t, e.dir)
	_, _, err := e.run(t, "", "audit", "verify", "--db", e.db)
	var ec *exitCodeError
	if !errors.As(err, &ec) || ec.code != exitAuditUnavailable {
		t.Fatalf("expected exit %d (verification unavailable), got %v", exitAuditUnavailable, err)
	}
	assertUnchanged(t, e, before, "audit verify")
}
