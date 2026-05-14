# Security Model & Guarantees

DSO is designed around a single principle: **secrets must never touch disk in plaintext**. This document explains the security properties of DSO, the trust boundaries, and the guarantees we provide.

---

## Core Security Principles

DSO is built on three security pillars:

1. **Zero-Persistence on Host Storage** — Plaintext secrets are never written to the host's physical filesystem. Intermediate data exists only in volatile memory (RAM) or kernel-managed `tmpfs` mounts.

2. **Least Privilege** — Secret files are injected with minimum required permissions (`0400` read-only) and assigned specific `UID/GID` owners to ensure application-level isolation.

3. **Volatile Secret Lifecycle** — Secrets are ephemeral; they are wiped from the host RAM upon provider rotation or agent shutdown.

---

## The Problem with `.env` Files

Most teams running Docker Compose face three concrete risks:

1. **Git Leaks**: `.env` files get committed. Once in git history, rotation is the only safe recovery.
2. **Process Exposure**: Any local process can read an unencrypted file.
3. **`docker inspect` Exposure**: Docker stores environment variables in its metadata layer. Anyone with Docker socket access can run `docker inspect <container>` and read your passwords in plain text — no breach required.

All three share the same root cause: **secrets are plaintext on disk before they reach the container**.

DSO solves all three by keeping secrets only in memory and injecting them at container startup.

---

## Secret Lifecycle

### Local Mode (Development/CI)

Single-shot injection, no daemon:

1. **Load & Decrypt** — Read encrypted `~/.dso/vault.enc`, decrypt with user's master key (Argon2id)
2. **Parse Compose** — Load `docker-compose.yaml` from disk
3. **Inject Secrets** (in-memory only):
   - `dso://secret-name` → environment variable injection
   - `dsofile://secret-name` → tmpfs file injection (recommended)
4. **Execute** — Pass resolved compose file to `docker compose up` via stdin (secrets never hit disk)
5. **Container Runtime** — Docker injects secrets into container process
6. **Cleanup** — CLI exits, process memory cleared by OS

**Secrets never touch host disk.**

### Cloud Mode (Production)

Continuous daemon with provider-based rotation:

1. **Daemon Start** — systemd starts `dso-agent`, connects to Docker socket
2. **Provider Auth** — Load dso.yaml configuration, authenticate with provider (Vault/AWS/Azure)
3. **Watch Events** — Listen for container lifecycle events (`start`, `stop`, `die`)
4. **Fetch & Cache** — On event, fetch secrets from provider over TLS into agent RAM
5. **Checksum Validation** — Compare new secrets against cached version
6. **Conditional Rotation**:
   - If changed: stop old container, start new with updated secret
   - If unchanged: skip (avoid unnecessary restarts)
7. **Health Check** — Wait for container health check to pass (if configured)
8. **Cleanup** — Remove old container; kernel manages tmpfs cleanup

**Secrets are cached in agent RAM; never written to disk.**

---

## Security Controls

### Encryption

**Local Mode**: Secrets stored in `~/.dso/vault.enc` using AES-256-GCM authenticated encryption. The encryption key is derived from a machine-specific master key via **Argon2id** (128 MB memory, 3 iterations), making offline brute-force attacks computationally infeasible.

**Cloud Mode**: Secrets never stored locally. Fetched from provider via TLS, held in agent RAM only.

### File Injection (Recommended for Production)

When you use `dsofile://`, DSO mounts a `tmpfs` RAM disk at `/run/secrets/dso/` inside the container and streams the secret via an in-memory tar archive using the Docker API.

- Never writes to host disk (SSD or HDD)
- Disappears if the container stops or the machine reboots
- **Invisible to `docker inspect`**
- File permissions: `0400` (read-only by owner, no group/world access)

### Environment Variable Injection

When you use `dso://`, secrets are injected as environment variables. **These are visible to `docker inspect`.** Only use for non-sensitive configuration data or when your application requires environment variable injection.

### Log Redaction

All DSO output passes through a redaction engine, masking:
- API keys (`api_key=`, `sk-*`)
- Tokens (`token=`, `authorization:`)
- Passwords and secrets
- Provider credentials

Secrets will not appear in logs, even when operations fail.

### Event Deduplication

TTL-based cache prevents repeated processing of same event, reducing the exposure window for secrets in agent RAM.

### Provider Supervision

Crash detection and health monitoring prevents cascade failures. v3.5 includes circuit breaker pattern for provider failure isolation.

### Automatic Recovery (v3.5+)

- Detects incomplete rotations on startup
- Cleans up orphaned containers automatically
- Validates original container state before recovery
- Handles critical errors with operator alerts

---

## Trust Boundaries

### Docker Daemon (Cloud Mode)

DSO assumes the Docker daemon is secure, uncompromised, and governed by appropriate access controls.

- **If Docker is compromised**: DSO secrets are compromised (Docker has full host access)
- **Recommendation**: Use Docker socket ACLs to restrict access to `dso-agent` process

### Host Kernel & RAM

DSO assumes host kernel is trusted and protects DSO process memory.

- **In Cloud Mode**: Agent RAM contains cached secrets; kernel page isolation provides protection
- **An attacker with `root`**: Can read DSO agent memory or `docker exec` into any container
- **Recommendation**: Restrict `sudo` access; use kernel LSM (AppArmor/SELinux) if available

### Provider Backends (Cloud Mode)

DSO assumes providers (Vault, AWS, Azure) are reachable over TLS and haven't been compromised.

- **Provider credentials**: Stored in `dso.yaml` on host filesystem (protect like `/etc/passwd`)
- **Recommendation**: Use provider IAM roles where available (IAM role on EC2, managed identity on Azure, Kubernetes service account for Vault)

---

## Security Guarantees (v3.5+)

DSO provides explicit security properties for production deployments:

### 1. No Secrets in Logs

**Guarantee**: Secrets, tokens, API keys, and credentials do NOT appear in DSO logs, even when operations fail.

**Scope**: Provider initialization errors, daemon reconnection failures, RPC errors, timeout context, nested error chains, stack traces.

**Validation**: All error logging passes through redaction engine. Patterns detected and redacted: API keys, tokens, Bearer auth, provider credentials.

### 2. No Operation Replay

**Guarantee**: Event deduplication prevents duplicate application of operations.

**Validation**: TTL-based cache ensures each secret change is processed once per deduplication window.

### 3. No Cascade Failures (v3.5+)

**Guarantee**: Provider failure doesn't cascade to other providers.

**Implementation**: Circuit breaker pattern isolates provider failures. States: Closed (normal), Open (rejecting), Half-open (testing). Auto-recovery with configurable thresholds.

**Validation**: Circuit breaker status visible in `docker dso status`.

### 4. No Memory Leaks

**Guarantee**: Long-duration deployments don't exhibit unbounded memory growth.

**Validation**: Agent memory usage stabilizes after initial setup. Goroutine leaks detected in race-condition testing.

### 5. No Goroutine Leaks

**Guarantee**: Goroutines are properly cleaned up on shutdown.

**Validation**: Resource stability testing validates goroutine cleanup. No unbounded growth over time.

### 6. Automatic Recovery from Incomplete Rotations (v3.5+)

**Guarantee**: Agent crashes during rotation do not leave containers in inconsistent state.

**Implementation**:
- On startup: Detects pending rotations older than 5 minutes
- Auto-rollback: Cleans up orphaned containers using naming patterns (`_dso_backup_`, `_dso_new_`)
- Validation: Original container verified running after recovery
- Fallback: Critical errors marked for operator review

**Validation**: Recovery procedures documented. Manual recovery available for edge cases.

---

## Recommended Secure Defaults

### Local Mode

```yaml
# docker-compose.yaml
services:
  db:
    image: postgres:15
    environment:
      # RECOMMENDED: Use dsofile:// for file-based injection
      POSTGRES_PASSWORD_FILE: dsofile://db/password
      # NOT RECOMMENDED: dso:// is visible to docker inspect
      # POSTGRES_PASSWORD: dso://db/password
```

Run with: `docker dso up -d`

### Cloud Mode

```yaml
# /etc/dso/dso.yaml
providers:
  vault:
    type: vault
    # Use IAM roles instead of hardcoded credentials where possible
    auth:
      method: iam_role  # On AWS EC2, uses instance IAM role

rotation:
  strategy: restart        # Gracefully stop/start containers
  grace_period: 30s

logging:
  level: info              # Prevents secret leaks in debug traces
```

---

## Limitations & What DSO Does NOT Protect Against

DSO does **not** provide protection against these threats:

| Threat | Protection | Mitigation |
|--------|-----------|-----------|
| **Container Compromise** | ❌ None | If attacker runs code in container, they can read injected secrets | Restrict container image sources; scan for malware |
| **Root Access** | ❌ None | `root` can read DSO agent RAM, inspect containers, access dso.yaml | Restrict `sudo` access; use containerized DSO if possible |
| **Docker Socket Exposure** | ❌ None | If `/var/run/docker.sock` is readable by untrusted user, they can bypass DSO | Restrict socket permissions (default: `root:docker`) |
| **Insecure Provider** | ❌ None | If provider backend is compromised, DSO can't help | Use TLS; validate provider certificates; use provider ACLs |
| **Misconfigured dso.yaml** | ❌ None | Wrong routing of secrets to containers | Restrict who can modify dso.yaml (root-only) |
| **Memory Scraping** | ⚠️ Limited | Privileged attacker can dump agent RAM on Cloud Mode | Use hardware security module (HSM) for key wrapping (future) |

---

## Comparison: `.env` vs `dso://` vs `dsofile://`

| Feature | `.env` files | `dso://` | `dsofile://` |
|---------|------------|---------|--------------|
| Stored encrypted at rest | ❌ No | ✅ Yes (local vault) | ✅ Yes (local vault) |
| Visible to `docker inspect` | ✅ Yes (exposed) | ✅ Yes (env vars) | ❌ No (tmpfs) |
| Written to host disk at runtime | ✅ Yes (risk) | ❌ No | ❌ No |
| Git leak risk | ✅ High | ❌ None | ❌ None |
| Survives container restart | ✅ Yes | ✅ Yes (re-injected) | ✅ Yes (re-injected) |
| Requires host filesystem | ✅ Yes | ❌ No | ❌ No |

**Recommendation**: Always prefer `dsofile://` for production workloads.

---

## Responsible Disclosure

We take security seriously. If you find a vulnerability, please report it responsibly:

- **Do NOT** create a public GitHub issue
- **Email**: security@docker-secret-operator.org
- **Follow**: Please allow 48 hours for acknowledgment and ~14 days for a fix before public disclosure

---

## See Also

- [THREAT_MODEL.md](THREAT_MODEL.md) — Detailed threat analysis
- [docs/PERSISTENCE_MODEL.md](docs/PERSISTENCE_MODEL.md) — What data DSO persists and where
- [docs/RECOVERY_PROCEDURES.md](docs/RECOVERY_PROCEDURES.md) — Recovery from failures
