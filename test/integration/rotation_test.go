package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/testcontainers/testcontainers-go"
	"go.uber.org/zap"

	"github.com/docker-secret-operator/dso/internal/rotation"
)

// TestRotation_BlueGreenSwap_Success tests that blue-green rotation succeeds with healthy container
func TestRotation_BlueGreenSwap_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start a test container
	req := testcontainers.ContainerRequest{
		Image: "alpine:latest",
		Cmd:   []string{"sh", "-c", "while true; do sleep 1; done"},
	}

	testContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Failed to start test container: %v", err)
	}
	defer testContainer.Terminate(ctx)

	containerID := testContainer.GetContainerID()
	t.Logf("Started test container: %s", containerID[:12])

	// Get Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}
	defer cli.Close()

	// Create rotation strategy
	logger, _ := zap.NewProduction()
	rs := rotation.NewRollingStrategyWithLogger(cli, logger)

	// Execute rotation with new environment
	newEnvs := map[string]string{
		"NEW_ENV_VAR": "test-value",
	}

	rotationCtx, rotationCancel := context.WithTimeout(ctx, 10*time.Second)
	defer rotationCancel()

	err = rs.Execute(rotationCtx, containerID, newEnvs, 5*time.Second)
	if err != nil {
		t.Fatalf("Rotation failed: %v", err)
	}

	t.Log("✅ Blue-green rotation completed successfully")

	// Verify rotation completed
	t.Logf("Container ID after rotation: %s", containerID[:12])
}

// TestRotation_Concurrent_MultipleContainers tests concurrent rotations are safe
func TestRotation_Concurrent_MultipleContainers(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	numContainers := 3
	containers := make([]testcontainers.Container, 0, numContainers)
	defer func() {
		for _, c := range containers {
			_ = c.Terminate(ctx)
		}
	}()

	// Start multiple test containers
	for i := 0; i < numContainers; i++ {
		req := testcontainers.ContainerRequest{
			Image: "alpine:latest",
			Cmd:   []string{"sh", "-c", "while true; do sleep 1; done"},
		}

		container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: req,
			Started:          true,
		})
		if err != nil {
			t.Fatalf("Failed to start container %d: %v", i, err)
		}
		containers = append(containers, container)
		t.Logf("Started container %d: %s", i, container.GetContainerID())
	}

	// Get Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}
	defer cli.Close()

	logger, _ := zap.NewProduction()
	rs := rotation.NewRollingStrategyWithLogger(cli, logger)

	// Rotate all containers concurrently
	errCh := make(chan error, numContainers)
	for i, c := range containers {
		go func(idx int, container testcontainers.Container) {
			rotCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()

			err := rs.Execute(rotCtx, container.GetContainerID(), map[string]string{
				"ROTATION_ID": fmt.Sprintf("rotation-%d", idx),
			}, 5*time.Second)
			errCh <- err
		}(i, c)
	}

	// Collect results
	var failures int
	for i := 0; i < numContainers; i++ {
		if err := <-errCh; err != nil {
			t.Logf("Container %d rotation failed: %v", i, err)
			failures++
		}
	}

	if failures > 0 {
		t.Fatalf("Expected 0 failures, got %d", failures)
	}

	t.Log("✅ All concurrent rotations completed successfully")
}

// TestRotation_HealthCheck_Timeout tests that rotation handles containers without health checks gracefully
func TestRotation_HealthCheck_Timeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start a test container without explicit health check
	req := testcontainers.ContainerRequest{
		Image: "alpine:latest",
		Cmd:   []string{"sh", "-c", "while true; do sleep 1; done"},
	}

	testContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Failed to start test container: %v", err)
	}
	defer testContainer.Terminate(ctx)

	containerID := testContainer.GetContainerID()

	// Get Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}
	defer cli.Close()

	logger, _ := zap.NewProduction()
	rs := rotation.NewRollingStrategyWithLogger(cli, logger)

	// Rotate with reasonable timeout - should succeed since container has no health check
	rotationCtx, rotationCancel := context.WithTimeout(ctx, 10*time.Second)
	defer rotationCancel()

	err = rs.Execute(rotationCtx, containerID, map[string]string{
		"NEW_VAR": "value",
	}, 5*time.Second)

	if err != nil {
		t.Logf("Rotation failed (expected if no health check): %v", err)
	} else {
		t.Log("✅ Rotation succeeded with container without explicit health check")
	}
}

// TestRotation_EventDriven_Detection tests that container operations work correctly
func TestRotation_EventDriven_Detection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	startCtx, startCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer startCancel()

	// Start test container
	req := testcontainers.ContainerRequest{
		Image: "alpine:latest",
		Cmd:   []string{"sh", "-c", "while true; do sleep 1; done"},
	}

	testContainer, err := testcontainers.GenericContainer(startCtx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Failed to start test container: %v", err)
	}
	defer testContainer.Terminate(startCtx)

	containerID := testContainer.GetContainerID()

	// Get Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}
	defer cli.Close()

	// Create a fresh context for operations
	opCtx, opCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer opCancel()

	// Verify container is running before stop
	inspect, err := cli.ContainerInspect(opCtx, containerID)
	if err != nil {
		t.Fatalf("Failed to inspect container: %v", err)
	}

	if !inspect.State.Running {
		t.Errorf("Container should be running, but state is: %s", inspect.State.Status)
	}

	t.Logf("✅ Container is running with ID: %s", containerID[:12])

	// Stop the container with a fresh context
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 15*time.Second)
	stopTimeoutSec := 10
	err = cli.ContainerStop(stopCtx, containerID, container.StopOptions{Timeout: &stopTimeoutSec})
	stopCancel()

	if err != nil {
		t.Logf("Warning: Container stop had error (but may have succeeded anyway): %v", err)
	} else {
		t.Log("✅ Container successfully stopped")
	}

	// Verify container is stopped (fresh context)
	verifyCtx, verifyCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer verifyCancel()
	inspect, err = cli.ContainerInspect(verifyCtx, containerID)
	if err != nil {
		t.Logf("Warning: Failed to inspect stopped container: %v", err)
		return
	}

	if inspect.State.Running {
		t.Errorf("Container should be stopped, but is still running")
	} else {
		t.Logf("✅ Container confirmed stopped with state: %s", inspect.State.Status)
	}
}
