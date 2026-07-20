# Path A: Smart Polling + Docker Event Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement adaptive polling with event-based rotation triggering to reduce secret backend API calls by 80% while enabling sub-10-second rotation latency for Docker label changes.

**Architecture:** SmartPoller adapts polling intervals based on secret change frequency (base 30s, backoff to 5m when idle, aggress to 5s after change detection). ContainerEventListener watches Docker events for immediate rotation triggers. EventReactor batches and prioritizes both polling discoveries and event triggers. No cloud provider webhooks required—all coordination happens inside DSO Agent.

**Tech Stack:** Go, Docker Events API, time-based adaptation, priority queues

---

## File Structure

**Core polling & events:**
- `internal/polling/smart_poller.go` — Adaptive interval logic (NEW)
- `internal/polling/poller_state.go` — Track polling state per secret (NEW)
- `internal/events/container_listener.go` — Docker event watching (NEW)
- `internal/events/event_reactor.go` — Unified event processing (NEW)
- `internal/events/event_types.go` — Event definitions (NEW)

**Integration:**
- `internal/agent/agent.go` — Replace fixed polling with SmartPoller (MODIFY)

**Tests:**
- `internal/polling/smart_poller_test.go` — SmartPoller unit + integration (NEW)
- `internal/events/container_listener_test.go` — Event watcher tests (NEW)
- `internal/events/event_reactor_test.go` — Reactor tests (NEW)

**Documentation:**
- `docs/SMART_POLLING.md` — How adaptive polling works (NEW)
- `docs/EVENT_DRIVEN_ROTATION.md` — Event triggering explained (NEW)

---

## Task 1: Define Event Types and Reactor Interface

**Files:**
- Create: `internal/events/event_types.go`
- Create: `internal/events/event_reactor.go` (interface only)
- Test: `internal/events/event_types_test.go`

**Full Task Details:** Define foundational event types (SecretChangeEvent, ContainerLabelEvent) with Source, Severity, Action enums. Create EventReactor interface with ProcessSecretEvent, ProcessContainerEvent, Start, Stop, IsHealthy methods. Add RotationTrigger callback type. Write 3 unit tests verifying String() methods and event structure.

**Expected Commits:** 1 commit with event types, reactor interface, and tests

---

## Task 2: Implement SmartPoller with Adaptive Intervals

**Files:**
- Create: `internal/polling/smart_poller.go`
- Create: `internal/polling/poller_state.go`
- Test: `internal/polling/smart_poller_test.go`

**Full Task Details:** Implement CalculateInterval(timeSinceChange) returning 5s for <2min, 30s for <10min, 5m for 10m+. Create SmartPoller with NewSmartPoller(), GetNextInterval(secretName), RecordChange(secretName), RecordPoll(secretName), GetStats(secretName). Create PollerState struct tracking LastChangeTime, LastPollTime, ChangeCount, PollCount. Write 5+ unit tests for interval calculation, state changes, concurrent access.

**Expected Commits:** 1 commit with smart poller implementation and tests

---

## Task 3: Implement Docker Event Listener

**Files:**
- Create: `internal/events/container_listener.go`
- Test: `internal/events/container_listener_test.go`

**Full Task Details:** Create ContainerListener with Docker client integration. Implement HasRelevantLabels() checking for 'secret', 'rotation-strategy', 'dso.*' labels. Implement DetectLabelChanges(before, after) returning changed labels. Implement NewContainerListener(), Start(ctx), watchEvents(), initializeContainers(), Events() <-chan ContainerLabelEvent. Write tests for label filtering, change detection, event emission.

**Expected Commits:** 1 commit with listener implementation and tests

---

## Task 4: Implement Event Reactor (Main Event Processing)

**Files:**
- Create: `internal/events/event_reactor_impl.go`
- Test: `internal/events/event_reactor_test.go`

**Full Task Details:** Implement EventReactorImpl satisfying EventReactor interface. Add event queue with priority sorting. Implement ProcessSecretEvent() with deduplication (skip if same secret within 1s). Implement ProcessContainerEvent() extracting secrets from labels. Implement batching: 5-second window, max 5 rotations per batch. Implement Start() spawning processBatches() goroutine, IsHealthy() checking event freshness, Stop() graceful shutdown. Write tests for batching, priority, deduplication.

**Expected Commits:** 1 commit with reactor implementation and tests

---

## Task 5: Integrate SmartPoller into Agent Loop

**Files:**
- Modify: `internal/agent/agent.go` (main loop ~lines 100-150)
- Test: `internal/agent/agent_test.go` (add test case)

**Full Task Details:** Replace fixed polling_interval with SmartPoller. Create runMainLoop() spawning SmartPoller, ContainerListener, EventReactor. For each secret, create ticker with adaptive interval. Select on: ctx.Done(), containerEvents channel, poll tickers. On poll, call pollSecret() comparing versions, record change if different. Update interval via GetNextInterval(). Handle concurrent safe ticker updates. Write integration test verifying adaptation on change.

**Expected Commits:** 1 commit with agent integration and test

---

## Task 6: Add Comprehensive Tests for Smart Polling

**Files:**
- Create: `internal/polling/smart_poller_integration_test.go`
- Modify: `internal/polling/smart_poller_test.go` (add edge cases)

**Full Task Details:** Add edge case tests: concurrent access (100 parallel reads/writes), state preservation across calls, unknown secret handling (defaults to 30s). Add integration test simulating realistic polling lifecycle. Write load test: 1000 operations on 100 secrets, verify completion within 100ms. All tests pass with -race flag.

**Expected Commits:** 1 commit with comprehensive edge case and load tests

---

## Task 7: Documentation and Monitoring

**Files:**
- Create: `docs/SMART_POLLING.md`
- Create: `docs/EVENT_DRIVEN_ROTATION.md`
- Modify: `README.md` (add smart polling section)

**Full Task Details:** Create SMART_POLLING.md explaining baseline (30s), aggressive (5s), backoff (5m) intervals with timeline example. Document 80% API call reduction calculation. Create EVENT_DRIVEN_ROTATION.md explaining two triggers (polling + events), event flow diagram, batching/dedup strategy, configuration. Add monitoring examples and health checks. Update README with smart polling feature bullet. Include performance impact metrics.

**Expected Commits:** 1 commit with documentation

---

## Task 8: Integration Tests and Performance Validation

**Files:**
- Create: `internal/integration_test/smart_polling_integration_test.go`

**Full Task Details:** Write TestSmartPolling_Integration(): verify poller adapts interval on change, event reactor processes events, batch processing works. Write TestSmartPolling_LoadTest(): 100 secrets, 1000 operations, verify <100ms completion. Write TestSmartPolling_ConcurrentStress(): concurrent polling and event processing without panics. All tests pass with -race flag.

**Expected Commits:** 1 commit with integration and performance tests

---

## Summary: Path A Complete

| Component | Files | Status |
|-----------|-------|--------|
| Event Types | event_types.go, event_reactor.go | NEW |
| SmartPoller | smart_poller.go, poller_state.go | NEW |
| Event Listener | container_listener.go | NEW |
| Event Reactor | event_reactor_impl.go | NEW |
| Agent Integration | agent.go (modify) | MODIFY |
| Tests | *_test.go files | NEW |
| Documentation | SMART_POLLING.md, EVENT_DRIVEN_ROTATION.md | NEW |

**Timeline**: 3-4 months (8 tasks)  
**User Complexity**: Minimal (just configure DSO)  
**API Call Reduction**: 80% (30s baseline → 5m backoff)  
**Production Ready**: Yes (all components tested with -race flag)
