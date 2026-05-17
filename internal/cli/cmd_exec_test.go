package cli

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestRootHelp(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := NewRootCmd()
	cmd.SetArgs([]string{"--help"})
	cmd.Execute()

	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	os.Stdout = oldStdout

	if !bytes.Contains(buf.Bytes(), []byte("Usage:")) {
		t.Error("Expected help output")
	}
}

func TestVersionOutput(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := NewRootCmd()
	cmd.SetArgs([]string{"version"})
	cmd.Execute()

	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	os.Stdout = oldStdout

	if !bytes.Contains(buf.Bytes(), []byte("v3.5.10")) {
		t.Errorf("Expected version output, got: %s", buf.String())
	}
}

func TestUpHelp(t *testing.T) {
	oldStderr := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w

	cmd := NewRootCmd()
	cmd.SetArgs([]string{"up", "--help"})
	cmd.Execute()

	w.Close()
	os.Stderr = oldStderr
}

func TestDownHelp(t *testing.T) {
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"down", "--help"})
	cmd.Execute()
}

func TestLogsHelp(t *testing.T) {
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"logs", "--help"})
	cmd.Execute()
}

func TestSystemHelp(t *testing.T) {
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"system", "--help"})
	cmd.Execute()
}
