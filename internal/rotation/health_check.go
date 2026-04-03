package rotation

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// WaitHealthy monitors the shadow instance status before cutover
func WaitHealthy(ctx context.Context, cli *client.Client, containerID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		inspect, err := cli.ContainerInspect(ctx, containerID)
		if err != nil {
			return fmt.Errorf("failed to inspect container health: %w", err)
		}

		// Check Docker native health check status if present
		if inspect.State.Health != nil {
			switch inspect.State.Health.Status {
			case "healthy":
				return nil
			case "unhealthy":
				return fmt.Errorf("container became unhealthy during rotation")
			case "starting":
				// still waiting...
			}
		} else {
			// Fallback: If no health check defined, just wait for 'running' state
			if inspect.State.Running && !inspect.State.Restarting {
				return nil
			}
		}

		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("rotation timed out after %v without health confirmation", timeout)
}

// ExecProbe runs a specific command inside the container to verify state (e.g. secret existence).
func ExecProbe(ctx context.Context, cli *client.Client, containerID string, path string, timeout time.Duration, retries int) error {
	if retries <= 0 {
		retries = 3
	}
	interval := timeout / time.Duration(retries)

	for i := 0; i < retries; i++ {
		execConfig := container.ExecOptions{
			Cmd:          []string{"test", "-s", path},
			AttachStdout: true,
			AttachStderr: true,
		}
		
		response, err := cli.ContainerExecCreate(ctx, containerID, execConfig)
		if err != nil {
			return err
		}

		err = cli.ContainerExecStart(ctx, response.ID, container.ExecStartOptions{})
		if err != nil {
			return err
		}

		// Check exit code
		inspect, err := cli.ContainerExecInspect(ctx, response.ID)
		if err != nil {
			return err
		}

		if inspect.ExitCode == 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}

	return fmt.Errorf("exec probe failed for %s after %d retries", path, retries)
}
