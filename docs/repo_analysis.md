# 🔍 DSO: Deep Repository Analysis (V3.1)

## 1. Structural Overview
The Docker Secret Operator (DSO) follows a **Modular Monolith** structure in Go, optimized for high-speed IPC and secure secret handling.

### Core Modules
| Module | Location | Responsibility |
| :--- | :--- | :--- |
| **CLI Engine** | `internal/cli/` | Cobra-based Docker CLI entrypoints (up, down, agent, etc). |
| **Reconciler** | `internal/agent/trigger.go` | The "Brain" that monitors secret state and triggers rotations. |
| **Core Logic** | `internal/core/compose.go` | In-memory YAML transformation and Docker Compose pipe orchestration. |
| **Injection** | `internal/injector/` | Secure Tar streaming logic used to bypass host disk persistence. |
| **Providers** | `pkg/provider/` | Pluggable interface for AWS, Vault, Azure, and Local File systems. |
| **Observability** | `pkg/observability/` | Unified logging, metrics (Prometheus), and event streaming. |

---

## 2. Security Architecture: The Zero-Persistence Path
DSO's primary value proposition is its "Zero-Persistence" execution model.

1.  **Transport**: Secrets are retrieved over TLS from the Provider (e.g., AWS Secrets Manager).
2.  **Memory Store**: Secrets are decrypted into the **DSO Agent's volatile RAM** (guarded by `SecretCache`).
3.  **In-Memory Pipe**: When `docker dso up` is called, the YAML compose file is transformed in RAM.
4.  **Docker API Stream**: Secrets are packaged into a Tar archive in-memory and streamed to the Docker Socket.
5.  **Target Injection**: Secrets reside only in the container's `tmpfs` (RAM-disk) mounts.

---

## 3. DevOps Integration Patterns
DSO is designed to fit existing Docker ecosystems with zero friction:
*   **CLI Plugin Native**: Recognized by Docker as `docker dso`, avoiding "custom tool" overhead.
*   **Label-Based Discovery**: DevOps teams don't need to change `docker-compose.yml` logic; they just add labels.
*   **Infrastructure as Code**: The `dso.yaml` acts as the single source of truth for secret mappings.

---

## 4. Key Improvements in V3.1
*   **Atomic Rotation**: Solved the "half-deployed" state issue with `WaitHealthy` logic.
*   **Backoff Jitter**: Prevented "Thundering Herd" syndrome on AWS/Vault APIs with exponential backoff.
*   **Plugin-First Identity**: Standalone binaries removed to enforce the native Docker plugin workflow.
