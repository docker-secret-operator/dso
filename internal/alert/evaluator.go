// Package alert implements DSO's Phase 4.1 event-based operational alerts:
// a small, explicit evaluator that turns a bounded set of operational
// EventStream events (rotation_failed, "signal_failed", service_degraded,
// their resolution counterparts) into deduplicated, cooldown-gated
// notifications via the existing internal/notify.Dispatcher.
//
// This is deliberately NOT a generic rule engine, metric-threshold
// alerting system, or incident manager -- see the package-level comments
// on Evaluator for the explicit event handling this implements instead.
package alert

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/docker-secret-operator/dso/internal/notify"
	"go.uber.org/zap"
)

// DefaultCooldown is used when no operator-configured cooldown is set (see
// pkg/config.NotificationsConfig.AlertCooldown). Chosen to suppress a
// flapping container's repeat failures without silencing an operator for
// an operationally-relevant amount of time.
const DefaultCooldown = 5 * time.Minute

// maxTrackedAlerts bounds the evaluator's in-memory state. Firing alerts
// are never evicted (there are as many of these as there are genuinely
// broken resources, which is inherently small); only resolved alerts are
// evicted, oldest-resolved-first, once the total exceeds this bound -- the
// same bounded-history philosophy as internal/server.EventStore's ring
// buffer, just keyed by dedup key instead of insertion order.
const maxTrackedAlerts = 500

// Status values for Alert.Status.
const (
	StatusFiring   = "firing"
	StatusResolved = "resolved"
)

// Alert is the smallest useful alert record: enough for an operator to
// know what broke, where, and whether it's still broken. Deliberately no
// secret plaintext, old/new secret values, provider credentials, tokens,
// or private keys ever populate any field -- Resource is always a secret
// NAME or container ID/name, and Message is a fixed, templated string
// (see Evaluate), never raw provider error text.
type Alert struct {
	ID               string     `json:"id"`
	Type             string     `json:"type"` // rotation_failed | injection_failed | service_degraded
	Severity         string     `json:"severity"`
	Status           string     `json:"status"` // firing | resolved
	Resource         string     `json:"resource"`
	Message          string     `json:"message"`
	DedupKey         string     `json:"dedup_key"`
	FirstTriggeredAt time.Time  `json:"first_triggered_at"`
	LastTriggeredAt  time.Time  `json:"last_triggered_at"`
	ResolvedAt       *time.Time `json:"resolved_at,omitempty"`
}

// Dispatcher is the subset of *notify.Dispatcher the evaluator needs --
// letting tests substitute a fake without importing notify's concrete
// delivery machinery. Exported so callers (internal/server.StartRESTServer)
// can declare a properly nil-typed interface variable when no Dispatcher
// is configured: passing a nil *notify.Dispatcher directly as this
// interface would instead produce a non-nil interface wrapping a nil
// pointer, defeating the `dispatcher == nil` check in dispatch() and
// panicking on the first alert.
type Dispatcher interface {
	Dispatch(notify.RotationEvent)
}

// Evaluator consumes runtime operational events (see Evaluate) and decides
// whether each represents a new alert, a repeat of an active alert
// (deduplicated, cooldown-gated), or a resolution of one. It is the single
// decision point for whether internal/notify.Dispatcher.Dispatch is ever
// called for these event types -- see internal/agent/trigger.go's
// emitEvent, which pushes to observability.EventStream unconditionally but
// deliberately no longer calls Dispatch directly for rotation outcomes,
// and internal/watcher/controller.go's markDegraded/clearDegraded and
// internal/injector/docker.go's LogInjectionEvent, neither of which ever
// called Dispatch.
//
// Supported events (everything else is ignored):
//
//	rotation_failed    -> alert (dedup: rotation_failed:<secret>)
//	rotation_succeeded -> resolves a firing rotation_failed alert for the same secret, if any
//	"signal_failed"    -> alert, surfaced as type "injection_failed" (dedup: injection_failed:<container>)
//	"signal_success"   -> resolves a firing injection_failed alert for the same container, if any
//	service_degraded   -> alert (dedup: degraded:<container>)
//	service_recovered  -> resolves a firing degraded alert for the same container, if any
//
// Evaluate is safe for concurrent use and never blocks: it holds a mutex
// only across in-memory map operations plus a Dispatch call, and Dispatch
// itself is a non-blocking buffered-channel send. It must never be called
// with historical/replayed events -- callers should subscribe at the same
// point internal/server.EventStore's live consumer does (newly arriving
// events only), never against EventStore's replay-on-startup path.
type Evaluator struct {
	mu       sync.Mutex
	alerts   map[string]*Alert // keyed by DedupKey
	resolved []string          // DedupKeys of resolved alerts, oldest first, for maxTrackedAlerts eviction

	cooldown   time.Duration
	dispatcher Dispatcher // nil-safe: notifications are simply skipped when nil
	logger     *zap.Logger

	nextID uint64
}

// NewEvaluator builds an Evaluator. d may be nil (notifications configured
// off) -- alert state is still tracked and served via GET /api/alerts even
// without a delivery destination. cooldown <= 0 uses DefaultCooldown.
func NewEvaluator(d Dispatcher, cooldown time.Duration, logger *zap.Logger) *Evaluator {
	if cooldown <= 0 {
		cooldown = DefaultCooldown
	}
	return &Evaluator{
		alerts:     make(map[string]*Alert),
		cooldown:   cooldown,
		dispatcher: d,
		logger:     logger,
	}
}

// Evaluate is the evaluator's single entry point, called once per newly
// arriving EventStream event. See the Evaluator doc comment for the
// supported event_type values; everything else returns immediately.
func (e *Evaluator) Evaluate(ev map[string]interface{}) {
	eventType, _ := ev["event_type"].(string)
	provider, _ := ev["provider"].(string)
	container, _ := ev["container"].(string)
	var containers []string
	if container != "" {
		containers = strings.Split(container, ",")
	}

	switch eventType {
	case "rotation_failed":
		secret, _ := ev["secret"].(string)
		if secret == "" {
			return
		}
		e.fire("rotation_failed", "rotation_failed:"+secret, secret,
			fmt.Sprintf("Secret rotation failed for %s.", secret), provider, containers)
	case "rotation_succeeded":
		secret, _ := ev["secret"].(string)
		if secret == "" {
			return
		}
		e.resolve("rotation_failed:"+secret,
			fmt.Sprintf("Secret rotation for %s recovered.", secret), provider, containers)
	case "signal_failed":
		if container == "" {
			return
		}
		e.fire("injection_failed", "injection_failed:"+container, container,
			fmt.Sprintf("Secret injection failed for container %s.", container), provider, containers)
	case "signal_success":
		if container == "" {
			return
		}
		e.resolve("injection_failed:"+container,
			fmt.Sprintf("Secret injection for container %s recovered.", container), provider, containers)
	case "service_degraded":
		if container == "" {
			return
		}
		e.fire("service_degraded", "degraded:"+container, container,
			fmt.Sprintf("Container %s became degraded.", container), provider, containers)
	case "service_recovered":
		if container == "" {
			return
		}
		e.resolve("degraded:"+container,
			fmt.Sprintf("Container %s recovered.", container), provider, containers)
	default:
		return
	}
}

// fire records (creating if needed) a firing alert for dedupKey and sends
// a notification, UNLESS the same dedupKey already notified within the
// cooldown window -- in which case only LastTriggeredAt is updated
// (visible via GET /api/alerts) and no second notification is sent. A
// dedupKey transitioning back from resolved->firing (a re-occurrence of a
// previously-recovered incident) is treated identically: it still respects
// the cooldown clock from the last notification, which is deliberate flap
// protection for a container that degrades/recovers/degrades rapidly.
func (e *Evaluator) fire(alertType, dedupKey, resource, message, provider string, containers []string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now().UTC()
	a, exists := e.alerts[dedupKey]
	if !exists {
		e.nextID++
		a = &Alert{
			ID:               fmt.Sprintf("alert-%d", e.nextID),
			Type:             alertType,
			Severity:         "critical",
			DedupKey:         dedupKey,
			FirstTriggeredAt: now,
		}
		e.alerts[dedupKey] = a
	}

	wasResolved := a.Status == StatusResolved
	// shouldNotify compares against the last time a notification actually
	// went out (LastTriggeredAt only ever advances on a notify -- see
	// below), not merely the last time this event was observed.
	shouldNotify := !exists || wasResolved || now.Sub(a.LastTriggeredAt) >= e.cooldown

	a.Status = StatusFiring
	a.Resource = resource
	a.Message = message
	a.ResolvedAt = nil

	if !shouldNotify {
		return
	}
	a.LastTriggeredAt = now
	e.dispatch(alertType, resource, message, provider, containers, false)
}

// resolve marks a firing alert for dedupKey resolved and sends exactly one
// recovery notification. A dedupKey with no active (firing) alert -- never
// fired, or already resolved -- is a no-op: "repeated recovered ->
// suppressed", matching internal/watcher/controller.go's own
// healthy->healthy = zero-events guarantee one layer up.
func (e *Evaluator) resolve(dedupKey, message, provider string, containers []string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	a, exists := e.alerts[dedupKey]
	if !exists || a.Status != StatusFiring {
		return
	}

	now := time.Now().UTC()
	a.Status = StatusResolved
	a.LastTriggeredAt = now
	a.Message = message
	a.ResolvedAt = &now
	e.resolved = append(e.resolved, dedupKey)
	e.evictOldestResolvedLocked()

	e.dispatch(a.Type, a.Resource, message, provider, containers, true)
}

// dispatch sends one notification via the configured Dispatcher, or does
// nothing when none is configured (mirrors internal/agent/trigger.go's own
// `if t.Dispatcher == nil { return }` nil-safety). Called with e.mu held;
// safe because notify.Dispatcher.Dispatch is itself a non-blocking
// buffered-channel send (see internal/notify/dispatcher.go) -- it never
// performs I/O inline, so this never stalls the evaluator, let alone the
// rotation/injection/reconciliation code that ultimately produced the
// event being evaluated. provider/containers enrich the outgoing
// notify.RotationEvent payload when the originating EventStream event
// carried them (see Evaluate) -- they are never stored on the Alert
// record itself, keeping that model to its documented minimal fields.
func (e *Evaluator) dispatch(alertType, resource, message, provider string, containers []string, resolved bool) {
	if e.dispatcher == nil {
		return
	}
	var eventType notify.EventType
	switch {
	case resolved && alertType == "rotation_failed":
		eventType = notify.RotationSucceeded
	case resolved:
		eventType = notify.ServiceRecovered
	case alertType == "injection_failed":
		eventType = notify.InjectionFailed
	case alertType == "service_degraded":
		eventType = notify.ServiceDegraded
	default:
		eventType = notify.RotationFailed
	}
	e.dispatcher.Dispatch(notify.RotationEvent{
		EventID:      newID(),
		Type:         eventType,
		Timestamp:    time.Now().UTC(),
		Provider:     provider,
		SecretName:   resource,
		Containers:   containers,
		ErrorMessage: message,
	})
}

// evictOldestResolvedLocked drops the oldest resolved alert once total
// tracked state exceeds maxTrackedAlerts. Must be called with e.mu held.
// Firing alerts are never evicted.
func (e *Evaluator) evictOldestResolvedLocked() {
	for len(e.alerts) > maxTrackedAlerts && len(e.resolved) > 0 {
		oldest := e.resolved[0]
		e.resolved = e.resolved[1:]
		if a, ok := e.alerts[oldest]; ok && a.Status == StatusResolved {
			delete(e.alerts, oldest)
		}
	}
}

// Alerts returns a snapshot of all tracked alerts (firing and resolved),
// newest-triggered first. Safe for concurrent use.
func (e *Evaluator) Alerts() []Alert {
	e.mu.Lock()
	defer e.mu.Unlock()

	out := make([]Alert, 0, len(e.alerts))
	for _, a := range e.alerts {
		out = append(out, *a)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].LastTriggeredAt.After(out[i].LastTriggeredAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

var (
	idMu      sync.Mutex
	idCounter uint64
)

// newID generates a small, unique-enough-for-logging event ID for the
// synthetic RotationEvent an alert notification is delivered as. Not
// security-sensitive (unlike notify's own newEventID, this is never used
// as an idempotency key across retries of the SAME delivery --
// Dispatcher.Stop's at-least-once contract is unaffected either way).
func newID() string {
	idMu.Lock()
	defer idMu.Unlock()
	idCounter++
	return fmt.Sprintf("alert-evt-%d-%d", time.Now().UnixNano(), idCounter)
}
