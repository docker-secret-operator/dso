# Health Check Mechanism

## Overview

During rolling rotation (zero-downtime secret updates), DSO validates that new containers are healthy before routing traffic to them. This ensures secrets are properly loaded and the container is fully operational before the atomic swap occurs.

Health checks are critical for safe, zero-downtime rotations. A container without health checks scores -10 points in the rotation strategy decision engine, which may cause DSO to fall back to the restart strategy (brief downtime) if other risk factors are present.

## Health Check Trigger

Rolling rotation follows these sequential phases:

### Phase 1: Preparation
- Analyze container configuration to determine rotation strategy
- If score < 70 (due to missing health check or other factors): Use restart strategy instead
- If score >= 70: Proceed with rolling strategy

### Phase 2: New Container Deployment
- Create a new "shadow" container with updated secrets
- Start the shadow container alongside the original
- **At this point, the original container still handles traffic**

### Phase 3: Health Validation
- Monitor the new shadow container for health status
- If container has Docker HEALTHCHECK defined:
  - Wait for container to report "healthy" state
  - Proceed only when healthy confirmed
  - Fail immediately if container reports "unhealthy"
- If no HEALTHCHECK defined:
  - Require two consecutive stable running polls (4 seconds apart)
  - Minimum confirmation that container did not crash on startup

### Phase 4: Atomic Swap
- On successful health validation: Rename original → backup, new → original
- This atomic operation is the commitment point—before this, all changes are reversible
- If health check fails: Rollback (stop and remove) new container, keep original active

### Phase 5: Cleanup
- Stop and remove the backup container
- Rotation complete

## Implementation

### Core Health Check Logic

**File**: `internal/rotation/health_check.go`

The `WaitHealthy()` function implements the health validation:

```go
// WaitHealthy monitors the shadow instance status before cutover
// CRITICAL: Does not return success until app is actually healthy, not just running
func WaitHealthy(ctx context.Context, cli *client.Client, containerID string, timeout time.Duration) error
```

**Key behaviors**:
- Polls container state every 2 seconds
- Returns immediately on "healthy" status (if HEALTHCHECK defined)
- Returns error immediately on "unhealthy" status or container exit
- Requires stable state confirmation if no HEALTHCHECK defined

### Rolling Strategy Orchestration

**File**: `internal/rotation/rolling_strategy.go`

The `RollingStrategy.Execute()` method coordinates the full rotation lifecycle:

```go
// Execute performs an atomic blue/green swap on a single container
// 1. Prepare new container config
// 2. Create new container with temporary name
// 3. Start new container
// 4. Verify health with timeout  ← Health check occurs here
// 5. Rename old → backup, new → original (atomic swap)
// 6. Cleanup backup container
```

On health check failure, the strategy:
- Stops the new container
- Removes the new container
- Leaves original container active
- Returns error to caller for logging and retry

### Events

**File**: `internal/setup/events.go`

Health check completion is emitted as an event:

```go
EventHealthCheckCompleted EventType = "health_check_completed"
```

The CLI listens for this event and can display rotation progress in real time.

## Health Check Timeout

### Default Configuration

The health check timeout defaults to **30 seconds** and is configurable.

**File**: `pkg/config/config.go`
```go
type RotationConfigV2 struct {
    HealthCheckTimeout string `yaml:"health_check_timeout,omitempty"`
}
```

**File**: `internal/cli/setup.go` (default config template)
```yaml
rotation:
  enabled: true
  strategy: rolling
  health_check_timeout: "30s"
```

### Behavior on Timeout

If the health check does not complete within the timeout period:

- **With HEALTHCHECK defined**: Rotation fails with error:
  ```
  "rotation timed out after 30s - container has health check but never reached healthy state"
  ```
  The new container is removed and original remains active.

- **Without HEALTHCHECK**: Rotation fails with error:
  ```
  "rotation timed out after 30s - container did not reach stable running state"
  ```
  DSO was unable to confirm stable state, so falls back to restart strategy.

### Retry Behavior

When health check times out:
- If using rolling strategy: Rotation fails, operator must retry
- If using restart strategy: Container is stopped and restarted with new secrets
- Automatic retries depend on the calling code; health check itself does not retry

To avoid timeout, ensure your container's HEALTHCHECK is responsive and your service initializes quickly.

## Docker HEALTHCHECK Support

### Overview

DSO integrates with Docker's native HEALTHCHECK instruction for robust health validation.

### How It Works

1. **Container Definition**: Container includes HEALTHCHECK in Dockerfile or run config
   ```dockerfile
   HEALTHCHECK --interval=5s --timeout=3s --start-period=10s --retries=3 \
     CMD curl -f http://localhost:8080/health || exit 1
   ```

2. **Docker Execution**: Docker daemon runs the health check command periodically
   - Monitors container without requiring DSO intervention
   - Reports status: healthy, unhealthy, or starting

3. **DSO Integration**: DSO queries Docker API for health status
   ```go
   inspect.State.Health.Status  // "healthy" | "unhealthy" | "starting"
   ```

4. **Decision**: DSO only proceeds with swap if status is "healthy"

### Health Status States

- **Healthy**: Container passed health checks, safe for traffic routing
- **Unhealthy**: Health command failed, traffic will not be routed
- **Starting**: Health checks not yet conclusive, waiting for start-period to elapse
- **None**: No HEALTHCHECK defined (DSO uses fallback polling strategy)

### Best Practices

- **Define HEALTHCHECK in Dockerfile** for all services handling secrets
- **Use fast health checks**: 3-5 second timeout recommended
- **Include start-period**: Allows time for secrets to load and services to initialize
- **Test health checks locally**: Verify they work before deployment

Example with secret file health check:
```dockerfile
HEALTHCHECK --interval=5s --timeout=2s --start-period=15s --retries=2 \
  CMD test -f /run/secrets/api_key && curl -f http://localhost:8080/health || exit 1
```

## Containers Without HEALTHCHECK

### Penalty in Rotation Strategy

Containers without HEALTHCHECK definition receive a **-10 point penalty** in the scoring system:

**File**: `internal/strategy/decision_engine.go`
```go
if !result.HasHealthCheck {
    score -= 10
    reasons = append(reasons, "Lack of health check prevents safe cutover validation")
}
```

### Fallback Polling Strategy

Without HEALTHCHECK, DSO uses a fallback mechanism:

1. Polls container state every 2 seconds
2. Checks `ContainerState.Running` flag
3. Requires **two consecutive stable polls** (4 seconds apart) before declaring success
4. If container exits or restarts: Immediately reports failure

This fallback is **less safe** than Docker HEALTHCHECK because:
- Cannot distinguish between "running but starting up" and "running and ready"
- A container may crash after initialization but during the 2-second poll interval
- No application-level validation that services are actually ready

### Impact on Strategy Selection

Example score calculations:

- Web service (no health check, no other issues):
  - Score: 100 - 10 = **90** → Rolling strategy (still safe, but lower confidence)

- Service with health check + fixed port binding:
  - Score: 100 - 50 - 10 = **40** → Restart strategy (health check does not overcome port binding risk)

- Service with no health check + restart:always policy:
  - Score: 100 - 20 - 10 = **70** → Borderline; rolling (just barely)

### Recommendation

**All containers handling secrets should define HEALTHCHECK** to:
1. Maintain rolling strategy eligibility despite other risks
2. Enable application-level validation (not just process-level)
3. Provide monitoring and troubleshooting data via `docker inspect`
4. Maximize safety and confidence in zero-downtime rotations

## Health Status Monitoring

### CLI Status Output

View health status via the `dso status` command:

```bash
docker dso status              # Show current system status
docker dso status --watch      # Auto-refresh every 2 seconds
docker dso status --json       # Machine-readable output
```

**Text Output Example**:
```
┌─────────────────────────────────────────────────────────────┐
│              DSO Runtime Status                             │
├─────────────────────────────────────────────────────────────┤
│ ...
│
│ CONTAINERS                                                  │
│ ├─ postgres: ✓  (secret: db_password)                       │
│ ├─ redis:   ✓  (secret: redis_pwd)                          │
│ └─ api:     ✓  running                                      │
│
│ HEALTH: ✓ All systems nominal                              │
└─────────────────────────────────────────────────────────────┘
```

### JSON Status Output

```json
{
  "containers": [
    {
      "name": "postgres",
      "status": "healthy",
      "secrets": "db_password",
      "message": "running"
    },
    {
      "name": "redis",
      "status": "healthy",
      "secrets": "redis_pwd",
      "message": "running"
    }
  ],
  "health": "All systems nominal"
}
```

**File**: `internal/cli/status.go`

Status types include:
- `healthy`: Container passing health checks, secrets properly injected
- `unhealthy`: Failed health checks or configuration errors
- `stopped`: Container not running
- `unknown`: Unable to determine status

### Docker Inspect Direct

You can also inspect container health directly via Docker:

```bash
docker inspect <container> | jq '.State.Health'
```

Output:
```json
{
  "Status": "healthy",
  "FailingStreak": 0,
  "Log": [
    {
      "Start": "2026-07-20T10:30:15.123456789Z",
      "End": "2026-07-20T10:30:15.125123456Z",
      "ExitCode": 0,
      "Output": ""
    }
  ]
}
```

## Testing Health Checks

### Unit Tests

**File**: `internal/rotation/rolling_strategy_test.go`

Tests the atomic swap mechanism including health validation:
- Success case: Health check passes
- Failure case: Health check times out
- Edge cases: Container exits during validation, docker API errors

Run tests:
```bash
cd internal/rotation
go test -v -run TestRolling
```

**File**: `internal/rotation/lock_manager_test.go`

Tests concurrent rotation safety and health check state transitions.

### Integration Tests

**File**: `test/integration/rotation_test.go`

End-to-end rotation tests with real Docker containers:
- Verifies health check integration with Docker HEALTHCHECK
- Tests timeout behavior with slow-starting containers
- Validates atomic swap with health validation failure
- Confirms rollback on unhealthy containers

Run integration tests:
```bash
cd test/integration
go test -v -run TestRotation
```

### Manual Testing

Test health checks locally:

1. **Create test container with HEALTHCHECK**:
   ```dockerfile
   FROM alpine
   COPY app /usr/local/bin/
   HEALTHCHECK --interval=5s --timeout=2s --retries=2 \
     CMD app status
   CMD app run
   ```

2. **Start container and observe health**:
   ```bash
   docker run --name test-app test-app
   docker inspect test-app | jq '.State.Health.Status'
   ```

3. **Trigger rotation**:
   ```bash
   docker dso rotate --service test-app --secret API_KEY
   ```

4. **Watch status during rotation**:
   ```bash
   docker dso status --watch
   ```

5. **Verify health check was invoked**: Check Docker logs
   ```bash
   docker logs test-app | grep -i health
   ```

## Summary

| Aspect | Details |
|--------|---------|
| **Default Timeout** | 30 seconds (configurable) |
| **HEALTHCHECK Support** | Docker native integration via API |
| **Fallback Strategy** | Two consecutive stable polls (4s total) without HEALTHCHECK |
| **Score Penalty** | -10 points without HEALTHCHECK |
| **Impact** | May trigger restart strategy if combined with other risk factors |
| **Event Type** | `EventHealthCheckCompleted` |
| **Implementation** | `internal/rotation/health_check.go`, `internal/rotation/rolling_strategy.go` |
| **Configuration** | `pkg/config/config.go`, rotation.health_check_timeout |
| **Monitoring** | `dso status` command, Docker inspect API |

Health checks are essential for safe, zero-downtime secret rotations. Always define HEALTHCHECK in your containers for the best security and reliability.
