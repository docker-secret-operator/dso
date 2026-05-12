package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSortedKeys(t *testing.T) {
	m := map[string]string{"b": "2", "a": "1", "c": "3"}
	keys := sortedKeys(m)
	if len(keys) > 0 && keys[0] != "a" {
		t.Fatal("not sorted")
	}
}

func TestCheckPath(t *testing.T) {
	dir := t.TempDir()

	// Exists
	_, st := checkPath(dir)
	if st == "❌ " {
		t.Fatal("expected dir to exist")
	}

	// Doesn't exist
	checkPath(filepath.Join(dir, "missing"))
}

func TestValidateProviders(t *testing.T) {
	_, err := validateProviders("aws,vault")
	if err != nil {
		t.Fatal("expected valid providers")
	}

	_, err = validateProviders("invalid")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveProviders(t *testing.T) {
	res, _ := resolveProviders("")
	if len(res) == 0 {
		t.Fatal("expected defaults")
	}

	res2, _ := resolveProviders("aws,vault")
	if len(res2) != 2 {
		t.Fatal("expected 2")
	}
}

func TestIsTerminal(t *testing.T) {
	_ = isTerminal()
}

func TestValidateChecksum(t *testing.T) {
	err := validateChecksum("missing", "hash")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	os.WriteFile(src, []byte("data"), 0644)

	err := copyFile(src, dst)
	if err != nil {
		t.Fatal(err)
	}
}
