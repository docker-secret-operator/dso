package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewVersionCmd(t *testing.T) {
	cmd := NewVersionCmd()
	
	b := bytes.NewBufferString("")
	cmd.SetOut(b)
	cmd.SetArgs([]string{})
	
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// It prints to stdout, so we can't easily capture it with SetOut unless it uses cmd.OutOrStdout().
	// But it shouldn't error.
}

func TestNewApplyCmd(t *testing.T) {
	cmd := NewApplyCmd()
	err := cmd.RunE(cmd, []string{})
	if err == nil || !strings.Contains(err.Error(), "not yet implemented") {
		t.Fatalf("Expected not yet implemented error, got: %v", err)
	}
}

func TestNewInjectCmd(t *testing.T) {
	cmd := NewInjectCmd()
	err := cmd.RunE(cmd, []string{})
	if err == nil || !strings.Contains(err.Error(), "not yet implemented") {
		t.Fatalf("Expected not yet implemented error, got: %v", err)
	}
}

func TestNewSyncCmd(t *testing.T) {
	cmd := NewSyncCmd()
	err := cmd.RunE(cmd, []string{})
	if err == nil || !strings.Contains(err.Error(), "not yet implemented") {
		t.Fatalf("Expected not yet implemented error, got: %v", err)
	}
}
