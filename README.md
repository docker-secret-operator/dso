# Docker Secret Operator (DSO)

**Runtime secret injection and automatic rotation for Docker Compose — as a Docker CLI Plugin**

[![Latest Release](https://img.shields.io/github/v/release/docker-secret-operator/dso?label=latest)](https://github.com/docker-secret-operator/dso/releases/latest)
[![License](https://img.shields.io/github/license/docker-secret-operator/dso)](LICENSE)
[![Go Report](https://goreportcard.com/badge/github.com/docker-secret-operator/dso)](https://goreportcard.com/report/github.com/docker-secret-operator/dso)
[![Build Status](https://img.shields.io/github/actions/workflow/status/docker-secret-operator/dso/ci.yml?branch=main)](https://github.com/docker-secret-operator/dso/actions)

> **DSO is a cloud-native infrastructure platform for Docker Compose**—not a CLI utility. It handles the complete secret lifecycle: initialization, rotation, audit, and recovery.

---

## What is DSO?

DSO is a runtime secret injection daemon for Docker and Docker Compose. It solves a concrete operational problem: **how to rotate secrets in containerized applications safely without exposing them to the host filesystem or Docker's metadata layers**.

**Key features:**
- **Avoids disk persistence** — decrypted secrets exist only in process memory and container tmpfs, never written to host storage
- **Supports automatic rotation** — detects secret changes and refreshes containers with blue-green deployment
- **Multi-provider** — works with Vault, AWS Secrets Manager, Azure Key Vault, or local encrypted storage
- **Deterministic rollback** — failed rotations automatically restore the previous container state
- **No Swarm/Kubernetes** — works with standard `docker compose` on any machine

**Designed for:** Teams running Docker Compose in development, CI/CD, and single-host production who need secrets management without adopting Kubernetes or Swarm.

---

## Quick Start

### Local Development (5 minutes)

```bash
# 1. Install DSO as a Docker plugin
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sh

# 2. Initialize your local environment
docker dso bootstrap local

# 3. Set a secret
docker dso secret set app/db_password mypassword

# 4. Update docker-compose.yaml to use the secret
# services:
#   postgres:
#     environment:
#       DB_PASSWORD: dso://app/db_password

# 5. Deploy
docker dso compose up
```

### Production Deployment (Systemd)

```bash
# 1. Install DSO binary
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sudo sh

# 2. Initialize agent runtime
sudo docker dso bootstrap agent

# 3. Configure providers and settings
sudo nano /etc/dso/dso.yaml

# 4. Enable and start the agent
sudo docker dso system enable

# 5. Verify installation
docker dso doctor
docker dso status

# 6. Deploy with docker-compose.yaml
docker compose up
```

---

## The Problem

Most teams running Docker Compose face a difficult choice: either hardcode secrets in compose files, store them in `.env` files (which risk accidental commits), or adopt Kubernetes. DSO addresses three concrete issues:

1. **Disk Persistence**: Secrets in `.env` files or environment variables live on disk, risking forensic recovery and accidental exposure.

2. **Metadata Leakage**: Any user with Docker socket access can run `docker inspect` and read all environment variables in plaintext.

3. **Manual Rotation**: Rotating a secret requires manual container recreation, coordination, and error-prone scripts.

## How DSO Works

DSO injects secrets at container startup and automatically rotates them via blue-green deployment:

```
Secret Backend (Vault/AWS/Local)
    ↓
DSO Agent detects change
    ↓
Create new container with updated secret
    ↓
Verify health
    ↓
Atomic container swap (rename)
    ↓
Stop old container
    ↓
Rollback on failure (auto-restore)
```

**Result:**
- Secrets never written to host disk
- Rotation is deterministic and reversible
- Rotation completes in ~30 seconds with minimal application disruption
- Failed rotations automatically restore previous state

---

## Key Capabilities

| Capability | Details |
|---|---|
| **Secret Injection** | Inject via `dso://` (environment) or `dsofile://` (tmpfs). Resolved at container startup, not during parsing. |
| **Automatic Rotation** | Event-driven detection from secret backend. Blue-green deployment with automatic rollback on failure. |
| **Multi-Provider** | Vault, AWS Secrets Manager, Azure Key Vault, local encrypted storage, or custom plugins. |
| **Health Verification** | Native Docker health checks or custom probes. Configurable timeouts and retries. |
| **Observability** | Prometheus metrics, structured JSON logs, runtime status queries. |
| **Crash Recovery** | Persisted rotation state enables detection and recovery after agent restarts. |

---

## Architecture Overview

DSO is a single-agent daemon that monitors Docker events and secret backends, then orchestrates container rotations via blue-green deployment.

```
Secret Backends (Vault/AWS/Local)
                ↓
        DSO Agent Process
    ┌────────────────────┐
    │ • Event Watcher    │
    │ • Rotation Engine  │
    │ • Health Checks    │
    │ • State Tracker    │
    │ • Provider Plugins │
    └────────────────────┘
                ↓
        Docker Host Containers
```

**Core design:**
- **Single-agent per host** — manages all containers on a Docker host
- **Event-driven** — responds to Docker events and provider webhooks
- **Blue-green rotation** — create new container, verify health, atomic swap
- **Local state** — persists rotation state for crash recovery
- **Plugin-based providers** — isolated subprocesses for each secret backend

For detailed architecture, see [docs/architecture.md](docs/architecture.md).

---

## Core Concepts

**Secret Resolution**: When containers reference `dso://` or `dsofile://`, DSO fetches secrets at container startup (not during parsing). Secrets are resolved from the configured provider, optionally cached, then injected via environment or tmpfs.

**Event-Driven Rotation**: DSO monitors Docker and provider events. When a secret changes, it's queued, debounced (5-second window), then rotation is triggered.

**Blue-Green Deployment**: Rotation follows a fixed sequence: create new container with updated secret → verify health → atomic container rename → stop old → rollback on any failure.

**Crash Recovery**: Rotation state is persisted to disk, enabling detection of incomplete rotations on restart.

For detailed workflows, see [docs/architecture.md](docs/architecture.md) and [docs/runtime.md](docs/runtime.md).

---

## Installation

### Prerequisites

| Requirement | Details |
|---|---|
| **Docker** | Any recent version (20.10+); `docker compose` required for Local Mode |
| **OS** | Linux (amd64, arm64) or macOS (amd64, arm64) |
| **Root** | Not required for Local Mode; required for Cloud Mode (systemd setup) |
| **Go** | Not required — DSO ships as prebuilt binary |

### Binary Installation

**User install (Local Mode only):**
```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sh
```

**Global install (Cloud Mode + systemd):**
```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sudo sh
```

**Manual install:**
```bash
# Download for your platform
curl -Lo dso https://github.com/docker-secret-operator/dso/releases/download/v3.3.0/dso-linux-amd64

# Verify checksum (optional but recommended)
echo "..." | sha256sum -c -

# Install to Docker plugin directory
mkdir -p ~/.docker/cli-plugins
chmod +x dso
mv dso ~/.docker/cli-plugins/

# Or install globally
sudo mkdir -p /usr/local/lib/docker/cli-plugins
sudo mv dso /usr/local/lib/docker/cli-plugins/
sudo chmod +x /usr/local/lib/docker/cli-plugins/dso
```

### Verify Installation

```bash
docker dso version
# Docker Secret Operator v3.3.0

docker dso doctor
# ┌─────────────────────────────────────────┐
# │     DSO Diagnostics Report              │
# ├─────────────────────────────────────────┤
# │ ✓ Docker socket                        │
# │   running                              │
# │ - Local vault                          │
# │   not initialized (run: dso bootstrap) │
# ...
```

---

## Operational Commands

DSO provides a comprehensive set of operational commands following infrastructure platform patterns:

### Bootstrap — Initialize Runtime

```bash
# Development environment (non-root)
docker dso bootstrap local

# Production agent (requires sudo + systemd)
sudo docker dso bootstrap agent
```

Creates directory structure, generates configuration, initializes encryption, and validates environment. Run this **first** after installation.

### Doctor — Validate Environment

```bash
# Quick health check
docker dso doctor

# Comprehensive validation (system checks)
docker dso doctor --level full

# Machine-readable JSON output
docker dso doctor --json
```

Validates Docker connectivity, providers, containers, cache, system resources, and permissions. Use this to diagnose setup issues.

### Status — Monitor Operations

```bash
# Single status check
docker dso status

# Real-time monitoring (refreshes every 2 seconds)
docker dso status --watch

# JSON output for scripting
docker dso status --json
```

Shows runtime mode, version, uptime, provider health, container status, cache metrics, rotation stats, and queue health.

### Config — Manage Configuration

```bash
# View configuration file
docker dso config show

# Edit configuration in $EDITOR
docker dso config edit

# Validate configuration for errors
docker dso config validate
```

Manages the YAML configuration file with syntax validation and smart error reporting.

### System — Agent Management

```bash
# Show systemd service status
sudo docker dso system status

# Enable and start agent service
sudo docker dso system enable

# Disable and stop agent service
sudo docker dso system disable

# Restart agent service
sudo docker dso system restart

# View agent logs (follow with -f)
docker dso system logs
docker dso system logs -f -n 50
```

Manages the DSO agent systemd service for production deployments.

---

## Quick Start

### Local Mode (5 minutes)

**1. Initialize vault**
```bash
docker dso init
# Created ~/.dso/vault.enc with AES-256-GCM encryption
```

**2. Store a secret**
```bash
docker dso secret set app/postgres_password
# Enter secret value (will be hidden): ••••••••••
```

**3. Create docker-compose.yaml**
```yaml
version: '3.8'
services:
  postgres:
    image: postgres:15
    environment:
      POSTGRES_PASSWORD_FILE: dsofile://app/postgres_password
    volumes:
      - postgres_data:/var/lib/postgresql/data

volumes:
  postgres_data:
```

**4. Deploy with DSO**
```bash
docker dso up -d

# What happens:
# 1. DSO intercepts docker compose up
# 2. Resolves dsofile://app/postgres_password from vault
# 3. Mounts tmpfs at /run/secrets/app/postgres_password
# 4. Starts postgres with POSTGRES_PASSWORD_FILE=/run/secrets/app/postgres_password
# 5. postgres reads secret from file (never exposed to docker inspect)
```

**5. Verify**
```bash
# Secret is NOT in docker inspect
docker inspect postgres | grep POSTGRES_PASSWORD
# (no output)

# But the file exists in the container
docker exec postgres cat /run/secrets/app/postgres_password
# postgres_secret_value
```

**6. Rotate the secret**
```bash
docker dso secret set app/postgres_password
# Enter new secret value: ••••••••••

# DSO detects the change and automatically:
# 1. Creates new postgres container with new secret
# 2. Waits for it to become healthy
# 3. Atomically swaps container names
# 4. Stops old container
# (All happens in ~30 seconds; stateless workloads see minimal disruption)
```

### Cloud Mode (with Vault)

**1. Install globally**
```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sudo sh
```

**2. Create configuration**
```bash
sudo tee /etc/dso/dso.yaml > /dev/null <<EOF
providers:
  vault:
    addr: https://vault.example.com:8200
    auth:
      method: "token"
      token_env: "VAULT_TOKEN"
    mount_path: "secret/data"

agent:
  watch:
    polling_interval: "5m"
    debounce_window: "5s"
  
  cache:
    ttl: "1h"
    max_cache_size: "100Mi"
EOF
```

**3. Setup systemd service**
```bash
sudo VAULT_TOKEN=s.xxxxxxxxxxxxx docker dso system setup
# Downloads provider plugins
# Creates /etc/systemd/system/dso-agent.service
# Starts dso-agent daemon
```

**4. Create docker-compose.yaml**
```yaml
version: '3.8'
services:
  app:
    image: myapp:latest
    environment:
      DATABASE_URL: dso://vault:database/url
      API_KEY: dso://vault:api/key
```

**5. Deploy**
```bash
docker compose up -d
# DSO agent fetches secrets from Vault
# Injects into containers
# Rotates when Vault secret changes
```

---

## Configuration

### dso.yaml Schema

```yaml
# Define secret providers
providers:
  vault:
    addr: "https://vault.example.com:8200"
    auth:
      method: "token"  # token, approle, kubernetes, jwt
      token: "s.xxxxx" # or token_env: "VAULT_TOKEN"
    mount_path: "secret/data"  # KV v2 mount
    tls:
      insecure: false
      ca_cert: "/etc/dso/ca.crt"

  aws:
    region: "us-east-1"
    auth:
      method: "iam"  # iam, sso, static
      # IAM role attached to instance for credentials

  azure:
    vault_url: "https://my-vault.vault.azure.net"
    auth:
      method: "managed_identity"  # managed_identity, client_secret

# Agent runtime configuration
agent:
  # Secret resolution behavior
  watch:
    polling_interval: "5m"  # How often to check if secrets changed
    debounce_window: "5s"   # Batch rapid changes
  
  # Health verification
  health_check:
    timeout: "30s"           # Max time to wait for container healthy
    retries: 3
    interval: "2s"
  
  # Cache behavior
  cache:
    ttl: "1h"                # How long before re-fetching from provider
    max_cache_size: "100Mi"  # Total cache size limit
    max_secret_size: "10Mi"  # Per-secret size limit
  
  # Rotation behavior
  rotation:
    strategy: "restart"      # restart, signal, none
    timeout: "30s"           # Max time for rotation
    rollback_on_failure: true

# Defaults for all containers
defaults:
  rotation:
    strategy: "restart"
```

### Secret Mapping in Compose

```yaml
services:
  app:
    image: myapp:latest
    environment:
      # Environment variable injection (visible to docker inspect)
      DATABASE_PASSWORD: dso://vault:db/password
      
      # File injection via tmpfs (invisible to docker inspect, preferred)
      DATABASE_PASSWORD_FILE: dsofile://vault:db/password
      
      # Multiple providers
      API_KEY: dso://aws:api/key
      SIGNING_KEY: dso://vault:crypto/signing-key
```

---

## Provider System

### Supported Providers

| Provider | Status | Authentication | Rotation Support |
|---|---|---|---|
| **Local** | ✅ GA | File-based encryption | Yes (manual) |
| **Vault** | ✅ GA | Token, AppRole, Kubernetes, JWT | Yes (lease renewal) |
| **AWS Secrets Manager** | ✅ GA | IAM role, static credentials | Yes (automatic) |
| **Azure Key Vault** | ✅ GA | Managed identity, client secret | Yes (automatic) |
| **Custom** | ✅ Supported | Via plugin interface | Via plugin |

### Plugin Architecture

Custom providers implement the `SecretProvider` interface:

```go
type SecretProvider interface {
    // Get secret by name
    GetSecret(ctx context.Context, secretName string) (map[string]string, error)
    
    // Watch for secret changes (webhooks)
    WatchSecret(secretName string, interval time.Duration) (<-chan SecretUpdate, error)
}
```

Plugins are:
- Standalone binaries implementing go-plugin interface
- Discovered in `/usr/local/lib/dso/plugins/`
- Executed as isolated subprocesses
- Verified via SHA256 hash before execution

Example: Create a custom provider for an internal secrets service
```bash
# Plugin binary at /usr/local/lib/dso/plugins/dso-provider-internal
# DSO automatically discovers and loads it
```

---

## Secret Rotation Lifecycle

### From Change Detection to Completion

```
Time    Event                          State
────    ─────                          ─────
T0      Secret changes at Vault        [original container running]
        ↓
        Provider webhook notifies DSO
        ↓
T1      DSO watcher receives event     [event queued]
        ↓
        Debounce window (5s)
        ↓
T6      Rotation triggered             [lock acquired, shadow creating]
        ↓
T7      Shadow container created       [health check starting]
        with new secret
        ↓
T8-25   Health verification            [waiting for "healthy" status]
        (native health check or timeout)
        ↓
T25     Health confirmed               [original running, shadow healthy]
        ↓
T26     Atomic swap:                   [brief window: both named]
        - original → original-old
        - original-new → original
        ↓
T27     Old container stopped          [only new container remains]
        ↓
T28     Rotation complete              [new container fully active]
```

**Timeline:** ~28 seconds for typical rotation
- 5s debounce
- ~15-20s health verification (depends on app startup time)
- <1s for atomic swap
- 3s graceful stop of old container

### Failure Scenarios

**Scenario 1: Health check fails**
```
Shadow container doesn't pass health check within 30s
  ↓
Timeout detected
  ↓
Rollback: Stop shadow, don't perform swap
  ↓
Original container still running
  ↓
Operator reviews logs, fixes issue, retries
```

**Scenario 2: Atomic swap fails**
```
docker rename original → original-old succeeds
docker rename original-new → original FAILS
  ↓
State verification detects: original is running (swap failed)
  ↓
Recovery: Restore original-new back to shadow name
           Stop original-new
  ↓
Rollback: docker rename original-old → original
  ↓
Original container restored
```

**Scenario 3: Agent crashes mid-rotation**
```
Rotation in progress (status: "in_progress" in state tracker)
Agent crashes
  ↓
Agent restarts
  ↓
recoverPendingRotations() runs
  ↓
Detects: rotation > 5 minutes old
  ↓
Mark as "rollback_required" in state tracker
  ↓
Operators see ERROR logs with container IDs
  ↓
Manual recovery: 
    - Inspect state with docker ps
    - Clean up shadow containers
    - Restart rotation if needed
```

---

## Observability

### Prometheus Metrics

DSO exposes metrics on `:9090/metrics`:

```prometheus
# Rotation metrics
dso_rotation_duration_seconds{secret="db_password",status="success"}
dso_rotation_total{secret="db_password",status="success|failure|rollback"}
dso_rotation_health_check_duration_seconds{secret="db_password"}

# Event metrics
dso_event_queue_size{} = 15
dso_event_queue_drops_total{reason="full"} = 2
dso_event_debounce_batches_total{} = 123

# Cache metrics
dso_cache_size_bytes{} = 1048576
dso_cache_entries{} = 5
dso_cache_hits_total{} = 1500
dso_cache_misses_total{} = 23

# Provider metrics
dso_provider_request_duration_seconds{provider="vault",method="GetSecret"}
dso_provider_errors_total{provider="vault",error="timeout"}

# Container metrics
dso_container_rotation_duration_seconds{container="postgres"}
dso_container_rename_failures_total{}
```

### Logging

Logs are structured JSON for parsing:

```json
{"level":"info","msg":"Rotation started","secret":"db_password","provider":"vault","ts":"2024-01-15T10:30:00Z"}
{"level":"info","msg":"Health check passed","container":"postgres-new","status":"healthy","duration_ms":5200}
{"level":"info","msg":"Atomic swap completed","original":"postgres","old":"postgres-old","new":"postgres"}
{"level":"error","msg":"Health check failed","container":"postgres-new","reason":"unhealthy","timeout_ms":30000}
{"level":"error","msg":"ROTATION ROLLBACK","container":"postgres","original_container":"postgres-old","reason":"health_check_failed"}
```

### Runtime Status

Query agent status:

```bash
docker dso status
# Pending Rotations:
#   db_password (started 2m5s ago, status: in_progress)
#
# Cache:
#   Entries: 5
#   Size: 1.2 MB
#   TTL: 1h
#
# Providers:
#   vault: healthy (last_contact: 30s ago)
#   aws: healthy (last_contact: 45s ago)
```

---

## Security Model

**Quick summary:** DSO keeps secrets out of persistent storage and Docker metadata, but inherits trust assumptions from Docker and the configured secret provider.

**Key design decisions:**
- Secrets are decrypted only in process memory (agent or container)
- No secrets written to host filesystem (except encrypted vault file)
- Container secrets injected via tmpfs (invisible to `docker inspect`)
- Agents assume Docker daemon is trusted and secure

**What DSO relies on:**
- Docker daemon isolation (if Docker is compromised, secrets are compromised)
- Configured provider security (Vault, AWS, Azure)
- Host kernel process isolation
- Operator trust (admins have access to secrets)

**What DSO does NOT provide:**
- Multi-tenant isolation (all secrets in single agent process)
- Operator audit logs (use provider audit: Vault, AWS CloudTrail)
- Secret scanning or compliance checking
- Protection against root/privileged attackers (they can inspect agent memory)
- Plugin sandboxing (binary verification only)

**Detailed threat model:** See [SECURITY.md](SECURITY.md) and [THREAT_MODEL.md](THREAT_MODEL.md).

---

## Operational Considerations

### Deployment Model

DSO is designed for **single-agent deployments** per Docker host:
- One `dso-agent` process per machine
- Manages all secrets for all containers on that machine
- State is local to machine (no distributed coordination)

**Not recommended for:**
- Multi-agent setups (concurrency issues without consensus layer)
- HA deployments (state is local; failover requires manual recovery)
- Kubernetes (use ExternalSecrets Operator instead)

### Scaling Constraints

| Constraint | Limit | Reason |
|---|---|---|
| **Containers per agent** | ~500 | Event queue depth (2000 events max) |
| **Secrets per agent** | ~1000 | Cache memory (default 100 MB limit) |
| **Concurrent rotations** | ~10 | File-based locking, Docker API limits |
| **Secret size** | ~10 MB | Per-secret limit; configurable |
| **Event queue depth** | 2000 | Fixed size; drops on overflow |

For larger deployments:
- Run multiple agents (one per subset of containers)
- Partition secrets by agent
- Monitor queue drop rate (`dso_event_queue_drops_total`)

### Failure Handling Philosophy

DSO's approach to failure:

1. **Fail safe** — Incomplete rotation rolls back automatically; original container remains running
2. **Fail observable** — All errors logged at ERROR level with context; no silent failures
3. **Fail manual** — Agent crashes during rotation require operator review (marked in state tracker)
4. **Fail recoverable** — State tracker enables detection and recovery on restart

**Operator responsibility:**
- Monitor logs for ERROR level messages
- Periodically check `docker dso status` for pending rotations
- Clean up orphaned containers if manual recovery required
- Review provider connectivity

### Network Partition Behavior

If Docker daemon becomes unreachable:

```
Event stream disconnects
  ↓
Exponential backoff reconnect (max 30s between attempts)
  ↓
Event stream reconnects
  ↓
CRITICAL: Immediate reconcileRuntimeState()
  ├─ List containers
  ├─ Detect dual-running (e.g., postgres + postgres-old both named "postgres")
  ├─ If found: keep newest, stop older
  └─ Detect stale rotation state
     └─ Mark for manual intervention
```

**Dual-running detection:** DSO uses naming pattern (e.g., `<name>-old`, `<name>-new`) to detect incomplete rotations. On reconnection, it cleans up.

---

## Current Scope & Limitations

DSO is optimized for **single-host Docker Compose environments**. It is not a replacement for Kubernetes-grade secret management.

**Explicit scope:**
- Single Docker host per agent (no multi-host coordination)
- Local state persistence (no distributed consensus)
- File-based locking (scales to ~100s of secrets, not 1000s)
- Manual recovery after agent crashes during rotation (marked in state tracker)
- Docker socket access required (must trust Docker daemon)

**Known limitations:**
- **No HA**: State is local; failover requires manual recovery
- **No distributed locking**: Multiple agents on same host cause race conditions
- **No multi-tenant isolation**: All secrets decrypted in single agent process
- **No audit logs**: Use provider audit logs (Vault, AWS CloudTrail) for compliance
- **Reconciliation still evolving**: Dual-running detection works but may need manual cleanup in edge cases
- **Plugin trust**: Binary verification only; no plugin sandboxing

**Intentional non-goals:**
- Kubernetes support (use ExternalSecrets Operator)
- HSM integration (future, if needed)
- Multi-region coordination (out of scope)
- Secret scanning / compliance checking (use external tools)

---

## When Not to Use DSO

DSO is not appropriate for:

| Use Case | Better Choice | Reason |
|---|---|---|
| **Kubernetes environments** | ExternalSecrets Operator or similar | DSO is Docker Compose–only |
| **Multi-tenant strict isolation** | Vault or dedicated HSM | DSO has no multi-tenant controls |
| **Highly regulated environments** | Vault with audit trail | DSO has minimal audit capabilities |
| **Distributed systems** | Consul or etcd–based secrets | DSO assumes single host |
| **1000s of secrets** | Enterprise secret management | DSO's cache/locking assumes 100s of secrets |
| **HA/failover required** | Kubernetes or Consul | DSO requires manual recovery |

**If you're already using:** Vault Agent, Sealed Secrets, or Doppler, DSO may duplicate your infrastructure. Evaluate if the Docker Compose–specific focus justifies replacing existing tooling.

---

## Project Structure

```
dso/
├── cmd/                          # CLI entrypoints
│   ├── dso/                      # Main CLI binary
│   ├── dso-agent/                # Agent daemon (Cloud Mode)
│   └── dso-cli/                  # Operations CLI
│
├── internal/                     # Private packages (not importable by plugins)
│   ├── agent/                    # Agent lifecycle, state, trigger engine
│   │   ├── trigger.go            # Secret rotation orchestration
│   │   ├── state_tracker.go      # Persist rotation state (crash recovery)
│   │   ├── cache.go              # Secret caching with TTL
│   │   └── timeout_controller.go # Per-secret timeout isolation
│   │
│   ├── rotation/                 # Container rotation logic
│   │   ├── rolling_strategy.go   # Blue-green deployment, atomic swap
│   │   ├── health_check.go       # Health verification with retries
│   │   └── lock_manager.go       # Distributed locking (concurrency)
│   │
│   ├── watcher/                  # Event detection
│   │   └── controller.go         # Docker event stream, reconciliation
│   │
│   ├── events/                   # Event queue
│   │   ├── queue.go              # Bounded queue with backpressure
│   │   ├── deduplication.go      # Rapid event coalescing
│   │   └── backpressure.go       # Drop policy when queue full
│   │
│   ├── providers/                # Provider system
│   │   ├── manager.go            # Plugin discovery, lifecycle
│   │   ├── store.go              # Provider registry
│   │   └── supervisor.go         # Plugin restart on crash
│   │
│   └── core/                     # Shared utilities
│       ├── compose.go            # Docker Compose AST handling
│       └── encryption.go         # AES-256-GCM cipher
│
├── pkg/                          # Public packages (importable by plugins)
│   ├── api/                      # SecretProvider interface
│   ├── config/                   # dso.yaml schema
│   └── observability/            # Prometheus metrics
│
├── providers/                    # Built-in provider plugins
│   ├── vault/                    # HashiCorp Vault
│   ├── aws/                      # AWS Secrets Manager
│   ├── azure/                    # Azure Key Vault
│   └── local/                    # Local encrypted vault
│
├── test/                         # Tests
│   ├── integration/              # Docker integration tests
│   ├── unit/                     # Unit tests
│   └── testdata/                 # Test fixtures
│
├── docs/                         # Documentation
├── scripts/                      # Build, install, CI
└── ARCHITECTURE.md               # Deep architecture reference
```

### Key Package Dependencies

```
Agent tier (orchestration):
  trigger.go → rotation/ + providers/ + watcher/

Rotation tier (execution):
  rolling_strategy.go → health_check.go, lock_manager.go

Provider tier (backends):
  providers/manager.go → plugins (isolated processes)

Event tier (detection):
  watcher/controller.go → events/ (queue, dedup)

Cache tier (optimization):
  agent/cache.go → (no dependencies, standalone)
```

---

## Development

### Local Setup

```bash
# Clone repository
git clone https://github.com/docker-secret-operator/dso.git
cd dso

# Install dependencies
go mod download

# Build locally
go build -o dso ./cmd/dso
go build -o dso-agent ./cmd/dso-agent

# Run tests with race detection
go test -race ./...

# Lint
golangci-lint run ./...
```

### Running Tests

```bash
# Unit tests only
go test ./internal/... -v

# Integration tests (requires Docker)
go test -tags=integration ./test/integration/... -v

# Race detector (catch concurrency bugs)
go test -race ./...

# Coverage
go test -cover ./...
```

### Key Test Patterns

**State Tracker**: Verifies persistence of rotation state across restarts
**Health Check**: Tests container restart detection, timeout handling
**Lock Manager**: Tests concurrent access, stale lock cleanup
**Event Queue**: Tests backpressure, deduplication under load
**Rotation**: End-to-end blue-green deployment with health checks

### Contributing

1. Fork repository
2. Create feature branch: `git checkout -b feature/...`
3. Make changes; ensure tests pass: `go test -race ./...`
4. Lint: `golangci-lint run ./...`
5. Commit with clear message
6. Push and open PR against `main`

**Code review criteria:**
- Tests for all new functionality
- No race conditions (`-race` flag must pass)
- No silent error swallowing
- Backward compatible APIs
- Clear log messages for operational visibility

---

## Architecture Deep Dive

### Why Event-Driven Rotation?

Alternative approaches and their tradeoffs:

| Approach | Latency | Complexity | Reliability |
|---|---|---|---|
| **Event-driven** (DSO) | ~30s | High (event queue, dedup) | High (handles failures well) |
| Polling only | 5-10m | Low | Medium (periodic misses) |
| Manual (operator) | Hours | Low | Low (error-prone) |
| Kubernetes operators | Real-time | Very high | High (consensus built-in) |

DSO chose event-driven because:
- **Latency**: 30s is acceptable for rotation; faster than polling
- **Reliability**: Complete recovery from network partitions
- **Operability**: Structured events make debugging easier

### Why Local State Tracking?

DSO persists rotation state to `~/.dso/state/` because:

1. **Crash detection**: Orphaned containers are detectable
2. **Operator visibility**: State query shows pending rotations
3. **Manual recovery**: Operators can inspect and retry

Tradeoff: Single-agent deployments only. Multi-agent coordination would require distributed consensus (etcd, Consul).

### Why File-Based Locking?

DSO uses file-based locks (`/var/lib/dso/locks/`) because:

1. **Simplicity**: No external service required
2. **Visibility**: Locks are inspectable files
3. **Stale detection**: Can identify and clean up old locks

Tradeoff: Does not scale to 1000+ secrets. For large deployments, would need etcd-based locking.

---

## Roadmap & Future Direction

### Near-term (Current Release)
- ✅ Event-driven rotation
- ✅ Blue-green deployment with rollback
- ✅ Multi-provider support (Vault, AWS, Azure)
- ✅ State recovery after crashes
- ✅ Prometheus observability

### Medium-term (Next releases)
- [ ] Distributed consensus layer (etcd/Consul) for multi-agent coordination
- [ ] Automated cleanup of orphaned containers
- [ ] Additional providers (HashiCorp Boundary, custom HTTP backends)
- [ ] Health check extensibility (custom scripts, HTTP endpoints)
- [ ] Metrics-driven rotation (rotate only if changed)

### Long-term (Strategic direction)
- [ ] HA deployments (active-active or active-standby)
- [ ] Integration with orchestrators (Docker, Podman)
- [ ] Policy-based rotation (rotate all secrets every X days)
- [ ] Audit trail integration (ship to external audit log)

### Out of Scope
- Kubernetes (use ExternalSecrets Operator)
- Secrets management backend (use Vault, AWS, Azure)
- Secret scanning / compliance checking
- Multi-tenant isolation

---

## FAQ

### Why not Docker Secrets?

Docker Secrets requires Docker Swarm. If you're on `docker compose` (most teams), it's not available.

| Feature | Docker Secrets | DSO |
|---|---|---|
| Works with `docker compose` | ❌ No | ✅ Yes |
| Requires Swarm | ✅ Yes | ❌ No |
| Automatic rotation | ❌ No | ✅ Yes |
| Local dev workflow | ❌ No | ✅ Yes |

**Use Docker Secrets if:** You're already on Swarm and happy with it.  
**Use DSO if:** You're on `docker compose` and need rotation.

### Does DSO persist secrets to disk?

No. Secrets are:
- Decrypted only in agent memory
- Injected into container at runtime via tmpfs
- Never written to host filesystem
- Cleaned up when container stops

Only the encrypted vault file (`~/.dso/vault.enc` or provider backend) is persisted.

### Does DSO support Kubernetes?

No. DSO is for Docker Compose. For Kubernetes, use [ExternalSecrets Operator](https://external-secrets.io/).

### How fast is rotation?

~30 seconds typical:
- 5s debounce
- 15-20s app startup + health check
- 1s atomic swap
- 3s graceful stop

Depends on app startup time. Can be tuned with `health_check.timeout` in config.

### What happens if rotation fails?

Rotation is fully rolled back:
1. Old container is restored (renamed back)
2. New container is stopped and removed
3. Error is logged with container IDs
4. Operators review logs and retry

No data loss; original container remains active.

### What if the agent crashes?

State is persisted to disk. On restart:
1. Agent reads rotation state file
2. Detects in-progress rotations (older than 5 minutes)
3. Marks them as "rollback_required"
4. Logs ERROR message with container IDs
5. Operators clean up manually

Prevented by: automatic rollback of incomplete rotations.

### Can I run multiple DSO agents?

Not recommended. Each agent assumes it owns the local Docker socket and manages all containers. Multiple agents could:
- Race on container renames
- Interfere with each other's rotations
- Corrupt state

**Recommended pattern:** One agent per Docker host. For multi-host, run agent on each host independently.

### How do I debug a stuck rotation?

```bash
# Check agent status
docker dso status

# Check logs
docker logs <agent_container>

# Inspect container state
docker ps -a | grep <secret_name>

# Check state file
cat ~/.dso/state/rotations.json

# For manual recovery:
docker inspect <container_id> | jq '.Name'
docker rm <orphaned_container>
```

### How do I add a custom provider?

Implement the `SecretProvider` interface in `pkg/api/`:

```go
type SecretProvider interface {
    GetSecret(ctx context.Context, name string) (map[string]string, error)
    WatchSecret(name string, interval time.Duration) (<-chan SecretUpdate, error)
}
```

1. Create binary `dso-provider-myservice`
2. Implement go-plugin interface
3. Place at `/usr/local/lib/dso/plugins/dso-provider-myservice`
4. Reference in dso.yaml: `provider: myservice`

See [provider development guide](docs/providers.md) for details.

### How do I report a security issue?

Please email security@docker-secret-operator.org with:
- Description of vulnerability
- Steps to reproduce
- Your contact information

Do not open public issues for security vulnerabilities.

---

## Comparison to Alternatives

### vs. Doppler

| | DSO | Doppler |
|---|---|---|
| Requires SaaS | ❌ No | ✅ Yes |
| Self-hosted | ✅ Yes | ❌ No |
| Docker Compose support | ✅ Yes | ✅ Yes (CLI) |
| Automatic rotation | ✅ Yes | Manual |
| Local development | ✅ Yes | ✅ Yes |
| Cost | Free (open source) | Paid SaaS |

Use DSO if you want self-hosted, automatic rotation, zero cost.

### vs. External Secrets Operator

ESO is for Kubernetes; DSO is for Docker Compose.

| | DSO | ESO |
|---|---|---|
| Kubernetes | ❌ No | ✅ Yes |
| Docker Compose | ✅ Yes | ❌ No |
| Auto rotation | ✅ Yes | ✅ Yes |
| Multi-backend | ✅ Yes | ✅ Yes |

Use ESO if you're on Kubernetes; use DSO if you're on Docker Compose.

### vs. Vault Agent

Vault Agent templates secrets into files for consumption. DSO injects at container startup.

| | DSO | Vault Agent |
|---|---|---|
| No setup required | ✅ Local mode | ❌ Requires Vault |
| Auto rotation | ✅ Yes | ✅ Yes |
| Works with docker compose | ✅ Yes | ⚠️ Via template |
| Operational complexity | Low | Medium |

Use DSO for simplicity; use Vault Agent if already running Vault infrastructure.

---

## Support & Community

- **GitHub Issues**: [Report bugs](https://github.com/docker-secret-operator/dso/issues)
- **Discussions**: [Community Q&A](https://github.com/docker-secret-operator/dso/discussions)
- **Security**: security@docker-secret-operator.org
- **Documentation**: [Full docs](docs/)

---

<p align="center">Built by the open-source community for Docker Compose teams who need secret rotation without Swarm or Kubernetes.</p>

## Troubleshooting

### Bootstrap Issues

**"Docker socket not accessible"**
```bash
# Verify Docker is running
docker ps

# Check socket permissions
ls -la /var/run/docker.sock

# Add current user to docker group
sudo usermod -aG docker $USER
newgrp docker
```

**"Vault initialization failed"**
```bash
# Remove failed state and retry
rm -rf ~/.dso
docker dso bootstrap local
```

### Common Diagnostics

**Check environment health:**
```bash
docker dso doctor --level full
```

**Monitor runtime status:**
```bash
docker dso status --watch
```

**View agent logs (production):**
```bash
docker dso system logs -f
```

**Validate configuration:**
```bash
docker dso config validate
```

### Agent Service Issues

**Service won't start:**
```bash
# Check systemd status
sudo systemctl status dso-agent

# View full service logs
sudo journalctl -u dso-agent -n 100

# Verify permissions
sudo docker dso doctor --level full
```

**Configuration mismatch:**
```bash
# Edit configuration
sudo nano /etc/dso/dso.yaml

# Validate changes
sudo docker dso config validate

# Restart service
sudo docker dso system restart
```

---

## Development Modes

### Local Development

Use for development and testing on a single machine. Secrets stored in encrypted local vault.

```bash
docker dso bootstrap local
docker dso status
docker dso compose up
```

**Characteristics:**
- Non-root operation
- Local encrypted storage
- No systemd service
- Quick setup (<2 minutes)

### Agent Mode (Production)

Use for production deployments on single Docker hosts. Managed via systemd with automatic restarts.

```bash
sudo docker dso bootstrap agent
sudo docker dso system enable
docker dso status --watch
```

**Characteristics:**
- Requires root/sudo
- Systemd-managed service
- Persistent state tracking
- Health checks and monitoring
- Automatic restart on failure

---

## Complete Workflow Example

### Step 1: Install

```bash
# Download and install binary
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sh
```

### Step 2: Initialize

```bash
# Bootstrap local environment
docker dso bootstrap local
```

### Step 3: Validate

```bash
# Check everything is working
docker dso doctor
docker dso status
```

### Step 4: Configure

```bash
# View current configuration
docker dso config show

# Edit if needed
docker dso config edit

# Validate changes
docker dso config validate
```

### Step 5: Deploy

```bash
# Use secrets in docker-compose.yaml
# services:
#   database:
#     environment:
#       PASSWORD: dso://myapp/db_password

# Deploy with DSO
docker dso compose up
```

### Step 6: Monitor

```bash
# Watch status in real-time
docker dso status --watch

# View logs
docker dso system logs -f

# Check diagnostics
docker dso doctor
```

---
