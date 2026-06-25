package cli

import (
	"testing"
)

// ── D1: EDITOR command injection ─────────────────────────────────────────────

// TestResolveEditor_Default verifies that a missing $EDITOR falls back to
// "nano" and that nano is found in PATH on any CI image that has it.
func TestResolveEditor_Default(t *testing.T) {
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	bin, _, err := resolveEditor()
	if err != nil {
		// nano not present in this environment — skip rather than fail.
		t.Skipf("fallback editor not found in PATH: %v", err)
	}
	if bin == "" {
		t.Error("resolveEditor returned empty binary")
	}
}

// TestResolveEditor_EditorWithArgs verifies that "code --wait" splits correctly
// into binary="code" and extraArgs=["--wait"].
func TestResolveEditor_EditorWithArgs(t *testing.T) {
	// Use a binary that is always present on macOS/Linux CI.
	t.Setenv("EDITOR", "sh -c true")

	bin, args, err := resolveEditor()
	if err != nil {
		t.Fatalf("resolveEditor: %v", err)
	}
	if bin == "" {
		t.Error("binary must not be empty")
	}
	if len(args) != 2 || args[0] != "-c" || args[1] != "true" {
		t.Errorf("extraArgs = %v, want [-c true]", args)
	}
}

// TestResolveEditor_NonExistentBinary verifies that a garbage $EDITOR value
// returns an error rather than silently resolving.
func TestResolveEditor_NonExistentBinary(t *testing.T) {
	t.Setenv("EDITOR", "definitely-does-not-exist-xyzzy")

	_, _, err := resolveEditor()
	if err == nil {
		t.Error("expected error for non-existent editor binary, got nil")
	}
}

// ── D2: SSRF — loopback validation ───────────────────────────────────────────

func TestValidateLoopbackURL_Loopback(t *testing.T) {
	valid := []string{
		"http://127.0.0.1:8471",
		"http://localhost:8471",
		"http://127.0.0.1:8471/api/events",
	}
	for _, addr := range valid {
		if err := validateLoopbackURL(addr); err != nil {
			t.Errorf("validateLoopbackURL(%q) unexpectedly failed: %v", addr, err)
		}
	}
}

func TestValidateLoopbackURL_NonLoopback(t *testing.T) {
	nonLoopback := []string{
		"http://169.254.169.254",          // AWS metadata endpoint
		"http://10.0.0.1:8471",            // internal network
		"http://192.168.1.1:8471",         // LAN
		"ftp://127.0.0.1:8471",            // wrong scheme
		"not-a-url",                       // unparseable
	}
	for _, addr := range nonLoopback {
		if err := validateLoopbackURL(addr); err == nil {
			t.Errorf("validateLoopbackURL(%q): expected rejection, got nil", addr)
		}
	}
}
