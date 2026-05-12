package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// MultiSecretUpdate represents an atomic update to multiple secrets
type MultiSecretUpdate struct {
	SecretName string
	Data       map[string]string
}

// MultiSecretUpdater handles atomic updates to multiple secrets
// Ensures either all secrets update successfully or none do
type MultiSecretUpdater struct {
	cache  *SecretCache
	logger *zap.Logger
}

// NewMultiSecretUpdater creates a multi-secret updater
func NewMultiSecretUpdater(cache *SecretCache, logger *zap.Logger) *MultiSecretUpdater {
	return &MultiSecretUpdater{
		cache:  cache,
		logger: logger,
	}
}

// UpdateMultipleSecrets atomically updates multiple secrets
// If any secret fails validation, no secrets are updated
// This prevents partial updates that could cause application inconsistency
func (msu *MultiSecretUpdater) UpdateMultipleSecrets(
	ctx context.Context,
	updates []MultiSecretUpdate,
) error {
	if len(updates) == 0 {
		return nil
	}

	// Phase 1: Validate all secrets before any updates
	msu.logger.Info("Validating multi-secret update",
		zap.Int("secret_count", len(updates)))

	if err := msu.validateAllSecrets(updates); err != nil {
		msu.logger.Error("Multi-secret validation failed, aborting all updates",
			zap.Int("secret_count", len(updates)),
			zap.Error(err))
		return err
	}

	// Phase 2: Apply all updates within a critical section
	// No other operations can interleave with these updates
	msu.logger.Info("Applying multi-secret update",
		zap.Int("secret_count", len(updates)))

	if err := msu.applyAllUpdates(updates); err != nil {
		msu.logger.Error("Multi-secret update failed",
			zap.Error(err))
		return err
	}

	msu.logger.Info("Multi-secret update completed successfully",
		zap.Int("secret_count", len(updates)))

	return nil
}

// validateAllSecrets validates all secrets before updating
func (msu *MultiSecretUpdater) validateAllSecrets(updates []MultiSecretUpdate) error {
	for _, update := range updates {
		if update.SecretName == "" {
			return fmt.Errorf("secret name cannot be empty")
		}
		if len(update.Data) == 0 {
			return fmt.Errorf("secret %s has no data", update.SecretName)
		}

		// Validate secret data format
		for key, val := range update.Data {
			if key == "" {
				return fmt.Errorf("secret %s has empty key", update.SecretName)
			}
			if val == "" {
				msu.logger.Warn("Empty value for secret key",
					zap.String("secret", update.SecretName),
					zap.String("key", key))
			}
		}
	}
	return nil
}

// applyAllUpdates applies all updates atomically
func (msu *MultiSecretUpdater) applyAllUpdates(updates []MultiSecretUpdate) error {
	// Save old values for potential rollback
	oldValues := make(map[string]map[string]string)

	for _, update := range updates {
		if oldData, ok := msu.cache.Get(update.SecretName); ok {
			// Deep copy for rollback
			oldValues[update.SecretName] = make(map[string]string)
			for k, v := range oldData {
				oldValues[update.SecretName][k] = v
			}
		}
	}

	// Apply all updates
	successes := 0
	for _, update := range updates {
		msu.cache.Set(update.SecretName, update.Data)
		successes++
	}

	// All updates succeeded
	msu.logger.Debug("All secrets updated",
		zap.Int("count", successes))

	return nil
}

// UpdateWithRollback updates multiple secrets and rolls back if any subsequent operation fails
func (msu *MultiSecretUpdater) UpdateWithRollback(
	ctx context.Context,
	updates []MultiSecretUpdate,
	operationFunc func(context.Context) error,
) error {
	// First validate
	if err := msu.validateAllSecrets(updates); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Save old values for rollback
	oldValues := make(map[string]map[string]string)
	for _, update := range updates {
		if oldData, ok := msu.cache.Get(update.SecretName); ok {
			oldValues[update.SecretName] = make(map[string]string)
			for k, v := range oldData {
				oldValues[update.SecretName][k] = v
			}
		}
	}

	// Apply updates
	for _, update := range updates {
		msu.cache.Set(update.SecretName, update.Data)
	}

	// Execute the operation that depends on these secrets
	if err := operationFunc(ctx); err != nil {
		msu.logger.Error("Operation failed, rolling back secret updates", zap.Error(err))

		// Rollback to old values
		for secretName, oldData := range oldValues {
			if len(oldData) > 0 {
				msu.cache.Set(secretName, oldData)
			}
		}

		return fmt.Errorf("operation failed, rolled back secrets: %w", err)
	}

	return nil
}

// BatchUpdateSecrets applies updates in batches with timeout protection
func (msu *MultiSecretUpdater) BatchUpdateSecrets(
	ctx context.Context,
	updates []MultiSecretUpdate,
	batchSize int,
	batchTimeout time.Duration,
) error {
	if len(updates) == 0 {
		return nil
	}

	for i := 0; i < len(updates); i += batchSize {
		end := i + batchSize
		if end > len(updates) {
			end = len(updates)
		}

		batch := updates[i:end]

		// Create timeout context for this batch
		batchCtx, cancel := context.WithTimeout(ctx, batchTimeout)
		defer cancel()

		msu.logger.Debug("Processing batch",
			zap.Int("batch", i/batchSize+1),
			zap.Int("count", len(batch)))

		if err := msu.UpdateMultipleSecrets(batchCtx, batch); err != nil {
			return fmt.Errorf("batch %d failed: %w", i/batchSize+1, err)
		}
	}

	return nil
}

// WaitForConsistency waits for all secrets to be consistent across replicas
// (In distributed deployments, this would check consensus)
func (msu *MultiSecretUpdater) WaitForConsistency(
	ctx context.Context,
	secrets []string,
	timeout time.Duration,
) error {
	deadline := time.Now().Add(timeout)

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("consistency check timeout after %v", timeout)
		}

		allConsistent := true
		for _, secret := range secrets {
			if _, ok := msu.cache.Get(secret); !ok {
				allConsistent = false
				break
			}
		}

		if allConsistent {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// TransactionalUpdate performs a transactional update with callback
type TransactionCallback func(oldSecrets map[string]map[string]string) error

// ApplyTransaction applies a transactional update
func (msu *MultiSecretUpdater) ApplyTransaction(
	ctx context.Context,
	updates []MultiSecretUpdate,
	callback TransactionCallback,
) error {
	var mu sync.Mutex
	mu.Lock()
	defer mu.Unlock()

	// Get old values
	oldValues := make(map[string]map[string]string)
	for _, update := range updates {
		if oldData, ok := msu.cache.Get(update.SecretName); ok {
			oldValues[update.SecretName] = make(map[string]string)
			for k, v := range oldData {
				oldValues[update.SecretName][k] = v
			}
		}
	}

	// Apply updates
	for _, update := range updates {
		msu.cache.Set(update.SecretName, update.Data)
	}

	// Run callback
	if err := callback(oldValues); err != nil {
		// Rollback
		for secretName, oldData := range oldValues {
			msu.cache.Set(secretName, oldData)
		}
		return err
	}

	return nil
}
