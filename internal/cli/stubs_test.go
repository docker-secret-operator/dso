package cli

import (
	"bytes"
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
