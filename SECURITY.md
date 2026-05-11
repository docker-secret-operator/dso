# Security Policy (DSO)

## 1. Security Principles
The Docker Secret Operator (DSO) is built on three core security pillars:
- **Zero-Persistence on Host Storage**: Plaintext secrets are never written to the host's physical filesystem. Intermediate data exists only in volatile memory (RAM) or kernel-managed `tmpfs` mounts.
- **Least Privilege**: Secret files are injected with the minimum required permissions (`0400`) and assigned specific `UID/GID` owners to ensure application-level isolation.
- **Volatile Secret Lifecycle**: Secrets are ephemeral; they are wiped from the host RAM upon provider rotation or agent shutdown.

## 2. Secret Lifecycle

### Local Mode (Development/CI)
Single-shot injection, no daemon:
1. **Load & Decrypt**: Read encrypted `~/.dso/vault.enc`, decrypt with user's master key
2. **Parse Compose**: Load `docker-compose.yaml` from disk
3. **Inject Secrets** (in-memory only):
   - `dso://secret-name` → environment variable injection
   - `dsofile://secret-name` → tmpfs file injection (recommended)
4. **Execute**: Pass resolved compose file to `docker compose up` via stdin (secrets never hit disk)
5. **Container Runtime**: Docker injects secrets into container process
6. **Cleanup**: CLI exits, process memory cleared by OS

### Cloud Mode (Production)
Continuous daemon, provider-based:
1. **Daemon Start**: systemd starts `dso-agent`, connects to Docker socket
2. **Provider Auth**: Load dso.yaml configuration, authenticate with provider (Vault/AWS/Azure)
3. **Watch Events**: Listen for container lifecycle events (`start`, `stop`, `die`)
4. **Fetch & Cache**: On event, fetch secrets from provider over TLS into agent RAM
5. **Checksum Validation**: Compare new secrets against cached version
6. **Conditional Rotation**:
   - If changed: stop old container, start new with updated environment
   - If unchanged: skip (avoid unnecessary restarts)
7. **Health Check**: Wait for container health check to pass (if configured)
8. **Cleanup**: Remove old container; kernel manages tmpfs cleanup

## 3. Security Controls

- **File Permissions**: Injected files default to `0400` (read-only by owner, no group/world access)
- **Identity Injection**: Configurable `UID` and `GID` for file ownership inside container (prevents sibling processes from reading)
- **Log Redaction**: All DSO output passes through redaction engine, masking secrets before stdout/stderr
- **Event Deduplication**: TTL-based cache prevents repeated processing of same event (reduces secret exposure window)
- **Provider Supervision**: Crash detection and health monitoring prevents cascade failures

### Mode-Specific Security Considerations

#### Local Mode
- **Threat Model**: Protect user's local vault from other host users
- **Trust Boundary**: Host kernel (you own the machine)
- **Secret Exposure**: Minimal (CLI memory, short-lived)
- **No Root Required**: User isolation is enforced by OS
- **Recommended**: `dsofile://` for file-based injection (tmpfs)

#### Cloud Mode
- **Threat Model**: Protect agent process from other host users; assume Docker daemon is trusted
- **Trust Boundary**: Host kernel + Docker daemon
- **Secret Exposure**: Longer (cached in agent RAM)
- **Root Required**: systemd daemon requires root privilege
- **Limitation**: `dsofile://` not supported (environment variables only)

### Threat Actors DSO Protects Against
- **Unprivileged host users** (Local Mode: can't read vault or CLI memory; Cloud Mode: can't access agent process)
- **Container inspection tools** (`docker inspect` doesn't see `dsofile://` secrets)
- **Process introspection** (`dso://` env vars are visible; `dsofile://` are not)

## 4. Trust Boundaries

### Docker Daemon (Cloud Mode)
- DSO assumes the Docker daemon is secure, uncompromised, and governed by appropriate access controls
- If Docker daemon is compromised, DSO secrets are compromised (Docker has full host access)
- Recommendation: Use Docker socket ACLs to restrict access to `dso-agent` process

### Host Kernel & RAM
- DSO assumes host kernel is trusted and protects DSO process memory
- In Cloud Mode, agent RAM contains cached secrets; kernel page isolation provides protection
- An attacker with `root` can read DSO agent memory or `docker exec` into any container
- Recommendation: Restrict `sudo` access; use kernel LSM (AppArmor/SELinux) if available

### Provider Backends (Cloud Mode)
- DSO assumes providers (Vault, AWS, Azure) are reachable over TLS and haven't been compromised
- Provider credentials stored in `dso.yaml` on host filesystem (protect like you would protect `/etc/passwd`)
- Recommendation: Use provider IAM roles where available (IAM role on EC2, service account on GKE, etc.)

## 5. Lightweight Threat Model

Understanding the boundaries of DSO's protection is critical for production deployments.

### 5.1 Environment Variable Exposure
**Risk**: Standard environment variables used for secret injection are visible to any user who can run `docker inspect` or read `/proc/<pid>/environ`.
**DSO Control**: DSO supports **File Injection** into `tmpfs` mounts, which ensures secrets never appear in the container's environment metadata.

### 5.2 File Mount & RAM Risks
**Risk**: While DSO prevents secrets from hitting the physical disk, an attacker with **Root/Sudo** access on the host can still inspect volatile memory (RAM) or read the `tmpfs` mount points.
**DSO Control**: DSO minimizes the exposure window by ensuring secrets are only present in RAM during the lifecycle of the container. 

### 5.3 Docker Socket Escape
**Risk**: DSO requires access to `/var/run/docker.sock`. If the DSO container itself is compromised, an attacker could potentially gain full control over the Docker host.
**DSO Control**: We recommend running DSO with `--network none` and `--restart unless-stopped` to minimize the attack surface of the operator itself.

## 6. Recommended Secure Defaults

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

## 7. Explicit Limitations

DSO does **not** protect against:

| Threat | Protection | Mitigation |
|--------|-----------|-----------|
| **Container Compromise** | ❌ None | If attacker runs code in container, they can read injected secrets | Restrict container image sources; scan for malware |
| **Root Access** | ❌ None | `root` can read DSO agent RAM, inspect containers, access dso.yaml | Restrict `sudo` access; use containerized DSO if possible |
| **Docker Socket Exposure** | ❌ None | If `/var/run/docker.sock` is readable by untrusted user, they can bypass DSO | Restrict socket permissions (default: `root:docker`) |
| **Insecure Provider** | ❌ None | If provider backend is compromised, DSO can't help | Use TLS; validate provider certificates; use provider ACLs |
| **Malicious dso.yaml** | ❌ None | Misconfigured dso.yaml could route secrets to wrong containers | Restrict who can modify dso.yaml (root-only) |
| **Memory Scraping** | ⚠️ Limited | Privileged attacker can dump agent RAM on Cloud Mode | Use hardware security module (HSM) for key wrapping (future) |

---

## 8. Responsible Disclosure

We take security seriously. If you find a vulnerability, please report it responsibly:

- **Do NOT** create a public GitHub issue
- **Email**: security@docker-secret-operator.io (to be confirmed)
- **Follow**: Please allow 48 hours for acknowledgment and ~14 days for a fix before public disclosure

See [SECURITY.md](./SECURITY.md) for current contact information.
