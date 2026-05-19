package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestConfig_LegacyUnmarshal_Full(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		yaml  string
		check func(*testing.T, *SecretMapping)
	}{
		{
			name: "legacy inject and rotation",
			yaml: `
inject: env
rotation: true
`,
			check: func(t *testing.T, s *SecretMapping) {
				if s.Inject.Type != "env" || !s.Rotation.Enabled {
					t.Errorf("Legacy inject/rotation failed: %+v", s)
				}
			},
		},
		{
			name: "legacy reload_strategy",
			yaml: `
reload_strategy:
  type: rolling
`,
			check: func(t *testing.T, s *SecretMapping) {
				if s.Rotation.Strategy != "rolling" || !s.Rotation.Enabled {
					t.Errorf("Legacy reload_strategy failed: %+v", s)
				}
			},
		},
	}

	for _, tt := range tests {
		var s SecretMapping
		if err := yaml.Unmarshal([]byte(tt.yaml), &s); err != nil {
			t.Errorf("%s: unmarshal failed: %v", tt.name, err)
			continue
		}
		tt.check(t, &s)
	}
}

func TestConfig_PathWithinDir_Edge(t *testing.T) {
	t.Parallel()
	if pathWithinDir("/tmp/a", "/tmp") == false {
		t.Error("Expected /tmp/a to be within /tmp")
	}
	if pathWithinDir("/etc/passwd", "/tmp") == true {
		t.Error("Expected /etc/passwd NOT to be within /tmp")
	}
}

func TestIsSafePath_WithBaseDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Valid relative path within base dir
	result, err := IsSafePath(dir, "subdir/file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(result, dir) {
		t.Errorf("expected result under %s, got %s", dir, result)
	}

	// Absolute path within base dir
	absFile := filepath.Join(dir, "cfg.yaml")
	result, err = IsSafePath(dir, absFile)
	if err != nil {
		t.Fatalf("abs path in base dir: %v", err)
	}
	if result != absFile {
		t.Errorf("expected %s, got %s", absFile, result)
	}

	// Path traversal attempt
	_, err = IsSafePath(dir, "../etc/passwd")
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}

func TestLoadConfigWithDecryption_WithKey(t *testing.T) {
	content := `version: v1.0.0
mode: agent
providers:
  main:
    type: aws
    region: us-east-1
secrets: []
`
	f, err := os.CreateTemp(".", "dso-coverage-key-test-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	masterKey := make([]byte, 32)
	cfg, err := LoadConfigWithDecryption(filepath.Base(f.Name()), masterKey)
	if err != nil {
		t.Fatalf("LoadConfigWithDecryption(with key): %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
}

func TestLoadConfigWithDecryption_NoKey(t *testing.T) {
	// IsSafePath rejects absolute paths outside system dirs, so use a relative temp file.
	content := `version: v1.0.0
mode: agent
providers:
  main:
    type: aws
    region: us-east-1
secrets: []
`
	f, err := os.CreateTemp(".", "dso-coverage-test-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	cfg, err := LoadConfigWithDecryption(filepath.Base(f.Name()), nil)
	if err != nil {
		t.Fatalf("LoadConfigWithDecryption(no key): %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
}
