package injector

import (
	"context"
	"github.com/docker/docker/client"
	"testing"
)

func TestInjectOneFile_DockerErrors(t *testing.T) {
	cli, _ := client.NewClientWithOpts(client.WithHost("tcp://127.0.0.1:12345"))

	err := injectOneFile(context.Background(), cli, "cid", "test.txt", "content", 0, 0)
	if err == nil {
		t.Fatal("expected error")
	}
}
