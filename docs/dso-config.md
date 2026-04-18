# DSO Configuration Reference

DSO works with **standard `docker-compose.yml` files out of the box**.
No custom syntax is required for the common case.

For advanced control, add an optional `x-dso` block to any service.

---

## Auto-Detection (Zero Config)

When you run `dso generate`, DSO evaluates each service and applies these rules
**in order** (first matching rule wins):

| Priority | Condition | Result |
|----------|-----------|--------|
| 1 | `x-dso.enabled: false` | **Skip** — never proxied |
| 2 | `x-dso.enabled: true` | **Proxy** — always proxied |
| 3 | Has `ports` + not a known database image | **Proxy** — auto-detected |
| 4 | Everything else | **Pass-through** — unchanged |

This means most users need zero configuration. Add `x-dso` only when you
want to override the defaults.

---

## `x-dso` Extension Field

Add `x-dso` to any service in your `docker-compose.yml` to control DSO behaviour
for that specific service. Docker and standard compose tooling ignore the field
(extension fields are part of the Compose specification).

### Full schema

```yaml
services:
  your-service:
    image: your-image:tag
    ports:
      - "3000:3000"

    x-dso:
      enabled: true       # optional bool. Override auto-detection.
      strategy: rolling   # optional string. Deployment strategy (default: rolling).
```

### `enabled`

**Type:** `bool` | **Default:** auto-detected

Explicitly opt a service in or out of DSO proxy injection.

| Value | Behaviour |
|-------|-----------|
| `true` | Always inject a proxy, even for known database images |
| `false` | Never inject a proxy, even if `ports` are declared |
| *(absent)* | Auto-detect based on ports and image name |

**Examples:**

```yaml
# Force a postgres read-replica into proxy mode (unusual but supported)
analytics-db:
  image: postgres:16
  ports:
    - "5433:5432"
  x-dso:
    enabled: true

# Keep an admin panel's port directly on the service, not the proxy
admin:
  image: admin-ui:latest
  ports:
    - "9000:9000"
  x-dso:
    enabled: false
```

### `strategy`

**Type:** `string` | **Default:** `rolling`

The deployment strategy to use when rotating this service.

| Value | Description |
|-------|-------------|
| `rolling` | Replace the container in place. New version starts, old version drains. |
| `canary` | *(Phase 2)* Gradually shift traffic to the new version over time. |

The strategy is stored as the label `dso.strategy` on the backing service and is
used by the DSO agent when driving automated rollouts.

---

## Database Auto-Exclusion

DSO recognises the following image names as stateful services that must not be
traffic-proxied by default. Recognition is **tag-agnostic** (`:latest`, `:8.0`,
`:alpine` etc. are all matched) and **registry-agnostic** (private registries,
`docker.io/library/`, etc. are stripped before matching).

| Image name | Type |
|------------|------|
| `mysql` | Relational database |
| `postgres` / `postgresql` | Relational database |
| `mariadb` | Relational database |
| `mongo` / `mongodb` | Document database |
| `redis` | Cache / message broker |
| `elasticsearch` | Search index |
| `cassandra` | Wide-column database |
| `rabbitmq` | Message queue |
| `memcached` | Cache |
| `couchdb` | Document database |
| `influxdb` | Time-series database |
| `neo4j` | Graph database |
| `kafka` | Distributed log |
| `zookeeper` | Coordination service |
| `etcd` | Key-value store |

**Why exclude databases?**
Mid-connection container replacement on a stateful database causes TCP resets
and can lead to in-flight transaction loss. DSO errs on the side of safety.

**Override:** Use `x-dso: {enabled: true}` to proxy a database image anyway
(e.g. for a read replica behind a connection pool that handles reconnection).

---

## Disabling the `x-dso` Strip

By default DSO strips the `x-dso` key from the generated compose file so
Docker never sees it. This is always the correct behaviour and cannot currently
be disabled. The generated file is valid standard Docker Compose YAML.

---

## Environment Variables (DSO Proxy Container)

The generated `dso-proxy-<service>` container is configured via environment
variables. You don't set these manually — DSO generates them from your port
mappings. They are documented here for reference and for advanced scripting
(e.g. calling the control API in deployment scripts).

| Variable | Format | Example |
|----------|--------|---------|
| `DSO_PROXY_BINDS` | `listenPort:service:targetPort,...` | `3000:api:3000,443:api:443` |
| `DSO_PROXY_BACKENDS` | `id:service:host:port,...` | `api-default:api:api:0` |
| `DSO_PROXY_API_PORT` | integer | `9900` |

**`DSO_PROXY_BINDS`** — one entry per port mapping. The proxy opens one TCP
listener per entry. Multiple entries are comma-separated.

**`DSO_PROXY_BACKENDS`** — initial backends to register at startup. The host
field uses the Docker service DNS name (e.g. `api`), which Docker resolves to
the container's IP on `dso_mesh`. `port: 0` means "use the `targetPort` from
the corresponding bind spec."

**`DSO_PROXY_API_PORT`** — the HTTP control API port. Always `9900` in
generated files. The port is exposed on `dso_mesh` only; it is not accessible
from outside Docker.

---

## Control API Reference

The DSO proxy exposes an HTTP API that allows dynamic backend management
without restarting the proxy.

**Base URL:** `http://dso-proxy-<service>:<DSO_PROXY_API_PORT>`
(from within the `dso_mesh` network)

### `GET /health`

Returns liveness status and the number of registered backends.

```json
{"status": "ok", "backends": 2}
```

### `GET /backends`

Returns all registered backends across all services.

```json
{
  "backends": [
    {"id": "api-v1", "service": "api", "host": "172.20.0.3", "port": 3000, "added_at": "..."},
    {"id": "api-v2", "service": "api", "host": "172.20.0.5", "port": 3000, "added_at": "..."}
  ],
  "count": 2
}
```

### `POST /backends`

Register a new backend. All fields except `weight` are required.

```bash
curl -X POST http://dso-proxy-api:9900/backends \
  -H 'Content-Type: application/json' \
  -d '{
    "id":      "api-v2",
    "service": "api",
    "host":    "172.20.0.5",
    "port":    3000
  }'
```

Returns `201 Created` with the registered backend object.
Returns `409 Conflict` if the ID is already registered.

If `id` is omitted, DSO auto-generates one as `<service>-<host>-<port>`.

### `DELETE /backends/{id}`

Deregister a backend by ID. Returns `204 No Content` on success.
Returns `404 Not Found` if the ID does not exist.

```bash
curl -X DELETE http://dso-proxy-api:9900/backends/api-v1
```

### `GET /bindings`

Lists the active TCP port bindings.

```json
{
  "bindings": [
    {"listen_port": 3000, "service": "api", "target_port": 3000}
  ]
}
```

---

## CLI Flags

### `dso generate`

```
Usage: dso generate [flags]

Flags:
  -i, --input  string   Path to input docker-compose.yml  (default: docker-compose.yml)
  -o, --output string   Path to generated output file     (default: docker-compose.generated.yml)
  -h, --help            Show help
```

**Examples:**

```bash
# Use defaults (reads docker-compose.yml, writes docker-compose.generated.yml)
dso generate

# Explicit paths
dso generate --input compose/production.yml --output compose/production.generated.yml
```
