# DSO Local Mode Complete Guide

Comprehensive guide to using Docker Secret Operator in local development mode with real-world examples.

**Target Audience**: Developers setting up local development environments  
**Time to Complete**: 15-20 minutes  
**Prerequisite**: Docker 20.10+ with Docker Compose

---

## Table of Contents

1. [What is Local Mode?](#what-is-local-mode)
2. [Installation](#installation)
3. [Quick Start (5 minutes)](#quick-start-5-minutes)
4. [Real-World Example: MySQL + phpMyAdmin](#real-world-example-mysql--phpmyadmin)
5. [Secret Management](#secret-management)
6. [Docker Compose Configuration](#docker-compose-configuration)
7. [Deployment & Verification](#deployment--verification)
8. [Monitoring & Debugging](#monitoring--debugging)
9. [Troubleshooting](#troubleshooting)
10. [Best Practices](#best-practices)
11. [Security Model](#security-model)

---

## What is Local Mode?

Local Mode is DSO's development-friendly configuration that stores secrets in an **encrypted local vault** on your machine (`~/.dso/vault.enc`).

### Key Features

| Feature | Details |
|---------|---------|
| **Storage** | AES-256 encrypted local vault (`~/.dso/vault.enc`) |
| **Root Required** | ❌ No — runs as your user |
| **Secret Rotation** | Manual or automated via `docker dso secret set` |
| **Cloud Providers** | ❌ Not needed — local storage only |
| **Perfect For** | Development, testing, single-machine setups |

### Local vs. Agent Mode

| Aspect | Local Mode | Agent Mode |
|--------|-----------|-----------|
| **Storage** | `~/.dso/vault.enc` | AWS/Azure/Vault/Huawei |
| **Root Required** | No | Yes (systemd service) |
| **When to Use** | Development | Production |
| **Rotation** | Manual | Automatic (event-driven) |
| **Setup Time** | <1 second | ~5 minutes |

---

## Installation

### Step 1: Install DSO

```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | bash
```

Verify installation:
```bash
docker dso version
```

### Step 2: Initialize Local Mode

```bash
docker dso setup --mode local
```

**Expected output:**
```
╔════════════════════════════════════════════════════╗
║   Docker Secret Operator (DSO) Setup Wizard        ║
║              (Local Development Mode)              ║
╚════════════════════════════════════════════════════╝

📝 Creating configuration...
✓ Configuration created: ./dso.yaml

Setup Complete!

📚 What's next:

  1. Initialize the local vault:
     docker dso init

  2. Store secrets:
     docker dso secret set <name>

  3. Deploy your services:
     docker dso up -d

  4. Check status:
     docker dso status
```

### Step 3: Initialize the Vault

```bash
docker dso init
```

⚠️ **Important**: Run this as your **regular user**, NOT with `sudo`. The vault must be owned by your user.

**What was created:**
- `~/.dso/vault.enc` — Your encrypted secret storage
- `~/.dso/master.key` — Vault master key (back this up!)

---

## Quick Start (5 minutes)

### 1. Set Up Secrets

```bash
# Interactive prompts (hidden input)
docker dso secret set myapp/db_password
# Enter secret for 'myapp/db_password': (invisible)

docker dso secret set myapp/db_user "myappuser"
docker dso secret set myapp/api_key "sk-1234567890"
```

### 2. Create `docker-compose.yml`

```yaml
version: '3.8'

services:
  postgres:
    image: postgres:15
    labels:
      dso.reloader: "true"
    environment:
      POSTGRES_USER: dso://myapp/db_user
      POSTGRES_PASSWORD: dso://myapp/db_password
      POSTGRES_DB: myapp
    ports:
      - "5432:5432"
```

### 3. Deploy

```bash
docker dso up -d
```

### 4. Verify

```bash
docker dso doctor
docker dso status
```

**That's it!** Secrets are now injected into your containers.

---

## Real-World Example: MySQL + phpMyAdmin

This example demonstrates a complete local development setup with MySQL database and phpMyAdmin web interface.

### Step 1: Set Up Secrets

```bash
# Set MySQL root password
docker dso secret set prod-ms/mysql-root-password
# Enter secret for 'prod-ms/mysql-root-password': [your-root-password]

# Set MySQL application user
docker dso secret set prod-ms/mysql-user
# Enter secret for 'prod-ms/mysql-user': [username, e.g., "appuser"]

# Set MySQL application user password
docker dso secret set prod-ms/mysql-user-password
# Enter secret for 'prod-ms/mysql-user-password': [user-password]
```

Verify secrets were saved:
```bash
docker dso secret list prod-ms
```

**Output:**
```
Secrets in project 'prod-ms':
  - prod-ms/mysql-root-password
  - prod-ms/mysql-user
  - prod-ms/mysql-user-password
```

### Step 2: Create `docker-compose.yml`

```yaml
version: '3.8'

services:
  # MySQL Database
  mysql_db:
    container_name: prod-hms-mysql-container
    image: mysql:latest
    labels:
      dso.reloader: "true"
      dso.secrets: "prod-ms"
      dso.update.strategy: "rolling"
    environment:
      # ✅ CORRECT: All secrets use dso:// prefix
      MYSQL_ROOT_PASSWORD: dso://prod-ms/mysql-root-password
      MYSQL_USER: dso://prod-ms/mysql-user
      MYSQL_PASSWORD: dso://prod-ms/mysql-user-password
      # Application database
      MYSQL_DATABASE: "myapp_db"
    ports:
      - "3506:3306"
    restart: always
    volumes:
      - mysql_data:/var/lib/mysql
      # Optional: custom MySQL config
      # - ./my.cnf:/etc/my.cnf:ro
    healthcheck:
      test: ["CMD", "mysqladmin", "ping", "-h", "localhost"]
      interval: 10s
      timeout: 5s
      retries: 5

  # phpMyAdmin - Database Management UI
  phpmyadmin:
    container_name: prod-hms-phpmyadmin-container
    image: phpmyadmin/phpmyadmin:latest
    labels:
      dso.reloader: "true"
      dso.secrets: "prod-ms"
    ports:
      - "28080:80"
    restart: always
    environment:
      # Connect to MySQL service
      PMA_HOST: mysql_db
      PMA_PORT: 3306
      # Use secrets for credentials
      PMA_USER: dso://prod-ms/mysql-user
      PMA_PASSWORD: dso://prod-ms/mysql-user-password
    depends_on:
      mysql_db:
        condition: service_healthy
    volumes:
      - phpmyadmin_data:/sessions

volumes:
  mysql_data:
  phpmyadmin_data:

networks:
  default:
    name: prod-hms-network
```

**Key Points:**
- ✅ All secrets use `dso://` prefix
- ✅ Services are labeled with `dso.reloader: "true"`
- ✅ Health checks ensure services are ready
- ✅ Volumes persist data across restarts
- ✅ Secrets are never hardcoded in the file

### Step 3: Deploy

```bash
docker dso up -d
```

**Expected output:**
```
[DSO] Running in LOCAL mode (auto-detected (~/.dso/vault.enc))
[DSO] Resolving secrets...
🚀 Starting DSO agent...
⚠️  WARNING: Service 'mysql_db' is injecting a secret into environment variable 
   'MYSQL_ROOT_PASSWORD' via dso:// (Environment injection). 
   This is visible in docker inspect.

2026/05/20 13:32:20 ✅ [DSO Agent] Docker event stream connected
🐳 Running docker compose...
[+] up 2/2
 ✔ Network prod-hms-network           Created
 ✔ Container prod-hms-mysql-container Created
 ✔ Container prod-hms-phpmyadmin-container Created
```

### Step 4: Verify Deployment

```bash
# Check health
docker dso doctor
docker dso status

# Access services
# MySQL:     localhost:3506
# phpMyAdmin: localhost:28080 (username: dso://prod-ms/mysql-user, password: dso://prod-ms/mysql-user-password)
```

---

## Secret Management

### Set a Secret

#### Interactive (Recommended)

```bash
docker dso secret set prod-ms/mysql-root-password
# Enter secret for 'prod-ms/mysql-root-password': (hidden prompt)
```

#### From Pipe

```bash
echo "my-secure-password" | docker dso secret set prod-ms/mysql-root-password
```

#### From File

```bash
cat ./secrets/password.txt | docker dso secret set prod-ms/mysql-root-password
```

### List Secrets

```bash
# All secrets
docker dso secret list

# Secrets in a specific project
docker dso secret list prod-ms

# JSON output
docker dso secret list --json
```

### Retrieve a Secret

```bash
docker dso secret get prod-ms/mysql-root-password
```

⚠️ **Warning**: Only available in local mode. In agent mode, secrets remain encrypted.

### Update a Secret

```bash
# Simply set it again with a new value
docker dso secret set prod-ms/mysql-root-password
# Enter secret for 'prod-ms/mysql-root-password': [new-password]

# DSO automatically detects the change and rotates containers
```

### Delete a Secret

```bash
docker dso secret delete prod-ms/mysql-root-password
```

### Bulk Import from .env

```bash
# Create .env file
cat > .env << EOF
MYSQL_ROOT_PASSWORD=root-secret
MYSQL_USER=appuser
MYSQL_PASSWORD=user-secret
EOF

# Import all variables with prefix 'prod-ms'
docker dso env import .env prod-ms

# Verify
docker dso secret list prod-ms
```

---

## Docker Compose Configuration

### Labels

```yaml
labels:
  dso.reloader: "true"                    # Enable DSO management
  dso.secrets: "prod-ms"                  # Which secret group(s) to watch
  dso.update.strategy: "rolling"          # Rotation strategy
  dso.host_ports: "3306:3306"             # Optional: ports to proxy
```

### Secret Injection Methods

#### Environment Variable (`dso://`)

**When to use:** Most applications, simple configuration

```yaml
environment:
  MYSQL_ROOT_PASSWORD: dso://prod-ms/mysql-root-password
```

**Pros:**
- Simple syntax
- Works everywhere
- Immediate availability

**Cons:**
- Visible in `docker inspect`
- Visible in process listing
- Limited to smaller secrets

**Example:**
```yaml
environment:
  DB_USER: dso://myapp/db_user
  DB_PASSWORD: dso://myapp/db_password
  API_KEY: dso://myapp/api_key
```

#### File Injection (`dsofile://`)

**When to use:** Large secrets, certificates, keys, API tokens

```yaml
environment:
  MYSQL_PASSWORD_FILE: dsofile://prod-ms/mysql-root-password
```

**Pros:**
- Invisible to `docker inspect`
- Not in environment variables
- Better for sensitive data

**Cons:**
- Application must read file
- Slightly more complex

**Example:**
```yaml
environment:
  TLS_CERT_FILE: dsofile://myapp/tls_cert
  TLS_KEY_FILE: dsofile://myapp/tls_key
command: >
  start-server
    --cert=/run/secrets/myapp/tls_cert
    --key=/run/secrets/myapp/tls_key
```

### Rotation Strategies

#### `rolling` (Zero-Downtime - Default)

```yaml
labels:
  dso.update.strategy: "rolling"
```

When a secret changes:
1. Start new container with updated secret
2. Health check new container
3. Atomic swap (old → backup, new → active)
4. Stop old container

**Result:** ✅ Zero downtime, ~30 seconds

**Use for:** Databases, APIs, any production service

#### `restart`

```yaml
labels:
  dso.update.strategy: "restart"
```

When a secret changes:
1. Stop container
2. Start new container with updated secret

**Result:** ⚠️ Brief downtime (5-10 seconds)

**Use for:** Stateless services, workers

#### `signal`

```yaml
labels:
  dso.update.strategy: "signal"
```

When a secret changes:
1. Send SIGHUP to running container
2. Application reloads configuration

**Result:** ✅ No restart, application handles reload

**Use for:** Apps that support SIGHUP reload

#### `none`

```yaml
labels:
  dso.update.strategy: "none"
```

Secrets updated but container not restarted.

**Use for:** Manual rotation workflows

---

## Deployment & Verification

### Initial Setup Checklist

```bash
# ✅ 1. Install DSO
docker dso version

# ✅ 2. Initialize local mode
docker dso setup --mode local
docker dso init

# ✅ 3. Create directory for data
mkdir -p mysql-data

# ✅ 4. Set all required secrets
docker dso secret set prod-ms/mysql-root-password
docker dso secret set prod-ms/mysql-user
docker dso secret set prod-ms/mysql-user-password

# ✅ 5. Create docker-compose.yml
cat > docker-compose.yml << 'EOF'
# Your compose file here
EOF

# ✅ 6. Validate
docker compose config

# ✅ 7. Deploy
docker dso up -d

# ✅ 8. Verify
docker dso doctor
docker dso status
```

### Deployment

```bash
# Deploy all services
docker dso up -d

# Deploy specific compose file
docker dso up -f docker-compose.yml -d

# View logs
docker dso logs -f

# With vanilla docker compose (local mode)
docker compose up -d
```

### Verification

```bash
# Quick health check
docker dso doctor

# Full diagnostics
docker dso doctor --level full

# Real-time status
docker dso status

# Watch status (refreshes every 2s)
docker dso status --watch

# Check running containers
docker ps

# Check container logs
docker logs prod-hms-mysql-container
docker logs prod-hms-phpmyadmin-container
```

### Accessing Services

```bash
# MySQL
mysql -h 127.0.0.1 -P 3506 -u appuser -p

# phpMyAdmin
# Open browser: http://localhost:28080
# Username: appuser (from dso://prod-ms/mysql-user)
# Password: (from dso://prod-ms/mysql-user-password)
```

---

## Monitoring & Debugging

### Real-Time Status

```bash
# Current snapshot
docker dso status

# Live monitoring
docker dso status --watch

# JSON for scripts
docker dso status --json
```

### Health Checks

```bash
# Quick check (5 seconds)
docker dso doctor

# Full diagnostics
docker dso doctor --level full

# Check specific aspect
docker dso doctor --check vault
docker dso doctor --check docker
docker dso doctor --check secrets
```

### Watch Secret Rotations

```bash
# Live rotation events
docker dso watch

# With debug info
docker dso watch --debug
```

### View Container Information

```bash
# Check environment variables in container
docker exec prod-hms-mysql-container env | grep MYSQL

# Check file-based secrets
docker exec prod-hms-mysql-container ls -la /run/secrets/

# View actual secret value (local mode only)
docker dso secret get prod-ms/mysql-root-password
```

### View Logs

```bash
# DSO logs (if available)
docker dso system logs

# Container logs
docker logs prod-hms-mysql-container -f
docker logs prod-hms-phpmyadmin-container -f

# All logs
docker compose logs -f
```

---

## Troubleshooting

### "docker dso: command not found"

```bash
# Check installation
ls ~/.docker/cli-plugins/docker-dso
ls ~/.local/bin/dso

# Reload Docker plugins
docker ps

# Add to PATH (if needed)
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc
source ~/.bashrc
```

### "vault already exists" when running `docker dso init`

```bash
# Vault is already initialized. Proceed to setting secrets:
docker dso secret set prod-ms/mysql-root-password
```

### "Service not healthy" / Health check failing

```bash
# Check container logs
docker logs prod-hms-mysql-container

# Wait longer (MySQL takes time to initialize)
sleep 30
docker dso doctor

# Check if port is already in use
lsof -i :3506
netstat -tlnp | grep 3506

# Use different port
# Edit docker-compose.yml and change ports
```

### Secrets not being injected

```bash
# Check labels are set correctly
docker inspect prod-hms-mysql-container | grep -A 5 Labels

# Check secrets exist
docker dso secret list prod-ms

# Check dso:// prefix is used (not just the name)
grep "MYSQL_" docker-compose.yml

# Verify format:
# ❌ WRONG: MYSQL_PASSWORD: prod-ms/mysql-root-password
# ✅ RIGHT: MYSQL_PASSWORD: dso://prod-ms/mysql-root-password

# Redeploy
docker dso down
docker dso up -d
```

### "No configuration file found"

```bash
# Create configuration
docker dso setup --mode local

# Or specify config location
docker dso --config ~/.dso/dso.yaml up -d
```

### Container keeps restarting

```bash
# Check logs
docker logs prod-hms-mysql-container

# Common causes:
# 1. Invalid secret (empty or wrong format)
docker dso secret get prod-ms/mysql-root-password

# 2. Missing required secret
docker dso secret list prod-ms

# 3. Port already in use
sudo netstat -tlnp | grep 3506

# 4. Volume permission issue
ls -la mysql-data/
chmod 755 mysql-data
```

### Performance Issues / Slow startup

```bash
# Check Docker daemon
docker ps

# Check system resources
free -h  # Memory
df -h    # Disk

# Reduce service count in compose file
# Start with just one service for debugging

# Check DSO logs
docker dso doctor --level full
```

---

## Best Practices

### 1. Secret Naming

✅ **Use clear, hierarchical names:**
```bash
docker dso secret set prod-ms/mysql-root-password
docker dso secret set prod-ms/mysql-user
docker dso secret set prod-ms/mysql-user-password
docker dso secret set api/api-key-prod
```

❌ **Avoid vague names:**
```bash
docker dso secret set password          # Ambiguous
docker dso secret set secret1           # Unclear purpose
docker dso secret set tmp               # Too generic
```

### 2. Environment Variables vs. Files

| Type | Use Case | Example |
|------|----------|---------|
| `dso://` (env vars) | Small, simple secrets | Passwords, usernames, API keys |
| `dsofile://` (files) | Large secrets | Certificates, private keys, configs |

### 3. Rotation Strategy

```yaml
# For critical services (databases)
dso.update.strategy: "rolling"

# For stateless services
dso.update.strategy: "restart"

# For apps that support signal reload
dso.update.strategy: "signal"

# For manual control
dso.update.strategy: "none"
```

### 4. Health Checks

Always include health checks:
```yaml
healthcheck:
  test: ["CMD", "mysqladmin", "ping", "-h", "localhost"]
  interval: 10s
  timeout: 5s
  retries: 5
```

### 5. Backup Your Vault

The master key (`~/.dso/master.key`) is critical:
```bash
# Backup
cp ~/.dso/master.key ~/.dso/master.key.backup

# Store safely
# DO NOT commit to git
# DO NOT share publicly
```

### 6. Never Hardcode Secrets

❌ **Wrong:**
```yaml
environment:
  MYSQL_ROOT_PASSWORD: "my-secret-password"
```

✅ **Right:**
```yaml
environment:
  MYSQL_ROOT_PASSWORD: dso://prod-ms/mysql-root-password
```

### 7. Use .gitignore

```bash
# .gitignore
.dso/
~/.dso/
dso.yaml
dso.lock
docker-compose.override.yml
.env
secrets/
```

### 8. Document Your Setup

Create `SETUP.md` for team members:
```markdown
## Local Development Setup

1. Install DSO: `./scripts/install.sh`
2. Initialize: `docker dso setup --mode local && docker dso init`
3. Set secrets:
   - `docker dso secret set prod-ms/mysql-root-password`
   - `docker dso secret set prod-ms/mysql-user`
   - `docker dso secret set prod-ms/mysql-user-password`
4. Deploy: `docker dso up -d`
5. Verify: `docker dso doctor`
```

---

## Security Model

### What DSO Protects

✅ **Plaintext secrets never touch disk**
- Held only in process memory
- tmpfs temporary storage

✅ **Encryption at rest**
- AES-256-GCM encryption
- Master key stored separately

✅ **No exposure to `docker inspect`**
- File-based secrets invisible
- Environment secrets visible (by design)

✅ **Process isolation**
- Agent socket restricted to `root:dso`
- Non-root users need group membership

### Threat Model

| Threat | Mitigation |
|--------|-----------|
| Secrets in logs | Log redaction, file injection |
| `docker inspect` exposure | Use `dsofile://` for sensitive data |
| Host filesystem access | tmpfs storage, volatile memory |
| Unauthorized user access | File permissions, group membership |
| Master key theft | Keep `~/.dso/master.key` secure |

### Master Key Protection

```bash
# Master key location
ls -la ~/.dso/master.key

# Permissions (should be 600)
ls -l ~/.dso/master.key
# -rw------- 1 user user 32 May 20 13:32 /home/user/.dso/master.key

# Backup (keep in secure location)
cp ~/.dso/master.key ~/secure-backup/
```

---

## Next Steps

### After Local Setup Works

1. **Test rotation** — Change a secret and watch it rotate
   ```bash
   docker dso secret set prod-ms/mysql-root-password
   docker dso status --watch
   ```

2. **Add more services** — Test multi-service coordination

3. **Learn advanced features** — Explore rotation strategies

4. **Move to production** — Use agent mode with cloud provider
   - See [Agent Mode Guide](../docs/getting-started.md)

### Useful Commands Reference

```bash
# Setup & init
docker dso setup --mode local
docker dso init

# Secret management
docker dso secret set prod-ms/mysql-root-password
docker dso secret list prod-ms
docker dso secret get prod-ms/mysql-root-password
docker dso secret delete prod-ms/mysql-root-password

# Deployment
docker dso up -d
docker dso down

# Monitoring
docker dso doctor
docker dso status --watch
docker dso watch

# Config
docker dso config show
docker dso config validate
```

---

## See Also

- **Getting Started Guide**: [getting-started.md](getting-started.md)
- **CLI Reference**: [cli.md](cli.md)
- **Configuration Reference**: [configuration.md](configuration.md)
- **Examples**: [examples/LOCAL_MODE_EXAMPLES.md](../examples/LOCAL_MODE_EXAMPLES.md)
- **Quick Reference**: [QUICKREF.md](QUICKREF.md)

---

## FAQ

**Q: Can I use DSO local mode in production?**  
A: Not recommended. Local mode is designed for development. For production, use agent mode with cloud provider (AWS, Azure, Vault, Huawei).

**Q: How do I migrate from local mode to agent mode?**  
A: Secrets are stored separately, so you'll need to re-enter them in the cloud provider. See migration guide in [operational-guide.md](operational-guide.md).

**Q: What happens if I lose my master key?**  
A: Your vault becomes unrecoverable. Always backup `~/.dso/master.key`.

**Q: Can multiple users share the same vault?**  
A: Not recommended. Each developer should have their own vault (`~/.dso/vault.enc`).

**Q: How large can secrets be?**  
A: Local vault supports secrets up to ~1MB each. For larger data, use file injection.

**Q: Is local mode secure?**  
A: Yes, for development. Secrets are AES-256 encrypted and never written to disk. For production, use cloud providers.

---

## Support

- **Issues**: [GitHub Issues](https://github.com/docker-secret-operator/dso/issues)
- **Discussions**: [GitHub Discussions](https://github.com/docker-secret-operator/dso/discussions)
- **Documentation**: [Full Docs](../docs/)

---

**Version**: 1.0  
**Last Updated**: May 2026  
**Status**: Stable ✅

---

---

## 📚 Documentation Structure

### Getting Started

**New to DSO? Start here:**

1. **[Quick Start (5 min)](getting-started.md#local-mode-setup)** - Minimal setup to deploy your first service
2. **[Complete Local Mode Guide](LOCAL_MODE_GUIDE.md)** - Comprehensive guide with examples and troubleshooting

### Real-World Examples

**Learn by doing:**

- **[MySQL + phpMyAdmin Quick Start](../examples/MYSQL_PHPMYADMIN_QUICKSTART.md)** ⭐ **START HERE**
  - Your real-world use case
  - Step-by-step walkthrough
  - Common tasks and troubleshooting
  - ~5 minute deployment

- **[MySQL + phpMyAdmin Full Example](../examples/mysql-phpmyadmin-local.yml)**
  - Complete, annotated docker-compose.yml
  - Production-like configuration
  - Best practices included

- **[Minimal Example](../examples/local-mode-minimal.yml)**
  - PostgreSQL + Python app
  - Simplest possible setup
  - Good for learning

- **[Full-Featured Example](../examples/local-mode-compose.yml)**
  - PostgreSQL, API, Redis, pgAdmin
  - Multiple secret injection patterns
  - Advanced configuration

- **[Local Mode Examples Guide](../examples/LOCAL_MODE_EXAMPLES.md)**
  - How to run each example
  - Rotation strategies explained
  - Secret management reference

### Reference Documentation

**For specific information:**

- **[CLI Reference](cli.md)** - Complete command reference
- **[Configuration Reference](configuration.md)** - YAML schema and options
- **[Quick Reference Card](QUICKREF.md)** - One-page cheat sheet

### Advanced Topics

- **[Architecture Guide](architecture.md)** - How DSO works internally
- **[Operational Guide](operational-guide.md)** - Day-2 operations
- **[Security Model](../SECURITY.md)** - Security analysis
- **[Recovery Procedures](RECOVERY_PROCEDURES.md)** - Failure recovery

---

## 🎯 By Use Case

### I want to...

#### **Get started in 5 minutes** ⭐
→ [MySQL + phpMyAdmin Quick Start](../examples/MYSQL_PHPMYADMIN_QUICKSTART.md)

#### **Understand how local mode works**
→ [Complete Local Mode Guide](LOCAL_MODE_GUIDE.md)

#### **Set up my own Docker Compose file**
→ [Complete Local Mode Guide - Docker Compose Configuration](LOCAL_MODE_GUIDE.md#docker-compose-configuration)

#### **Manage my secrets**
→ [Complete Local Mode Guide - Secret Management](LOCAL_MODE_GUIDE.md#secret-management)

#### **Test secret rotation**
→ [MySQL + phpMyAdmin Quick Start - Update a Secret](../examples/MYSQL_PHPMYADMIN_QUICKSTART.md#update-a-secret)

#### **Troubleshoot issues**
→ [MySQL + phpMyAdmin Quick Start - Troubleshooting](../examples/MYSQL_PHPMYADMIN_QUICKSTART.md#troubleshooting)

#### **Learn all CLI commands**
→ [CLI Reference](cli.md)

#### **Move to production**
→ [Complete Local Mode Guide - Next Steps](LOCAL_MODE_GUIDE.md#next-steps)

---

## 📋 Documentation Overview

### Local Mode Guide (`LOCAL_MODE_GUIDE.md`)
**Length**: ~2000 words | **Time to Read**: 20 minutes | **Scope**: Comprehensive

Topics:
- What is local mode
- Installation (3 steps)
- Quick start (5 minutes)
- Real-world example: MySQL + phpMyAdmin
- Secret management (set, list, get, delete, import)
- Docker Compose configuration (labels, injection methods, rotation strategies)
- Deployment & verification
- Monitoring & debugging
- Troubleshooting (9 common issues)
- Best practices (8 guidelines)
- Security model

**Best for**: Complete understanding, reference material

---

### MySQL + phpMyAdmin Quick Start (`MYSQL_PHPMYADMIN_QUICKSTART.md`)
**Length**: ~1000 words | **Time to Read**: 5-10 minutes | **Scope**: Hands-on tutorial

Topics:
- 5-step deployment walkthrough
- How to access services
- Common tasks (backup, logs, connection)
- Troubleshooting (4 common issues)
- Command cheat sheet
- Tips & tricks

**Best for**: Hands-on learning, quick reference

---

### Complete Examples (`examples/LOCAL_MODE_EXAMPLES.md`)
**Length**: ~1500 words | **Time to Read**: 10 minutes | **Scope**: All examples explained

Topics:
- How to run minimal example
- How to run full-featured example
- Secret injection methods explained
- All DSO labels and meanings
- Rotation strategies (rolling, restart, signal, none)
- Secret management commands
- Monitoring & debugging
- Troubleshooting

**Best for**: Understanding different configurations, learning features

---

### Quick Reference (`QUICKREF.md`)
**Length**: ~500 words | **Time to Read**: 2-3 minutes | **Scope**: Condensed reference

Topics:
- Essential commands
- Configuration examples
- Docker Compose integration
- File locations
- Permissions

**Best for**: Quick lookup, one-page reference

---

### Getting Started Guide (`getting-started.md`)
**Length**: ~1000 words | **Time to Read**: 10 minutes | **Scope**: All modes (local & cloud)

Topics:
- Prerequisites
- Installation
- Mode selection
- Local mode setup (step-by-step)
- Cloud mode setup
- Verification

**Best for**: Official getting started guide, all deployment modes

---

## 🔄 Common Workflows

### Workflow 1: Initial Setup
```
1. Read: MySQL + phpMyAdmin Quick Start (5 min)
2. Follow: Steps 1-5 in MYSQL_PHPMYADMIN_QUICKSTART.md
3. Reference: QUICKREF.md for available commands
4. Done! ✅
```

### Workflow 2: Deep Learning
```
1. Read: Local Mode Guide introduction
2. Follow: Real-world example walkthrough
3. Practice: Run example from examples/
4. Reference: CLI and configuration docs as needed
5. Done! ✅
```

### Workflow 3: Troubleshooting
```
1. Run: docker dso doctor --level full
2. Check: Troubleshooting section in LOCAL_MODE_GUIDE.md
3. Search: MYSQL_PHPMYADMIN_QUICKSTART.md#troubleshooting
4. Read: Complete Local Mode Guide for detailed info
5. Done! ✅
```

### Workflow 4: Docker Compose Setup
```
1. Read: Docker Compose Configuration section (LOCAL_MODE_GUIDE.md)
2. Reference: mysql-phpmyadmin-local.yml for example
3. Reference: local-mode-minimal.yml for minimal setup
4. Create: Your own docker-compose.yml
5. Follow: Deployment & Verification section
6. Done! ✅
```

---

## 📖 Quick Navigation

### By Learning Style

**Visual Learners**
- Start with examples: `examples/`
- Use annotated compose files: `mysql-phpmyadmin-local.yml`
- Follow step-by-step: `MYSQL_PHPMYADMIN_QUICKSTART.md`

**Hands-On Learners**
- Follow quick start: `MYSQL_PHPMYADMIN_QUICKSTART.md`
- Modify example configs: `examples/`
- Experiment with commands: `QUICKREF.md`

**Reference-Oriented Learners**
- Read complete guide: `LOCAL_MODE_GUIDE.md`
- Check CLI reference: `cli.md`
- Use quick reference: `QUICKREF.md`

**Deep-Dive Learners**
- Read architecture: `architecture.md`
- Study security: `SECURITY.md`
- Review operations: `operational-guide.md`

---

## 🎓 Learning Paths

### Path 1: 30-Minute Quick Start (Beginner)
1. Read MYSQL_PHPMYADMIN_QUICKSTART.md (5 min)
2. Follow steps 1-5 (10 min)
3. Explore services (10 min)
4. Practice updating a secret (5 min)
5. Done! ✅

### Path 2: 1-Hour Comprehensive (Intermediate)
1. Read LOCAL_MODE_GUIDE.md introduction (10 min)
2. Follow quick start (5 min)
3. Read real-world example section (15 min)
4. Follow MySQL + phpMyAdmin example (20 min)
5. Practice troubleshooting (10 min)
6. Done! ✅

### Path 3: 2-Hour Deep Dive (Advanced)
1. Read LOCAL_MODE_GUIDE.md completely (30 min)
2. Review all examples (20 min)
3. Follow MySQL + phpMyAdmin (15 min)
4. Test rotation strategies (20 min)
5. Troubleshoot intentionally (20 min)
6. Read security model (15 min)
7. Done! ✅

---

## 📌 Key Concepts

### Secret Injection Methods

| Method | Visibility | Best For | Documentation |
|--------|-----------|----------|---------------|
| `dso://` | Env vars | Simple secrets | LOCAL_MODE_GUIDE.md#environment-variable |
| `dsofile://` | Files | Sensitive data | LOCAL_MODE_GUIDE.md#file-injection |

→ [Learn more](LOCAL_MODE_GUIDE.md#docker-compose-configuration)

### Rotation Strategies

| Strategy | Downtime | Best For | Documentation |
|----------|----------|----------|---------------|
| `rolling` | Zero | Production services | LOCAL_MODE_GUIDE.md#rolling |
| `restart` | Brief | Stateless services | LOCAL_MODE_GUIDE.md#restart |
| `signal` | None | SIGHUP-aware apps | LOCAL_MODE_GUIDE.md#signal |
| `none` | N/A | Manual rotation | LOCAL_MODE_GUIDE.md#none |

→ [Learn more](LOCAL_MODE_GUIDE.md#rotation-strategies)

### Secret Management

```bash
docker dso secret set <name>        # Create/update
docker dso secret list [project]    # View all
docker dso secret get <name>        # Retrieve (local mode only)
docker dso secret delete <name>     # Remove
docker dso env import <file> <proj> # Bulk import
```

→ [Learn more](LOCAL_MODE_GUIDE.md#secret-management)

---

## ✅ Verification Checklist

After following any guide, verify your setup:

```bash
# ✅ 1. DSO installed
docker dso version

# ✅ 2. Local vault exists
ls ~/.dso/vault.enc ~/.dso/master.key

# ✅ 3. Secrets are set
docker dso secret list

# ✅ 4. Services running
docker ps

# ✅ 5. Health check passing
docker dso doctor

# ✅ 6. Status OK
docker dso status

# ✅ All systems operational!
```

---

## 🔗 Related Documentation

**For other deployment modes:**
- Cloud mode: [Agent Mode in Getting Started](getting-started.md#cloud-mode-setup)
- Providers: [Providers Guide](providers.md)

**For operations:**
- Day-2 operations: [Operational Guide](operational-guide.md)
- Recovery: [Recovery Procedures](RECOVERY_PROCEDURES.md)
- Monitoring: [Status Monitoring](LOCAL_MODE_GUIDE.md#monitoring--debugging)

**For development:**
- Architecture: [Architecture Guide](architecture.md)
- Security: [Security Model](../SECURITY.md)
- API: [API Reference](../docs/) (if available)

---

## 🎯 Quick Links

### Most Popular
- [MySQL + phpMyAdmin Quick Start](../examples/MYSQL_PHPMYADMIN_QUICKSTART.md) ⭐
- [Complete Local Mode Guide](LOCAL_MODE_GUIDE.md)
- [Quick Reference Card](QUICKREF.md)

### Examples
- [MySQL + phpMyAdmin Compose File](../examples/mysql-phpmyadmin-local.yml)
- [Minimal Example](../examples/local-mode-minimal.yml)
- [Full-Featured Example](../examples/local-mode-compose.yml)

### Reference
- [CLI Commands](cli.md)
- [Configuration Schema](configuration.md)
- [Troubleshooting](LOCAL_MODE_GUIDE.md#troubleshooting)

---

## 📞 Getting Help

**Something not working?**

1. **Quick check**: `docker dso doctor --level full`
2. **View logs**: `docker compose logs -f`
3. **Check secrets**: `docker dso secret list`
4. **Read troubleshooting**:
   - [MySQL Quick Start Troubleshooting](../examples/MYSQL_PHPMYADMIN_QUICKSTART.md#troubleshooting)
   - [Local Mode Guide Troubleshooting](LOCAL_MODE_GUIDE.md#troubleshooting)

**Still stuck?**

- **Full documentation**: [LOCAL_MODE_GUIDE.md](LOCAL_MODE_GUIDE.md)
- **Examples**: [LOCAL_MODE_EXAMPLES.md](../examples/LOCAL_MODE_EXAMPLES.md)
- **GitHub Issues**: https://github.com/docker-secret-operator/dso/issues
- **Discussions**: https://github.com/docker-secret-operator/dso/discussions

---

## 📄 Document Versions

| Document | Version | Updated | Status |
|----------|---------|---------|--------|
| LOCAL_MODE_GUIDE.md | 1.0 | May 2026 | ✅ Current |
| MYSQL_PHPMYADMIN_QUICKSTART.md | 1.0 | May 2026 | ✅ Current |
| LOCAL_MODE_EXAMPLES.md | 1.0 | May 2026 | ✅ Current |
| getting-started.md | 3.5.17 | May 2026 | ✅ Current |
| QUICKREF.md | Latest | May 2026 | ✅ Current |

---

## 🚀 Next Steps

### After Local Mode Works

1. **Test rotation** - Intentionally change a secret and watch zero-downtime swap
2. **Add more services** - Expand to full-stack application
3. **Explore advanced features** - Try different rotation strategies
4. **Move to production** - Switch to agent mode with cloud provider
5. **Contribute** - Help improve documentation or contribute to DSO

---

**Ready to get started?**

👉 **[Go to MySQL + phpMyAdmin Quick Start](../examples/MYSQL_PHPMYADMIN_QUICKSTART.md)**

**Want to learn everything?**

👉 **[Go to Complete Local Mode Guide](LOCAL_MODE_GUIDE.md)**

**Need a quick reference?**

👉 **[Go to Quick Reference Card](QUICKREF.md)**

---

**Version**: 1.0  
**Last Updated**: May 2026  
**Status**: Production Ready ✅
