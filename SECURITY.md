# Security Policy (DSO)

## 1. Security Principles
The Docker Secret Operator (DSO) is built on three core security pillars:
- **Zero-Persistence on Host Storage**: Plaintext secrets are never written to the host's physical filesystem. Intermediate data exists only in volatile memory (RAM) or kernel-managed `tmpfs` mounts.
- **Least Privilege**: Secret files are injected with the minimum required permissions (`0400`) and assigned specific `UID/GID` owners to ensure application-level isolation.
- **Volatile Secret Lifecycle**: Secrets are ephemeral; they are wiped from the host RAM upon provider rotation or agent shutdown.

## 2. Secret Lifecycle
DSO manages the transition of sensitive data through the following security stages:
1. **Fetch & Cache**: The **DSO Agent** retrieves secrets from an external provider (e.g., HashiCorp Vault) over a TLS-encrypted connection directly into its process RAM cache.
2. **Orchestration**: The **Reloader Controller** renames the target container to `<service_name>_old_dso` for backup and creates a new instance in a stopped state.
3. **In-Memory Tar Streaming**: Secrets are archived into an in-memory buffer and streamed by the **Tar Streamer** to the container's `tmpfs` mount via the Docker Engine API.
4. **Validation**: The container starts, and DSO performs `ExecProbes` to verify secret availability before finalizing the rotation.
5. **Cleanup**: Upon success, the old container and its associated `tmpfs` are destroyed by the kernel, and the **DSO Agent** clears any ephemeral rotation state.

## 3. Security Controls
- **File Permissions**: Injected files default to `0400` (read-only by owner).
- **Identity Injection**: Supports configurable `UID` and `GID` for file ownership inside the container.
- **Log Redaction**: All DSO output is passed through a global redaction utility that masks secret values before they hit `stdout`, `stderr`, or external observability stacks.

### Threat Actors
- **Unprivileged host users**
- **Compromised containers**
- **Malicious sidecar processes**

## 4. Trust Boundaries
- **Trusted Docker Daemon**: DSO assumes the Docker Engine is running a secure, uncompromised version and is governed by appropriate access controls.
- **Secure Host Environment**: The operator assumes the host kernel, RAM, and DSO Agent process space are protected from unauthorized inspection or memory scraping.

## 5. Explicit Limitations
DSO does **not** protect against the following scenarios:
- **Container Compromise**: If an attacker gains code execution within a target container, they can read any secrets injected into that specific container.
- **Root-Level Host Access**: An attacker with root privileges on the host can inspect the DSO process memory or `docker exec` into any container.
- **Docker Socket exposure**: If the `/var/run/docker.sock` is exposed to untrusted users, those users can bypass DSO and manually inspect container configurations.

## 6. Responsible Disclosure
We take security seriously. If you find a vulnerability, please do NOT create a public issue. Instead, report it to the maintainers via:
- **Email**: [security@docker-secret-operator.io](mailto:security@docker-secret-operator.io) (Placeholder)
- **GPG**: [Key ID: 0xREDACTED] (Placeholder)

We aim to acknowledge reports within 48 hours and provide a fix within 14 days.
