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
