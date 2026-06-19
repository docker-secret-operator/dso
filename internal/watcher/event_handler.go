package watcher

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/events"
)

var debouncer = NewEventDebouncer(3 * time.Second)

var (
	recentDSOActions sync.Map
	// purgeOnce ensures the background purge goroutine is started exactly once.
	purgeOnce sync.Once
)

// actionEntryTTL is how long an action entry is retained before being purged.
// Must be larger than the 15-second ignore window used in ProcessEvent.
const actionEntryTTL = 5 * time.Minute

// startActionPurge launches a background goroutine that periodically removes
// recentDSOActions entries older than actionEntryTTL (H5 fix).
func startActionPurge() {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			purgeStaleActions()
		}
	}()
}

// purgeStaleActions removes entries from recentDSOActions that are older than
// actionEntryTTL. Safe to call from multiple goroutines concurrently.
func purgeStaleActions() {
	cutoff := time.Now().Add(-actionEntryTTL)
	recentDSOActions.Range(func(k, v interface{}) bool {
		if v.(time.Time).Before(cutoff) {
			recentDSOActions.Delete(k)
		}
		return true
	})
}

// RecordDSOAction marks a container ID or compose project as being restarted by DSO to avoid loop events
func RecordDSOAction(identifier string) {
	// Start the purge goroutine lazily on the first write (H5).
	purgeOnce.Do(startActionPurge)
	recentDSOActions.Store(identifier, time.Now())
}

// ProcessEvent formats and logs a docker event message properly.
func ProcessEvent(msg events.Message, debug bool) {
	eventID := msg.Actor.ID + ":" + string(msg.Action)
	if !debouncer.CheckAndRecord(eventID) {
		if debug {
			fmt.Printf("   [DEBUG] \033[1;33m[DSO DEBOUNCE]\033[0m Ignored duplicate %s event within debounce window\n", strings.ToUpper(string(msg.Action)))
		}
		return
	}

	if val, exists := recentDSOActions.Load(msg.Actor.ID); exists {
		if time.Since(val.(time.Time)) < 15*time.Second {
			fmt.Printf("\033[1;33m[DSO WATCH]\033[0m Ignoring self-triggered event → %s\n", shortID(msg.Actor.ID))
			return
		}
	}

	projectName := msg.Actor.Attributes["com.docker.compose.project"]
	if projectName != "" {
		if val, exists := recentDSOActions.Load(projectName); exists {
			if time.Since(val.(time.Time)) < 15*time.Second {
				fmt.Printf("\033[1;33m[DSO WATCH]\033[0m Ignoring self-triggered event → project: %s\n", projectName)
				return
			}
		}
	}

	timestamp := time.Now().Format("15:04:05")
	event := strings.ToUpper(string(msg.Action))
	name := msg.Actor.Attributes["name"]
	if name == "" {
		name = shortID(msg.Actor.ID)
	}

	// Output format: [DSO WATCH] [TIMESTAMP] EVENT → container_name
	fmt.Printf("\033[1;36m[DSO WATCH]\033[0m [%s] \033[1;1m%s\033[0m → %s\n", timestamp, event, name)

	if debug {
		fmt.Printf("   [DEBUG] ID: %s, Image: %s, Action: %v\n", shortID(msg.Actor.ID), msg.Actor.Attributes["image"], msg.Action)
		for k, v := range msg.Actor.Attributes {
			if k != "name" {
				fmt.Printf("           %s: %s\n", k, v)
			}
		}
	}
}
