# DSO Complete Setup Guide (v3.4.2)

A comprehensive step-by-step guide for installing and configuring Docker Secret Operator for both local development and production environments.

---

## Table of Contents

1. [Quick Start (5 minutes)](#quick-start)
2. [Installation](#installation)
3. [Local Development Setup](#local-development-setup)
4. [Production Setup with Cloud Providers](#production-setup)
5. [Configuration Guide](#configuration)
6. [Verification & Testing](#verification)
7. [Troubleshooting](#troubleshooting)
8. [Next Steps](#next-steps)

---

## Quick Start

### For Local Development (Non-Root)

```bash
# 1. Install DSO
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sh

# 2. Verify installation
docker dso version

# 3. Initialize local environment
docker dso bootstrap local

# 4. Check health
docker dso doctor

# 5. View status
docker dso status

# You're ready! Create docker-compose.yaml with dso:// references
docker dso compose up
```

### For Production (Requires Root + Systemd)

```bash
# 1. Install DSO globally
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sudo bash

# 2. Initialize agent environment
sudo docker dso bootstrap agent

# 3. Edit configuration
sudo nano /etc/dso/dso.yaml

# 4. Enable and start service
sudo docker dso system enable

# 5. Monitor status
docker dso status --watch
```

---

## Installation

### Prerequisites

| Requirement | Details |
|---|---|
| **OS** | Linux (amd64, arm64) or macOS (amd64, arm64) |
| **Docker** | 20.10+ with Docker Compose support |
| **Root** | Only required for production/systemd setup |
| **Go** | Not required — binary is prebuilt |

### Step 1: Download and Install Binary

**Local User Installation** (recommended for development):
```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sh
```

**Global Installation** (requires sudo, for production):
```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sudo bash
```

**Manual Installation:**
```bash
# Download the appropriate binary for your platform
curl -Lo dso https://github.com/docker-secret-operator/dso/releases/download/v3.4.2/dso-linux-amd64

# Make it executable
chmod +x dso

# Install locally
mkdir -p ~/.docker/cli-plugins
mv dso ~/.docker/cli-plugins/

# Or install globally
sudo mkdir -p /usr/local/lib/docker/cli-plugins
sudo mv dso /usr/local/lib/docker/cli-plugins/
sudo chmod +x /usr/local/lib/docker/cli-plugins/dso
```

### Step 2: Verify Installation

```bash
docker dso version
# Output: Docker Secret Operator v3.4.2

docker dso --help
# Shows all available commands
```

### Step 3: Reload Docker Plugin Cache (if needed)

```bash
docker ps  # This reloads plugin cache
docker dso version
```

---

## Local Development Setup

Perfect for development, testing, and CI/CD environments. No root required.

### Phase 1: Bootstrap Local Environment

Initialize DSO with local encrypted storage:

```bash
docker dso bootstrap local
```

**What this creates:**
```
~/.dso/
├── config.yaml          # Configuration file
├── vault.enc           # Encrypted secret storage (AES-256)
├── state/              # Rotation state tracking
│   └── rotations.json
└── cache/              # Secret cache
    └── secrets.cache
```

**Output indicates:**
```
✓ DSO local environment initialized
✓ Configuration: ~/.dso/config.yaml
✓ Vault: ~/.dso/vault.enc
✓ Ready for deployment
```

### Phase 2: Verify Health

```bash
docker dso doctor
```

Should show all checks passing:
```
✓ Docker connectivity: OK
✓ Runtime mode: local
✓ Configuration: valid
✓ Vault: initialized
✓ Permissions: OK
```

### Phase 3: Review Configuration

View the auto-generated configuration:
```bash
docker dso config show
```

Default local configuration:
```yaml
version: "1.0"
runtime:
  mode: local
  log_level: info

providers:
  local:
    type: file
    enabled: true

defaults:
  inject:
    type: env
  rotation:
    enabled: true
    strategy: rolling

agent:
  cache: true
  watch:
    polling_interval: 1m
```

### Phase 4: Create a Secret

Store a test secret:

```bash
docker dso secret set myapp/db_password
# Enter secret value: (hidden prompt)
```

Or pipe from a file:
```bash
echo "my-secret-value" | docker dso secret set myapp/api_key
```

### Phase 5: Create docker-compose.yaml

Create a test compose file with secret references:

```yaml
version: '3.8'

services:
  postgres:
    image: postgres:15
    environment:
      # File injection (recommended - invisible to docker inspect)
      POSTGRES_PASSWORD_FILE: dsofile://myapp/db_password
      
      # Or environment injection (visible but convenient for dev)
      # POSTGRES_PASSWORD: dso://myapp/db_password
    
    volumes:
      - postgres_data:/var/lib/postgresql/data

  api:
    image: myapp:latest
    environment:
      DATABASE_URL: postgres://postgres:5432/mydb
      API_KEY: dso://myapp/api_key
    depends_on:
      - postgres
    ports:
      - "8080:8080"

volumes:
  postgres_data:
```

### Phase 6: Deploy with DSO

Start containers with secret injection:

```bash
docker dso compose up
```

DSO will:
1. Read docker-compose.yaml
2. Resolve all `dso://` and `dsofile://` references
3. Fetch secrets from encrypted vault
4. Inject into containers at startup
5. Start containers normally

**Verify secrets are NOT exposed:**
```bash
# Secret NOT visible in docker inspect
docker inspect postgres | grep POSTGRES_PASSWORD
# (no output — this is correct)

# But file exists in container
docker exec postgres cat /run/secrets/myapp/db_password
# my-secret-value
```

### Phase 7: Rotate a Secret

Update a secret and watch automatic rotation:

```bash
# Update the secret
docker dso secret set myapp/db_password
# Enter new secret value: (hidden prompt)

# Watch rotation happen automatically
docker dso status --watch
```

DSO will:
1. Detect the change
2. Create a new container with the updated secret
3. Verify it's healthy
4. Perform atomic container swap
5. Stop the old container
6. Complete in ~30 seconds

---

## Production Setup

For production deployments on single Docker hosts using systemd service management.

### Prerequisites for Production

- Linux system with systemd
- Docker installed and running
- Root/sudo access
- Internet access to fetch provider plugins

### Phase 1: Install Globally

Install for all users on the system:

```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sudo bash
```

Verify installation:
```bash
docker dso version
```

### Phase 2: Bootstrap Agent Environment

Initialize production environment:

```bash
sudo docker dso bootstrap agent
```

**What this creates:**
```
/etc/dso/                           # Configuration directory (root:dso, 0755)
├── dso.yaml                        # Configuration (root:dso, 0664)
└── ca.crt                          # TLS certificates (if needed)

/var/lib/dso/                       # State directory (root:dso, 0770)
├── state/
│   └── rotations.json              # Rotation tracking
├── cache/                          # Secret cache
└── locks/                          # Rotation locks

/var/log/dso/                       # Log directory (root:dso, 0770)
└── dso-agent.log

/var/run/dso/                       # Runtime directory (root:dso, 0775)
└── dso-agent.sock                  # Agent socket

/etc/systemd/system/
└── dso-agent.service               # Systemd service unit
```

**Output:**
```
✓ DSO agent environment initialized
✓ Configuration: /etc/dso/dso.yaml
✓ Service: /etc/systemd/system/dso-agent.service
✓ Ready to configure providers
```

### Phase 3: Configure Cloud Provider

Edit the configuration file to add your secret backend:

```bash
sudo nano /etc/dso/dso.yaml
```

#### AWS Secrets Manager

```yaml
version: "1.0"
runtime:
  mode: agent
  log_level: info

providers:
  aws:
    type: aws
    region: us-east-1
    # Uses IAM role attached to EC2 instance

secrets:
  - name: myapp/db_password
    provider: aws
    mappings:
      value: DATABASE_PASSWORD

defaults:
  inject:
    type: env
  rotation:
    enabled: true
    strategy: rolling

agent:
  cache: true
  watch:
    polling_interval: 5m
```

#### Azure Key Vault

```yaml
version: "1.0"
runtime:
  mode: agent
  log_level: info

providers:
  azure:
    type: azure
    vault_url: https://myvault.vault.azure.net
    # Uses Managed Identity attached to VM

secrets:
  - name: myapp/db-password
    provider: azure
    mappings:
      value: DATABASE_PASSWORD

defaults:
  inject:
    type: env
  rotation:
    enabled: true
    strategy: rolling

agent:
  cache: true
  watch:
    polling_interval: 5m
```

#### HashiCorp Vault

```yaml
version: "1.0"
runtime:
  mode: agent
  log_level: info

providers:
  vault:
    type: vault
    address: https://vault.example.com:8200
    auth:
      method: token
      token_env: VAULT_TOKEN  # Read from environment variable
    mount_path: secret/data

secrets:
  - name: myapp/db_password
    provider: vault
    mappings:
      value: DATABASE_PASSWORD

defaults:
  inject:
    type: env
  rotation:
    enabled: true
    strategy: rolling

agent:
  cache: true
  watch:
    polling_interval: 5m
```

#### Huawei Cloud KMS

```yaml
version: "1.0"
runtime:
  mode: agent
  log_level: info

providers:
  huawei:
    type: huawei
    region: cn-east-2
    project_id: "your-project-id"

secrets:
  - name: myapp/db_password
    provider: huawei
    mappings:
      value: DATABASE_PASSWORD

defaults:
  inject:
    type: env
  rotation:
    enabled: true
    strategy: rolling

agent:
  cache: true
  watch:
    polling_interval: 5m
```

### Phase 4: Validate Configuration

Test the configuration before starting the service:

```bash
sudo docker dso config validate
```

Should show:
```
✓ Configuration syntax: valid
✓ Providers configured: 1
✓ Secrets defined: 1
✓ Ready to start service
```

### Phase 5: Enable and Start Service

Start the DSO agent as a systemd service:

```bash
sudo docker dso system enable
```

This will:
1. Enable service to start on boot
2. Start the service immediately
3. Verify service is running

**Check service status:**
```bash
docker dso system status
```

Output:
```
Service Status: active (running)
Enabled: yes
Uptime: 2m 15s
Recent logs:
  [INFO] Agent started
  [INFO] Configuration loaded from /etc/dso/dso.yaml
  [INFO] Provider 'aws' initialized
  [INFO] Event watcher started
```

### Phase 6: Monitor Logs

View real-time service logs:

```bash
docker dso system logs -f
```

Or view specific time range:
```bash
docker dso system logs --since 1h  # Last hour
docker dso system logs -n 100      # Last 100 lines
```

### Phase 7: Enable Non-Root Access (Optional)

Allow non-root users to run DSO commands without sudo:

```bash
# Automatically configure current user for non-root access
sudo docker dso bootstrap agent --enable-nonroot
```

**What this does:**
- Adds your user to the `dso` group
- Adds your user to the `docker` group
- Enables you to run `docker dso status`, `docker dso config show`, etc. without sudo

**Important:** You must **log out and log back in** for group changes to take effect.

**Without this flag:**
- You can still use DSO, but CLI commands require `sudo`
- Only non-root operations require this; production deployments often don't need it

---

## Configuration

### Configuration File Location

| Mode | Location | Owner | Permissions |
|---|---|---|---|
| **Local** | `~/.dso/config.yaml` | User | 0600 |
| **Agent** | `/etc/dso/dso.yaml` | root:dso | 0664 |

### Core Configuration Schema

```yaml
# Version (required)
version: "1.0"

# Runtime settings (required)
runtime:
  mode: local|agent           # local or agent
  log_level: debug|info|warn|error

# Secret providers (required)
providers:
  # Local file backend (for development)
  local:
    type: file
    enabled: true
  
  # AWS Secrets Manager
  aws:
    type: aws
    region: us-east-1
    auth:
      method: iam|sso|static
  
  # Azure Key Vault
  azure:
    type: azure
    vault_url: https://vault.vault.azure.net
    auth:
      method: managed_identity|client_secret
  
  # HashiCorp Vault
  vault:
    type: vault
    address: https://vault.example.com:8200
    auth:
      method: token|approle|kubernetes|jwt
      token_env: VAULT_TOKEN
    mount_path: secret/data
  
  # Huawei Cloud KMS
  huawei:
    type: huawei
    region: cn-east-2
    project_id: "project-id"

# Secret mappings (required)
secrets:
  - name: app/db_password
    provider: vault
    mappings:
      value: DATABASE_PASSWORD

# Default injection and rotation settings
defaults:
  inject:
    type: env|file              # env=environment, file=tmpfs
    path: /run/secrets          # For file injection
  
  rotation:
    enabled: true
    strategy: rolling           # blue-green deployment
    timeout: 30s

# Agent runtime configuration
agent:
  cache: true
  
  # Secret resolution
  watch:
    polling_interval: 5m        # How often to check for changes
    debounce_window: 5s         # Batch rapid changes
  
  # Health verification
  health_check:
    timeout: 30s                # Max time to wait for healthy
    retries: 3
    interval: 2s
```

### Configuration Management Commands

**View configuration:**
```bash
docker dso config show
```

**Edit configuration:**
```bash
docker dso config edit
# Opens in $EDITOR
```

**Validate configuration:**
```bash
docker dso config validate
```

**Apply changes (production):**
```bash
# For changes to take effect:
sudo docker dso system restart
```

---

## Verification & Testing

### Health Checks

**Quick health check:**
```bash
docker dso doctor
```

**Full diagnostics:**
```bash
docker dso doctor --level full
```

**JSON output for scripting:**
```bash
docker dso doctor --json
```

### Status Monitoring

**Single status check:**
```bash
docker dso status
```

**Live monitoring:**
```bash
docker dso status --watch
```

**JSON output:**
```bash
docker dso status --json
```

### Testing with Example Containers

**Test with PostgreSQL:**
```yaml
version: '3.8'

services:
  postgres:
    image: postgres:15
    environment:
      POSTGRES_PASSWORD_FILE: dsofile://test/db_password
      POSTGRES_USER: postgres
    ports:
      - "5432:5432"
```

Store the secret:
```bash
docker dso secret set test/db_password
# Enter: mypassword
```

Deploy:
```bash
docker dso compose up
docker exec postgres psql -U postgres -c "SELECT version();"
```

**Test rotation:**
```bash
# Update the secret
docker dso secret set test/db_password
# Enter: newpassword

# Watch the rotation
docker dso status --watch

# Verify new password works
docker exec postgres psql -U postgres -c "SELECT version();"
```

---

## Troubleshooting

### Installation Issues

**"docker dso: command not found"**
```bash
# Reload plugin cache
docker ps

# Verify plugin installed
ls -la ~/.docker/cli-plugins/docker-dso
# or
ls -la /usr/local/lib/docker/cli-plugins/docker-dso

# Try again
docker dso version
```

**Permission denied on install**
```bash
# Local install (no sudo needed)
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sh

# Or fix permissions manually
sudo chmod +x /usr/local/lib/docker/cli-plugins/dso
```

### Bootstrap Issues

**"Docker socket not accessible"**
```bash
# Check if Docker is running
docker ps

# Check socket permissions
ls -la /var/run/docker.sock

# Add user to docker group
sudo usermod -aG docker $USER
newgrp docker
```

**"Configuration directory cannot be created"**
```bash
# Local mode - check home directory
ls -la ~/

# Agent mode - check /etc/dso ownership
ls -la /etc/dso/

# Fix permissions if needed
sudo chown root:dso /etc/dso/
sudo chmod 755 /etc/dso/
```

### Configuration Issues

**"Configuration validation failed"**
```bash
# Check syntax
docker dso config validate

# View full configuration
docker dso config show

# Edit and fix
docker dso config edit
```

**"Provider not found"**
```bash
# List available providers
docker dso system status

# Check provider configuration
docker dso config show | grep providers

# Test provider connectivity
docker dso doctor --level full
```

### Secret Resolution Issues

**"Secret not resolved / empty value"**
```bash
# Check if secret exists
docker dso secret list

# Check secret value
docker dso secret get app/db_password

# Verify in config
docker dso config show

# Check container logs
docker logs <container_name>
```

**"dso:// references not resolving"**
```bash
# Verify mode
docker dso status | grep "Mode"

# Check if dso-agent is running
docker dso system status

# View logs for errors
docker dso system logs -p err
```

### Service Issues (Production)

**"Service failed to start"**
```bash
# Check service status
sudo systemctl status dso-agent.service

# View detailed logs
sudo journalctl -u dso-agent -n 100

# Check configuration
sudo docker dso config validate

# Try manual restart
sudo docker dso system restart
```

**"Service crashes on startup"**
```bash
# View crash logs
sudo journalctl -u dso-agent -p err

# Check systemd service file
cat /etc/systemd/system/dso-agent.service

# Check directory permissions
ls -la /etc/dso/
ls -la /var/lib/dso/

# Verify configuration
sudo docker dso config validate
```

**"Permission denied errors"**
```bash
# Check directory ownership
ls -la /etc/dso/
ls -la /var/lib/dso/
ls -la /var/log/dso/

# Fix ownership if needed
sudo chown -R root:dso /etc/dso/
sudo chown -R root:dso /var/lib/dso/
sudo chown -R root:dso /var/log/dso/

# Fix permissions
sudo chmod 755 /etc/dso/
sudo chmod 770 /var/lib/dso/
sudo chmod 770 /var/log/dso/
```

### Secret Rotation Issues

**"Rotation stuck / pending"**
```bash
# Check status
docker dso status

# View logs
docker dso system logs -f

# Check container state
docker ps -a | grep -E "postgres|postgres-old|postgres-new"

# Manual recovery (if needed)
docker stop postgres-new 2>/dev/null || true
docker rm postgres-new 2>/dev/null || true
docker rename postgres-old postgres 2>/dev/null || true
```

**"Health check failed"**
```bash
# View logs for details
docker dso system logs -p err

# Check container logs
docker logs <container_name>

# Increase timeout if needed
docker dso config edit
# Increase health_check.timeout to 60s or more
```

---

## Next Steps

### Learn More

- **[CLI Reference](cli.md)** — All available commands
- **[Configuration Guide](configuration.md)** — Advanced config options
- **[Architecture Overview](architecture.md)** — How DSO works internally
- **[Provider Setup Guides](providers.md)** — Detailed provider-specific instructions
- **[Operational Guide](operational-guide.md)** — Day-2 operations and monitoring

### Example Deployments

See the [Examples](examples/) directory for complete working configurations:
- PostgreSQL with local vault
- Node.js with AWS Secrets Manager
- Django with Azure Key Vault
- Full-stack app with Vault

### Security

Review the [Security Model](../SECURITY.md) and [Threat Model](../THREAT_MODEL.md) for security considerations and best practices.

---

## Getting Help

- **Issues:** [GitHub Issues](https://github.com/docker-secret-operator/dso/issues)
- **Discussions:** [GitHub Discussions](https://github.com/docker-secret-operator/dso/discussions)
- **Documentation:** [Full Docs](../docs)
- **Security:** security@docker-secret-operator.org

---

## Summary

| Mode | Setup Time | Root Required | Ideal For |
|---|---|---|---|
| **Local** | 5 min | No | Development, testing, CI |
| **Production** | 15 min | Yes | Production deployments |

**You're ready to use DSO!** Start with [Quick Start](#quick-start) or follow the detailed guides above for your use case.
