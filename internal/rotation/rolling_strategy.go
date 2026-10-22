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
	cli           *client.Client
	logger        *zap.Logger
	renameTimeout time.Duration

	// OnShadowCreated, if set, is called synchronously immediately after the
	// shadow container is created and BEFORE it is started. It exists so the
	// caller can suppress the proxy registration that would otherwise happen
	// when Docker emits this container's "start" event -- an unready shadow
	// must never be selected by the router. Called at most once per Execute.
	OnShadowCreated func(newContainerID string)

	// OnReady, if set, is called after the shadow has passed every readiness
	// gate (health check plus any file-secret probes) and BEFORE the atomic
	// rename swap or any change to the old container. This is where a caller
	// performs the proxy handoff. Returning an error aborts the rotation:
	// the shadow is torn down and the old container is left untouched and
	// still serving.
	OnReady func(ctx context.Context, newContainerID string) error

	// PreCleanupDelay is how long to leave the OLD container running and
	// reachable after traffic has been handed to the replacement, before
	// stopping and removing it. It exists because marking a proxy backend
	// "draining" only stops NEW connections being routed there -- connections
	// that selected it moments earlier are still dialing or mid-response, and
	// killing the container out from under them is a client-visible failure.
	//
	// Callers performing a proxy handoff should set this to the proxy's drain
	// window. Zero (the default) preserves the previous behaviour for
	// rotations with no proxy involvement, where the old container holds no
	// traffic and can be reclaimed immediately.
	PreCleanupDelay time.Duration
}

func NewRollingStrategy(cli *client.Client) *RollingStrategy {
	return &RollingStrategy{
		cli:           cli,
		logger:        zap.NewNop(),
		renameTimeout: 30 * time.Second,
	}
}

// NewRollingStrategyWithLogger creates a RollingStrategy with custom logger
func NewRollingStrategyWithLogger(cli *client.Client, logger *zap.Logger) *RollingStrategy {
	return &RollingStrategy{
		cli:           cli,
		logger:        logger,
		renameTimeout: 30 * time.Second,
	}
}

// renameWithTimeout executes a container rename with a timeout to prevent indefinite hangs
func (rs *RollingStrategy) renameWithTimeout(ctx context.Context, containerID, newName string) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, rs.renameTimeout)
	defer cancel()

	return rs.cli.ContainerRename(timeoutCtx, containerID, newName)
}

// cleanupContainerWithRetry attempts to stop and remove a container with exponential backoff retry.
// Makes 3 attempts: immediate, after 1s, after 2s. Logs at each attempt.
func (rs *RollingStrategy) cleanupContainerWithRetry(ctx context.Context, containerID, backupName string, timeout int) {
	maxAttempts := 3
	sleepDurations := []time.Duration{0, 1 * time.Second, 2 * time.Second}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Sleep before attempt if needed (skip on first attempt)
		if attempt > 1 {
			rs.logger.Info("Waiting before cleanup retry",
				zap.String("backup_name", backupName),
				zap.Int("attempt", attempt),
				zap.Duration("wait_duration", sleepDurations[attempt-1]))
			time.Sleep(sleepDurations[attempt-1])
		}

		rs.logger.Info("Attempting to stop backup container",
			zap.String("backup_name", backupName),
			zap.Int("attempt", attempt),
			zap.Int("max_attempts", maxAttempts))

		// Try to stop the container
		stopErr := rs.cli.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeout})
		if stopErr != nil {
			if attempt < maxAttempts {
				rs.logger.Warn("Failed to stop backup container, will retry",
					zap.String("backup_name", backupName),
					zap.Int("attempt", attempt),
					zap.Int("max_attempts", maxAttempts),
					zap.Error(stopErr))
				continue
			}
			rs.logger.Error("Failed to stop backup container after all retries",
				zap.String("backup_name", backupName),
				zap.Int("attempts", maxAttempts),
				zap.Error(stopErr))
		}

		rs.logger.Info("Attempting to remove backup container",
			zap.String("backup_name", backupName),
			zap.Int("attempt", attempt))

		// Try to remove the container
		removeErr := rs.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
		if removeErr != nil {
			if attempt < maxAttempts {
				rs.logger.Warn("Failed to remove backup container, will retry",
					zap.String("backup_name", backupName),
					zap.Int("attempt", attempt),
					zap.Int("max_attempts", maxAttempts),
					zap.Error(removeErr))
				continue
			}
			rs.logger.Error("Failed to remove backup container after all retries",
				zap.String("backup_name", backupName),
				zap.Int("attempts", maxAttempts),
				zap.Error(removeErr))
		}

		// If we get here, both stop and remove succeeded
		rs.logger.Info("Successfully cleaned up backup container",
			zap.String("backup_name", backupName),
			zap.Int("attempt", attempt))
		return
	}
}

// Execute performs an atomic blue/green swap on a single container.
// The rotation follows these atomic steps:
// 1. Prepare new container config (no state changes)
// 2. Create new container with temporary name
// 3. Stream file-mode secrets into the new container's filesystem
// 4. Start new container
// 5. Verify health with timeout
// 6. Verify file-mode secrets actually landed (exec probe)
// 7. Rename old -> backup, new -> original (atomic swap point)
// 8. Cleanup backup container
// If any step fails before the swap, full rollback occurs with no state changes.
//
// newEnvs must contain ONLY secrets configured for env-mode injection
// (SecretMapping.Inject.Type == "env", DSO's documented, intentionally-visible
// mode). Secrets configured for file-mode injection belong in fileSecrets
// instead -- PrepareShadowConfig merges newEnvs directly into Config.Env, so
// a file-mode secret placed there would leak into `docker inspect` output.
// See internal/watcher/controller.go's callers, which build these two maps
// by checking Inject.Type.
// managedEnvNames (SEC-P0-3) is every env var name this container's config
// maps ANY of its secrets to, independent of each secret's current inject
// type -- see the doc comment on PrepareShadowConfig for why it's required.
func (rs *RollingStrategy) Execute(ctx context.Context, containerID string, newEnvs map[string]string, fileSecrets []FileSecretSpec, managedEnvNames map[string]bool, timeout time.Duration) error {
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
	config, hostConfig, networkingConfig, _, err := cloner.PrepareShadowConfig(ctx, containerID, newEnvs, managedEnvNames)
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

	// Suppress proxy registration for this shadow before it is ever started.
	// Docker emits "start" the moment ContainerStart returns, and the
	// watcher's start handler would otherwise register it as an ACTIVE
	// backend while the application inside is still booting -- routing real
	// client traffic at a container that is not listening yet.
	if rs.OnShadowCreated != nil {
		rs.OnShadowCreated(newContainerID)
	}

	// Step 3.5: Stream file-mode secrets into the new container's filesystem
	// before it starts, so the app never starts without them present (same
	// ordering already used by the compose-recreation path in
	// internal/watcher/controller.go). CopyToContainer works on a
	// created-but-not-started container.
	for _, fs := range fileSecrets {
		if err := StreamSecretToContainer(ctx, rs.cli, newContainerID, fs.Path, fs.Data, fs.UID, fs.GID); err != nil {
			rs.logger.Error("Failed to stream file-mode secret into new container, rolling back",
				zap.String("new_container_id", newContainerID),
				zap.String("path", fs.Path),
				zap.Error(err))
			_ = rs.cli.ContainerRemove(ctx, newContainerID, container.RemoveOptions{Force: true})
			return fmt.Errorf("failed to stream file-mode secret to new container: %w", err)
		}
	}

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

	// Step 5.5: Verify file-mode secrets actually landed on disk before we
	// commit to the swap -- catches a partially-completed injection (e.g. the
	// stream succeeded but something removed/overwrote the file) rather than
	// swapping traffic onto a container missing a secret it needs.
	for _, fs := range fileSecrets {
		if err := ExecProbe(ctx, rs.cli, newContainerID, fs.Path, 15*time.Second, 3); err != nil {
			rs.logger.Warn("File-mode secret probe failed, rolling back",
				zap.String("new_container_id", newContainerID),
				zap.String("path", fs.Path),
				zap.Error(err))
			_ = rs.cli.ContainerStop(ctx, newContainerID, container.StopOptions{})
			_ = rs.cli.ContainerRemove(ctx, newContainerID, container.RemoveOptions{Force: true})
			return fmt.Errorf("file-mode secret probe failed for %s: %w", fs.Path, err)
		}
	}

	// Step 6: Proxy handoff. The shadow has now passed every readiness gate,
	// so it is finally safe for it to receive production traffic. This runs
	// BEFORE the renames and before the old container is touched, so a failed
	// handoff costs nothing: we tear down the shadow and the old container is
	// still named correctly, still running, and still the only active backend.
	if rs.OnReady != nil {
		if err := rs.OnReady(ctx, newContainerID); err != nil {
			rs.logger.Error("Proxy handoff failed, rolling back (old container keeps serving)",
				zap.String("new_container_id", newContainerID),
				zap.Error(err))
			_ = rs.cli.ContainerStop(ctx, newContainerID, container.StopOptions{})
			_ = rs.cli.ContainerRemove(ctx, newContainerID, container.RemoveOptions{Force: true})
			return fmt.Errorf("proxy handoff failed: %w", err)
		}
	}

	// ATOMIC SWAP POINT: All previous steps are reversible. After this point,
	// we use a non-cancellable context to ensure critical state transitions complete
	// even if the overall context is cancelled. The caller's context cancellation is
	// only respected before this point.

	swapCtx := context.Background()

	// Step 6: Rename original container to backup
	rs.logger.Info("Renaming original container to backup",
		zap.String("original_id", containerID),
		zap.String("backup_name", backupName))

	if err := rs.renameWithTimeout(swapCtx, containerID, backupName); err != nil {
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

	if err := rs.renameWithTimeout(swapCtx, newContainerID, originalName); err != nil {
		rs.logger.Error("FATAL: Failed to rename new to original. Attempting recovery.",
			zap.String("new_container_id", newContainerID),
			zap.String("original_name", originalName),
			zap.Error(err))

		// Check if rename actually succeeded despite error (race condition detection)
		// Explicit state verification before recovery path
		newInspect, inspectErr := rs.cli.ContainerInspect(ctx, newContainerID)
		if inspectErr == nil && newInspect.Name != "" {
			currentName := strings.TrimPrefix(newInspect.Name, "/")
			if currentName == originalName {
				// Rename succeeded despite error - skip recovery and proceed to verification
				rs.logger.Info("Rename succeeded despite API error (race condition detected)",
					zap.String("new_container_id", newContainerID),
					zap.String("original_name", originalName))
				goto verifySwap
			}
		}
		// If we reach here, rename actually failed - restore original container
		rs.logger.Info("Rename failed, restoring original container to original name",
			zap.String("container_id", containerID),
			zap.String("original_name", originalName))

		if err := rs.renameWithTimeout(swapCtx, containerID, originalName); err != nil {
			rs.logger.Error("FATAL: Could not restore original container. State is corrupted.",
				zap.String("container_id", containerID),
				zap.String("original_name", originalName),
				zap.Error(err))
			return fmt.Errorf("FATAL: State corruption - could not complete atomic swap: %w", err)
		}

		// Restore succeeded, cleanup the new container
		_ = rs.cli.ContainerStop(ctx, newContainerID, container.StopOptions{})
		_ = rs.cli.ContainerRemove(ctx, newContainerID, container.RemoveOptions{Force: true})
		return fmt.Errorf("failed to finalize swap: %w", err)
	}

verifySwap:
	// VERIFICATION: Ensure atomic swap actually completed
	// This is critical for detecting partial swap failures due to daemon crashes
	if err := rs.verifyAtomicSwap(ctx, containerID, newContainerID,
		originalName, backupName); err != nil {
		rs.logger.Error("Atomic swap verification failed - state may be corrupted",
			zap.String("original_id", containerID),
			zap.String("new_id", newContainerID),
			zap.Error(err))

		// Attempt to restore to consistent state
		// Try to rename containers back if swap failed
		if err := rs.renameWithTimeout(swapCtx, containerID, originalName); err != nil {
			rs.logger.Error("FATAL: Could not restore original name. State is corrupted.",
				zap.String("container_id", containerID),
				zap.Error(err))
			return fmt.Errorf("FATAL: State corruption - could not restore consistency: %w", err)
		}

		// If we got here, managed to restore original container to original name
		// Remove the new container that's stuck with temp/wrong name
		_ = rs.cli.ContainerStop(ctx, newContainerID, container.StopOptions{})
		_ = rs.cli.ContainerRemove(ctx, newContainerID, container.RemoveOptions{Force: true})

		return fmt.Errorf("atomic swap verification failed, rolled back to original state")
	}

	// Success! The new container is now the active one
	rs.logger.Info("Rotation complete, new container is active",
		zap.String("original_name", originalName),
		zap.String("backup_name", backupName))

	// Let connections that selected the old backend just before the handoff
	// finish against a container that is still alive. Without this the drain
	// is only nominal: the registry entry drains, but the process serving
	// those connections is killed immediately below.
	if rs.PreCleanupDelay > 0 {
		rs.logger.Info("Holding old container for connection drain before cleanup",
			zap.String("backup_name", backupName),
			zap.Duration("drain", rs.PreCleanupDelay))
		select {
		case <-time.After(rs.PreCleanupDelay):
		case <-ctx.Done():
			// Shutting down: fall through and reclaim the container anyway
			// rather than leaking it.
		}
	}

	// Step 8: Cleanup backup container with retry
	rs.logger.Info("Cleaning up backup container with retry",
		zap.String("backup_name", backupName),
		zap.String("original_id", containerID))

	stopTimeout := int(timeout.Seconds())
	rs.cleanupContainerWithRetry(ctx, containerID, backupName, stopTimeout)

	rs.logger.Info("Rotation finished successfully",
		zap.String("original_name", originalName))
	return nil
}

// verifyAtomicSwap checks that both containers have expected names after swap.
// This ensures the atomic swap actually completed despite any errors.
func (rs *RollingStrategy) verifyAtomicSwap(ctx context.Context,
	originalContainerID, newContainerID string,
	originalName, backupName string) error {

	// Verify original container renamed to backup
	originalInspect, err := rs.cli.ContainerInspect(ctx, originalContainerID)
	if err != nil {
		rs.logger.Error("Cannot verify original container after swap",
			zap.String("container_id", originalContainerID),
			zap.Error(err))
		return fmt.Errorf("cannot verify original container: %w", err)
	}

	actualBackupName := strings.TrimPrefix(originalInspect.Name, "/")
	if actualBackupName != backupName {
		rs.logger.Error("Original container has wrong name after swap",
			zap.String("expected", backupName),
			zap.String("actual", actualBackupName),
			zap.String("container_id", originalContainerID))
		return fmt.Errorf("original container name mismatch: expected %s, got %s",
			backupName, actualBackupName)
	}

	// Verify new container renamed to original name
	newInspect, err := rs.cli.ContainerInspect(ctx, newContainerID)
	if err != nil {
		rs.logger.Error("Cannot verify new container after swap",
			zap.String("container_id", newContainerID),
			zap.Error(err))
		return fmt.Errorf("cannot verify new container: %w", err)
	}

	actualNewName := strings.TrimPrefix(newInspect.Name, "/")
	if actualNewName != originalName {
		rs.logger.Error("New container has wrong name after swap",
			zap.String("expected", originalName),
			zap.String("actual", actualNewName),
			zap.String("container_id", newContainerID))
		return fmt.Errorf("new container name mismatch: expected %s, got %s",
			originalName, actualNewName)
	}

	rs.logger.Info("Atomic swap verification passed",
		zap.String("backup_name", backupName),
		zap.String("active_name", originalName))

	return nil
}
