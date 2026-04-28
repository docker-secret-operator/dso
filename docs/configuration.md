# DSO Configuration Reference (v3.2)

> **Important:** `dso.yaml` is only required for **Cloud Mode**. Local Mode users do not need this file — secrets are stored in `~/.dso/vault.enc` instead.

---

## When Do You Need `dso.yaml`?

| Mode | Config File Needed? |
|---|---|
| **Local Mode** | ❌ No — use `docker dso init` and `docker dso secret set` |
| **Cloud Mode** | ✅ Yes — `/etc/dso/dso.yaml` or `./dso.yaml` |

DSO auto-detects Cloud Mode when it finds `/etc/dso/dso.yaml` or `./dso.yaml`. No flag required.

---

## Config File Location

DSO resolves the config file in this priority order:

1. `--config <path>` / `-c <path>` flag
2. `/etc/dso/dso.yaml` (global system config — Cloud Mode)
3. `./dso.yaml` (project-level config — Cloud Mode)

---

## Root Structure

| Field | Required | Description |
|---|---|---|
| `providers` | ✅ Yes (Cloud) | Map of secret backend configurations |
| `secrets` | ✅ Yes (Cloud) | List of secret mappings |
| `defaults` | Optional | Shared defaults for injection and rotation |
| `logging` | Optional | Global logging settings |
| `agent` | Optional | Agent-specific lifecycle settings |

---

## Providers (`providers`)

A map of named provider configurations. Each entry declares a backend type and its auth config.

```yaml
providers:
  vault-prod:
    type: vault
    address: https://vault.example.com:8200
    token: ${VAULT_TOKEN}
    mount: secret

  aws-east:
    type: aws
    region: us-east-1
    auth:
      method: iam_role
```

### Supported Provider Types

| Type | Backend | Status |
|---|---|---|
| `vault` | HashiCorp Vault | ✅ Fully implemented |
| `aws` | AWS Secrets Manager | ✅ Fully implemented |
| `azure` | Azure Key Vault | ✅ Fully implemented |
| `huawei` | Huawei Cloud CSMS | ✅ Fully implemented |
| `file` | Local filesystem (dev only) | ✅ Supported |

---

## Secret Mappings (`secrets`)

Defines which secrets to fetch and how to map them into containers.

| Field | Required | Description |
|---|---|---|
| `name` | ✅ Yes | The exact path/name of the secret in the provider |
| `provider` | ✅ Yes | Name of the provider from the `providers` map |
| `inject` | Optional | Injection configuration (`type`, `path`, `uid`, `gid`) |
| `rotation` | Optional | Rotation config (`enabled`, `strategy`, `signal`) |
| `targets` | Optional | Filter which containers receive this secret |
| `mappings` | Optional | Key-value mapping from provider fields to env var names |

### Injection (`inject`)

```yaml
inject:
  type: env       # 'env' (default) or 'file'
  path: /run/secrets/db_pass   # required if type: file
  uid: 1000                    # optional, file owner
  gid: 1000
```

### Rotation (`rotation`)

```yaml
rotation:
  enabled: true
  strategy: rolling   # restart | signal | rolling | none
  signal: SIGHUP      # required for signal strategy
```

### Targeting (`targets`)

```yaml
targets:
  containers:
    - api
    - worker
  labels:
    app.tier: backend
```

---

## Full Example

```yaml
providers:
  vault-prod:
    type: vault
    address: https://vault.example.com:8200
    token: ${VAULT_TOKEN}
    mount: secret

secrets:
  - name: prod/db_password
    provider: vault-prod
    inject:
      type: env
    mappings:
      DB_PASSWORD: DATABASE_PASSWORD

  - name: prod/tls_cert
    provider: vault-prod
    inject:
      type: file
      path: /run/secrets/tls.crt
    targets:
      containers:
        - nginx

defaults:
  rotation:
    enabled: true
    strategy: restart

logging:
  level: info
  format: json

agent:
  cache: true
  watch:
    polling_interval: 5m
```

---

## Validate Your Config

Before deploying, always validate:

```bash
docker dso validate
# or with explicit path:
docker dso validate --config /etc/dso/dso.yaml
```

Exits `0` on success. Exits `1` with a descriptive error on parse or schema failure.
