# DSO Zero-Downtime Examples

This directory contains ready-to-use example configurations showing how DSO
works with standard Docker Compose files.

---

## Examples

### `basic/` — Zero-Config Auto-Detection

A standard Node.js API with no DSO-specific fields. Run `dso generate` and DSO
automatically detects the service and injects a proxy.

**What to learn:** How DSO works with an unmodified compose file.

```bash
cd basic
dso generate
docker compose -f docker-compose.generated.yml up -d
```

---

### `advanced/` — Explicit `x-dso` Control

Shows all four control cases:
- Explicit opt-in (`enabled: true`) with a custom strategy
- Explicit opt-out (`enabled: false`) to keep a port directly on a service
- Database force-enabled (`postgres` with `enabled: true` override)
- Database auto-excluded (`mysql` — skipped without any configuration)

**What to learn:** When and how to use the `x-dso` extension field.

---

### `database/` — Database Auto-Exclusion

A realistic API + MySQL + Redis stack. DSO automatically skips the database
and cache layers and proxies only the API service.

**What to learn:** How DSO identifies and skips stateful database images.

---

### `generated/` — Annotated Generated Output

The fully annotated `docker-compose.generated.yml` produced by DSO. Every
injected field is documented inline.

**What to learn:** Exactly what DSO changes in the generated file and why.

---

## Running an Example

```bash
# Step 1: Generate the enhanced compose file
dso generate --input <example>/docker-compose.yml \
             --output <example>/docker-compose.generated.yml

# Step 2: Start the stack
docker compose -f <example>/docker-compose.generated.yml up -d

# Step 3: Check the proxy status
docker exec dso-proxy-api curl -s http://localhost:9900/health
```

---

## See Also

- [docs/architecture.md](../docs/architecture.md) — How the components fit together
- [docs/how-it-works.md](../docs/how-it-works.md) — Step-by-step walkthrough
- [docs/dso-config.md](../docs/dso-config.md) — Full configuration reference
