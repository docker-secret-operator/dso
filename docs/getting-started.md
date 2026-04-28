# Getting Started with DSO

This guide covers a fresh installation and your first deployment for both modes.

---

## Prerequisites

- Docker (any recent version)
- Linux or macOS (amd64 or arm64)
- **No Go required** — DSO ships as a prebuilt binary

---

## Step 1 — Install

**User install (recommended for local development):**
```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | bash
```

**System-wide install (required for cloud/enterprise mode):**
```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sudo bash
```

The installer downloads the binary for your OS and architecture, verifies its SHA256 checksum, and places it in your Docker plugin directory. No compilation happens on your machine.

Verify:
```bash
docker dso version
```

---

## Step 2 — Choose Your Mode

DSO has two modes. Pick the one that matches your use case.

### Local Mode (default — recommended for most users)

No cloud account. No root. No systemd.

Secrets are stored in `~/.dso/vault.enc` (AES-256-GCM encrypted).

→ Continue to [Local Mode Setup](#local-mode-setup) below.

### Cloud Mode (enterprise)

For teams using AWS Secrets Manager, Azure Key Vault, or HashiCorp Vault. Requires root and Linux with systemd.

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
- Writes `/etc/dso/dso-agent.service` (systemd unit)
- Downloads and verifies provider plugins from the GitHub release
- Runs `systemctl enable --now dso-agent`

### 2. Create `/etc/dso/dso.yaml`

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

Cloud mode is auto-detected when `/etc/dso/dso.yaml` exists. You do not need to pass `--mode=cloud` explicitly.

### 3. Deploy
```bash
docker dso up -d
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
