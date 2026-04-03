package rotation

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"path/filepath"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// StreamSecretToContainer uploads secrets into a container's tmpfs mount using an in-memory tar stream.
// It enforces 0400 permissions and prevents path traversal by using filepath.Base.
func StreamSecretToContainer(ctx context.Context, cli *client.Client, containerID string, targetPath string, data map[string]string, uid, gid int) error {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	for name, content := range data {
		// Prevent path traversal by only using the base name
		safeName := filepath.Base(name)
		
		hdr := &tar.Header{
			Name: filepath.Join(targetPath, safeName),
			Mode: 0400, // Read-only for the owner
			Size: int64(len(content)),
			Uid:  uid,
			Gid:  gid,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("failed to write tar header for %s: %w", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			return fmt.Errorf("failed to write tar content for %s: %w", name, err)
		}
	}

	if err := tw.Close(); err != nil {
		return fmt.Errorf("failed to close tar writer: %w", err)
	}

	// Copy to container at the root (the header names are absolute paths inside the container)
	err := cli.CopyToContainer(ctx, containerID, "/", bytes.NewReader(buf.Bytes()), container.CopyToContainerOptions{
		AllowOverwriteDirWithFile: true,
	})
	if err != nil {
		return fmt.Errorf("failed to copy tar to container: %w", err)
	}

	return nil
}

// InjectedSecretSync is a helper for syncing a single secret's data
func InjectedSecretSync(ctx context.Context, cli *client.Client, containerID string, targetPath string, data map[string]string, uid, gid int) error {
	return StreamSecretToContainer(ctx, cli, containerID, targetPath, data, uid, gid)
}
