# REST API & IPC Interface Review

**Date:** 2026-07-20  
**Scope:** REST API endpoints, IPC socket, authentication, and event streaming

---

## Server Details

### REST API Server
- **Location**: `internal/server/rest.go`
- **Address**: 127.0.0.1:8471 (localhost only, no external access)
- **Protocol**: HTTP/1.1 with WebSocket upgrade support
- **Start function**: `StartRESTServer()`
- **Security**: CSWSH protection, Origin validation for WebSocket

### IPC Socket
- **Location**: `/run/dso/dso.sock` (Unix domain socket)
- **Permissions**: 0660 (root:dso group access)
- **Purpose**: CLI ↔ Agent communication
- **Implementation**: Likely not REST, but request/response protocol

### Docker Plugin Socket
- **Location**: `/run/docker/plugins/dso.sock`
- **Purpose**: Docker secret driver plugin (V2 API)
- **Status**: Mentioned but implementation unclear

---

## REST API Endpoints

### Inferred from Code:

| Method | Path | Purpose | Response |
|--------|------|---------|----------|
| GET | /health | Health check | JSON status |
| GET | /metrics | Prometheus metrics | Text format |
| GET | /events | Event stream (WebSocket) | JSON events |
| GET | /status | Agent status | JSON status |
| POST | /webhook | Provider notifications | Triggers rotation |
| ? | /config | Config query | JSON config |
| ? | /containers | List managed containers | JSON array |

### WebSocket Endpoint (`/events`)
- **Upgrade**: HTTP → WebSocket
- **Origin check**: Validates origin header (localhost or same-host only)
- **Authentication**: Token header (Bearer token)
- **Broadcast**: All connected clients receive events
- **Event types** (inferred):
  - `container.start` - New container created
  - `container.stop` - Container stopped
  - `secret.rotate` - Secret rotation started
  - `secret.rotated` - Secret rotation completed
  - `rotation.failed` - Rotation error
  - `system.error` - System error
  - `agent.ready` - Agent initialized

### Health Endpoint (`/health`)
- **Purpose**: Liveness check for Docker or K8s
- **Response**: JSON `{"status": "ok"}`
- **Status codes**:
  - 200 OK - Agent healthy
  - 503 Service Unavailable - Agent unhealthy?

### Metrics Endpoint (`/metrics`)
- **Format**: Prometheus text format
- **Metrics** (likely):
  - `dso_secrets_rotated_total` - Total rotations
  - `dso_rotation_duration_seconds` - Rotation timing
  - `dso_provider_latency_seconds` - Secret fetch latency
  - `dso_cache_size` - Cache items count
  - `dso_cache_hits_total` - Cache hit count
  - `dso_containers_managed` - Managed container count

---

## IPC Interface

### Communication Pattern
- **Type**: Unix domain socket
- **Protocol**: Likely JSON request/response (guessed)
- **Encoding**: JSON (common in Go)

### Expected Message Types
- **List containers** - Get managed containers
- **Fetch secret** - Request secret value
- **Trigger rotation** - Initiate manual rotation
- **Query status** - Get agent status
- **Get logs** - Fetch recent logs

### Error Response Format
Likely:
```json
{
  "error": "error message",
  "code": "ERROR_CODE",
  "details": {...}
}
```

---

## Authentication

### Token-Based Auth
- **Method**: Bearer token in Authorization header
- **Source**: Configurable or environment variable
- **Validation**: Token compared via `crypto/subtle.ConstantTimeCompare()` (timing-safe)
- **Scope**: Protects WebSocket and sensitive REST endpoints

### Token Requirements
- **Length**: Min/max length enforced
- **Format**: Bearer <token>
- **Expiration**: No TTL (static tokens?)
- **Rotation**: No mechanism documented

### CSWSH Protection
- **Vulnerability**: Cross-Site WebSocket Hijacking
- **Protection**: Origin header validation
- **Allowed origins**:
  - Localhost variants (127.0.0.1, ::1, localhost)
  - Same-host requests (origin == host)
  - No origin header (same-origin requests)

---

## Event Types (Inferred)

### Container Lifecycle Events
- `container.start` - Container started/created
- `container.stop` - Container stopped/removed
- `container.updated` - Container configuration changed

### Rotation Events
- `secret.rotate.start` - Rotation initiated
- `secret.rotate.success` - Rotation completed
- `secret.rotate.failed` - Rotation failed
- `secret.rotate.rollback` - Rollback performed

### System Events
- `system.ready` - Agent ready
- `system.error` - Fatal error
- `provider.error` - Provider connection failed

### Event Format (Likely)
```json
{
  "type": "container.start",
  "timestamp": "2026-07-20T12:34:56Z",
  "actor": {
    "type": "container",
    "id": "abc123def456",
    "attributes": {
      "name": "mysql",
      "project": "myapp"
    }
  },
  "action": "create",
  "data": {
    "secret_name": "database_creds",
    "inject_method": "env"
  }
}
```

---

## Metrics Exported

### Counter Metrics
- `dso_secrets_rotated_total` - Total rotations completed
- `dso_rotation_failed_total` - Total rotation failures
- `dso_cache_hits_total` - Secret cache hits
- `dso_cache_misses_total` - Secret cache misses

### Gauge Metrics
- `dso_containers_managed` - Current managed containers
- `dso_cache_size` - Secrets in cache
- `dso_provider_available{provider="aws"}` - Provider up/down status

### Histogram Metrics
- `dso_rotation_duration_seconds` - Rotation time distribution
- `dso_provider_latency_seconds` - Secret fetch latency
- `dso_event_processing_duration_seconds` - Event handling time

---

## Response Schemas

### Status Response
```json
{
  "status": "ready",
  "mode": "agent",
  "uptime_seconds": 3600,
  "containers_managed": 5,
  "secrets_managed": 2,
  "provider": "aws",
  "last_rotation": "2026-07-20T12:30:00Z",
  "next_check": "2026-07-20T12:35:00Z"
}
```

### Config Response
```json
{
  "version": "v1.0.0",
  "mode": "agent",
  "provider": "aws",
  "watch_interval": "30s",
  "rotation_strategy": "rolling",
  "cache_enabled": true,
  "logging_level": "info"
}
```

### Container List
```json
{
  "containers": [
    {
      "id": "abc123",
      "name": "mysql",
      "project": "myapp",
      "secrets": ["database_creds"],
      "strategy": "rolling",
      "status": "running",
      "last_rotation": "2026-07-20T12:30:00Z"
    }
  ]
}
```

---

## Gaps/Issues Found

### Critical Issues:

1. **❌ Authentication token lifecycle unclear**
   - No token expiration
   - No token rotation mechanism
   - Single static token (no multi-token support)
   - Risk: Compromised token never expires

2. **❌ WebSocket auth token leak**
   - Token in URL or header?
   - If in URL, logged in access logs
   - If in header, safer but need to verify

3. **❌ IPC socket documentation missing**
   - Protocol not documented
   - Message format unknown
   - No reference for CLI-agent communication

### Medium Issues:

4. **❓ Metrics cardinality explosion**
   - If metrics have high-cardinality labels (secret name, container id)
   - Can cause Prometheus memory/performance issues
   - Need label value limits

5. **❓ Event retention**
   - WebSocket events stream live
   - No event history/replay
   - Client misses events if disconnected
   - EventStore class exists but purpose unclear

6. **❓ Rate limiting**
   - `internal/server/ratelimit.go` exists
   - Purpose: WebSocket message rate? API request rate?
   - Limits unclear

### Low Issues:

7. **❓ Health endpoint detail level**
   - /health returns just status
   - No per-component health (Docker, provider auth, etc.)
   - /healthz (detailed) endpoint missing?

8. **❓ Graceful shutdown signaling**
   - Does /health return 503 during shutdown?
   - Time to drain connections?
   - Event to signal readiness loss?

---

## Testing Gaps

### What Should Be Tested:
1. ✓ /health endpoint returns 200
2. ✓ /metrics endpoint returns valid Prometheus format
3. ✓ WebSocket connection and authentication
4. ✓ Event broadcasting to multiple clients
5. ✓ Token validation (valid/invalid/missing)
6. ✓ Origin validation (same-host/different-host)
7. ✓ Event parsing and formatting
8. ✓ Rate limiting (if applicable)
9. ✓ Graceful shutdown (no event loss)
10. ✓ Reconnection after disconnect

### Integration Tests:
- Full event lifecycle (rotation event from start to completion)
- Multiple WebSocket clients receiving events
- Token-based authentication for all endpoints

---

## Recommendations

1. **Add token rotation** - Implement token expiration and refresh
2. **Document IPC protocol** - Specify message format and contract
3. **Implement token revocation** - Ability to invalidate compromised tokens
4. **Add detailed health endpoint** - per-component status (/healthz)
5. **Document metrics cardinality** - Limit label values
6. **Add event replay** - Store recent events, provide catch-up on connect
7. **Document rate limits** - Specify limits per token/IP
8. **Add mutual TLS** - Encrypt IPC socket communication
9. **Implement audit logging** - Log all API requests
10. **Add OpenAPI spec** - Document all endpoints formally
