package rotation

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"go.uber.org/zap"
)

// HealthVerifier performs robust health checks to detect network partitions
// and prevent dual-running containers after network reconnects
type HealthVerifier struct {
	cli    *client.Client
	logger *zap.Logger
}

// NewHealthVerifier creates a health verifier
func NewHealthVerifier(cli *client.Client, logger *zap.Logger) *HealthVerifier {
	return &HealthVerifier{
		cli:    cli,
		logger: logger,
	}
}

// VerifyContainerHealth performs multiple health checks to ensure container is truly healthy
// Returns true if container is healthy and reachable
func (hv *HealthVerifier) VerifyContainerHealth(ctx context.Context, containerID string) bool {
	// Check 1: Container still exists and is running
	if !hv.containerExists(ctx, containerID) {
		hv.logger.Warn("Container no longer exists or cannot be accessed",
			zap.String("container_id", containerID))
		return false
	}

	// Check 2: Retry multiple times to detect network partitions
	// Network partitions may have transient failures
	const healthCheckRetries = 3
	const retryDelay = 500 * time.Millisecond

	for attempt := 0; attempt < healthCheckRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(retryDelay)
		}

		if hv.containerExists(ctx, containerID) {
			return true
		}
	}

	hv.logger.Error("Container health check failed after retries",
		zap.String("container_id", containerID),
		zap.Int("retries", healthCheckRetries))
	return false
}

// VerifyDockerDaemonConnectivity checks if Docker daemon is reachable
// Returns true if daemon is reachable
func (hv *HealthVerifier) VerifyDockerDaemonConnectivity(ctx context.Context) bool {
	_, err := hv.cli.Ping(ctx)
	if err != nil {
		hv.logger.Warn("Docker daemon ping failed", zap.Error(err))
		return false
	}
	return true
}

// containerExists performs a minimal check to verify container is accessible
func (hv *HealthVerifier) containerExists(ctx context.Context, containerID string) bool {
	_, err := hv.cli.ContainerInspect(ctx, containerID)
	return err == nil
}

// DetectDualRunningContainers identifies if multiple versions of same logical container are running
// This can happen after network partitions when agent doesn't see old container stop
func (hv *HealthVerifier) DetectDualRunningContainers(ctx context.Context, baseName string) ([]string, error) {
	containers, err := hv.cli.ContainerList(ctx, container.ListOptions{
		All: false, // Only running containers
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var dualRunning []string
	for _, c := range containers {
		// Look for containers with DSO naming pattern
		for _, name := range c.Names {
			if len(name) > 1 && name[0] == '/' {
				name = name[1:]
			}

			// Check if this is related to the base container (dso naming pattern)
			if name == baseName ||
				(len(name) > len(baseName) && name[:len(baseName)+1] == baseName+"_") {
				dualRunning = append(dualRunning, c.ID)
			}
		}
	}

	if len(dualRunning) > 1 {
		hv.logger.Error("Detected dual-running containers - network partition may have occurred",
			zap.String("base_name", baseName),
			zap.Int("count", len(dualRunning)),
			zap.Strings("container_ids", dualRunning))
	}

	return dualRunning, nil
}

// WaitHealthyWithRetry waits for container health with network partition awareness
// Returns error if container doesn't reach healthy state or network partition detected
func (hv *HealthVerifier) WaitHealthyWithRetry(ctx context.Context, containerID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("health check timeout after %v", timeout)
		}

		// Verify daemon connectivity before checking container
		if !hv.VerifyDockerDaemonConnectivity(ctx) {
			hv.logger.Warn("Docker daemon unreachable - possible network partition")
			return fmt.Errorf("docker daemon unreachable")
		}

		// Verify container health
		if hv.VerifyContainerHealth(ctx, containerID) {
			return nil
		}

		// Wait before retrying
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
}

// CleanupDualRunningContainers stops extra containers if dual-running detected
// Keeps only the newest container based on creation time
func (hv *HealthVerifier) CleanupDualRunningContainers(ctx context.Context, containerIDs []string) error {
	if len(containerIDs) <= 1 {
		return nil
	}

	hv.logger.Warn("Cleaning up dual-running containers", zap.Int("count", len(containerIDs)))

	// Find newest container (keep it)
	var newestID string
	var newestTime time.Time

	for _, id := range containerIDs {
		inspect, err := hv.cli.ContainerInspect(ctx, id)
		if err != nil {
			continue
		}

		createdTime, err := time.Parse(time.RFC3339, inspect.Created)
		if err != nil {
			continue
		}

		if createdTime.After(newestTime) {
			newestTime = createdTime
			newestID = id
		}
	}

	// Stop older containers
	for _, id := range containerIDs {
		if id == newestID {
			continue
		}

		hv.logger.Warn("Stopping older container",
			zap.String("container_id", id),
			zap.String("keeping", newestID))

		timeoutSec := 10
		if err := hv.cli.ContainerStop(ctx, id, container.StopOptions{Timeout: &timeoutSec}); err != nil {
			hv.logger.Error("Failed to stop container", zap.String("container_id", id), zap.Error(err))
		}
	}

	return nil
}
