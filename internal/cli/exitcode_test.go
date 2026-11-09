package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func runCLI(args ...string) (code int, stdout, stderr string) {
	var o, e bytes.Buffer
	code = Run(args, &o, &e)
	return code, o.String(), e.String()
}

func TestRun_UnknownSubcommandsFailEverywhere(t *testing.T) {
	for _, group := range [][]string{{}, {"secret"}, {"central"}, {"config"}, {"system"}, {"env"}} {
		args := append(append([]string{}, group...), "definitely-not-a-command")
		code, out, errOut := runCLI(args...)
		if code == 0 {
			t.Errorf("dso %v: exit 0, want non-zero", args)
		}
		if !strings.Contains(errOut, "definitely-not-a-command") {
			t.Errorf("dso %v: stderr should name the bad command, got %q", args, errOut)
		}
		if strings.Count(errOut, "unknown command") != 1 {
			t.Errorf("dso %v: error must print exactly once, got %q", args, errOut)
		}
		if strings.Contains(out, "Usage:") {
			t.Errorf("dso %v: usage must not be dumped to stdout on error", args)
		}
	}
}

func TestRun_EveryGroupCommandRejectsStrayArgs(t *testing.T) {
	var walk func(c *cobra.Command, path string)
	walk = func(c *cobra.Command, path string) {
		if c.HasSubCommands() && c.Name() != "help" {
			if err := c.Args(c, []string{"stray-arg"}); err == nil {
				t.Errorf("%s: group command accepts stray positional args", path)
			}
		}
		for _, sub := range c.Commands() {
			walk(sub, path+" "+sub.Name())
		}
	}
	walk(NewRootCmd(), "dso")
}

func TestRun_BareGroupPrintsHelpAndSucceeds(t *testing.T) {
	code, out, _ := runCLI("secret")
	if code != 0 {
		t.Fatalf("bare group should exit 0, got %d", code)
	}
	if !strings.Contains(out, "Available Commands") {
		t.Errorf("expected help on stdout, got %q", out)
	}
}

func TestRun_HelpVersionCompletionStillSucceed(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"secret", "--help"}, {"help", "secret"}, {"version"}, {"completion", "bash"}} {
		if code, _, errOut := runCLI(args...); code != 0 {
			t.Errorf("dso %v: exit %d, stderr %q", args, code, errOut)
		}
	}
}

func TestRun_InvalidAndMissingArgsFail(t *testing.T) {
	cases := [][]string{
		{"secret", "set"},                 // missing required arg
		{"secret", "get", "a/b", "extra"}, // too many args
		{"secret", "get", "--no-such-flag", "a/b"},
		{"--no-such-flag"},
	}
	for _, args := range cases {
		code, _, errOut := runCLI(args...)
		if code == 0 {
			t.Errorf("dso %v: exit 0, want non-zero", args)
		}
		if strings.TrimSpace(errOut) == "" {
			t.Errorf("dso %v: expected an error on stderr", args)
		}
	}
}

func TestRun_RuntimeErrorPropagatesOnceWithoutUsage(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no vault here
	t.Setenv("DSO_MASTER_KEY", "synthetic-test-master-key-not-real")
	code, out, errOut := runCLI("secret", "get", "proj/missing")
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if strings.Count(errOut, "Error:") != 1 {
		t.Errorf("error must print once, got %q", errOut)
	}
	if strings.Contains(errOut, "Usage:") || strings.Contains(out, "Usage:") {
		t.Error("runtime failures must not print usage")
	}
}

func TestRun_SuccessExitsZero(t *testing.T) {
	isolatedVault(t)
	if code, _, e := runCLI("secret", "list", "proj"); code != 0 {
		t.Fatalf("exit %d: %s", code, e)
	}
}
