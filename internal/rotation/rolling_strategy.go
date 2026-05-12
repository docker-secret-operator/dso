package rotation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"go.uber.org/zap"
)

// RollingStrategy coordinates the zero-downtime rotation lifecycle with atomic guarantees
type RollingStrategy struct {
	cli    *client.Client
	logger *zap.Logger
}

func NewRollingStrategy(cli *client.Client) *RollingStrategy {
	return &RollingStrategy{
		cli:    cli,
		logger: zap.NewNop(),
	}
}

// NewRollingStrategyWithLogger creates a RollingStrategy with custom logger
func NewRollingStrategyWithLogger(cli *client.Client, logger *zap.Logger) *RollingStrategy {
	return &RollingStrategy{
		cli:    cli,
		logger: logger,
	}
}

// Execute performs an atomic blue/green swap on a single container.
// The rotation follows these atomic steps:
// 1. Prepare new container config (no state changes)
// 2. Create new container with temporary name
// 3. Start new container
// 4. Verify health with timeout
// 5. Rename old -> backup, new -> original (atomic swap point)
// 6. Cleanup backup container
// If any step fails before the swap, full rollback occurs with no state changes.
func (rs *RollingStrategy) Execute(ctx context.Context, containerID string, newEnvs map[string]string, timeout time.Duration) error {
	rs.logger.Info("Starting rotation",
		zap.String("container_id", containerID),
		zap.Duration("health_timeout", timeout))

	cloner := NewContainerCloner(rs.cli)

	// Step 1: Get the original container info
	inspect, err := rs.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		rs.logger.Error("Failed to inspect original container",
			zap.String("container_id", containerID),
			zap.Error(err))
		return fmt.Errorf("cannot inspect container before rotation: %w", err)
	}

	if inspect.Name == "" {
		rs.logger.Error("Container returned empty name",
			zap.String("container_id", containerID))
		return fmt.Errorf("container %s returned empty name, refusing rotation", containerID)
	}

	originalName := strings.TrimPrefix(inspect.Name, "/")
	tempNewName := originalName + "_dso_new_" + fmt.Sprintf("%d", time.Now().Unix())
	backupName := originalName + "_dso_backup_" + fmt.Sprintf("%d", time.Now().Unix())

	rs.logger.Info("Rotation plan",
		zap.String("original_name", originalName),
		zap.String("temp_new_name", tempNewName),
		zap.String("backup_name", backupName))

	// Step 2: Prepare new container config
	config, hostConfig, networkingConfig, _, err := cloner.PrepareShadowConfig(ctx, containerID, newEnvs)
	if err != nil {
		rs.logger.Error("Failed to prepare shadow config",
			zap.String("container_id", containerID),
			zap.Error(err))
		return fmt.Errorf("failed to prepare shadow config: %w", err)
	}

	// Step 3: Create new container with temporary name
	rs.logger.Info("Creating new container", zap.String("temp_name", tempNewName))
	createResp, err := rs.cli.ContainerCreate(ctx, config, hostConfig, networkingConfig, nil, tempNewName)
	if err != nil {
		rs.logger.Error("Failed to create new container",
			zap.String("temp_name", tempNewName),
			zap.Error(err))
		return fmt.Errorf("failed to create new container: %w", err)
	}
	newContainerID := createResp.ID

	// Step 4: Start new container
	rs.logger.Info("Starting new container", zap.String("new_container_id", newContainerID))
	if err := rs.cli.ContainerStart(ctx, newContainerID, container.StartOptions{}); err != nil {
		// Rollback: remove the new container
		rs.logger.Warn("Failed to start new container, rolling back",
			zap.String("new_container_id", newContainerID),
			zap.Error(err))
		_ = rs.cli.ContainerRemove(ctx, newContainerID, container.RemoveOptions{Force: true})
		return fmt.Errorf("failed to start new container: %w", err)
	}

	// Step 5: Verify health
	rs.logger.Info("Waiting for health verification",
		zap.String("new_container_id", newContainerID),
		zap.Duration("timeout", timeout))

	if err := WaitHealthy(ctx, rs.cli, newContainerID, timeout); err != nil {
		// Rollback: remove the unhealthy container
		rs.logger.Warn("Health check failed, rolling back",
			zap.String("new_container_id", newContainerID),
			zap.Error(err))
		_ = rs.cli.ContainerStop(ctx, newContainerID, container.StopOptions{})
		_ = rs.cli.ContainerRemove(ctx, newContainerID, container.RemoveOptions{Force: true})
		return fmt.Errorf("health verification failed: %w", err)
	}

	// ATOMIC SWAP POINT: All previous steps are reversible. After this point,
	// we're committed to the new container becoming the active one.

	// Step 6: Rename original container to backup
	rs.logger.Info("Renaming original container to backup",
		zap.String("original_id", containerID),
		zap.String("backup_name", backupName))

	if err := rs.cli.ContainerRename(ctx, containerID, backupName); err != nil {
		rs.logger.Error("FATAL: Failed to rename original to backup. Cannot proceed with swap.",
			zap.String("original_id", containerID),
			zap.String("backup_name", backupName),
			zap.Error(err))
		// At this point, the original container is still active with its original name,
		// and the new container is running under the temp name. This is a recoverable state.
		// The operator must manually fix this by removing the new container.
		_ = rs.cli.ContainerStop(ctx, newContainerID, container.StopOptions{})
		_ = rs.cli.ContainerRemove(ctx, newContainerID, container.RemoveOptions{Force: true})
		return fmt.Errorf("FATAL: Failed to rename original container to backup: %w", err)
	}

	// Step 7: Rename new container to original name
	rs.logger.Info("Renaming new container to original name",
		zap.String("new_container_id", newContainerID),
		zap.String("original_name", originalName))

	if err := rs.cli.ContainerRename(ctx, newContainerID, originalName); err != nil {
		rs.logger.Error("FATAL: Failed to rename new to original. Attempting recovery.",
			zap.String("new_container_id", newContainerID),
			zap.String("original_name", originalName),
			zap.Error(err))

		// CRITICAL FIX: Verify current state before recovery
		// The rename might have partially succeeded
		newInspect, inspectErr := rs.cli.ContainerInspect(ctx, newContainerID)
		if inspectErr == nil && newInspect.Name != "" {
			// Container still exists - check its current name
			currentName := strings.TrimPrefix(newInspect.Name, "/")
			if currentName == originalName {
				// Rename actually succeeded! Continue normally
				rs.logger.Info("Rename verification: new container successfully renamed (race condition)")
				// Fall through to success path
			} else {
				// Rename failed and new container is stuck with temp name
				// Try to restore the original container to its original name
				if err := rs.cli.ContainerRename(ctx, containerID, originalName); err != nil {
					rs.logger.Error("FATAL: Could not restore original container. State is corrupted.",
						zap.String("container_id", containerID),
						zap.String("original_name", originalName),
						zap.Error(err))
					return fmt.Errorf("FATAL: State corruption - could not complete atomic swap: %w", err)
				}
				// Restore succeeded, remove the new container
				_ = rs.cli.ContainerStop(ctx, newContainerID, container.StopOptions{})
				_ = rs.cli.ContainerRemove(ctx, newContainerID, container.RemoveOptions{Force: true})
				return fmt.Errorf("failed to finalize swap: %w", err)
			}
		} else {
			// Container disappeared - assume partial failure, try restore
			if err := rs.cli.ContainerRename(ctx, containerID, originalName); err != nil {
				rs.logger.Error("FATAL: Could not restore original container. State is corrupted.",
					zap.String("container_id", containerID),
					zap.String("original_name", originalName),
					zap.Error(err))
				return fmt.Errorf("FATAL: State corruption - could not complete atomic swap: %w", err)
			}
		}
	}

	// Success! The new container is now the active one
	rs.logger.Info("Rotation complete, new container is active",
		zap.String("original_name", originalName),
		zap.String("backup_name", backupName))

	// Step 8: Cleanup backup container (best effort)
	rs.logger.Info("Cleaning up backup container",
		zap.String("backup_name", backupName),
		zap.String("original_id", containerID))

	stopTimeout := int(timeout.Seconds())
	if err := rs.cli.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &stopTimeout}); err != nil {
		rs.logger.Warn("Failed to stop backup container",
			zap.String("backup_name", backupName),
			zap.Error(err))
	}

	if err := rs.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true}); err != nil {
		rs.logger.Warn("Failed to remove backup container",
			zap.String("backup_name", backupName),
			zap.Error(err))
	}

	rs.logger.Info("Rotation finished successfully",
		zap.String("original_name", originalName))
	return nil
}
