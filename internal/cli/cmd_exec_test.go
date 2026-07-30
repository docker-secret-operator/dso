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
	_ = cmd.Execute()

	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
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
	_ = cmd.Execute()

	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	os.Stdout = oldStdout

	if !bytes.Contains(buf.Bytes(), []byte("Version: "+Version)) {
		t.Errorf("Expected version output, got: %s", buf.String())
	}
	if !bytes.Contains(buf.Bytes(), []byte("Commit: "+Commit)) {
		t.Errorf("Expected commit output, got: %s", buf.String())
	}
}

func TestUpHelp(t *testing.T) {
	oldStderr := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w

	cmd := NewRootCmd()
	cmd.SetArgs([]string{"up", "--help"})
	_ = cmd.Execute()

	_ = w.Close()
	os.Stderr = oldStderr
}

func TestDownHelp(t *testing.T) {
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"down", "--help"})
	_ = cmd.Execute()
}

func TestLogsHelp(t *testing.T) {
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"logs", "--help"})
	_ = cmd.Execute()
}

func TestSystemHelp(t *testing.T) {
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"system", "--help"})
	_ = cmd.Execute()
}
