# DSO System Architecture

## Overview

DSO (Docker Secret Operator) is a cloud-native secret lifecycle runtime for Docker Compose. It operates as a Docker CLI plugin that manages secret injection, rotation, and audit across containerized applications.

```
┌─────────────────────────────────────────────────────────────┐
│                   DSO System Architecture                    │
└─────────────────────────────────────────────────────────────┘

                   Secret Backends
              (Vault / AWS / Azure / Local)
                        ↓
        ┌───────────────────────────────────┐
        │    DSO Bootstrap & Configuration   │
        │  (Local or Agent mode via phases)  │
        └───────────────────────────────────┘
                        ↓
        ┌───────────────────────────────────┐
        │      DSO Agent Process (Phase 4)   │
        │  ┌─────────────────────────────┐   │
        │  │ • Bootstrap System          │   │
        │  │ • Event Watcher             │   │
        │  │ • Secret Provider           │   │
        │  │ • Rotation Engine           │   │
        │  │ • Health Checks             │   │
        │  │ • State Tracker             │   │
        │  └─────────────────────────────┘   │
        └───────────────────────────────────┘
                        ↓
        ┌───────────────────────────────────┐
        │      Docker Compose Containers     │
        │    (Secret Injection via Phase 1)  │
        └───────────────────────────────────┘
```

## Component Architecture

### 1. Bootstrap System (Phase 1)
**Purpose**: Initialize DSO runtime environment with proper structure and permissions.

- **Local Mode** (`docker dso bootstrap local`)
  - Creates `~/.dso/` directory structure
  - Initializes encrypted local vault
  - Generates development configuration
  - Requires: non-root user, Docker socket access
  
- **Agent Mode** (`sudo docker dso bootstrap agent`)
  - Creates `/etc/dso/` and `/var/lib/dso/` directories
  - Generates production configuration
  - Creates systemd service file
  - Requires: root privileges, systemd

### 2. Operational Commands (Phase 2-4)

- **Doctor** (`docker dso doctor`)
  - Environment validation
  - Provider connectivity checks
  - System resource verification
  
- **Status** (`docker dso status`)
  - Real-time system metrics
  - Provider health status
  - Container and cache information
  
- **Config** (`docker dso config`)
  - Configuration management
  - YAML validation
  - Editor integration

- **System** (`docker dso system`) - Phase 4
  - Systemd service management
  - Agent lifecycle control
  - Log viewing and filtering

### 3. Agent Process (Phase 4: Systemd Integration)
**Purpose**: Long-running service that manages secrets and rotations.

**Deployment**:
- Runs as systemd service: `dso-agent`
- Listens on Unix socket: `/run/dso/agent.sock`
- Logs to journald: `journalctl -u dso-agent`

**Responsibilities**:
- Watch Docker events
- Monitor secret backends
- Trigger secret rotations
- Manage container lifecycle (blue-green deployment)
- Track state persistently

### 4. Secret Providers
**Supported**:
- Local encrypted vault (`~/.dso/vault.enc`)
- HashiCorp Vault
- AWS Secrets Manager
- Azure Key Vault

**Plugin Architecture**:
- Provider plugins run as isolated subprocesses
- Communication via stdin/stdout
- Pluggable architecture for custom providers

## Operational Modes

### Local Development Mode
```
User Machine
    ↓
[docker dso bootstrap local]
    ↓
~/.dso/vault.enc (encrypted local storage)
    ↓
[docker dso compose up] (secret injection)
    ↓
Running containers with injected secrets
```

**Characteristics**:
- No system-wide changes
- Non-root operation
- Suitable for development and testing
- Manual rotation or polling-based detection

### Production Agent Mode
```
Host Machine (systemd)
    ↓
[sudo docker dso bootstrap agent]
    ↓
/etc/dso/dso.yaml + systemd service
    ↓
[sudo docker dso system enable]
    ↓
dso-agent systemd service (running continuously)
    ↓
Event-driven secret rotation
    ↓
Blue-green container deployment
```

**Characteristics**:
- System-wide installation
- Requires root for installation
- Managed as systemd service
- Persistent state tracking
- Event-driven rotation
- Automatic restarts on failure
- Journal logging integration

## Data Flow

### Secret Resolution
```
Application Container
    ↓
dso://vault:secret_name (in environment variable)
    ↓
DSO Agent reads secret reference
    ↓
Fetch from provider (cache-aware)
    ↓
Decrypt/resolve value
    ↓
Inject into container
    ↓
Application uses secret at runtime
```

### Automatic Rotation
```
Secret Backend detects change
    ↓
Provider webhook or polling event
    ↓
DSO Agent queues rotation
    ↓
Debounce window (5 seconds default)
    ↓
Create new container with fresh secret
    ↓
Health checks pass?
    ├─ YES: Atomic swap (rename containers)
    │       Stop old container
    │       Return to steady state
    ├─ NO: Rollback (restore old container)
    │      Log failure
    │      Alert operator
    ↓
Rotation complete (or failed)
```

## Configuration & State

### Configuration Files
- **Local mode**: `~/.dso/config.yaml`
- **Agent mode**: `/etc/dso/dso.yaml`

**Example structure**:
```yaml
version: v1alpha1

runtime:
  mode: local  # or 'agent'
  log_level: info

providers:
  local:
    type: file
    enabled: true
    path: ~/.dso/vault

agent:
  cache:
    ttl: 1h
    max_size: 100Mi
  rotation:
    strategy: restart
    timeout: 30s
```

### State Tracking
- **Local mode**: `~/.dso/state/`
- **Agent mode**: `/var/lib/dso/state/`

Tracks:
- Rotation history
- Container mappings
- Cache metadata
- Pending operations

## Security Model

### Encryption
- Secrets encrypted at rest using AES-256
- Master key stored securely
- Local vault: `~/.dso/vault.enc` (0600 permissions)
- Agent vault: configured per provider

### Isolation
- Container tmpfs for secret files (no disk persistence)
- Environment variables read at startup only
- No secret logging or debugging output
- Secrets cleared from memory after use

### Access Control
- Local mode: user-scoped via home directory
- Agent mode: system-wide with root initialization
- Docker socket access required for operations
- Systemd service runs with restricted permissions

## Extension Points

### Custom Providers
Implement provider protocol:
- Stdin/stdout JSON communication
- Standard secret resolution interface
- Health check support

### Health Checks
- Container health checks integration
- Custom probe scripts
- Configurable timeouts and retries

### Event Hooks
- Pre/post rotation hooks
- Custom health check handlers
- Notification integrations

## Deployment Topologies

### Single Host (Most Common)
```
Docker Host
    ↓
[dso-agent systemd service]
    ↓
Multiple docker compose stacks
    ↓
All containers managed by single agent
```

### Development with Multiple Modes
```
Developer Machine
    ├─ [docker dso bootstrap local] → Local dev
    │   └─ ~/.dso/vault.enc
    │
    └─ [docker dso bootstrap agent] (in VM/container)
        └─ systemd service
```

## Performance Characteristics

| Operation | Typical Duration |
|-----------|------------------|
| Bootstrap local | <10 seconds |
| Bootstrap agent | <30 seconds |
| Secret resolution (cached) | <1ms |
| Secret resolution (uncached) | 100-500ms |
| Rotation (full cycle) | 20-60 seconds |
| Health check verification | 5-30 seconds |

## Monitoring & Observability

### Commands (Phase 2-4)
- `docker dso doctor` - Environment validation
- `docker dso status` - Real-time metrics
- `docker dso system logs` - Agent logs
- `docker dso config validate` - Config verification

### Metrics Tracked
- Cache hit/miss rates
- Rotation success/failure counts
- Container health status
- Provider availability
- Queue depth and latency

### Logging
- Local mode: stdout/file logging
- Agent mode: journald integration
- Structured JSON logs
- Configurable log levels

---

For operational details, see [runtime.md](runtime.md).
For day-2 operations, see [operational-guide.md](operational-guide.md).
