package watcher

import (
	"sync"
	"testing"
	"time"
)

// TestPurgeStaleActions_RemovesOldEntries verifies that purgeStaleActions deletes
// entries older than actionEntryTTL and keeps recent ones (H5 regression test).
func TestPurgeStaleActions_RemovesOldEntries(t *testing.T) {
	// Reset the map between tests — package-level var is shared.
	recentDSOActions.Range(func(k, _ interface{}) bool {
		recentDSOActions.Delete(k)
		return true
	})

	// Store a stale entry (older than actionEntryTTL).
	recentDSOActions.Store("stale-container", time.Now().Add(-(actionEntryTTL + time.Second)))
	// Store a fresh entry.
	recentDSOActions.Store("fresh-container", time.Now())

	purgeStaleActions()

	if _, ok := recentDSOActions.Load("stale-container"); ok {
		t.Error("H5 regression: stale entry was not purged from recentDSOActions")
	}
	if _, ok := recentDSOActions.Load("fresh-container"); !ok {
		t.Error("purgeStaleActions incorrectly removed a fresh entry")
	}
}

// TestPurgeStaleActions_EmptyMap verifies no panic on an empty map (edge case).
func TestPurgeStaleActions_EmptyMap(t *testing.T) {
	recentDSOActions.Range(func(k, _ interface{}) bool {
		recentDSOActions.Delete(k)
		return true
	})
	// Must not panic.
	purgeStaleActions()
}

// TestRecordDSOAction_StoresEntry verifies that RecordDSOAction stores an entry
// with a recent timestamp and does not panic on repeated calls.
func TestRecordDSOAction_StoresEntry(t *testing.T) {
	recentDSOActions.Range(func(k, _ interface{}) bool {
		recentDSOActions.Delete(k)
		return true
	})

	RecordDSOAction("container-abc")

	val, ok := recentDSOActions.Load("container-abc")
	if !ok {
		t.Fatal("RecordDSOAction did not store the entry")
	}
	ts := val.(time.Time)
	if time.Since(ts) > time.Second {
		t.Errorf("stored timestamp is too old: %v", ts)
	}
}

// TestRecordDSOAction_Concurrent verifies that concurrent RecordDSOAction calls
// do not race or panic (H5 concurrent access test).
func TestRecordDSOAction_Concurrent(t *testing.T) {
	const goroutines = 50
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := "container-" + string(rune('a'+n%26))
			RecordDSOAction(id)
		}(i)
	}
	// Concurrent purge while writes are in flight.
	wg.Add(1)
	go func() {
		defer wg.Done()
		purgeStaleActions()
	}()
	wg.Wait()
}

// TestPurgeStaleActions_OnlyPurgesExpired verifies that multiple entries at
// different ages are handled correctly in a single purge sweep.
func TestPurgeStaleActions_OnlyPurgesExpired(t *testing.T) {
	recentDSOActions.Range(func(k, _ interface{}) bool {
		recentDSOActions.Delete(k)
		return true
	})

	// Entry just barely expired.
	recentDSOActions.Store("just-expired", time.Now().Add(-(actionEntryTTL + 100*time.Millisecond)))
	// Entry that should survive.
	recentDSOActions.Store("almost-expired", time.Now().Add(-(actionEntryTTL - 30*time.Second)))

	purgeStaleActions()

	if _, ok := recentDSOActions.Load("just-expired"); ok {
		t.Error("just-expired entry should have been purged")
	}
	if _, ok := recentDSOActions.Load("almost-expired"); !ok {
		t.Error("almost-expired entry should still be present")
	}
}
