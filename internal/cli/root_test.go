package cli

import (
	"bytes"
	"os"
	"testing"
)

func TestNewRootCmd(t *testing.T) {
	cmd := NewRootCmd()
	if cmd == nil {
		t.Fatal("Expected non-nil root command")
	}

	if cmd.Use != "dso" {
		t.Fatalf("Expected Use to be 'dso', got %s", cmd.Use)
	}

	expectedMinCommands := 10
	if len(cmd.Commands()) < expectedMinCommands {
		t.Errorf("Expected at least %d commands, got %d", expectedMinCommands, len(cmd.Commands()))
	}
}

func TestResolveConfig(t *testing.T) {
	t.Cleanup(func() { CfgFile = ""; cfgFileExplicit = false })

	// Simple test, assumes dso.yaml is default
	CfgFile = ""
	cfgFileExplicit = false
	cfg := ResolveConfig()
	if cfg != "dso.yaml" && cfg != "/etc/dso/dso.yaml" {
		t.Errorf("Unexpected default config path: %s", cfg)
	}

	CfgFile = "custom.yaml"
	cfgFileExplicit = false
	cfg = ResolveConfig()
	if cfg != "custom.yaml" {
		t.Errorf("Expected custom.yaml, got: %s", cfg)
	}
}

// TestResolveConfig_ExplicitDsoYamlNotConfusedWithDefault is the P1-2
// regression test (Phase 9A finding): `-c dso.yaml` must resolve to the
// explicitly-named file via Priority 1, not be silently treated the same as
// -c never being passed at all. Before the fix, CfgFile alone could not
// distinguish these two cases because the flag's own default value is
// literally "dso.yaml", so ResolveConfig() fell through past Priority 1 in
// both cases -- an operator running `dso agent -c dso.yaml` from a directory
// containing their own dso.yaml would silently have it ignored in favor of
// /etc/dso/dso.yaml whenever both existed, which is exactly what happened
// live during this project's own Phase 9A/9B testing.
//
// Runs from a fresh empty temp directory so Priority 3 (./dso.yaml) can
// never coincidentally produce the same answer as Priority 1 -- isolating
// exactly the branch this fix changes.
func TestResolveConfig_ExplicitDsoYamlNotConfusedWithDefault(t *testing.T) {
	t.Cleanup(func() { CfgFile = ""; cfgFileExplicit = false })

	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWd) })
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("failed to chdir to temp dir: %v", err)
	}

	// Operator explicitly passed `-c dso.yaml` -- Changed("config") is true.
	// ResolveConfig() must return "dso.yaml" via Priority 1 (explicit CLI
	// flag) unconditionally, without even consulting /etc/dso/dso.yaml.
	CfgFile = "dso.yaml"
	cfgFileExplicit = true
	got := ResolveConfig()
	if got != "dso.yaml" {
		t.Fatalf("P1-2 regression: explicit `-c dso.yaml` resolved to %q instead of the explicitly-named \"dso.yaml\" -- it was silently treated as if -c had never been passed", got)
	}
}

func TestExecuteHelp(t *testing.T) {
	cmd := NewRootCmd()

	b := bytes.NewBufferString("")
	cmd.SetOut(b)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Unexpected error executing help: %v", err)
	}

	out := b.String()
	if len(out) == 0 {
		t.Fatal("Expected help output, got empty string")
	}
}
