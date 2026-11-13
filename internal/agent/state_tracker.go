package agent

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// RotationState tracks in-flight rotations to enable recovery after crashes
type RotationState struct {
	ProviderName string `json:"provider_name"`
	SecretName   string `json:"secret_name"`
	// OriginalContainerID is the legacy comma-joined form, still written so older
	// readers keep working. OriginalContainerIDs is the structured source of truth
	// (empty in state files written by older releases; OriginalIDs() handles both).
	OriginalContainerID  string   `json:"original_container_id"`
	OriginalContainerIDs []string `json:"original_container_ids,omitempty"`
	NewContainerID       string   `json:"new_container_id"`
	// RotationID uniquely identifies this rotation attempt; Phase is the last
	// phase DSO acknowledged ("rotation_requested", "completed", ...). Recovery
	// never trusts Phase over what Docker actually shows: a crash between a Docker
	// action and its acknowledgement leaves Phase behind reality.
	RotationID   string    `json:"rotation_id,omitempty"`
	Phase        string    `json:"phase,omitempty"`
	Status       string    `json:"status"` // in_progress, completed, rollback_required, recovered, critical_error
	StartTime    time.Time `json:"start_time"`
	LastUpdate   time.Time `json:"last_update"`
	ErrorMessage string    `json:"error_message,omitempty"` // For critical_error status
}

// OriginalIDs returns the container IDs of this rotation as a slice, from the
// structured field when present, otherwise by splitting the legacy comma-joined
// string (blank entries dropped).
func (r *RotationState) OriginalIDs() []string {
	if len(r.OriginalContainerIDs) > 0 {
		return append([]string(nil), r.OriginalContainerIDs...)
	}
	return splitContainerIDs(r.OriginalContainerID)
}

func splitContainerIDs(joined string) []string {
	var out []string
	for _, p := range strings.Split(joined, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// StateTracker persists rotation state to enable recovery from crashes
type StateTracker struct {
	stateDir string
	logger   *zap.Logger
	mu       sync.RWMutex
	states   map[string]*RotationState

	// corrupt is set when the persisted state file existed but could not be
	// parsed. The unreadable file is preserved (renamed) as evidence and recovery
	// performs NO mutations while this is set: with unknown state the only safe
	// action is to leave every workload alone and report.
	corrupt     bool
	corruptPath string
}

// Corrupt reports whether the persisted state could not be read at startup, and
// where the unreadable file was preserved.
func (st *StateTracker) Corrupt() (bool, string) {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.corrupt, st.corruptPath
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
		ProviderName:         providerName,
		SecretName:           secretName,
		OriginalContainerID:  originalContainerID,
		OriginalContainerIDs: splitContainerIDs(originalContainerID),
		NewContainerID:       newContainerID,
		RotationID:           newRotationID(),
		Phase:                "rotation_requested",
		Status:               "in_progress",
		StartTime:            time.Now(),
		LastUpdate:           time.Now(),
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
		state.Phase = "completed"
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

// GetPendingRotations returns all rotations that require recovery
func (st *StateTracker) GetPendingRotations() []*RotationState {
	st.mu.RLock()
	defer st.mu.RUnlock()

	var pending []*RotationState
	now := time.Now()
	for _, state := range st.states {
		// Include in_progress rotations older than 5 minutes (assume crashed)
		if state.Status == "in_progress" && now.Sub(state.LastUpdate) > 5*time.Minute {
			pending = append(pending, state)
		}
		// Include explicit rollback_required or critical_error states
		if state.Status == "rollback_required" || state.Status == "critical_error" {
			pending = append(pending, state)
		}
	}
	return pending
}

// GetUnfinishedRotationsAtStartup returns every rotation that never reached a
// terminal state. It must only be used right after the agent started: no rotation
// can be in flight in a process that has just begun, so an "in_progress" record
// (of any age) is by definition an interrupted one. GetPendingRotations keeps its
// five-minute heuristic for the running agent.
func (st *StateTracker) GetUnfinishedRotationsAtStartup() []*RotationState {
	st.mu.RLock()
	defer st.mu.RUnlock()
	var out []*RotationState
	for _, state := range st.states {
		switch state.Status {
		case "in_progress", "rollback_required", "critical_error":
			out = append(out, state)
		}
	}
	return out
}

// CleanupOldStates removes completed and recovered rotations older than retentionPeriod.
// This prevents state file bloat from long-running agents.
func (st *StateTracker) CleanupOldStates(retentionPeriod time.Duration) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	cutoff := time.Now().Add(-retentionPeriod)
	var removedCount int

	for key, state := range st.states {
		if state.Status == "completed" || state.Status == "recovered" {
			if state.LastUpdate.Before(cutoff) {
				delete(st.states, key)
				removedCount++
			}
		}
	}

	if removedCount > 0 {
		st.logger.Debug("Cleaned up old rotation states",
			zap.Int("removed_count", removedCount),
			zap.Duration("retention_period", retentionPeriod))
		return st.persistState()
	}

	return nil
}

// MarkRecovered marks a rotation as recovered after automatic recovery completed
func (st *StateTracker) MarkRecovered(providerName, secretName, originalContainerID string) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	key := fmt.Sprintf("%s:%s:%s", providerName, secretName, originalContainerID)
	if state, ok := st.states[key]; ok {
		state.Status = "recovered"
		state.LastUpdate = time.Now()
		return st.persistState()
	}
	return nil
}

// MarkCriticalError marks a rotation as having a critical error that requires operator intervention
func (st *StateTracker) MarkCriticalError(providerName, secretName, originalContainerID, errorMsg string) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	key := fmt.Sprintf("%s:%s:%s", providerName, secretName, originalContainerID)
	if state, ok := st.states[key]; ok {
		state.Status = "critical_error"
		state.ErrorMessage = errorMsg
		state.LastUpdate = time.Now()
		return st.persistState()
	}
	return nil
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

	// Crash-safe replace: write a temp file, fsync it, rename over the state
	// file, then fsync the directory so the rename itself is durable. This
	// protects against a process crash at any point (the state file is always
	// the old or the new complete version) and, with the fsyncs, against power
	// loss on filesystems that honour them. It does not make the Docker action
	// and its acknowledgement one atomic transaction.
	tmpFile := stateFile + ".tmp"
	f, err := os.OpenFile(tmpFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600) // #nosec G304 -- fixed name under the agent state dir
	if err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("failed to write state file: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("failed to sync state file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close state file: %w", err)
	}
	if err := os.Rename(tmpFile, stateFile); err != nil {
		return fmt.Errorf("failed to rename state file: %w", err)
	}
	if d, err := os.Open(st.stateDir); err == nil { // #nosec G304 -- agent state dir
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

func newRotationID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("rot-%d", time.Now().UnixNano())
	}
	return "rot-" + hex.EncodeToString(b)
}

func (st *StateTracker) loadStates() error {
	stateFile := filepath.Join(st.stateDir, "rotations.json")
	// #nosec G304 -- filename is a fixed literal; st.stateDir is a config-time
	// value set at agent construction, not per-request input
	data, err := os.ReadFile(stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No previous state
		}
		return err
	}

	states := make(map[string]*RotationState)
	if err := json.Unmarshal(data, &states); err != nil {
		// Keep the unreadable file as evidence and fail safe: recovery will do
		// nothing automatic while the persisted state is unknown.
		quarantined := fmt.Sprintf("%s.corrupt-%d", stateFile, time.Now().Unix())
		if rerr := os.Rename(stateFile, quarantined); rerr != nil {
			quarantined = stateFile
		}
		st.corrupt, st.corruptPath = true, quarantined
		return fmt.Errorf("failed to unmarshal state (preserved at %s; automatic recovery disabled): %w", quarantined, err)
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
