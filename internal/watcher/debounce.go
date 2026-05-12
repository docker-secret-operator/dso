package watcher

import (
	"sync"
	"time"
)

// EventRecord tracks when an event was last seen
type EventRecord struct {
	lastSeen time.Time
}

// EventDebouncer tracks recently processed docker events to ignore rapid duplicates.
// Uses a type-safe map instead of sync.Map to eliminate unsafe type assertions.
type EventDebouncer struct {
	mu     sync.RWMutex
	events map[string]EventRecord
	window time.Duration
	stopCh chan struct{}
}

func NewEventDebouncer(window time.Duration) *EventDebouncer {
	ed := &EventDebouncer{
		events: make(map[string]EventRecord),
		window: window,
		stopCh: make(chan struct{}),
	}
	go ed.cleanupLoop()
	return ed
}

// Stop terminates the background cleanup goroutine.
func (ed *EventDebouncer) Stop() {
	close(ed.stopCh)
}

// CheckAndRecord returns true if the event has NOT been seen in the window (i.e. is fresh).
// Duplicate events within the window do not extend the window.
func (ed *EventDebouncer) CheckAndRecord(eventID string) bool {
	ed.mu.Lock()
	defer ed.mu.Unlock()

	now := time.Now()

	if record, exists := ed.events[eventID]; exists {
		if now.Sub(record.lastSeen) < ed.window {
			// Still within debounce window — duplicate detected
			// Do NOT update the timestamp; keep the original window
			return false
		}
		// Outside window — treat as fresh event
		ed.events[eventID] = EventRecord{lastSeen: now}
		return true
	}

	// New event
	ed.events[eventID] = EventRecord{lastSeen: now}
	return true
}

// cleanupLoop periodically removes old event records from the map.
// Type-safe implementation prevents panics.
func (ed *EventDebouncer) cleanupLoop() {
	ticker := time.NewTicker(ed.window * 2)
	defer ticker.Stop()

	for {
		select {
		case <-ed.stopCh:
			return
		case <-ticker.C:
			ed.mu.Lock()
			cutoff := time.Now().Add(-ed.window * 2)
			for eventID, record := range ed.events {
				if record.lastSeen.Before(cutoff) {
					delete(ed.events, eventID)
				}
			}
			ed.mu.Unlock()
		}
	}
}
