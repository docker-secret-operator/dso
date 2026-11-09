package observability

import "sync/atomic"

// Operational counters backing the Phase 3 Analytics Overview.
//
// These are process-lifetime-only, in-memory monotonic counters -- NOT
// persisted anywhere, and reset to zero on every process restart. The
// WebUI/API must present them as "since last restart," never as an
// all-time total; do not fabricate historical continuity across restarts.
//
// Each counter increments at exactly one authoritative call site --
// TriggerEngine.emitEvent for rotation outcomes, DockerInjector.
// LogInjectionEvent for injection outcomes -- the same single place that
// already emits the corresponding event onto EventStream, so these counts
// track logical operation outcomes, never HTTP requests, retries, or
// internal rollback attempts.
var (
	RotationSuccessTotal  atomic.Uint64
	RotationFailureTotal  atomic.Uint64
	InjectionSuccessTotal atomic.Uint64
	InjectionFailureTotal atomic.Uint64
)
