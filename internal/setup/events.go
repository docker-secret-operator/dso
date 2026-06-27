package setup

import (
	"sync"
	"time"
)

// EventType identifies what happened during a setup run.
type EventType string

const (
	// Setup lifecycle — emitted by the Engine orchestrator.
	EventSetupStarted         EventType = "setup_started"
	EventDetectionCompleted   EventType = "detection_completed"
	EventValidationCompleted  EventType = "validation_completed"
	EventPlanGenerated        EventType = "plan_generated"
	EventPreviewGenerated     EventType = "preview_generated"
	EventApplyStarted         EventType = "apply_started"
	EventApplyCompleted       EventType = "apply_completed"
	EventRollbackStarted      EventType = "rollback_started"
	EventRollbackCompleted    EventType = "rollback_completed"
	EventRollbackFailed       EventType = "rollback_failed"
	EventHealthCheckCompleted EventType = "health_check_completed"
	EventSetupCompleted       EventType = "setup_completed"
	EventSetupFailed          EventType = "setup_failed"
	EventResumeStarted        EventType = "resume_started"
	EventResumeCompleted      EventType = "resume_completed"

	// Transaction lifecycle — emitted by the Applier and Executors.
	// These fire between EventApplyStarted and EventApplyCompleted.
	EventTransactionStarted   EventType = "transaction_started"
	EventTransactionCompleted EventType = "transaction_completed"
	EventTransactionFailed    EventType = "transaction_failed"
	EventOperationStarted     EventType = "operation_started"
	EventOperationCompleted   EventType = "operation_completed"
	EventOperationFailed      EventType = "operation_failed"
)

// Event carries a single lifecycle notification from the setup engine.
// The CLI subscribes to these and renders them; the engine never prints.
type Event struct {
	Type      EventType
	Timestamp time.Time
	// Data holds event-specific payload (e.g. *Environment, *InstallPlan).
	Data  interface{}
	Error error
}

// Emitter broadcasts events to all registered listeners.
// Listeners are called asynchronously so a slow renderer cannot block the engine.
type Emitter struct {
	mu        sync.RWMutex
	listeners []func(Event)
}

// Subscribe registers a listener. Safe to call from multiple goroutines.
func (e *Emitter) Subscribe(fn func(Event)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.listeners = append(e.listeners, fn)
}

// Emit sends an event to all listeners synchronously in registration order.
// Synchronous delivery guarantees that the CLI renders events in the same
// sequence the engine produces them. Listeners should not block.
func (e *Emitter) Emit(evt Event) {
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now()
	}
	e.mu.RLock()
	listeners := make([]func(Event), len(e.listeners))
	copy(listeners, e.listeners)
	e.mu.RUnlock()

	for _, fn := range listeners {
		fn(evt)
	}
}

// emit is a convenience helper for the engine to build and send an event.
func (e *Emitter) emit(t EventType, data interface{}, err error) {
	e.Emit(Event{
		Type:      t,
		Timestamp: time.Now(),
		Data:      data,
		Error:     err,
	})
}
