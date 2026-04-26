<h1 align="center">Docker Secret Operator (DSO)</h1>
<p align="center">
  <b>The ultimate local-first secret manager for Docker Compose. Zero `.env` leaks. Zero `docker inspect` exposure.</b>
</p>

---

## 🚫 The Problem

Local development traditionally relies on `.env` files to pass database passwords, API keys, and other secrets into containers. This creates two massive security vulnerabilities:
1. **Git Leaks:** Developers accidentally commit `.env` files to source control, exposing production credentials to the public.
2. **Docker Inspect Exposure:** Environment variables are permanently baked into the container's metadata. Anyone with access to the host can run `docker inspect <container_id>` and read your plain-text secrets.

## ✨ The Solution: DSO v3.2

Docker Secret Operator (DSO) permanently solves local secret management by replacing `.env` files with a highly secure, local-first **Native Vault** and **Zero-Persistence Injection**. 

Instead of writing secrets to a file, you store them in your AES-256-GCM encrypted Native Vault (`~/.dso/`). DSO intercepts your `docker compose up` command, decrypts your secrets in memory, and natively injects them into your containers using two intuitive protocols:
* **`dsofile://`** dynamically streams your secret into an isolated RAM disk (`tmpfs`) just milliseconds before your container boots. Your secrets never touch your hard drive and are completely invisible to `docker inspect`.
* **`dso://`** injects secrets securely into environment variables for legacy containers that do not support reading from files.

## 🚀 Quick Start

Get started in seconds without requiring AWS, Azure, or HashiCorp accounts.

**1. Initialize your local Vault**
```bash
docker dso init
```

**2. Securely store your secret**
```bash
docker dso secret set app/db_pass
```

**3. Reference the secret in your `docker-compose.yaml`**
```yaml
services:
  db:
    image: postgres:15
    environment:
      POSTGRES_USER: admin
      POSTGRES_PASSWORD_FILE: dsofile://app/db_pass
```

**4. Deploy!**
```bash
docker dso up -d
```
*That's it! DSO handles the AST resolution, spins up a background agent in your local namespace, creates the RAM disks, and streams the secrets effortlessly.*

---

## ⚙️ Modes of Operation

DSO v3.2 introduces **Dual Mode Execution** to ensure both cutting-edge local development and enterprise cloud backward compatibility.

| Mode | Trigger | Description |
| :--- | :--- | :--- |
| **LOCAL Mode** (Default) | Native fallback | The new standard. Secrets are read from your local Native Vault (`~/.dso/`). Provides full `dsofile://` (tmpfs) RAM injection capabilities. |
| **CLOUD Mode** (Legacy) | `/etc/dso/dso.yaml` | Retained for enterprise backwards compatibility. Reads credentials from AWS Secrets Manager or Azure Key Vault via legacy provider plugins. |

You can explicitly override auto-detection:
```bash
docker dso up -d --mode=local
docker dso up -d --mode=cloud
```

## 🔒 Security Architecture

1. **AES-256-GCM Encryption:** Your local Vault (`~/.dso/vault.enc`) is cryptographically sealed. Even if an attacker steals the vault file, they cannot read it without your Master Key.
2. **Zero-Persistence Files:** Using `dsofile://`, secrets are streamed directly over Unix sockets into isolated `tmpfs` mounts. They are stored in RAM, bypassing the host OS file system entirely.
3. **Inspect Evasion:** Because `dsofile://` avoids standard environment block population, your secrets are 100% invisible to `docker inspect` or host-level process inspection tools.

## 📦 Installation

Run the automated installer:
```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | bash
```

*Requirements: Docker and Go 1.21+*

## 📚 Examples & Integrations

Whether you're running PostgreSQL, Redis, Node.js, or Django, DSO fits your architecture perfectly. 

Check out our [Examples Directory](docs/examples/) for copy-paste architectural guides!

## 🎬 Demo

*(Placeholder for future demo videos)*
* [demo-local.gif]
* [demo-cloud.gif]

---
<p align="center">Built with ❤️ for the open-source DevOps community.</p>
