package events

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

// ContainerListener watches Docker events and emits ContainerLabelEvent when relevant labels change
type ContainerListener struct {
	client     *client.Client
	eventsChan chan *ContainerLabelEvent
	stopChan   chan struct{}
	ctx        context.Context
	cancel     context.CancelFunc
	lastLabels map[string]map[string]string // containerID -> labels snapshot
	mu         sync.RWMutex                 // Protect lastLabels
	wg         sync.WaitGroup               // Track watchEvents goroutine completion
}

// NewContainerListener creates a new ContainerListener with the given Docker client
func NewContainerListener(dockerClient *client.Client) *ContainerListener {
	if dockerClient == nil {
		panic("docker client cannot be nil")
	}
	return &ContainerListener{
		client:     dockerClient,
		eventsChan: make(chan *ContainerLabelEvent, 10),
		stopChan:   make(chan struct{}),
		lastLabels: make(map[string]map[string]string),
	}
}

// Start begins listening for Docker events
func (cl *ContainerListener) Start(ctx context.Context) error {
	if cl.ctx != nil {
		return fmt.Errorf("listener already started")
	}

	// Create a cancellable context
	cl.ctx, cl.cancel = context.WithCancel(ctx)

	// Initialize containers with relevant labels
	if err := cl.initializeContainers(); err != nil {
		cl.cancel()
		cl.ctx = nil
		return fmt.Errorf("failed to initialize containers: %w", err)
	}

	// Start the event watching goroutine
	cl.wg.Add(1)
	go cl.watchEvents()

	return nil
}

// Stop stops listening for Docker events gracefully. The listener can be
// Start()ed again afterward (REL-5): ctx/cancel are reset, and eventsChan/
// stopChan are recreated since watchEvents closes eventsChan on exit.
func (cl *ContainerListener) Stop() error {
	if cl.cancel != nil {
		cl.cancel()
	}

	// Best-effort signal in case watchEvents is somehow still waiting on
	// stopChan rather than cl.ctx.Done() (belt-and-suspenders; cancel() above
	// is what actually stops it). Non-blocking: once cl.cancel() fires,
	// watchEvents exits via its ctx.Done() case and nothing is left to
	// receive here, so a blocking send with a timeout previously stalled
	// every Stop() call for the full timeout window for no benefit.
	select {
	case cl.stopChan <- struct{}{}:
	default:
	}

	// Wait for watchEvents goroutine to exit (it will close eventsChan)
	cl.wg.Wait()

	cl.ctx = nil
	cl.cancel = nil
	// eventsChan is guarded by cl.mu (unlike ctx/cancel/stopChan) because,
	// unlike those, it's read concurrently by the public Events() method,
	// which callers may legitimately call while Stop() is running.
	cl.mu.Lock()
	cl.eventsChan = make(chan *ContainerLabelEvent, 10)
	cl.mu.Unlock()
	cl.stopChan = make(chan struct{})

	return nil
}

// Events returns a read-only channel for ContainerLabelEvent emissions
func (cl *ContainerListener) Events() <-chan *ContainerLabelEvent {
	cl.mu.RLock()
	defer cl.mu.RUnlock()
	return cl.eventsChan
}

// HasRelevantLabels checks if a container has labels matching the DSO pattern
func HasRelevantLabels(labels map[string]string) bool {
	for key := range labels {
		// Check for exact matches
		if key == "secret" || key == "rotation-strategy" {
			return true
		}
		// Check for dso.* prefix
		if strings.HasPrefix(key, "dso.") {
			return true
		}
	}
	return false
}

// DetectLabelChanges returns a map of changed labels (key -> new value, empty string if removed)
func DetectLabelChanges(before, after map[string]string) map[string]string {
	changed := make(map[string]string)

	// Check for relevant labels that were present before or after
	allKeys := make(map[string]bool)
	for key := range before {
		if isRelevantLabel(key) {
			allKeys[key] = true
		}
	}
	for key := range after {
		if isRelevantLabel(key) {
			allKeys[key] = true
		}
	}

	// Check each relevant key for changes
	for key := range allKeys {
		beforeVal := before[key]
		afterVal, afterExists := after[key]

		// If the value changed or was added/removed
		if beforeVal != afterVal {
			if afterExists {
				changed[key] = afterVal
			} else {
				// Label was removed, store empty string to indicate removal
				changed[key] = ""
			}
		}
	}

	return changed
}

// resolveLabelAction maps a Docker container event action to the label action
// DSO emits. Extracted from handleEvent so this mapping is unit-testable
// without a live Docker daemon: it drives which rotation path fires, and it
// reads `event.Action` (the current field) rather than the deprecated
// `event.Status`. For container-type events the Docker daemon sets Status to
// the same string as Action, so the two are equivalent here.
//
// Callers only reach this when at least one relevant label actually changed,
// which is why "not previously tracked" alone implies a create.
func resolveLabelAction(dockerAction events.Action, wasTracked bool) Action {
	switch {
	case dockerAction == "start" || !wasTracked:
		return ActionLabelCreate
	case dockerAction == "stop" || dockerAction == "die":
		return ActionLabelRemove
	default:
		return ActionLabelUpdate
	}
}

// isRelevantLabel checks if a label key is relevant to DSO
func isRelevantLabel(key string) bool {
	return key == "secret" || key == "rotation-strategy" || strings.HasPrefix(key, "dso.")
}

// initializeContainers scans all running containers and stores their labels if relevant
func (cl *ContainerListener) initializeContainers() error {
	containers, err := cl.client.ContainerList(cl.ctx, container.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	cl.mu.Lock()
	defer cl.mu.Unlock()

	for _, c := range containers {
		if HasRelevantLabels(c.Labels) {
			cl.lastLabels[c.ID] = copyLabels(c.Labels)
		}
	}

	return nil
}

// watchEvents is the main event watching loop
func (cl *ContainerListener) watchEvents() {
	defer cl.wg.Done()
	defer close(cl.eventsChan)

	// Set up event filters for container events.
	//
	// PERF-4: filter by action, not just type. Subscribing to bare
	// type=container delivers every container event on the host --
	// exec_create/exec_start/exec_die, attach, top, resize, health_status,
	// oom, kill -- and handleEvent's first act used to be a full
	// ContainerInspect round-trip. A single `docker exec` produced three
	// events (so three inspects), and any container with a HEALTHCHECK
	// emitted health_status forever, for containers DSO does not even manage.
	// The cost scaled with total host activity rather than DSO's tracked set.
	//
	// These are the only actions that can change a container's labels or its
	// existence, which is all this listener reacts to: update carries label
	// changes on a running container, and create/start/stop/die/destroy carry
	// lifecycle transitions (destroy is kept so tracking cleanup still runs).
	filter := filters.NewArgs()
	filter.Add("type", "container")
	for _, action := range []string{"create", "start", "stop", "die", "destroy", "update"} {
		filter.Add("event", action)
	}

	eventChan, errChan := cl.client.Events(cl.ctx, events.ListOptions{Filters: filter})

	for {
		select {
		case <-cl.stopChan:
			return
		case <-cl.ctx.Done():
			return
		case event := <-eventChan:
			cl.handleEvent(event)
		case err, ok := <-errChan:
			if !ok {
				// Channel closed, time to exit
				return
			}
			if err != nil && err != context.Canceled {
				// Docker event stream error - log and exit gracefully
				// In production, would use proper logging here
				// For now, return to cleanly exit the watch loop
				return
			}
		}
	}
}

// handleEvent processes a Docker event and emits ContainerLabelEvent if labels changed
func (cl *ContainerListener) handleEvent(event events.Message) {
	containerID := event.Actor.ID

	// PERF-4: reject irrelevant containers from the event payload before
	// paying for an API round-trip. The daemon already ships the container's
	// labels in Actor.Attributes, so for the common case -- an event about a
	// container DSO does not manage -- this avoids the ContainerInspect below
	// entirely.
	//
	// Guarded on len(Attributes) > 0: if a daemon/API version ever delivered
	// an event without attributes, an empty map would look like "no relevant
	// labels" and we would silently skip a container we should be watching.
	// Falling through to the inspect in that case keeps the old behavior as
	// the conservative default rather than trading correctness for calls.
	if len(event.Actor.Attributes) > 0 && !HasRelevantLabels(event.Actor.Attributes) {
		// Still drop any tracking state, exactly as the post-inspect
		// irrelevance path below does -- this is local map work, no API call.
		cl.mu.Lock()
		delete(cl.lastLabels, containerID)
		cl.mu.Unlock()
		return
	}

	// Get current container info
	inspect, err := cl.client.ContainerInspect(cl.ctx, containerID)
	if err != nil {
		// Container might have been removed, clean up from lastLabels
		cl.mu.Lock()
		delete(cl.lastLabels, containerID)
		cl.mu.Unlock()
		return
	}

	currentLabels := inspect.Config.Labels
	if currentLabels == nil {
		currentLabels = make(map[string]string)
	}

	// Check if this container has relevant labels
	if !HasRelevantLabels(currentLabels) {
		// If we were tracking this container, clean it up
		cl.mu.Lock()
		delete(cl.lastLabels, containerID)
		cl.mu.Unlock()
		return
	}

	cl.mu.Lock()
	previousLabels, wasTracked := cl.lastLabels[containerID]
	if !wasTracked {
		previousLabels = make(map[string]string)
	}

	// Detect changes
	changed := DetectLabelChanges(previousLabels, currentLabels)

	// Update the stored labels
	cl.lastLabels[containerID] = copyLabels(currentLabels)
	cl.mu.Unlock()

	// Emit event if there were changes
	if len(changed) > 0 {
		action := resolveLabelAction(event.Action, wasTracked)

		labelEvent := &ContainerLabelEvent{
			ContainerID: containerID,
			Labels:      changed,
			Action:      action,
			Timestamp:   time.Now(),
		}

		// Send event, with timeout to prevent blocking
		select {
		case cl.eventsChan <- labelEvent:
		case <-time.After(100 * time.Millisecond):
			// Timeout - event channel may be full, skip this event
		case <-cl.ctx.Done():
			return
		}
	}
}

// copyLabels creates a deep copy of a labels map
func copyLabels(labels map[string]string) map[string]string {
	if labels == nil {
		return make(map[string]string)
	}
	copy := make(map[string]string)
	for k, v := range labels {
		copy[k] = v
	}
	return copy
}
