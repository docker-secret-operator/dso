# Docker Secret Operator (DSO)

**Runtime secret injection and automatic rotation for Docker Compose — as a Docker CLI Plugin**

[![Latest Release](https://img.shields.io/github/v/release/docker-secret-operator/dso?label=latest)](https://github.com/docker-secret-operator/dso/releases/latest)
[![License](https://img.shields.io/github/license/docker-secret-operator/dso)](LICENSE)
[![Go Report](https://goreportcard.com/badge/github.com/docker-secret-operator/dso)](https://goreportcard.com/report/github.com/docker-secret-operator/dso)

---

## What is DSO?

DSO is a runtime secret injection daemon for Docker and Docker Compose. It solves a concrete operational problem: **how to rotate secrets in containerized applications safely without exposing them to the host filesystem or Docker's metadata layers**.

**In 30 seconds:**
- Inject secrets from Vault, AWS Secrets Manager, Azure Key Vault, or local encrypted storage
- Automatically rotate containers when secrets change
- Keep secrets out of logs, `docker inspect`, and host disk
- Single Docker host per agent (no Kubernetes required)

---

## Core Features

| Feature | Description |
|---------|-------------|
| **Zero-Persistence** | Plaintext secrets never written to disk; held only in process memory and tmpfs |
| **Automatic Rotation** | Detects secret changes and refreshes containers with blue-green deployment |
| **Multi-Provider** | Works with Vault, AWS Secrets Manager, Azure Key Vault, or local encrypted storage |
| **Deterministic Rollback** | Failed rotations automatically restore the previous container state |
| **Event-Driven** | Responds to secret changes within seconds via webhooks or polling |
| **Production-Ready** | systemd integration, crash recovery, comprehensive monitoring |
| **No Swarm/Kubernetes** | Works with standard `docker compose` on any machine |

---

## Quick Start

### Development & Production (2-3 minutes) — Recommended

```bash
# 1. Install DSO
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | bash

# 2. Run interactive setup wizard
docker dso setup

# The wizard will:
# - Auto-detect your environment (local or cloud)
# - Generate configuration
# - Install provider plugins
# - Validate setup

# 3. You're ready! Follow the wizard's next steps.
```

### Local Development Alternative (Manual Steps)

```bash
# 1. Install DSO
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | bash

# 2. Initialize local environment
docker dso bootstrap local

# 3. Set a secret
docker dso secret set app/db_password

# 4. Use in docker-compose.yaml
# services:
#   postgres:
#     image: postgres:15
#     environment:
#       POSTGRES_PASSWORD_FILE: dsofile://app/db_password

# 5. Deploy
docker dso compose up
```

### Production with Systemd Alternative (Manual Steps)

```bash
# 1. Install globally
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sudo bash

# 2. Initialize agent
sudo docker dso bootstrap agent

# 3. Configure providers
sudo nano /etc/dso/dso.yaml

# 4. Enable and start
sudo docker dso system enable

# 5. Verify
docker dso doctor
```

---

## How It Works

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

**Result**: Secrets never written to host disk. Rotation completes in ~30 seconds with minimal disruption. Failed rotations automatically restore previous state.

---

## Installation

### Prerequisites

- **Docker** (20.10+) with `docker compose`
- **Linux** (amd64, arm64) or **macOS** (amd64, arm64)
- **Root**: Not required for Local Mode; required for Cloud Mode (systemd setup)

### Install

```bash
# User install (Local Mode)
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | bash

# System-wide install (Cloud Mode + systemd)
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sudo bash

# Verify
docker dso version
docker dso doctor
```

See **[Getting Started Guide](docs/getting-started.md)** for detailed setup.

---

## Configuration

### Local Mode

```yaml
# ~/.dso/dso.yaml
providers:
  local:
    type: file
    enabled: true

agent:
  cache:
    ttl: 1h
    max_size: 500Mi
```

### Cloud Mode (Vault Example)

```yaml
# /etc/dso/dso.yaml
providers:
  vault:
    address: https://vault.example.com:8200
    auth:
      method: token
      token_env: VAULT_TOKEN
    mount_path: secret/data

agent:
  watch:
    polling_interval: 5m
  cache:
    ttl: 1h
```

See **[Configuration Reference](docs/configuration.md)** for all options.

---

## Secret Injection

### File Injection (Recommended)

```yaml
services:
  database:
    image: postgres:15
    environment:
      # Secret injected via tmpfs; invisible to docker inspect
      POSTGRES_PASSWORD_FILE: dsofile://app/db_password
    volumes:
      - data:/var/lib/postgresql/data
```

### Environment Injection (Legacy)

```yaml
services:
  api:
    image: myapp:latest
    environment:
      # Visible to docker inspect (not recommended for production)
      DATABASE_URL: dso://vault:database/url
```

---

## Architecture Overview

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

**Design**:
- Single-agent per Docker host
- Event-driven with debouncing
- Blue-green rotation with atomic swap
- Local state persistence (crash recovery)
- Plugin-based provider system

See **[Architecture Guide](docs/architecture.md)** for details.

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
- Automatic cleanup on container stop/restart

See **[Security Model](SECURITY.md)** for detailed threat analysis and guarantees.

---

## Operations

### Monitor Status

```bash
# Real-time status
docker dso status

# Watch metrics
docker dso status --watch

# JSON output
docker dso status --json
```

### View Logs

```bash
# Follow agent logs
docker dso system logs -f

# View errors only
docker dso system logs -p err

# Last hour of logs
docker dso system logs --since 1h
```

### Troubleshoot

```bash
# Health check
docker dso doctor

# Full diagnostics
docker dso doctor --level full

# Validate config
docker dso config validate
```

See **[Operational Guide](docs/operational-guide.md)** for day-2 operations, monitoring, and recovery procedures.

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
| **[Configuration](docs/configuration.md)** | YAML schema & options |
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
- **Security**: security@docker-secret-operator.org
- **Contributing**: [CONTRIBUTING.md](CONTRIBUTING.md)

---

## License

DSO is licensed under the **[MIT License](LICENSE)**.

---

<p align="center">Built by the open-source community for Docker Compose teams who need secret rotation without Swarm or Kubernetes.</p>
