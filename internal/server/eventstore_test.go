package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeEventLines(t *testing.T, path string, lines []string) {
	t.Helper()
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write fixture file: %v", err)
	}
}

func marshalEvent(t *testing.T, ev Event) string {
	t.Helper()
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("failed to marshal fixture event: %v", err)
	}
	return string(b)
}

// Test 1 — missing file: EventStore starts successfully with no history.
func TestEventStore_MissingFile_StartsEmpty(t *testing.T) {
	dir := t.TempDir()
	store := newEventStoreAt(50, nil, dir, "events.jsonl")

	got := store.GetLast(0, "")
	if len(got) != 0 {
		t.Fatalf("expected 0 events from a missing file, got %d", len(got))
	}
}

// Test 2 — empty file: EventStore starts with zero history.
func TestEventStore_EmptyFile_StartsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	writeEventLines(t, path, nil)

	store := newEventStoreAt(50, nil, dir, "events.jsonl")

	got := store.GetLast(0, "")
	if len(got) != 0 {
		t.Fatalf("expected 0 events from an empty file, got %d", len(got))
	}
}

// Test 3 — existing events: write several events, create a new EventStore,
// verify they are restored (a real restart-persistence scenario).
func TestEventStore_ExistingEvents_AreRestored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	e1 := Event{"timestamp": "2026-01-01T00:00:00Z", "secret": "a", "event_type": "rotation_succeeded", "status": "success"}
	e2 := Event{"timestamp": "2026-01-01T00:01:00Z", "secret": "b", "event_type": "rotation_failed", "status": "failure", "error": "timeout"}
	writeEventLines(t, path, []string{marshalEvent(t, e1), marshalEvent(t, e2)})

	store := newEventStoreAt(50, nil, dir, "events.jsonl")

	got := store.GetLast(0, "")
	if len(got) != 2 {
		t.Fatalf("expected 2 restored events, got %d: %+v", len(got), got)
	}
	if got[0]["secret"] != "a" || got[1]["secret"] != "b" {
		t.Fatalf("restored events out of order or wrong content: %+v", got)
	}
}

// Test 4 — malformed line: one malformed JSON line must not destroy all
// valid historical records.
func TestEventStore_MalformedLine_DoesNotDiscardValidHistory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	e1 := Event{"timestamp": "2026-01-01T00:00:00Z", "secret": "a", "status": "success"}
	e2 := Event{"timestamp": "2026-01-01T00:02:00Z", "secret": "c", "status": "success"}
	writeEventLines(t, path, []string{
		marshalEvent(t, e1),
		"{not valid json at all",
		marshalEvent(t, e2),
	})

	store := newEventStoreAt(50, nil, dir, "events.jsonl")

	got := store.GetLast(0, "")
	if len(got) != 2 {
		t.Fatalf("expected 2 valid events survived, got %d: %+v", len(got), got)
	}
	if got[0]["secret"] != "a" || got[1]["secret"] != "c" {
		t.Fatalf("unexpected surviving events: %+v", got)
	}
}

// Test 5 — history limit: replay must respect the configured maximum
// history size, keeping the most recent entries.
func TestEventStore_Replay_RespectsHistoryLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	var lines []string
	for i := 0; i < 10; i++ {
		lines = append(lines, marshalEvent(t, Event{"secret": string(rune('a' + i)), "status": "success"}))
	}
	writeEventLines(t, path, lines)

	store := newEventStoreAt(3, nil, dir, "events.jsonl")

	got := store.GetLast(0, "")
	if len(got) != 3 {
		t.Fatalf("expected replay to be capped at limit=3, got %d", len(got))
	}
	// The last 3 written events are indices 7,8,9 -> secrets h,i,j.
	if got[0]["secret"] != "h" || got[2]["secret"] != "j" {
		t.Fatalf("expected the most recent 3 events to survive the limit, got %+v", got)
	}
}

// Test 6 — historical events aren't broadcast: starting EventStore with
// existing history must not emit anything onto the hub's broadcast channel.
func TestEventStore_Replay_DoesNotBroadcastHistoricalEvents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	var lines []string
	for i := 0; i < 10; i++ {
		lines = append(lines, marshalEvent(t, Event{"secret": "s", "status": "success"}))
	}
	writeEventLines(t, path, lines)

	hub := NewHub(nil)
	store := newEventStoreAt(50, hub, dir, "events.jsonl")

	if len(store.GetLast(0, "")) != 10 {
		t.Fatalf("expected 10 replayed events in history")
	}
	if len(hub.broadcast) != 0 {
		t.Fatalf("replay must not broadcast; hub.broadcast has %d pending messages", len(hub.broadcast))
	}
}

// Test 7 — new event: Add() still updates history and broadcasts normally
// after a replay has already happened.
func TestEventStore_Add_AfterReplay_UpdatesHistoryAndBroadcasts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	writeEventLines(t, path, []string{marshalEvent(t, Event{"secret": "old", "status": "success"})})

	hub := NewHub(nil)
	store := newEventStoreAt(50, hub, dir, "events.jsonl")

	store.Add(Event{"secret": "new", "status": "success"})

	got := store.GetLast(0, "")
	if len(got) != 2 {
		t.Fatalf("expected history to grow to 2 after Add, got %d", len(got))
	}
	if got[1]["secret"] != "new" {
		t.Fatalf("expected the new event to be last, got %+v", got)
	}
	if len(hub.broadcast) != 1 {
		t.Fatalf("expected exactly 1 broadcast from the new Add, got %d", len(hub.broadcast))
	}
}

// Test 8 — concurrent Add: run under the race detector.
func TestEventStore_ConcurrentAdd(t *testing.T) {
	dir := t.TempDir()
	hub := NewHub(nil)
	store := newEventStoreAt(50, hub, dir, "events.jsonl")

	done := make(chan struct{})
	for i := 0; i < 20; i++ {
		go func(i int) {
			store.Add(Event{"secret": "concurrent", "status": "success"})
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 20; i++ {
		<-done
	}

	got := store.GetLast(0, "")
	if len(got) != 20 {
		t.Fatalf("expected 20 events after concurrent Add, got %d", len(got))
	}
}
