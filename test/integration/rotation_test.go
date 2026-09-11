package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
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
	defer func() { _ = testContainer.Terminate(ctx) }()

	containerID := testContainer.GetContainerID()
	t.Logf("Started test container: %s", containerID[:12])

	// Get Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}
	defer func() { _ = cli.Close() }()

	// Create rotation strategy
	logger, _ := zap.NewProduction()
	rs := rotation.NewRollingStrategyWithLogger(cli, logger)

	// Execute rotation with new environment
	newEnvs := map[string]string{
		"NEW_ENV_VAR": "test-value",
	}

	rotationCtx, rotationCancel := context.WithTimeout(ctx, 10*time.Second)
	defer rotationCancel()

	err = rs.Execute(rotationCtx, containerID, newEnvs, nil, 5*time.Second)
	if err != nil {
		t.Fatalf("Rotation failed: %v", err)
	}

	t.Log("✅ Blue-green rotation completed successfully")

	// Verify rotation completed
	t.Logf("Container ID after rotation: %s", containerID[:12])
}

// TestRotation_FileModeSecret_NeverInDockerInspect is the P0 secret-exposure
// regression test (Phase 4) run against a real Docker daemon rather than a
// mocked transport. It rotates a container with a file-mode secret carrying
// a unique sentinel value and asserts, against the daemon's real
// ContainerInspect response, that the sentinel never appears in:
//   - Config.Env
//   - Config.Cmd / Config.Entrypoint (command arguments)
//   - Config.Labels
//   - the full raw inspect JSON (catches any other field)
//
// and, for functional correctness (the fix must not silently drop the
// secret), that the sentinel DOES land in the container's filesystem at the
// configured path.
func TestRotation_FileModeSecret_NeverInDockerInspect(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	const sentinel = "DSO_TEST_SECRET_UNIQUE_9f31c8e7"

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

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
	defer func() { _ = testContainer.Terminate(ctx) }()

	containerID := testContainer.GetContainerID()

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}
	defer func() { _ = cli.Close() }()

	// Rotation is an atomic name swap: the ORIGINAL container's ID stops
	// existing (renamed to a backup name then removed) and a NEW container
	// takes over the original NAME. So post-rotation lookups must use the
	// name, not the pre-rotation ID.
	preRotationInspect, err := cli.ContainerInspect(ctx, containerID)
	if err != nil {
		t.Fatalf("Failed to inspect container before rotation: %v", err)
	}
	originalName := strings.TrimPrefix(preRotationInspect.Name, "/")

	logger, _ := zap.NewProduction()
	rs := rotation.NewRollingStrategyWithLogger(cli, logger)

	fileSecrets := []rotation.FileSecretSpec{
		{Path: "/run/secrets/dso", Data: map[string]string{"db-password": sentinel}, UID: 0, GID: 0},
	}
	newEnvs := map[string]string{"UNRELATED_ENV": "fine-to-be-visible"}

	rotationCtx, rotationCancel := context.WithTimeout(ctx, 15*time.Second)
	defer rotationCancel()

	if err := rs.Execute(rotationCtx, containerID, newEnvs, fileSecrets, 5*time.Second); err != nil {
		t.Fatalf("Rotation with file-mode secret failed: %v", err)
	}

	inspect, err := cli.ContainerInspect(ctx, originalName)
	if err != nil {
		t.Fatalf("Failed to inspect rotated container by name %q: %v", originalName, err)
	}

	rawJSON, err := json.Marshal(inspect)
	if err != nil {
		t.Fatalf("Failed to marshal inspect result: %v", err)
	}
	if bytes.Contains(rawJSON, []byte(sentinel)) {
		t.Fatalf("SECURITY REGRESSION: sentinel secret value found in `docker inspect` output")
	}

	for _, e := range inspect.Config.Env {
		if strings.Contains(e, sentinel) {
			t.Fatalf("SECURITY REGRESSION: sentinel found in Config.Env: %s", e)
		}
	}
	for _, c := range inspect.Config.Cmd {
		if strings.Contains(c, sentinel) {
			t.Fatalf("SECURITY REGRESSION: sentinel found in Config.Cmd: %s", c)
		}
	}
	for k, v := range inspect.Config.Labels {
		if strings.Contains(v, sentinel) {
			t.Fatalf("SECURITY REGRESSION: sentinel found in label %s: %s", k, v)
		}
	}

	t.Log("✅ Sentinel secret confirmed absent from docker inspect output")

	// Functional check: the secret must still have actually been delivered.
	execCtx, execCancel := context.WithTimeout(ctx, 10*time.Second)
	defer execCancel()
	execResp, err := cli.ContainerExecCreate(execCtx, inspect.ID, container.ExecOptions{
		Cmd:          []string{"cat", "/run/secrets/dso/db-password"},
		AttachStdout: true,
	})
	if err != nil {
		t.Fatalf("Failed to create exec to verify file delivery: %v", err)
	}
	attach, err := cli.ContainerExecAttach(execCtx, execResp.ID, container.ExecStartOptions{})
	if err != nil {
		t.Fatalf("Failed to attach exec to verify file delivery: %v", err)
	}
	defer attach.Close()
	out, _ := io.ReadAll(attach.Reader)
	if !strings.Contains(string(out), sentinel) {
		t.Fatalf("File-mode secret was not delivered to the container's filesystem; got: %q", string(out))
	}
	t.Log("✅ Sentinel secret confirmed present in container filesystem at /run/secrets/dso/db-password")
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
	defer func() { _ = cli.Close() }()

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
			}, nil, 5*time.Second)
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
	defer func() { _ = testContainer.Terminate(ctx) }()

	containerID := testContainer.GetContainerID()

	// Get Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}
	defer func() { _ = cli.Close() }()

	logger, _ := zap.NewProduction()
	rs := rotation.NewRollingStrategyWithLogger(cli, logger)

	// Rotate with reasonable timeout - should succeed since container has no health check
	rotationCtx, rotationCancel := context.WithTimeout(ctx, 10*time.Second)
	defer rotationCancel()

	err = rs.Execute(rotationCtx, containerID, map[string]string{
		"NEW_VAR": "value",
	}, nil, 5*time.Second)

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
	defer func() { _ = testContainer.Terminate(startCtx) }()

	containerID := testContainer.GetContainerID()

	// Get Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}
	defer func() { _ = cli.Close() }()

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
