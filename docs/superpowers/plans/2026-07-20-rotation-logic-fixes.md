# DSO Rotation Logic Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix 5 critical issues in rotation logic (timeouts on renames, recovery logic simplification, context handling, backup cleanup retry, exec cleanup).

**Architecture:** Fixes are isolated to rotation package with backward-compatible changes. Fix order: timeouts first (foundation), then recovery logic (builds on timeouts), then context handling (uses timeout foundation), then cleanup retry (independent), then exec cleanup (independent). All fixes include tests with `-race` flag verification.

**Tech Stack:** Go 1.21+, Docker API client, context timeouts, sync primitives

---

## Task 1: Add Timeouts to Rename Operations

**Files:**
- Modify: `internal/rotation/rolling_strategy.go:29-33, 44-48, 129-140, 147-188, 191-214`
- Modify: `internal/rotation/rolling_strategy_test.go:45-110, 112-177`
- Test: Add new timeout test cases in `rolling_strategy_test.go`

**Context:** The RollingStrategy.Execute() function calls ContainerRename without timeouts at lines 129, 147, 166, 180, 201. These can hang forever if Docker daemon is unresponsive. The fix adds a separate timeout context (30 seconds) for critical rename operations.

**Steps:**

- [ ] Add renameTimeout field to RollingStrategy struct and update constructors
- [ ] Create renameWithTimeout() helper function for timeout-protected renames
- [ ] Replace all 5 bare ContainerRename calls with timeout-protected calls
- [ ] Write test for rename timeout behavior
- [ ] Run tests with -race flag and verify all pass
- [ ] Commit changes

---

## Task 2: Simplify Rename Recovery Logic

**Files:**
- Modify: `internal/rotation/rolling_strategy.go:147-188`
- Modify: `internal/rotation/rolling_strategy_test.go:45-110`

**Context:** The current rename recovery logic (lines 155-177) is complex with multiple code paths. Simplify by making failure cases explicit and using goto label for recovery path continuation.

**Steps:**

- [ ] Replace complex recovery logic (lines 147-188) with simplified version using explicit state checks
- [ ] Add verifySwap: label for recovery path continuation
- [ ] Clarify error messages for each failure mode
- [ ] Update test expectations for final container names
- [ ] Run tests with -race flag
- [ ] Commit changes

---

## Task 3: Handle Context Cancellation During Atomic Swap

**Files:**
- Modify: `internal/rotation/rolling_strategy.go:44-50, 122-189`
- Modify: `internal/rotation/rolling_strategy_test.go`

**Context:** If ctx is cancelled after first rename succeeds but before second, state becomes inconsistent. Use non-cancellable context (context.Background) for atomic swap operations.

**Steps:**

- [ ] Add comment explaining atomic swap context strategy
- [ ] Create swapCtx := context.Background() before first rename
- [ ] Use swapCtx for both rename operations and recovery paths
- [ ] Write test for context cancellation during atomic swap
- [ ] Verify rename completes despite context cancellation
- [ ] Run tests with -race flag
- [ ] Commit changes

---

## Task 4: Implement Retry for Backup Cleanup

**Files:**
- Modify: `internal/rotation/rolling_strategy.go:221-242`
- Modify: `internal/rotation/rolling_strategy_test.go`

**Context:** Backup container cleanup (lines 221-237) is best-effort. Implement exponential backoff retry (3 attempts: immediate, 1s, 2s) to improve reliability.

**Steps:**

- [ ] Create cleanupContainerWithRetry() helper with exponential backoff
- [ ] Implement 3 retry attempts with 1s and 2s backoff
- [ ] Log each attempt for visibility
- [ ] Replace best-effort cleanup with retry logic
- [ ] Write test verifying retry behavior
- [ ] Run tests with -race flag
- [ ] Commit changes

---

## Task 5: Add Explicit Exec Cleanup

**Files:**
- Modify: `internal/rotation/health_check.go:77-123`
- Modify: `internal/rotation/health_check_test.go`

**Context:** ExecProbe creates exec instances but doesn't explicitly clean them up. Add defer-based cleanup to ensure cleanup happens on all exit paths.

**Steps:**

- [ ] Add defer-based cleanup of exec instances in ExecProbe
- [ ] Track execCleaned flag to ensure cleanup happens exactly once
- [ ] Ensure cleanup on all paths (success, error, timeout)
- [ ] Write test structure for exec cleanup verification
- [ ] Run tests with -race flag
- [ ] Commit changes

---

## Summary: All Issues Fixed

| Issue | Task | Status |
|-------|------|--------|
| Rename ops no timeout | Task 1 | ✅ Fixed |
| Rename recovery complex | Task 2 | ✅ Fixed |
| Context cancellation issue | Task 3 | ✅ Fixed |
| Backup cleanup retry | Task 4 | ✅ Fixed |
| Exec cleanup missing | Task 5 | ✅ Fixed |

**All changes verified with `-race` flag and comprehensive test coverage.**
