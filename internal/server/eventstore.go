package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type Event map[string]interface{}

type EventStore struct {
	mu      sync.RWMutex
	events  []Event
	limit   int
	hub     *Hub
	logFile *os.File
}

// eventStoreDir and eventStoreFile are the production location of the
// append-only event log. Tests use newEventStoreAt with a temp directory
// instead of touching this path directly.
const (
	eventStoreDir  = "/var/run/dso"
	eventStoreFile = "events.jsonl"
)

func NewEventStore(limit int, hub *Hub) *EventStore {
	return newEventStoreAt(limit, hub, eventStoreDir, eventStoreFile)
}

// newEventStoreAt builds an EventStore rooted at dir/filename. Split out
// from NewEventStore so tests can exercise startup replay against a
// temporary directory instead of the real /var/run/dso path (which a
// non-root test process cannot reliably write to).
func newEventStoreAt(limit int, hub *Hub, dir, filename string) *EventStore {
	store := &EventStore{
		events: make([]Event, 0, limit),
		limit:  limit,
		hub:    hub,
	}

	path := filepath.Join(dir, filename)

	// Replay existing history BEFORE opening the file for writes, and
	// BEFORE returning the store to any caller -- this is synchronous,
	// single-threaded construction, so there is no concurrent access to
	// race against. Replay only ever populates s.events; it never touches
	// s.hub, so restart never re-broadcasts old events to WebSocket
	// clients (see replayHistory's doc comment).
	store.replayHistory(path)

	if err := os.MkdirAll(dir, 0o750); err != nil {
		return store
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err == nil {
		store.logFile = f
	}

	return store
}

// replayHistory reads path (if it exists) and populates s.events from its
// contents, so an agent restart doesn't lose event history that was
// already durably written. A missing file (first run, or the directory
// doesn't exist yet) is not an error -- the store just starts empty, same
// as before this feature existed. A single malformed line (partial write
// from a prior crash, disk corruption, etc.) is skipped rather than
// aborting the whole replay: one bad historical record must never prevent
// the agent from starting or discard every other valid record around it.
//
// This function must never send anything to s.hub. Replayed events already
// happened before this process started; broadcasting them over the
// WebSocket as if they were new live events would flood every freshly
// connected client with stale history on every restart.
func (s *EventStore) replayHistory(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()

	replayed := make([]Event, 0, s.limit)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		replayed = append(replayed, ev)
	}

	// Respect the same history limit Add() enforces, keeping the most
	// recent entries (chronological order is preserved -- events.jsonl is
	// append-only, so the tail of the file is already the newest events).
	if len(replayed) > s.limit {
		replayed = replayed[len(replayed)-s.limit:]
	}
	s.events = replayed
}

func (s *EventStore) Add(e Event) {
	s.mu.Lock()
	s.events = append(s.events, e)
	if len(s.events) > s.limit {
		s.events = s.events[len(s.events)-s.limit:]
	}

	if s.logFile != nil {
		if b, err := json.Marshal(e); err == nil {
			b = append(b, '\n')
			_, _ = s.logFile.Write(b)
			_ = s.logFile.Sync()
		}
	}
	s.mu.Unlock()

	if s.hub != nil {
		select {
		case s.hub.broadcast <- e:
		default:
			// Hub buffer full; drop broadcast to prevent blocking Add callers.
		}
	}
}

func (s *EventStore) GetLast(limit int, severityFilter string) []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var matched []Event
	for _, ev := range s.events {
		if severityFilter != "" {
			if stat, ok := ev["status"].(string); ok && stat != severityFilter {
				continue // Skip if filtering by severity/status and it doesn't match
			}
		}
		matched = append(matched, ev)
	}

	l := len(matched)
	if limit > l || limit <= 0 {
		limit = l
	}

	res := make([]Event, limit)
	copy(res, matched[l-limit:])
	return res
}
