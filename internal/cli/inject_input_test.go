package cli

import (
	"bytes"
	"strings"
	"testing"
)

const injectCanary = "SYNTHETIC-INJECT-CANARY"

// runInject runs `inject` with a bogus Docker endpoint so nothing real is
// contacted: input handling is exercised, then Docker lookup fails.
func runInject(t *testing.T, stdin string, args ...string) (err error, stdout, stderr string) {
	t.Helper()
	t.Setenv("DOCKER_HOST", "unix:///nonexistent/dso-test.sock")
	cmd := NewInjectCmd()
	var o, e bytes.Buffer
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(&o)
	cmd.SetErr(&e)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return err, o.String(), e.String()
}

func TestInject_ValueFlagWarnsAndNeverEchoesSecret(t *testing.T) {
	err, out, errOut := runInject(t, "", "--container", "c", "--secret", "s", "--value", injectCanary)
	if err == nil {
		t.Fatal("expected failure reaching the bogus Docker endpoint")
	}
	if !strings.Contains(errOut, "--value puts the secret in the command line") {
		t.Errorf("missing unsafe-input warning on stderr: %q", errOut)
	}
	for name, s := range map[string]string{"error": err.Error(), "stdout": out, "stderr": errOut} {
		if strings.Contains(s, injectCanary) {
			t.Errorf("%s leaked the secret value", name)
		}
	}
}

func TestInject_ValueStdinReadsPipedInputWithoutWarning(t *testing.T) {
	err, out, errOut := runInject(t, injectCanary+"\n", "--container", "c", "--secret", "s", "--value-stdin")
	if err == nil || !strings.Contains(err.Error(), "container not found") {
		t.Fatalf("expected to reach Docker lookup, got %v", err)
	}
	if strings.Contains(errOut, "--value puts") {
		t.Error("stdin mode must not print the --value warning")
	}
	for _, s := range []string{err.Error(), out, errOut} {
		if strings.Contains(s, injectCanary) {
			t.Error("secret leaked into output")
		}
	}
}

func TestInject_InputValidation(t *testing.T) {
	cases := map[string][]string{
		"value and stdin exclusive": {"--container", "c", "--secret", "s", "--value", "x", "--value-stdin"},
		"empty --value":             {"--container", "c", "--secret", "s", "--value", ""},
		"missing container":         {"--secret", "s", "--value", "x"},
		"missing secret":            {"--container", "c", "--value", "x"},
	}
	for name, args := range cases {
		if err, _, _ := runInject(t, "", args...); err == nil || strings.Contains(err.Error(), "container not found") {
			t.Errorf("%s: want validation error before Docker access, got %v", name, err)
		}
	}
	if err, _, _ := runInject(t, "", "--container", "c", "--secret", "s", "--value-stdin"); err == nil || strings.Contains(err.Error(), "container not found") {
		t.Errorf("empty stdin must be rejected before Docker access, got %v", err)
	}
}

func TestInject_HelpDocumentsRisksAndSafeAlternatives(t *testing.T) {
	long := NewInjectCmd().Long
	for _, want := range []string{"shell history", "process listings", "CI logs", "--value-stdin", "UNSAFE"} {
		if !strings.Contains(long, want) {
			t.Errorf("help missing %q", want)
		}
	}
}
