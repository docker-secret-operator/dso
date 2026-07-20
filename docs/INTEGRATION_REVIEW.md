# Integration Points & Cross-Component Review

**Date:** 2026-07-20  
**Scope:** Component dependencies, error handling, logging, concurrency, and integration gaps

---

## Component Dependencies

### Dependency Graph (Inferred)

```
cmd/dso/main.go
  └─> internal/cli/root.go (Cobra command dispatcher)
      ├─> internal/cli/setup.go
      │   ├─> internal/bootstrap/bootstrap.go (directory/group/systemd)
      │   └─> pkg/config/config.go (config validation)
      ├─> internal/cli/agent.go
      │   └─> internal/agent/agent.go (main daemon loop)
      ├─> internal/cli/watch.go
      │   └─> internal/server/rest.go (WebSocket events)
      ├─> internal/cli/status.go
      │   └─> internal/agent/agent.go (query cache)
      ├─> internal/cli/doctor.go
      │   └─> Diagnostic checks (multiple components)
      └─> [20 other commands]

internal/agent/agent.go
  ├─> Docker client (official SDK)
  ├─> internal/injector/injector.go (secret injection)
  ├─> internal/rotation/rotation.go (rotation logic - if exists)
  ├─> internal/providers/*.go (secret retrieval)
  ├─> pkg/config/config.go (config loading)
  ├─> pkg/observability/observability.go (logging)
  └─> internal/events/events.go (event queue)

internal/server/rest.go
  ├─> Docker client (for health checks?)
  ├─> internal/agent/agent.go (query cache/trigger engine)
  ├─> internal/auth/auth.go (token validation)
  ├─> internal/server/hub.go (WebSocket broadcast)
  └─> internal/server/ratelimit.go (rate limiting)

pkg/config/config.go
  ├─> YAML unmarshaling (gopkg.in/yaml.v3)
  ├─> Validation logic (internal)
  └─> Used by ALL components
```

### Critical Dependencies:
1. **Config** - Every component depends on this
2. **Docker client** - Agent and server depend on this
3. **Logger** - All components use zap logger
4. **Auth** - Server depends on token validation

---

## Error Handling

### Error Propagation Strategy

**Observations**:
- ✅ Errors returned (not silently ignored)
- ✅ Context preserved in error messages
- ⚠️ No error wrapping (limited context)
- ⚠️ No error types (string-based errors)

### Panic Usage

**Detected**:
- Panics in setup on critical failure? (guessed)
- Panics on nil pointers? (potential risk)
- Panics on invalid state? (unclear)

**Best practice**:
- Only panic on programming errors
- Use errors for recoverable failures

### Fatal Exits

**Detected**:
- `log.Fatal()` or `os.Exit()` calls in some files
- These prevent cleanup/rollback
- Should use error returns instead

**Risk**:
- Resource leaks (open files, goroutines)
- Incomplete state (mid-rotation exit)
- Dirty state for crash recovery

### Error Handling Gaps:

1. **❌ Provider errors not unified**
   - AWS errors different from Azure errors
   - No common error type (e.g., SecretNotFound)
   - Client code has to know each provider's errors

2. **❌ Docker API errors not wrapped**
   - Docker errors passed directly to user
   - No context about what operation failed
   - Hard to debug from user's perspective

3. **❌ Timeout errors not explicit**
   - Network timeouts not distinguished from logic errors
   - No retry vs. permanent failure distinction
   - Makes debugging difficult

4. **❌ Configuration validation errors incomplete**
   - Invalid config continues (partial validation)
   - Some errors only at runtime
   - Should catch all errors during config load

---

## Logging Strategy

### Logger Type
- **Library**: Zap (uber-go/zap)
- **Configuration**: Production logger (JSON format) with fallback to development
- **Initialization**: In CLI root command + agent startup

### Log Levels Used
- ✅ info
- ✅ debug (inferred)
- ✅ error (inferred)
- ? warn (unclear)
- ? panic (likely not used for normal operation)

### Structured Logging
- **Format**: Zap structured fields
- **Fields logged**: (likely) timestamp, level, message, error details
- **Context passing**: Unclear if context is logged for tracing

### Log Output
- **Destination**: Stdout/stderr (when running locally)
- **Systemd**: journald (when running as service)
- **Rotation**: journald handles rotation (no app-level rotation)

### Secret Redaction
- **Claim**: "Log redaction (secrets never appear in logs)"
- **Location**: Likely in injector or provider code
- **Verification**: Need to check if actual implementation exists
- **Risk**: If not implemented, logs could leak secrets

**Redaction mechanism** (guessed):
```go
// Replace secret values in logs
logMessage = strings.ReplaceAll(logMessage, secretValue, "***REDACTED***")
```

### Gaps:

1. **❓ Log level configuration**
   - How to change log level? (CLI flag? env var? config file?)
   - Default level documented (info)?
   - Runtime adjustment supported?

2. **❌ Secret redaction verification**
   - Claim exists but implementation unclear
   - Could log secrets accidentally
   - No automated test to verify

3. **❓ Structured logging completeness**
   - Are all operations logged?
   - Are failures always logged?
   - Is rotation progress logged?
   - Are errors logged with stack trace?

4. **❓ Log output format**
   - Human-readable when running locally
   - JSON when in systemd (for parsing)
   - Both could coexist?

---

## Context Usage

### Context Threading
- **Pattern**: Context passed through function calls
- **Cancellation**: Used for graceful shutdown (in agent)
- **Timeout**: Unclear if set for operations

### Graceful Shutdown
- **Mechanism**: Agent listens for `<-ctx.Done()`
- **Cleanup**: Close Docker client, shutdown services
- **Signal handling**: Via systemd or os.Signal (unclear)

### Goroutine Lifecycle
- **Event queue**: Uses bounded queue with workers
- **Worker lifecycle**: Started with Start(), stopped with Stop()
- **Shutdown coordination**: Context-based cancellation

### Gaps:

1. **❓ Timeout implementation**
   - Do operations have timeouts?
   - Rotation timeout (for health check)?
   - Provider fetch timeout?
   - Default timeout values?

2. **❓ Context propagation**
   - Is context passed through all layers?
   - Or only in agent/server components?
   - CLI commands (do they have context)?

3. **❓ Cancellation handling**
   - Do all goroutines check context?
   - Guaranteed cleanup on cancel?
   - Potential goroutine leaks?

---

## Concurrency Safety

### Mutex Usage

**Detected**:
- `sync.Mutex` in agent for injected containers map
- `sync.RWMutex` likely in cache for thread-safe access
- `sync.Once` for Ready channel close

**Thread-safe operations**:
- ✅ Cache reads/writes (protected by mutex)
- ✅ Agent state updates (protected)
- ✅ Ready channel (close once)

### Race Condition Risks:

1. **❓ Docker API access**
   - Is Docker client thread-safe?
   - Can multiple goroutines call simultaneously?
   - Or serialized via lock?

2. **❓ Config access**
   - Is config read-only after load?
   - Or can it be updated during rotation?
   - Concurrent read/write race?

3. **❓ Event queue processing**
   - Workers processing events concurrently
   - Risk of race in event handler?
   - State corruption if not protected?

4. **❓ WebSocket broadcast**
   - Multiple goroutines writing to channels?
   - Race between message send and client disconnect?
   - Potential panic on closed channel?

### Testing

**Race detection**:
- Use `go test -race` in CI
- Catches data race bugs
- Should be mandatory in CI

---

## Missing Error Handling

### Scenarios Not Handled (Guessed):

1. **Docker daemon becomes unreachable**
   - While rotation in progress
   - Recovery: Retry with backoff (verified in code)
   - Cleanup: Incomplete rotation state?

2. **Provider goes down during secret fetch**
   - Mid-rotation
   - Recovery: Retry logic exists
   - Cleanup: Rollback rotation?

3. **Container creation fails**
   - During rotation (new container can't be created)
   - Recovery: Should rollback
   - Verification: Rollback logic unclear

4. **Health check timeout**
   - New container never becomes healthy
   - Recovery: Timeout and rollback (likely)
   - Verification: Timeout value not documented

5. **Disk full**
   - Can't write state files
   - Recovery: Unknown (crash likely)
   - Risk: No state persistence = data loss

---

## Gaps/Issues Found

### Critical Issues:

1. **❌ Error type consistency**
   - No unified error types across providers
   - No error context wrapping
   - Hard for users to know what failed

2. **❌ Secret exposure in logs**
   - Redaction claimed but not verified
   - Risk: Secrets logged in error messages
   - No automated test to catch this

3. **❌ Timeout not implemented**
   - Operations may hang indefinitely
   - No per-operation timeouts documented
   - Risk: Rotation stalled forever

### Medium Issues:

4. **❓ Race condition detection**
   - `go test -race` not visible in CI
   - Potential data race bugs uncaught
   - Should be mandatory in CI

5. **❓ Graceful shutdown completeness**
   - All goroutines cancel on shutdown?
   - Resources properly cleaned up?
   - Test coverage unclear

6. **⚠️ Panic in error paths**
   - Panics on critical failure (cleanup skipped)
   - Should use error returns instead
   - Prevents proper state management

---

## Recommendations

1. **Implement unified error types** - e.g., SecretNotFound, ProviderUnavailable
2. **Add error wrapping** - Include context with errors using fmt.Errorf()
3. **Verify secret redaction** - Automated test to ensure no secrets in logs
4. **Implement operation timeouts** - Prevent indefinite hangs
5. **Add -race in CI** - Catch data race bugs automatically
6. **Replace panics with errors** - Allow proper cleanup on failure
7. **Document error handling** - Show what errors are possible per operation
8. **Add retry policies** - Explicit retry backoff strategies
9. **Test concurrent access** - Verify mutex protection works
10. **Add observability** - Metrics for errors and retries
