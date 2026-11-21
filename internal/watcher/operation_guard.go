package watcher

import "errors"

// Competing-mutation guard (M0-04 finding 3.3).
//
// Every way of starting a rotation -- the first/scheduled poll, a webhook, an IPC
// reconcile (`docker dso sync`/`apply`), the REST manual-rotate endpoint and the watcher's
// own event-driven dispatch -- funnels into ReloaderController.TriggerReload. The
// per-service rotation lock that serialises them is IN-MEMORY: a restarted agent
// starts with every service unlocked, so on its own it cannot know that the previous
// process died halfway through an operation on the same service. The guard is the
// durable counterpart. It is consulted per target, before the service lock is taken,
// and refuses to start a competing mutation while the service has an operation that
// is neither complete nor reconciled (or while startup recovery has not finished).

// ErrRotationBlocked is reported to TriggerReload's onComplete (wrapped together with
// ErrRotationDeferred, so callers that already retry a deferred rotation keep doing
// so) when at least one target was refused by the OperationGuard. Nothing was
// applied to that target.
var ErrRotationBlocked = errors.New("rotation blocked: the service has an unresolved operation")

// OperationGuard decides whether a new mutating operation may start on a service.
// keys are every identifier the target is known by (container name, compose
// service name, container ID); an implementation matches its records against any
// of them, because strategies journal under different service identifiers.
type OperationGuard interface {
	RotationBlocked(keys ...string) (reason string, blocked bool)
}
