package injector

import (
	"context"
	"github.com/docker/docker/client"
	"strings"
	"testing"
)

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

// TestInjectOneFile_ShellInjection verifies that filenames carrying shell
// metacharacters (e.g. from an attacker-controlled compose service name)
// are rejected before reaching the shell command, not just path traversal
// sequences (SEC-6 fix).
func TestInjectOneFile_ShellInjection(t *testing.T) {
	cli, _ := client.NewClientWithOpts(client.WithHost("tcp://127.0.0.1:12345"))

	dangerous := []string{
		"app; touch /run/secrets/dso/pwned #",
		"app`id`",
		"app$(id)",
		"app|id",
		"app&id",
		"app secret",
		"app\nid",
		"app'; rm -rf / #",
	}
	for _, name := range dangerous {
		err := InjectFiles(context.Background(), cli, "cid", map[string]string{name: "content"}, 0, 0)
		if err == nil {
			t.Errorf("InjectFiles(%q): expected rejection error, got nil", name)
			continue
		}
		if !strings.Contains(err.Error(), "invalid secret file name") {
			t.Errorf("InjectFiles(%q): expected filename validation error, got: %v", name, err)
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
