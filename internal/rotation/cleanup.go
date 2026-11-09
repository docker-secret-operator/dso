package rotation

import (
	"context"
	"time"
)

// CleanupTimeout bounds each rollback/cleanup Docker call made after a rotation
// failed, timed out or was cancelled. A variable so tests can shorten it.
var CleanupTimeout = 30 * time.Second

// CleanupContext returns a short, bounded context for rollback and cleanup work.
// It keeps ctx's values but NOT its cancellation or deadline: a rotation that
// was cancelled (daemon shutdown) or ran out of time (manual-rotation timeout)
// must still be able to undo its half-finished changes, otherwise a restored
// original name or a removed shadow container is silently skipped and the
// system is left in an ambiguous state. It must only be used for undoing a
// failed rotation, never to carry the forward path past a cancellation.
func CleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), CleanupTimeout)
}
