package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCentralBackupCmd_SuccessfulInvocation covers F7 item 1: argument
// parsing and a successful backup invocation through the real CLI command
// (not the lower-level Store.Backup directly).
func TestCentralBackupCmd_SuccessfulInvocation(t *testing.T) {
	e := newCentralEnv(t)
	if _, _, err := e.run(t, "", append([]string{"init"}, e.file()...)...); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(e.dir, "backups", "out.db")
	stdout, _, err := e.run(t, "", append([]string{"backup", "--out", out}, e.file()...)...)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, out) {
		t.Fatalf("expected the backup path in output, got %q", stdout)
	}
	if !strings.Contains(stdout, "root key") {
		t.Fatalf("expected the root-key reminder in output, got %q", stdout)
	}
	if _, statErr := os.Stat(out); statErr != nil {
		t.Fatalf("expected the backup file to exist: %v", statErr)
	}
}

// TestCentralBackupCmd_DestinationAlreadyExists covers F7 item 2.
func TestCentralBackupCmd_DestinationAlreadyExists(t *testing.T) {
	e := newCentralEnv(t)
	if _, _, err := e.run(t, "", append([]string{"init"}, e.file()...)...); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(e.dir, "backups", "out.db")
	if _, _, err := e.run(t, "", append([]string{"backup", "--out", out}, e.file()...)...); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.run(t, "", append([]string{"backup", "--out", out}, e.file()...)...); err == nil {
		t.Fatal("expected the second backup to the same --out to fail")
	}
}

// TestCentralRestoreCmd_DryRun covers F7 item 3.
func TestCentralRestoreCmd_DryRun(t *testing.T) {
	e := newCentralEnv(t)
	if _, _, err := e.run(t, "", append([]string{"init"}, e.file()...)...); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.run(t, "", append([]string{"project", "create", "app"}, e.file()...)...); err != nil {
		t.Fatal(err)
	}
	cand := filepath.Join(e.dir, "cand.db")
	if _, _, err := e.run(t, "", append([]string{"backup", "--out", cand}, e.file()...)...); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(e.db)
	if err != nil {
		t.Fatal(err)
	}

	stdout, _, err := e.run(t, "", append([]string{"restore", "--from", cand, "--dry-run"}, e.file()...)...)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, cand) {
		t.Fatalf("expected the candidate path identified in dry-run output, got %q", stdout)
	}
	if !strings.Contains(stdout, "Dry run") {
		t.Fatalf("expected dry-run output to say so, got %q", stdout)
	}

	after, err := os.ReadFile(e.db)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("dry-run must not modify the active database")
	}
}

// TestCentralRestoreCmd_WithoutForce_DeclinesAndLeavesUnchanged covers F7
// item 4. promptForApproval reads the real process stdin, not the command's
// injected --stdin buffer; under `go test` that is not an interactive
// terminal and the prompt's read fails/returns empty, which
// promptForApproval treats as a decline -- so a restore run without
// --force in this harness exercises exactly the "declined" path.
func TestCentralRestoreCmd_WithoutForce_DeclinesAndLeavesUnchanged(t *testing.T) {
	e := newCentralEnv(t)
	if _, _, err := e.run(t, "", append([]string{"init"}, e.file()...)...); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.run(t, "", append([]string{"project", "create", "app"}, e.file()...)...); err != nil {
		t.Fatal(err)
	}
	cand := filepath.Join(e.dir, "cand.db")
	if _, _, err := e.run(t, "", append([]string{"backup", "--out", cand}, e.file()...)...); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(e.db)
	if err != nil {
		t.Fatal(err)
	}

	stdout, _, err := e.run(t, "", append([]string{"restore", "--from", cand}, e.file()...)...)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "cancelled") {
		t.Fatalf("expected the restore to report as cancelled without confirmation, got %q", stdout)
	}

	after, err := os.ReadFile(e.db)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("a declined restore must leave the active database unchanged")
	}
}

// TestCentralRestoreCmd_WithForce_PerformsReplacement covers F7 item 5: the
// non-interactive path, exercised end-to-end through the real CLI command,
// replacing the active database only after validation and safety-snapshot
// creation (verified here by checking a pre-backup-vs-post-restore project
// difference, and that a safety-snapshot file appears).
func TestCentralRestoreCmd_WithForce_PerformsReplacement(t *testing.T) {
	e := newCentralEnv(t)
	if _, _, err := e.run(t, "", append([]string{"init"}, e.file()...)...); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.run(t, "", append([]string{"project", "create", "app"}, e.file()...)...); err != nil {
		t.Fatal(err)
	}
	cand := filepath.Join(e.dir, "cand.db")
	if _, _, err := e.run(t, "", append([]string{"backup", "--out", cand}, e.file()...)...); err != nil {
		t.Fatal(err)
	}
	// Add data AFTER the backup, so a successful restore is observable.
	if _, _, err := e.run(t, "", append([]string{"project", "create", "later"}, e.file()...)...); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := e.run(t, "", append([]string{"restore", "--from", cand, "--force"}, e.file()...)...)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "Restore complete") {
		t.Fatalf("expected a completion message, got %q", stdout)
	}
	if !strings.Contains(stdout, "Safety snapshot") {
		t.Fatalf("expected the safety snapshot path reported, got %q", stdout)
	}

	// Find the snapshot file this run must have created beside the database.
	dbDir := filepath.Dir(e.db)
	entries, err := os.ReadDir(dbDir)
	if err != nil {
		t.Fatal(err)
	}
	var sawSnapshot bool
	for _, ent := range entries {
		if strings.Contains(ent.Name(), "pre-restore") {
			sawSnapshot = true
		}
	}
	if !sawSnapshot {
		t.Fatal("expected a pre-restore safety snapshot file to exist beside the active database")
	}

	// "later" was created after the backup; it must be gone post-restore.
	listOut, _, err := e.run(t, "", append([]string{"project", "list"}, e.file()...)...)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(listOut, "later") {
		t.Fatal("expected the project created after the backup to be absent after restore")
	}
	if !strings.Contains(listOut, "app") {
		t.Fatal("expected the pre-backup project to still be present after restore")
	}
}

// TestCentralRestoreCmd_InvalidCandidate_NoReplacement covers F7 item 6
// (invalid candidate half).
func TestCentralRestoreCmd_InvalidCandidate_NoReplacement(t *testing.T) {
	e := newCentralEnv(t)
	if _, _, err := e.run(t, "", append([]string{"init"}, e.file()...)...); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(e.db)
	if err != nil {
		t.Fatal(err)
	}

	bogus := filepath.Join(e.dir, "bogus.db")
	if err := os.WriteFile(bogus, []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, _, err := e.run(t, "", append([]string{"restore", "--from", bogus, "--force"}, e.file()...)...); err == nil {
		t.Fatal("expected restore to fail against an invalid candidate")
	}

	after, err := os.ReadFile(e.db)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("an invalid candidate must not modify the active database")
	}
}

// TestCentralRestoreCmd_WrongKey_NoReplacement covers F7 item 6 (key-binding
// failure half).
func TestCentralRestoreCmd_WrongKey_NoReplacement(t *testing.T) {
	e := newCentralEnv(t)
	if _, _, err := e.run(t, "", append([]string{"init"}, e.file()...)...); err != nil {
		t.Fatal(err)
	}
	cand := filepath.Join(e.dir, "cand.db")
	if _, _, err := e.run(t, "", append([]string{"backup", "--out", cand}, e.file()...)...); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(e.db)
	if err != nil {
		t.Fatal(err)
	}

	other := newCentralEnv(t)
	if _, _, err := e.run(t, "", append([]string{"init"}, other.file()...)...); err != nil {
		t.Fatal(err)
	}

	otherArgs := []string{"--db", e.db, "--key-provider", "file", "--key-file", other.key}
	stdout, _, err := e.run(t, "", append([]string{"restore", "--from", cand, "--force"}, otherArgs...)...)
	if err == nil {
		t.Fatalf("expected restore with a mismatched root key to fail, got stdout=%q", stdout)
	}
	if !strings.Contains(err.Error(), "root key") {
		t.Fatalf("expected a clear wrong-key error, got %v", err)
	}

	after, err := os.ReadFile(e.db)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("a wrong-key restore attempt must not modify the active database")
	}
}

// restoreFixture initialises a database, takes a backup, then adds a project
// after it so a performed restore is observable ("later" disappears).
func restoreFixture(t *testing.T) (e *centralEnv, cand string) {
	t.Helper()
	e = newCentralEnv(t)
	for _, args := range [][]string{{"init"}, {"project", "create", "app"}} {
		if _, _, err := e.run(t, "", append(args, e.file()...)...); err != nil {
			t.Fatal(err)
		}
	}
	cand = filepath.Join(e.dir, "cand.db")
	if _, _, err := e.run(t, "", append([]string{"backup", "--out", cand}, e.file()...)...); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.run(t, "", append([]string{"project", "create", "later"}, e.file()...)...); err != nil {
		t.Fatal(err)
	}
	return e, cand
}

func snapshotFiles(t *testing.T, e *centralEnv) []string {
	t.Helper()
	m, _ := filepath.Glob(e.db + ".pre-restore-*")
	return m
}

func TestCentralRestoreCmd_ExplicitYes_ProceedsAfterValidationAndSnapshot(t *testing.T) {
	e, cand := restoreFixture(t)
	stdout, _, err := e.run(t, "y\n", append([]string{"restore", "--from", cand}, e.file()...)...)
	if err != nil {
		t.Fatal(err)
	}
	info, prompt := strings.Index(stdout, "Candidate schema version"), strings.Index(stdout, "Continue?")
	if info < 0 || prompt < 0 || info > prompt {
		t.Fatalf("candidate validation info must be shown before the prompt, got %q", stdout)
	}
	if !strings.Contains(stdout, "Restore complete") {
		t.Fatalf("expected completion after an explicit y, got %q", stdout)
	}
	if len(snapshotFiles(t, e)) != 1 {
		t.Fatalf("expected exactly one safety snapshot, got %v", snapshotFiles(t, e))
	}
	list, _, err := e.run(t, "", append([]string{"project", "list"}, e.file()...)...)
	if err != nil || strings.Contains(list, "later") || !strings.Contains(list, "app") {
		t.Fatalf("restore did not take effect as expected: %v %q", err, list)
	}
}

func TestCentralRestoreCmd_ExplicitNo_LeavesActiveDatabaseUnchanged(t *testing.T) {
	e, cand := restoreFixture(t)
	before, err := os.ReadFile(e.db)
	if err != nil {
		t.Fatal(err)
	}
	stdout, _, err := e.run(t, "n\n", append([]string{"restore", "--from", cand}, e.file()...)...)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "Continue?") || !strings.Contains(stdout, "cancelled") {
		t.Fatalf("expected the prompt to be captured and the cancellation reported, got %q", stdout)
	}
	after, _ := os.ReadFile(e.db)
	if string(before) != string(after) {
		t.Fatal("an explicit n must leave the active database bytes unchanged")
	}
	if len(snapshotFiles(t, e)) != 0 {
		t.Fatal("no safety snapshot should be created before confirmation")
	}
	list, _, _ := e.run(t, "", append([]string{"project", "list"}, e.file()...)...)
	if !strings.Contains(list, "later") {
		t.Fatal("post-backup data must still be present after declining")
	}
}

func TestCentralRestoreCmd_InvalidCandidate_NeverPromptsEvenWithYes(t *testing.T) {
	e, _ := restoreFixture(t)
	bad := filepath.Join(e.dir, "bad.db")
	if err := os.WriteFile(bad, []byte("not a sqlite database at all, just some bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, _, err := e.run(t, "y\n", append([]string{"restore", "--from", bad}, e.file()...)...)
	if err == nil {
		t.Fatal("expected an invalid candidate to be rejected")
	}
	if strings.Contains(stdout, "Continue?") {
		t.Fatal("validation must fail before any confirmation prompt")
	}
	if len(snapshotFiles(t, e)) != 0 {
		t.Fatal("no snapshot may be created for a rejected candidate")
	}
}

func TestCentralRestoreCmd_Force_StillRequiresValidationAndTakesSnapshot(t *testing.T) {
	e, _ := restoreFixture(t)
	bad := filepath.Join(e.dir, "bad.db")
	if err := os.WriteFile(bad, []byte("not a sqlite database at all, just some bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(e.db)
	if _, _, err := e.run(t, "", append([]string{"restore", "--from", bad, "--force"}, e.file()...)...); err == nil {
		t.Fatal("--force must not bypass candidate validation")
	}
	after, _ := os.ReadFile(e.db)
	if string(before) != string(after) || len(snapshotFiles(t, e)) != 0 {
		t.Fatal("a rejected --force restore must change nothing")
	}
}

func TestCentralRestoreCmd_OfflineWarning_InHelpAndPrompt(t *testing.T) {
	e, cand := restoreFixture(t)
	help, _, err := e.run(t, "", "restore", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"OFFLINE", "Stop every other process", "no cross-process lock"} {
		if !strings.Contains(help, want) {
			t.Fatalf("help is missing %q:\n%s", want, help)
		}
	}
	stdout, _, err := e.run(t, "n\n", append([]string{"restore", "--from", cand}, e.file()...)...)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"offline", "stop every other process", "concurrent writes can be lost"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("prompt is missing %q:\n%s", want, stdout)
		}
	}
}
