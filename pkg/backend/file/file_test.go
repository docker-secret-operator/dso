package file

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFileProvider_GetSecret(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "dso-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	p := &FileProvider{basePath: tempDir}

	// Test Case 1: JSON Secret
	secretData := map[string]string{"API_KEY": "12345", "DB_PASS": "secret"}
	b, _ := json.Marshal(secretData)
	err = os.WriteFile(filepath.Join(tempDir, "mysercret.json"), b, 0644)
	if err != nil {
		t.Fatal(err)
	}

	got, err := p.GetSecret("mysercret")
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}
	if got["API_KEY"] != "12345" {
		t.Errorf("Expected API_KEY=12345, got %v", got["API_KEY"])
	}

	// Test Case 2: Plain Text Secret
	err = os.WriteFile(filepath.Join(tempDir, "plain"), []byte("just-text"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	got, err = p.GetSecret("plain")
	if err != nil {
		t.Fatalf("Failed to get plain secret: %v", err)
	}
	if got["value"] != "just-text" {
		t.Errorf("Expected value=just-text, got %v", got["value"])
	}

	// Test Case 3: Missing Secret
	_, err = p.GetSecret("nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent secret, got nil")
	}
}

func TestFileProvider_Init(t *testing.T) {
	p := &FileProvider{}
	err := p.Init(map[string]string{"path": "/tmp/custom"})
	if err != nil {
		t.Fatal(err)
	}
	if p.basePath != "/tmp/custom" {
		t.Errorf("Expected basePath=/tmp/custom, got %s", p.basePath)
	}

	// Test Default
	p = &FileProvider{}
	_ = p.Init(map[string]string{})
	if p.basePath != "/etc/dso/secrets" {
		t.Errorf("Expected default basePath=/etc/dso/secrets, got %s", p.basePath)
	}
}
