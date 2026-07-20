package polling

import (
	"sync"
	"time"
)

// CalculateInterval returns an adaptive polling interval based on time since last secret change.
// - If timeSinceChange < 2 minutes: return 5s (aggressive polling)
// - If timeSinceChange < 10 minutes: return 30s (baseline polling)
// - If timeSinceChange >= 10 minutes: return 5m (backoff polling)
func CalculateInterval(timeSinceChange time.Duration) time.Duration {
	if timeSinceChange < 2*time.Minute {
		return 5 * time.Second
	}
	if timeSinceChange < 10*time.Minute {
		return 30 * time.Second
	}
	return 5 * time.Minute
}

// SmartPoller manages adaptive polling intervals for multiple secrets.
// It tracks state for each secret and provides thread-safe access via sync.RWMutex.
type SmartPoller struct {
	mu    sync.RWMutex
	state map[string]*PollerState
}

// NewSmartPoller creates and returns a new SmartPoller instance.
func NewSmartPoller() *SmartPoller {
	return &SmartPoller{
		state: make(map[string]*PollerState),
	}
}

// GetNextInterval returns the adaptive polling interval for the given secret.
// If the secret is unknown, it defaults to 30 seconds.
// The interval is calculated based on time since the last change was recorded.
func (sp *SmartPoller) GetNextInterval(secretName string) time.Duration {
	sp.mu.RLock()
	state, exists := sp.state[secretName]
	sp.mu.RUnlock()

	// Unknown secret defaults to 30s baseline
	if !exists {
		return 30 * time.Second
	}

	// Calculate time since last change
	lastChange := state.GetLastChangeTime()
	if lastChange.IsZero() {
		// Never changed, use baseline
		return 30 * time.Second
	}

	timeSinceChange := time.Since(lastChange)
	return CalculateInterval(timeSinceChange)
}

// RecordChange updates the state when a secret change is detected.
// It sets LastChangeTime to now and increments ChangeCount.
func (sp *SmartPoller) RecordChange(secretName string) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	state, exists := sp.state[secretName]
	if !exists {
		state = &PollerState{}
		sp.state[secretName] = state
	}

	state.SetLastChangeTime(time.Now())
	state.IncrementChangeCount()
}

// RecordPoll updates the state when a poll is performed.
// It sets LastPollTime to now and increments PollCount.
func (sp *SmartPoller) RecordPoll(secretName string) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	state, exists := sp.state[secretName]
	if !exists {
		state = &PollerState{}
		sp.state[secretName] = state
	}

	state.SetLastPollTime(time.Now())
	state.IncrementPollCount()
}

// GetStats returns a copy of the current state for the given secret.
// Returns nil if the secret has not been tracked yet.
func (sp *SmartPoller) GetStats(secretName string) *PollerState {
	sp.mu.RLock()
	state, exists := sp.state[secretName]
	sp.mu.RUnlock()

	if !exists {
		return nil
	}

	// Return a snapshot to prevent external mutation
	snapshot := state.Snapshot()
	return &snapshot
}
