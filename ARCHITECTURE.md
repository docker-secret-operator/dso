# DSO System Architecture (V3.1)

The Docker Secret Operator (DSO) is a **Secret Reconciliation Engine** designed for standalone Docker and Docker Compose environments. It implements a **zero-persistence model** to ensure sensitive data is fetched from trusted providers and injected into containers without ever being written to the host's physical disk.

---

## 🏗️ Core Components

### 1. DSO Agent
The primary long-running orchestration process. It manages authentication with secret providers, maintains an in-memory RAM cache, and executes the core reconciliation loops.

### 2. Multi-Provider Layer
A pluggable system that connects to various secret backends (HashiCorp Vault, AWS, Azure, Huawei, File). V3.1 allows **simultaneous** connection to multiple backends via the `providers` map.

### 3. Watcher Engine
An event-driven component that monitors:
- **Docker Socket**: Real-time listening for container lifecycle events (`start`, `die`, `stop`).
- **Secret Providers**: Periodic polling (configurable) or webhook-based detection of secret updates.

### 4. Reloader Controller
Responsible for the atomic rotation of target containers. In V2, it incorporates **Smart Checksum Comparison** to only trigger rotations when actual secret data changes.

---

## 🔄 Data Flow Execution

DSO follows a precise, deterministic sequence for every secret reconciliation event:

1. **Load Configuration**: The CLI/Agent resolves the `dso.yaml` through the `ResolveConfig` utility, performing a high-speed schema validation.
2. **Initialize Providers**: The provider registry invokes the required RPC-based plugins (AWS, Vault, etc.) and establishes secure authentication sessions.
3. **Fetch & Decrypt**: Secret data is retrieved over TLS and decrypted into the Agent's volatile RAM. 
4. **Compare Checksum**: The `TriggerEngine` computes a SHA-256 hash of the new secret and compares it with the cached version. If identical, the process silently terminates to avoid container churn.
5. **Prepare Tar Stream**: If a change is detected, the `TarStreamer` packages the secret into an in-memory tar archive, ensuring no plaintext data hits the physical disk.
6. **Trigger Rotation**:
    - **Backup**: The `Reloader` renames the active container to a backup name.
    - **Inject**: The tar stream is uploaded to the new container's `tmpfs` via the Docker API.
    - **Start**: The new container starts; the old one is removed only after a successful health check pass.

---

## 📉 Failure Flow & Resilience

DSO is designed for high availability, ensuring that transient provider failures do not impact running containers.

1. **Detection**: If a provider call (e.g., AWS Secrets Manager) fails due to network timeout or expired credentials, the Agent catches the error.
2. **Retry with Backoff + Jitter**: DSO immediately triggers an exponential backoff loop. It waits for a base interval (default 2s) which doubles with each failure, adding a random jitter to prevent "thundering herd" syndrome on your secret backend.
3. **Max Attempts & Thresholds**: If the failure persists beyond the maximum retry count (default 5 attempts), the Agent ceases active retries for that specific reconciliation cycle.
4. **Resilient Skip**: DSO **never** removes or overwrites a secret if the fetch fails. It retains the last known-good secret in the RAM cache and logs a structured `ERROR` with the specific provider failure details for external alerting systems.

### 👤 User Impact
In the event of a provider outage or sync failure, the system falls back to a "safe state":
- **No Deletion**: Secrets are **never** removed from target containers.
- **No Restarts**: Containers are **not** restarted or rotated if a fetch occurs in a failure state.
- **Continuity**: The application continues to run using its last-known-good secret configuration while the operator continues to retry the provider in the background.

---

##  diagrama Flow Diagram (ASCII)

```text
  [Secret Providers]
          ↓
  [DSO Multi-Provider Map] -- (Retry + Jitter)
          ↓
  [In-Memory RAM Cache] -- (Checksum Check)
          ↓
  [DSO Reconciliation Engine] -- (Target Selection)
          ↓
  [Docker API (Socket)]
          ↓
  [In-Memory Tar Streaming]
          ↓
  [Target Container (tmpfs)]
```

---

## 🛡️ Security Design Decisions

- **In-Memory Tar Streaming**: DSO packages secrets into a tar archive in RAM and uploads them directly to the container's `tmpfs` mounts via the Docker API, bypassing the host's filesystem entirely.
- **Service-Level Concurrency Locking**: DSO prevents race conditions by ensuring only one rotation is active per service at any time.
- **Log Redaction**: Centralized redaction logic ensures that sensitive values are masked before reaching any output stream.
- **Strict Targeting**: V2's `targets` block ensures that secrets are only delivered to explicitly authorized containers, even if labels are accidentally applied elsewhere.

---

## 🏗️ Why This Design?

### 🔄 Checksum-based Rotation
**The Problem**: Traditional operators often restart containers every time a sync occurs, even if the secret hasn't changed, leading to unnecessary downtime.
**The DSO Solution**: DSO calculates a SHA-256 checksum of the fetched secret. We only trigger the Docker rotation logic if the checksum differs from the last successful injection.

### 📡 Multi-Provider Map
**The Problem**: Many organizations use multiple secret stores (e.g., Vault for DBs, AWS for infrastructure). Switching between them usually requires different agents.
**The DSO Solution**: The V2 `providers` map allows a single DSO instance to bridge multiple backends simultaneously, providing a unified injection interface.

### 🎯 Label-based Targeting
**The Problem**: Hardcoding container names in configuration files is fragile and doesn't scale with dynamic stacks.
**The DSO Solution**: By using Docker labels (`dso.inject: "true"`), DSO can automatically discover and inject secrets into any container that matches the selector, making it ideal for elastic environments.

---

## 📈 Roadmap

DSO is evolving into an intelligent reconciliation platform. For the detailed phase-by-phase vision, see the [ROADMAP.md](./ROADMAP.md).

- **Phase 1**: Core Engine Completion (`apply`, `diff`, background loop).
- **Phase 2**: Controlled Ecosystem Expansion (GSM, 1Password).
- **Phase 3**: Intelligence & Observability (AI Sentinel, OTel).
