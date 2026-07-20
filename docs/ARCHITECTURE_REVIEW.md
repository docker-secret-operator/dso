# DSO Architecture Review

**Date:** 2026-07-20  
**Scope:** System entry points, core components, data flows, and integration boundaries

---

## Executive Summary

DSO is a well-structured Go CLI tool with a daemon agent component. The architecture follows a clear separation of concerns: CLI handles user interaction via Cobra, the agent daemon listens to Docker events, and a REST API layer provides operational endpoints. Main entry point is simple (`cmd/dso/main.go`), delegating to `internal/cli` root command dispatcher.

---

## Entry Points

### 1. CLI Command Dispatcher (`cmd/dso/main.go` → `internal/cli/root.go`)
- **Minimal bootstrap**: Sets version/commit/date, calls `cli.Execute()`
- **Root command setup** in `NewRootCmd()`: Cobra-based CLI with 23 subcommands
- **Config resolution priority**:
  1. CLI flag (`-c` / `--config`)
  2. `/etc/dso/dso.yaml` (agent mode)
  3. `./dso.yaml` (local mode)
  4. Fallback: `dso.yaml`
- **Subcommands**: setup, bootstrap, doctor, status, config, system, agent, metadata, compose, fetch, init, apply, inject, sync, up, down, watch, version, validate, export, inspect, diff, logs, secret, env-import

### 2. Agent Daemon (`internal/agent/agent.go`)
- **Docker integration**: Uses Docker Go client for event streaming
- **Event filtering**: Listens for container create/start/die/destroy events
- **Main loop**: Reconnection logic with exponential backoff (1s → 5m max)
- **Event queue**: Bounded queue (1000 max events, 16 concurrent workers)
- **State management**: In-memory cache of injected secrets with mutex protection
- **Graceful shutdown**: Context-based cancellation via `<-ctx.Done()`

### 3. REST API Server (`internal/server/rest.go`)
- **HTTP endpoint**: 127.0.0.1:8471 (loopback only)
- **WebSocket support**: Gorilla websocket for real-time event streaming
- **Security headers**: X-Content-Type-Options and CSWSH protection
- **Authentication**: Token-based auth via `internal/auth`
- **Endpoints** (inferred from code):
  - WebSocket endpoint for event streaming
  - Health check
  - Status endpoint
  - Configuration endpoints
  - Webhook receiver for provider events

### 4. Docker Plugin Socket
- **Location**: `/run/docker/plugins/dso.sock`
- **Purpose**: Docker secret driver plugin integration (V2 API)
- **Not yet verified**: Whether this is fully implemented

---

## Core Components

### 1. CLI Handler (`internal/cli/`)
- **Pattern**: Cobra command framework
- **Responsibilities**:
  - Parse config file via flag resolution
  - Dispatch to appropriate command handler
  - Render user-facing output
- **Key commands**:
  - `setup.go`: Installation & initialization pipeline
  - `status.go`: Query operational status
  - `doctor.go`: Run 17+ diagnostic checks
  - `up.go`: Start containers
  - `down.go`: Stop containers
  - `watch.go`: Stream events
  - `agent.go`: Run daemon
  - `secret.go`: Secret management (set/get/list)
  - `logs.go`: View agent logs
  - `system_mgmt.go`: System-level operations

### 2. Agent Daemon (`internal/agent/`)
- **Cache**: In-memory secret cache with mutex-protected access
- **Docker client**: Uses official Docker Go client for event listening
- **Event queue**: Bounded queue with backpressure protection
- **Event handler**: `handleEvent()` processes container lifecycle events
- **State tracking**: Injected secrets tracked per container ID
- **Ready channel**: Signals when event stream is connected

### 3. Setup Pipeline (`internal/cli/setup.go`)
- **Stage 1 (Detect)**: Analyze environment, detect cloud provider
- **Stage 2 (Validate)**: Verify prerequisites and permissions
- **Stage 3 (Plan)**: Generate setup plan with all operations
- **Stage 4 (Apply)**: Execute setup with rollback on failure
- **Integration**: Calls bootstrap, creates config, manages systemd

### 4. Configuration Loader (`pkg/config/config.go`)
- **Format**: YAML-based configuration
- **Loading**: Unmarshals into `Config` struct
- **Validation**: `pkg/config/validation.go` performs checks
- **Providers**: AWS, Azure, HashiCorp Vault, Local
- **Secrets**: Mapping from secret name to provider + path

### 5. Provider System (`internal/providers/` + `cmd/plugins/`)
- **Interface**: Likely defines Get/Set/List/Delete methods
- **Implementations**: Separate plugin binaries per provider
- **Resolution**: Config specifies which provider to use per secret
- **Caching**: Agent caches secrets in memory

### 6. Docker Integration (`internal/compose/`, `internal/injector/`)
- **Compose handling**: Parse docker-compose.yaml, inject labels
- **Secret injection**: Support for `dso://` (env) and `dsofile://` (file) syntax
- **Label detection**: Look for `dso.reloader: "true"` label
- **Container management**: Create/update/remove containers with updated secrets

### 7. REST API Server (`internal/server/`)
- **HTTP handler**: Routes requests to appropriate handlers
- **WebSocket**: Real-time event streaming to clients
- **Authentication**: Token-based auth for security
- **Event hub**: Manages client connections and broadcasts
- **Rate limiting**: Protects against abuse (rate_limit.go)

### 8. Logging & Observability (`pkg/observability/`)
- **Logger**: Zap-based structured logging
- **Log level**: Configurable (info default)
- **Format**: Console output (structured JSON in prod)

---

## Data Flows

### Flow 1: CLI Setup Command
```
User runs: docker dso setup
  ↓
CLI root dispatcher → setup.go
  ↓
Detect environment (cloud provider, os)
  ↓
Validate prerequisites (Docker, permissions, systemd)
  ↓
Plan installation (directories, config, systemd, groups)
  ↓
Apply (create dirs, generate config, install systemd)
  ↓
On error: Rollback changes (restore previous state)
  ↓
Return status to user
```

### Flow 2: Agent Daemon Event Loop
```
Agent starts: docker dso agent (or systemd service)
  ↓
Connect to Docker daemon
  ↓
Register event filters (container create/start/die/destroy)
  ↓
For each event:
  ├─ Enqueue in bounded queue
  ├─ Worker processes event concurrently
  ├─ Extract container labels (dso.reloader, dso.secrets)
  ├─ Check if secret needs rotation
  ├─ Fetch new secret from provider
  ├─ Inject into container (env or file)
  └─ Update cache
  ↓
On error: Log & retry with backoff
```

### Flow 3: Secret Rotation
```
Secret change detected (polling or webhook)
  ↓
Acquire lock (prevent concurrent rotations)
  ↓
Get rotation strategy from config (rolling/restart/signal/none)
  ↓
For rolling strategy:
  ├─ Create new container with updated secret
  ├─ Health check new container
  ├─ Swap old ↔ new (atomic rename)
  ├─ Route traffic via TCP proxy
  └─ Stop old container
  ↓
On failure: Rollback (restore previous container)
  ↓
Release lock
```

### Flow 4: REST API Event Streaming
```
Client connects: WebSocket to 127.0.0.1:8471/events
  ↓
Authenticate with token
  ↓
Add client to event hub (broadcast list)
  ↓
Hub broadcasts events to all connected clients
  ↓
Client receives real-time events (JSON format)
  ↓
On client disconnect: Remove from hub
```

---

## Known Integration Points

### Files that coordinate between components:
- **`internal/cli/setup.go`** ↔ bootstrap, config generation
- **`internal/cli/agent.go`** ↔ agent daemon startup
- **`cmd/dso/main.go`** ↔ version/commit injection
- **`internal/agent/agent.go`** ↔ Docker events, cache, config
- **`internal/server/rest.go`** ↔ agent cache, trigger engine
- **`pkg/config/config.go`** ↔ all components (loaded on startup)

---

## Component Dependencies

### Dependency Graph:
```
cmd/dso/main.go
  └─ internal/cli/root.go (all commands)
      ├─ internal/cli/setup.go → bootstrap, analyzer
      ├─ internal/cli/agent.go → agent daemon
      ├─ internal/cli/status.go → agent cache
      ├─ internal/cli/doctor.go → diagnostic checks
      ├─ internal/cli/watch.go → event streaming
      └─ [23 other commands]

internal/agent/agent.go
  ├─ Docker go client
  ├─ Observability (zap logger)
  ├─ Event queue (async processing)
  ├─ Provider system (fetch secrets)
  ├─ Injector (modify containers)
  └─ Config (secrets to watch)

internal/server/rest.go
  ├─ Agent cache (query state)
  ├─ Trigger engine (secret rotations)
  ├─ Auth (token validation)
  ├─ WebSocket (event streaming)
  └─ Event hub (client broadcast)

pkg/config/config.go
  ├─ YAML unmarshaling
  ├─ Validation rules
  └─ All components depend on this
```

---

## Key Design Patterns

### 1. **Cobra CLI Framework**
- Standard Go CLI pattern
- Subcommands map to feature areas
- Flags for configuration overrides

### 2. **Agent Daemon with Event Streaming**
- Docker event listener pattern
- Asynchronous processing via bounded queue
- Exponential backoff for resilience

### 3. **REST API with WebSocket**
- HTTP/1.1 with WebSocket upgrade
- Real-time event broadcasting
- Token-based authentication

### 4. **Context-Based Cancellation**
- Go context for graceful shutdown
- Timeout support for operations
- Cancellation propagation through goroutines

### 5. **In-Memory Cache with Mutex Protection**
- Thread-safe secret cache
- No external state (all in-process)
- Loss on daemon restart (recovered from Docker/provider)

---

## Entry Points Summary Table

| Entry Point | Location | Purpose | Protocol |
|-------------|----------|---------|----------|
| CLI | `cmd/dso/main.go` | User commands | Stdout/Stderr |
| Agent Daemon | `internal/agent/agent.go` | Background process | Docker socket |
| REST API | `internal/server/rest.go` | Operational queries | HTTP/WebSocket |
| Docker Plugin | `/run/docker/plugins/dso.sock` | Secret driver | Docker plugin V2 |
| IPC Socket | `/run/dso/dso.sock` | CLI-agent communication | Unix domain socket |
| systemd | `dso-agent.service` | Service management | systemd API |

---

## Gaps & Observations

### Verified Implementations:
- ✅ CLI dispatcher with 23 commands
- ✅ Agent event loop with backoff logic
- ✅ REST API with WebSocket and token auth
- ✅ Configuration loading and validation
- ✅ Docker event streaming and processing
- ✅ Zap-based logging

### Not Yet Verified:
- Docker plugin socket implementation (claimed but not found in code review)
- IPC socket implementation (mentioned in README but not found yet)
- Distributed lock mechanism (mentioned for rotation concurrency)
- Crash recovery mechanism (mentioned in spec)
- Secret redaction in logs (mentioned in spec)

### Questions for Further Investigation:
1. How is distributed locking implemented for concurrent rotations?
2. Where is crash recovery state persisted and how is it recovered?
3. How are secrets redacted from logs?
4. Is Docker plugin socket actually implemented or only documented?
5. Where is the IPC socket communication implemented?

---

## Architecture Strengths

1. **Clear separation of concerns**: CLI, agent, server are distinct
2. **Event-driven**: Agent reacts to Docker events efficiently
3. **Extensible provider system**: Plugin architecture for secret backends
4. **Real-time visibility**: WebSocket streaming for monitoring
5. **Gradeful shutdown**: Context-based cancellation throughout
6. **Resilient**: Exponential backoff, automatic reconnection

---

## Architecture Weaknesses

1. **In-memory only**: No persistent state (except config)
2. **Single-host only**: No distributed setup
3. **No distributed locking**: File-based locking may not scale
4. **Loss on restart**: All in-memory cache lost on daemon restart
5. **No audit trail**: Events not persisted (only streamed)
