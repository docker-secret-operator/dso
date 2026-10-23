package config

// Follow-up hardening of owner-id initialisation: lost updates, multi-document
// YAML, directory-sync failures and owner-id validation.

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func failOnRename(t *testing.T) {
	t.Helper()
	old := osRename
	osRename = func(string, string) error {
		t.Error("the config was replaced although it must not have been")
		return nil
	}
	t.Cleanup(func() { osRename = old })
}

func setHook(t *testing.T, target *func(), f func()) {
	t.Helper()
	old := *target
	*target = f
	t.Cleanup(func() { *target = old })
}

// ---- F1: in-place edits are not overwritten ---------------------------------

func TestEnsureOwnerID_InPlaceEditDuringUpdateIsNotOverwritten(t *testing.T) {
	dir := writeCfg(t, minimalCfg, 0o640)
	t.Chdir(dir)
	before, _ := os.Stat("dso.yaml")
	edited := strings.Replace(minimalCfg, "mode: local", "mode: OPERATOR_EDIT", 1)

	failOnRename(t)
	setHook(t, &beforeReplaceCheck, func() {
		// an editor saving IN PLACE (same inode) after our read, before our replace
		if err := os.WriteFile("dso.yaml", []byte(edited), 0o640); err != nil {
			t.Error(err)
		}
	})

	c := &Config{}
	err := EnsureOwnerID(c, "dso.yaml")
	if err == nil || !strings.Contains(err.Error(), "modified") || !strings.Contains(err.Error(), "retry") {
		t.Fatalf("want a clear modified/retry error, got %v", err)
	}
	if c.Agent.OwnerID != "" {
		t.Errorf("a conflict returned owner id %q as a success", c.Agent.OwnerID)
	}
	if b, _ := os.ReadFile("dso.yaml"); string(b) != edited {
		t.Errorf("the operator's edit was not preserved:\n%s", b)
	}
	if after, _ := os.Stat("dso.yaml"); !os.SameFile(before, after) {
		t.Error("fixture did not exercise the in-place (same inode) case")
	}
	assertNoTempFiles(t, dir)
}

// The check compares content, not timestamps: touching the file (mtime change,
// identical bytes) is not a conflict, and a normal initialisation still works.
func TestEnsureOwnerID_TouchWithoutContentChangeIsNotAConflict(t *testing.T) {
	dir := writeCfg(t, minimalCfg, 0o640)
	t.Chdir(dir)
	setHook(t, &beforeReplaceCheck, func() {
		future := time.Now().Add(time.Hour)
		_ = os.Chtimes("dso.yaml", future, future)
	})
	c := &Config{}
	if err := EnsureOwnerID(c, "dso.yaml"); err != nil || c.Agent.OwnerID == "" {
		t.Fatalf("id=%q err=%v", c.Agent.OwnerID, err)
	}
	if persistedID(t) != c.Agent.OwnerID {
		t.Error("returned id is not the persisted one")
	}
	assertNoTempFiles(t, dir)
}

// ---- F2: multi-document YAML fails closed -----------------------------------

func TestEnsureOwnerID_MultiDocumentConfigIsRefused(t *testing.T) {
	content := minimalCfg + "---\nsecond: document\n# trailing comment\n"
	dir := writeCfg(t, content, 0o640)
	t.Chdir(dir)
	failOnRename(t)

	c := &Config{}
	err := EnsureOwnerID(c, "dso.yaml")
	if err == nil || !strings.Contains(err.Error(), "more than one YAML document") {
		t.Fatalf("want an actionable multi-document error, got %v", err)
	}
	if c.Agent.OwnerID != "" {
		t.Errorf("returned id %q", c.Agent.OwnerID)
	}
	if b, _ := os.ReadFile("dso.yaml"); string(b) != content {
		t.Errorf("the multi-document config was modified:\n%s", b)
	}
	assertNoTempFiles(t, dir)
	// LoadConfig still reads such a file (it only ever used the first document).
	if _, err := LoadConfig("dso.yaml"); err != nil {
		t.Errorf("read-only load of a multi-document config changed behaviour: %v", err)
	}
}

// A multi-document file whose FIRST document already has the id needs no
// rewrite, so nothing is refused and nothing is touched.
func TestEnsureOwnerID_MultiDocumentWithExistingIDNeedsNoRewrite(t *testing.T) {
	content := minimalCfg + "  owner_id: dso-established-id\n---\nsecond: document\n"
	dir := writeCfg(t, content, 0o640)
	t.Chdir(dir)
	failOnRename(t)
	c := &Config{}
	if err := EnsureOwnerID(c, "dso.yaml"); err != nil || c.Agent.OwnerID != "dso-established-id" {
		t.Fatalf("id=%q err=%v", c.Agent.OwnerID, err)
	}
	if b, _ := os.ReadFile("dso.yaml"); string(b) != content {
		t.Error("file was rewritten")
	}
}

// ---- F3: directory-sync failure is reported, not ignored --------------------

func TestEnsureOwnerID_DirectorySyncFailureReturnsNoID(t *testing.T) {
	dir := writeCfg(t, minimalCfg, 0o640)
	t.Chdir(dir)
	oldSync := syncDirFn
	syncDirFn = func(string) error { return errors.New("injected EIO") }
	t.Cleanup(func() { syncDirFn = oldSync })

	c := &Config{}
	err := EnsureOwnerID(c, "dso.yaml")
	if err == nil || !strings.Contains(err.Error(), "durability is uncertain") || !strings.Contains(err.Error(), "not rolled back") {
		t.Fatalf("want an honest durability error, got %v", err)
	}
	if c.Agent.OwnerID != "" {
		t.Errorf("a non-durable id %q was returned as a success", c.Agent.OwnerID)
	}
	// The replacement HAS happened (rename cannot be undone safely): the file now
	// holds an id, and it was not rolled back.
	written := persistedID(t)
	if written == "" {
		t.Fatal("expected the already-replaced file to hold the new id")
	}
	assertNoTempFiles(t, dir)

	// Re-running adopts exactly that id, without rewriting again.
	syncDirFn = oldSync
	failOnRename(t)
	c2 := &Config{}
	if err := EnsureOwnerID(c2, "dso.yaml"); err != nil || c2.Agent.OwnerID != written {
		t.Fatalf("second call: id=%q err=%v, want %q", c2.Agent.OwnerID, err, written)
	}
}

func TestEnsureOwnerID_ExistingIDNeverTouchesDirectorySync(t *testing.T) {
	content := minimalCfg + "  owner_id: dso-established-id\n"
	dir := writeCfg(t, content, 0o640)
	t.Chdir(dir)
	oldSync := syncDirFn
	syncDirFn = func(string) error { t.Error("directory sync attempted although nothing was written"); return nil }
	t.Cleanup(func() { syncDirFn = oldSync })
	c := &Config{}
	if err := EnsureOwnerID(c, "dso.yaml"); err != nil || c.Agent.OwnerID != "dso-established-id" {
		t.Fatalf("id=%q err=%v", c.Agent.OwnerID, err)
	}
}

// ---- F5: owner-id validation -------------------------------------------------

func TestValidateOwnerID(t *testing.T) {
	gen, err := generateOwnerID()
	if err != nil {
		t.Fatal(err)
	}
	for _, ok := range []string{gen, "dso-abc123", "tenant-a/b_c.d", "dso-é", "a b"} {
		if err := validateOwnerID(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for name, bad := range map[string]string{
		"empty": "", "spaces only": "   ", "tab only": "\t",
		"leading space": " dso-x", "trailing space": "dso-x ", "trailing newline": "dso-x\n",
		"embedded newline": "a\nb", "embedded CR": "a\rb", "embedded tab": "a\tb", "embedded NUL": "a\x00b",
		"DEL": "a\x7fb", "invalid UTF-8": "a\xffb",
	} {
		if err := validateOwnerID(bad); err == nil {
			t.Errorf("%s (%q) accepted", name, bad)
		}
	}
}

func TestEnsureOwnerID_InvalidConfiguredIDFailsClosed(t *testing.T) {
	for name, line := range map[string]string{
		"leading space":    `  owner_id: " dso-x"` + "\n",
		"trailing space":   `  owner_id: "dso-x "` + "\n",
		"embedded newline": `  owner_id: "a\nb"` + "\n",
		"embedded tab":     `  owner_id: "a\tb"` + "\n",
		"embedded NUL":     `  owner_id: "a\u0000b"` + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			content := minimalCfg + line
			dir := writeCfg(t, content, 0o640)
			t.Chdir(dir)
			failOnRename(t)

			c := &Config{}
			if err := EnsureOwnerID(c, "dso.yaml"); err == nil || !strings.Contains(err.Error(), "agent.owner_id") {
				t.Fatalf("want an owner_id validation error, got %v", err)
			}
			if c.Agent.OwnerID != "" {
				t.Errorf("returned id %q", c.Agent.OwnerID)
			}
			// The loader that gates label stamping hands back no config at all.
			if cfg, err := LoadConfigWithOwnerID("dso.yaml"); err == nil || cfg != nil {
				t.Errorf("LoadConfigWithOwnerID must fail before any container can be stamped: cfg=%v err=%v", cfg, err)
			}
			if b, _ := os.ReadFile("dso.yaml"); string(b) != content {
				t.Error("an established (if invalid) identity was rewritten")
			}
			assertNoTempFiles(t, dir)
		})
	}
	// The same rule when the invalid id is already in the caller's Config.
	if err := EnsureOwnerID(&Config{Agent: AgentConfig{OwnerID: "bad\nid"}}, "dso.yaml"); err == nil {
		t.Error("an invalid in-memory owner id was accepted")
	}
}

// A blank value is "missing", not an identity: it is replaced by a generated id.
func TestEnsureOwnerID_BlankIDIsTreatedAsMissing(t *testing.T) {
	for _, line := range []string{"  owner_id: \"\"\n", "  owner_id: \"   \"\n", "  owner_id:\n"} {
		dir := writeCfg(t, minimalCfg+line, 0o640)
		t.Chdir(dir)
		c := &Config{}
		if err := EnsureOwnerID(c, "dso.yaml"); err != nil {
			t.Fatalf("%q: %v", line, err)
		}
		if err := validateOwnerID(c.Agent.OwnerID); err != nil || persistedID(t) != c.Agent.OwnerID {
			t.Errorf("%q: id=%q persisted=%q err=%v", line, c.Agent.OwnerID, persistedID(t), err)
		}
		if b, _ := os.ReadFile("dso.yaml"); bytes.Count(b, []byte("owner_id")) != 1 {
			t.Errorf("%q: owner_id key duplicated:\n%s", line, b)
		}
	}
}

func TestGeneratedOwnerIDAlwaysValid(t *testing.T) {
	for i := 0; i < 50; i++ {
		id, err := generateOwnerID()
		if err != nil || validateOwnerID(id) != nil {
			t.Fatalf("generated id %q invalid: %v", id, err)
		}
	}
}

// ---- N1: validation errors are not reported as persistence failures ---------

const persistenceHint = "make the file and its directory writable"

func assertNoPersistenceAdvice(t *testing.T, err error) {
	t.Helper()
	for _, bad := range []string{persistenceHint, "persist generated", "writable"} {
		if strings.Contains(err.Error(), bad) {
			t.Errorf("the error wrongly points at file permissions (%q): %v", bad, err)
		}
	}
}

func TestEnsureOwnerID_InvalidExistingIDIsNotReportedAsAPersistenceFailure(t *testing.T) {
	for name, id := range map[string]string{"padded": `" x"`, "trailing newline": `"x\n"`, "control char": `"a\tb"`} {
		t.Run(name, func(t *testing.T) {
			content := minimalCfg + "  owner_id: " + id + "\n"
			dir := writeCfg(t, content, 0o640)
			t.Chdir(dir)
			failOnRename(t)

			// 1. stale caller (nothing in memory): the invalid id is found in the file.
			err := EnsureOwnerID(&Config{}, "dso.yaml")
			if err == nil || !strings.Contains(err.Error(), "agent.owner_id") {
				t.Fatalf("want the owner_id validation error, got %v", err)
			}
			assertNoPersistenceAdvice(t, err)
			var ce *configContentError
			if !errors.As(err, &ce) {
				t.Errorf("the validation error identity was lost: %T", err)
			}

			// 2. the id is already in the caller's Config.
			bad, _ := loadRaw(t)
			err = EnsureOwnerID(&Config{Agent: AgentConfig{OwnerID: bad}}, "dso.yaml")
			if err == nil || !strings.Contains(err.Error(), "agent.owner_id") {
				t.Fatalf("in-memory path: got %v", err)
			}
			assertNoPersistenceAdvice(t, err)

			if b, _ := os.ReadFile("dso.yaml"); string(b) != content {
				t.Error("the config was modified")
			}
		})
	}
}

// loadRaw returns the owner_id value as a Go string by decoding the fixture.
func loadRaw(t *testing.T) (string, error) {
	t.Helper()
	b, err := os.ReadFile("dso.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var p ownerIDProbe
	if err := yamlUnmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	return p.Agent.OwnerID, nil
}

// Genuine persistence failures keep the permissions advice.
func TestEnsureOwnerID_PersistenceFailureStillCarriesPermissionAdvice(t *testing.T) {
	dir := writeCfg(t, minimalCfg, 0o640)
	t.Chdir(dir)
	if err := os.Mkdir(dir+"/"+ownerIDLockFile, 0o700); err != nil { // lock path unusable
		t.Fatal(err)
	}
	err := EnsureOwnerID(&Config{}, "dso.yaml")
	if err == nil || !strings.Contains(err.Error(), persistenceHint) || !strings.Contains(err.Error(), "agent.owner_id") {
		t.Fatalf("a persistence failure must keep the writable-directory advice, got %v", err)
	}
	var ce *configContentError
	if errors.As(err, &ce) {
		t.Error("a persistence failure was classified as a content error")
	}
}

// ---- N2: an empty file gets a clear error -----------------------------------

func TestEnsureOwnerID_EmptyConfigGetsAClearError(t *testing.T) {
	for name, content := range map[string]string{
		"zero bytes": "", "whitespace only": " \n  \n", "comments only": "# nothing here\n# at all\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := writeCfg(t, content, 0o640)
			t.Chdir(dir)
			failOnRename(t)
			c := &Config{}
			err := EnsureOwnerID(c, "dso.yaml")
			if err == nil || !strings.Contains(err.Error(), "no YAML document") || !strings.Contains(err.Error(), "valid DSO configuration") {
				t.Fatalf("want an actionable empty-file error, got %v", err)
			}
			if strings.Contains(err.Error(), "EOF") {
				t.Errorf("the raw parser error leaked: %v", err)
			}
			assertNoPersistenceAdvice(t, err)
			if c.Agent.OwnerID != "" {
				t.Errorf("returned id %q", c.Agent.OwnerID)
			}
			if b, _ := os.ReadFile("dso.yaml"); string(b) != content {
				t.Error("the file was modified")
			}
			assertNoTempFiles(t, dir)
		})
	}
}

// Unchanged behaviour next to the empty-file case: a trailing separator counts
// as a second document (a rewrite is refused), a malformed later document is a
// YAML error, and neither modifies the file.
func TestEnsureOwnerID_SeparatorAndMalformedDocumentBehaviourUnchanged(t *testing.T) {
	for name, tc := range map[string]struct{ content, want string }{
		"trailing separator":           {minimalCfg + "---\n", "more than one YAML document"},
		"trailing separator + comment": {minimalCfg + "---\n# note\n", "more than one YAML document"},
		"malformed second document":    {minimalCfg + "---\n: [bad\n", "not valid YAML"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := writeCfg(t, tc.content, 0o640)
			t.Chdir(dir)
			failOnRename(t)
			err := EnsureOwnerID(&Config{}, "dso.yaml")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
			assertNoPersistenceAdvice(t, err)
			if b, _ := os.ReadFile("dso.yaml"); string(b) != tc.content {
				t.Error("the file was modified")
			}
			assertNoTempFiles(t, dir)
		})
	}
}
