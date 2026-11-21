package config

import (
	"os"
	"strings"
	"testing"
)

// EnsureOwnerID must keep the config file's existing mode (M1 policy: never
// loosen), keep the rest of the file, and the generated id must survive
// RedactYAML (it is an identifier, not a credential).
func TestEnsureOwnerID_PersistsPreservesModeAndIsNotRedacted(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir) // IsSafePath only accepts relative paths
	path := "dso.yaml"
	if err := os.WriteFile(path, []byte("# keep me\nagent:\n  mode: local\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{}
	if err := EnsureOwnerID(cfg, path); err != nil {
		t.Fatalf("EnsureOwnerID: %v", err)
	}
	if cfg.Agent.OwnerID == "" {
		t.Fatal("owner id not set on the config")
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o, want 0640", st.Mode().Perm())
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "# keep me") || !strings.Contains(string(raw), cfg.Agent.OwnerID) {
		t.Fatalf("config rewritten without comment or id:\n%s", raw)
	}
	red, err := RedactYAML(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(red), cfg.Agent.OwnerID) {
		t.Fatalf("owner_id was redacted:\n%s", red)
	}
	// idempotent: a second call keeps the same id
	id := cfg.Agent.OwnerID
	if err := EnsureOwnerID(cfg, path); err != nil || cfg.Agent.OwnerID != id {
		t.Fatalf("second call changed id or failed: %v", err)
	}
}

// A looser file must be tightened on rewrite, never kept or loosened.
func TestEnsureOwnerID_TightensLooseMode(t *testing.T) {
	for _, tc := range []struct{ in, want os.FileMode }{{0o664, 0o640}, {0o644, 0o640}, {0o755, 0o640}, {0o600, 0o600}, {0o640, 0o640}} {
		dir := t.TempDir()
		t.Chdir(dir)
		if err := os.WriteFile("dso.yaml", []byte("agent:\n  mode: local\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod("dso.yaml", tc.in); err != nil {
			t.Fatal(err)
		}
		if err := EnsureOwnerID(&Config{}, "dso.yaml"); err != nil {
			t.Fatalf("mode %o: %v", tc.in, err)
		}
		st, _ := os.Stat("dso.yaml")
		if st.Mode().Perm() != tc.want {
			t.Errorf("input %o: mode = %o, want %o", tc.in, st.Mode().Perm(), tc.want)
		}
	}
}
