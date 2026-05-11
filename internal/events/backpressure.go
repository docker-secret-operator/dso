package events

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/docker/docker/api/types/events"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

var (
	// EventQueueDepth tracks current queue depth
	EventQueueDepth = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "dso_event_queue_depth",
			Help: "Current number of events in processing queue",
		},
	)

	// EventQueueMaxDepth tracks peak queue depth
	EventQueueMaxDepth = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "dso_event_queue_max_depth",
			Help: "Peak event queue depth observed",
		},
	)

	// EventsDropped tracks events dropped due to queue overflow
	EventsDropped = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "dso_events_dropped_total",
			Help: "Total number of events dropped due to queue overflow",
		},
	)

	// WorkerUtilization tracks worker pool utilization percentage
	WorkerUtilization = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "dso_worker_utilization_percent",
			Help: "Percentage of workers currently busy",
		},
	)
)

// EventHandler is a function that processes a Docker event
type EventHandler func(ctx context.Context, msg events.Message) error

// BoundedEventQueue provides backpressure protection for event processing
type BoundedEventQueue struct {
	logger        *zap.Logger
	queue         chan events.Message
	maxQueueSize  int
	numWorkers    int
	activeWorkers int32
	wg            sync.WaitGroup
	maxDepth      int32
	handler       EventHandler
}

// NewBoundedEventQueue creates a new bounded queue with worker pool
// maxQueueSize: maximum events in queue before dropping (e.g., 1000)
// numWorkers: number of concurrent workers (e.g., 16)
// handler: function to process each event
func NewBoundedEventQueue(logger *zap.Logger, maxQueueSize, numWorkers int, handler EventHandler) *BoundedEventQueue {
	if maxQueueSize < 100 {
		maxQueueSize = 100 // Minimum sensible queue size
	}
	if numWorkers < 1 {
		numWorkers = 1
	}
	if numWorkers > 256 {
		numWorkers = 256 // Cap max workers
	}

	return &BoundedEventQueue{
		logger:       logger,
		queue:        make(chan events.Message, maxQueueSize),
		maxQueueSize: maxQueueSize,
		numWorkers:   numWorkers,
		handler:      handler,
	}
}

// Start begins processing events from the queue
func (beq *BoundedEventQueue) Start(ctx context.Context) {
	// Start worker pool
	for i := 0; i < beq.numWorkers; i++ {
		beq.wg.Add(1)
		go beq.worker(ctx, i)
	}

	// Start metrics reporter
	beq.wg.Add(1)
	go beq.reportMetrics(ctx)
}

// worker processes events from the queue
func (beq *BoundedEventQueue) worker(ctx context.Context, id int) {
	defer beq.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-beq.queue:
			if !ok {
				return // Queue closed
			}

			atomic.AddInt32(&beq.activeWorkers, 1)
			defer atomic.AddInt32(&beq.activeWorkers, -1)

			// Process with timeout and panic recovery
			func() {
				defer func() {
					if r := recover(); r != nil {
						beq.logger.Error("Panic in event handler",
							zap.Int("worker", id),
							zap.String("container_id", msg.Actor.ID),
							zap.Any("panic", r))
					}
				}()

				// 30-second timeout per event
				procCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				defer cancel()

				if err := beq.handler(procCtx, msg); err != nil {
					beq.logger.Warn("Event processing failed",
						zap.Int("worker", id),
						zap.String("container_id", msg.Actor.ID),
						zap.String("action", string(msg.Action)),
						zap.Error(err))
				}
			}()

			// Update queue depth
			depth := int32(len(beq.queue))
			EventQueueDepth.Set(float64(depth))
			for {
				currentMax := atomic.LoadInt32(&beq.maxDepth)
				if depth <= currentMax || atomic.CompareAndSwapInt32(&beq.maxDepth, currentMax, depth) {
					break
				}
			}
		}
	}
}

// reportMetrics periodically reports queue and worker metrics
func (beq *BoundedEventQueue) reportMetrics(ctx context.Context) {
	defer beq.wg.Done()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			depth := int32(len(beq.queue))
			maxDepth := atomic.LoadInt32(&beq.maxDepth)
			active := atomic.LoadInt32(&beq.activeWorkers)
			utilization := float64(active) / float64(beq.numWorkers) * 100

			EventQueueDepth.Set(float64(depth))
			EventQueueMaxDepth.Set(float64(maxDepth))
			WorkerUtilization.Set(utilization)

			if utilization > 80 {
				beq.logger.Warn("High worker utilization",
					zap.Int32("active_workers", active),
					zap.Int("total_workers", beq.numWorkers),
					zap.Float64("utilization_percent", utilization),
					zap.Int("queue_depth", int(depth)))
			}
		}
	}
}

// Enqueue attempts to add an event to the queue
// Returns false if queue is full (event dropped)
func (beq *BoundedEventQueue) Enqueue(msg events.Message) bool {
	select {
	case beq.queue <- msg:
		return true
	default:
		// Queue full - drop event and record metric
		EventsDropped.Inc()
		return false
	}
}

// Stop gracefully shuts down the queue and workers
func (beq *BoundedEventQueue) Stop() {
	close(beq.queue)
	beq.wg.Wait()
}

// GetStats returns current queue statistics for operational insight
func (beq *BoundedEventQueue) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"queue_depth":     int32(len(beq.queue)),
		"max_queue_size":  beq.maxQueueSize,
		"active_workers":  atomic.LoadInt32(&beq.activeWorkers),
		"total_workers":   beq.numWorkers,
		"max_depth_seen":  atomic.LoadInt32(&beq.maxDepth),
		"utilization_pct": float64(atomic.LoadInt32(&beq.activeWorkers)) / float64(beq.numWorkers) * 100,
	}
}
