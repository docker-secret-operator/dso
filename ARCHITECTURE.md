# DSO System Architecture (V3.2)

The Docker Secret Operator (DSO) is a **runtime secret injector** designed for standalone Docker and Docker Compose environments. It implements a **zero-persistence model** to ensure sensitive data is never written to the host's physical disk — secrets are fetched at runtime and injected directly into container memory via `tmpfs`.

---

## 🏗️ Execution Models

DSO has **two distinct execution modes**, each with its own architecture:

### Local Mode (Default)
- **Trigger**: `docker dso up <args>`
- **Execution**: In-process CLI tool (no daemon)
- **Secret Store**: User's encrypted local vault (`~/.dso/vault.enc`)
- **Injection**: Environment variables (`dso://`) or tmpfs files (`dsofile://`)
- **Use Case**: Development, CI/CD, local testing — no root required, no cloud account needed

**Local Mode Flow:**
```
docker dso up
  → Parse docker-compose.yaml
  → Resolve dso:// and dsofile:// from vault.enc
  → Inject secrets into compose file (memory only)
  → Execute `docker compose up` with resolved file
  → Secrets never touch disk
```

### Cloud Mode (Optional)
- **Trigger**: Systemd daemon (`dso-agent`)
- **Execution**: Background long-running process (requires root, systemd)
- **Secret Store**: Provider backends (Vault, AWS, Azure, Huawei)
- **Injection**: Environment variables only (file injection not supported in Cloud Mode)
- **Use Case**: Production multi-host deployments, centralized secret management

**Cloud Mode Flow:**
```
systemd: dso-agent
  → Listen on Docker socket (/var/run/docker.sock)
  → Watch for container lifecycle events
  → Fetch secrets from provider backends on demand
  → Rotate secrets when changes detected
  → Maintain in-memory cache of secrets
```

---

## 🏗️ Key Components

### CLI Layer (Local Mode)
- **Compose File Parser**: AST-based parsing of docker-compose.yaml
- **Vault Manager**: AES-256-GCM encryption/decryption of local secrets
- **Injector Client**: RPC communication with optional Cloud Mode daemon
- **Commands**: `init`, `secret set/get/list`, `up`, `system doctor`

### Agent Layer (Cloud Mode)
- **Event Listener**: Docker socket monitoring for container lifecycle events
- **Provider Registry**: RPC-based plugin system for Vault, AWS, Azure, Huawei
- **Reconciliation Engine**: Determines when containers need secret rotation
- **Rotation Controller**: Executes container restart/restart+wait sequences

### Observability Layer (Both Modes)
- **Logging Audit**: Prevents secrets from appearing in logs or stderr
- **Prometheus Metrics**: 30+ metrics for monitoring injection, rotation, provider health
- **Event Deduplication**: TTL-based cache prevents duplicate processing
- **Provider Supervision**: Crash detection, restart backoff, health tracking

---

## 🔄 Secret Injection Flow

### Local Mode Injection (Single-shot)
```
1. Parse compose file (AST)
2. Identify dso:// and dsofile:// references
3. For each reference:
   a. Decrypt secret from vault.enc
   b. Inject into environment map (dso://)
   c. OR prepare tmpfs mount (dsofile://)
4. Merge resolved secrets into compose AST
5. Pass resolved compose to docker compose up
6. Docker runs containers with injected secrets
7. Secrets wiped from CLI process on exit
```

### Cloud Mode Rotation (Event-driven)
```
1. Docker daemon emits container lifecycle event
2. Event handler deduplicates (prevents duplicates within TTL)
3. Check dso.yaml configuration for this container
4. Fetch secrets from configured providers (with retry backoff)
5. Compare new secrets vs cached secrets (checksum)
6. If changed:
   a. Stop current container (gracefully or hard)
   b. Start new container with updated environment
   c. Verify health (if health check configured)
   d. Clean up old container
7. If unchanged: skip rotation (avoid unnecessary restarts)
8. Log rotation event with redacted secrets
```

### Retry & Resilience Behavior
- **Provider Failure**: Exponential backoff (1s → 2s → 4s → ... → 30s max) with jitter
- **Max Attempts**: Configurable, default 5 before giving up
- **Graceful Degradation**: If fetch fails, keep running with last-known-good secrets
- **No Data Loss**: Secrets never deleted, only updated or skipped

---

## 🛠️ Provider Supervision & Recovery

DSO includes runtime supervision features to maintain operational reliability:

- **Health Monitoring**: Periodic heartbeat pings to providers (default 30s interval)
- **Crash Detection**: Tracks consecutive failures per provider
- **Restart Backoff**: Exponential backoff when restarting failed providers (1s → 2s → 4s → ... → 30s cap)
- **Degraded Mode**: If a provider fails, DSO logs error but continues with cached secrets
- **No Cascading Failures**: Container rotation only triggers when secrets actually change (checksum validation)

---

## 📊 Data Flow Diagram

### Local Mode (Single Request)
```
docker dso up
    ├─ Parse docker-compose.yaml
    ├─ Read dso:// and dsofile:// references
    ├─ Decrypt from ~/.dso/vault.enc (AES-256-GCM)
    ├─ Inject into environment (memory only)
    └─ Execute: docker compose up [resolved file via stdin]
        └─ Secrets never written to host disk
```

### Cloud Mode (Continuous)
```
systemd: dso-agent
    ├─ Connect to Docker socket (/var/run/docker.sock)
    ├─ Listen for events (container start, stop, die)
    ├─ Deduplicate events (TTL: 30s default)
    ├─ Load dso.yaml configuration
    ├─ Fetch secrets from providers (Vault/AWS/Azure/Huawei)
    │   └─ Retry with exponential backoff on failure
    ├─ Cache secrets in RAM
    ├─ Compare checksum: new vs cached
    │   ├─ If changed: trigger rotation
    │   │   ├─ Stop old container (graceful or hard)
    │   │   ├─ Start new container (env updated)
    │   │   ├─ Health check (if configured)
    │   │   └─ Clean up old container
    │   └─ If unchanged: skip (avoid unnecessary restarts)
    └─ Emit metrics (Prometheus)
        └─ rotation_total, rotation_errors, provider_health, etc.
```

---

## 🛡️ Security Design Decisions

### Zero-Persistence Guarantee
- **Local Mode**: Secrets exist only in CLI process memory; never written to disk
- **Cloud Mode**: Secrets cached in agent RAM; never persisted to host filesystem
- **Injection**: Files written to container `tmpfs` (RAM disk), automatically cleaned on container exit

### Injection Strategy Tradeoffs
| Strategy | Local Mode | Cloud Mode | Security | Notes |
|----------|-----------|-----------|----------|-------|
| `dso://` (env var) | ✓ | ✓ | ⚠️ Lower | Visible to `docker inspect`, process env |
| `dsofile://` (tmpfs) | ✓ | ❌ | ✓ Higher | Invisible to `docker inspect`, file-based |

**Recommendation**: Use `dsofile://` in production (Local Mode only); use `dso://` for development convenience.

### Log & Error Redaction
- All DSO output passes through centralized redaction engine
- Pattern-based detection removes secrets from logs, errors, stack traces
- Prevents accidental exposure in observability systems

### Event Deduplication
- Prevents same secret from being processed multiple times in quick succession
- TTL-based cache (default 30s) eliminates "thundering herd" on Docker daemon restarts
- Reduces unnecessary container churn

### Provider Trust Assumptions
- Providers must be reachable over TLS (HTTPS or encrypted protocols)
- Provider credentials configured in `dso.yaml` (on secure host filesystem)
- Docker daemon assumed secure and uncompromised
- Host user assumed untrusted (no root required for Local Mode)

---

## 🏗️ Why These Design Choices?

### Why Checksum Validation?
**Problem**: Traditional operators restart containers on every sync, even if secrets haven't changed → unnecessary downtime and log churn.  
**Solution**: Validate that secret content actually changed before triggering rotation. Only restart if necessary.

### Why Event Deduplication?
**Problem**: Docker daemon restart emits many container events; reprocessing same secret multiple times is wasteful.  
**Solution**: TTL-based fingerprinting cache deduplicates identical events within a window, preventing cascade failures.

### Why Two Execution Modes?
**Problem**: Local development and production have completely different requirements (no daemon vs. centralized management).  
**Solution**: Local Mode for dev/CI (no root, encrypted vault on laptop); Cloud Mode for production (daemon, provider backends).

### Why tmpfs Injection?
**Problem**: Environment variables are visible to `docker inspect`, host users, and even the container itself.  
**Solution**: `dsofile://` mounts secrets as read-only files in `tmpfs` (kernel RAM), making them invisible to introspection tools.

### Why RPC + Plugin System?
**Problem**: Hard-coding each provider (Vault, AWS, Azure) in the main codebase creates maintenance burden and version lock-in.  
**Solution**: Plugin system via HashiCorp's `go-plugin` allows independent provider versioning and testing.
