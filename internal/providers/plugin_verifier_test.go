package providers

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"go.uber.org/zap/zaptest"
)

func TestPluginVerifier_Constructor(t *testing.T) {
	logger := zaptest.NewLogger(t)
	pv := NewPluginVerifier(logger, true)
	if pv == nil {
		t.Fatal("expected non-nil PluginVerifier")
	}
}

func TestPluginVerifier_RegisterTrustedHash(t *testing.T) {
	logger := zaptest.NewLogger(t)
	pv := NewPluginVerifier(logger, false)

	validHash := "a3f5c1d2e4b6078910f2a3b4c5d6e7f8a3f5c1d2e4b6078910f2a3b4c5d6e7f8"
	if err := pv.RegisterTrustedHash("my-plugin", validHash); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Short hash
	if err := pv.RegisterTrustedHash("bad", "tooshort"); err == nil {
		t.Fatal("expected error for short hash")
	}

	// Non-hex hash
	invalidHex := fmt.Sprintf("%064s", "zz")
	if err := pv.RegisterTrustedHash("bad", invalidHex); err == nil {
		t.Fatal("expected error for invalid hex")
	}
}

func TestPluginVerifier_GeneratePluginHash(t *testing.T) {
	logger := zaptest.NewLogger(t)
	pv := NewPluginVerifier(logger, false)

	// Write a temp file to hash
	dir := t.TempDir()
	f := filepath.Join(dir, "fake-plugin")
	if err := os.WriteFile(f, []byte("hello world"), 0600); err != nil {
		t.Fatal(err)
	}

	hash, err := pv.GeneratePluginHash(f)
	if err != nil {
		t.Fatalf("GeneratePluginHash: %v", err)
	}
	if len(hash) != 64 {
		t.Errorf("expected 64-char hash, got %d", len(hash))
	}
}

func TestPluginVerifier_LoadTrustedHashesFromFile(t *testing.T) {
	logger := zaptest.NewLogger(t)
	pv := NewPluginVerifier(logger, false)

	dir := t.TempDir()
	manifest := filepath.Join(dir, "hashes.txt")
	validHash := "a3f5c1d2e4b6078910f2a3b4c5d6e7f8a3f5c1d2e4b6078910f2a3b4c5d6e7f8"
	content := fmt.Sprintf("my-plugin=%s\n# comment line\n\nbad-line\n", validHash)
	if err := os.WriteFile(manifest, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	if err := pv.LoadTrustedHashesFromFile(manifest); err != nil {
		t.Fatalf("LoadTrustedHashesFromFile: %v", err)
	}

	// Non-existent file
	if err := pv.LoadTrustedHashesFromFile("/nonexistent/path"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestPluginVerifier_LoadTrustedHashesFromFile_DuplicateEntry(t *testing.T) {
	logger := zaptest.NewLogger(t)
	pv := NewPluginVerifier(logger, false)

	dir := t.TempDir()
	manifest := filepath.Join(dir, "hashes.txt")
	hash := "a3f5c1d2e4b6078910f2a3b4c5d6e7f8a3f5c1d2e4b6078910f2a3b4c5d6e7f8"
	content := fmt.Sprintf("my-plugin=%s\nmy-plugin=%s\n", hash, hash)
	if err := os.WriteFile(manifest, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	if err := pv.LoadTrustedHashesFromFile(manifest); err == nil {
		t.Fatal("expected error for duplicate manifest entry")
	}
}

func TestPluginVerifier_LoadTrustedHashesFromFile_OversizedLine(t *testing.T) {
	logger := zaptest.NewLogger(t)
	pv := NewPluginVerifier(logger, false)

	dir := t.TempDir()
	manifest := filepath.Join(dir, "hashes.txt")
	// Write a line longer than 1024 bytes.
	content := "my-plugin=" + string(make([]byte, 1100)) + "\n"
	if err := os.WriteFile(manifest, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	if err := pv.LoadTrustedHashesFromFile(manifest); err == nil {
		t.Fatal("expected error for oversized manifest line")
	}
}

// TestPluginVerifier_TrimSpaceManifest verifies that names/hashes with surrounding
// whitespace in the manifest are parsed correctly.
func TestPluginVerifier_TrimSpaceManifest(t *testing.T) {
	logger := zaptest.NewLogger(t)
	pv := NewPluginVerifier(logger, false)

	dir := t.TempDir()
	binPath := filepath.Join(dir, "dso-provider-test")
	if err := os.WriteFile(binPath, []byte("binary content"), 0600); err != nil {
		t.Fatal(err)
	}
	hash, err := pv.GeneratePluginHash(binPath)
	if err != nil {
		t.Fatal(err)
	}

	// Write manifest with surrounding whitespace on both sides of '='
	manifest := filepath.Join(dir, "hashes.txt")
	content := fmt.Sprintf("  dso-provider-test  =  %s  \n", hash)
	if err := os.WriteFile(manifest, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := pv.LoadTrustedHashesFromFile(manifest); err != nil {
		t.Fatalf("LoadTrustedHashesFromFile with whitespace: %v", err)
	}
	if err := pv.VerifyPluginBinary(binPath); err != nil {
		t.Fatalf("VerifyPluginBinary with whitespace-trimmed entry: %v", err)
	}
}

// TestPluginVerifier_ConcurrentAccess exercises concurrent RegisterTrustedHash and
// VerifyPluginBinary calls to confirm no data race under -race.
func TestPluginVerifier_ConcurrentAccess(t *testing.T) {
	logger := zaptest.NewLogger(t)
	pv := NewPluginVerifier(logger, true)

	dir := t.TempDir()
	binPath := filepath.Join(dir, "dso-provider-concurrent")
	if err := os.WriteFile(binPath, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	hash, err := pv.GeneratePluginHash(binPath)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = pv.RegisterTrustedHash("dso-provider-concurrent", hash)
		}()
		go func() {
			defer wg.Done()
			_ = pv.VerifyPluginBinary(binPath)
		}()
	}
	wg.Wait()
}
