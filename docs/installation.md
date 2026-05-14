# Installation Guide — Docker Plugin

DSO is a Docker CLI plugin. This guide covers installation as `docker dso` command.

---

## Quick Install

### User-Level Install (Local Development)

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
# Docker Secret Operator (DSO) vX.Y.Z
```

If not found, restart Docker:
```bash
docker ps  # Reloads plugins
docker dso version
```

---

## How DSO Works as a Docker Plugin

Docker automatically discovers binaries named `docker-<pluginname>` in plugin directories:

1. **User plugins** (checked first): `~/.docker/cli-plugins/`
2. **System plugins** (checked second): `/usr/local/lib/docker/cli-plugins/`

When you run `docker dso bootstrap local`, Docker:
1. Looks for `docker-dso` in plugin directories
2. Executes it with `dso bootstrap local` as arguments
3. DSO strips duplicate "dso" argument and processes

This allows seamless integration with the `docker` command itself.

---

## Installation Methods

### Method 1: Automated Script (Recommended)

**User-level:**
```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sh
```

**System-wide:**
```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sudo sh
```

The script:
- Downloads the binary for your OS/architecture (amd64, arm64)
- Verifies SHA256 checksum
- Places it in the appropriate plugin directory
- Sets executable permissions

### Method 2: Manual Install

**User-level:**
```bash
mkdir -p ~/.docker/cli-plugins
curl -Lo ~/.docker/cli-plugins/docker-dso \
  https://github.com/docker-secret-operator/dso/releases/download/vX.Y.Z/dso-linux-amd64
chmod +x ~/.docker/cli-plugins/docker-dso
```

**System-wide:**
```bash
sudo mkdir -p /usr/local/lib/docker/cli-plugins
sudo curl -Lo /usr/local/lib/docker/cli-plugins/docker-dso \
  https://github.com/docker-secret-operator/dso/releases/download/vX.Y.Z/dso-linux-amd64
sudo chmod +x /usr/local/lib/docker/cli-plugins/docker-dso
```

### Method 3: Build from Source

```bash
git clone https://github.com/docker-secret-operator/dso.git
cd dso

# Build the binary
make build

# Install to user plugins directory
mkdir -p ~/.docker/cli-plugins
cp docker-dso ~/.docker/cli-plugins/

# Or system-wide
sudo install -m 755 docker-dso /usr/local/lib/docker/cli-plugins/
```

---

## Requirements

| | Local Bootstrap | Agent Bootstrap |
|---|---|---|
| **OS** | Linux, macOS | Linux only |
| **Architecture** | amd64, arm64 | amd64, arm64 |
| **Docker** | Any recent version (20.10+) | Any recent version |
| **systemd** | Not required | Required |
| **Root** | Not required | Required |
| **Go** | Not required | Not required |

---

## Verification

```bash
# List installed plugins
docker plugin ls
# (Note: CLI plugins not shown here, only daemon plugins)

# Check DSO plugin specifically
docker dso version

# Check plugin location
which docker-dso
# or
ls ~/.docker/cli-plugins/docker-dso
ls /usr/local/lib/docker/cli-plugins/docker-dso
```

---

## Troubleshooting Installation

### Plugin Not Found

```bash
docker: 'dso' is not a docker command
```

**Solutions:**

1. Verify plugin exists:
   ```bash
   ls ~/.docker/cli-plugins/docker-dso
   # or
   ls /usr/local/lib/docker/cli-plugins/docker-dso
   ```

2. Fix permissions:
   ```bash
   chmod +x ~/.docker/cli-plugins/docker-dso
   # or
   sudo chmod +x /usr/local/lib/docker/cli-plugins/docker-dso
   ```

3. Reload plugins:
   ```bash
   docker ps
   docker dso version
   ```

### Cannot Execute Binary

```bash
Permission denied while trying to connect to Docker daemon socket
```

**Solutions:**

1. Check Docker is running:
   ```bash
   docker ps
   ```

2. Add user to docker group:
   ```bash
   sudo usermod -aG docker $USER
   newgrp docker
   ```

3. Or use `sudo`:
   ```bash
   sudo docker dso bootstrap agent
   ```

---

## Next Steps

After installation:

1. **Bootstrap environment:**
   ```bash
   # Local development
   docker dso bootstrap local
   
   # Or production (requires root)
   sudo docker dso bootstrap agent
   ```

2. **Check health:**
   ```bash
   docker dso doctor
   ```

3. **View status:**
   ```bash
   docker dso status
   ```

For detailed setup instructions, see [Getting Started](getting-started.md).

---

For Docker plugin details, see [Docker Plugin Integration](docker-plugin.md).

---

## Cloud Mode: Installing Provider Plugins

After installing the CLI, Cloud Mode requires provider plugins. Install **only the providers you need**:

### Option 1: `--providers` flag (recommended)

```bash
# Install only Vault
sudo docker dso system setup --providers vault

# Install AWS + Vault
sudo docker dso system setup --providers aws,vault

# Install all providers
sudo docker dso system setup --providers vault,aws,azure,huawei
```

### Option 2: `DSO_PROVIDERS` environment variable (CI/CD)

```bash
# Suitable for non-interactive scripts and pipelines
DSO_PROVIDERS=aws,vault sudo docker dso system setup

# In a CI YAML:
# env:
#   DSO_PROVIDERS: "vault"
# run: sudo docker dso system setup
```

### Option 3: Interactive prompt (terminal only)

If no flag or environment variable is provided and stdin is a terminal, DSO presents a numbered menu:

```
[DSO] Select providers to install:
  [1] vault       (dso-provider-vault)
  [2] aws         (dso-provider-aws)
  [3] azure       (dso-provider-azure)
  [4] huawei      (dso-provider-huawei)

  Enter numbers separated by commas (e.g. 1,2), or press Enter for default [vault]:
```

### Option 4: Default (vault only)

If nothing is specified and there is no terminal (e.g. a piped script), DSO installs **vault** as the safe default:

```bash
# This installs vault only
sudo docker dso system setup
```

---

## Available Providers

| Provider | Flag Name | Description |
|---|---|---|
| HashiCorp Vault | `vault` | KV v2, token auth |
| AWS Secrets Manager | `aws` | IAM role, env vars, `~/.aws/credentials` |
| Azure Key Vault | `azure` | Managed Identity, service principal, `az login` |
| Huawei Cloud CSMS | `huawei` | AK/SK, IAM Agency, security token |

---

## Re-running Setup to Add Providers

`system setup` is safe to re-run. Existing plugins are preserved; only the newly specified providers are downloaded and installed.

```bash
# Initial setup — only Vault
sudo docker dso system setup --providers vault

# Later, add AWS without touching Vault
sudo docker dso system setup --providers aws

# Both vault and aws are now installed
docker dso system doctor
```

> Re-running will restart the `dso-agent` service — brief downtime expected.

---

## Error: Provider Not Installed

If `docker dso up` fails because a required provider is not installed:

```text
Error: provider plugin 'aws' is not installed.
  Expected: /usr/local/lib/dso/plugins/dso-provider-aws
  Fix: sudo docker dso system setup --providers aws
  Run: docker dso system doctor
```

---

## Post-Install Verification

```bash
# Check mode detection, config, vault, systemd, and plugin status
docker dso system doctor
```

Example output after installing Vault only:

```
DSO System Diagnostics — v3.4.0
════════════════════════════════════════════════════════════════════
Component         Status       Detail
────────────────────────────────────────────────────────────────────
Binary            OK           /usr/local/lib/docker/cli-plugins/docker-dso (v3.4.0)
Effective UID     root
Detected Mode     CLOUD        Reason: auto-detected (/etc/dso/dso.yaml)
Config            OK           /etc/dso/dso.yaml
Vault             NOT FOUND    /home/user/.dso/vault.enc
Systemd Service   OK           File: /etc/systemd/system/dso-agent.service | Runtime: active
────────────────────────────────────────────────────────────────────
Provider Plugins
────────────────────────────────────────────────────────────────────
Plugin: vault     OK           /usr/local/lib/dso/plugins/dso-provider-vault (v3.4.0)
Plugin: aws       NOT INSTALLED  Install: sudo docker dso system setup --providers aws
Plugin: azure     NOT INSTALLED  Install: sudo docker dso system setup --providers azure
Plugin: huawei    NOT INSTALLED  Install: sudo docker dso system setup --providers huawei
════════════════════════════════════════════════════════════════════
```

`NOT INSTALLED` is informational — it means the provider was intentionally not installed, not that something is broken.

---

## Uninstalling

```bash
# Remove CLI binary (user install)
rm ~/.docker/cli-plugins/docker-dso ~/.local/bin/dso

# Remove CLI binary (global install)
sudo rm /usr/local/lib/docker/cli-plugins/docker-dso /usr/local/bin/dso

# Remove plugins
sudo rm -rf /usr/local/lib/dso/plugins

# Remove systemd service
sudo systemctl stop dso-agent
sudo systemctl disable dso-agent
sudo rm /etc/systemd/system/dso-agent.service
sudo systemctl daemon-reload

# Remove local vault
rm -rf ~/.dso

# Remove config
sudo rm -rf /etc/dso
```

---

## Installer Environment Variables

| Variable | Description |
|---|---|
| `DSO_VERSION` | Override the version to install (e.g. `v3.4.0`) |
| `DSO_PROVIDERS` | Comma-separated providers for `system setup` (e.g. `aws,vault`) |
| `DSO_SOCKET_PATH` | Override agent socket path (default: `/var/run/dso.sock`) |
| `DSO_PLUGIN_DIR` | Override plugin directory (default: `/usr/local/lib/dso/plugins`) |
