package polling

import (
	"sync"
	"time"
)

// PollerState tracks the state of polling for a specific secret.
// It maintains thread-safe access via an embedded sync.Mutex.
type PollerState struct {
	mu             sync.Mutex
	LastChangeTime time.Time // When secret last changed
	LastPollTime   time.Time // When last polled
	ChangeCount    int       // Total changes observed
	PollCount      int       // Total polls performed
}

// GetLastChangeTime returns the last change time in a thread-safe manner.
func (ps *PollerState) GetLastChangeTime() time.Time {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.LastChangeTime
}

// SetLastChangeTime sets the last change time in a thread-safe manner.
func (ps *PollerState) SetLastChangeTime(t time.Time) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.LastChangeTime = t
}

// IncrementChangeCount increments the change count in a thread-safe manner.
func (ps *PollerState) IncrementChangeCount() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.ChangeCount++
}

// SetLastPollTime sets the last poll time in a thread-safe manner.
func (ps *PollerState) SetLastPollTime(t time.Time) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.LastPollTime = t
}

// IncrementPollCount increments the poll count in a thread-safe manner.
func (ps *PollerState) IncrementPollCount() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.PollCount++
}

// Snapshot returns a thread-safe copy of the current state.
func (ps *PollerState) Snapshot() PollerState {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return PollerState{
		LastChangeTime: ps.LastChangeTime,
		LastPollTime:   ps.LastPollTime,
		ChangeCount:    ps.ChangeCount,
		PollCount:      ps.PollCount,
	}
}
