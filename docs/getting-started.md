# Getting Started with DSO (Phase 1-6)

This guide covers fresh installation and your first deployment using the Phase 1-6 operational workflow.

---

## Quick Start (Recommended)

**Get DSO up and running in 2-3 minutes:**

### Step 1: Install DSO
```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | bash
docker ps  # Reload plugins
```

### Step 2: Run the Setup Wizard
```bash
docker dso setup
```

This interactive wizard will:
- 🔍 Auto-detect your cloud provider (AWS, Azure, Vault, or local)
- 📋 Suggest the appropriate deployment mode
- 📦 Install required provider plugins
- 📝 Generate a pre-configured `dso.yaml` file
- ✓ Validate your setup

That's it! The wizard will show you the next steps.

**For advanced users or manual configuration**, see [Detailed Setup](#detailed-setup) below.

---

## Prerequisites

- Docker (any recent version, 20.10+)
- Linux or macOS (amd64 or arm64)
- **No Go required** — DSO ships as a prebuilt binary

---

## CHOOSE YOUR MODE (Before Starting)

**DSO runs in two modes. Choose one:**

### Local Mode (Development)
Use if you are:
- Developing locally
- Testing DSO
- Using docker-compose without a daemon
- A single developer on one machine

**Characteristics**:
- No root required
- No systemd service
- Simple setup (~5 minutes)
- Secrets in encrypted local vault
- Manual deployment via `docker dso compose up`

**Install**: `curl ... | bash` (user install)

### Cloud Mode (Production)
Use if you are:
- Running production workloads
- Need automatic secret rotation
- Using cloud provider secrets (Vault, AWS, Azure)
- Running on a server with systemd

**Characteristics**:
- Root required (systemd)
- Long-running daemon service
- Automatic rotation from providers
- Complete setup (~15 minutes)
- Observability and monitoring included
- Automatic crash recovery (v3.5+)

**Install**: `curl ... | sudo bash` (global install)

---

**Not sure?** Start with Local Mode. You can always switch to Cloud Mode later.

---

## Detailed Setup

For users who want more control or need to troubleshoot, follow these steps manually.

### Step 1 — Install DSO Plugin

DSO is a Docker CLI plugin. Install it to make `docker dso` available.

### Local User Install (Development)

```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | bash
```

Installs to: `~/.docker/cli-plugins/docker-dso`

### System-Wide Install (Production)

```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sudo bash
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

See [CLI Reference](cli.md) or [Docker Plugin Integration](docker-plugin.md) for troubleshooting.

---

### Step 2 — Bootstrap DSO Environment (Phase 1)

**Alternative: Use the interactive setup wizard** (`docker dso setup`) for a guided experience.

Choose your deployment mode and initialize DSO:

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

### Step 3 — Check Environment Health (Phase 2)

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
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | bash

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
- **For security details**, see [Security Model](../SECURITY.md)
- **For recovery procedures**, see [Recovery Guide](RECOVERY_PROCEDURES.md)

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

### 1. Bootstrap Agent (once, as root)

```bash
sudo docker dso bootstrap agent
```

This command:
- Creates `/etc/dso/dso.yaml` with provider pre-configured
- Writes `/etc/systemd/system/dso-agent.service` (systemd unit)
- Sets up system directories with proper permissions
- (Optional) Adds your user to `dso` and `docker` groups with `--enable-nonroot` flag

Output includes:
```
✓ DSO Agent Runtime Initialized
✓ Configuration: /etc/dso/dso.yaml
✓ Service: /etc/systemd/system/dso-agent.service

🔧 NEXT STEPS:
1. Edit configuration: sudo nano /etc/dso/dso.yaml
2. Add your secrets under the 'secrets:' section
3. Validate: sudo docker dso config validate
4. Enable service: sudo docker dso system enable
5. Check status: docker dso status --watch
```

### 2. Configure Secrets in `/etc/dso/dso.yaml`

The bootstrap process created your config file. Now you need to add your secrets:

```bash
sudo nano /etc/dso/dso.yaml
```

The file already contains:
- ✅ **Provider configured** (AWS/Azure/Vault/Huawei)
- ✅ **Agent settings** (cache, watch, rotation strategy)
- ❌ **Secrets** (you need to add these manually)

**Example for Vault:**
```yaml
secrets:
  - name: prod/db_password
    provider: vault
    mappings:
      value: POSTGRES_PASSWORD
  
  - name: prod/api-credentials
    provider: vault
    mappings:
      username: API_USER
      password: API_PASSWORD
```

**Example for AWS:**
```yaml
secrets:
  - name: prod/mysql-password
    provider: aws
    mappings:
      value: MYSQL_ROOT_PASSWORD
  
  - name: prod/app-secrets          # JSON secret with multiple values
    provider: aws
    mappings:
      api_key: API_KEY
      db_pass: DB_PASSWORD
```

### 3. Validate and Enable Service

```bash
# Validate configuration syntax
sudo docker dso config validate

# Enable and start the systemd service
sudo docker dso system enable

# Watch logs in real-time
docker dso status --watch
```

The `/etc/dso/dso.yaml` is the source of truth. It is auto-loaded and determines all runtime behavior.

### 4. Deploy with docker-compose.yaml

```bash
docker compose up -d
```

Containers automatically fetch secrets from the configured provider.

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
- [Security Model](../SECURITY.md) — threat model and design decisions
- [Recovery Procedures](RECOVERY_PROCEDURES.md) — handling failures
