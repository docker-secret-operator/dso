package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const ownerTestCfg = "version: v1\nagent:\n  mode: local\nproviders:\n  vault:\n    type: vault\n    config:\n      address: http://127.0.0.1:1\nsecrets: []\n"

// The agent can resolve ~/.dso/dso.yaml as its config; persisting a generated
// owner id into it must work, while every other absolute path stays rejected.
func TestEnsureOwnerID_PersistsIntoUserConfigOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	userCfg := filepath.Join(home, ".dso", "dso.yaml")
	if err := os.MkdirAll(filepath.Dir(userCfg), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userCfg, []byte(ownerTestCfg), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfigWithOwnerID(userCfg)
	if err != nil {
		t.Fatalf("user config must be usable by the agent loader: %v", err)
	}
	if cfg.Agent.OwnerID == "" {
		t.Fatal("owner id not generated")
	}
	data, _ := os.ReadFile(userCfg)
	if !strings.Contains(string(data), cfg.Agent.OwnerID) {
		t.Error("owner id not persisted to the user config")
	}
	if st, _ := os.Stat(userCfg); st.Mode().Perm() != 0o600 {
		t.Errorf("permissions changed to %v", st.Mode().Perm())
	}
	again, err := LoadConfigWithOwnerID(userCfg)
	if err != nil || again.Agent.OwnerID != cfg.Agent.OwnerID {
		t.Errorf("owner id must be stable across loads: %v / %v", err, again)
	}

	// Any other absolute path is still refused (fail closed).
	other := filepath.Join(home, "elsewhere.yaml")
	if err := os.WriteFile(other, []byte(ownerTestCfg), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfigWithOwnerID(other); err == nil {
		t.Error("absolute path outside the allowed set must still be rejected")
	}
}
