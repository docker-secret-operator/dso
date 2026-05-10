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

func TestEventDebouncer_Janitor(t *testing.T) {
	window := 50 * time.Millisecond
	ed := NewEventDebouncer(window)

	ed.CheckAndRecord("cleanup-me")
	
	// Wait for janitor (window * 2)
	time.Sleep(window*2 + 10*time.Millisecond)
	
	// Check if it's gone from sync.Map
	_, loaded := ed.events.Load("cleanup-me")
	if loaded {
		t.Error("Janitor failed to delete event from map")
	}
}
