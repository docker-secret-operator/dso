package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// DraftService handles draft operations
type DraftService struct {
	store storage.DraftStore
}

// NewDraftService creates a new draft service
func NewDraftService(store storage.DraftStore) *DraftService {
	return &DraftService{store: store}
}

// CreateDraft creates a new draft
func (ds *DraftService) CreateDraft(ctx context.Context, workspaceID, ownerID, title, description, config string) (*storage.Draft, error) {
	if workspaceID == "" || ownerID == "" || title == "" || config == "" {
		return nil, fmt.Errorf("required fields cannot be empty")
	}

	draft := &storage.Draft{
		ID:          generateID(),
		WorkspaceID: workspaceID,
		OwnerID:     ownerID,
		Title:       title,
		Description: description,
		Config:      config,
		Status:      "draft",
		VersionNumber: 1,
		Checksum:    calculateChecksum(config),
		CreatedAt:   time.Now(),
		ModifiedAt:  time.Now(),
	}

	if err := ds.store.Create(ctx, draft); err != nil {
		return nil, err
	}

	return draft, nil
}

// UpdateDraft updates an existing draft
func (ds *DraftService) UpdateDraft(ctx context.Context, id string, title, description, config string) (*storage.Draft, error) {
	if id == "" {
		return nil, fmt.Errorf("draft ID cannot be empty")
	}

	draft, err := ds.store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if title != "" {
		draft.Title = title
	}
	if description != "" {
		draft.Description = description
	}
	if config != "" {
		draft.Config = config
		draft.VersionNumber++
		draft.Checksum = calculateChecksum(config)
	}
	draft.ModifiedAt = time.Now()

	if err := ds.store.Update(ctx, draft); err != nil {
		return nil, err
	}

	return draft, nil
}

// GetDraft retrieves a draft
func (ds *DraftService) GetDraft(ctx context.Context, id string) (*storage.Draft, error) {
	return ds.store.GetByID(ctx, id)
}

// ListDrafts retrieves all drafts for an owner
func (ds *DraftService) ListDrafts(ctx context.Context, ownerID string) ([]*storage.Draft, error) {
	return ds.store.List(ctx, ownerID)
}

// DeleteDraft soft-deletes a draft
func (ds *DraftService) DeleteDraft(ctx context.Context, id string) error {
	draft, err := ds.store.GetByID(ctx, id)
	if err != nil {
		return err
	}

	draft.Status = "archived"
	draft.ModifiedAt = time.Now()

	return ds.store.Update(ctx, draft)
}

// GetDraftVersions retrieves version history
func (ds *DraftService) GetDraftVersions(ctx context.Context, draftID string) ([]*storage.DraftVersion, error) {
	return ds.store.GetVersions(ctx, draftID)
}

// SaveVersion saves a version of a draft
func (ds *DraftService) SaveVersion(ctx context.Context, draftID, config string) (*storage.DraftVersion, error) {
	if draftID == "" || config == "" {
		return nil, fmt.Errorf("required fields cannot be empty")
	}

	draft, err := ds.store.GetByID(ctx, draftID)
	if err != nil {
		return nil, err
	}

	version := &storage.DraftVersion{
		ID:            generateID(),
		DraftID:       draftID,
		VersionNumber: draft.VersionNumber,
		Config:        config,
		Checksum:      calculateChecksum(config),
		CreatedAt:     time.Now(),
	}

	if err := ds.store.SaveVersion(ctx, version); err != nil {
		return nil, err
	}

	return version, nil
}

// calculateChecksum calculates SHA256 checksum of config
func calculateChecksum(config string) string {
	hash := sha256.Sum256([]byte(config))
	return hex.EncodeToString(hash[:])
}
