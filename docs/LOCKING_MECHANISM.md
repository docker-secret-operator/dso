# Distributed Locking Mechanism

## Overview

DSO uses a **two-tier locking strategy** to prevent concurrent rotations of the same container or secret:

1. **In-memory locks** (sync.Mutex) — Goroutine-level synchronization within a single process
2. **File-based advisory locks** (flock) — Cross-process synchronization for distributed environments

This ensures that only one rotation operation proceeds at a time for a given container, preventing:
- Duplicate configuration changes
- Race conditions during container state transitions
- Concurrent modifications to secrets or environment variables
- Atomic swap failures due to competing updates

## Locking Strategy

### Two-Tier Approach

DSO combines in-memory and file-based locking to handle both single-process and multi-process scenarios:

#### Tier 1: In-Memory Locks

- **Implementation**: `sync.Mutex` per container/secret key
- **Scope**: Single process (all goroutines)
- **Overhead**: Minimal; no filesystem I/O
- **Use case**: Preventing goroutine races within the same DSO daemon

Each unique container ID or secret name gets its own `sync.Mutex` on first acquisition. The `LockManager` maintains a map of these mutexes and tracks which keys are currently locked.

#### Tier 2: File-Based Locks

- **Implementation**: `flock(2)` (kernel-managed advisory locking)
- **Location**: `${RUNTIME_DIR}/<container-id>.lock` (e.g., `/var/run/dso/my-app.lock`)
- **Scope**: Cross-process; works across multiple DSO instances
- **Atomicity**: Guaranteed by the kernel; no TOCTOU (check-then-act) window
- **Auto-release**: If the holding process dies, the kernel automatically releases the lock

The file-based lock is acquired **after** the in-memory lock, creating a critical section that is protected at both the goroutine and process level.

### Lock Acquisition Flow

When a rotation is triggered for a container:

```
1. Rotation triggered
   └─ Change detected or manual trigger received

2. Lock manager invoked
   └─ AcquireLock(containerID, timeout)

3. In-memory lock phase
   ├─ Try immediate lock with sync.Mutex.TryLock()
   │  └─ Returns immediately if available
   │
   └─ If blocked, poll with exponential backoff
      ├─ Initial backoff: 1ms
      ├─ Max backoff: 25ms
      ├─ Deadline: current_time + timeout
      └─ Retry every backoff interval until timeout

4. File-based lock phase (if lockDir configured)
   ├─ Open/create lock file: ${lockDir}/<sanitized-name>.lock
   ├─ Attempt flock(fd, LOCK_EX | LOCK_NB) — exclusive, non-blocking
   │
   └─ If EWOULDBLOCK (held by another process)
      ├─ Poll every 50ms until deadline
      ├─ If timeout: return error
      └─ Kernel auto-releases on process death (no manual cleanup needed)

5. Lock acquired — rotation proceeds
   └─ Log entry: "Acquired distributed lock"

6. Rotation executes (critical section)
   ├─ Inspect container
   ├─ Prepare new config
   ├─ Create temp container
   ├─ Start and health-check
   └─ Atomic rename swap
      (old → backup, new → original)

7. Lock released
   ├─ ReleaseLock(containerID)
   ├─ sync.Mutex.Unlock()
   └─ flock(fd, LOCK_UN) + close(fd)
      (file remains; flock status removed)
```

### Backoff and Timeout Behavior

**In-Memory Lock Acquisition** (sync.Mutex.TryLock)

- Uses exponential backoff if the lock is held:
  - 1ms → 2ms → 4ms → 8ms → 16ms → 25ms (caps at 25ms)
- Polls until the deadline is reached
- Default timeout: **1 second** (configurable per call)

**File-Based Lock Acquisition** (flock)

- Uses fixed 50ms sleep between polls
- Timeout inherited from caller
- If holder crashes:
  - Kernel automatically releases the lock
  - No stale-lock heuristic or cleanup required
  - Next process acquires immediately on next poll attempt

Example:
```
AcquireLock("api-server", 5*time.Second)

If in-memory mutex is held:
- T=0ms: Try → blocked
- T=1ms: Try → blocked
- T=2ms: Try → blocked
- ...
- T=4900ms: Try → blocked
- T=5000ms: Timeout → error

If file lock is held by a live process:
- flock(fd, LOCK_EX|LOCK_NB) → EWOULDBLOCK
- Sleep 50ms
- Retry until deadline
- If deadline passed: error

If file lock holder crashes mid-rotation:
- Kernel drops lock from dead PID's file descriptor
- Next poll attempt succeeds immediately
- Rotation proceeds (risk: previous holder was mid-operation)
```

## Concurrency Scenarios

### Same Container, Sequential Rotations

```
Process A acquires lock for "web-app"
├─ Runs rotation
├─ Releases lock

Process B acquires lock for "web-app"
├─ Runs rotation
└─ Releases lock

✓ Serialized — no concurrent writes to "web-app"'s state
```

### Different Containers, Parallel Rotations

```
Process A acquires lock for "web-app"      Process B acquires lock for "api-server"
├─ Runs rotation for web-app      ║      ├─ Runs rotation for api-server
├─ Releases after 2 seconds        ║      ├─ Runs rotation for api-server
└─ Done                            ║      └─ Done (simultaneous with A)

✓ Parallel — separate containers have independent locks
```

### Single Container, Multiple Waiting Processes

```
Process A: AcquireLock("shared", 5s) → succeeds immediately
Process B: AcquireLock("shared", 5s) → blocks in flock()
Process C: AcquireLock("shared", 5s) → blocks in flock()

A finishes rotation, releases lock (after 1.2s)
├─ B's flock() retry succeeds → B proceeds
└─ C waits for B to finish

C's turn: AcquireLock("shared", 5s) → succeeds after A and B complete
```

### Multiple DSO Agents (Distributed)

**Note**: DSO is designed as a **single-host operator** but can be deployed on multiple hosts with shared storage. In a distributed setup:

- **Same lock directory** (NFS-mounted, for example):
  - File-based locks work across network (flock on NFS)
  - Performance may degrade (network latency + NFS flock overhead)
  - Recommended: Reserve for high-availability clusters only

- **Separate lock directories** (common):
  - Each host has its own `/var/run/dso/` (local filesystem)
  - In-memory locks are process-local only
  - **Risk**: Two DSO instances on different hosts can rotate the same container concurrently
  - Mitigation: Use external locking service (Consul, etcd) or single-host deployment

**Best practice**: Deploy DSO on a single management host per cluster, or use shared distributed lock service if redundancy is required.

## Lock Timeout Handling

### Default Timeout

The timeout is configurable per `AcquireLock()` call. Typical values:

- **Rotation timeout**: 5-10 seconds (chosen by the caller)
- **Health check timeout**: 30 seconds (from rolling_strategy.go)

### Timeout Expiration

If a lock cannot be acquired within the timeout:

```
Process A holds lock for "web-app" (acquired 30s ago)
Process B tries: AcquireLock("web-app", 5s)

After 5s:
├─ B's deadline reached
├─ Error returned: "timeout acquiring distributed lock for web-app"
├─ B does NOT forcibly remove A's lock
├─ A continues to hold the lock
└─ B must retry or fail the operation

Options for B:
1. Log warning and skip rotation (retry next cycle)
2. Retry with longer timeout
3. Fail the operation and alert operator
```

### Handling Stale Locks (Process Crash)

**Scenario**: Process A crashes while holding a lock

```
Process A acquires flock for "web-app"
Process A crashes (SIGKILL, power loss, etc.)

Kernel action:
├─ File descriptor is closed
├─ flock is automatically released by kernel
└─ Lock file remains on disk (not deleted)

Process B attempts: AcquireLock("web-app", 5s)
├─ flock(fd, LOCK_EX|LOCK_NB) → succeeds immediately
├─ Lock acquired (kernel cleaned up A's lock)
└─ Rotation proceeds

Risk: If A crashed mid-rotation:
├─ Container state may be partially updated
├─ Temporary containers ("_dso_new_", "_dso_backup_") may exist
├─ B's rotation may encounter conflicts
└─ Doctor checks detect and repair orphaned state
```

The **Doctor engine** (DSO-DOCTOR-012/013) detects stale lock files and orphaned container state:

- **DSO-DOCTOR-012**: Runtime directory exists and is accessible
- **DSO-DOCTOR-013**: No stale lock files in the runtime directory

When stale locks are detected, the Repair engine offers to:
1. Remove orphaned lock files
2. Clean up temporary containers (if present)
3. Resume rotations from a known state

## Lock File Locations and Names

### Lock Directory

- **Default**: `/var/run/dso/` (or configured via `--lock-dir`)
- **Permissions**: `0700` (owner only; prevents unprivileged processes from interfering)
- **Created by**: LockManager on first use

### Lock File Format

Lock files use a sanitized form of the container ID to avoid path traversal:

```
Container ID:  api-server
Sanitized:     api-server (no special chars)
Lock file:     /var/run/dso/api-server.lock

Container ID:  my-app/prod (contains /)
Sanitized:     my-app_prod (replace / with _)
Lock file:     /var/run/dso/my-app_prod.lock

Container ID:  ../escape (path traversal attempt)
Sanitized:     __escape (.. replaced with __)
Lock file:     /var/run/dso/__escape.lock
```

**Sanitization rules** (from `sanitizeLockName()`):
- `/` → `_`
- `\` → `_`
- `..` → `__`
- OS path separator → `_`

This prevents malicious container IDs from creating locks outside the lock directory.

### Lock File Content

When a lock is acquired, the lock file stores the PID of the lock holder:

```bash
$ cat /var/run/dso/web-app.lock
12345
```

This aids debugging:
- Operator can identify which process holds a lock
- Can verify process is still running: `ps 12345`
- Can safely kill a hung process if needed

On release, the file is truncated (not deleted) to avoid inode-reuse races with flock.

## Deadlock Detection and Recovery

### Signs of Deadlock or Lock Contention

1. **Rotation takes > 2 minutes** (normal: 30 seconds)
   - Indicates lock was not acquired or rotation is stalled
   - Check logs for "timeout acquiring" errors

2. **Multiple pending rotations** in logs
   ```
   ERROR: timeout acquiring distributed lock for web-app
   ERROR: timeout acquiring distributed lock for api-server
   ```
   - Suggests one process is holding locks for a long time
   - Or previous process crashed without releasing

3. **"Stale lock files" warning** from Doctor engine
   - DSO-DOCTOR-013: N lock file(s) found
   - Indicates previous DSO process exited without cleanup

4. **Lock file exists but process is dead**
   ```bash
   $ cat /var/run/dso/web-app.lock
   45678
   $ ps 45678
   # (no output — process does not exist)
   ```

### Recovery Procedure

#### Option 1: Safe Recovery (Recommended)

1. **Run the Doctor engine**:
   ```bash
   docker dso doctor
   ```
   - Detects DSO-DOCTOR-013 (stale locks)
   - Shows PID of lock holder (if still in lock file)

2. **Run the Repair engine**:
   ```bash
   docker dso doctor --repair
   ```
   - Prompts for confirmation before removing locks
   - Automatically cleans up temporary containers
   - Verifies DSO is not running before proceeding

3. **Resume rotations**:
   ```bash
   docker dso rotate
   ```
   - Locks are now available
   - Rotation proceeds normally

#### Option 2: Force Release (Use with Caution)

If Doctor/Repair is unavailable or permissions don't allow:

```bash
# 1. Verify DSO is not running
systemctl status dso

# 2. Verify the lock holder process is dead
PID=$(cat /var/run/dso/web-app.lock 2>/dev/null)
ps $PID  # Should have no output

# 3. Remove the lock file
sudo rm /var/run/dso/web-app.lock

# 4. Verify cleanup
ls /var/run/dso/*.lock  # Should be empty or have fewer files

# 5. Start DSO and retry rotation
systemctl start dso
docker dso rotate web-app
```

**WARNING**: Force removal while DSO is running can cause concurrent rotations. Always verify DSO has stopped first.

#### Option 3: Timeout-Based Self-Recovery

DSO includes timeout protection to avoid indefinite hangs:

1. **Lock timeout**: If a lock is held > 5 seconds, the next process trying to acquire times out
2. **Rotation timeout**: If rotation > 30 seconds, the operation is cancelled
3. **Health check timeout**: If new container health check > 30 seconds, rotation is aborted

This means stale locks automatically become invisible after the timeout expires, allowing the next operation to proceed.

## Testing Locking Behavior

The locking mechanism includes comprehensive test coverage. See `/internal/rotation/lock_manager_test.go`:

### In-Memory Lock Tests

```go
// TestLockManager_AcquireRelease_InMemory
// Verifies basic lock acquire and release work
lm.AcquireLock("svc-a", time.Second)
lm.ReleaseLock("svc-a")

// TestLockManager_Contention_TimesOut
// Verifies timeout when lock is held
lm.AcquireLock("svc-b", time.Second)
err := lm.AcquireLock("svc-b", 50*time.Millisecond)  // times out
// err != nil ✓

// TestLockManager_ConcurrentMutualExclusion
// 20 goroutines contend for same lock; verifies serialization
// Run with: go test -race
```

### File-Based Lock Tests

```go
// TestFileLock_AcquireRelease
// Verifies flock file is created and can be re-acquired after release

// TestFileLock_CrossHandleContention
// Two LockManager instances, same lock directory
// Verifies flock prevents concurrent acquisition across processes

// TestLockManager_IdempotentRelease
// Verifies ReleaseLock is safe even if never acquired
// (Prevents panics in defer statements)
```

### Running the Tests

```bash
# Run all lock tests
go test ./internal/rotation -run Lock -v

# Run with race detection
go test ./internal/rotation -run Lock -race

# Run in verbose mode to see timing
go test ./internal/rotation -run TestLockManager_Contention -v -timeout 30s
```

### Key Test Scenarios

| Test | Validates |
|------|-----------|
| `TestLockManager_AcquireRelease_InMemory` | Basic lock lifecycle (acquire → release → re-acquire) |
| `TestLockManager_Contention_TimesOut` | Lock timeout behavior and independent key handling |
| `TestFileLock_AcquireRelease` | File lock creation and release (flock dropping) |
| `TestFileLock_CrossHandleContention` | Cross-process mutual exclusion via flock |
| `TestLockManager_ConcurrentMutualExclusion` | 20 concurrent goroutines; verifies no data races |
| `TestLockManager_IdempotentRelease` | Safe double-release (no panic, no double-unlock) |
| `TestTryLockWithTimeout` | Exponential backoff timing and deadline behavior |
| `TestSanitizeLockName` | Path traversal protection (sanitization) |

## Implementation Details

### Architecture

```
LockManager (public API)
├─ sync.Mutex per container (in-memory)
├─ acquired map[string]bool (tracks held locks)
└─ FileLock (optional, cross-process)
    └─ flock(2) per container
```

### Key Invariants

1. **Exclusivity**: Only one goroutine/process can hold a lock for a given key
2. **Non-blocking**: `TryLock()` never blocks; timeouts use polling
3. **Idempotent release**: `ReleaseLock()` is safe even if never acquired
4. **Leak prevention**: No goroutines are leaked on timeout (CQ-C1)
5. **Atomicity**: flock is atomic; no TOCTOU window for stale locks (CQ-H6)

### Performance Characteristics

| Operation | Latency | Notes |
|-----------|---------|-------|
| Acquire (uncontended) | < 1ms | TryLock succeeds immediately |
| Acquire (contended, 5s timeout) | ~5s | Polls with exponential backoff |
| Acquire (file lock) | +10-50ms | flock overhead + 50ms poll interval |
| Release | < 1ms | Unlock + file close |

### Goroutine Leaks

**Previous issue (CQ-C1, fixed)**:

Old implementation spawned a goroutine that called `mutex.Lock()` and raced it against `time.After()`. On timeout, the goroutine leaked forever, blocked on a mutex that was never unlocked.

**Current fix**:
- Uses `TryLock()` which never blocks
- Polls in the caller's goroutine
- No goroutine leaks on timeout

## Security Considerations

### Lock File Permissions

- Lock files are created with `0600` (owner only)
- Lock directory is `0700` (owner only)
- Prevents unprivileged processes from viewing/modifying lock state

### Path Traversal Prevention

- Lock names are sanitized before use as filenames
- `..`, `/`, and backslashes are replaced with `_`
- `filepath.Base()` ensures lock file stays within lock directory

### Lock Holder Identification

- Lock file stores PID for debugging
- Operator can verify process legitimacy before force-releasing
- No plaintext secrets stored in lock file

## See Also

- [DOCTOR_ENGINE.md](DOCTOR_ENGINE.md) — Stale lock detection and diagnosis
- [REPAIR_ENGINE.md](REPAIR_ENGINE.md) — Stale lock removal and recovery
- [ROTATION_STRATEGY.md](ROTATION_STRATEGY.md) — Where locks are acquired/released
- `/internal/rotation/lock_manager.go` — Source implementation
- `/internal/rotation/lock_manager_test.go` — Test suite
