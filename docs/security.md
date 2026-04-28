# Security Model

DSO is designed around a single principle: **secrets must never touch disk in plaintext**. This applies to both Local and Cloud Mode.

---

## The Problem with `.env` Files

1. **Git leaks:** `.env` files get committed. Once in git history, rotation is the only safe recovery.
2. **Process exposure:** Any local process can read an unencrypted file.
3. **`docker inspect` exposure:** Docker stores environment variables in its metadata layer. Anyone with Docker socket access can run `docker inspect <container>` and read your passwords in plain text — no breach required.

All three problems share the same root cause: secrets are plaintext on disk before they reach the container.

---

## Local Mode Security

### Encrypted Vault (AES-256-GCM)

Secrets are stored in `~/.dso/vault.enc` using AES-256-GCM authenticated encryption. The encryption key is derived from a machine-specific master key via **Argon2id** (128 MB memory, 3 iterations), making offline brute-force attacks computationally infeasible.

The vault is **user-owned** — `docker dso init` refuses to run as root to prevent root-owned vault files that a non-root user cannot decrypt.

### Zero-Persistence File Injection (`tmpfs`)

When you use `dsofile://`, DSO mounts a `tmpfs` RAM disk at `/run/secrets/dso/` inside the container and streams the secret via an in-memory tar archive using the Docker API. The secret:

- Never writes to host disk (SSD or HDD)
- Disappears if the container stops or the machine reboots
- Is invisible to `docker inspect`

### `docker inspect` Evasion

`dsofile://` secrets are injected post-creation as isolated files inside the container. The compose file seen by Docker only contains the path (`POSTGRES_PASSWORD_FILE=/run/secrets/dso/db_password`) — not the value.

`dso://` secrets **are** visible to `docker inspect` since they are injected as environment variables.

### In-Memory-Only Secret Lifetime

The DSO Local Mode agent only holds secrets in RAM for the duration of the deploy operation. The agent receives an `AgentSeed` — a deduplicated hash map of exactly the secrets required for the active compose file. Inactive secrets remain encrypted in the vault.

---

## Cloud Mode Security

### No Local Storage

In Cloud Mode, secrets are **never stored on the machine running DSO**. They are fetched on demand from the configured external provider (HashiCorp Vault, AWS, Azure, etc.) via an encrypted transport, used in-memory to resolve the compose AST, and then discarded.

### Plugin Integrity Verification

Provider plugins are downloaded with SHA256 checksum validation during `sudo docker dso system setup`. The installer aborts if the checksum does not match. Plugin binaries must reside in allowed system paths (`/usr/local/lib/dso/plugins/`) and cannot be symlinks — validated at load time to prevent path hijacking.

### Systemd Process Isolation

The `dso-agent` runs as a systemd service with `RuntimeDirectory=dso`, providing an OS-managed runtime directory isolated from regular user processes.

### Principle of Least Privilege

Configure your cloud provider credentials with read-only access scoped to only the specific secrets DSO requires. See the [Providers Guide](providers.md) for IAM best practices.

---

## Security Comparison

| Feature | `.env` files | `dso://` | `dsofile://` |
|---|---|---|---|
| Stored encrypted at rest | ❌ No | ✅ Yes (vault) | ✅ Yes (vault) |
| Visible to `docker inspect` | ✅ Yes | ✅ Yes | ❌ No |
| Written to host disk at runtime | ✅ Yes | ❌ No | ❌ No |
| Git leak risk | ✅ High | ❌ None | ❌ None |
| Survives container restart | ✅ Yes | ✅ Yes (re-injected) | ✅ Yes (re-injected) |

---

## Recommendations

1. **Always prefer `dsofile://`** over `dso://` for production workloads. Most modern Docker images support `_FILE` env var suffixes.
2. **Never commit `dso.yaml`** if it contains inline credentials. Use environment variable references (`${VAULT_TOKEN}`).
3. **Delete `.env` files** after importing with `docker dso env import`. DSO warns you to do this on every import.
4. **Run `docker dso system doctor`** after installation to verify plugin integrity and service health.
