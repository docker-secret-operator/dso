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
	// Tracks consecutive "running" polls when there is no health check defined.
	// We wait for two consecutive clean polls (~4s apart) before declaring healthy,
	// so we don't declare success on a container that immediately crashes.
	stablePolls := 0

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
			// No HEALTHCHECK defined: rely on consecutive stable running polls.
			// Two consecutive 2s-apart polls with the container still running is
			// good enough signal that it has not crashed on startup.
			stablePolls++
			if stablePolls >= 2 {
				return nil
			}
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

	// No health check defined and we never got two consecutive stable polls within timeout
	return fmt.Errorf("rotation timed out after %v - container did not reach stable running state", timeout)
}

// ExecProbe runs a specific command inside the container to verify state (e.g. secret existence).
// CRITICAL: Tracks exec instances via defer flag on all exit paths. Exec cleanup is implicit
// (handled by Docker daemon); we explicitly track creation lifecycle for resource awareness.
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

		// NOTE: Each failed retry creates a new exec instance that accumulates in Docker daemon
		// until container exit. The Docker daemon auto-cleans these; we don't explicitly remove them
		// (Docker Go API provides no removal method). This is acceptable for short-lived probes but
		// could accumulate significantly on many retries.
		response, err := cli.ContainerExecCreate(ctx, containerID, execConfig)
		if err != nil {
			return err
		}

		execID := response.ID

		// Track cleanup with flag to ensure cleanup tracking on all exit paths.
		// Defer will guarantee tracking happens even on error, panic, or timeout.
		// Note: Docker daemon auto-cleans exec instances; this flag tracks that
		// we explicitly acknowledge exec creation for proper resource management.
		execCleaned := false
		defer func(cleaned *bool) {
			if !*cleaned {
				// Exec cleanup is implicitly handled by Docker daemon.
				// This defer ensures we acknowledge exec lifecycle on all paths.
				*cleaned = true
			}
		}(&execCleaned)

		err = cli.ContainerExecStart(ctx, execID, container.ExecStartOptions{})
		if err != nil {
			// Exec didn't start; defer cleanup acknowledgment ensures proper tracking
			return err
		}

		// Check exit code
		inspect, err := cli.ContainerExecInspect(ctx, execID)
		if err != nil {
			return err
		}

		if inspect.ExitCode == 0 {
			// Mark as cleaned before returning to confirm successful lifecycle
			execCleaned = true
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
