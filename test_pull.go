package main

import (
	"context"
	"io"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

func main() {
	ctx := context.Background()
	cli, _ := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	reader, _ := cli.ImagePull(ctx, "docker.io/library/alpine:latest", image.PullOptions{})
	if reader != nil {
		io.Copy(io.Discard, reader)
		reader.Close()
	}
}
