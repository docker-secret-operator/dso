# Installation Guide (v3.2)

---

## Quick Install

**User install (Local Mode — recommended for development):**
```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | bash
```

**Global install (required for Cloud Mode / systemd):**
```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sudo bash
```

Verify:
```bash
docker dso version
# Docker Secret Operator (DSO) v3.2.0
```

---

## What the Installer Does

`install.sh` installs **only the DSO CLI binary**. It does not install provider plugins or configure systemd.

| Action | Done by |
|---|---|
| Download CLI binary | `install.sh` |
| Verify SHA256 checksum | `install.sh` |
| Place binary in plugin dir | `install.sh` |
| Install provider plugins | `sudo docker dso system setup` |
| Configure systemd | `sudo docker dso system setup` |
| Initialize local vault | `docker dso init` |

---

## Requirements

| | Local Mode | Cloud Mode |
|---|---|---|
| **OS** | Linux or macOS | Linux only |
| **Architecture** | amd64, arm64 | amd64, arm64 |
| **Docker** | Any recent version | Any recent version |
| **systemd** | Not required | Required |
| **Root** | Not required | Required for `system setup` |
| **Go** | Not required | Not required |

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
DSO System Diagnostics — v3.2.0
════════════════════════════════════════════════════════════════════
Component         Status       Detail
────────────────────────────────────────────────────────────────────
Binary            OK           /usr/local/lib/docker/cli-plugins/docker-dso (v3.2.0)
Effective UID     1000
Detected Mode     CLOUD        Reason: auto-detected (/etc/dso/dso.yaml)
Config            OK           /etc/dso/dso.yaml
Vault             NOT FOUND    /home/user/.dso/vault.enc
Systemd Service   OK           File: /etc/systemd/system/dso-agent.service | Runtime: active
────────────────────────────────────────────────────────────────────
Provider Plugins
────────────────────────────────────────────────────────────────────
Plugin: vault     OK           /usr/local/lib/dso/plugins/dso-provider-vault (v3.2.0)
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
| `DSO_VERSION` | Override the version to install (e.g. `v3.2.0`) |
| `DSO_PROVIDERS` | Comma-separated providers for `system setup` (e.g. `aws,vault`) |
| `DSO_SOCKET_PATH` | Override agent socket path (default: `/var/run/dso.sock`) |
| `DSO_PLUGIN_DIR` | Override plugin directory (default: `/usr/local/lib/dso/plugins`) |
