package vault

import (
	"os"
	"testing"
)

func TestVault_MoreCoverage(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	_ = os.Setenv("HOME", tmpDir)
	defer func() { _ = os.Setenv("HOME", oldHome) }()

	_ = InitDefault()
	v, _ := LoadDefault()

	// 1. Set with existing key
	_ = v.Set("p", "k", "v1")
	_ = v.Set("p", "k", "v2")

	// 2. SetBatch with mixed keys
	batch := map[string]string{
		"":  "invalid",
		"k": "v",
	}
	_ = v.SetBatch("p", batch)

	// 3. getMasterKey error (missing dir)
	_ = os.Setenv("HOME", "/non-existent")
	_, _ = getMasterKey()
	_ = os.Setenv("HOME", tmpDir)
}
