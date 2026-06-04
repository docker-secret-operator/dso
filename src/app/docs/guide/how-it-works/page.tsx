import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "How DSO Works",
  description: "Complete rotation workflow, state transitions, health validation, and recovery paths in Docker Secret Operator."
};

export default function HowItWorksPage() {
  return (
    <div>
      <h1>How Secret Rotation Works</h1>

      <p>
        This guide explains the complete workflow for secret rotation in DSO, including all state transitions, health validation, atomic operations, and recovery paths. Understanding this helps you predict system behavior and debug issues.
      </p>

      <h2>High-Level Flow</h2>

      <pre><code className="language-text">
Secret Changes → Detect → Lock → Create → Validate → Swap → Cleanup → Success
                                           ↑         ↓
                                        Fail → Rollback → Recovery
      </code></pre>

      <h2>Stage 1: Detection (Poll or Webhook)</h2>

      <h3>Local Mode: No Detection</h3>

      <p>
        In Local mode, rotation is explicitly triggered:
      </p>

      <pre><code className="language-bash">
docker dso up
      </code></pre>

      <p>
        CLI immediately proceeds to Stage 2 (Lock).
      </p>

      <h3>Agent Mode: Event Detection</h3>

      <pre><code className="language-text">
Polling Mode (Interval-Based):
1. Every N seconds (5-60s, configurable):
   a. Request current secrets from provider
   b. Hash secrets
   c. Compare with last known hash
   d. If changed: Proceed to Stage 2

Webhook Mode (Event-Driven):
1. Provider sends webhook when secret changes
2. DSO receives webhook at /webhook endpoint
3. Verify webhook signature (provider-specific)
4. Immediately proceed to Stage 2

Timing:
• Polling: Rotation happens within 5-60 seconds of change
• Webhook: Rotation happens within 1-5 seconds of change
      </code></pre>

      <h3>Change Detection Logic</h3>

      <pre><code className="language-bash">
Current secrets hash = sha256(DATABASE_PASSWORD + API_KEY + ...)
Previous hash = Read from checkpoint file

If current hash != previous hash:
  → Rotation needed
  → Schedule rotation
  → Update detection timestamp
Else:
  → No change, skip rotation
      </code></pre>

      <h2>Stage 2: Lock Acquisition</h2>

      <h3>Purpose</h3>

      <p>
        Prevent concurrent rotations from two agents (multi-host scenarios). Ensure only one rotation happens at a time.
      </p>

      <h3>Lock Acquisition Process</h3>

      <pre><code className="language-text">
1. Attempt to create lock file: /var/lib/dso/rotation.lock
2. Write content:
   {
     "agent_id": "host-1.prod",
     "pid": 12345,
     "timestamp": "2025-03-15T10:30:00Z",
     "ttl_seconds": 300
   }
3. If file exists:
   a. Read existing lock
   b. Check age (current - timestamp)
   c. If age < TTL: WAIT (exponential backoff, max 30s)
   d. If age >= TTL: FORCE RELEASE (assume dead agent)
   e. Retry acquisition
4. Success: Lock acquired, proceed to Stage 3
5. Timeout: Wait exceeded 30s, give up, retry later
      </code></pre>

      <h3>Lock Timing</h3>

      <ul>
        <li><strong>TTL:</strong> 300 seconds (5 minutes) typical</li>
        <li><strong>Acquisition Timeout:</strong> 30 seconds (abort if can't acquire)</li>
        <li><strong>Stale Check Interval:</strong> Every 5 minutes (detect dead agents)</li>
      </ul>

      <h2>Stage 3: New Container Creation</h2>

      <h3>Step 1: Load Configuration</h3>

      <pre><code className="language-text">
1. Read docker-compose.yml
2. Load secrets from provider (fresh fetch)
3. Substitute environment variables:
   - Load: DATABASE_PASSWORD = "new-secret-abc123"
   - In compose: environment: { DATABASE_PASSWORD: "${DATABASE_PASSWORD}" }
   - Result: environment: { DATABASE_PASSWORD: "new-secret-abc123" }
4. Prepare container spec
      </code></pre>

      <h3>Step 2: Create Container with Updated Secrets</h3>

      <pre><code className="language-text">
Naming Strategy:
• Original name: "api"
• New container: "api.new"
• Old container (if rotation fails): "api.old"

Injection Method:
1. Secrets loaded into agent memory
2. Passed to Docker CLI via tmpfs mount
3. Example: docker run --tmpfs /run/secrets:size=10m
4. Secrets written to tmpfs (RAM-backed, never disk)
5. Container reads from /run/secrets/* files
6. After rotation: tmpfs unmounted, secrets cleared

Creation Command Pattern:
docker create \
  --name api.new \
  --env DATABASE_PASSWORD=new-secret-abc123 \
  --health-cmd="curl http://localhost:3000/health" \
  --health-interval=10s \
  --health-timeout=5s \
  --health-retries=3 \
  myimage:latest

Important: No --detach flag here (container not started yet)
      </code></pre>

      <h3>Step 3: Start Container</h3>

      <pre><code className="language-text">
docker start api.new

Container starts:
• Secrets available in memory
• Network interfaces configured
• Logging initialized
• Ready for health checks
      </code></pre>

      <h2>Stage 4: Health Validation</h2>

      <h3>Health Check Extraction</h3>

      <pre><code className="language-text">
From docker-compose.yml:
healthcheck:
  test: ["CMD", "curl", "-f", "http://localhost:3000/health"]
  interval: 10s
  timeout: 5s
  retries: 3
  start_period: 30s

Extract: test command, timeout, retries
      </code></pre>

      <h3>Validation Process</h3>

      <pre><code className="language-text">
Timeline for container "api.new":

t=0s: Container starts
      • Secrets injected
      • Initialization begins
      • PID allocated

t=0-30s: Start period (no health checks)
      • Application boots
      • Connections initialized
      • Dependencies resolved

t=30s: First health check
      • Run: curl http://localhost:3000/health
      • Timeout: 5 seconds
      • Expected: Exit code 0 (success)

Results:
PASS:   Log success, increment counter (need 2 passes)
FAIL:   Log failure, increment failure counter
TIMEOUT: Consider as failure

t=40s: Second health check (if first passed)
      • If 2nd passes: Container is healthy ✓
      • Proceed to Stage 5 (Swap)

t=65s: Total timeout
      • If still failing after 65s: Abort
      • Kill new container
      • Rollback to old
      • Log failure
      </code></pre>

      <h3>What Makes Container "Healthy"?</h3>

      <ul>
        <li><strong>Health Check Passes:</strong> Exit code 0, within timeout</li>
        <li><strong>Consecutive Passes:</strong> 2 passes required (reduces flakiness)</li>
        <li><strong>All Dependencies Ready:</strong> Database responsive, services up</li>
        <li><strong>No Crash Loops:</strong> Container must stay running</li>
      </ul>

      <h2>Stage 5: Atomic Swap (Rename)</h2>

      <h3>The Critical Moment</h3>

      <p>
        Atomic swap is the moment where traffic switches from old container to new. Implemented using Docker container rename (atomic at syscall level).
      </p>

      <h3>Swap Sequence</h3>

      <pre><code className="language-text">
Before Swap:
• Container "api" running (old)
• Container "api.new" running and healthy (new)
• Traffic routes to "api"

Swap Step 1: Rename old → old.backup
  docker rename api api.old
  • Atomic operation
  • No connection loss (name is just label)
  • Old container still running

Swap Step 2: Rename new → primary
  docker rename api.new api
  • Atomic operation
  • New container is now "api"
  • Traffic now reaches new container

After Swap:
• Container "api" running (new)
• Container "api.old" running (old)
• Traffic routes to "api" (the new one)
• Timing: Both renames complete within 100ms

Failure Handling During Swap:
If rename fails (unlikely but possible):
• Reverse swap: Restore original names
• Keep old "api" as primary
• Remove partially-renamed containers
• Consider rotation failed
• Retry later
      </code></pre>

      <h3>Why This Approach?</h3>

      <ul>
        <li><strong>Atomic:</strong> Swap is atomic at Linux syscall level</li>
        <li><strong>No Downtime:</strong> Old container still up during transition</li>
        <li><strong>Instant Revert:</strong> Reverse swap in milliseconds if needed</li>
        <li><strong>Network Awareness:</strong> Containers with same name are aliases, so traffic follows automatically</li>
      </ul>

      <h2>Stage 6: Cleanup</h2>

      <h3>Post-Swap Actions</h3>

      <pre><code className="language-text">
1. Wait 10 seconds
   • Allow any in-flight requests to old container to finish
   • Old container still running but unreachable (renamed to api.old)

2. Stop old container
   docker stop api.old
   • Graceful shutdown (SIGTERM, 30s timeout)

3. Remove old container
   docker rm api.old
   • Free disk space
   • Clean up network interfaces
   • Remove from Docker metadata

4. Clean up state file
   • Update rotation status to "completed"
   • Record rotation duration
   • Store new container ID
   • Remove in-progress markers

5. Release lock file
   rm /var/lib/dso/rotation.lock
   • Other agents can now proceed
      </code></pre>

      <h3>Cleanup Timing</h3>

      <ul>
        <li><strong>Total Cleanup:</strong> 3-5 seconds</li>
        <li><strong>Grace Period:</strong> 10 seconds before stopping old container</li>
        <li><strong>Stop Timeout:</strong> 30 seconds (if doesn't stop, force kill)</li>
      </ul>

      <h2>Successful Rotation Summary</h2>

      <pre><code className="language-text">
Rotation: api (DATABASE_PASSWORD updated)

Detection: 2025-03-15 10:25:00Z (polling detected change)
Lock Acquired: 2025-03-15 10:25:01Z (1ms, no contention)
Container Created: 2025-03-15 10:25:02Z (1.2s creation)
Health Checks: 2025-03-15 10:25:35Z (33s, app startup)
  - t=30s: First check - PASS
  - t=40s: Second check - PASS
Swap Complete: 2025-03-15 10:25:35Z (100ms operation)
Cleanup: 2025-03-15 10:25:40Z (5s to stop old)
Lock Released: 2025-03-15 10:25:40Z

Total Duration: 40 seconds
Result: ✓ Success (zero downtime)
      </code></pre>

      <h2>Failure Scenarios & Recovery</h2>

      <h3>Failure 1: Health Check Failure (Most Common)</h3>

      <pre><code className="language-text">
At: Stage 4 (Health Validation)

Scenario:
• Database password updated
• New container created
• Health check runs: curl http://localhost:3000/health
• Connection fails: Database not accepting new password
• Result: FAIL

Recovery Flow:
1. Detect health check failure at t=65s
2. Kill new container "api.new"
   docker stop api.new
   docker rm api.new
3. Original "api" still running (unchanged)
4. Log: "Health check failed: Connection refused"
5. Alert: Page on-call engineer
6. Next rotation: Will retry (if secret was re-updated)

Operator Action:
• Check application logs: Why did health check fail?
• Verify database accepted new password
• Check network connectivity
• Manual rotation once issue resolved
      </code></pre>

      <h3>Failure 2: Agent Crash During Rotation</h3>

      <pre><code className="language-text">
At: Stage 5 (Atomic Swap)

Scenario:
• Health checks pass
• Attempting rename: "api" → "api.old"
• System runs out of memory
• Agent process killed by OOM killer
• Rename partially complete

System State After Crash:
• "api" container may or may not be renamed
• "api.new" container is running and healthy
• Lock file still exists (but stale)
• State file in-progress

Recovery On Agent Restart:
1. Load state file (shows in-progress rotation)
2. Check rotation timestamp
3. Is rotation older than 5 minutes?
   Yes: → Perform auto-rollback
   No: → Retry swap

Auto-Rollback:
1. Find all api.* containers
2. If "api.new" exists: Stop and remove it
3. If "api.old" exists: Rename back to "api"
4. If "api" missing: Restore from backup
5. Update state: "failed_with_auto_recovery"
6. Log recovery action
7. Continue normal operation

Result: System restored to consistent state automatically
No operator intervention needed
      </code></pre>

      <h3>Failure 3: Provider Unreachable</h3>

      <pre><code className="language-text">
At: Stage 1 or 3 (Detection or Secret Load)

Scenario:
• AWS API unreachable (network issue)
• Secret fetch times out
• Cannot proceed with rotation

Behavior:
1. Timeout waiting for provider response
2. Log: "Provider timeout: AWS API unreachable"
3. Alert: Non-critical alert (retry scheduled)
4. Wait 30-60 seconds
5. Retry next polling interval

Result: Rotation deferred, no error state
Old secrets continue to work
System stable, just waiting for provider recovery
      </code></pre>

      <h3>Failure 4: Concurrent Rotation (Lock Timeout)</h3>

      <pre><code className="language-text">
Scenario (unlikely on single host):
• Agent 1 starts rotation at t=0
• Acquires lock
• Agent 2 starts rotation at t=5s
• Tries to acquire lock
• Lock held by Agent 1

Behavior:
1. Agent 2 reads lock file
2. Checks owner (Agent 1)
3. Waits (exponential backoff)
4. Timeout at 30 seconds
5. If still locked: Abort, retry later
6. Agent 1 releases lock at t=40s
7. Agent 2 retries, acquires lock
8. Proceeds with rotation

Result: Serialized operations, no conflict
Both agents' rotations eventually complete
      </code></pre>

      <h2>State Transitions</h2>

      <pre><code className="language-text">
Rotation State Machine:

                    ┌─ pending
                    │
              [detect change]
                    │
                    ▼
            lock_acquired
                    │
         ┌──────────┴──────────┐
         │                     │
      [create]             [timeout]
         │                     │
         ▼                     ▼
    container_created    lock_timeout_err
         │                 (retry later)
         │
    [start container]
         │
         ▼
   health_checking
         │
    ┌────┴────┐
    │          │
 [pass]    [fail]
    │          │
    ▼          ▼
 swapping   rollback
    │          │
    │    ┌─────┘
    │    │
    ▼    ▼
cleanup - completed (with error flag)
    │
    ▼
lock_released
    │
    ▼
  completed ✓

State Persistence:
• Each state transition logged with timestamp
• State file updated after each stage
• Checkpoint created after successful rotation
• Audit trail maintained for compliance
      </code></pre>

      <h2>Performance Timeline (Typical)</h2>

      <table>
        <thead>
          <tr>
            <th>Stage</th>
            <th>Duration</th>
            <th>Notes</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>Detection</td>
            <td>5-60s</td>
            <td>Polling interval (webhook: 1-5s)</td>
          </tr>
          <tr>
            <td>Lock Acquisition</td>
            <td>1-5ms</td>
            <td>Instant if no contention</td>
          </tr>
          <tr>
            <td>Container Creation</td>
            <td>1-3s</td>
            <td>Pull image from cache, instantiate</td>
          </tr>
          <tr>
            <td>Health Checks</td>
            <td>30-65s</td>
            <td>Depends on app startup time</td>
          </tr>
          <tr>
            <td>Atomic Swap</td>
            <td>100ms</td>
            <td>Rename operations (very fast)</td>
          </tr>
          <tr>
            <td>Cleanup</td>
            <td>3-5s</td>
            <td>Stop old container, remove</td>
          </tr>
          <tr>
            <td>Total</td>
            <td>7-35s</td>
            <td>Typical real-world scenario</td>
          </tr>
        </tbody>
      </table>

      <h2>Key Invariants</h2>

      <ul>
        <li><strong>Always Consistent:</strong> System is never in an invalid state</li>
        <li><strong>Automatic Recovery:</strong> Crashes automatically resolved on restart</li>
        <li><strong>Deterministic:</strong> Same inputs produce same outputs</li>
        <li><strong>Observable:</strong> Every action logged and visible</li>
        <li><strong>No Manual Cleanup:</strong> Operator never needs to manually fix state</li>
      </ul>

      <h2>Next Steps</h2>

      <ul>
        <li><a href="/docs/guide/architecture">Detailed architecture explanation</a></li>
        <li><a href="/docs/guide/recovery-procedures">Recovery procedures</a></li>
        <li><a href="/docs/guide/observability">Monitoring and observability</a></li>
        <li><a href="/docs/guide/troubleshooting">Troubleshooting guide</a></li>
      </ul>
    </div>
  );
}
