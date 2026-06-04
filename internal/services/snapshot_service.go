package services

import (
	"context"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// SnapshotService handles snapshot operations
type SnapshotService struct {
	store storage.SnapshotStore
}

// NewSnapshotService creates a new snapshot service
func NewSnapshotService(store storage.SnapshotStore) *SnapshotService {
	return &SnapshotService{store: store}
}

// CreateSnapshot creates a new snapshot
func (ss *SnapshotService) CreateSnapshot(ctx context.Context, draftID, source, config string) (*storage.Snapshot, error) {
	if draftID == "" || source == "" || config == "" {
		return nil, fmt.Errorf("required fields cannot be empty")
	}

	snapshot := &storage.Snapshot{
		ID:        generateID(),
		DraftID:   draftID,
		Config:    config,
		Checksum:  calculateChecksum(config),
		Source:    source,
		Verified:  false,
		Applied:   false,
		CreatedAt: time.Now(),
	}

	if err := ss.store.Create(ctx, snapshot); err != nil {
		return nil, err
	}

	return snapshot, nil
}

// CreateSnapshotWithMetadata creates a snapshot with metadata
func (ss *SnapshotService) CreateSnapshotWithMetadata(ctx context.Context, draftID, source, config, description string, tags *string) (*storage.Snapshot, error) {
	if draftID == "" || source == "" || config == "" {
		return nil, fmt.Errorf("required fields cannot be empty")
	}

	snapshot := &storage.Snapshot{
		ID:          generateID(),
		DraftID:     draftID,
		Config:      config,
		Checksum:    calculateChecksum(config),
		Source:      source,
		Description: &description,
		Tags:        tags,
		Verified:    false,
		Applied:     false,
		CreatedAt:   time.Now(),
	}

	if err := ss.store.Create(ctx, snapshot); err != nil {
		return nil, err
	}

	return snapshot, nil
}

// GetSnapshot retrieves a snapshot
func (ss *SnapshotService) GetSnapshot(ctx context.Context, id string) (*storage.Snapshot, error) {
	return ss.store.GetByID(ctx, id)
}

// ListSnapshots retrieves all snapshots for a draft
func (ss *SnapshotService) ListSnapshots(ctx context.Context, draftID string) ([]*storage.Snapshot, error) {
	return ss.store.ListForDraft(ctx, draftID)
}

// DeleteSnapshot deletes a snapshot
func (ss *SnapshotService) DeleteSnapshot(ctx context.Context, id string) error {
	return ss.store.Delete(ctx, id)
}

// Note: MarkVerified and MarkApplied require Update() method on SnapshotStore
// These will be implemented in Phase 4.0C when status update endpoints are added
