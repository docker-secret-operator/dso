// Package polling provides SmartPoller, an adaptive per-secret poll-interval
// calculator.
//
// Status (G024 disposition, docs/design/g024-disposition.md): RETAINED, NOT
// WIRED. No production code path imports this package; it is exercised only by
// its own tests and internal/integration_test. Provider polling in production
// is performed by TriggerEngine.StartPolling in internal/agent, which uses its
// own interval-with-backoff logic. docs/SMART_POLLING.md documents SmartPoller
// as a design reference for a possible future adoption inside that function.
// No API-call reduction is delivered today, and no timeline is committed.
package polling
