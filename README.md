# Docker Secret Operator (DSO)

> Zero-downtime deployments for Docker — no Kubernetes required.

[![Build Status](https://img.shields.io/badge/Build-Passing-brightgreen.svg)](https://github.com/docker-secret-operator/dso/actions)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.22+-blue.svg)](go.mod)

DSO is a Docker Compose enhancer that transparently injects a Go-based TCP proxy
between your host ports and your containers. When you update a service, in-flight
connections aren't dropped — the proxy holds the port and routes traffic around
the container replacement.

It works with your existing `docker-compose.yml`. No custom syntax required.

---

## Quick Start

```bash
# 1. Install DSO
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sudo bash

# 2. Point it at your existing compose file
dso generate

# 3. Start the enhanced stack
docker compose -f docker-compose.generated.yml up -d
```

That's it. Your app is now behind a zero-downtime proxy.

---

## How It Works

Take a standard compose file:

```yaml
# docker-compose.yml — no changes needed
services:
  api:
    image: myapp:latest
    ports:
      - "3000:3000"
```

Run `dso generate`. DSO analyses each service and injects a proxy for any
service with `ports` that isn't a known database image.

```
Parsed 2 service(s) — 1 eligible for proxy injection

DSO Transform Summary:
  DSO: Enabling zero-downtime for service 'api'
  DSO: Injecting proxy for port 3000 → api

Generated: docker-compose.generated.yml
```

The generated file creates two services in place of one:

```
Client :3000 → dso-proxy-api (owns host port) → api (expose only, replaceable)
```

The proxy is a lightweight pure-Go TCP reverse proxy that:
- Accepts connections on the host port permanently (no gap during container replacement)
- Routes traffic to the current backend container(s) using round-robin
- Exposes an HTTP control API (`:9900`, internal only) for dynamic backend management

When you update `api`, the proxy seamlessly routes around the container switch.
No client sees a connection refused or TCP reset.

---

## Zero Config — Works With What You Have

DSO reads standard `docker-compose.yml` files. You don't need to learn a new syntax.

**Auto-detection rules (applied per service):**

| Condition | Result |
|-----------|--------|
| `x-dso.enabled: false` | Skip — leave ports on the service |
| `x-dso.enabled: true` | Always inject proxy |
| Has `ports` + not a database image | Auto-inject proxy |
| No ports, or database image | Pass through unchanged |

**Known database images (never proxied by default):**
`mysql`, `postgres`, `mariadb`, `mongo`, `redis`, `elasticsearch`, `cassandra`,
`rabbitmq`, `memcached`, `couchdb`, `influxdb`, `neo4j`, `kafka`, `zookeeper`, `etcd`

---

## Optional `x-dso` Configuration

For services where you want to override the defaults:

```yaml
services:
  api:
    image: myapp:latest
    ports:
      - "3000:3000"
    x-dso:
      enabled: true      # explicit opt-in (auto-detection would catch this anyway)
      strategy: rolling  # deployment strategy: rolling (default) | canary (Phase 2)

  admin:
    image: admin:latest
    ports:
      - "9000:9000"
    x-dso:
      enabled: false     # opt out — keep port directly on the service
```

The `x-dso` block is stripped from the generated file. Docker never sees it.

---

## Multi-Port Services

Multiple port mappings are handled natively — one proxy listener per port:

```yaml
# Input
frontend:
  ports:
    - "80:80"
    - "443:443"

# Generated proxy config
# DSO_PROXY_BINDS: "80:frontend:80,443:frontend:443"
# → two listeners: :80 → frontend:80 | :443 → frontend:443
```

---

## Deploying a New Version (Zero Downtime)

```bash
# Pull the new image
docker pull myapp:v2

# Edit docker-compose.yml to reference myapp:v2, then regenerate
dso generate

# Restart only the backing service — the proxy stays running
docker compose -f docker-compose.generated.yml up -d --no-deps api
```

The proxy seamlessly routes between the old and new container during the transition.
In-flight connections complete on the old container; new connections go to the new one.

---

## Runtime Inspection

The proxy exposes an HTTP control API (internal, `dso_mesh` network only):

```bash
# Check proxy status
docker exec dso-proxy-api curl -s http://localhost:9900/health

# List current backends
docker exec dso-proxy-api curl -s http://localhost:9900/backends | jq

# Add a backend (e.g. a newly started container)
docker exec dso-proxy-api curl -s -X POST http://localhost:9900/backends \
  -H 'Content-Type: application/json' \
  -d '{"id":"api-v2","service":"api","host":"172.20.0.5","port":3000}'

# Remove a backend (drain the old container)
docker exec dso-proxy-api curl -s -X DELETE http://localhost:9900/backends/api-v1
```

---

## Examples

| Example | What it shows |
|---------|--------------|
| [`examples/basic/`](examples/basic/) | Zero-config auto-detection for a Node app |
| [`examples/advanced/`](examples/advanced/) | `x-dso` explicit enable/disable/strategy |
| [`examples/database/`](examples/database/) | Database auto-exclusion (mysql + redis) |
| [`examples/generated/`](examples/generated/) | Annotated generated compose output |

---

## Documentation

| Page | Description |
|------|-------------|
| [docs/architecture.md](docs/architecture.md) | How the proxy, registry, and mesh network fit together |
| [docs/how-it-works.md](docs/how-it-works.md) | Step-by-step walkthrough of the full lifecycle |
| [docs/dso-config.md](docs/dso-config.md) | Full `x-dso` reference, control API, CLI flags |

---

## Installation

**Linux / macOS / WSL:**
```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sudo bash
```

**Build from source:**
```bash
git clone https://github.com/docker-secret-operator/dso.git
cd dso
go build -o dso ./cmd/dso
```

**Verify:**
```bash
dso --help
```

---

## Secret Management

DSO also provides enterprise-grade secret injection via external providers
(AWS Secrets Manager, HashiCorp Vault, Azure Key Vault). The proxy architecture
and secret management are complementary features — you can use either or both.

See [docs/configuration.md](docs/configuration.md) for the full secret provider configuration reference.

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). All contributions welcome.

## License

Apache 2.0. See [LICENSE](LICENSE).
