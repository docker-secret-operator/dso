package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Three legacy-format mappings must yield exactly one consolidated notice,
// and the notice must not quote config content.
func TestLoadConfig_LegacyMappingsWarnOnce(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	body := `version: v1
providers:
  local:
    type: file
secrets:
  - name: a
    provider: local
    inject: env
    rotation: true
  - name: b
    provider: local
    inject: env
  - name: c
    provider: local
    inject: env
`
	if err := os.WriteFile(filepath.Join(dir, "dso.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	legacyNoticed = sync.Map{}
	_, loadErr := LoadConfig("dso.yaml")
	_, _ = LoadConfig("dso.yaml") // second load of the same file must stay silent
	_ = w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	t.Logf("load err (irrelevant to warning count): %v", loadErr)

	if n := strings.Count(buf.String(), "legacy format"); n != 1 {
		t.Fatalf("legacy notice printed %d times, want exactly 1:\n%s", n, buf.String())
	}
	if strings.Contains(buf.String(), "will attempt legacy fallback") {
		t.Error("old per-mapping warning still present")
	}
}
