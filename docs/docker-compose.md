# Docker Compose Integration

> **Mode note:** `dso://` and `dsofile://` URI patterns are **Local Mode only**. In Cloud Mode, secret routing is defined in `dso.yaml` via `secrets[].mappings` — not in the compose file. See the [Configuration Reference](configuration.md).

DSO integrates seamlessly into your existing `docker-compose.yaml` files without requiring custom schema extensions. It utilizes standard `yaml.v3` AST parsing to preserve your comments and structure.

## 1. File Injection (Best Practice)
Most modern Docker images support `_FILE` suffixes for environment variables. DSO uses `dsofile://` to satisfy these natively.

```yaml
services:
  web:
    image: myapp:latest
    environment:
      # DSO will mount this inside /run/secrets/dso/
      STRIPE_API_KEY_FILE: dsofile://billing/stripe_key
```

## 2. Environment Injection (Legacy)
If your application absolutely cannot read from a file, use `dso://`. DSO will inject the literal text into the environment block right before boot.

```yaml
services:
  web:
    image: legacyapp:1.0
    environment:
      # DSO evaluates this and passes STRIPE_API_KEY=sk_live_...
      STRIPE_API_KEY: dso://billing/stripe_key
```

## 3. Mixed Usage
You can freely mix list-formats and map-formats, as well as `dso://` and `dsofile://`.

```yaml
services:
  mixed_app:
    image: complex:latest
    environment:
      - DATABASE_PASSWORD_FILE=dsofile://global/db_pass
      - REDIS_URL=dso://global/redis_url
      - DEBUG=true
```

## Implicit vs Explicit Scoping
- `dso://db_pass`: Resolves to the *current* docker-compose project context.
- `dso://shared/db_pass`: Resolves explicitly to the `shared` namespace in the DSO vault.

## Published ports and the DSO proxy

For services DSO manages, DSO can take over **TCP** host ports so a rotation can swap the backend without closing the listener. It does this only when it can reproduce *every* published mapping of the service exactly:

| Mapping | Result |
|---|---|
| `8080:80` | proxied, listens on all interfaces (as Docker would) |
| `127.0.0.1:5432:5432`, `[::1]:8443:443`, `192.168.1.5:9000:9000` | proxied on exactly that address; **never widened** |
| long syntax (`target`, `published`, `host_ip`, `protocol: tcp`, `mode: host`) | same as the short forms |
| UDP / SCTP, port ranges, auto-assigned host ports (`127.0.0.1::80`), host ports below 1024, two addresses for one host port, unknown long-syntax options | **not proxied** |

If any mapping of a service is not proxiable, DSO leaves that service's `ports:` exactly as written and prints a note on stderr. Docker then publishes them directly and DSO uses its non-proxy rotation path (the old container is stopped before the new one starts, because two containers cannot publish the same host port), so such services do not get proxy-based zero-downtime rotation. UDP is never translated to TCP.

The proxy label `dso.host_ports` accepts `host:container`, `ip:host:container` and `[ipv6]:host:container`; entries with a hostname, zone, malformed address, privileged host port, or a second address for an already-listed port are ignored. Runtime verification of these bindings in Docker has not yet been performed (see the M0 report).
