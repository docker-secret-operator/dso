# Getting Started with DSO (Phase 1-6)

This guide covers fresh installation and your first deployment using the Phase 1-6 operational workflow.

---

## Prerequisites

- Docker (any recent version, 20.10+)
- Linux or macOS (amd64 or arm64)
- **No Go required** — DSO ships as a prebuilt binary

---

## Step 1 — Install DSO Plugin

DSO is a Docker CLI plugin. Install it to make `docker dso` available.

### Local User Install (Development)

```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sh
```

Installs to: `~/.docker/cli-plugins/docker-dso`

### System-Wide Install (Production)

```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sudo sh
```

Installs to: `/usr/local/lib/docker/cli-plugins/docker-dso`

### Verify Installation

```bash
docker dso version
```

If not found:
```bash
docker ps  # Reload plugins
docker dso version
```

See [Installation Guide](installation.md) for troubleshooting.

---

## Step 2 — Bootstrap DSO Environment (Phase 1)

Choose your deployment mode and initialize DSO.

### Local Development Mode

**Initialize local environment (no root required):**

```bash
docker dso bootstrap local
```

Output:
```
✓ DSO local environment initialized
✓ Configuration: ~/.dso/config.yaml
✓ Vault: ~/.dso/vault.enc (AES-256 encrypted)
✓ Next steps:
  - Review config: docker dso config show
  - Check health: docker dso doctor
  - View status: docker dso status
```

**What was created:**
- `~/.dso/` — Local state directory
- `~/.dso/config.yaml` — Development configuration
- `~/.dso/vault.enc` — Encrypted local secret storage
- `~/.dso/state/` — Rotation and container tracking
- `~/.dso/cache/` — Secret cache

### Production Agent Mode

**Initialize production environment with systemd (requires root):**

```bash
sudo docker dso bootstrap agent
```

Output:
```
✓ DSO agent initialized
✓ Configuration: /etc/dso/dso.yaml
✓ Service: /etc/systemd/system/dso-agent.service
✓ Vault: configured per provider
✓ Next steps:
  - Review config: docker dso config show
  - Enable service: sudo docker dso system enable
  - Monitor logs: docker dso system logs -f
```

**What was created:**
- `/etc/dso/` — Production configuration directory
- `/etc/dso/dso.yaml` — Production configuration
- `/var/lib/dso/` — State and cache directory
- `/var/log/dso/` — Log directory
- `/run/dso/` — Runtime socket directory
- `/etc/systemd/system/dso-agent.service` — Systemd service file

---

## Step 3 — Check Environment Health (Phase 2)

Validate DSO setup and environment.

### Basic Health Check

```bash
docker dso doctor
```

Checks:
- Docker connectivity ✓
- Runtime environment ✓
- Provider availability ✓
- System permissions ✓

**Example output:**
```
✓ Docker connectivity: OK
✓ Runtime mode: local
✓ Configuration: valid
✓ System permissions: OK
```

### Full Diagnostics

```bash
docker dso doctor --level full
```

Additional checks:
- Provider detailed status
- Container health
- Cache effectiveness
- System resource usage

### Troubleshooting

If any checks fail, fix the issue before proceeding:

```bash
# Docker not running?
docker ps

# Configuration invalid?
docker dso config validate

# Permission issues?
sudo usermod -aG docker $USER
newgrp docker
```

---

## Step 4 — Review Configuration (Phase 3)

View and configure DSO settings.

### View Current Configuration

```bash
docker dso config show
```

Default local config:
```yaml
version: v1alpha1
runtime:
  mode: local
  log_level: info

providers:
  local:
    type: file
    enabled: true

agent:
  cache:
    ttl: 1h
    max_size: 500Mi
  rotation:
    strategy: restart
    timeout: 30s
```

### Edit Configuration

```bash
docker dso config edit
```

Opens in `$EDITOR`. After saving:
- Validates syntax automatically
- Shows any errors
- Suggests next steps

**Common changes:**

Local mode:
```yaml
# Use external vault instead
providers:
  vault:
    type: vault
    address: https://vault.example.com:8200
    token: ${VAULT_TOKEN}
```

Agent mode:
```yaml
# Increase cache for production
agent:
  cache:
    ttl: 4h
    max_size: 2Gi
```

---

## Step 5 — Monitor Status (Phase 3)

View real-time system metrics.

### Current Status

```bash
docker dso status
```

Shows:
- Provider health
- Container information
- Cache metrics
- Rotation statistics
- System metrics

### Live Monitoring

```bash
docker dso status --watch
```

Auto-refreshes every 2 seconds. Press Ctrl+C to exit.

---

## Step 6 — Enable Systemd Service (Phase 4 — Agent Mode Only)

Start the long-running agent service.

### Enable and Start Service

```bash
sudo docker dso system enable
```

This:
- Enables the dso-agent systemd service
- Starts it immediately
- Configures auto-restart on failure

### Check Service Status

```bash
docker dso system status
```

Output:
```
Service Status: active (running)
Enabled: yes
Uptime: 2h 15m
Recent logs:
  [INFO] Agent started
  [INFO] Configuration loaded
  [INFO] Event watcher started
```

### View Live Logs

```bash
docker dso system logs -f
```

Follow logs in real-time. Press Ctrl+C to exit.

### Common Operations

```bash
# Check status
docker dso system status

# Restart after config changes
sudo docker dso system restart

# View recent errors
docker dso system logs -p err

# View last hour of logs
docker dso system logs --since 1h
```

---

## Complete Workflow Example

Here's a full setup for local development:

```bash
# 1. Install
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sh

# 2. Bootstrap local environment
docker dso bootstrap local

# 3. Check health
docker dso doctor

# 4. Review configuration
docker dso config show

# 5. View status
docker dso status

# 6. Edit config if needed
docker dso config edit

# 7. Validate changes
docker dso config validate

# 8. You're ready to use DSO!
docker compose up
```

For production agent setup, replace step 2 with `sudo docker dso bootstrap agent`, then step 6 with `sudo docker dso system enable`.

---

## Next Steps

- **For Docker plugin details**, see [Docker Plugin Integration](docker-plugin.md)
- **For command reference**, see [CLI Reference](cli.md)
- **For architecture details**, see [System Architecture](architecture.md)
- **For day-2 operations**, see [Operational Guide](operational-guide.md)
- **For runtime details**, see [Runtime Operation](runtime.md)

→ Continue to [Cloud Mode Setup](#cloud-mode-setup) below.

---

## Local Mode Setup

### 1. Initialize the vault
```bash
docker dso init
```

This creates `~/.dso/vault.enc` and generates your master key.

> ⚠️ Do not run `docker dso init` with `sudo`. The vault must be owned by your user account.

### 2. Store a secret
```bash
docker dso secret set myapp/db_password
# Enter secret value: (invisible prompt)
```

The path format is `<project>/<key>`. You can also pipe a value:
```bash
cat ./private.key | docker dso secret set myapp/tls_key
```

### 3. Reference it in docker-compose.yaml

**File injection** (recommended — invisible to `docker inspect`):
```yaml
services:
  db:
    image: postgres:15
    environment:
      POSTGRES_PASSWORD_FILE: dsofile://myapp/db_password
```

**Environment injection** (for legacy containers):
```yaml
services:
  api:
    image: my-api:latest
    environment:
      STRIPE_KEY: dso://myapp/stripe_key
```

### 4. Deploy
```bash
docker dso up -d
```

DSO reads your compose file, resolves all `dso://` and `dsofile://` references in memory, starts an inline agent, and passes a sanitized compose file to Docker.

---

## Cloud Mode Setup

Cloud mode requires root and runs the `dso-agent` as a systemd service.

### 1. Run system setup (once, as root)
```bash
sudo docker dso system setup
```

This command:
- Writes `/etc/systemd/system/dso-agent.service` (systemd unit)
- Downloads and verifies provider plugins from the GitHub release
- Creates `/etc/dso/dso.yaml` (source of truth)

### 2. Enable and start the agent

```bash
sudo docker dso system enable
```

### 3. Configure providers in `/etc/dso/dso.yaml`

Edit the configuration file:
```bash
sudo nano /etc/dso/dso.yaml
```

Example configuration:
```yaml
providers:
  vault-prod:
    type: vault
    address: https://vault.example.com
    auth:
      method: token
      token: ${VAULT_TOKEN}

secrets:
  - name: prod/db_password
    provider: vault-prod
```

The `/etc/dso/dso.yaml` is the source of truth. It is auto-loaded and determines all runtime behavior.

### 4. Deploy with docker-compose.yaml

```bash
docker compose up -d
```

---

## Diagnose Your Setup

At any time, run:
```bash
docker dso system doctor
```

This shows your detected mode, vault status, config file presence, systemd service state, and plugin availability. It is read-only and safe to run at any time.

---

## Next Steps

- [CLI Reference](cli.md) — all commands explained
- [Docker Compose Guide](docker-compose.md) — `dso://` vs `dsofile://` in depth
- [Examples](examples/) — PostgreSQL, Redis, Django, Node.js
- [Security](security.md) — threat model and design decisions
