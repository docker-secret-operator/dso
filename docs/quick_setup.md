# DSO Quick Setup Guide (v3.2)

Get DSO running in under 3 minutes.

---

## 1. Install

```bash
# Local user install (recommended for development)
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | bash

# Global system install (required for Cloud Mode)
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sudo bash
```

Verify:
```bash
docker dso version
# Docker Secret Operator (DSO) v3.2.0
```

---

## 2. Choose Your Mode

| | Local Mode | Cloud Mode |
|---|---|---|
| **Use case** | Development, CI, offline | Production with AWS/Vault/Azure |
| **Requires** | Nothing extra | Root + Linux + systemd |
| **Secret store** | `~/.dso/vault.enc` | External provider |
| **Setup command** | `docker dso init` | `sudo docker dso system setup` |

---

## 🏠 Local Mode (Quick Start)

```bash
# 1. Initialize the encrypted vault
docker dso init

# 2. Store secrets
docker dso secret set myapp/db_password
docker dso secret set myapp/api_key

# Or bulk-import from an existing .env file
docker dso env import .env.local myapp

# 3. Reference secrets in docker-compose.yaml
#    dsofile:// — secure file injection (recommended)
#    dso://     — environment variable injection

# 4. Deploy
docker dso up -d
```

**Example `docker-compose.yaml`:**
```yaml
services:
  db:
    image: postgres:15
    environment:
      POSTGRES_PASSWORD_FILE: dsofile://myapp/db_password

  api:
    image: my-api:latest
    environment:
      STRIPE_KEY: dso://myapp/api_key
```

---

## ☁️ Cloud Mode (Production)

```bash
# 1. Install globally (as root)
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sudo bash

# 2. Install plugins + configure systemd
sudo docker dso system setup

# 3. Create /etc/dso/dso.yaml
# 4. Deploy
docker dso up -d
```

**Minimal `/etc/dso/dso.yaml`:**
```yaml
providers:
  vault-prod:
    type: vault
    address: https://vault.example.com:8200
    token: ${VAULT_TOKEN}

secrets:
  - name: prod/db_password
    provider: vault-prod
    mappings:
      DB_PASSWORD: DATABASE_PASSWORD
```

DSO auto-detects Cloud Mode when `/etc/dso/dso.yaml` exists. No `--mode=cloud` flag needed.

---

## 3. Diagnose

```bash
docker dso system doctor
```

Run this at any time to verify mode detection, config file status, systemd service state, and plugin availability.

---

## Production Checklist

- [ ] Use `dsofile://` instead of `dso://` — secrets are invisible to `docker inspect`
- [ ] Run `docker dso validate` before first deploy
- [ ] Delete plaintext `.env` files after importing with `docker dso env import`
- [ ] Use `docker dso system doctor` after Cloud Mode setup
- [ ] Monitor with `docker dso logs -f` and `docker dso watch`

---

## Key Commands Reference

| Goal | Command |
|---|---|
| Initialize Local vault | `docker dso init` |
| Store a secret | `docker dso secret set myapp/key` |
| List secrets | `docker dso secret list myapp` |
| Import `.env` file | `docker dso env import .env myapp` |
| Deploy stack | `docker dso up -d` |
| Stop stack | `docker dso down` |
| Cloud Mode setup | `sudo docker dso system setup` |
| Validate config | `docker dso validate` |
| Run diagnostics | `docker dso system doctor` |
| Watch live events | `docker dso watch` |
| View agent logs | `docker dso logs -f` |
