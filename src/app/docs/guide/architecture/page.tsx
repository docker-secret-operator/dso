import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "DSO Architecture",
  description: "Deep dive into Docker Secret Operator architecture, state management, crash recovery, and atomic operations."
};

export default function ArchitecturePage() {
  return (
    <div>
      <h1>Architecture</h1>

      <p>
        Understanding DSO's architecture is key to operating it safely in production. This guide explains how the system works, how state is managed, how recovery works, and how atomicity is guaranteed.
      </p>

      <h2>System Overview</h2>

      <p>
        DSO is a runtime secret injection daemon that solves the core problem: <strong>how to rotate secrets in containerized applications safely without exposing them to the host filesystem or Docker metadata layers</strong>.
      </p>

      <p>
        The system has five core components:
      </p>

      <ul>
        <li><strong>Secret Backend:</strong> External system (Vault, AWS, Azure, Huawei) or local encrypted vault</li>
        <li><strong>DSO Agent:</strong> Runtime daemon that detects changes, manages rotations, and handles recovery</li>
        <li><strong>State Tracker:</strong> Persistent state storage for crash recovery</li>
        <li><strong>Lock Manager:</strong> Prevents concurrent operations and multi-agent conflicts</li>
        <li><strong>Container Runtime:</strong> Docker API integration for container lifecycle</li>
      </ul>

      <h2>Architecture Diagram</h2>

      <pre><code className="language-text">
┌─────────────────────────────────────────────────────────────┐
│ Secret Backends (Vault, AWS, Azure, Huawei, Local)          │
└──────────────────────────┬──────────────────────────────────┘
                           │ Poll/Webhook
                           ▼
┌─────────────────────────────────────────────────────────────┐
│ DSO Agent                                                     │
├─────────────────────────────────────────────────────────────┤
│ • Event Detector (Polling/Webhooks)                         │
│ • Rotation Orchestrator                                      │
│ • Health Validator                                           │
│ • Recovery Manager                                           │
│ • State Manager                                              │
└──────────┬──────────────────────────────────────────────────┘
           │
    ┌──────┴──────┬──────────────┬─────────────┐
    │             │              │             │
    ▼             ▼              ▼             ▼
┌────────┐  ┌─────────┐  ┌──────────┐  ┌────────────┐
│ Locks  │  │ Docker  │  │ State    │  │ Config     │
│ Files  │  │ API     │  │ Files    │  │ Files      │
└────────┘  └────┬────┘  └──────────┘  └────────────┘
                 │
        ┌────────┴────────┐
        ▼                 ▼
    ┌─────────┐     ┌──────────┐
    │ Primary │     │ Secondary│
    │ Container       │ Container│
    │ (Old)   │     │ (New)    │
    └─────────┘     └──────────┘
      </code></pre>

      <h2>Local Mode Architecture</h2>

      <h3>Components</h3>

      <ul>
        <li><strong>Local Vault:</strong> <code>~/.dso/vault.enc</code> (AES-256 encrypted, passphrase stored in system keyring)</li>
        <li><strong>DSO CLI:</strong> Single-shot command runner (no daemon)</li>
        <li><strong>State File:</strong> <code>.dso/state.json</code> (tracks current container state)</li>
        <li><strong>Lock File:</strong> <code>.dso/rotation.lock</code> (prevents concurrent operations)</li>
      </ul>

      <h3>Workflow</h3>

      <pre><code className="language-text">
1. CLI invokes: docker dso up
2. Load vault (unlock with passphrase)
3. Load docker-compose.yml
4. Create lock file
5. For each container:
   a. Substitute environment variables from vault
   b. Create container with secrets in tmpfs
   c. Wait for health check
   d. Store old container ID in state
   e. Atomic rename (old → old.backup, new → primary)
   f. Delete old container
6. Release lock
7. Update state file
      </code></pre>

      <h3>Storage</h3>

      <ul>
        <li><strong>Secrets:</strong> Memory/tmpfs during runtime, never on disk</li>
        <li><strong>State:</strong> <code>.dso/state.json</code> (encrypted at rest)</li>
        <li><strong>Vault:</strong> <code>~/.dso/vault.enc</code> (AES-256)</li>
      </ul>

      <h2>Agent Mode Architecture</h2>

      <h3>Components</h3>

      <ul>
        <li><strong>Systemd Service:</strong> Persistent daemon at <code>/etc/systemd/system/dso-agent.service</code></li>
        <li><strong>Configuration:</strong> <code>/etc/dso/config.yml</code> (system-wide settings)</li>
        <li><strong>State Directory:</strong> <code>/var/lib/dso/</code> (state files, locks, audit trail)</li>
        <li><strong>Log Directory:</strong> <code>/var/log/dso/</code> (structured logs via journald or file)</li>
        <li><strong>Provider Credentials:</strong> Secure storage via provider-specific credential manager</li>
      </ul>

      <h3>Runtime Structure</h3>

      <pre><code className="language-text">
/etc/dso/
├── config.yml                      # Agent configuration
├── providers/
│   ├── aws.yml                    # AWS credentials
│   ├── azure.yml                  # Azure credentials
│   └── vault.yml                  # Vault config
└── compose/
    ├── docker-compose.yml         # Secrets to inject
    └── .dso/
        ├── state.json             # Current rotation state
        ├── rotation.lock          # Multi-agent lock
        ├── checkpoint.json        # Last known good state
        └── audit.log              # Rotation audit trail

/var/lib/dso/
├── containers.json                # Tracked container metadata
├── rotations/
│   ├── rotation-1.json           # Per-rotation state
│   └── rotation-2.json
└── locks/
    └── global.lock               # Global lock file

/var/log/dso/
├── dso-agent.log                 # Main agent log
├── rotations.log                 # Rotation operations
└── errors.log                    # Error log
      </code></pre>

      <h3>Event Processing Loop</h3>

      <pre><code className="language-text">
Agent Main Loop (Tick every 5-60 seconds):
1. Check for secret changes (polling/webhook)
2. If change detected:
   a. Acquire global lock (wait max 30s)
   b. Load current state
   c. Validate state integrity
   d. For each affected container:
      - Start rotation
      - Create new container
      - Validate health
      - Atomic swap
      - Cleanup old
   e. Release lock
   f. Log rotation completion
   g. Update checkpoint
3. Monitor container health
4. Check for stale locks (auto-break if >30min)
5. Cleanup orphaned state
6. Report metrics
      </code></pre>

      <h2>State Management</h2>

      <h3>State File Structure</h3>

      <pre><code className="language-json">
{
  "version": "3.5.1",
  "timestamp": "2025-03-15T10:30:45Z",
  "agent_id": "host-1.prod.example.com",
  "rotations": [
    {
      "id": "rotation-abc123",
      "service": "api",
      "status": "completed",
      "old_container_id": "abc123def456",
      "new_container_id": "xyz789abc456",
      "started_at": "2025-03-15T10:25:00Z",
      "completed_at": "2025-03-15T10:30:45Z",
      "duration_ms": 345,
      "health_check_passed": true,
      "secrets_count": 3,
      "changes": {
        "DATABASE_PASSWORD": "***",
        "API_KEY": "***"
      }
    }
  ],
  "checkpoint": {
    "secret_hash": "sha256:abc123...",
    "last_validated": "2025-03-15T10:30:45Z",
    "container_count": 2,
    "healthy": true
  }
}
      </code></pre>

      <h3>State Validation</h3>

      <p>
        On startup, DSO validates state:
      </p>

      <ul>
        <li><strong>Version Check:</strong> Ensure state file version matches agent version</li>
        <li><strong>Lock Integrity:</strong> Verify lock file is valid (age, content, owner)</li>
        <li><strong>Orphaned Containers:</strong> Detect containers with no matching state entry</li>
        <li><strong>In-Progress Rotations:</strong> Detect rotations older than 5 minutes (auto-rollback)</li>
        <li><strong>Health Status:</strong> Verify all containers are healthy</li>
        <li><strong>Checkpoint Consistency:</strong> Compare checkpoint hash with current secrets</li>
      </ul>

      <h2>Atomic Rotation (Blue-Green Deployment)</h2>

      <h3>The Problem</h3>

      <p>
        Traditional rotation strategies have issues:
      </p>

      <ul>
        <li><strong>In-place update:</strong> Downtime during rotation, invalid state if it fails</li>
        <li><strong>Restart approach:</strong> Container restart loses in-memory state, may fail</li>
        <li><strong>Env var substitution:</strong> Requires restart, risky timing issues</li>
      </ul>

      <h3>The Solution: Blue-Green Deployment</h3>

      <pre><code className="language-text">
State: Container "api" running with old secret

Step 1: Create Green Container
   • Start new container with updated secret
   • Name: "api.new"
   • Same image, config, volumes as blue
   • Secrets injected to tmpfs/memory only

Step 2: Health Validation
   • Wait for health check passes (with timeout)
   • If health check fails:
     - Kill green container
     - Rollback to blue
     - Log failure and alert

Step 3: Atomic Swap
   • Docker CLI: Rename "api" → "api.old"
   • Docker CLI: Rename "api.new" → "api"
   • Now traffic routes to green container
   • Atomic at Docker level (no traffic loss)

Step 4: Cleanup
   • Wait 10 seconds
   • Kill old container "api.old"
   • Remove from state

Result: Zero-downtime rotation, automatic rollback on failure
      </code></pre>

      <h3>Guarantees</h3>

      <ul>
        <li><strong>Atomicity:</strong> Rename is atomic at Docker level</li>
        <li><strong>No Data Loss:</strong> Old container persists until new is validated</li>
        <li><strong>Automatic Rollback:</strong> Health check failure triggers instant rollback</li>
        <li><strong>Deterministic:</strong> Same inputs always produce same behavior</li>
        <li><strong>Recoverable:</strong> State persisted for crash recovery</li>
      </ul>

      <h2>Crash Recovery</h2>

      <h3>Scenario: Agent Crash During Rotation</h3>

      <pre><code className="language-text">
Timeline:
1. Agent starts rotation (creates state file)
2. Creates new container "api.new" ✓
3. New container passes health check ✓
4. Agent attempts rename: "api" → "api.old"
   → CRASH BEFORE RENAME COMPLETES

On Restart:
1. Load state file (shows in-progress rotation)
2. Check timestamp (5 minutes old? Auto-rollback)
3. Rollback logic:
   a. Find "api.old" container (if exists)
   b. Find "api.new" container (if exists)
   c. If "api.new" is running and "api.old" exists:
      - Stop and remove "api.new"
      - Restore old naming if needed
   d. If "api" is gone but "api.old" exists:
      - Rename "api.old" → "api"
   e. Update state to "failed"
4. Log recovery details
5. Continue normal operation
      </code></pre>

      <h3>Recovery Guarantees</h3>

      <ul>
        <li><strong>Deterministic Recovery:</strong> Same state always recovers the same way</li>
        <li><strong>No Manual Intervention:</strong> Automatic rollback restores consistency</li>
        <li><strong>State Integrity:</strong> Never leaves system in half-finished state</li>
        <li><strong>Audit Trail:</strong> All recovery actions logged with timestamps</li>
        <li><strong>Operator Visibility:</strong> Failed rotations visible in logs and metrics</li>
      </ul>

      <h2>Concurrency & Lock Management</h2>

      <h3>Lock File Structure</h3>

      <pre><code className="language-text">
/var/lib/dso/rotation.lock:

agent_id: host-1.prod
pid: 12345
timestamp: 2025-03-15T10:30:00Z
ttl_seconds: 300
operation: rotation
service: api
      </code></pre>

      <h3>Lock Lifecycle</h3>

      <ul>
        <li><strong>Acquire:</strong> Create lock file with agent ID and timestamp</li>
        <li><strong>Hold:</strong> Held for duration of rotation (typically 5-30 seconds)</li>
        <li><strong>Release:</strong> Deleted after rotation completes or times out</li>
        <li><strong>Stale Check:</strong> Every 5 minutes, check if lock is older than TTL (300s)</li>
        <li><strong>Auto-Break:</strong> If lock is stale, force-release and proceed</li>
      </ul>

      <h3>Multi-Agent Conflict Prevention</h3>

      <pre><code className="language-text">
Scenario: Two agents running on same host

Agent A: Rotation starting
  1. Try to acquire lock
  2. Create /var/lib/dso/rotation.lock
  3. Success ✓
  4. Start rotation

Agent B: Rotation starting
  1. Try to acquire lock
  2. Read existing lock file
  3. Check owner (Agent A)
  4. Wait (max 30 seconds)
  5. Lock released by Agent A
  6. Acquire lock
  7. Start rotation

Result: Serialized operations, no conflict
      </code></pre>

      <h2>Health Checks</h2>

      <h3>Health Validation Strategy</h3>

      <pre><code className="language-text">
For each new container:
1. Extract health check from docker-compose.yml
2. Wait for container to start (max 5s)
3. Run health check (per Docker healthcheck spec)
4. Timeout: 30 seconds max
5. Success criteria: 2 consecutive passes
6. Failure criteria: Any timeout or non-zero exit

Health Check Types Supported:
• CMD: Run command, check exit code
• CMD-SHELL: Run shell command
• NONE: Skip health check (proceed immediately)
      </code></pre>

      <h3>Failure Handling</h3>

      <ul>
        <li><strong>Health Check Timeout:</strong> Consider container unhealthy, rollback</li>
        <li><strong>Health Check Failure:</strong> Consider container unhealthy, rollback</li>
        <li><strong>Missing Health Check:</strong> Proceed after startup delay (default 10s)</li>
        <li><strong>Rollback on Failure:</strong> Kill new container, restore old immediately</li>
      </ul>

      <h2>Storage & Persistence</h2>

      <h3>Secrets Persistence: Zero</h3>

      <ul>
        <li>Loaded into process memory only</li>
        <li>Injected to container tmpfs/memory</li>
        <li>Never written to host filesystem as plaintext</li>
        <li>Never persisted in Docker metadata (docker inspect)</li>
        <li>Cleared from memory when rotation completes</li>
      </ul>

      <h3>State Persistence: Full</h3>

      <ul>
        <li>Rotation state persisted to <code>/var/lib/dso/state.json</code></li>
        <li>Encrypted with provider credentials or local key</li>
        <li>Checkpoint file stores last known good state</li>
        <li>Audit log records all operations with timestamps</li>
        <li>Used for crash recovery and operational visibility</li>
      </ul>

      <h2>Performance Characteristics</h2>

      <h3>Typical Rotation Timeline</h3>

      <ul>
        <li><strong>Detection to Start:</strong> 5-60 seconds (depends on polling interval)</li>
        <li><strong>Lock Acquisition:</strong> 1-5ms (usually instant)</li>
        <li><strong>New Container Creation:</strong> 1-3 seconds</li>
        <li><strong>Health Check:</strong> 5-30 seconds (depends on application)</li>
        <li><strong>Atomic Swap:</strong> 100ms (Docker API)</li>
        <li><strong>Cleanup:</strong> 1-2 seconds</li>
        <li><strong>Total Rotation Time:</strong> 7-35 seconds (typical)</li>
      </ul>

      <h3>Resource Usage</h3>

      <ul>
        <li><strong>Agent Memory:</strong> 50-100MB baseline</li>
        <li><strong>Agent CPU:</strong> <5% idle, 10-30% during rotation</li>
        <li><strong>State Files:</strong> 1-5MB per rotation history</li>
        <li><strong>Lock File:</strong> <1KB</li>
      </ul>

      <h2>Design Philosophy</h2>

      <p>
        DSO's architecture is guided by three core principles:
      </p>

      <h3>1. Determinism</h3>

      <p>
        Same inputs always produce same outputs. No randomness, no timing-dependent behavior. This enables crash recovery—if rotation fails, rerunning with same inputs will restore consistency.
      </p>

      <h3>2. Atomicity</h3>

      <p>
        Operations are atomic at Docker level. Using rename (atomic syscall) ensures no in-between states. Either rotation succeeds completely or fails completely—never partially.
      </p>

      <h3>3. Observability</h3>

      <p>
        Every operation is logged with precise timestamps. State persisted for operator visibility. Failures are noisy (alerting immediately) not silent. Operators can understand what happened and why.
      </p>

      <h2>Next Steps</h2>

      <ul>
        <li><a href="/docs/guide/how-it-works">Complete rotation workflow explained</a></li>
        <li><a href="/docs/guide/recovery-procedures">Recovery procedures and troubleshooting</a></li>
        <li><a href="/docs/guide/observability">Observability and monitoring</a></li>
        <li><a href="/docs/guide/best-practices">Operational best practices</a></li>
      </ul>
    </div>
  );
}
