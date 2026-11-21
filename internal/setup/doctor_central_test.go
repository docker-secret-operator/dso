package setup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Central-store custody checks (DSO-DOCTOR-026 through DSO-DOCTOR-031). The
// central store has no persisted configuration of its own -- every `dso
// central` invocation takes --db/--key-file/--key-provider explicitly -- so
// these checks only run when `dso doctor` is given the matching
// --central-db/--central-key-file/--central-key-provider flags. Temporary
// directories only; no privileged setup required.

func newCentralDir(t *testing.T, mode os.FileMode) string {
	t.Helper()
	d, err := os.MkdirTemp("", "dso-central-")
	if err != nil {
		t.Skipf("no temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	if err := os.Chmod(d, mode); err != nil {
		t.Fatal(err)
	}
	return d
}

func writeCentralFile(t *testing.T, dir, name string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("x"), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

// centralOK builds a well-formed key+db pair (0700 dirs, 0400 key, 0600 db)
// in separate directories, so individual tests can deviate one axis at a
// time from a known-good baseline.
func centralOK(t *testing.T) (db, keyFile string) {
	t.Helper()
	dbDir := newCentralDir(t, 0o700)
	keyDir := newCentralDir(t, 0o700)
	db = writeCentralFile(t, dbDir, "central.db", 0o600)
	keyFile = writeCentralFile(t, keyDir, "root.key", 0o400)
	return db, keyFile
}

func TestCentralChecks_Skipped_WhenUnconfigured(t *testing.T) {
	cc := newCentralChecks(DoctorOptions{})
	checks := cc.run(context.Background())
	if len(checks) != 1 {
		t.Fatalf("want exactly 1 check when unconfigured, got %d: %+v", len(checks), checks)
	}
	c := checks[0]
	assertCheckID(t, c, "DSO-DOCTOR-026")
	assertInfo(t, c)
	if c.Status == DoctorFail {
		t.Fatalf("unconfigured central checks must never FAIL: %+v", c)
	}
}

func TestCentralChecks_KeyFileWrongMode(t *testing.T) {
	db, keyFile := centralOK(t)
	if err := os.Chmod(keyFile, 0o644); err != nil {
		t.Fatal(err)
	}
	cc := newCentralChecks(DoctorOptions{CentralDB: db, CentralKeyFile: keyFile})
	checks := cc.run(context.Background())
	c := findCheck(t, checks, "DSO-DOCTOR-027")
	assertFail(t, c)
	if c.Severity != DoctorHigh {
		t.Errorf("severity: want high, got %q", c.Severity)
	}
	if len(c.Recovery) == 0 {
		t.Error("expected Recovery text pointing at a remediation command")
	}
	if !strings.Contains(c.Detail, "0644") {
		t.Errorf("detail should mention the actual mode: %q", c.Detail)
	}
}

func TestCentralChecks_KeyDirWrongMode(t *testing.T) {
	db, keyFile := centralOK(t)
	if err := os.Chmod(filepath.Dir(keyFile), 0o755); err != nil {
		t.Fatal(err)
	}
	cc := newCentralChecks(DoctorOptions{CentralDB: db, CentralKeyFile: keyFile})
	checks := cc.run(context.Background())
	c := findCheck(t, checks, "DSO-DOCTOR-028")
	assertFail(t, c)
	if len(c.Recovery) == 0 {
		t.Error("expected Recovery text")
	}
}

func TestCentralChecks_DBFileWrongMode(t *testing.T) {
	db, keyFile := centralOK(t)
	if err := os.Chmod(db, 0o644); err != nil {
		t.Fatal(err)
	}
	cc := newCentralChecks(DoctorOptions{CentralDB: db, CentralKeyFile: keyFile})
	checks := cc.run(context.Background())
	c := findCheck(t, checks, "DSO-DOCTOR-029")
	assertFail(t, c)
	if !strings.Contains(c.Detail, "0644") {
		t.Errorf("detail should mention the actual mode: %q", c.Detail)
	}
}

func TestCentralChecks_DBDirWrongMode(t *testing.T) {
	db, keyFile := centralOK(t)
	if err := os.Chmod(filepath.Dir(db), 0o755); err != nil {
		t.Fatal(err)
	}
	cc := newCentralChecks(DoctorOptions{CentralDB: db, CentralKeyFile: keyFile})
	checks := cc.run(context.Background())
	c := findCheck(t, checks, "DSO-DOCTOR-030")
	assertFail(t, c)
}

func TestCentralChecks_KeyAndDBColocated(t *testing.T) {
	dir := newCentralDir(t, 0o700)
	db := writeCentralFile(t, dir, "central.db", 0o600)
	keyFile := writeCentralFile(t, dir, "root.key", 0o400)
	cc := newCentralChecks(DoctorOptions{CentralDB: db, CentralKeyFile: keyFile})
	checks := cc.run(context.Background())
	c := findCheck(t, checks, "DSO-DOCTOR-031")
	assertWarn(t, c)
	if c.Status == DoctorFail {
		t.Fatalf("co-location must be a WARN, not a hard FAIL: %+v", c)
	}
	if !strings.Contains(strings.ToLower(c.RootCause+c.Detail), "same") && !strings.Contains(strings.ToLower(c.RootCause+c.Detail), "directory") {
		t.Errorf("message should document the separation concern: %+v", c)
	}
}

func TestCentralChecks_MissingKeyFile(t *testing.T) {
	db, keyFile := centralOK(t)
	if err := os.Remove(keyFile); err != nil {
		t.Fatal(err)
	}
	cc := newCentralChecks(DoctorOptions{CentralDB: db, CentralKeyFile: keyFile})
	var checks []DoctorCheck
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("missing key file must not panic: %v", r)
			}
		}()
		checks = cc.run(context.Background())
	}()
	c := findCheck(t, checks, "DSO-DOCTOR-027")
	assertFail(t, c)
	if !strings.Contains(c.Detail, keyFile) {
		t.Errorf("detail should name the missing path: %q", c.Detail)
	}
}

func TestCentralChecks_PassphraseProviderModeSkipsKeyFileModeCheck(t *testing.T) {
	// In passphrase mode the configured --central-key-file path holds the KDF
	// parameter file, not a raw key -- installed with the same 0400/0700
	// contract (internal/crypto.InstallPassphraseKeyFile). The check must
	// still apply the permission contract to that file (never skip it
	// outright just because there is no "raw key"), and it must not fail
	// a correctly-permissioned KDF file.
	db, keyFile := centralOK(t)
	cc := newCentralChecks(DoctorOptions{CentralDB: db, CentralKeyFile: keyFile, CentralKeyProvider: "passphrase"})
	checks := cc.run(context.Background())
	c := findCheck(t, checks, "DSO-DOCTOR-027")
	assertPass(t, c)

	// And a wrongly-permissioned KDF file is still caught -- passphrase mode
	// must never falsely skip the check.
	if err := os.Chmod(keyFile, 0o644); err != nil {
		t.Fatal(err)
	}
	cc2 := newCentralChecks(DoctorOptions{CentralDB: db, CentralKeyFile: keyFile, CentralKeyProvider: "passphrase"})
	c2 := findCheck(t, cc2.run(context.Background()), "DSO-DOCTOR-027")
	assertFail(t, c2)
}

func findCheck(t *testing.T, checks []DoctorCheck, id string) DoctorCheck {
	t.Helper()
	for _, c := range checks {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no check with ID %s found among %d checks: %+v", id, len(checks), checks)
	return DoctorCheck{}
}

func TestSameDirectory(t *testing.T) {
	a := newCentralDir(t, 0o700)
	b := newCentralDir(t, 0o700)

	same, err := SameDirectory(a, a)
	if err != nil || !same {
		t.Errorf("a directory is the same as itself: same=%v err=%v", same, err)
	}
	same, err = SameDirectory(a, b)
	if err != nil || same {
		t.Errorf("two distinct directories must not compare equal: same=%v err=%v", same, err)
	}
}
