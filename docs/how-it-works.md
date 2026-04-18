# How DSO Works

This page walks through what happens when you run DSO from the command line,
step by step.

---

## Overview

```
Your docker-compose.yml
        │
        ▼
  dso generate
        │
        ▼
docker-compose.generated.yml
        │
        ▼
docker compose up -f docker-compose.generated.yml
        │
        ▼
dso-proxy-api  ←── owns port 3000 on the host
api            ←── expose only, dso_mesh, replaceable without downtime
```

---

## Step 1: Write a standard `docker-compose.yml`

Nothing special is required. Write your compose file as you normally would.
DSO reads it and figures out what to do.

```yaml
services:
  api:
    image: myapp:latest
    ports:
      - "3000:3000"
```

If you want fine-grained control, add an optional `x-dso` block to any service:

```yaml
services:
  api:
    image: myapp:latest
    ports:
      - "3000:3000"
    x-dso:
      enabled: true
      strategy: rolling
```

See [dso-config.md](./dso-config.md) for all `x-dso` options.

---

## Step 2: Run `dso generate`

```bash
dso generate
# or with explicit paths:
dso generate --input docker-compose.yml --output docker-compose.generated.yml
```

### What the CLI does

**1. Parse**

DSO reads your `docker-compose.yml` and evaluates each service against the
auto-detection rules (in priority order):

| Rule | Result |
|------|--------|
| `x-dso.enabled: false` | Never eligible — skipped regardless of ports |
| `x-dso.enabled: true` | Always eligible — proxied regardless of image |
| Has `ports` + not a known database image | Eligible — proxy injected |
| Everything else | Pass-through — unchanged |

Known database images (auto-excluded): `mysql`, `postgres`, `mariadb`, `mongo`,
`redis`, `elasticsearch`, `cassandra`, `rabbitmq`, `memcached`, `couchdb`,
`influxdb`, `neo4j`, `kafka`, `zookeeper`, `etcd`.

**2. Transform eligible services**

For each service marked eligible:

- `ports` entries are removed from the backing service.
- `expose` entries are added (container-side port only, no host binding).
- `networks: [dso_mesh]` is merged into the service's network list.
- Labels `dso.service`, `dso.managed` (and optionally `dso.strategy`) are added.
- A new `dso-proxy-<service>` service is generated that owns the host ports.

**3. Pass through ineligible services**

Services that are not eligible (databases, opt-out, no ports) are copied to
the generated file verbatim. Only `container_name` and `x-dso` are stripped —
everything else is preserved exactly as written.

**4. Emit the summary**

```
Parsed 3 service(s) — 1 eligible for proxy injection

DSO Transform Summary:
  DSO: Enabling zero-downtime for service 'api'
  DSO: Injecting proxy for port 3000 → api

Generated: docker-compose.generated.yml
```

---

## Step 3: Start the stack

```bash
docker compose -f docker-compose.generated.yml up -d
```

Docker starts:

1. **`api`** — binds only to the internal `dso_mesh` network; no host port.
2. **`dso-proxy-api`** — starts after `api` (`depends_on`); owns port 3000 on the host.
   - Reads `DSO_PROXY_BINDS=3000:api:3000` and opens a TCP listener on `:3000`.
   - Reads `DSO_PROXY_BACKENDS=api-default:api:api:0` and registers the `api` DNS
     name as the initial backend.
   - Starts the HTTP control API on port `9900` (dso_mesh only).

At this point, all traffic that arrives on `host:3000` flows through the proxy to
the `api` container. The setup is invisble to the caller.

---

## Step 4: Deploying a new version (zero downtime)

When you roll out a new image version:

```bash
# Update the image tag in docker-compose.yml, then re-generate:
dso generate

# Pull the new image:
docker compose -f docker-compose.generated.yml pull api

# Recreate only the backing service (NOT the proxy):
docker compose -f docker-compose.generated.yml up -d --no-deps api
```

**What happens under the hood:**

```
time 0s   Old container (api-v1) running, proxy routing all traffic to it.
time 1s   New container (api-v2) starts. Gets a new container ID / IP.
time 2s   DSO agent (or deploy script) calls:
          POST http://dso-proxy-api:9900/backends
          {"id":"api-v2","service":"api","host":"<api-v2-ip>","port":3000}
time 3s   Proxy begins routing new connections round-robin to api-v1 AND api-v2.
time 4s   In-flight connections to api-v1 complete naturally (no TCP reset).
time 5s   DSO agent deregisters api-v1:
          DELETE http://dso-proxy-api:9900/backends/api-v1
time 6s   All traffic goes to api-v2. api-v1 container is stopped and removed.
```

No client observed a connection failure. No load balancer rule was changed.
No downtime occurred.

---

## Step 5: Inspecting the proxy at runtime

The DSO proxy exposes its state via an HTTP API on port `9900` (reachable from
other services on the `dso_mesh` network):

```bash
# List registered backends
docker exec dso-proxy-api curl -s http://localhost:9900/backends | jq

# Liveness check
docker exec dso-proxy-api curl -s http://localhost:9900/health

# List active port bindings
docker exec dso-proxy-api curl -s http://localhost:9900/bindings

# Register a new backend manually
docker exec dso-proxy-api curl -s -X POST http://localhost:9900/backends \
  -H 'Content-Type: application/json' \
  -d '{"id":"api-v2","service":"api","host":"172.20.0.5","port":3000}'

# Remove a backend
docker exec dso-proxy-api curl -s -X DELETE http://localhost:9900/backends/api-v1
```

---

## Multi-Port Services

Services with multiple port mappings are fully supported. Each port mapping
produces one entry in `DSO_PROXY_BINDS`, and the proxy opens one TCP listener
per entry:

```yaml
# Input:
frontend:
  ports:
    - "80:80"
    - "443:443"

# Generated DSO_PROXY_BINDS:
# "80:frontend:80,443:frontend:443"
```

The generated `dso-proxy-frontend` service listens on both ports simultaneously.
Connections on port 80 are forwarded to `frontend:80`; connections on port 443
are forwarded to `frontend:443`.

---

## Backward Compatibility

If your compose file still contains a `dso-proxy` block from an older DSO version,
DSO will:

1. Print a deprecation warning to stderr.
2. Parse the block and use it to force-enable eligibility for the referenced services.
3. Continue generating the output as normal.

The warning:
```
WARNING: The dso-proxy block is deprecated. Use a standard docker-compose.yml with
optional x-dso extension fields instead. Support will be removed in a future major release.
```

Migrate by removing the `dso-proxy` block. Auto-detection will handle the same services
without any configuration.
