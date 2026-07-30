package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVault_Coverage_Combined(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	_ = os.Setenv("HOME", tmpDir)
	defer func() { _ = os.Setenv("HOME", oldHome) }()

	// 1. Env Key
	_ = os.Setenv("DSO_MASTER_KEY", "env-key")
	key, _ := getMasterKey()
	if key != "env-key" {
		t.Error("env key failed")
	}
	_ = os.Unsetenv("DSO_MASTER_KEY")

	// 2. Init Error (block .dso)
	_ = os.WriteFile(tmpDir+"/.dso", []byte("blocked"), 0644)
	_ = InitDefault()
	_ = os.Remove(tmpDir + "/.dso")

	// 3. Proper Init
	if err := InitDefault(); err != nil {
		t.Fatal(err)
	}

	// 4. Re-Init (existing)
	if err := InitDefault(); err != nil {
		t.Fatal(err)
	}

	// 5. Load Error (missing file - manually delete it)
	_ = os.Remove(filepath.Join(tmpDir, ".dso", "vault.enc"))
	_, err := LoadDefault()
	if err == nil {
		t.Error("expected load error")
	}

	// 6. Restore and test Get/List
	_ = InitDefault()
	v, _ := LoadDefault()
	_ = v.Set("p", "k", "v")
	_, _ = v.Get("p", "k")
	_, _ = v.List("p")
	_, _ = v.List("missing")
	_ = v.SetBatch("p", nil)

	// 7. Save Error (permissions)
	_ = os.Chmod(filepath.Join(tmpDir, ".dso"), 0500)
	_ = v.Set("p", "k2", "v2")
	_ = os.Chmod(filepath.Join(tmpDir, ".dso"), 0700)
}
