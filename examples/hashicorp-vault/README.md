# HashiCorp Vault + MySQL + DSO

This example demonstrates how to use **Docker Secret Operator (DSO)** to securely inject MySQL credentials from **HashiCorp Vault** into a containerized application, eliminating the need for insecure `.env` files.

---

## 1. Introduction

### What is HashiCorp Vault?
[HashiCorp Vault](https://www.vaultproject.io/) is an identity-based secrets and encryption management system. It provides a central place to store and manage sensitive information such as API keys, passwords, and certificates.

### Why `.env` files are insecure
- **Plain Text**: Secrets are stored in human-readable format on disk.
- **Git Risks**: Accidentally committing `.env` files exposes secrets to the entire version control history.
- **Leaked Logs**: Simple environment variables are often leaked in application logs or crash dumps.
- **Static**: Hard to rotate and manage across multiple environments.

### What DSO does
The **Docker Secret Operator (DSO)** intercepts the container startup process to inject secrets directly from secure providers like Vault. Secrets are only present in memory at runtime and never stored in your repository or as plain text on the host disk.

---

## 2. Architecture Overview

```text
[ Developer ] --(1) Store Secret--> [ HashiCorp Vault ]
                                          |
[ Docker CLI ] --(2) docker dso up --> [ DSO Agent ]
                                          |
                                    (3) Fetch Secret
                                          |
[ App Container ] <--(4) Inject ENV -- [ DSO Agent ]
```

1. **Store**: Secrets are stored encrypted in Vault's KV engine.
2. **Intercept**: DSO intercepts the Docker Compose command.
3. **Fetch**: DSO Agent authenticates and retrieves secrets into memory.
4. **Inject**: Secrets are injected as environment variables into the container environment at the moment of creation.

---

## 3. Prerequisites

- **Docker** installed and running.
- **DSO CLI** installed (`curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | bash`).
- **Make** (optional, for the automated Quick Start).

---

## 4. Project Structure

```text
examples/hashicorp-vault/
├── README.md              # Documentation (this file)
├── Makefile               # Task automation
├── dso.yaml               # DSO provider configuration
├── docker-compose.yaml    # Application stack
└── .env.example           # Insecure baseline (for demonstration)
```

---

## 5. Quick Start (Recommended)

If you have `make` installed, you can set up the entire environment in seconds:

```bash
# 1. Start Vault in dev mode
make up

# 2. Configure the KV secrets engine and add MySQL credentials
make setup-vault

# 3. Deploy the MySQL stack with DSO injection
docker dso up -d
```

---

## 6. Manual Setup: Start Vault Locally (Dev Mode)

If you prefer to run commands manually, start the Vault container:

```bash
docker run -d \
  --name vault \
  -p 8200:8200 \
  -e VAULT_DEV_ROOT_TOKEN_ID=root \
  hashicorp/vault
```

### Expected Output
```text
[+] Running 1/1
 ⠿ Container vault  Started
```

> [!CAUTION]
> **Security Warning**: Vault "Dev Mode" is for development only. It stores data in memory (lost on restart) and uses a non-production root token. **Never use `VAULT_DEV_ROOT_TOKEN_ID=root` in production.**

### Vault Health Check
Confirm Vault is up and unsealed:
```bash
curl http://127.0.0.1:8200/v1/sys/health
```

### Expected Output
```json
{"initialized":true,"sealed":false,"standby":false,"performance_standby":false,"replication_performance_cluster_id":"","replication_dr_cluster_id":"","server_time_utc":1712132400,"version":"1.15.0"}
```

---

## 7. Configure Vault

We use `docker exec` to configure Vault internally, avoiding the need for a local Vault CLI installation.

### 7.1 Enable KV Secrets Engine
Vault needs the Key-Value (KV) engine enabled to store our database credentials.

```bash
docker exec vault sh -c "vault secrets enable -path=secret kv-v2"
```

### Expected Output
```text
Success! Enabled the kv-v2 secrets engine at: secret/
```

### 7.2 Store MySQL Credentials
Now, we store our sensitive database information inside Vault under the path `secret/mysql`.

```bash
docker exec vault sh -c "vault kv put secret/mysql \
  username='root' \
  password='mypassword' \
  host='localhost' \
  port='3306'"
```

### Expected Output
```text
Success! Data written to: secret/data/mysql
```

---

## 8. Run MySQL Standalone (Optional Verification)

To verify MySQL works independently, you can run it with manual environment variables:

```bash
docker run -d \
  --name mysql-demo \
  -e MYSQL_ROOT_PASSWORD=mypassword \
  -p 3306:3306 \
  mysql:8
```

### Expected Output
```text
[+] Running 1/1
 ⠿ Container mysql-demo  Started
```

---

## 9. Traditional Approach (The Problem with .env)

In a traditional setup, you might create a `.env` file like this:

**`.env.example`**
```env
DB_USER=root
DB_PASS=mypassword
DB_HOST=localhost
DB_PORT=3306
```

> [!WARNING]
> **Git Protection**: If you use `.env` files, **ALWAYS** add them to your `.gitignore`.
> ```text
> # .gitignore
> .env
> ```

### Why this approach is insecure:
1. **Plaintext**: Anyone with access to the machine can read your password.
2. **Git Exposure**: If you forget to add `.env` to `.gitignore`, your credentials are leaked to your repository forever.

---

## 10. How DSO Works (Under the Hood)

When you run `docker dso up`, the following happens:

1. **Config Read**: DSO reads `dso.yaml` to identify the provider (Vault).
2. **Fetch**: DSO connects to Vault using the provided token and fetches the secret at `mysql`.
3. **Map**: DSO maps the Vault fields (e.g., `username`) to the requested environment variable names (e.g., `DB_USER`).
4. **Inject**: DSO injects these variables into the current shell process.
5. **Exec**: DSO calls `docker compose up`, allowing Docker to see the variables as if they were set manually.

---

## 11. Configure DSO Vault Provider (`dso.yaml`)

```yaml
# DSO Example: HashiCorp Vault (V3.2)
providers:
  dev-vault:
    type: vault
    address: http://host.docker.internal:8200
    token: root
    mount: secret

defaults:
  inject:
    type: env

secrets:
  - name: mysql
    provider: dev-vault
    mappings:
      username: DB_USER
      password: DB_PASS
```

### Linux Users Notice
> [!WARNING]
> On some Linux distributions, `host.docker.internal` may not resolve. If you encounter connection errors, edit `dso.yaml` and replace the address with:
> `address: http://172.17.0.1:8200`

---

## 12. Run with DSO

Execute the deployment using the `docker dso` wrapper:

```bash
docker dso up -d
```

### Expected Output
```text
DSO: Authenticating with Vault...
DSO: Successfully fetched secret 'mysql'
DSO: Injecting secrets into environment...
[+] Running 1/1
 ⠿ Container mysql-vault-demo  Started
```

---

## 13. Verify Secrets Inside Container

```bash
docker exec -it mysql-vault-demo env | grep DB_
```

### Expected Output
```text
DB_USER=root
DB_PASS=mypassword
```

---

## 14. Troubleshooting

### Vault not reachable
- **Error**: `DSO: Failed to connect to Vault at http://host.docker.internal:8200`
- **Fix**: Verify the Vault container is running (`docker ps`). For Linux users, try replacing `host.docker.internal` with `172.17.0.1`.

### Secrets not injected
- **Error**: `DB_USER` is empty inside the container.
- **Fix**: Ensure the variable names in `dso.yaml` (right side of mappings) exactly match the names in `docker-compose.yaml`.

### Permission Denied
- **Error**: `vault: Permission denied`
- **Fix**: Ensure the `token` in `dso.yaml` is correct for your Vault instance. For this demo, it must be `root`.

---

## 15. Why DSO is Better than .env

| Feature | `.env` File | DSO + Vault |
| :--- | :--- | :--- |
| **Secret Storage** | Plain text on host disk | Encrypted in Vault |
| **Encryption** | None | AES-256 (Vault Standard) |
| **Git Safety** | High risk of accidental commit | Zero risk (secrets never in repo) |
| **Secret Rotation** | Manual update | Automated rotation support |
| **Production Readiness** | Low (PoC only) | High (Zero-Trust Compliant) |

---

## 16. Cleanup

```bash
make down
```

---

## 17. Conclusion

By integrating **HashiCorp Vault** with **DSO**, you have replaced static, insecure configuration files with a dynamic, identity-based secrets management workflow. 

> [!CAUTION]
> **Production Reminder**: For production environments, always use a dedicated Vault Service Account (AppRole or Kubernetes Auth) instead of a root token, and ensure Vault is configured with high availability and a secure storage backend.

---
