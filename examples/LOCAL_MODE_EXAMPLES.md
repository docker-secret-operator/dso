# DSO Local Mode Examples

Complete working examples for setting up DSO in local development mode.

---

## Quick Start (2 minutes)

### 1. Minimal Example

**File**: `local-mode-minimal.yml`

Simplest possible setup — PostgreSQL + Python app with secrets injected.

```bash
# Setup DSO
docker dso setup --mode local
docker dso init

# Set your secrets
docker dso secret set app/db_user "postgres"
docker dso secret set app/db_password "mysecretpass"
docker dso secret set app/api_key "sk-1234567890"

# Deploy
docker dso up -d local-mode-minimal.yml

# Verify
docker dso doctor
docker dso status
```

**Use when**: Getting started, learning DSO, or prototyping locally.

---

### 2. Full-Featured Example

**File**: `local-mode-compose.yml`

Production-like setup with PostgreSQL, API, Redis, and pgAdmin. Shows multiple secret injection patterns.

```bash
# Setup DSO
docker dso setup --mode local
docker dso init

# Set all required secrets
docker dso secret set myapp/db_user "myapp"
docker dso secret set myapp/db_password "secure-db-password"
docker dso secret set myapp/api_key "api-key-value"
docker dso secret set myapp/redis_password "redis-password"
docker dso secret set myapp/pgadmin_email "admin@example.com"
docker dso secret set myapp/pgadmin_password "pgadmin-password"

# Deploy
docker dso up -d local-mode-compose.yml

# Verify
docker dso doctor
docker dso status
```

**Use when**: Testing multi-service deployments, learning rotation strategies, or advanced features.

---

## Secret Injection Methods

### Environment Variable Injection (`dso://`)

Secrets are injected as environment variables inside the container.

```yaml
services:
  app:
    environment:
      DB_PASSWORD: dso://myapp/db_password    # Becomes $DB_PASSWORD inside container
      API_KEY: dso://myapp/api_key
```

**Pros:**
- Simple
- Works with any application
- Secrets available immediately

**Cons:**
- Visible via `docker inspect`
- Visible in container process list
- Not suitable for very large secrets

### File Injection (`dsofile://`)

Secrets are mounted as files at `/run/secrets/path/to/secret`. Container reads the file.

```yaml
services:
  redis:
    environment:
      REDIS_PASSWORD_FILE: dsofile://myapp/redis_password
    command: redis-server --requirepass "$(cat /run/secrets/myapp/redis_password)"
```

**Pros:**
- Invisible to `docker inspect`
- Not in environment variables or process list
- Suitable for large secrets (keys, certs)

**Cons:**
- Application must read from file
- Requires application changes
- Slightly more complex

---

## Common DSO Labels

```yaml
labels:
  dso.reloader: "true"                    # DSO manages this container
  dso.secrets: "secret_group_name"        # Which secret group to watch
  dso.update.strategy: "rolling"          # rolling | restart | signal | none
  dso.host_ports: "3306:3306,8000:8000"   # Optional: which ports to proxy
```

| Label | Purpose | Values |
|-------|---------|--------|
| `dso.reloader` | Enable DSO management | `"true"` or `"false"` |
| `dso.secrets` | Which secret group(s) | comma-separated names |
| `dso.update.strategy` | How to rotate on secret change | `rolling` (default), `restart`, `signal`, `none` |
| `dso.host_ports` | Port bindings DSO owns | `"3306:3306,8000:8000"` |

---

## Rotation Strategies

### `rolling` (Zero-downtime — default)

```yaml
dso.update.strategy: "rolling"
```

When a secret changes:
1. Start new container with updated secret
2. Health check new container
3. Atomically swap (old → backup, new → active)
4. Stop old container

**Result**: Zero downtime, ~30 seconds rotation.

**Use for**: Databases, APIs, any production service.

### `restart`

```yaml
dso.update.strategy: "restart"
```

When a secret changes:
1. Stop container
2. Start new container with updated secret

**Result**: Brief downtime (few seconds).

**Use for**: Stateless services, CI/CD workers.

### `signal`

```yaml
dso.update.strategy: "signal"
```

When a secret changes:
1. Send SIGHUP to running container
2. Application reloads config on signal

**Result**: No restart, application handles reload.

**Use for**: Apps that support SIGHUP reload.

### `none`

```yaml
dso.update.strategy: "none"
```

Secrets updated in cache but container not restarted.

**Use for**: Manual rotation workflows.

---

## Managing Secrets

### Set a Secret

```bash
# Interactive prompt
docker dso secret set myapp/db_password
# Enter secret for 'myapp/db_password': (hidden input)

# From pipe
echo "my-secret" | docker dso secret set myapp/api_key

# From file
cat ./private.key | docker dso secret set myapp/tls_key
```

### List Secrets

```bash
# All secrets
docker dso secret list

# Secrets in a project
docker dso secret list myapp

# JSON output
docker dso secret list --json
```

### Retrieve a Secret (Local Mode Only)

```bash
docker dso secret get myapp/db_password
```

### Delete a Secret

```bash
docker dso secret delete myapp/api_key
```

### Bulk Import from .env

```bash
docker dso env import .env myapp
```

File format:
```
# .env
DB_USER=postgres
DB_PASSWORD=secretpass
API_KEY=sk-123456
```

After import:
```
myapp/DB_USER = postgres
myapp/DB_PASSWORD = secretpass
myapp/API_KEY = sk-123456
```

---

## Monitoring & Debugging

### Real-time Status

```bash
# Current snapshot
docker dso status

# Live monitoring (refreshes every 2s)
docker dso status --watch

# JSON output for scripts
docker dso status --json
```

### Health Check

```bash
# Quick health check
docker dso doctor

# Full diagnostics
docker dso doctor --level full
```

### Watch Rotations

```bash
# Live rotation events
docker dso watch

# With raw event payloads
docker dso watch --debug
```

### View Secrets in Container

```bash
# Check environment variables
docker exec <container_name> env | grep -i password

# Check mounted files
docker exec <container_name> ls -la /run/secrets/
docker exec <container_name> cat /run/secrets/myapp/db_password
```

---

## Troubleshooting

### Secrets Not Injected

```bash
# Check container labels
docker inspect <container_name> | grep -A5 Labels

# Check DSO status
docker dso status

# Check secret exists
docker dso secret list

# View DSO logs
docker dso doctor --level full
```

### Container Won't Start

```bash
# Check logs
docker logs <container_name>

# Verify compose file syntax
docker compose config -f local-mode-compose.yml

# Try vanilla Docker Compose first
docker compose -f local-mode-compose.yml up
```

### Secret Rotation Failed

```bash
# Check status
docker dso status

# View diagnostics
docker dso doctor --level full

# Check container states
docker ps -a | grep <service>

# Manual recovery
docker dso down
docker dso up -d
```

---

## Next Steps

1. **Start with minimal example** → understand basic workflow
2. **Add more services** → test multiple secret groups
3. **Test rotation** → change a secret, watch `docker dso status --watch`
4. **Learn codebase** → read `docs/getting-started.md`
5. **Move to production** → use agent mode with cloud provider

---

## File Reference

| File | Purpose | Complexity |
|------|---------|-----------|
| `local-mode-minimal.yml` | PostgreSQL + Python app | ⭐ Beginner |
| `local-mode-compose.yml` | Multi-service setup | ⭐⭐⭐ Advanced |

---

## Tips

- ✅ Always run `docker dso init` as your regular user (NOT sudo)
- ✅ Use `dso.update.strategy: "rolling"` for zero-downtime
- ✅ Test rotation with `docker dso secret set <name>` to trigger updates
- ✅ Check `docker dso doctor` before and after deployments
- ⚠️ Don't put secrets in `docker-compose.yml` files
- ⚠️ Secrets are never written to disk in plaintext (by design)

---

## See Also

- **Getting Started**: [docs/getting-started.md](../docs/getting-started.md)
- **CLI Reference**: [docs/cli.md](../docs/cli.md)
- **Configuration**: [docs/configuration.md](../docs/configuration.md)

