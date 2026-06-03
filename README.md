# Docker Secret Operator (DSO)

**Runtime secret injection and automatic rotation for Docker Compose — as a Docker CLI Plugin**

[![Latest Release](https://img.shields.io/github/v/release/docker-secret-operator/dso?label=latest)](https://github.com/docker-secret-operator/dso/releases/latest)
[![License](https://img.shields.io/github/license/docker-secret-operator/dso)](LICENSE)
[![Go Report](https://goreportcard.com/badge/github.com/docker-secret-operator/dso)](https://goreportcard.com/report/github.com/docker-secret-operator/dso)
[![Code Coverage](https://codecov.io/gh/docker-secret-operator/dso/branch/main/graph/badge.svg)](https://codecov.io/gh/docker-secret-operator/dso)

> **Current version: v3.5.20**  
> **Status**: CNCF Sandbox Ready ✅  
> **Governance**: [View GOVERNANCE.md](GOVERNANCE.md) | **Roadmap**: [View ROADMAP.md](ROADMAP.md)

---

## What is DSO?

DSO is a runtime secret injection daemon for Docker and Docker Compose. It solves a concrete operational problem: **how to rotate secrets in containerized applications safely without exposing them to the host filesystem or Docker's metadata layers**.

**In 30 seconds:**
- Inject secrets from AWS Secrets Manager, Azure Key Vault, HashiCorp Vault, or local encrypted storage
- Automatically rotate containers when secrets change — zero-downtime rolling swap by default
- Keep secrets out of logs, `docker inspect`, and host disk
- Single Docker host per agent (no Kubernetes required)

---

## Core Features

| Feature | Description |
|---------|-------------|
| **Zero-Persistence** | Plaintext secrets never written to disk; held only in process memory and tmpfs |
| **Rolling Rotation** | Zero-downtime blue-green container swap — new container starts, health-checked, old container stops |
| **Multi-Provider** | Works with AWS Secrets Manager, Azure Key Vault, HashiCorp Vault, or local encrypted storage |
| **Non-Root Operation** | Members of the `dso` group can run all standard commands without `sudo` |
| **Deterministic Rollback** | Failed rotations automatically restore the previous container state |
| **TCP Proxy** | DSO owns host port bindings (e.g. MySQL 3306) so traffic is never interrupted during rotation |
| **Crash Recovery** | Agent restarts automatically recover orphaned containers and resume incomplete rotations |
| **Production-Ready** | systemd integration, crash recovery, Prometheus metrics, comprehensive monitoring |
| **No Swarm/Kubernetes** | Works with standard `docker compose` on any machine |

---

## Quick Start

### For Local Development (2-3 minutes)

```bash
# 1. Install DSO
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | bash

# 2. Run setup wizard for local mode
docker dso setup --mode local

# 3. Initialize the local vault
docker dso init

# 4. Store a secret
docker dso secret set myapp/db_password

# 5. Add secret references to docker-compose.yaml
# services:
#   app:
#     image: myapp:latest
#     environment:
#       DB_PASSWORD: dso://myapp/db_password

# 6. Deploy
docker dso up -d

# 7. Verify
docker dso doctor
docker dso status
```

### For Production (Agent Mode with Cloud Provider)

```bash
# 1. Install DSO system-wide
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sudo bash

# 2. Bootstrap agent mode (auto-detects cloud provider, configures systemd service)
sudo docker dso bootstrap agent

# 3. Configure your secrets
sudo vi /etc/dso/dso.yaml

# 4. Start the agent
sudo systemctl start dso-agent
sudo systemctl enable dso-agent

# 5. Verify everything is healthy
docker dso doctor
docker dso status
```

### Local Development (no systemd)

```bash
# 1. Install DSO
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | bash

# 2. Run setup wizard for local mode
docker dso setup --mode local

# 3. Initialize the local vault
docker dso init

# 4. Set a secret
docker dso secret set app/db_password

# 5. Use in docker-compose.yaml
# services:
#   postgres:
#     image: postgres:15
#     environment:
#       POSTGRES_PASSWORD_FILE: dsofile://app/db_password

# 6. Deploy
docker dso up -d

# 7. Verify
docker dso doctor
```

### Advanced: Non-interactive Setup

```bash
# Local mode (non-interactive)
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | bash
docker dso setup --mode local --non-interactive
docker dso init

# Agent mode (non-interactive)
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sudo bash
sudo docker dso bootstrap agent --non-interactive --provider aws
```

---

## How It Works

```
Secret Backend (AWS / Azure / Vault / Local)
    ↓ polling every 30s–5m (adaptive)
DSO Agent detects secret change
    ↓
Acquire distributed lock (prevent concurrent rotation)
    ↓
Create new container with updated secret env
    ↓
Health check new container
    ↓
Atomic swap (rename old → backup, new → active)
    ↓
DSO TCP Proxy re-routes traffic to new container
    ↓
Stop old container
    ↓
Rollback on failure (auto-restore previous state)
```

**Result**: Secrets never written to host disk. Zero-downtime rolling rotation completes in ~30 seconds. Failed rotations automatically restore the previous state.

---

## Installation

### Prerequisites

- **Docker** 20.10+ with `docker compose`
- **Linux** (amd64, arm64) — macOS supported for local mode only
- **systemd** — required for Cloud/Agent mode
- **Non-root access** — add your user to the `dso` group after setup:
  ```bash
  sudo usermod -aG dso $USER
  # log out and back in to apply
  ```

### Install

```bash
# System-wide install (recommended for production)
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sudo bash

# User install (local development only)
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | bash

# Verify
docker dso version
docker dso doctor
```

See **[Getting Started Guide](docs/getting-started.md)** for detailed setup instructions.

---

## Configuration

Configuration lives at `/etc/dso/dso.yaml` (Cloud/Agent mode) or `./dso.yaml` (Local mode).  
Members of the `dso` group can read and edit `/etc/dso/dso.yaml` without `sudo`.

### Cloud Mode — AWS Example

```yaml
# /etc/dso/dso.yaml
version: v1.0.0
mode: agent

providers:
  aws:
    type: aws
    region: us-east-1
    auth:
      method: iam_role        # Uses EC2 instance role — no credentials in config
    retry:
      attempts: 3
      backoff: "1s"

agent:
  cache: true
  watch:
    mode: polling
    polling_interval: "30s"   # How often to check for secret changes
  rotation:
    enabled: true
    strategy: rolling         # Zero-downtime blue-green swap (default)
    health_check_timeout: "30s"

defaults:
  inject:
    type: env
  rotation:
    enabled: true
    strategy: rolling

secrets:
  - name: database_credentials
    provider: aws
    rotation:
      enabled: true
      strategy: rolling       # rolling | restart | signal | none
    targets:
      containers:
        - mysql               # Container name (matches dso.reloader label)
    mappings:
      MYSQL_ROOT_PASSWORD: prod/mysql/root_password
      MYSQL_PASSWORD: prod/mysql/app_password
```

### Cloud Mode — HashiCorp Vault Example

```yaml
version: v1.0.0
mode: agent

providers:
  vault:
    type: vault
    auth:
      method: token
      params:
        address: https://vault.example.com:8200
        token: "${VAULT_TOKEN}"

agent:
  watch:
    mode: polling
    polling_interval: "30s"

secrets:
  - name: app_secrets
    provider: vault
    mappings:
      DB_PASSWORD: secret/data/prod/database/password
      API_KEY: secret/data/prod/api/key
```

### Local Mode

```yaml
# ./dso.yaml
version: v1.0.0
mode: local

providers:
  local:
    type: local
    vault_file: ~/.dso/vault.enc
    master_key_file: ~/.dso/master.key

secrets: {}
```

See **[Configuration Reference](docs/configuration.md)** for all options.

---

## Docker Compose Integration

Label your containers so DSO knows which ones to manage:

```yaml
# docker-compose.yaml
services:
  mysql:
    image: mysql:8
    labels:
      dso.reloader: "true"                  # DSO will manage this container
      dso.secrets: "database_credentials"   # Which secret to inject
      dso.update.strategy: "rolling"        # Zero-downtime swap (default)
      dso.host_ports: "3306:3306"           # DSO TCP Proxy owns this port binding
    environment:
      MYSQL_ROOT_PASSWORD: ""               # Injected at runtime by DSO
      MYSQL_PASSWORD: ""
    expose:
      - "3306"                              # Expose internally; DSO proxy handles host binding

  app:
    image: myapp:latest
    labels:
      dso.reloader: "true"
      dso.secrets: "database_credentials"
      dso.update.strategy: "rolling"
    environment:
      DB_PASSWORD: ""                       # Injected at runtime by DSO
```

Start with:
```bash
docker dso compose up    # Injects DSO labels and starts containers
```

---

## Non-Root Access

After running `docker dso setup` or `docker dso bootstrap agent`, all DSO directories and the agent socket are group-owned by `dso`:

| Path | Permissions | Notes |
|------|------------|-------|
| `/etc/dso/` | `0775 root:dso` | Group members can list and read |
| `/etc/dso/dso.yaml` | `0664 root:dso` | Group members can read and edit |
| `/run/dso/dso.sock` | `0660 root:dso` | Group members can connect to agent |

Add your user to the `dso` group once and all DSO commands work without `sudo`:

```bash
sudo usermod -aG dso $USER
newgrp dso                   # Apply immediately without logout

# These now work without sudo:
docker dso watch
docker dso status
docker dso compose up
vi /etc/dso/dso.yaml

# These still require sudo (system-level operations):
sudo docker dso system enable
sudo systemctl restart dso-agent
```

---

## Architecture Overview

```
Secret Backends (AWS / Azure / Vault / Local)
                ↓  polling / webhooks
        DSO Agent Process  (systemd: dso-agent.service)
    ┌──────────────────────────────────┐
    │  Trigger Engine  (polling loop)  │
    │  Reloader Controller             │
    │  TCP Proxy Manager               │
    │  State Tracker + Lock Manager    │
    │  Provider Plugin System          │
    │  Crash Recovery                  │
    └──────────────────────────────────┘
         │              │              │
    IPC Socket     REST API      Docker Plugin Socket
  /run/dso/dso.sock  :8471      /run/docker/plugins/dso.sock
         │
    Docker Host Containers
```

**Ports & Sockets**:

| Endpoint | Default | Purpose |
|----------|---------|---------|
| `127.0.0.1:8471` | TCP | REST API — health, metrics, events, webhook |
| `/run/dso/dso.sock` | Unix | IPC — CLI→agent communication |
| `/run/docker/plugins/dso.sock` | Unix | Docker V2 secret driver plugin |
| Dynamic (e.g. `3306`) | TCP | DSO TCP Proxy — owns container host port bindings |

**Design principles**:
- Single agent per Docker host
- Timer-based adaptive polling (backs off up to 4× when no changes detected)
- Blue-green rolling rotation with atomic container swap
- Local state persistence for crash recovery
- Plugin-based provider system (separate binaries per provider)

See **[Architecture Guide](docs/architecture.md)** for details.

---

## Operations

### Monitor Status

```bash
# Real-time status
docker dso status

# Watch metrics continuously
docker dso status --watch

# JSON output for scripting
docker dso status --json
```

### Watch Rotations Live

```bash
# Live event stream (Docker events + DSO rotation events)
docker dso watch

# With raw event payloads
docker dso watch --debug
```

### View Logs

```bash
# Follow agent logs (via journald)
docker dso system logs -f

# View errors only
docker dso system logs -p err

# Last hour
docker dso system logs --since 1h

# Via REST API (when journald unavailable)
docker dso system logs --api --api-addr http://localhost:8471
```

### Health Check

```bash
# CLI health check
docker dso doctor

# Full diagnostics
docker dso doctor --level full

# REST health endpoint
curl http://localhost:8471/health

# Validate config
docker dso config validate
```

### Service Management

```bash
# Start / stop / restart
sudo systemctl start dso-agent
sudo systemctl stop dso-agent
sudo systemctl restart dso-agent

# Enable / disable autostart
sudo docker dso system enable
sudo docker dso system disable

# View service status
sudo docker dso system status
```

See **[Operational Guide](docs/operational-guide.md)** for day-2 operations, monitoring, and recovery procedures.

---

## Rotation Strategies

| Strategy | Behaviour | Use Case |
|----------|-----------|---------|
| `rolling` | Zero-downtime blue-green swap. New container starts, health-checked, old container stops. DSO TCP Proxy holds port bindings. | **Default — production databases, APIs** |
| `restart` | Stop old container, start new container with updated env. Brief downtime. | Stateless services where downtime is acceptable |
| `signal` | Send SIGHUP to running container (no restart). | Applications that reload config on SIGHUP |
| `none` | Update secret cache only, no container action. | Manual rotation workflows |

Configure per-secret or globally in `dso.yaml`:

```yaml
defaults:
  rotation:
    strategy: rolling           # applies to all secrets unless overridden

secrets:
  - name: api_keys
    rotation:
      strategy: signal          # override for this secret only
```

Or per-container via label:

```yaml
labels:
  dso.update.strategy: "rolling"
```

---

## Limitations

DSO is optimized for **single-host Docker Compose environments**:

| Limit | Details |
|-------|---------|
| **Scope** | Single Docker host per agent (no multi-host coordination) |
| **State** | Local persistence only (no distributed consensus) |
| **Locking** | File-based locking (scales to ~100s of secrets) |
| **Recovery** | Some edge cases require manual operator recovery (documented) |
| **Isolation** | Assumes Docker daemon is trusted and secure |

**Not recommended for**: Kubernetes (use ExternalSecrets Operator), multi-host setups, 1000+ secrets per agent, multi-tenant strict isolation.

---

## Security Model

**Core principle**: Plaintext secrets never touch the host filesystem.

- Secrets held only in process memory and container tmpfs
- AES-256-GCM encryption for local vault
- Log redaction (secrets never appear in logs)
- File injection invisible to `docker inspect`
- Agent IPC socket restricted to `root:dso` group (`0660`)
- Docker plugin socket restricted to root only (`0600`)
- Automatic cleanup on container stop/restart

See **[Security Model](SECURITY.md)** for detailed threat analysis and guarantees.

---

## Examples

Working examples for common providers:

- **Local Development** — [examples/](examples/)
- **AWS Secrets Manager** — [examples/aws-compose/](examples/aws-compose/)
- **Azure Key Vault** — [examples/azure-compose/](examples/azure-compose/)
- **HashiCorp Vault** — [examples/hashicorp-vault/](examples/hashicorp-vault/)
- **Huawei Cloud KMS** — [examples/huawei-compose/](examples/huawei-compose/)

---

## Documentation

| Document | Purpose |
|----------|---------|
| **[Getting Started](docs/getting-started.md)** | Installation & first deployment |
| **[CLI Reference](docs/cli.md)** | Complete command reference |
| **[Configuration](docs/configuration.md)** | YAML schema & all options |
| **[Providers](docs/providers.md)** | Provider-specific setup guides |
| **[Architecture](docs/architecture.md)** | System design & internals |
| **[Operational Guide](docs/operational-guide.md)** | Day-2 operations & monitoring |
| **[Recovery Procedures](docs/RECOVERY_PROCEDURES.md)** | Failure recovery & troubleshooting |
| **[Security Model](SECURITY.md)** | Threat analysis & guarantees |
| **[Persistence Model](docs/PERSISTENCE_MODEL.md)** | What data DSO persists |

---

## Support & Contributing

- **Issues**: [Report bugs](https://github.com/docker-secret-operator/dso/issues)
- **Discussions**: [Community Q&A](https://github.com/docker-secret-operator/dso/discussions)
- **Security**: umairmd385@gmail.com
- **Contributing**: [CONTRIBUTING.md](CONTRIBUTING.md)

---

## License

DSO is licensed under the **[Apache 2.0](LICENSE)**.

---
