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
	mu         sync.RWMutex                  // Protect lastLabels
	wg         sync.WaitGroup                // Track watchEvents goroutine completion
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

// Stop stops listening for Docker events gracefully
func (cl *ContainerListener) Stop() error {
	if cl.cancel != nil {
		cl.cancel()
	}

	// Signal the watch loop to stop
	select {
	case cl.stopChan <- struct{}{}:
	case <-time.After(100 * time.Millisecond):
		// Timeout, move on
	}

	// Wait for watchEvents goroutine to exit (it will close eventsChan)
	cl.wg.Wait()

	return nil
}

// Events returns a read-only channel for ContainerLabelEvent emissions
func (cl *ContainerListener) Events() <-chan *ContainerLabelEvent {
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

	// Set up event filters for container events
	filter := filters.NewArgs()
	filter.Add("type", "container")

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
		// Determine the action based on the event type
		var action Action
		if event.Status == "start" || (!wasTracked && len(changed) > 0) {
			action = ActionLabelCreate
		} else if event.Status == "stop" || event.Status == "die" {
			action = ActionLabelRemove
		} else {
			action = ActionLabelUpdate
		}

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
