package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/docker-secret-operator/dso/internal/rotation"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// InjectOptions holds flags for the inject command
type InjectOptions struct {
	Container string
	Secret    string
	Value     string
	Mount     string
}

var injectOpts = InjectOptions{
	Mount: "/run/secrets",
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

The secret is mounted as a file inside the container. This does NOT
persist to the vault or configuration - use configuration for persistent changes.

Examples:
  docker dso inject --container my-app --secret db_password --value "secret123"
  docker dso inject --container abc123 --secret api_key  # Prompts for value
  echo "secret123" | docker dso inject --container my-app --secret pwd --mount /etc/secrets`,
		RunE: injectCommand,
	}

	cmd.Flags().StringVar(&injectOpts.Container, "container", "",
		"Target container ID or name (required)")
	cmd.Flags().StringVar(&injectOpts.Secret, "secret", "",
		"Secret path/name (required)")
	cmd.Flags().StringVar(&injectOpts.Value, "value", "",
		"Secret value (will prompt if not provided)")
	cmd.Flags().StringVar(&injectOpts.Mount, "mount", "/run/secrets",
		"Mount path inside container")

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

	// 2. Get secret value (prompt if not provided)
	if injectOpts.Value == "" {
		fmt.Printf("Secret value for '%s': ", injectOpts.Secret)
		valueBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		if err != nil {
			return fmt.Errorf("failed to read secret value: %w", err)
		}
		fmt.Println() // newline after password input
		injectOpts.Value = string(valueBytes)

		if injectOpts.Value == "" {
			return fmt.Errorf("secret value cannot be empty")
		}
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
	defer dockerClient.Close()

	// 4. Find container
	fmt.Printf("[DSO] Locating container '%s'...\n", injectOpts.Container)
	containerID, err := findContainerID(dockerClient, injectOpts.Container)
	if err != nil {
		return fmt.Errorf("container not found: %w", err)
	}

	fmt.Printf("[DSO] ✓ Found container %s\n", containerID[:12])

	// 5. Inject secret via tar streaming
	fmt.Printf("[DSO] Injecting secret into container...\n")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	secretData := map[string]string{
		injectOpts.Secret: injectOpts.Value,
	}

	if err := rotation.StreamSecretToContainer(ctx, dockerClient, containerID,
		injectOpts.Mount, secretData, 0, 0); err != nil {
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
	fmt.Printf("\n✓ Secret '%s' injected into container %s\n", injectOpts.Secret, containerID[:12])
	fmt.Printf("  Mount point: %s/%s\n", injectOpts.Mount, injectOpts.Secret)

	return nil
}

// findContainerID finds a container by ID or name
func findContainerID(dockerClient *client.Client, containerRef string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Try to inspect the container directly (works for both ID and name)
	inspect, err := dockerClient.ContainerInspect(ctx, containerRef)
	if err == nil {
		return inspect.ID, nil
	}

	// If direct inspection fails, list containers and search by name
	containers, err := dockerClient.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to list containers: %w", err)
	}

	for _, c := range containers {
		// Check by ID prefix
		if strings.HasPrefix(c.ID, containerRef) {
			return c.ID, nil
		}

		// Check by name (names include leading slash)
		for _, name := range c.Names {
			if strings.TrimPrefix(name, "/") == containerRef {
				return c.ID, nil
			}
		}
	}

	return "", fmt.Errorf("no container found matching '%s'", containerRef)
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
