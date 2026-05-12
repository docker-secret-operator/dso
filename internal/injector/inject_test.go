package injector

import (
	"context"
	"encoding/base64"
	"github.com/docker/docker/client"
	"strings"
	"testing"
)

func TestBuildInjectCmd(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("my-secret"))
	cmd := buildInjectCmd(encoded, "/run/secrets/dso/test", 1000, 2000)

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
}

func TestBuildInjectCmdNoChown(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("my-secret"))
	cmd := buildInjectCmd(encoded, "/run/secrets/dso/test", 0, 0)

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
