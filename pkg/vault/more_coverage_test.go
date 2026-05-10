package vault

import (
	"os"
	"testing"
)

func TestVault_MoreCoverage(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	InitDefault()
	v, _ := LoadDefault()
	
	// 1. Set with existing key
	v.Set("p", "k", "v1")
	v.Set("p", "k", "v2")
	
	// 2. SetBatch with mixed keys
	batch := map[string]string{
		"":  "invalid",
		"k": "v",
	}
	v.SetBatch("p", batch)
	
	// 3. getMasterKey error (missing dir)
	os.Setenv("HOME", "/non-existent")
	_, _ = getMasterKey()
	os.Setenv("HOME", tmpDir)
}
