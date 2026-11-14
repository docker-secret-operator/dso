package cli

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/docker-secret-operator/dso/internal/injector"
	"github.com/docker-secret-operator/dso/internal/util"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/spf13/cobra"
)

// defaultInjectDir is the tmpfs directory DSO mounts for file secrets
// (SECURITY.md "File Injection"); it is the default --mount.
const defaultInjectDir = "/run/secrets/dso"

// injectNameRe is the file-name alphabet shared with internal/injector.
var injectNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// InjectOptions holds flags for the inject command
type InjectOptions struct {
	Container string
	Secret    string
	Value     string
	// ValueStdin reads the value from stdin instead of --value.
	ValueStdin  bool
	TrimNewline bool
	Mount       string
}

var injectOpts = InjectOptions{
	Mount: defaultInjectDir,
}

// NewInjectCmd creates the inject command
func NewInjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inject",
		Short: "Inject secrets directly into a running container",
		Long: `Inject a secret into a running container without persisting to vault.

This is a one-time injection useful for:
- Testing injection logic
- Debugging application behavior
- Ad-hoc secret updates
- Emergency secret rotation

The secret is written as a file (mode 0400, owner root) into a directory that
must ALREADY be a tmpfs mount of the RUNNING target container; otherwise the
command refuses and writes nothing, so plaintext never reaches the container's
writable layer or a volume. Delivery uses 'docker exec' (the image needs sh,
mktemp, base64 and sha256sum) and an atomic rename. This does NOT persist to the
vault or configuration - use configuration for persistent changes.

This is an administrative command: it acts on whichever container you name
through the Docker endpoint in your environment (DOCKER_HOST, default
/var/run/docker.sock); it does not require the container to be DSO-managed.

Providing the value (safest first):
  - Hidden prompt (default on a terminal).
  - --value-stdin, or piping into the command: the exact bytes are injected;
    a trailing newline is kept unless --trim-newline is given.
  - --value: UNSAFE for real secrets. Command-line arguments are visible in
    shell history, process listings (ps, /proc), CI logs and audit/diagnostic
    tooling. Use it only for throwaway test values.

Examples (synthetic values):
  docker dso inject --container abc123 --secret api_key              # hidden prompt
  printf %s "$NEW_VALUE" | docker dso inject --container my-app --secret pwd --value-stdin
  docker dso inject --container my-app --secret tls_key --value-stdin < key.pem
  docker dso inject --container my-app --secret scratch --value "test-only-value"  # unsafe`,
		RunE: injectCommand,
	}

	cmd.Flags().StringVar(&injectOpts.Container, "container", "",
		"Target container ID or name (required)")
	cmd.Flags().StringVar(&injectOpts.Secret, "secret", "",
		"Secret path/name (required)")
	cmd.Flags().StringVar(&injectOpts.Value, "value", "",
		"UNSAFE for real secrets: visible in shell history, process lists and CI logs. Prefer --value-stdin or the prompt")
	cmd.Flags().BoolVar(&injectOpts.ValueStdin, "value-stdin", false,
		"Read the secret value from stdin (exact bytes, nothing trimmed)")
	cmd.Flags().BoolVar(&injectOpts.TrimNewline, "trim-newline", false,
		"Remove one trailing newline from stdin input (nothing else is trimmed)")
	cmd.Flags().StringVar(&injectOpts.Mount, "mount", defaultInjectDir,
		"Directory inside the container; must be an existing tmpfs mount")

	return cmd
}

// injectCommand is the main inject command handler
func injectCommand(cmd *cobra.Command, args []string) error {
	// 1. Validate required flags
	if injectOpts.Container == "" {
		return fmt.Errorf("--container is required")
	}
	if injectOpts.Secret == "" {
		return fmt.Errorf("--secret is required")
	}

	if err := validateInjectDestination(injectOpts.Mount, injectOpts.Secret); err != nil {
		return err
	}

	// 2. Get secret value: --value (unsafe), else stdin/prompt with the same
	// exact-byte semantics as `secret set`.
	if cmd.Flags().Changed("value") {
		if injectOpts.ValueStdin {
			return fmt.Errorf("--value and --value-stdin are mutually exclusive")
		}
		if injectOpts.Value == "" {
			return fmt.Errorf("secret value cannot be empty")
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "warning: --value puts the secret in the command line (shell history, process list, CI logs); prefer --value-stdin or the prompt")
	} else {
		v, err := readSecretValue(cmd, injectOpts.Secret, injectOpts.TrimNewline)
		if err != nil {
			return err
		}
		injectOpts.Value = v
	}

	// 3. Connect to Docker
	fmt.Printf("[DSO] Connecting to Docker...\n")
	dockerClient, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	defer func() { _ = dockerClient.Close() }()

	// 4. Resolve the target ONCE and pin its full ID for every later call.
	fmt.Printf("[DSO] Locating container '%s'...\n", injectOpts.Container)
	inspect, err := inspectInjectTarget(dockerClient, injectOpts.Container)
	if err != nil {
		return fmt.Errorf("container not found: %w", err)
	}
	containerID := inspect.ID
	if err := checkInjectTarget(inspect, injectOpts.Mount); err != nil {
		return fmt.Errorf("refusing to inject into container %s: %w", util.ShortID(containerID), err)
	}

	fmt.Printf("[DSO] ✓ Found container %s\n", util.ShortID(containerID))

	// 5. Inject through the hardened exec injector (tmpfs required, atomic
	// rename, 0400, no plaintext in argv). The archive endpoint is NOT used: the
	// daemon never reaches tmpfs mounts through it.
	fmt.Printf("[DSO] Injecting secret into container...\n")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	secretData := map[string]string{
		injectOpts.Secret: injectOpts.Value,
	}

	if err := injector.InjectFilesWithOptions(ctx, dockerClient, containerID, secretData, 0, 0,
		injector.Options{Dir: injectOpts.Mount, RequireTmpfs: true}); err != nil {
		return fmt.Errorf("injection failed: %w", err)
	}

	fmt.Printf("[DSO] ✓ Secret injected successfully\n")

	// 6. Verify injection (optional - check if file exists in container)
	fmt.Printf("[DSO] Verifying secret in container...\n")
	if err := verifySecretInjection(ctx, dockerClient, containerID, injectOpts.Mount, injectOpts.Secret); err != nil {
		fmt.Printf("[DSO] ⚠ Verification failed (non-fatal): %v\n", err)
		// Don't fail completely - injection may have worked even if verification fails
	} else {
		fmt.Printf("[DSO] ✓ Verification successful\n")
	}

	// 7. Display success message
	fmt.Printf("\n✓ Secret '%s' injected into container %s\n", injectOpts.Secret, util.ShortID(containerID))
	fmt.Printf("  Mount point: %s\n", path.Join(injectOpts.Mount, injectOpts.Secret))

	return nil
}

// validateInjectDestination rejects an unsafe directory or file name before any
// Docker call. The name must be a plain file name: nothing is silently reduced
// to its base name.
func validateInjectDestination(dir, name string) error {
	if !path.IsAbs(dir) || path.Clean(dir) != dir || dir == "/" || strings.ContainsRune(dir, 0) {
		return fmt.Errorf("invalid --mount %q: must be an absolute, clean, non-root directory", dir)
	}
	if name == "." || name == ".." || !injectNameRe.MatchString(name) {
		return fmt.Errorf("invalid --secret %q: use a plain file name (letters, digits, '.', '_', '-'; no path separators)", name)
	}
	return nil
}

// inspectInjectTarget resolves the container with a single inspect (Docker
// accepts a name, a full ID or a UNIQUE ID prefix; an ambiguous prefix is an
// error). There is no list-and-guess fallback: a reference that does not resolve
// to exactly one container never selects "the first match".
func inspectInjectTarget(dockerClient *client.Client, containerRef string) (container.InspectResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	inspect, err := dockerClient.ContainerInspect(ctx, containerRef)
	if err != nil {
		return container.InspectResponse{}, fmt.Errorf("cannot resolve '%s' to exactly one container: %w", containerRef, err)
	}
	if inspect.ContainerJSONBase == nil || inspect.ID == "" {
		return container.InspectResponse{}, fmt.Errorf("daemon returned no identity for '%s'", containerRef)
	}
	return inspect, nil
}

// checkInjectTarget refuses unless the container is running and dir lies inside
// a tmpfs mount declared in its configuration. The deepest declared mount that
// covers dir decides (a bind or volume nested under a tmpfs wins and refuses).
// The in-container /proc/mounts check in the injector remains the authority;
// this gate fails early and never lets a stopped container or an undeclared
// directory reach the exec.
func checkInjectTarget(in container.InspectResponse, dir string) error {
	if in.ContainerJSONBase == nil || in.State == nil {
		return fmt.Errorf("container state is unavailable")
	}
	if !in.State.Running || in.State.Paused || in.State.Restarting {
		return fmt.Errorf("container is not running (status %q); secrets are delivered only to a running container's tmpfs, never to the writable layer of a stopped one", in.State.Status)
	}
	best, bestTmpfs := -1, false
	consider := func(dest string, isTmpfs bool) {
		dest = path.Clean(dest)
		if dest == dir || dest == "/" || strings.HasPrefix(dir, dest+"/") {
			if len(dest) > best {
				best, bestTmpfs = len(dest), isTmpfs
			}
		}
	}
	if in.HostConfig != nil {
		for dest := range in.HostConfig.Tmpfs {
			consider(dest, true)
		}
	}
	for _, m := range in.Mounts {
		consider(m.Destination, m.Type == mount.TypeTmpfs)
	}
	if !bestTmpfs {
		return fmt.Errorf("%s is not inside a tmpfs mount of this container (found: %s); refusing to write plaintext to a persistent filesystem. Mount a tmpfs there (for example tmpfs: [%s] in the service definition) and recreate the container", dir, describeInjectCover(best), defaultInjectDir)
	}
	return nil
}

func describeInjectCover(best int) string {
	if best < 0 {
		return "no declared mount, so the container's writable layer"
	}
	return "a non-tmpfs mount"
}

// verifySecretInjection checks if the secret file exists in the container with retry logic
func verifySecretInjection(ctx context.Context, dockerClient *client.Client,
	containerID, mountPath, secretName string) error {

	maxRetries := 3
	retryDelay := time.Duration(100) * time.Millisecond

	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Create timeout context for verification (5 second timeout per attempt)
		verifyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)

		err := checkSecretFileExists(verifyCtx, dockerClient, containerID, mountPath, secretName)
		cancel()

		if err == nil {
			return nil // Success
		}

		if attempt < maxRetries {
			fmt.Printf("[DSO] Verification attempt %d failed: %v, retrying in %v\n", attempt, err, retryDelay)
			time.Sleep(retryDelay)
			retryDelay *= 2 // Exponential backoff
		} else {
			return fmt.Errorf("verification failed after %d attempts: %w", maxRetries, err)
		}
	}

	return nil
}

// checkSecretFileExists verifies file exists in container using exec
func checkSecretFileExists(ctx context.Context, dockerClient *client.Client,
	containerID, mountPath, secretName string) error {

	// Construct path to check
	filePath := strings.TrimRight(mountPath, "/") + "/" + secretName

	// Validate container is still running before attempting exec
	inspect, err := dockerClient.ContainerInspect(ctx, containerID)
	if err != nil {
		return fmt.Errorf("container not found or inaccessible: %w", err)
	}

	if !inspect.State.Running {
		return fmt.Errorf("container is not running (state: %s)", inspect.State.Status)
	}

	// Use 'test -f' to check file existence (exit code 0 = exists, non-0 = doesn't exist)
	// This is more reliable than 'ls' and works across different container types
	resp, err := dockerClient.ContainerExecCreate(ctx, containerID, container.ExecOptions{
		Cmd:          []string{"test", "-f", filePath},
		AttachStderr: true,
		AttachStdout: true,
	})
	if err != nil {
		return fmt.Errorf("failed to create exec: %w", err)
	}

	execID := resp.ID

	// Run the exec command
	resp2, err := dockerClient.ContainerExecAttach(ctx, execID, container.ExecAttachOptions{})
	if err != nil {
		return fmt.Errorf("failed to execute verification: %w", err)
	}
	defer resp2.Close()

	// Get exit code
	inspectResp, err := dockerClient.ContainerExecInspect(ctx, execID)
	if err != nil {
		return fmt.Errorf("failed to get exec result: %w", err)
	}

	if inspectResp.ExitCode != 0 {
		return fmt.Errorf("secret file not found at %s (exit code: %d)", filePath, inspectResp.ExitCode)
	}

	return nil
}
