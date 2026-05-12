package watcher

import (
	"testing"
	"time"
)

func TestEventDebouncer_CheckAndRecord(t *testing.T) {
	window := 100 * time.Millisecond
	ed := NewEventDebouncer(window)

	// First event should be accepted
	if !ed.CheckAndRecord("event1") {
		t.Error("First event should have been accepted")
	}

	// Immediate duplicate should be rejected
	if ed.CheckAndRecord("event1") {
		t.Error("Immediate duplicate should have been rejected")
	}

	// Another event should be accepted
	if !ed.CheckAndRecord("event2") {
		t.Error("Different event should have been accepted")
	}

	// Wait for window to pass
	time.Sleep(window + 10*time.Millisecond)

	// After window, same event ID should be accepted again
	if !ed.CheckAndRecord("event1") {
		t.Error("Event after window should have been accepted")
	}
}

func TestEventDebouncer_Cleanup(t *testing.T) {
	window := 50 * time.Millisecond
	ed := NewEventDebouncer(window)
	defer ed.Stop()

	// Record an event
	fresh := ed.CheckAndRecord("cleanup-me")
	if !fresh {
		t.Error("First record should be fresh")
	}

	// Wait for cleanup (cutoff = now - window*2, cleanup runs every window*2)
	// Total wait needed: 2*window + buffer to ensure cleanup tick fires
	time.Sleep(window*2 + 100*time.Millisecond)

	// After the old entry is removed from the map, recording the same ID
	// should return true (fresh) since it's not in the map anymore
	fresh = ed.CheckAndRecord("cleanup-me")
	if !fresh {
		t.Error("After cleanup, same ID should be treated as fresh")
	}
}
