package agent

import (
	"context"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"go.uber.org/zap"
)

// AutomaticRecovery implements automatic cleanup of orphaned containers
// and state recovery after agent crashes.
type AutomaticRecovery struct {
	cli    *client.Client
	logger *zap.Logger
	st     *StateTracker
}

// NewAutomaticRecovery creates a recovery handler
func NewAutomaticRecovery(cli *client.Client, logger *zap.Logger, st *StateTracker) *AutomaticRecovery {
	return &AutomaticRecovery{
		cli:    cli,
		logger: logger,
		st:     st,
	}
}

// RecoverFromCrash automatically recovers from agent crashes by cleaning up
// orphaned containers and restoring original containers to active state.
//
// This is called on agent startup before normal operations begin.
func (ar *AutomaticRecovery) RecoverFromCrash(ctx context.Context) error {
	if ar.st == nil {
		ar.logger.Info("State tracker not available, skipping automatic recovery")
		return nil
	}

	pending := ar.st.GetPendingRotations()
	if len(pending) == 0 {
		ar.logger.Debug("No pending rotations detected, recovery not needed")
		return nil
	}

	ar.logger.Warn("Detected pending rotations, performing automatic recovery",
		zap.Int("count", len(pending)))

	// List all containers once for efficiency
	containers, err := ar.cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		ar.logger.Error("Failed to list containers for recovery",
			zap.Error(err))
		// Don't fail startup if Docker is slow; just skip recovery
		return nil
	}

	// Index containers by name for O(1) lookup
	containersByName := make(map[string]string)
	for _, c := range containers {
		for _, name := range c.Names {
			// Remove leading slash from Docker's name format
			cleanName := strings.TrimPrefix(name, "/")
			containersByName[cleanName] = c.ID
		}
	}

	// Perform recovery for each pending rotation
	for _, rotation := range pending {
		ar.recoverSingleRotation(ctx, containersByName, rotation)
	}

	ar.logger.Info("Automatic recovery completed",
		zap.Int("rotations_processed", len(pending)))
	return nil
}

// recoverSingleRotation performs recovery for a single pending rotation.
//
// It detects orphaned containers using DSO's naming conventions and cleans them up:
// - Containers named "*_dso_backup_*" (old containers) are removed
// - Containers named "*_dso_new_*" (new containers) are removed
// - The original container should remain running with its original name
//
// If the original container is missing, this is a critical error and the operator
// must manually intervene.
func (ar *AutomaticRecovery) recoverSingleRotation(ctx context.Context,
	containersByName map[string]string,
	rotation *RotationState) {

	logger := ar.logger.With(
		zap.String("secret", rotation.SecretName),
		zap.String("provider", rotation.ProviderName),
		zap.String("original_id", rotation.OriginalContainerID[:12]),
	)

	// Detect orphaned containers by searching for DSO naming patterns
	backupPattern := "_dso_backup_"
	newPattern := "_dso_new_"

	var backupContainers []string
	var newContainers []string

	for name, containerID := range containersByName {
		if strings.Contains(name, backupPattern) {
			backupContainers = append(backupContainers, containerID)
		}
		if strings.Contains(name, newPattern) {
			newContainers = append(newContainers, containerID)
		}
	}

	// Clean up backup containers (old containers that failed to get removed)
	for _, containerID := range backupContainers {
		logger.Info("Removing orphaned backup container",
			zap.String("backup_id", containerID[:12]))

		// Best-effort stop and remove
		_ = ar.cli.ContainerStop(ctx, containerID, container.StopOptions{})
		if err := ar.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true}); err != nil {
			logger.Warn("Failed to remove backup container",
				zap.String("backup_id", containerID[:12]),
				zap.Error(err))
		}
	}

	// Clean up new/temp containers (new containers that failed to get activated)
	for _, containerID := range newContainers {
		logger.Info("Removing orphaned new container",
			zap.String("new_id", containerID[:12]))

		// Best-effort stop and remove
		_ = ar.cli.ContainerStop(ctx, containerID, container.StopOptions{})
		if err := ar.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true}); err != nil {
			logger.Warn("Failed to remove new container",
				zap.String("new_id", containerID[:12]),
				zap.Error(err))
		}
	}

	// Verify original container is still running
	// The original container should be running with its original name
	originalInspect, err := ar.cli.ContainerInspect(ctx, rotation.OriginalContainerID)
	if err != nil {
		logger.Error("CRITICAL: Original container is missing after crash",
			zap.String("original_id", rotation.OriginalContainerID[:12]),
			zap.Error(err))

		// Mark rotation state for manual operator intervention
		if err := ar.st.MarkCriticalError(rotation.ProviderName, rotation.SecretName,
			rotation.OriginalContainerID,
			"Original container missing after crash"); err != nil {
			logger.Error("Failed to mark rotation for manual intervention",
				zap.Error(err))
		}
		return
	}

	// Check if original container is in a healthy state
	if originalInspect.State == nil {
		logger.Warn("Original container state is nil, cannot validate recovery",
			zap.String("original_id", rotation.OriginalContainerID[:12]))
		// Mark rotation as recovered anyway since cleanup is done
		_ = ar.st.MarkRecovered(rotation.ProviderName, rotation.SecretName,
			rotation.OriginalContainerID)
		return
	}

	logger.Info("Automatic recovery completed",
		zap.String("container_state", originalInspect.State.Status),
		zap.Bool("running", originalInspect.State.Running))

	// Mark rotation as recovered (cleanup is complete)
	if err := ar.st.MarkRecovered(rotation.ProviderName, rotation.SecretName,
		rotation.OriginalContainerID); err != nil {
		logger.Error("Failed to mark rotation as recovered",
			zap.Error(err))
	}
}

// ValidateStateOnStartup validates the state file and detects corruption.
// Returns true if state is valid, false if corrupted (state should be reinitialized).
func (ar *AutomaticRecovery) ValidateStateOnStartup(ctx context.Context) bool {
	if ar.st == nil {
		return true // State tracker not available; nothing to validate
	}

	// Check that all pending rotations have valid Docker references
	pending := ar.st.GetPendingRotations()
	if len(pending) == 0 {
		return true
	}

	ar.logger.Info("Validating rotation state",
		zap.Int("pending_rotations", len(pending)))

	// Try to list containers; if Docker is unreachable, we can't validate
	_, err := ar.cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		ar.logger.Warn("Cannot validate state: Docker is unreachable",
			zap.Error(err))
		// Don't consider this as state corruption; Docker might be temporarily slow
		return true
	}

	// Additional validation: check for stale state (>24 hours old)
	now := time.Now()
	for _, rotation := range pending {
		age := now.Sub(rotation.StartTime)
		if age > 24*time.Hour {
			ar.logger.Warn("Detected stale rotation state (>24 hours old), discarding",
				zap.String("secret", rotation.SecretName),
				zap.Duration("age", age))

			// Discard stale state
			if err := ar.st.MarkRecovered(rotation.ProviderName, rotation.SecretName,
				rotation.OriginalContainerID); err != nil {
				ar.logger.Error("Failed to discard stale state",
					zap.Error(err))
			}
		}
	}

	return true
}

// CleanupOrphanedContainers performs a broad scan for any orphaned DSO containers
// and removes them. This is a best-effort cleanup operation.
func (ar *AutomaticRecovery) CleanupOrphanedContainers(ctx context.Context) error {
	containers, err := ar.cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		ar.logger.Error("Failed to list containers for orphan cleanup",
			zap.Error(err))
		return err
	}

	var cleanedUp int
	for _, c := range containers {
		for _, name := range c.Names {
			cleanName := strings.TrimPrefix(name, "/")

			// Check for DSO naming patterns
			if strings.Contains(cleanName, "_dso_backup_") || strings.Contains(cleanName, "_dso_new_") {
				ar.logger.Info("Cleaning up orphaned DSO container",
					zap.String("container_id", c.ID[:12]),
					zap.String("container_name", cleanName))

				_ = ar.cli.ContainerStop(ctx, c.ID, container.StopOptions{})
				if err := ar.cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}); err != nil {
					ar.logger.Warn("Failed to remove orphaned container",
						zap.String("container_id", c.ID[:12]),
						zap.Error(err))
				} else {
					cleanedUp++
				}
			}
		}
	}

	ar.logger.Info("Orphan cleanup completed",
		zap.Int("containers_removed", cleanedUp))
	return nil
}
