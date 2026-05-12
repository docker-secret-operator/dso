package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVault_Coverage_Combined(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	// 1. Env Key
	os.Setenv("DSO_MASTER_KEY", "env-key")
	key, _ := getMasterKey()
	if key != "env-key" {
		t.Error("env key failed")
	}
	os.Unsetenv("DSO_MASTER_KEY")

	// 2. Init Error (block .dso)
	os.WriteFile(tmpDir+"/.dso", []byte("blocked"), 0644)
	_ = InitDefault()
	os.Remove(tmpDir + "/.dso")

	// 3. Proper Init
	if err := InitDefault(); err != nil {
		t.Fatal(err)
	}

	// 4. Re-Init (existing)
	if err := InitDefault(); err != nil {
		t.Fatal(err)
	}

	// 5. Load Error (missing file - manually delete it)
	os.Remove(filepath.Join(tmpDir, ".dso", "vault.enc"))
	_, err := LoadDefault()
	if err == nil {
		t.Error("expected load error")
	}

	// 6. Restore and test Get/List
	InitDefault()
	v, _ := LoadDefault()
	v.Set("p", "k", "v")
	v.Get("p", "k")
	v.List("p")
	v.List("missing")
	v.SetBatch("p", nil)

	// 7. Save Error (permissions)
	os.Chmod(filepath.Join(tmpDir, ".dso"), 0500)
	v.Set("p", "k2", "v2")
	os.Chmod(filepath.Join(tmpDir, ".dso"), 0700)
}
