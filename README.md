# Docker Secret Operator (DSO) 🚀

> **DSO brings Kubernetes-style secret management to Docker environments.**

[![Build Status](https://img.shields.io/badge/Build-Passing-brightgreen.svg)]()
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Security Audited](https://img.shields.io/badge/Security-Hardened-orange.svg)](SECURITY.md)

**Docker Secret Operator (DSO)** is a production-grade orchestration engine designed to securely manage the lifecycle of secrets in standalone Docker and Docker Compose environments. **DSO is a Docker CLI plugin — no Kubernetes required.**

DSO bridges the gap between enterprise secret providers (AWS, Vault, Azure) and your running containers by providing **In-Memory Tar Streaming**, **Deterministic Targeting**, and **Automated Rotation**.

---

## 🎯 Who is this for?

- 🐳 **Docker-based Microservices**: Teams running traditional Docker or Docker Compose who need enterprise-grade secret security.
- 🏗️ **Non-Kubernetes Environments**: Edge devices, CI/CD runners, or small-to-medium stacks where K8s is overkill.
- 🛡️ **Lightweight Secret Management**: Users who want the security of Vault or AWS Secrets Manager without the complexity of a full service mesh.

---

## 💡 Real-World Use Cases

- **API Security**: Securely inject Database credentials into your API containers directly from AWS Secrets Manager—secrets never touch the host disk.
- **Safe Rotation**: Automatically rotate API keys across 50+ containers without a single manual restart or container image rebuild.

---

## 🔥 Key Features (V3.1)

- 📡 **Multi-Provider Support**: Manage AWS, HashiCorp Vault, Azure, and Local Files simultaneously via a unified `providers` map.
- 🔄 **Smart Checksum Rotation**: Containers are only restarted if the secret value has actually changed, reducing unnecessary downtime.
- 🛡️ **Zero-Persistence Injection**: Secrets are streamed directly into container RAM (via `tmpfs`) without ever touching the host's physical disk.
- 🎯 **Deterministic Targeting**: Precisely control which containers receive specific secrets using explicit service names or label selectors.
- 📈 **Production-Grade Reliability**: Built-in exponential backoff with jitter for provider API calls and atomic rollbacks for failed rotations.

> **Note**: DSO V3.1 is production-ready and actively maintained.

---

## 📋 Prerequisites

Before running the Docker Secret Operator, ensure you have:
- **Docker Engine**: 20.10+ installed and running.
- **Socket Access**: Permission to read/write the Docker socket (`/var/run/docker.sock`).
- **Go 1.25+**: (Optional) Only required for building from source.

---

## ⚡ Quick Start (5 Minutes)

### Step 1: Install DSO (Docker CLI Plugin)
DSO is now a native Docker CLI plugin. Use our installer for your platform:

**Linux / macOS / WSL:**
```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | bash
```

**Windows (PowerShell):**
```powershell
irm https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.ps1 | iex
```

### Step 2: Verify Installation
```bash
docker dso version
```
*If `docker dso` is not recognized, ensure the plugin is in `~/.docker/cli-plugins` and restart your terminal.*

### Step 3: Minimal "First Run" Flow
DSO follows a safe, two-step "Validate -> Up" flow. Use the `-c` flag for your DSO config and everything else is forwarded to the Docker CLI.

1.  **Validate**: Ensure your providers are reachable and config is correct.
    ```bash
    docker dso validate -c examples/dso-minimal.yaml
    ```
2.  **Deploy**: Spawns your stack with dynamically injected secrets.
    ```bash
    docker dso up -c examples/dso-minimal.yaml -f docker-compose.yml -d
    ```
    *Note: `docker dso up` automatically ensures the DSO Agent is running in the background.*

### Step 4: Verify
Confirm successful injection with checkmark logs:
```text
✔ provider 'local' initialized
✔ secrets synced (app-key)
✔ container 'my-app' updated
```

---

## 🧰 Command Overview

DSO follows a strict Docker-native CLI design.

- `docker dso up`     → **User Command**: Deploys stacks and auto-starts the agent.
- `docker dso agent`  → **Runtime Command**: Runs the background reconciliation engine (used by systemd/Docker).
- `docker dso version` → **Verification**: Returns plugin metadata and agent status.

### Commands
- `docker dso up`: Deploy a stack and inject secrets from defined providers.
  ```bash
  docker dso up -c dso.yaml -f docker-compose.yml -d
  ```
  *Flag Forwarding: All standard Docker Compose flags (like `-d`, `--build`, `--scale`) are supported and forwarded directly.*
- `docker dso down`: Safely stop and remove containers managed by DSO.
  ```bash
  docker dso down -f docker-compose.yml
  ```
  *Scope: DSO down exclusively targets containers labeled with `dso.managed=true`. It provides a surgical teardown of operator-controlled services without affecting any other containers in your Docker environment.*
- `docker dso validate`: Strictly validate your `dso.yaml` schema and test provider connectivity.
- `docker dso watch`: Monitor live container events and rotation logs in real-time.
- `docker dso version`: Display the current version of the DSO Agent and CLI plugin metadata.
- `docker dso logs`: View DSO Agent logs directly from the terminal.
  ```bash
  docker dso logs              # Show last 100 log lines
  docker dso logs -f           # Follow logs in real-time (like tail -f)
  docker dso logs -n 50       # Show last 50 lines
  docker dso logs --since "10 minutes ago"  # Time-filtered logs
  docker dso logs --level error             # Show only errors
  docker dso logs --api        # Stream from REST API instead of journald
  ```

---

## 📦 Examples Guide

Explore our [Example Library](examples/) for various use cases:

| File | Environment | When to use |
| :--- | :--- | :--- |
| **[dso-minimal.yaml](examples/dso-minimal.yaml)** | Local Development | Rapid testing or offline development. |
| **[dso-aws.yaml](examples/dso-aws.yaml)** | Production (AWS) | Cloud-native environments using IAM. |
| **[dso-local.yaml](examples/dso-local.yaml)** | Air-gapped / Local | Secrets-from-files or local ENVs. |
| **[dso-v2.yaml](examples/dso-v2.yaml)** | Multi-Cloud | Production reference for complex, hybrid stacks. |

---

## 🔍 Observability & Debugging

DSO provides granular observability to track every secret's lifecycle.

### Successful Operations
```text
✔ 🚀 Executing Zero-Downtime Rolling Rotation  {"id": "api-srv-1"}
✔ provider 'vault-prod' synced successfully
```

### Typical Failures
```text
✖ retry attempt 2/3...                       {"provider": "aws-sm"}
✖ provider connection failed                 {"error": "timeout: connection reset"}
⚠ secret unchanged, skipping rotation        {"name": "db-pass"}
```

### How to Debug
- **Log Level**: Use `--log-level=debug` for deep internal traces.
- **Dry-run**: Use `docker dso validate` to check if your providers are reachable before deploying.
- **Watch mode**: Keep `docker dso watch` open in a separate terminal to see real-time rotation events.

### Debug Mode
When troubleshooting complex provider issues, enable verbose tracing in your `dso.yaml`:
```yaml
logging:
  level: debug
```
*Use this mode only in development or during active troubleshooting, as it may output internal metadata and increase log volume significantly.*

---

## 🔐 Security Hardening

### Best Practices
- **Use File Injection**: For high-value secrets, use `inject: {type: file}` to mount secrets into a `tmpfs` (RAM-only) volume.
- **Restrict Permissions**: Always define `uid` and `gid` in your config to match the application user inside the container.
- **Rotation Frequency**: Set short refresh intervals for production secrets to minimize exposure windows.

### Anti-Patterns (IMPORTANT)
- **Do NOT Log Secrets**: DSO automatically redacts logs, but never manually echo secrets into logs.
- **Do NOT Persist to Disk**: Avoid storing secrets in persistent volumes; DSO is designed for volatile memory injection only.
- **Avoid ENV for High Security**: Environment variables are easier to leak in crash dumps; use file injection where possible.

### Threat Model
For a detailed security analysis, including risks associated with the Docker Socket and environment variables, refer to our [Lightweight Threat Model](SECURITY.md#threat-model).

---

## 🔌 Provider Plugins

DSO uses a plugin architecture for cloud secret backends. All plugins are built and installed automatically by the installer.

| Provider | Plugin Binary | Auth Methods | Status |
| :--- | :--- | :--- | :--- |
| **AWS Secrets Manager** | `dso-provider-aws` | IAM Role, Access Key, ENV | ✅ Stable |
| **Azure Key Vault** | `dso-provider-azure` | Managed Identity, Client Secret | ✅ Stable |
| **Huawei Cloud CSMS** | `dso-provider-huawei` | Access Key / Secret Key | ✅ Stable |
| **HashiCorp Vault** | `dso-provider-vault` | Token, AppRole | ✅ Stable |

Plugins are installed to `/usr/local/lib/dso/plugins/` during system installation.

---

## 🩺 Monitoring & Health Checks

The DSO Agent exposes a built-in REST API for live monitoring (default port: `:8080`).

| Endpoint | Method | Description |
| :--- | :--- | :--- |
| `/health` | GET | Returns `{"status":"up"}` when the agent is running |
| `/api/secrets` | GET | JSON list of all secrets currently tracked in the cache |
| `/api/events` | GET | Returns recent secret lifecycle events (last 50) |
| `/api/events/ws` | WebSocket | Real-time event streaming for live dashboards |
| `/api/events/secret-update` | POST | Webhook trigger for external rotation notifications |

**Using the health check:**
```bash
curl http://localhost:8080/health
# {"status":"up"}
```

**To customize the API port:**
```bash
docker dso agent --api-addr :9090
```

When running as a `systemd` service, the health endpoint can be used in your infrastructure monitoring stack (e.g. Prometheus blackbox exporter, UptimeRobot, or Kubernetes readiness probes).

---

## 🏗️ Architecture Overview

DSO consists of three primary layers:
1.  **DSO Agent**: The long-running process that orchestrates lifecycle events.
2.  **Provider Layer**: Pluggable connectors for AWS, Vault, Azure, and Huawei.
3.  **Injection Engine**: Securely streams secrets into containers using the Docker API.

For a deeper dive, see our [Architecture Documentation](ARCHITECTURE.md).

---

## ⚠️ Breaking Changes (V3.1)

DSO has fully transitioned to a **single-binary Docker CLI plugin architecture**.

- **Unified Binary**: The legacy `dso` and `dso-agent` commands are preserved as **symlinks** to `docker-dso` for backward compatibility. Use `docker dso <command>` for all primary operations.
- **Plugin-First Usage**: DSO is now installed as a Docker CLI plugin (`docker-dso`), enabling the `docker dso` interface natively.
- **Unified Logic**: All core reconciliation and CLI logic reside within a single `docker-dso` executable.

---

## 🔄 Agent Lifecycle

DSO V3.1 introduces automated agent management to simplify the user experience.

- **Auto-Start**: The `docker dso up` command automatically checks if the DSO agent is running. If no responsive agent is detected on the Unix socket (`/var/run/dso.sock`), it spawns a background agent process.
- **Background Execution**: When started via `up`, the agent runs as a detached subprocess (`docker-dso agent`), ensuring that the CLI remains responsive. Logs for the background agent can be viewed via system logs or by running the agent in the foreground.
- **Foreground Mode**: You can manually run the agent in the foreground using `docker dso agent`. This is recommended when running DSO as a `systemd` service or during initial setup/debugging.
- **API Server**: The agent starts a REST API server on `:8080` by default. Use the `--api-addr` flag to customize the port (e.g. `docker dso agent --api-addr :9090`).
- **Graceful Shutdown**: The agent uses `SIGTERM` / `SIGINT` signals via `signal.NotifyContext` to cleanly stop all goroutines, flush sockets, and release resources before exiting.

---

## 🚚 Installation Paths

Depending on your use case, `docker-dso` should be installed in one of the following locations for Docker to recognize it as a plugin:

- **User Install** (Default):
  `~/.docker/cli-plugins/docker-dso` (Linux/macOS)
  `%USERPROFILE%\.docker\cli-plugins\docker-dso.exe` (Windows)

- **System Install** (Requires sudo):
  `/usr/local/lib/docker/cli-plugins/docker-dso`

Ensure the binary has executable permissions: `chmod +x ~/.docker/cli-plugins/docker-dso`.

---

## 🛠️ Troubleshooting

If `docker dso` is not recognized:
1. Ensure the binary is in the correct `cli-plugins` folder above.
2. Ensure the binary is named exactly `docker-dso` (or `docker-dso.exe`).
3. Restart your Docker CLI or terminal session.

## 📚 Documentation Links

- 🛠️ [Configuration Reference](docs/configuration.md)
- 📡 [Provider Setup Guide](docs/providers.md)
- 🏗️ [Architecture Deep Dive](ARCHITECTURE.md)
- 📁 [Example Library](examples/)

---

### ⭐ Contributing & Community
DSO is open-source and **aligned with CNCF Sandbox expectations**. We welcome [contributions](CONTRIBUTING.md) and [feedback](https://github.com/docker-secret-operator/dso/issues). If you like the project, please give us a star! ⭐

---

## 📚 Deep Dive Resources

- 🏗️ **Architecture**: Learn about our [Zero-Persistence Execution](ARCHITECTURE.md).
- 🛡️ **Security**: Review our [Hardening Guide and Threat Model](SECURITY.md).
- 📁 **Examples**: Explore the [Pre-configured Stack Library](examples/README.md).

Licensed under the Apache License, Version 2.0.
