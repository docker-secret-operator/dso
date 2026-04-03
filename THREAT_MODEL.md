# DSO Threat Model

## 1. Scope and Trust Boundaries
DSO (Docker Secret Operator) operates as a privileged agent on a Docker host. The trust boundaries are:

- **Boundary A: DSO Agent & Provider APIs**: DSO communicates with external secret providers (e.g., Vault, AWS).
- **Boundary B: Docker Daemon**: DSO interacts with the Docker Engine to manage container lifecycles.
- **Boundary C: In-Memory Secret Cache**: DSO stores secrets in its private RAM at runtime.
- **Boundary D: Container Environment/Volumes**: Target containers receive secrets via `env` or `tmpfs`.

## 2. Attacker Capabilities & Scenarios

### Attacker A: Compromised Target Container
- **Vector**: An application process is compromised.
- **Capability**: Attacker can read anything in the container's RAM or mounted volumes (`env` or `/run/secrets`).
- **DSO Protection**: DSO minimizes the blast radius by using `0400` read-only permissions for secret files. It does *not* protect against a compromised process reading its *own* secrets.

### Attacker B: Host-Level Access (Non-Privileged)
- **Vector**: Attacker has shells access to the host machine.
- **Capability**: Attacker can inspect the filesystem and environment of processes.
- **DSO Protection**: DSO uses **Zero-Disk** logic. Secrets never exist in plaintext on the host filesystem. Even if the attacker inspects the host's `/proc` or `/tmp` directories, they will not find DSO secrets.

### Attacker C: Docker Socket Access (Privileged)
- **Vector**: Attacker has access to the Docker Socket (`/var/run/docker.sock`).
- **Capability**: Attacker can inspect any container's configuration and variables.
- **DSO Protection**: DSO supports **file-based injection** (via `tmpfs`) over **env-based injection**. This ensures that secrets are *not* visible via `docker inspect`. However, if an attacker has full Docker socket access, they can still `docker exec` into target containers. DSO focuses on mitigating *passive* leaks in logs and metadata.

## 3. Assumptions and Dependencies
- **Docker Daemon Security**: DSO assumes the Docker daemon is correctly configured and not compromised.
- **Host Memory Integrity**: DSO assumes the host's RAM is not readable by malicious actors (e.g., no memory scraping).

## 4. Guarantees vs. Limitations

### What DSO Protects Against
- [x] Accidental secret leakage in Docker config metadata (`docker inspect`).
- [x] Accidental secret leakage in logs (via redaction).
- [x] Residual secret data on the host disk after rotation (via `tmpfs`).

### What DSO Does NOT Protect Against
- [!] Compromise of the DSO Agent's own authentication tokens (to providers).
- [!] Host-level root compromise where the attacker can scrape RAM.
- [!] Misconfigurations in the secret provider's access policies.
