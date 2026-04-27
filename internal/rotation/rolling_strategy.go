package rotation

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// RollingStrategy coordinates the zero-downtime rotation lifecycle
type RollingStrategy struct {
	cli *client.Client
}

func NewRollingStrategy(cli *client.Client) *RollingStrategy {
	return &RollingStrategy{cli: cli}
}

// Execute performs an atomic blue/green swap on a single container
func (rs *RollingStrategy) Execute(ctx context.Context, containerID string, newEnvs map[string]string, timeout time.Duration) error {
	cloner := NewContainerCloner(rs.cli)

	// Step 1: Prep Shadow config
	config, hostConfig, networkingConfig, _, err := cloner.PrepareShadowConfig(ctx, containerID, newEnvs)
	if err != nil {
		return err
	}

	// Step 2: Swap the old container to a temporary name to clear the path
	inspect, _ := rs.cli.ContainerInspect(ctx, containerID)
	originalName := inspect.Name // has the / prefix
	tempOldName := originalName + "_old_" + fmt.Sprintf("%d", time.Now().Unix())

	if err := rs.cli.ContainerRename(ctx, containerID, tempOldName); err != nil {
		return fmt.Errorf("failed to rename original container: %w", err)
	}

	// Step 3: Create and Start the New instance under the ORIGINAL name
	createResp, err := rs.cli.ContainerCreate(ctx, config, hostConfig, networkingConfig, nil, originalName)
	if err != nil {
		// ROLLBACK renaming if we can't create new
		if rbErr := rs.cli.ContainerRename(ctx, containerID, originalName); rbErr != nil {
			fmt.Printf("⚠️  [DSO WARNING] Rollback rename failed: %v\n", rbErr)
		}
		return fmt.Errorf("failed to create new container: %w", err)
	}

	if err := rs.cli.ContainerStart(ctx, createResp.ID, container.StartOptions{}); err != nil {
		// ROLLBACK
		if rbErr := rs.cli.ContainerRemove(ctx, createResp.ID, container.RemoveOptions{Force: true}); rbErr != nil {
			fmt.Printf("⚠️  [DSO WARNING] Rollback remove failed: %v\n", rbErr)
		}
		if rbErr := rs.cli.ContainerRename(ctx, containerID, originalName); rbErr != nil {
			fmt.Printf("⚠️  [DSO WARNING] Rollback rename failed: %v\n", rbErr)
		}
		return fmt.Errorf("failed to start new container: %w", err)
	}

	// Step 4: Verification Loop (Wait for health)
	fmt.Printf("⏳ [DSO ROTATION] Waiting for health verification (%v timeout)...\n", timeout)
	if err := WaitHealthy(ctx, rs.cli, createResp.ID, timeout); err != nil {
		// ROLLBACK
		fmt.Printf("❌ [DSO WARNING] New container failed health check. Rolling back...\n")
		if rbErr := rs.cli.ContainerStop(ctx, createResp.ID, container.StopOptions{}); rbErr != nil {
			fmt.Printf("⚠️  [DSO WARNING] Rollback stop failed: %v\n", rbErr)
		}
		if rbErr := rs.cli.ContainerRemove(ctx, createResp.ID, container.RemoveOptions{Force: true}); rbErr != nil {
			fmt.Printf("⚠️  [DSO WARNING] Rollback remove failed: %v\n", rbErr)
		}
		if rbErr := rs.cli.ContainerRename(ctx, containerID, originalName); rbErr != nil {
			fmt.Printf("⚠️  [DSO WARNING] Rollback rename failed: %v\n", rbErr)
		}
		return fmt.Errorf("verification failed: %w", err)
	}

	// Step 5: Success - Purge the OLD container
	fmt.Printf("✅ [DSO SUCCESS] Cutover complete. Cleaning up old instance.\n")
	if err := rs.cli.ContainerStop(ctx, containerID, container.StopOptions{}); err != nil {
		fmt.Printf("⚠️  [DSO WARNING] Failed to stop old container %s: %v\n", containerID, err)
	}
	if err := rs.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true}); err != nil {
		fmt.Printf("⚠️  [DSO WARNING] Failed to remove old container %s: %v\n", containerID, err)
	}

	return nil
}
