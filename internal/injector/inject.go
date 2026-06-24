package injector

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// InjectFiles writes secret files into the container's live filesystem using
// docker exec. The secret value is base64-encoded and passed as an exec
// environment variable (_DSO_SECRET) rather than embedded in the shell command,
// so it never appears in the process's argv or /proc/pid/cmdline. This targets
// tmpfs mounts which are only visible inside the container's mount namespace
// (not accessible via CopyToContainer from the host overlay layer).
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
	destPath := "/run/secrets/dso/" + filepath.Base(fileName)

	// base64-encode the secret so it is shell-safe (alphabet: A-Za-z0-9+/=).
	// The encoded value is passed as an exec environment variable (_DSO_SECRET)
	// rather than embedded in the command string, keeping it out of the process
	// argv and /proc/pid/cmdline for the duration of the exec.
	encoded := base64.StdEncoding.EncodeToString([]byte(content))

	cmd := buildInjectCmd(destPath, uid, gid)

	execID, err := cli.ContainerExecCreate(ctx, containerID, container.ExecOptions{
		AttachStdin:  false,
		AttachStdout: false,
		AttachStderr: false,
		// Secret is in the environment, not the command line.
		Env: []string{"_DSO_SECRET=" + encoded},
		Cmd: []string{"/bin/sh", "-c", cmd},
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
//  2. Reads the base64 secret from $_DSO_SECRET (env, not argv)
//  3. Decodes and writes it atomically via a temp file
//  4. Moves the temp file to the final path (atomic rename)
//  5. Sets ownership and strict read-only permissions
//
// The secret value is consumed from the environment variable _DSO_SECRET, which
// is set by the caller via ExecOptions.Env. This keeps the plaintext (and its
// base64 encoding) out of /proc/pid/cmdline for the lifetime of the exec.
func buildInjectCmd(destPath string, uid, gid int) string {
	dir := "/run/secrets/dso"
	tmp := destPath + ".tmp"

	cmd := fmt.Sprintf(
		"mkdir -p %s && printf '%%s' \"$_DSO_SECRET\" | base64 -d > %s && mv %s %s && chmod 0400 %s",
		dir, tmp, tmp, destPath, destPath,
	)

	if uid != 0 || gid != 0 {
		cmd += fmt.Sprintf(" && chown %d:%d %s", uid, gid, destPath)
	}

	return cmd
}
