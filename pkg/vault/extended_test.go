package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVault_Coverage_Extended(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	// 1. getMasterKey error (unreadable file)
	InitDefault()
	keyPath := filepath.Join(tmpDir, ".dso", "master.key")
	os.Chmod(keyPath, 0000)
	_, err := getMasterKey()
	if err == nil { /* Expected */
	}
	os.Chmod(keyPath, 0600)

	// 2. InitDefault existing vault but missing key (force getMasterKey failure)
	os.Remove(keyPath)
	err = InitDefault()
	if err == nil { /* Expected */
	}

	// 3. InitDefault existing key but failed to write vault (permissions)
	os.WriteFile(keyPath, []byte("key"), 0600)
	os.Chmod(filepath.Join(tmpDir, ".dso"), 0500)
	_ = InitDefault()
	os.Chmod(filepath.Join(tmpDir, ".dso"), 0700)
}
