package providers

import (
	"fmt"
	"os"
	"path/filepath"
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
