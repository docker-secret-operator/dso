package injector

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// InjectFiles uses the Docker CopyToContainer API to stream an in-memory tarball 
// directly into a container's filesystem. When interacting with tmpfs mounts, 
// this seamlessly bypasses the host disk.
func InjectFiles(ctx context.Context, cli *client.Client, containerID string, files map[string]string, uid, gid int) error {
	if len(files) == 0 {
		return nil
	}

	// Ensure directory exists: create "/run/secrets/dso" if missing.
	// Copying to /run handles it securely without copying to "/" directly.
	if _, err := cli.ContainerStatPath(ctx, containerID, "/run/secrets/dso"); err != nil {
		var dsoBuf bytes.Buffer
		dirTw := tar.NewWriter(&dsoBuf)
		if err := dirTw.WriteHeader(&tar.Header{
			Name:     "secrets/dso",
			Mode:     0755,
			Typeflag: tar.TypeDir,
			Uid:      uid,
			Gid:      gid,
		}); err != nil {
			return fmt.Errorf("failed to write directory tar header: %w", err)
		}
		_ = dirTw.Close()
		_ = cli.CopyToContainer(ctx, containerID, "/run", &dsoBuf, container.CopyToContainerOptions{})
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	addedCount := 0

	for fileName, value := range files {
		// Prevent Overwrite on Restart: skip injection if file already exists
		targetPath := fmt.Sprintf("/run/secrets/dso/%s", fileName)
		if _, err := cli.ContainerStatPath(ctx, containerID, targetPath); err == nil {
			continue // File already exists
		}

		addedCount++
		content := []byte(value)
		
		hdr := &tar.Header{
			Name:     fileName, // tar header uses filename only
			Mode:     0400,     // Secure read-only permissions
			Uid:      uid,
			Gid:      gid,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}

		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("failed to write tar header for %s: %w", fileName, err)
		}
		
		if _, err := tw.Write(content); err != nil {
			return fmt.Errorf("failed to write tar content for %s: %w", fileName, err)
		}
	}

	if addedCount == 0 {
		return nil // All files skipped, nothing to inject
	}

	if err := tw.Close(); err != nil {
		return fmt.Errorf("failed to close tar archive: %w", err)
	}

	// Copy specifically to /run/secrets/dso, NOT to "/"
	err := cli.CopyToContainer(ctx, containerID, "/run/secrets/dso", &buf, container.CopyToContainerOptions{})
	if err != nil {
		return fmt.Errorf("docker API copy failed: %w", err)
	}

	return nil
}
