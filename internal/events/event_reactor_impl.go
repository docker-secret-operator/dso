package events

import (
	"container/heap"
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// EventReactorImpl implements the EventReactor interface with batching,
// deduplication, and priority-based event processing
type EventReactorImpl struct {
	// Event queue with priority sorting
	queue PriorityQueue
	mu    sync.Mutex

	// Deduplication (1s window)
	lastSeen   map[string]time.Time
	lastSeenMu sync.RWMutex

	// Batching (5s window)
	batchTicker *time.Ticker
	batchTimeout time.Duration

	// State management
	ctx       context.Context
	cancel    context.CancelFunc
	stopChan  chan struct{}
	stopOnce  sync.Once
	startOnce sync.Once

	// Callback for rotation trigger
	rotationTrigger RotationTrigger

	// Health check
	lastEventTime atomic.Value // time.Time

	// Sequence counter for FIFO ordering within same priority
	seqCounter int64

	// Logger
	logger *zap.Logger
}

// Verify that EventReactorImpl implements the EventReactor interface
var _ EventReactor = (*EventReactorImpl)(nil)

// PriorityQueue is a min-heap based priority queue for events
// Higher priority values are processed first, with FIFO ordering within same priority
type PriorityQueue []*QueuedEvent

func (pq PriorityQueue) Len() int { return len(pq) }

func (pq PriorityQueue) Less(i, j int) bool {
	// Higher priority first
	if pq[i].Priority != pq[j].Priority {
		return pq[i].Priority > pq[j].Priority
	}
	// FIFO within same priority (lower sequence = older = process first)
	return pq[i].Sequence < pq[j].Sequence
}

func (pq PriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}

func (pq *PriorityQueue) Push(x interface{}) {
	*pq = append(*pq, x.(*QueuedEvent))
}

func (pq *PriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[0 : n-1]
	return item
}

// NewEventReactorImpl creates a new EventReactorImpl instance
func NewEventReactorImpl(rotationTrigger RotationTrigger) *EventReactorImpl {
	logger, err := zap.NewProduction()
	if err != nil {
		logger = zap.NewNop()
	}

	ctx, cancel := context.WithCancel(context.Background())

	reactor := &EventReactorImpl{
		queue:           make(PriorityQueue, 0),
		lastSeen:        make(map[string]time.Time),
		batchTimeout:    5 * time.Second,
		ctx:             ctx,
		cancel:          cancel,
		stopChan:        make(chan struct{}),
		rotationTrigger: rotationTrigger,
		seqCounter:      0,
		logger:          logger,
	}

	// Initialize lastEventTime to zero value
	reactor.lastEventTime.Store(time.Time{})

	return reactor
}

// ProcessSecretEvent processes a secret change event
func (r *EventReactorImpl) ProcessSecretEvent(ctx context.Context, event SecretChangeEvent) error {
	// Check for deduplication
	if !r.deduplicateSecret(event.SecretName) {
		return nil // Deduplicated, skip processing
	}

	// Map severity to priority
	priority := severityToPriority(event.Severity)

	// Enqueue the event
	r.enqueueEvent(&QueuedEvent{
		Event:      &event,
		Priority:   priority,
		EnqueuedAt: time.Now(),
	})

	return nil
}

// ProcessContainerEvent processes a container label event
func (r *EventReactorImpl) ProcessContainerEvent(ctx context.Context, event ContainerLabelEvent) error {
	// Extract secret name from labels (look for "secret" label)
	if secretName, ok := event.Labels["secret"]; ok && secretName != "" {
		// Create a SecretChangeEvent from the container event
		version := event.ContainerID
		if len(version) > 12 {
			version = version[:12]
		}
		secretEvent := SecretChangeEvent{
			SecretName: secretName,
			Version:    version,
			Source:     SourceDockerLabel,
			Severity:   SeverityNormal,
			Timestamp:  event.Timestamp,
			Metadata: map[string]string{
				"container_id": event.ContainerID,
				"action":       event.Action.String(),
			},
		}

		// Process as a secret event with deduplication
		if !r.deduplicateSecret(secretName) {
			return nil // Deduplicated, skip processing
		}

		// Enqueue with normal priority
		r.enqueueEvent(&QueuedEvent{
			Event:      &secretEvent,
			Priority:   PriorityNormal,
			EnqueuedAt: time.Now(),
		})
	}

	return nil
}

// Start starts the event reactor's batching loop
func (r *EventReactorImpl) Start(ctx context.Context) error {
	var startErr error
	r.startOnce.Do(func() {
		r.logger.Info("starting event reactor")

		// Override the internal context with the provided one if non-nil
		if ctx != nil {
			r.ctx, r.cancel = context.WithCancel(ctx)
		}

		// Create batch ticker
		r.batchTicker = time.NewTicker(r.batchTimeout)

		// Start the batching goroutine
		go r.processBatches()
	})

	return startErr
}

// Stop stops the event reactor
func (r *EventReactorImpl) Stop(ctx context.Context) error {
	r.logger.Info("stopping event reactor")

	var stopErr error
	r.stopOnce.Do(func() {
		if r.batchTicker != nil {
			r.batchTicker.Stop()
		}
		r.cancel()
		close(r.stopChan)
	})

	return stopErr
}

// IsHealthy returns true if an event was processed in the last 30 seconds
func (r *EventReactorImpl) IsHealthy() bool {
	lastTime, ok := r.lastEventTime.Load().(time.Time)
	if !ok || lastTime.IsZero() {
		return false
	}

	return time.Since(lastTime) < 30*time.Second
}

// processBatches is the main batching loop
func (r *EventReactorImpl) processBatches() {
	defer r.logger.Info("batch processor stopped")

	for {
		select {
		case <-r.ctx.Done():
			return
		case <-r.stopChan:
			return
		case <-r.batchTicker.C:
			r.processBatch()
		}
	}
}

// processBatch dequeues up to 5 events and triggers rotations
func (r *EventReactorImpl) processBatch() {
	batch := r.dequeueBatch(5)

	if len(batch) == 0 {
		return
	}

	r.logger.Debug("processing batch", zap.Int("batch_size", len(batch)))

	for _, queuedEvent := range batch {
		event := queuedEvent.Event

		// Extract secret name based on event type
		var secretName string
		var priority EventPriority

		switch e := event.(type) {
		case *SecretChangeEvent:
			secretName = e.SecretName
			priority = severityToPriority(e.Severity)
		case *ContainerLabelEvent:
			// Already processed in ProcessContainerEvent
			continue
		default:
			r.logger.Warn("unknown event type", zap.String("type", fmt.Sprintf("%T", event)))
			continue
		}

		// Call the rotation trigger
		err := r.rotationTrigger(r.ctx, secretName, priority)
		if err != nil {
			r.logger.Error("rotation trigger failed",
				zap.String("secret_name", secretName),
				zap.Error(err))
			// Continue processing even on error
		}

		// Update last event time for health check
		r.lastEventTime.Store(time.Now())
	}
}

// enqueueEvent adds an event to the priority queue (thread-safe)
func (r *EventReactorImpl) enqueueEvent(queuedEvent *QueuedEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Assign sequence number
	queuedEvent.Sequence = atomic.AddInt64(&r.seqCounter, 1) - 1

	heap.Push(&r.queue, queuedEvent)
	r.logger.Debug("event enqueued",
		zap.String("event", queuedEvent.Event.String()),
		zap.Int("priority", int(queuedEvent.Priority)))
}

// dequeueBatch dequeues up to maxSize events from the priority queue (thread-safe)
func (r *EventReactorImpl) dequeueBatch(maxSize int) []*QueuedEvent {
	r.mu.Lock()
	defer r.mu.Unlock()

	batch := make([]*QueuedEvent, 0, maxSize)

	for i := 0; i < maxSize && r.queue.Len() > 0; i++ {
		event := heap.Pop(&r.queue).(*QueuedEvent)
		batch = append(batch, event)
	}

	return batch
}

// deduplicateSecret checks if a secret has been seen in the last 1 second
// Returns true if the event should be processed (not a duplicate)
// Returns false if the event is a duplicate and should be skipped
func (r *EventReactorImpl) deduplicateSecret(secretName string) bool {
	r.lastSeenMu.Lock()
	defer r.lastSeenMu.Unlock()

	now := time.Now()
	lastSeen, exists := r.lastSeen[secretName]

	if exists && now.Sub(lastSeen) < 1*time.Second {
		// This is a duplicate within the 1s window
		r.logger.Debug("event deduplicated",
			zap.String("secret_name", secretName),
			zap.Duration("age", now.Sub(lastSeen)))
		return false
	}

	// Not a duplicate or window expired, record this event
	r.lastSeen[secretName] = now
	return true
}

// severityToPriority converts Severity to EventPriority
func severityToPriority(severity Severity) EventPriority {
	switch severity {
	case SeverityCritical:
		return PriorityCritical
	case SeverityHigh:
		return PriorityNormal
	case SeverityNormal:
		fallthrough
	default:
		return PriorityLow
	}
}
