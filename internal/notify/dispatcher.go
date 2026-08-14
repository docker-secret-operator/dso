package notify

import (
	"context"
	"sync"
	"time"

	"github.com/docker-secret-operator/dso/pkg/observability"
	"go.uber.org/zap"
)

// queueCapacity bounds the number of undelivered events held in memory. If
// the queue is full (every destination slow/unreachable for a sustained
// period), new events are DROPPED with a log + metric rather than ever
// blocking the rotation engine. Dropping observability is the designed
// trade; delaying rotation is not an option.
const queueCapacity = 64

// Dispatcher decouples event emission (called from rotation/recovery code)
// from delivery (a single background worker). Dispatch never blocks and
// never returns an error -- rotation code cannot observe, and therefore
// cannot be affected by, notification outcomes.
//
// The event channel is deliberately NEVER closed: rotation strategies run
// in detached goroutines that can fire their onComplete callbacks after
// agent shutdown has begun, so a late Dispatch racing Stop must remain
// safe (a send on a closed channel panics even inside a select). Stop
// signals the worker via a separate stop channel instead; late events
// after the worker exits simply accumulate in (or drop from) the buffer
// harmlessly until the process ends.
type Dispatcher struct {
	ch        chan RotationEvent
	stop      chan struct{}
	notifiers []*WebhookNotifier
	logger    *zap.Logger
	wg        sync.WaitGroup
	stopOnce  sync.Once
}

// NewDispatcher starts the delivery worker. notifiers may be empty (the
// worker then drains events to nowhere), but callers normally only
// construct a Dispatcher when at least one destination is configured.
func NewDispatcher(notifiers []*WebhookNotifier, logger *zap.Logger) *Dispatcher {
	d := &Dispatcher{
		ch:        make(chan RotationEvent, queueCapacity),
		stop:      make(chan struct{}),
		notifiers: notifiers,
		logger:    logger,
	}
	d.wg.Add(1)
	go d.run()
	return d
}

// Dispatch enqueues an event for background delivery. Non-blocking by
// construction: a full queue drops the event (logged + counted). Safe to
// call from multiple goroutines, including concurrently with (or after)
// Stop -- see the type comment for why this can never panic.
func (d *Dispatcher) Dispatch(event RotationEvent) {
	select {
	case d.ch <- event:
	default:
		observability.NotificationDroppedTotal.Inc()
		d.logger.Warn("notification queue full, dropping event",
			zap.String("event_type", string(event.Type)),
			zap.String("secret", event.SecretName))
	}
}

// Stop signals the worker to drain whatever is currently queued and exit,
// waiting up to timeout. Events still undelivered at the deadline are
// abandoned (in-flight HTTP calls are bounded by their own timeouts, so
// the goroutine itself always exits shortly after regardless).
func (d *Dispatcher) Stop(timeout time.Duration) {
	d.stopOnce.Do(func() { close(d.stop) })

	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		d.logger.Warn("notification dispatcher drain timed out; abandoning undelivered events")
	}
}

func (d *Dispatcher) run() {
	defer d.wg.Done()
	for {
		select {
		case event := <-d.ch:
			d.deliver(event)
		case <-d.stop:
			// Drain what's already queued, then exit. New events arriving
			// during the drain race the final default and may be missed --
			// acceptable: shutdown-time notification loss is within the
			// at-least-once (not exactly-once) delivery contract.
			for {
				select {
				case event := <-d.ch:
					d.deliver(event)
				default:
					return
				}
			}
		}
	}
}

func (d *Dispatcher) deliver(event RotationEvent) {
	for _, n := range d.notifiers {
		observability.NotificationAttemptsTotal.WithLabelValues(n.SafeName(), string(event.Type)).Inc()
		// Per-delivery bound independent of the notifier's own HTTP
		// timeout, so one pathological destination can't stall the
		// queue behind it indefinitely.
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		err := n.Notify(ctx, event)
		cancel()
		if err != nil {
			observability.NotificationFailuresTotal.WithLabelValues(n.SafeName(), string(event.Type)).Inc()
			// A notification failure is its own category of problem --
			// never escalated to anything rotation-related.
			d.logger.Warn("notification delivery failed",
				zap.String("destination", n.SafeName()),
				zap.String("event_type", string(event.Type)),
				zap.String("event_id", event.EventID),
				zap.Error(err))
		}
	}
}
