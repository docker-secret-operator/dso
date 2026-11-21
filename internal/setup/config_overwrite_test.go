package setup

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

// `dso setup` re-run on a configured host used to plan a "modify" of dso.yaml whose content
// is a 4-line stub and apply it with no confirmation, backup or merge: provider config,
// secret mappings and agent.owner_id (whose loss orphans every container the agent created)
// were destroyed. The existing-installation finding is only SeverityInfo and the CLI has no
// confirmation gate, so nothing stopped it. Replacing an existing configuration now needs
// the explicit ReplaceExistingConfig opt-in, and takes a backup first.

func existingInstall(configPath string) *ValidationResult {
	return &ValidationResult{Issues: []ValidationIssue{{
		Severity: SeverityInfo, Category: CategoryConfiguration, Code: CodeExistingInstallationFound,
		Message: fmt.Sprintf("DSO configuration found at %s — setup will upgrade", configPath),
	}}}
}

func TestPlanner_ExistingConfig_IsNotReplacedWithoutTheExplicitOptIn(t *testing.T) {
	p := newPlanner()
	env := &Environment{User: UserInfo{HomeDir: "/home/alice"}}
	configPath := "/home/alice/.dso/dso.yaml"

	plan, err := p.Plan(context.Background(), env, existingInstall(configPath), SetupOptions{Mode: ModeLocal, Provider: "local"})
	if err == nil {
		t.Fatalf("an existing configuration must not be planned for replacement without the opt-in; plan files: %+v", plan.Files)
	}
	if !strings.Contains(err.Error(), configPath) || !strings.Contains(err.Error(), "--replace-config") {
		t.Fatalf("the refusal must name the file and the opt-in, got: %v", err)
	}
}

func TestPlanner_ExistingConfig_OptInPlansAModifyWithABackup(t *testing.T) {
	p := newPlanner()
	env := &Environment{User: UserInfo{HomeDir: "/home/alice"}}
	configPath := "/home/alice/.dso/dso.yaml"

	plan, err := p.Plan(context.Background(), env, existingInstall(configPath),
		SetupOptions{Mode: ModeLocal, Provider: "local", ReplaceExistingConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Files[0].Operation != "modify" || !plan.Files[0].BackupExisting {
		t.Fatalf("the opt-in must plan a modify that backs the existing file up, got %+v", plan.Files[0])
	}
}

func TestPlanner_FirstTimeSetup_IsUnchanged(t *testing.T) {
	p := newPlanner()
	plan, err := p.Plan(context.Background(), &Environment{User: UserInfo{HomeDir: "/home/alice"}}, nil, SetupOptions{Mode: ModeLocal, Provider: "local"})
	if err != nil {
		t.Fatalf("a first-time setup must not be refused: %v", err)
	}
	if plan.Files[0].Operation != "create" || plan.Files[0].BackupExisting {
		t.Fatalf("a first-time setup creates the file with no backup, got %+v", plan.Files[0])
	}
}

// The executor side: a planned replacement writes the backup FIRST, with the previous bytes
// and mode 0600, and if the backup cannot be written the file is NOT replaced.

type memFS struct {
	files  map[string][]byte
	modes  map[string]os.FileMode
	failOn string // a write to a path containing this substring fails
	order  []string
}

func newMemFS(existing map[string]string) *memFS {
	m := &memFS{files: map[string][]byte{}, modes: map[string]os.FileMode{}}
	for p, c := range existing {
		m.files[p] = []byte(c)
	}
	return m
}

func (m *memFS) install(e *FileExecutor) {
	e.readFile = func(p string) ([]byte, error) {
		if b, ok := m.files[p]; ok {
			return b, nil
		}
		return nil, os.ErrNotExist
	}
	e.stat = func(p string) (os.FileInfo, error) {
		if _, ok := m.files[p]; ok {
			return os.Stat(".")
		}
		return nil, os.ErrNotExist
	}
	e.writeFile = func(p string, b []byte, mode os.FileMode) error {
		if m.failOn != "" && strings.Contains(p, m.failOn) {
			return fmt.Errorf("injected write failure for %s", p)
		}
		m.files[p] = append([]byte(nil), b...)
		m.modes[p] = mode
		m.order = append(m.order, p)
		return nil
	}
	e.chmod = func(p string, mode os.FileMode) error { m.modes[p] = mode; return nil }
	e.chown = noopChown
	e.ownerOf = func(string) (string, error) { return "", nil }
}

func TestFileExecutor_BackupExisting_WritesTheBackupFirst(t *testing.T) {
	const path = "/etc/dso/dso.yaml"
	fs := newMemFS(map[string]string{path: precious})
	e := newFileExecutor([]FileChange{{ID: "FILE-001", Path: path, Content: []byte("stub"), Mode: 0o600, Operation: "modify", BackupExisting: true}}, &Emitter{})
	fs.install(e)

	if err := e.execute(context.Background(), newTransaction("p")); err != nil {
		t.Fatal(err)
	}
	if len(fs.order) != 2 || fs.order[1] != path || !strings.HasPrefix(fs.order[0], path+".bak-") {
		t.Fatalf("the backup must be written before the replacement, got write order %v", fs.order)
	}
	if string(fs.files[fs.order[0]]) != precious || fs.modes[fs.order[0]] != 0o600 {
		t.Fatalf("the backup must hold the previous bytes with mode 0600 (mode %v)", fs.modes[fs.order[0]])
	}
	if string(fs.files[path]) != "stub" {
		t.Fatalf("the file was not replaced after a successful backup: %q", fs.files[path])
	}
}

func TestFileExecutor_BackupFailure_BlocksTheReplacement(t *testing.T) {
	const path = "/etc/dso/dso.yaml"
	fs := newMemFS(map[string]string{path: precious})
	fs.failOn = ".bak-"
	e := newFileExecutor([]FileChange{{ID: "FILE-001", Path: path, Content: []byte("stub"), Mode: 0o600, Operation: "modify", BackupExisting: true}}, &Emitter{})
	fs.install(e)

	if err := e.execute(context.Background(), newTransaction("p")); err == nil {
		t.Fatal("a failed backup must fail the operation")
	}
	if string(fs.files[path]) != precious {
		t.Fatalf("the existing configuration was replaced although its backup failed: %q", fs.files[path])
	}
}

func TestFileExecutor_NoBackupRequested_NoBackupWritten(t *testing.T) {
	fs := newMemFS(nil)
	e := newFileExecutor([]FileChange{{ID: "FILE-001", Path: "/etc/dso/dso.yaml", Content: []byte("v1"), Mode: 0o600, Operation: "create"}}, &Emitter{})
	fs.install(e)
	if err := e.execute(context.Background(), newTransaction("p")); err != nil {
		t.Fatal(err)
	}
	if len(fs.order) != 1 {
		t.Fatalf("a create writes only the file, got %v", fs.order)
	}
}

const precious = "agent:\n  owner_id: dso-owner-keep-me\nproviders:\n  prod-vault:\n    type: vault\n"
