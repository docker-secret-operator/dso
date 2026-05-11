<h1 align="center">Docker Secret Operator (DSO)</h1>
<p align="center">
  <b>Secure secret injection for Docker Compose — no .env files, no docker inspect leaks, no Swarm required, no cloud account required.</b>
</p>
<p align="center">
  <a href="https://github.com/docker-secret-operator/dso/releases/latest"><img src="https://img.shields.io/github/v/release/docker-secret-operator/dso?label=latest" alt="Latest Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/docker-secret-operator/dso" alt="License"></a>
</p>

---

## 🚫 The Problem

Local development typically uses `.env` files to pass secrets into containers. This creates two concrete, exploitable security gaps:

1. **Git leaks.** `.env` files get committed. Once a secret is in git history it cannot be safely removed — it must be rotated. This happens more often than teams admit.

2. **`docker inspect` exposure.** Every environment variable passed to a container is permanently stored in Docker's metadata layer. Any process on the host with Docker access can run:
   ```bash
   docker inspect <container_id> | grep -A5 'Env'
   ```
   and read your database passwords, API keys, and tokens in plain text. No breach required.

Both problems exist because secrets are stored as plaintext on disk before they reach the container.

## ✨ The Solution

DSO replaces `.env` files with an **encrypted local vault**. Secrets are injected at runtime, directly into container memory via `tmpfs`. They never touch disk. They are invisible to `docker inspect`.

## 👥 Who Is This For?

- **Local developers** tired of `.env` files leaking into git
- **CI/CD pipelines** that need secrets without environment variable exposure
- **Teams on `docker compose`** who want Docker Secrets-level guarantees without adopting Swarm
- **Platform teams** integrating a centralised vault (HashiCorp, AWS, Azure) into compose-based deployments

It works in two modes depending on your environment:

| Mode | Use Case | Requires |
| :--- | :--- | :--- |
| **Local** (default) | Development, CI, offline | Nothing — no cloud, no root |
| **Cloud** | Production with AWS/Azure/Vault | Root, systemd |

---

## 🎯 What DSO Is & Isn't

### ✅ DSO Is

- **Docker-native**: Works with plain `docker compose` (no Kubernetes, no Swarm required)
- **Runtime secret injection**: Secrets resolved at container startup, never persisted
- **Zero-persistence**: Secrets never touch the host filesystem
- **Multi-provider**: Supports Vault, AWS, Azure, Huawei, and local file backends
- **Developer-friendly**: Local Mode requires no setup — just `docker dso init`
- **Production-ready**: Cloud Mode includes supervision, health checks, rotation, observability

### ❌ DSO Is NOT

- **A centralized secrets platform** (like Doppler or HashiCorp Vault) — DSO is a client/daemon for Docker, not a backend
- **Kubernetes-first** — If you're on Kubernetes, use ExternalSecrets Operator instead
- **A secret manager** — DSO doesn't store secrets; it only injects them. You manage where they're stored.
- **Docker Swarm-oriented** — DSO works with `docker compose` on any machine; no Swarm required
- **A replacement for Docker Secrets** — If you already use Swarm and Docker Secrets, keep using them. DSO is for teams on `compose`.
- **Zero-downtime rotation** — Rotation requires restarting containers. Plan accordingly.

---

## 🧠 How DSO Works

When you run `docker dso up`, DSO intercepts the compose file before Docker sees it:

```
docker dso up
    │
    ├─ 1. Read docker-compose.yaml (AST parse)
    ├─ 2. Detect mode (local / cloud)
    │
    ├─ [Local]  Resolve dso:// and dsofile:// from ~/.dso/vault.enc
    │           Start inline in-process agent
    │           Mutate compose AST in memory
    │
    └─ [Cloud]  Route to running dso-agent (systemd)
                Agent fetches secrets from provider plugin (Vault, AWS, …)
                Mutate compose AST in memory
    │
    └─ 3. Pass resolved compose file to `docker compose up`
           No secrets written to disk at any point
```

**`dsofile://`** — streams the secret into a `tmpfs` RAM disk inside the container. Invisible to `docker inspect`. Nothing written to the host filesystem.

> ⚠️ **Security note:** `dso://` injects the secret as a standard environment variable — it **is** visible to `docker inspect`. Use `dsofile://` wherever the target application supports reading secrets from a file path.

---

## 🚀 Quick Start — Local Mode

> No cloud account. No Go installation. No root.

**1. Install DSO**
```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | bash
```
*Downloads the prebuilt binary for your OS/arch and places it in your Docker plugin directory.*

**2. Initialize your local vault**
```bash
docker dso init
```
*Creates `~/.dso/vault.enc` — your AES-256-GCM encrypted secret store.*

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
*`dsofile://` streams the secret into a RAM disk inside the container — never touches host disk, invisible to `docker inspect`.*

**5. Deploy**
```bash
docker dso up -d
```
*DSO parses the compose file, resolves secrets from the vault in memory, and passes a sanitized compose file to Docker.*

---

## ☁️ Cloud Mode — Production Setup

**Use Cloud Mode when:**
- You already run HashiCorp Vault, AWS Secrets Manager, or Azure Key Vault
- You need a single secrets backend shared across multiple hosts or teams
- A long-running daemon is acceptable (and preferred) for your deployment model
- You are on Linux with systemd

Runs a background `dso-agent` process managed by systemd. Secrets are fetched by provider plugins and never stored locally.

```bash
# Install globally
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sudo bash

# Download plugins, write systemd service, start daemon
sudo docker dso system setup
```

DSO auto-detects Cloud Mode when `/etc/dso/dso.yaml` exists — no flag needed. See [Cloud Mode Configuration](docs/configuration.md).

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

### Cloud Mode

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

## ❓ Why Not Docker Secrets?

[Docker Secrets](https://docs.docker.com/engine/swarm/secrets/) is a native Docker feature — but it requires **Docker Swarm**. If you are running plain `docker compose` (the majority of development and CI environments), Docker Secrets is not available.

| | Docker Secrets | DSO |
| :--- | :--- | :--- |
| Requires Swarm | ✅ Yes | ❌ No |
| Works with `docker compose` | ❌ No | ✅ Yes |
| Local dev workflow | ❌ No | ✅ Yes |
| Encrypted at rest | ✅ Yes (Swarm Raft) | ✅ Yes (AES-256-GCM) |
| Cloud provider integration | ❌ No | ✅ Yes (via plugins) |
| Invisible to `docker inspect` | ✅ Yes | ✅ Yes (dsofile://) |

If you are on Swarm and already using Docker Secrets, keep using them. DSO targets teams on `docker compose` who need the same security guarantees without adopting Swarm.

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
| `dso-provider-aws` | ✅ Fully supported |
| `dso-provider-azure` | ✅ Fully supported |
| `dso-provider-huawei` | ✅ Fully supported |

Plugins are downloaded automatically by `sudo docker dso system setup`. Each plugin is a standalone binary that implements the DSO `SecretProvider` interface via HashiCorp's `go-plugin` RPC framework.

---

## 📚 Documentation

### Getting Started
| Document | Description |
| :--- | :--- |
| [Quick Start](docs/quick_setup.md) | 5-minute Local Mode setup |
| [Getting Started](docs/getting-started.md) | Step-by-step first-run guide |
| [Installation](docs/installation.md) | Binary install, selective plugin setup, CI/CD |
| [Examples](docs/examples/) | PostgreSQL, Redis, Node.js, Django, fullstack |

### Core Concepts
| Document | Description |
| :--- | :--- |
| [Concepts](docs/concepts.md) | Dual-mode architecture, `dso://` vs `dsofile://`, execution models |
| [Architecture](ARCHITECTURE.md) | System design, data flows, runtime supervision |
| [Security](docs/security.md) | Threat model, Local/Cloud mode security, limitations |

### Configuration & Operation
| Document | Description |
| :--- | :--- |
| [CLI Reference](docs/cli.md) | All commands with examples |
| [Configuration Reference](docs/configuration.md) | Full `dso.yaml` schema (Cloud Mode) |
| [Docker Compose Guide](docs/docker-compose.md) | Env injection (`dso://`), file injection (`dsofile://`), examples |
| [Providers](docs/providers.md) | Cloud provider setup (Vault, AWS, Azure, Huawei) |

### Advanced & Upgrade
| Document | Description |
| :--- | :--- |
| [Migration Guide](docs/migration.md) | Upgrading from earlier versions |

---

## 🔍 Diagnose Your Setup

If `docker dso up` behaves unexpectedly — wrong mode selected, agent not starting, plugins missing — run the doctor command first before filing a bug report:

```bash
docker dso system doctor
```

```
DSO System Diagnostics — v3.2.0
════════════════════════════════════════════════════════════
Component         Status     Detail
────────────────────────────────────────────────────────────
Binary            OK         /usr/local/bin/dso (v3.2.0)
Effective UID     1000
Detected Mode     LOCAL      Reason: auto-detected (~/.dso/vault.enc)
Config            NOT FOUND  /etc/dso/dso.yaml
Vault             OK         /home/user/.dso/vault.enc
Systemd Service   NOT FOUND  File: /etc/systemd/system/dso-agent.service | Runtime: inactive/unknown
Plugin: vault     MISSING    /usr/local/lib/dso/plugins/dso-provider-vault
════════════════════════════════════════════════════════════
```

`MISSING` plugin status is normal in Local Mode. `INVALID` means the binary exists but is not executable — re-run `sudo docker dso system setup`.

---

<p align="center">Built with ❤️ for the open-source DevOps community.</p>
