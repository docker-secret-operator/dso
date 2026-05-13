package cli

import (
	"bytes"
	"testing"
)

func TestNewSystemCmd(t *testing.T) {
	cmd := NewSystemCmd()
	if cmd == nil || cmd.Use != "system" {
		t.Fatal("Expected system command")
	}

	foundStatus := false
	foundEnable := false
	foundLogs := false
	for _, c := range cmd.Commands() {
		if c.Name() == "status" {
			foundStatus = true
		}
		if c.Name() == "enable" {
			foundEnable = true
		}
		if c.Name() == "logs" {
			foundLogs = true
		}
	}
	if !foundStatus || !foundEnable || !foundLogs {
		t.Fatal("Expected status, enable, and logs subcommands")
	}
}

func TestNewSecretCmd(t *testing.T) {
	cmd := NewSecretCmd()
	if cmd == nil || cmd.Use != "secret" {
		t.Fatal("Expected secret command")
	}

	foundSet := false
	foundGet := false
	foundList := false
	for _, c := range cmd.Commands() {
		if c.Name() == "set" {
			foundSet = true
		}
		if c.Name() == "get" {
			foundGet = true
		}
		if c.Name() == "list" {
			foundList = true
		}
	}
	if !foundSet || !foundGet || !foundList {
		t.Fatal("Expected set, get, list subcommands")
	}
}

func TestNewAgentCmd(t *testing.T) {
	cmd := NewAgentCmd()
	if cmd == nil || cmd.Use != "legacy-agent" {
		t.Fatal("Expected legacy-agent command")
	}
}

func TestNewMetadataCmd(t *testing.T) {
	cmd := NewMetadataCmd()
	if cmd == nil || cmd.Use != "docker-cli-plugin-metadata" {
		t.Fatal("Expected docker-cli-plugin-metadata command")
	}
}

func TestNewInitCmd(t *testing.T) {
	cmd := NewInitCmd()
	if cmd == nil || cmd.Use != "init" {
		t.Fatal("Expected init command")
	}
}

func TestNewValidateCmd(t *testing.T) {
	cmd := NewValidateCmd()
	if cmd == nil || cmd.Use != "validate" {
		t.Fatal("Expected validate command")
	}
	b := bytes.NewBufferString("")
	cmd.SetOut(b)
	cmd.SetErr(b)
	cmd.SetArgs([]string{"--help"})
	cmd.Execute()
}

func TestNewUpCmd(t *testing.T) {
	cmd := NewUpCmd()
	if cmd == nil || cmd.Use != "up [args...]" {
		t.Fatal("Expected up command")
	}
}

func TestNewDownCmd(t *testing.T) {
	cmd := NewDownCmd()
	if cmd == nil || cmd.Use != "down [args...]" {
		t.Fatal("Expected down command")
	}
}

func TestNewLogsCmd(t *testing.T) {
	cmd := NewLogsCmd()
	if cmd == nil || cmd.Use != "logs" {
		t.Fatal("Expected logs command")
	}
}

func TestNewInspectCmd(t *testing.T) {
	cmd := NewInspectCmd()
	if cmd == nil || cmd.Use != "inspect [container-id]" {
		t.Fatal("Expected inspect command")
	}
}
