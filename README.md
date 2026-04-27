<h1 align="center">Docker Secret Operator (DSO)</h1>
<p align="center">
  <b>Secure secret injection for Docker Compose — no .env files, no docker inspect leaks, no cloud account required.</b>
</p>
<p align="center">
  <a href="https://github.com/docker-secret-operator/dso/releases/latest"><img src="https://img.shields.io/github/v/release/docker-secret-operator/dso?label=latest" alt="Latest Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/docker-secret-operator/dso" alt="License"></a>
</p>

---

## 🚫 The Problem

`.env` files cause two serious security problems:

1. **Git leaks** — developers accidentally commit secrets to source control.
2. **`docker inspect` exposure** — environment variables are baked into container metadata and readable by anyone with host access.

## ✨ The Solution

DSO replaces `.env` files with an **encrypted local vault** and injects secrets at runtime — directly into container memory via `tmpfs`. Secrets never touch disk and are invisible to `docker inspect`.

It works in two modes depending on your environment:

| Mode | Use Case | Requires |
| :--- | :--- | :--- |
| **Local** (default) | Development, CI, offline | Nothing — no cloud, no root |
| **Cloud** (legacy/enterprise) | Production with AWS/Azure/Vault | Root, systemd |

---

## 🚀 Quick Start — Local Mode

> No cloud account. No Go installation. No root.

**1. Install DSO**
```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | bash
```

**2. Initialize your local vault**
```bash
docker dso init
```

**3. Store a secret**
```bash
docker dso secret set app/db_pass
# You will be prompted to enter the value invisibly
```

**4. Reference it in `docker-compose.yaml`**
```yaml
services:
  db:
    image: postgres:15
    environment:
      POSTGRES_PASSWORD_FILE: dsofile://app/db_pass
```

**5. Deploy**
```bash
docker dso up -d
```

DSO resolves secrets in memory, starts an inline agent, and passes a mutated compose file to Docker. Your secrets are never written to disk.

---

## ☁️ Cloud Mode — Enterprise Setup

For teams using AWS Secrets Manager, Azure Key Vault, or HashiCorp Vault via a long-running systemd agent:

```bash
# Install globally first
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sudo bash

# Configure cloud mode (downloads plugins, writes systemd service, starts daemon)
sudo docker dso system setup
```

Cloud mode is auto-detected when `/etc/dso/dso.yaml` exists. See [Cloud Mode Configuration](docs/configuration.md).

---

## 🔧 Installation

### Requirements
- Docker (any recent version)
- Linux (amd64 or arm64) or macOS (amd64 or arm64)
- **No Go installation required** — DSO ships as a prebuilt binary

### Install

**User install (local mode):**
```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | bash
```

**Global install (needed for cloud mode / systemd):**
```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sudo bash
```

The installer:
- Downloads the prebuilt binary for your OS and architecture
- Verifies the SHA256 checksum
- Places it in `~/.docker/cli-plugins/` (user) or `/usr/local/lib/docker/cli-plugins/` (root)
- Creates a `dso` symlink for standalone use

---

## ⚙️ Modes of Operation

### Local Mode (default)

- Secret store: `~/.dso/vault.enc` (AES-256-GCM)
- No systemd. No root. No cloud account.
- Supports both `dso://` (env) and `dsofile://` (tmpfs file injection)

Mode is selected automatically. Override explicitly:
```bash
docker dso up --mode=local
DSO_FORCE_MODE=local docker dso up
```

### Cloud Mode (legacy/enterprise)

- Reads config from `/etc/dso/dso.yaml`
- Runs a background `dso-agent` via systemd
- Supports provider plugins (Vault fully supported; AWS/Azure/Huawei coming soon)
- `dsofile://` is **not supported** in Cloud Mode

Auto-detected when `/etc/dso/dso.yaml` or the systemd service file exists. Override:
```bash
docker dso up --mode=cloud
DSO_FORCE_MODE=cloud docker dso up
```

---

## 🔒 Security

| Feature | Detail |
| :--- | :--- |
| Vault encryption | AES-256-GCM with a user-owned master key |
| File injection | `dsofile://` streams to `tmpfs` — never touches disk |
| `docker inspect` | Secrets injected via `dsofile://` are invisible |
| Root guard | `dso init` refuses to run as root (vault must be user-owned) |

See [Security Architecture](docs/security.md) for full details.

---

## 📋 Key Commands

| Command | Description |
| :--- | :--- |
| `docker dso init` | Initialize local vault (non-root only) |
| `docker dso secret set <project>/<path>` | Store a secret |
| `docker dso secret get <project>/<path>` | Retrieve a secret |
| `docker dso secret list [project]` | List secret keys |
| `docker dso env import <file> [project]` | Import a `.env` file into the vault |
| `docker dso up [args...]` | Deploy with secret injection |
| `sudo docker dso system setup` | Configure cloud mode (root only) |
| `docker dso system doctor` | Diagnose the local DSO environment |

---

## 🧩 Plugin System

DSO supports cloud provider plugins for Cloud Mode:

| Plugin | Status |
| :--- | :--- |
| `dso-provider-vault` | ✅ Fully supported |
| `dso-provider-aws` | 🚧 Not yet implemented |
| `dso-provider-azure` | 🚧 Not yet implemented |
| `dso-provider-huawei` | 🚧 Not yet implemented |

Plugins are downloaded automatically by `sudo docker dso system setup`. Stub plugins for AWS, Azure, and Huawei are distributed with each release and return a clear `"not yet implemented"` error at runtime — no silent failures.

---

## 📚 Documentation

| Document | Description |
| :--- | :--- |
| [Getting Started](docs/getting-started.md) | Step-by-step first-run guide |
| [Concepts](docs/concepts.md) | `dso://` vs `dsofile://`, vault lifecycle |
| [CLI Reference](docs/cli.md) | All commands with examples |
| [Docker Compose Guide](docs/docker-compose.md) | Env injection, file injection, mixed usage |
| [Security](docs/security.md) | Why `.env` is unsafe, threat model |
| [Examples](docs/examples/) | PostgreSQL, Redis, Node.js, Django, fullstack |

---

## 🔍 Diagnose Your Setup

```bash
docker dso system doctor
```

```
DSO System Diagnostics — v3.2.0
════════════════════════════════════════════════════════════
Component         Status     Detail
────────────────────────────────────────────────────────────
Binary            OK         /usr/local/bin/dso (v3.2.0)
Detected Mode     LOCAL      Reason: default
Config            NOT FOUND  /etc/dso/dso.yaml
Vault             OK         /home/user/.dso/vault.enc
Systemd Service   NOT FOUND  (expected for cloud mode only)
Plugin: vault     MISSING    (expected for cloud mode only)
════════════════════════════════════════════════════════════
```

---

<p align="center">Built with ❤️ for the open-source DevOps community.</p>
