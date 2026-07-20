# Docker Integration & Container Lifecycle Review

**Date:** 2026-07-20  
**Scope:** Docker client integration, secret injection, proxy, and container management

---

## Docker Socket Access

### Socket Location
- **Standard**: `/var/run/docker.sock` (Linux)
- **Fallback**: `/run/docker.sock` (newer Linux)
- **Override**: Via `DOCKER_HOST` env var

### Access Control
- **Owner**: root:docker
- **Permissions**: 0660 (rw-rw----)
- **DSO requirement**: Must be in docker group (for non-root)
- **Root operation**: No sudo needed if user in docker group

### Error Handling
- **Connection timeout**: Likely 30 seconds (from code)
- **Reconnection logic**: Exponential backoff (1s → 5m max)
- **Max retries**: 20 consecutive failures → give up
- **Logging**: ✅ Observable in agent logs

---

## Secret Injection Methods

### Method 1: File Injection (`dsofile://`)

**Syntax**:
```yaml
secrets:
  - name: app_secrets
    inject:
      type: file
      path: /run/secrets/app
```

**Implementation**:
1. Create tmpfs mount in container
2. Write secret to file in tmpfs
3. Update container exec to mount tmpfs
4. File delivered via stdin (not visible to host)

**Advantages**:
- ✅ Not visible in `docker inspect`
- ✅ Not in process environment
- ✅ Secure for production

**Disadvantages**:
- ❌ Requires application to read file
- ❌ More complex than env injection

### Method 2: Env Injection (`dso://`)

**Syntax**:
```yaml
secrets:
  - name: app_secrets
    inject:
      type: env
```

**Implementation**:
1. Create environment variable
2. Inject into container via -e flag
3. Variable visible in docker inspect (by design)
4. Visible in /proc/<pid>/environ (by design)

**Advantages**:
- ✅ Simple (app already uses env vars)
- ✅ Standard Docker pattern

**Disadvantages**:
- ⚠️ Visible in `docker inspect` output
- ⚠️ Visible to anyone with container access
- ⚠️ DSO logs warning on startup

**Security Warning**:
README states: "DSO prints a warning at startup when env injection is used so the exposure is explicit."

---

## TCP Proxy

### Purpose
Own host port bindings (e.g., 127.0.0.1:3306) to prevent traffic interruption during zero-downtime rotation

### Implementation
**Location**: `internal/proxy/proxy.go`

**Mechanism**:
1. DSO binds host port (3306)
2. Listens for connections
3. Routes to current container port
4. During rotation: Switches route to new container
5. Old container stops, new handles requests

### Traffic Routing
- **Before rotation**: 127.0.0.1:3306 → old-container:3306
- **During rotation**: Maintains routing
- **After rotation**: 127.0.0.1:3306 → new-container:3306
- **Connection handling**: Existing connections close gracefully or redirect?

### Ports Configuration
**Label**: `dso.host_ports: "3306:3306"` (host:container)

**Multiple ports**: Likely comma-separated: `"3306:3306,5432:5432"`

### Cleanup
- **Timeout**: How long does proxy listen for unused ports?
- **On container stop**: Proxy port binding released
- **On rotation failure**: Proxy reverts to old container

---

## Container Labels

### Required Labels

| Label | Value | Purpose |
|-------|-------|---------|
| `dso.reloader` | `"true"` | Marks container for DSO management |
| `dso.secrets` | secret name | Which secret to inject |
| `dso.update.strategy` | rolling/restart/signal/none | Rotation strategy |
| `dso.host_ports` | "3306:3306" | Host port bindings (if using proxy) |

### Label Detection
- **When**: Container create/start events
- **How**: Read container labels via Docker API
- **Missing label**: Container ignored (not managed)
- **Multiple secrets**: Single secret name, or comma-separated?

### Label Injection via Compose
**Via docker-compose.yaml**:
```yaml
services:
  mysql:
    labels:
      dso.reloader: "true"
      dso.secrets: "database_credentials"
      dso.update.strategy: "rolling"
      dso.host_ports: "3306:3306"
```

**Via DSO CLI**:
```bash
docker dso compose up  # Injects labels automatically
```

---

## Compose File Handling

### Label Injection Process
- **Input**: docker-compose.yaml without DSO labels
- **Process**: `docker dso up` command
- **Injection**: Add dso.* labels to all services
- **Output**: Modified compose file or direct execution

### Label Preservation
- **Existing labels**: Preserved
- **DSO labels**: Added or updated
- **Conflicts**: How are existing dso.* labels handled? (overwrite or warn?)

### Environment Variable Handling
- **Empty env vars**: Can be specified without value
```yaml
services:
  mysql:
    environment:
      MYSQL_PASSWORD: ""  # Injected by DSO
```
- **Non-empty defaults**: What if env var already has value? (overwrite?)
- **Injection method**: Via -e flag or environment section?

---

## Container Lifecycle

### 1. Container Start
- **Event**: `create` or `start` event from Docker
- **Detection**: Agent receives event
- **Label read**: Check for `dso.reloader: "true"`
- **Extraction**: Read `dso.secrets`, `dso.update.strategy`, `dso.host_ports`
- **Caching**: Add to agent's managed containers map

### 2. Secret Injection
- **Timing**: On container start (or via exec?)
- **Method**: File injection or env injection
- **Secrets provider**: Fetch from configured provider
- **Error handling**: What if secret fetch fails? (container still starts?)
- **Confirmation**: How do we know injection succeeded?

### 3. Secret Change Detection
- **Mechanism**: Polling or webhook
- **Polling interval**: Configurable (30s default)
- **Adaptive backoff**: Reduces polling when no changes
- **Comparison**: Compare current vs cached secret value

### 4. Rotation Trigger
- **Condition**: Secret value changed
- **Action**: Initiate rotation via rotation engine
- **Locking**: Acquire lock to prevent concurrent rotations
- **Strategy check**: Read `dso.update.strategy` label

### 5. New Container Creation
- **Image**: Same as old container
- **Labels**: Copied from old container
- **Environment**: Original + updated secrets
- **Volumes**: Same as old container
- **Ports**: New container gets different ports (old keeps host:container)

### 6. Health Check
- **Method**: Docker HEALTHCHECK or TCP probe? (unknown)
- **Timeout**: 30 seconds (from config)
- **Failures**: How many consecutive failures before rollback?
- **Logging**: Failed checks logged

### 7. Atomic Swap
- **Mechanism**: Rename containers (old → backup, new → active)
- **Docker API**: container rename operation
- **Atomicity**: Guaranteed by Docker or application-level?
- **Rollback point**: If swap fails, can we swap back?

### 8. Traffic Routing
- **TCP proxy**: Update routing from host port
- **DNS**: Any DNS updates? (probably not, host port handles routing)
- **Connection draining**: How long do we wait for existing connections?
- **Forced disconnect**: Do we close connections or let them drain?

### 9. Old Container Cleanup
- **Timing**: After traffic shifted and drained
- **Actions**: Stop container, then remove
- **Backup retention**: Keep old container for how long? (rollback window?)
- **Cleanup error**: If stop fails, what happens?

---

## Gaps/Issues Found

### Critical Issues:

1. **❌ Health check method unknown**
   - Doc says "health-checked" but method unclear
   - Docker HEALTHCHECK? TCP probe? HTTP probe? Custom script?
   - Default behavior if no health check defined?

2. **❌ Proxy implementation details missing**
   - How does proxy switch routes during rotation?
   - How are existing connections handled?
   - What prevents traffic loss during switch?

3. **❌ Label injection via compose unclear**
   - Does `docker dso up` modify compose file?
   - Does it create temporary compose file?
   - How are labels persisted across restarts?

### Medium Issues:

4. **❓ Multiple secrets per container**
   - Can `dso.secrets` list multiple secrets?
   - How are they injected (separate env vars)?
   - If one fetch fails, do others proceed?

5. **❓ Connection handling during rotation**
   - How long are connections allowed to drain?
   - Timeout for forced disconnect?
   - Error behavior if connection won't close?

6. **❓ Label update detection**
   - If user updates dso.* labels, is rotation triggered?
   - Does container restart required?
   - Can strategy change between rotations?

### Low Issues:

7. **❓ Volume and port mapping preservation**
   - When creating new container, how are volumes copied?
   - Are port bindings (besides host:container) preserved?
   - Are environment variables (non-secret) copied?

8. **❓ Compose service ordering**
   - If multiple services use same secret, rotation order?
   - Sequential or concurrent?
   - Dependency handling?

---

## Testing Gaps

### What Should Be Tested:
1. ✓ Label detection (with/without labels)
2. ✓ Secret injection (file and env methods)
3. ✓ Rotation for each strategy (rolling/restart/signal/none)
4. ✓ Health check pass/fail scenarios
5. ✓ Rollback on health check failure
6. ✓ TCP proxy routing during rotation
7. ✓ Connection draining (if applicable)
8. ✓ Multiple secrets per container
9. ✓ Label update detection (if applicable)
10. ✓ Zero-downtime verification (30-second target)

### Integration Requirements:
- Real Docker daemon
- Multiple container scenarios
- Failure injection (health check failures, proxy errors)

---

## Recommendations

1. **Document health check method** - Specify how health is determined
2. **Document proxy behavior** - How connections are routed and switched
3. **Document multiple secrets** - Can container use multiple secrets?
4. **Test connection draining** - Verify graceful transition
5. **Test label updates** - What happens if labels change?
6. **Document compose integration** - How `docker dso up` works
7. **Add timeout configurability** - Make health check timeout per-secret
8. **Add debug logging** - Log injection/rotation steps for troubleshooting
9. **Test proxy cleanup** - Verify ports released after container stops
10. **Document rollback window** - How long is old container kept?
