package injector

import (
	"context"
	"github.com/docker/docker/client"
	"strings"
	"testing"
)

func TestBuildInjectCmd(t *testing.T) {
	cmd := buildInjectCmd("/run/secrets/dso/test", 1000, 2000)

	if !strings.Contains(cmd, "mkdir -p /run/secrets/dso") {
		t.Errorf("cmd missing mkdir")
	}
	if !strings.Contains(cmd, "base64 -d") {
		t.Errorf("cmd missing base64 decode")
	}
	if !strings.Contains(cmd, "chown 1000:2000 /run/secrets/dso/test") {
		t.Errorf("cmd missing chown")
	}
	if !strings.Contains(cmd, "chmod 0400") {
		t.Errorf("cmd missing chmod")
	}
	// Secret must never be embedded in the command — delivered via stdin only.
	if strings.Contains(cmd, "printf") || strings.Contains(cmd, "echo") {
		t.Errorf("cmd must not embed secret content — use stdin delivery")
	}
}

func TestBuildInjectCmdNoChown(t *testing.T) {
	cmd := buildInjectCmd("/run/secrets/dso/test", 0, 0)

	if strings.Contains(cmd, "chown") {
		t.Errorf("cmd should not contain chown when uid/gid are 0")
	}
}

func TestInjectFiles_Empty(t *testing.T) {
	err := InjectFiles(context.Background(), nil, "cid", nil, 0, 0)
	if err != nil {
		t.Fatal("Expected nil error for empty files")
	}
}

func TestInjectFiles_DockerFailFast(t *testing.T) {
	cli, _ := client.NewClientWithOpts(client.WithHost("tcp://127.0.0.1:12345"))

	files := map[string]string{
		"test1": "content",
	}

	err := InjectFiles(context.Background(), cli, "cid", files, 0, 0)
	if err == nil {
		t.Fatal("expected error due to invalid docker connection")
	}
}

// TestInjectOneFile_PathTraversal verifies that crafted filenames cannot escape
// /run/secrets/dso/ inside the container (H6 fix).
func TestInjectOneFile_PathTraversal(t *testing.T) {
	cli, _ := client.NewClientWithOpts(client.WithHost("tcp://127.0.0.1:12345"))

	dangerous := []string{
		"../etc/passwd",
		"../../root/.ssh/authorized_keys",
		"/etc/passwd",
		"subdir/secret",
		".",
		"",
	}
	for _, name := range dangerous {
		err := InjectFiles(context.Background(), cli, "cid", map[string]string{name: "content"}, 0, 0)
		if err == nil {
			t.Errorf("InjectFiles(%q): expected rejection error, got nil", name)
		}
	}
}

// TestInjectOneFile_ValidFilename confirms plain filenames are not rejected.
func TestInjectOneFile_ValidFilename(t *testing.T) {
	// A valid filename reaching Docker (which isn't running) should fail with a
	// connection error, not a filename validation error.
	cli, _ := client.NewClientWithOpts(client.WithHost("tcp://127.0.0.1:12345"))

	err := InjectFiles(context.Background(), cli, "cid", map[string]string{"my-secret.env": "val"}, 0, 0)
	if err == nil {
		t.Fatal("expected Docker connection error, got nil")
	}
	if strings.Contains(err.Error(), "invalid secret file name") {
		t.Errorf("valid filename was incorrectly rejected: %v", err)
	}
}
