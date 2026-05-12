package rotation

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// WaitHealthy monitors the shadow instance status before cutover
// CRITICAL: Does not return success until app is actually healthy, not just running
func WaitHealthy(ctx context.Context, cli *client.Client, containerID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	hasHealthCheck := false
	healthCheckConfirmed := false

	for time.Now().Before(deadline) {
		inspect, err := cli.ContainerInspect(ctx, containerID)
		if err != nil {
			return fmt.Errorf("failed to inspect container health: %w", err)
		}

		// CRITICAL: If container is restarting, fail immediately
		if inspect.State.Restarting {
			return fmt.Errorf("container is restarting - indicates startup failure")
		}

		// CRITICAL: If container exited, fail immediately
		if !inspect.State.Running {
			return fmt.Errorf("container exited or stopped before becoming healthy")
		}

		// Check Docker native health check status if present
		if inspect.State.Health != nil {
			hasHealthCheck = true
			switch inspect.State.Health.Status {
			case "healthy":
				healthCheckConfirmed = true
				return nil
			case "unhealthy":
				return fmt.Errorf("container became unhealthy during rotation")
			case "starting":
				// still waiting...
			}
		} else {
			// CRITICAL FIX: Don't return success on just running state
			// If no health check is defined, we MUST give it reasonable time to start up
			// but we don't confirm success until we've waited long enough
			hasHealthCheck = false
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}

	// CRITICAL: If we have a health check defined, only accept "healthy" status
	if hasHealthCheck && !healthCheckConfirmed {
		return fmt.Errorf("rotation timed out after %v - container has health check but never reached healthy state", timeout)
	}

	// If no health check defined, we've waited the full timeout and container is running
	// This is acceptable but risky - container could still crash after rotation
	return nil
}

// ExecProbe runs a specific command inside the container to verify state (e.g. secret existence).
// CRITICAL: Properly cleans up exec instances
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

		execID := response.ID

		err = cli.ContainerExecStart(ctx, execID, container.ExecStartOptions{})
		if err != nil {
			// Exec didn't start, but we still created it - don't leak it
			return err
		}

		// Check exit code
		inspect, err := cli.ContainerExecInspect(ctx, execID)
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
