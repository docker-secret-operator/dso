# Docker Secret Operator (DSO)

**Runtime secret injection and automatic rotation for Docker Compose**

[![Latest Release](https://img.shields.io/github/v/release/docker-secret-operator/dso?label=latest)](https://github.com/docker-secret-operator/dso/releases/latest)
[![License](https://img.shields.io/github/license/docker-secret-operator/dso)](LICENSE)
[![Go Report](https://goreportcard.com/badge/github.com/docker-secret-operator/dso)](https://goreportcard.com/report/github.com/docker-secret-operator/dso)
[![Build Status](https://img.shields.io/github/actions/workflow/status/docker-secret-operator/dso/ci.yml?branch=main)](https://github.com/docker-secret-operator/dso/actions)

---

## Overview

DSO is a runtime secret injection daemon for Docker Compose that solves a fundamental operational problem: **how to safely rotate secrets in containerized applications without exposing them to the host filesystem or Docker metadata layers**.

Unlike traditional approaches (environment variables, `.env` files, docker compose secrets), DSO:
- **Never persists secrets to disk** — decrypted secrets exist only in process memory and container `tmpfs`
- **Supports automatic rotation** — detects secret changes and rolls containers with blue-green deployment
- **Integrates with secret backends** — works with Vault, AWS Secrets Manager, Azure Key Vault, or local encrypted storage
- **Provides atomic rollback** — failed rotations automatically restore the previous container
- **Requires no Swarm or Kubernetes** — works with standard `docker compose` on any machine

DSO is designed for teams running Docker Compose in development, CI/CD, and production environments who need secret rotation capabilities without adopting Kubernetes or Docker Swarm.

---

## Why DSO Exists

### The Problem

Three concrete problems make secrets in Docker Compose dangerous:

**1. Persistent Disk Exposure**
```bash
.env → docker compose reads → env vars in process memory → ENV in container
↓
Secret lives on disk from creation until cleanup
↓
Forensic recovery, accidental commits, misconfiguration = breach
```

**2. `docker inspect` Leakage**
Any process with Docker access can inspect a running container and read all environment variables in plaintext:
```bash
$ docker inspect <container_id> | jq '.Config.Env[]'
POSTGRES_PASSWORD=secret123  # ← Visible to anyone with docker access
```

**3. No Rotation Mechanism**
Rotating a secret requires:
- Manual container recreation
- Downtime coordination
- Script complexity
- Error-prone multi-step procedures

### The Solution

DSO replaces this workflow with:

```
Secret Backend (Vault/AWS/Local) → DSO Agent → Docker Compose
                                      ↓
                                Detect change
                                      ↓
                              Create shadow container
                                      ↓
                            Inject secrets at startup
                                      ↓
                            Verify health (>5s health checks)
                                      ↓
                         Atomic swap (container rename)
                                      ↓
                           Stop old container
                                      ↓
                      Rollback on failure (undo rename)
```

**Key properties:**
- Secrets never written to host disk
- Rotation is deterministic and fully reversible
- Applications see no downtime for read-only rotations
- Failed rotations automatically restore previous state

---

## Key Capabilities

### Runtime Secret Injection
- **`dso://` (environment)**: Inject secret as env var (use when app must read from `ENV`)
- **`dsofile://` (tmpfs)**: Inject secret into container's RAM disk at `/run/secrets/` (preferred — invisible to `docker inspect`)
- **Resolved at container start** — not during compose parsing
- **Secrets never persisted** — only exist in container memory

### Automatic Rotation
- **Event-driven** — detects secret changes in real-time from provider
- **Blue-green deployment** — create new container, verify health, atomic swap
- **Atomic guarantees** — swap is all-or-nothing; partial states are rolled back
- **Zero-downtime** (for stateless apps) — clients see no interruption
- **Automatic rollback** — failed rotation automatically restores previous container

### Multi-Provider Architecture
- **Local mode**: AES-256-GCM encrypted vault at `~/.dso/vault.enc` (no cloud account needed)
- **Cloud mode**: Integration with enterprise secret backends
  - HashiCorp Vault (fully supported)
  - AWS Secrets Manager (fully supported)
  - Azure Key Vault (fully supported)
  - Custom providers via plugin interface

### Health Verification
- **Native Docker health checks** — respects container's HEALTHCHECK directive
- **Custom exec probes** — verify specific files or commands
- **Retry logic** — configurable timeouts and retry counts
- **Prevents broken containers from becoming active** — strict health criteria

### Observability
- **Prometheus metrics** — rotation timing, success/failure rates, cache stats
- **Structured logging** — event-driven logs with context propagation
- **Runtime status** — query agent for pending rotations, cache state
- **Debug tools** — introspect provider communication, event queue, rotation state

---

## Architecture Overview

### Component Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│ Docker Compose Environment                                      │
└─────────────────────────────────────────────────────────────────┘
                              ▲
                              │ docker compose up
                              │
┌─────────────────────────────────────────────────────────────────┐
│ DSO Agent (Daemon Process)                                      │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐           │
│  │  Watcher    │  │  Rotation   │  │   Health    │           │
│  │  (Events)   │  │   Engine    │  │   Check     │           │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘           │
│         │                │                │                   │
│         └────────┬───────┴────────┬───────┘                   │
│                  │                │                            │
│         ┌────────▼────────┐       │                           │
│         │   Event Queue   │       │                           │
│         │ (Debounced)     │       │                           │
│         └────────┬────────┘       │                           │
│                  │                │                            │
│         ┌────────▼────────────────▼────────┐                  │
│         │   Trigger Engine                 │                  │
│         │  (Execute Rotations)             │                  │
│         └────────┬───────────────┬─────────┘                  │
│                  │               │                             │
│      ┌───────────▼──────┬────────▼──────────┐                │
│      │   State Tracker  │  Lock Manager     │                │
│      │ (Crash Recovery) │ (Concurrency)     │                │
│      └────────┬─────────┴────────┬──────────┘                │
│               │                  │                             │
│      ┌────────▼──────────────────▼────────┐                  │
│      │   Provider Plugins                 │                  │
│      │ (Vault, AWS, Azure, Local)         │                  │
│      └────────────────┬────────────────────┘                  │
│                       │                                        │
└───────────────────────┼────────────────────────────────────────┘
                        │
          ┌─────────────▼──────────────┐
          │ Secret Backend             │
          │ (Vault/AWS/Azure/Local)    │
          └────────────────────────────┘
```

### Core Components

**Watcher** (`internal/watcher/`)
- Monitors Docker events and provider webhooks
- Detects container creation, removal, health state changes
- Triggers rotation when secret changes detected
- Debounces rapid changes to prevent thrashing

**Rotation Engine** (`internal/rotation/`)
- Orchestrates blue-green container deployment
- Creates shadow container with new secrets
- Verifies health with exponential backoff
- Performs atomic container rename (swap)
- Handles rollback on failure

**Event Queue** (`internal/events/`)
- Bounded queue with backpressure handling
- Deduplicates rapid events for same secret
- Worker pool for parallel event processing
- Metrics for queue depth and drop rate

**State Tracker** (`internal/agent/state_tracker.go`)
- Persists rotation state to filesystem (`/var/lib/dso/state/`)
- Enables crash recovery — detects interrupted rotations on restart
- Records rotation start time and status
- Marks stale rotations (>5 min) for manual intervention

**Lock Manager** (`internal/rotation/lock_manager.go`)
- Local (in-process) and distributed (file-based) locking
- Prevents concurrent rotation of same secret
- Stale lock detection and cleanup
- Prevents race conditions under network partition

**Provider Plugins** (`internal/providers/`)
- HashiCorp go-plugin RPC system
- Each provider is isolated subprocess
- Supports custom provider development
- Plugin discovery and lifecycle management

**Cache** (`internal/agent/cache.go`)
- In-memory secret caching with TTL
- Separate TTL for each provider (avoid over-rotation)
- Cache size limits to prevent memory bloat
- Pre-warming on agent startup

---

## How DSO Works Internally

### Secret Resolution Flow (Startup)

When `docker compose up` is executed with DSO secrets:

```
1. Docker calls dso://<secret-name> or dsofile://<secret-name>
   ↓
2. DSO Agent receives request (via Docker socket)
   ↓
3. Check cache:
   - If valid (not expired) → return cached value
   - If missing/expired → continue to step 4
   ↓
4. Query provider:
   - Authenticate to secret backend
   - Fetch secret
   - Store in cache (with TTL)
   ↓
5. For dsofile://:
   - Mount tmpfs at /run/secrets/
   - Write secret to file in tmpfs
   - Return path to container
   ↓
6. For dso://:
   - Return secret as environment variable
```

**Design decision:** Secrets are NOT resolved during compose parsing; they're resolved at container startup. This allows:
- Late binding to provider state
- Rotation without restarting services that don't use the secret
- Clean error handling per container

### Event Processing Pipeline

DSO's rotation detection is event-driven:

```
Docker Event                   Provider Webhook
      │                              │
      └──────────┬───────────────────┘
                 │
        ┌────────▼─────────┐
        │  Watcher         │
        │  (Deduplicate)   │
        └────────┬─────────┘
                 │
        ┌────────▼──────────────┐
        │  Event Queue          │
        │  (Bounded, Backpressure)
        └────────┬──────────────┘
                 │
        ┌────────▼─────────────────────┐
        │  Debounce Window             │
        │  (Batch rapid events)        │
        └────────┬────────────────────┘
                 │
        ┌────────▼──────────────────────┐
        │  Trigger Engine               │
        │  (Execute rotation logic)     │
        └──────────────────────────────┘
```

**Event sources:**
- **Docker watcher**: Container lifecycle changes (create/start/die/health_status)
- **Provider webhooks**: Secret updated at backend (Vault lease expiry, AWS rotation)
- **Polling interval**: Periodic refresh from provider (if webhooks unavailable)

**Debounce behavior:**
- Events for same secret coalesce within 5-second window
- Prevents cascading rotations from rapid secret changes
- Trades latency for stability

### Rotation Workflow

When a secret change is detected, DSO executes blue-green rotation:

```
Step 1: Verify lock
  └─ AcquireLock(secretName, 5sec timeout)
     └─ If fails: other agent rotating same secret, abort

Step 2: Create shadow container
  └─ docker run --name <original>-new
     └─ New container has new secret injected
     └─ Original container still running

Step 3: Wait for health
  └─ Poll container health status
  └─ Native health check (preferred): wait for "healthy"
  └─ No health check: wait 30s, assume ready
  └─ On restart detected: fail immediately
  └─ On timeout: fail and rollback

Step 4: Atomic swap
  └─ docker rename <original> <original>-old
  └─ docker rename <original>-new <original>
     └─ Two-step rename prevents partial state
     └─ If second rename fails: restore from <original>-old
  └─ Verify actual container state before recovery

Step 5: Stop old container
  └─ docker stop <original>-old (30s grace period)
  └─ docker rm <original>-old

Step 6: Verify new container
  └─ Poll container status
  └─ If crashes: undo rename, restore old container

On ANY failure:
  └─ Rollback step 4:
     └─ docker rename <original>-old <original>
     └─ docker stop/rm <original>-new
     └─ Mark rotation as requiring manual intervention
```

**Key safety guarantees:**
- **Atomic swap**: Only two containers exist at any point; never more
- **Automatic rollback**: Failed rotation restores original container
- **Idempotent**: Can be replayed without side effects
- **Crash-safe**: Interrupted rotation detected on restart, marked for manual review

### Rollback Behavior

Rollback is triggered on:
- Health check timeout (container never became healthy)
- Container restart during health verification
- Swap failure (rename fails)
- New container crash immediately after swap

Rollback process:
```
if rotation failed:
  1. Undo container renames (restore original)
  2. Stop new container
  3. Remove new container
  4. Mark rotation as "rollback_required" in state tracker
  5. Log container IDs for manual investigation
  6. Alert operators via logs (ERROR level)
```

Operators must manually clean up after rollback if needed (inspect state, verify application).

### Reconciliation (Network Partition Recovery)

When Docker daemon reconnects after a network partition, DSO performs immediate reconciliation:

```
Docker daemon → Event stream reconnects
                      │
                      ├─ Reset reconnect backoff timer
                      │
                      └─ Execute reconcileRuntimeState()
                           │
                           ├─ List all containers
                           ├─ Check for dual-running (DSO naming pattern)
                           ├─ If found:
                           │  └─ Keep newest, stop older
                           └─ Detect stale rotation state
                              └─ Mark for manual intervention
```

**Why this matters:**
- Network partition can cause agent to miss container rename events
- If agent renames container A→A-old but misses A-new→A, you have two active containers
- Reconciliation detects this within 1 second of reconnection
- Prevents service degradation from split-brain container state

### Provider Communication

Providers communicate via HashiCorp's go-plugin RPC:

```
┌──────────────────────┐
│ DSO Agent (Process A)│
└──────────┬───────────┘
           │ RPC socket
           │
┌──────────▼──────────────────┐
│ Provider Subprocess (Proc B) │
│ (dso-provider-vault)         │
│ (dso-provider-aws)           │
│ (dso-provider-azure)         │
└──────────┬──────────────────┘
           │ HTTP/gRPC to secret backend
           │
┌──────────▼──────────────────┐
│ Secret Backend               │
│ (Vault / AWS / Azure)        │
└──────────────────────────────┘
```

**Key properties:**
- Provider is isolated subprocess — crash doesn't kill agent
- RPC interface is version-stable — old agents work with new providers
- Plugin verification prevents unsigned binaries — SHA256 hash validation
- Automatic restart with exponential backoff if plugin crashes

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
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | bash
```

**Global install (Cloud Mode + systemd):**
```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sudo bash
```

**Manual install:**
```bash
# Download for your platform
curl -Lo dso https://github.com/docker-secret-operator/dso/releases/download/v1.0.0/dso-linux-amd64

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
# Docker Secret Operator v1.0.0

docker dso system doctor
# DSO System Diagnostics — v1.0.0
# ════════════════════════════════════════════════════════════
# Component         Status     Detail
# ────────────────────────────────────────────────────────────
# Binary            OK         /usr/local/lib/docker/cli-plugins/dso
# Detected Mode     LOCAL      ~/.dso/vault.enc exists
# Vault             OK         ~/.dso/vault.enc
```

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
# (All happens in ~10 seconds, zero downtime for clients)
```

### Cloud Mode (with Vault)

**1. Install globally**
```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sudo bash
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

### Secret Lifecycle

```
Secret entered by operator
  ↓ (in vault for Cloud mode, in local vault for Local mode)
  ↓
Provider retrieves on demand
  ↓
Secret decrypted in memory (process memory only)
  ↓
Injected into container at runtime
  ↓
Container reads from /run/secrets/ (tmpfs, not disk)
  ↓
Container process has secret in memory
  ↓
Memory is inaccessible to host (containerization boundary)
  ↓
On rotation:
  - Old secret overwritten in memory
  - Old container stopped and removed
  - Old secret no longer accessible
```

### Key Assumptions

**Operator trust**: DSO assumes the operator running the daemon and accessing the vault is trusted. No operator-facing secret validation.

**Container isolation**: Assumes standard Docker containerization prevents host processes from reading container memory.

**Provider security**: DSO trusts the configured provider (Vault, AWS, Azure) to secure secrets; it does not audit provider implementation.

**Local file permissions**: In Local Mode, `~/.dso/vault.enc` must have permissions `0600` (file-based encryption via AES-256-GCM).

**Network security**: Cloud Mode requires TLS to provider backends; DSO validates certificates by default.

### What DSO Does NOT Provide

**Multi-user isolation**: All secrets decrypted in single agent process; no per-container secret filtering.

**Operator audit**: No audit log of who rotated what secret; use provider audit (Vault audit logs, AWS CloudTrail).

**Secret scanning**: DSO does not scan container images for hardcoded secrets.

**Plugin verification**: SHA256 validation prevents tampering in transit; does not verify provider trustworthiness.

### Threat Model

| Threat | Mitigation | Residual Risk |
|---|---|---|
| Secret persisted to disk | Secrets only in memory/tmpfs; not written to host FS | Forensic recovery from swapped memory |
| `docker inspect` leakage | Use `dsofile://`; secret injected to tmpfs | `dso://` env vars visible to `docker inspect` |
| Plugin compromise | SHA256 verification before execution | Malicious plugin developer |
| Network eavesdropping | TLS to provider (mandatory) | TLS downgrade attack (requires network attacker + MITM) |
| Agent compromise | Compromised agent can decrypt cached secrets | Running as root required for cloud mode |
| Provider compromise | DSO trusts provider; uses provider's threat model | Out of scope (use provider's security) |

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
