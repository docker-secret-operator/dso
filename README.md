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

Local development typically uses `.env` files to pass secrets into containers. This creates two concrete, exploitable security gaps:

1. **Git leaks.** `.env` files get committed. Once a secret is in git history it cannot be safely removed — it must be rotated. This happens more often than teams admit.

2. **`docker inspect` exposure.** Every environment variable passed to a container is permanently stored in Docker's metadata layer. Any process on the host with Docker access can run:
   ```bash
   docker inspect <container_id> | grep -A5 'Env'
   ```
   and read your database passwords, API keys, and tokens in plain text. No breach required.

Both problems exist because secrets are stored as plaintext on disk before they reach the container.

## ✨ The Solution

DSO replaces `.env` files with an **encrypted local vault** and injects secrets at runtime — directly into container memory via `tmpfs`. Secrets never touch disk and are invisible to `docker inspect`.

It works in two modes depending on your environment:

| Mode | Use Case | Requires |
| :--- | :--- | :--- |
| **Local** (default) | Development, CI, offline | Nothing — no cloud, no root |
| **Cloud** | Production with AWS/Azure/Vault | Root, systemd |

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

**`dsofile://`** — secret is streamed into a `tmpfs` RAM disk inside the container. Invisible to `docker inspect`. Nothing on the host filesystem.

**`dso://`** — secret is injected as an environment variable. Simpler, but visible to `docker inspect`. Use `dsofile://` where possible.

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
*DSO parses the compose file, resolves secrets from the vault in memory, starts an inline agent, and hands a sanitized compose file to Docker. No secrets on disk at any point.*

---

## ☁️ Cloud Mode — Production Setup

For teams already using a centralised secrets backend (HashiCorp Vault, or AWS/Azure/Huawei when available). Runs a background `dso-agent` process managed by systemd, keeping secrets out of the deployment pipeline entirely.

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

If you are on Swarm and already using Docker Secrets, keep using them. DSO is for teams on `docker compose` who need the same guarantees without adopting Swarm.

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
Detected Mode     LOCAL      Reason: default
Config            NOT FOUND  /etc/dso/dso.yaml
Vault             OK         /home/user/.dso/vault.enc
Systemd Service   NOT FOUND  (expected for cloud mode only)
Plugin: vault     MISSING    (expected for cloud mode only)
════════════════════════════════════════════════════════════
```

`MISSING` plugin status is normal in Local Mode. `INVALID` means the binary exists but is not executable — re-run `sudo docker dso system setup`.

---

<p align="center">Built with ❤️ for the open-source DevOps community.</p>
