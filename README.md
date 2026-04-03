# Docker Secret Operator (DSO)

[![Build Status](https://img.shields.io/badge/Build-Passing-brightgreen.svg)]()
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Security Audited](https://img.shields.io/badge/Security-Hardened-orange.svg)](SECURITY.md)

Docker Secret Operator (DSO) is a production-grade orchestration engine designed to securely manage the lifecycle of secrets in non-Kubernetes Docker environments. It bridges the security gap between enterpise secret providers (HashiCorp Vault, AWS, Azure) and standalone Docker 엔진 or Docker Compose stacks.

---

## 1. Project Overview
DSO provides a centralized, event-driven mechanism to fetch, inject, and rotate secrets dynamically. It ensures that sensitive data is handled with minimal exposure, strictly following the principles of least privilege and zero-persistence on host storage via In-Memory Tar Streaming.

## 2. Problem Statement
Securing secrets in standard Docker environments remains a significant "last-mile" challenge in the cloud-native ecosystem:
- **Persistence of `.env` Files**: Traditional environment files are often leaked via process inspection, version control history, or insecure CI/CD artifacts.
- **Static Secret Lifecycle**: Native Docker secrets are immutable by design and do not support dynamic rotation without full container redeployment.
- **Tooling Gap**: Enterprise-grade secret management often requires Kubernetes (e.g., External Secrets Operator), leaving standalone engines and edge devices underserved.

## 3. Solution & Value Proposition
DSO implements a reconciliation pattern for Docker secrets. It monitors the desired state defined in secret providers and ensures the running state of Docker containers matches that definition.
- **In-Memory Tar Streaming**: Secrets are processed entirely in RAM and streamed directly to containers, bypassing the host's physical disk.
- **Automated Lifecycle**: Rotates containers automatically using blue/green (restart) or signal-based strategies.
- **Provider Unification**: Offers a single interface to manage multiple providers concurrently via a unified `dso.yaml` configuration.

## 4. Key Features
- **Dual Injection Modes**:
  - **`env`**: Direct environment variable injection.
  - **`file`**: In-Memory Tar Streaming to `tmpfs` mounts (no env transport).
- **Atomic Rotation with Rollback**: 3-retry idempotent logic to ensure stable recovery during rotation failures.
- **Secure File Permissions**: File-based secrets are injected with `0400` (read-only) permissions and configurable UID/GID ownership.
- **Global Log Redaction**: Automatic masking of sensitive data in all observability streams.
- **Service-Level Concurrency Locking**: Prevents race conditions during simultaneous secret updates.

## Quick Start (2 Minutes)

1. Start HashiCorp Vault in dev mode
2. Create a sample secret
3. Run the provided docker-compose.agent.yml
4. Verify secret injection inside the container

## 5. How It Works
DSO manages the secret lifecycle through an atomic state machine:
1. **Fetch & Cache**: Secret data is retrieved from providers and stored in the **DSO Agent's** volatile RAM cache.
2. **Reconcile**: The **Watcher Engine** detects state differences between the provider and the running containers.
3. **Trigger**: Upon change detection, the **Reloader Controller** executes the rotation strategy.
4. **Rename**: The stable container is renamed to `<service_name>_old_dso` for backup.
5. **Create**: A new container is created in a stopped state.
6. **Inject**: The **Tar Streamer** performs In-Memory Tar Streaming directly to the new container's address space.
7. **Start**: Container begins execution with the new secret state.
8. **Validate**: Post-deployment `ExecProbes` (`test -s`) verify data integrity before removing the backup.

## 6. Architecture Summary
DSO consists of a lightweight Go-based **DSO Agent**, a **Watcher Engine**, and a **Reloader Controller**. These components interact with the Docker Socket and Secret Providers via the **Tar Streamer** to enforce the desired secret state. For detailed diagrams, see [ARCHITECTURE.md](ARCHITECTURE.md).

## 7. Trust Boundaries
DSO assumes a trusted host and Docker daemon. It focuses on mitigating **passive leaks** and **metadata exposure**:
- **Encrypted in Transit**: Communications with providers are TLS-encrypted.
- **Encrypted in RAM**: Secrets are cached in memory and wiped immediately on agent shutdown.
- **In-Memory Tar Streaming**: Direct injection to `tmpfs` avoids leaving plaintext traces on physical storage.
- Detailed analysis can be found in [THREAT_MODEL.md](THREAT_MODEL.md).

## 8. Limitations
- **Docker Socket Access**: DSO requires privileged access to `/var/run/docker.sock` to manage container lifecycles.
- **Trusted Daemon**: Project assumes the underlying Docker daemon and host kernel are not compromised.
- **Container Breakout**: DSO does not protect against an attacker who has already gained root-level breakout access to the host or daemon.
- **Provider Authentication**: The operator depends on the security of its own authentication credentials to the configured secret provider.

## 9. Quick Start
### 1. Install
Download the latest binary from the [Releases](https://github.com/docker-secret-operator/dso/releases) page.

### 2. Configure (`dso.yaml`)
```yaml
provider: vault
config:
  address: "https://vault.example.com:8200"
secrets:
  - name: production/mysql/password
    inject: file
    path: "/run/secrets/db_password"
    uid: 1001
    gid: 1001
```

### 3. Run
```bash
dso up -d
```

## 10. Use Cases
- **Local Development**: Replicate production-like secret injection in local `docker-compose` stacks.
- **Secure CI/CD**: Inject dynamic build-time keys without exposing them in ephemeral runner logs.
- **Edge Computing**: Manage secrets on remote Docker engines at scale without the overhead of Kubernetes.

## Non-Goals

- Not a Kubernetes replacement
- Not a secret storage system
- Not a policy engine (future roadmap)

## 11. Documentation Links
- [Architecture Overview](ARCHITECTURE.md)
- [Security Policy](SECURITY.md)
- [Threat Model](THREAT_MODEL.md)
- [Project Governance](GOVERNANCE.md)
- [Contributing Guide](CONTRIBUTING.md)
- [Code of Conduct](CODE_OF_CONDUCT.md)
- [Configuration Reference](docs/CONFIGURATION.md)
- [Provider Guide](docs/PROVIDERS.md)

---
### License
Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE) for more details.
