package cli

import (
	"testing"
)

func TestParseKey(t *testing.T) {
	p, path, err := parseKey("proj/path")
	if err != nil || p != "proj" || path != "path" {
		t.Fatal("parseKey failed")
	}

	p, path, err = parseKey("path")
	if err != nil {
		t.Fatal("parseKey default failed")
	}

	_, _, err = parseKey("")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNewSecretSetCmd(t *testing.T) {
	cmd := newSecretSetCmd()
	if cmd == nil || cmd.Use != "set <project>/<path>" {
		t.Fatal("bad use string")
	}
}

func TestNewSecretGetCmd(t *testing.T) {
	cmd := newSecretGetCmd()
	if cmd == nil || cmd.Use != "get <project>/<path>" {
		t.Fatal("bad use string")
	}
}

func TestNewSecretListCmd(t *testing.T) {
	cmd := newSecretListCmd()
	if cmd == nil || cmd.Use != "list [project]" {
		t.Fatal("bad use string")
	}
}

func TestNewEnvImportSubCmd(t *testing.T) {
	cmd := newEnvImportSubCmd()
	if cmd == nil || cmd.Use != "import <file> [project]" {
		t.Fatal("bad use string")
	}
}

func TestNewSystemDoctorCmd(t *testing.T) {
	cmd := newSystemDoctorCmd()
	if cmd == nil || cmd.Use != "doctor" {
		t.Fatal("bad use string")
	}
}

func TestNewSystemSetupCmd(t *testing.T) {
	cmd := newSystemSetupCmd()
	if cmd == nil || cmd.Use != "setup" {
		t.Fatal("bad use string")
	}
}
