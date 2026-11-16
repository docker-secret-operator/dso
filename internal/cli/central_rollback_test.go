package cli

import (
	"strings"
	"testing"
)

// setupRolledBackSecret is a shared fixture: initialises a central store with
// one project/environment and a secret "k" at version 1 ("v1"), then adds
// version 2 ("v2"). Tests build on top of this common shape.
func setupTwoVersionSecret(t *testing.T, e *centralEnv) {
	t.Helper()
	if _, _, err := e.run(t, "", append([]string{"init"}, e.file()...)...); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := e.run(t, "", append([]string{"project", "create", "app"}, e.file()...)...); err != nil {
		t.Fatalf("project create: %v", err)
	}
	if _, _, err := e.run(t, "", append([]string{"environment", "create", "app", "prod"}, e.file()...)...); err != nil {
		t.Fatalf("environment create: %v", err)
	}
	if _, _, err := e.run(t, "v1\n", append([]string{"secret", "create", "app", "prod", "k", "--value-stdin"}, e.file()...)...); err != nil {
		t.Fatalf("secret create: %v", err)
	}
	if _, _, err := e.run(t, "v2\n", append([]string{"secret", "add-version", "app", "prod", "k", "--value-stdin"}, e.file()...)...); err != nil {
		t.Fatalf("secret add-version: %v", err)
	}
}

func TestCentralSecretRollback_Success(t *testing.T) {
	e := newCentralEnv(t)
	setupTwoVersionSecret(t, e)

	out, _, err := e.run(t, "", append([]string{"secret", "rollback", "app", "prod", "k", "1", "--comment", "revert"}, e.file()...)...)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if !strings.Contains(out, "3") {
		t.Fatalf("expected the new version number (3) in stdout, got %q", out)
	}

	// The rollback must have created a brand-new version 3 carrying v1's
	// value -- never an in-place revert of an existing version.
	got, _, err := e.run(t, "", append([]string{"secret", "get", "app", "prod", "k", "--reveal"}, e.file()...)...)
	if err != nil || got != "v1" {
		t.Fatalf("latest value after rollback = %q, %v; want v1", got, err)
	}
	versionsOut, _, err := e.run(t, "", append([]string{"secret", "versions", "app", "prod", "k"}, e.file()...)...)
	if err != nil {
		t.Fatalf("versions: %v", err)
	}
	if strings.Count(versionsOut, "\n") < 4 { // header + 3 version rows
		t.Fatalf("expected 3 versions after rollback, got:\n%s", versionsOut)
	}
}

func TestCentralSecretRollback_DisabledVersionRejected(t *testing.T) {
	e := newCentralEnv(t)
	setupTwoVersionSecret(t, e)
	if _, _, err := e.run(t, "", append([]string{"secret", "disable-version", "app", "prod", "k", "1"}, e.file()...)...); err != nil {
		t.Fatalf("disable-version: %v", err)
	}

	out, _, err := e.run(t, "", append([]string{"secret", "rollback", "app", "prod", "k", "1"}, e.file()...)...)
	if err == nil {
		t.Fatalf("expected an error rolling back from a disabled version, got out=%q", out)
	}
	if out != "" {
		t.Fatalf("rollback must print nothing on failure, got %q", out)
	}
	msg := err.Error()
	if !strings.Contains(msg, "disabled") {
		t.Fatalf("expected a clean 'disabled' message, got %q", msg)
	}
	for _, leak := range []string{"SQL", "sqlite3", ".go:", "goroutine", "SELECT", "panic"} {
		if strings.Contains(msg, leak) {
			t.Fatalf("error leaked internal detail %q: %s", leak, msg)
		}
	}

	// Nothing must have been appended: still exactly 2 versions.
	versionsOut, _, err := e.run(t, "", append([]string{"secret", "versions", "app", "prod", "k"}, e.file()...)...)
	if err != nil {
		t.Fatalf("versions: %v", err)
	}
	if strings.Count(versionsOut, "\n") != 3 { // header + 2 version rows
		t.Fatalf("rejected rollback must not append a version, got:\n%s", versionsOut)
	}
}

func TestCentralSecretGet_MissingVersion(t *testing.T) {
	e := newCentralEnv(t)
	setupTwoVersionSecret(t, e)

	out, _, err := e.run(t, "", append([]string{"secret", "get", "app", "prod", "k", "--reveal", "--version", "99"}, e.file()...)...)
	if err == nil {
		t.Fatalf("expected an error for a nonexistent version, got out=%q", out)
	}
	if out != "" {
		t.Fatalf("expected no output on failure, got %q", out)
	}
}

func TestCentralSecretGet_DisabledVersion(t *testing.T) {
	e := newCentralEnv(t)
	setupTwoVersionSecret(t, e)
	if _, _, err := e.run(t, "", append([]string{"secret", "disable-version", "app", "prod", "k", "1"}, e.file()...)...); err != nil {
		t.Fatalf("disable-version: %v", err)
	}

	out, _, err := e.run(t, "", append([]string{"secret", "get", "app", "prod", "k", "--reveal", "--version", "1"}, e.file()...)...)
	if err == nil {
		t.Fatalf("expected an error reading an explicitly-disabled version, got out=%q", out)
	}
	if out != "" {
		t.Fatalf("expected no output on failure, got %q", out)
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected a clean 'disabled' message, got %q", err.Error())
	}
}

func TestCentralSecretGet_DeletedSecret(t *testing.T) {
	e := newCentralEnv(t)
	setupTwoVersionSecret(t, e)
	if _, _, err := e.run(t, "", append([]string{"secret", "delete", "app", "prod", "k"}, e.file()...)...); err != nil {
		t.Fatalf("delete: %v", err)
	}

	out, _, err := e.run(t, "", append([]string{"secret", "get", "app", "prod", "k", "--reveal"}, e.file()...)...)
	if err == nil {
		t.Fatalf("expected an error reading a deleted secret, got out=%q", out)
	}
	if out != "" {
		t.Fatalf("expected no output on failure, got %q", out)
	}
}

func TestCentralSecretGet_WrongProjectOrEnv(t *testing.T) {
	e := newCentralEnv(t)
	setupTwoVersionSecret(t, e)

	if out, _, err := e.run(t, "", append([]string{"secret", "get", "nope", "prod", "k", "--reveal"}, e.file()...)...); err == nil || out != "" {
		t.Fatalf("wrong project accepted: out=%q err=%v", out, err)
	}
	if out, _, err := e.run(t, "", append([]string{"secret", "get", "app", "nope", "k", "--reveal"}, e.file()...)...); err == nil || out != "" {
		t.Fatalf("wrong environment accepted: out=%q err=%v", out, err)
	}
}

func TestCentralSecretGet_RevealRequiredForValue(t *testing.T) {
	e := newCentralEnv(t)
	if _, _, err := e.run(t, "", append([]string{"init"}, e.file()...)...); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := e.run(t, "", append([]string{"project", "create", "app"}, e.file()...)...); err != nil {
		t.Fatalf("project create: %v", err)
	}
	if _, _, err := e.run(t, "", append([]string{"environment", "create", "app", "prod"}, e.file()...)...); err != nil {
		t.Fatalf("environment create: %v", err)
	}
	const secret = "TOP-SECRET-reveal-gate-value"
	if _, _, err := e.run(t, secret+"\n", append([]string{"secret", "create", "app", "prod", "k", "--value-stdin"}, e.file()...)...); err != nil {
		t.Fatalf("secret create: %v", err)
	}

	out, errOut, err := e.run(t, "", append([]string{"secret", "get", "app", "prod", "k"}, e.file()...)...)
	if err == nil {
		t.Fatalf("expected an error omitting --reveal, got out=%q", out)
	}
	if strings.Contains(out+errOut, secret) {
		t.Fatalf("value leaked without --reveal: out=%q err=%q", out, errOut)
	}
	// Also with an explicit --version but no --reveal.
	out, errOut, err = e.run(t, "", append([]string{"secret", "get", "app", "prod", "k", "--version", "1"}, e.file()...)...)
	if err == nil {
		t.Fatalf("expected an error omitting --reveal with --version, got out=%q", out)
	}
	if strings.Contains(out+errOut, secret) {
		t.Fatalf("value leaked without --reveal (with --version): out=%q err=%q", out, errOut)
	}
}
