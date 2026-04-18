# DSO Architecture

Zero-downtime deployments for Docker — no Kubernetes required.

---

## The Problem DSO Solves

Standard Docker Compose has no concept of traffic routing. When you update a service,
Docker kills the old container and starts a new one. During the gap — typically 1–5 seconds
— any in-flight HTTP requests, WebSocket connections, or TCP sessions are dropped.

DSO inserts a **transparent proxy layer** between your host ports and your containers.
The proxy owns the host port permanently; containers come and go behind it without
external clients ever losing their connection.

---

## High-Level Architecture

```
                  ┌─────────────────────────────────────────────┐
                  │              Docker Host                     │
                  │                                              │
  Client          │  ┌─────────────┐        ┌───────────────┐   │
  ────────────────┼─►│ dso-proxy   │──────► │  api (v1)     │   │
  :3000           │  │  :3000      │        │  :3000        │   │
                  │  │             │   ┌──► │  expose only  │   │
                  │  │  Registry   │   │    └───────────────┘   │
                  │  │  Router     │   │                         │
                  │  │  API :9900  │   │    ┌───────────────┐   │
                  │  └─────────────┘   └──► │  api (v2)     │   │
                  │         │               │  :3000        │   │
                  │         │  dso_mesh     │  expose only  │   │
                  │         └──────────────►└───────────────┘   │
                  │                                              │
                  └─────────────────────────────────────────────┘
```

During a zero-downtime update:

1. `api (v2)` starts and registers itself with the proxy's control API.
2. The proxy begins routing a fraction of new connections to `api (v2)`.
3. In-flight connections to `api (v1)` complete normally (no reset, no drop).
4. Once `api (v1)` drains, it is removed from the backend registry and stopped.

---

## Components

### DSO CLI (`dso generate`)

The CLI reads your `docker-compose.yml` and produces `docker-compose.generated.yml`.
It never modifies your original file.

**Responsibilities:**
- Parse the compose file and identify eligible services (services with `ports` that
  are not known database images).
- For each eligible service: remove host port bindings, add `expose`, inject
  `dso_mesh` network and DSO labels.
- Generate a `dso-proxy-<service>` container for each eligible service.
- Pass through ineligible services (databases, opt-out services) completely unchanged.

**What it changes vs. what it preserves:**

| Field | Eligible service | Ineligible service |
|-------|-----------------|-------------------|
| `ports` | Moved to proxy | Unchanged |
| `expose` | Added | Unchanged |
| `networks` | `dso_mesh` added | Unchanged |
| `labels` | DSO labels added | Unchanged |
| `image`, `env`, `volumes`, `depends_on`, `healthcheck` | **Preserved exactly** | Preserved exactly |
| `container_name` | Stripped (architecture constraint) | Stripped |

---

### DSO Proxy (`dso-proxy`)

A pure Go TCP reverse proxy. One proxy container is generated per eligible service.

**Responsibilities:**
- Own the host-side TCP port binding (e.g. `0.0.0.0:3000`).
- Accept connections and route them to the current backend(s) using round-robin.
- Expose an HTTP control API on port `9900` (dso_mesh network only — not reachable from outside Docker).
- Register and deregister backends dynamically without restarting or dropping connections.

**Internal structure:**

```
Server
  └─ one net.Listener per PortBinding
       └─ handleConn()
            └─ Router.Next(service)  → picks a backend (round-robin, atomic counter)
                 └─ Registry          → thread-safe map[service]→[]Backend
```

**Control API endpoints:**

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Liveness probe, returns backend count |
| `GET` | `/backends` | List all registered backends |
| `POST` | `/backends` | Register a new backend |
| `DELETE` | `/backends/{id}` | Deregister a backend by ID |
| `GET` | `/bindings` | List active TCP port bindings |

The DSO agent (future) and deployment scripts can call this API to implement
blue/green or canary releases by adding the new container as a backend before
removing the old one.

---

### Backend Containers

Your application containers, completely unmodified. They:
- Lose their host-side port binding (replaced by `expose`).
- Join the `dso_mesh` bridge network so the proxy can reach them by Docker DNS name.
- Receive DSO labels (`dso.service`, `dso.managed`) but are otherwise identical to
  what you wrote in your compose file.

---

### dso_mesh Network

A Docker bridge network automatically created by DSO and attached to every proxy
and its backing service(s).

- **Purpose:** Provides DNS-based service discovery between the proxy and backends.
  The proxy dials `api:3000` (not an IP) — Docker's embedded DNS resolves this to
  whatever container is currently running for the `api` service.
- **Scope:** Internal only. External traffic enters via the proxy's host port binding.
- **Merging:** Any user-defined networks in the original compose file are preserved
  and merged with `dso_mesh`.

---

## Why the Proxy Owns the Host Port

The key insight behind DSO's zero-downtime model is **port ownership separation**:

| Role | Owns host port | Can be replaced live |
|------|---------------|---------------------|
| dso-proxy | ✅ Yes | ❌ No (it stays running) |
| api container | ❌ No (expose only) | ✅ Yes (proxy routes around it) |

Because the proxy never stops, the host port is always bound. External load
balancers, health checks, and clients never see a TCP connection refused. The
only observable effect of a container replacement is a slightly higher latency for
the connections that land on the new backend while it warms up.

---

## How Zero Downtime Is Achieved

### Current (Phase 1) — Seamless replacement

```
1. Old api container running, proxy routes all traffic to it.
2. `docker compose up -d` or the DSO agent starts api-v2.
3. api-v2 registers with the proxy via the control API.
4. Proxy starts round-robining connections between api-v1 and api-v2.
5. api-v1 drains and is removed from the registry.
6. All traffic flows to api-v2. No external connection was dropped.
```

### Future (Phase 2) — Canary and weighted routing

The `x-dso.strategy: canary` option will add weighted traffic splitting,
allowing gradual rollouts: 5% → 25% → 100% with automatic regression detection.

---

## Constraints and Design Decisions

**No `container_name`**
DSO strips `container_name` from all generated services. Explicit container names
prevent Docker from managing container lifecycle during rotation. Service DNS
(e.g. `api`) works correctly without it.

**Stateless proxy**
The proxy binary holds no persistent state. The backend registry is in-memory.
If the proxy container restarts, the DSO agent re-registers all backends via the
control API.

**Pure TCP**
The proxy operates at the TCP layer, making it protocol-agnostic. HTTP/1.1,
HTTP/2, gRPC, WebSockets, and raw TCP all work transparently. HTTP-layer
optimisations (connection pooling, header rewriting) are planned for Phase 2.
