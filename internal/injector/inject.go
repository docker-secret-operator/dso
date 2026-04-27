package injector

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// InjectFiles writes secret files into the container's live filesystem using
// docker exec. The secret value is base64-encoded and embedded directly in the
// shell command, avoiding any stdin stream framing issues. This correctly
// targets tmpfs mounts which are only visible inside the container's mount
// namespace (not accessible via CopyToContainer from the host overlay layer).
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
	destPath := "/run/secrets/dso/" + fileName

	// base64-encode the secret so it is shell-safe (alphabet: A-Za-z0-9+/=,
	// no quotes, no special chars). We embed it directly in the command
	// instead of piping via stdin, which avoids Docker exec stream framing
	// issues and works regardless of TTY mode.
	encoded := base64.StdEncoding.EncodeToString([]byte(content))

	cmd := buildInjectCmd(encoded, destPath, uid, gid)

	execID, err := cli.ContainerExecCreate(ctx, containerID, container.ExecOptions{
		AttachStdin:  false,
		AttachStdout: false,
		AttachStderr: false,
		Cmd:          []string{"/bin/sh", "-c", cmd},
	})
	if err != nil {
		return fmt.Errorf("exec create failed for %s: %w", fileName, err)
	}

	// Detach=false: ContainerExecStart blocks until the exec finishes.
	if err := cli.ContainerExecStart(ctx, execID.ID, container.ExecStartOptions{Detach: false}); err != nil {
		return fmt.Errorf("exec start failed for %s: %w", fileName, err)
	}

	// Verify exit code.
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
//  2. Decodes the base64 secret and writes it atomically via a temp file
//  3. Moves the temp file to the final path (atomic rename)
//  4. Sets ownership and strict read-only permissions
func buildInjectCmd(encodedContent, destPath string, uid, gid int) string {
	dir := "/run/secrets/dso"
	tmp := destPath + ".tmp"

	cmd := fmt.Sprintf(
		"mkdir -p %s && printf '%%s' '%s' | base64 -d > %s && mv %s %s && chmod 0400 %s",
		dir, encodedContent, tmp, tmp, destPath, destPath,
	)

	if uid != 0 || gid != 0 {
		cmd += fmt.Sprintf(" && chown %d:%d %s", uid, gid, destPath)
	}

	return cmd
}
