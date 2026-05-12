package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"
)

// RotationState tracks in-flight rotations to enable recovery after crashes
type RotationState struct {
	ProviderName   string    `json:"provider_name"`
	SecretName     string    `json:"secret_name"`
	OriginalContainerID string    `json:"original_container_id"`
	NewContainerID string    `json:"new_container_id"`
	Status         string    `json:"status"` // in_progress, completed, rollback_required
	StartTime      time.Time `json:"start_time"`
	LastUpdate     time.Time `json:"last_update"`
}

// StateTracker persists rotation state to enable recovery from crashes
type StateTracker struct {
	stateDir string
	logger   *zap.Logger
	mu       sync.RWMutex
	states   map[string]*RotationState
}

// NewStateTracker creates a state tracker with persistent storage
func NewStateTracker(stateDir string, logger *zap.Logger) (*StateTracker, error) {
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create state directory: %w", err)
	}

	st := &StateTracker{
		stateDir: stateDir,
		logger:   logger,
		states:   make(map[string]*RotationState),
	}

	// Load any previously saved states
	if err := st.loadStates(); err != nil {
		logger.Warn("Failed to load previous rotation states", zap.Error(err))
	}

	return st, nil
}

// StartRotation marks a rotation as in progress
func (st *StateTracker) StartRotation(providerName, secretName, originalContainerID, newContainerID string) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	key := fmt.Sprintf("%s:%s:%s", providerName, secretName, originalContainerID)
	state := &RotationState{
		ProviderName:   providerName,
		SecretName:     secretName,
		OriginalContainerID: originalContainerID,
		NewContainerID: newContainerID,
		Status:         "in_progress",
		StartTime:      time.Now(),
		LastUpdate:     time.Now(),
	}

	st.states[key] = state
	return st.persistState()
}

// CompleteRotation marks a rotation as completed
func (st *StateTracker) CompleteRotation(providerName, secretName, originalContainerID string) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	key := fmt.Sprintf("%s:%s:%s", providerName, secretName, originalContainerID)
	if state, ok := st.states[key]; ok {
		state.Status = "completed"
		state.LastUpdate = time.Now()
		return st.persistState()
	}
	return nil
}

// MarkRollback marks a rotation as requiring rollback
func (st *StateTracker) MarkRollback(providerName, secretName, originalContainerID string) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	key := fmt.Sprintf("%s:%s:%s", providerName, secretName, originalContainerID)
	if state, ok := st.states[key]; ok {
		state.Status = "rollback_required"
		state.LastUpdate = time.Now()
		return st.persistState()
	}
	return nil
}

// GetPendingRotations returns all rotations that require recovery (in_progress > 5 minutes)
func (st *StateTracker) GetPendingRotations() []*RotationState {
	st.mu.RLock()
	defer st.mu.RUnlock()

	var pending []*RotationState
	now := time.Now()
	for _, state := range st.states {
		if state.Status == "in_progress" && now.Sub(state.LastUpdate) > 5*time.Minute {
			pending = append(pending, state)
		}
		if state.Status == "rollback_required" {
			pending = append(pending, state)
		}
	}
	return pending
}

// DeleteState removes a completed rotation from tracking
func (st *StateTracker) DeleteState(providerName, secretName, originalContainerID string) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	key := fmt.Sprintf("%s:%s:%s", providerName, secretName, originalContainerID)
	delete(st.states, key)
	return st.persistState()
}

func (st *StateTracker) persistState() error {
	stateFile := filepath.Join(st.stateDir, "rotations.json")
	data, err := json.MarshalIndent(st.states, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// Write atomically with temp file
	tmpFile := stateFile + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0600); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	if err := os.Rename(tmpFile, stateFile); err != nil {
		return fmt.Errorf("failed to rename state file: %w", err)
	}

	return nil
}

func (st *StateTracker) loadStates() error {
	stateFile := filepath.Join(st.stateDir, "rotations.json")
	data, err := os.ReadFile(stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No previous state
		}
		return err
	}

	states := make(map[string]*RotationState)
	if err := json.Unmarshal(data, &states); err != nil {
		return fmt.Errorf("failed to unmarshal state: %w", err)
	}

	st.states = states
	st.logger.Info("Loaded rotation states", zap.Int("count", len(states)))
	return nil
}

// Close gracefully closes the state tracker
func (st *StateTracker) Close() error {
	st.mu.Lock()
	defer st.mu.Unlock()
	// Final persist before closing
	return st.persistState()
}
