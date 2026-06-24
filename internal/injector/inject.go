package injector

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// InjectFiles writes secret files into the container's live filesystem using
// docker exec with stdin delivery. This correctly targets tmpfs mounts which
// are only visible inside the container's mount namespace (not accessible via
// CopyToContainer from the host overlay layer).
func InjectFiles(ctx context.Context, cli *client.Client, containerID string, files map[string]string, uid, gid int) error {
	if len(files) == 0 {
		return nil
	}

	for fileName, content := range files {
		if err := injectOneFile(ctx, cli, containerID, fileName, content, uid, gid); err != nil {
			return err
		}
	}
	return nil
}

func injectOneFile(ctx context.Context, cli *client.Client, containerID, fileName, content string, uid, gid int) error {
	// Sanitize fileName to prevent path traversal inside the container.
	fileName = filepath.Base(fileName)
	if fileName == "" || fileName == "." || strings.ContainsRune(fileName, '/') {
		return fmt.Errorf("invalid secret file name %q: must be a plain filename with no path components", fileName)
	}

	destPath := "/run/secrets/dso/" + fileName
	cmd := buildInjectCmd(destPath, uid, gid)

	execID, err := cli.ContainerExecCreate(ctx, containerID, container.ExecOptions{
		AttachStdin:  true,
		AttachStdout: false,
		AttachStderr: false,
		Cmd:          []string{"/bin/sh", "-c", cmd},
	})
	if err != nil {
		return fmt.Errorf("exec create failed for %s: %w", fileName, err)
	}

	// Attach to exec so we can write the secret to its stdin.
	// The base64-encoded secret is delivered via the exec's stdin stream and
	// never appears in /proc/<pid>/cmdline or docker inspect output.
	resp, err := cli.ContainerExecAttach(ctx, execID.ID, container.ExecStartOptions{Detach: false})
	if err != nil {
		return fmt.Errorf("exec attach failed for %s: %w", fileName, err)
	}
	defer resp.Close()

	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	if _, err := fmt.Fprintf(resp.Conn, "%s\n", encoded); err != nil {
		return fmt.Errorf("failed to write secret to stdin for %s: %w", fileName, err)
	}
	resp.CloseWrite()

	// Poll for exec exit.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		info, err := cli.ContainerExecInspect(ctx, execID.ID)
		if err != nil {
			return fmt.Errorf("exec inspect failed for %s: %w", fileName, err)
		}
		if !info.Running {
			if info.ExitCode != 0 {
				return fmt.Errorf("secret write command exited %d for %s", info.ExitCode, fileName)
			}
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}

	return fmt.Errorf("exec timed out writing %s", fileName)
}

// buildInjectCmd constructs a shell one-liner that:
//  1. Creates the target directory (idempotent)
//  2. Reads base64-encoded secret from stdin and decodes it atomically via a temp file
//  3. Moves the temp file to the final path (atomic rename)
//  4. Sets ownership and strict read-only permissions
//
// Secret content is never embedded in the command string — it is always read
// from stdin so it does not appear in /proc/<pid>/cmdline.
func buildInjectCmd(destPath string, uid, gid int) string {
	dir := "/run/secrets/dso"
	tmp := destPath + ".tmp"

	cmd := fmt.Sprintf(
		"mkdir -p %s && base64 -d > %s && mv %s %s && chmod 0400 %s",
		dir, tmp, tmp, destPath, destPath,
	)

	if uid != 0 || gid != 0 {
		cmd += fmt.Sprintf(" && chown %d:%d %s", uid, gid, destPath)
	}

	return cmd
}
