# DSO Configuration Guide (V3.1)

The `dso.yaml` file is the central configuration for the Docker Secret Operator. This document provides a complete technical reference for all supported fields in the V3.1 schema.

---

## 🏗️ Root Structure

| Field | Description |
| :--- | :--- |
| `providers` | **(Required)** A map of secret backend configurations. |
| `secrets` | **(Required)** A list of secret mappings and their destinations. |
| `defaults` | (Optional) Shared defaults for injection and rotation. |
| `logging` | (Optional) Global logging settings. |
| `agent` | (Optional) Agent-specific lifecycle settings. |

---

## 📡 Providers (`providers`)

DSO V2 supports multiple concurrent providers.

```yaml
providers:
  aws-east:
    type: aws
    region: us-east-1
  vault-dev:
    type: vault
    address: http://127.0.0.1:8200
```

### Supported Provider Types:
- `aws`: AWS Secrets Manager
- `vault`: HashiCorp Vault
- `azure`: Azure Key Vault
- `huawei`: Huawei Cloud CSMS
- `file`: Local filesystem secrets

---

## 📦 Secret Mappings (`secrets`)

Defines how secrets from a provider are mapped into containers.

| Field | Description |
| :--- | :--- |
| `name` | The exact name/path of the secret in the provider. |
| `provider` | The name of the provider from the `providers` map. |
| `inject` | Structured injection config (`type`, `path`, `uid`, `gid`). |
| `rotation` | Structured rotation config (`enabled`, `strategy`, `signal`). |
| `targets` | **(New)** Filtering logic for container destination. |
| `mappings` | Key-value mapping from provider to container. |

### Injection (`inject`)
- `type`: `env` (default) or `file`.
- `path`: (Required for `file`) Destination inside the container.
- `uid/gid`: (Optional for `file`) Ownership of the injected file.

### Rotation (`rotation`)
- `enabled`: `true` or `false`.
- `strategy`: `restart`, `signal`, `rolling`, or `none`.
- `signal`: (Required for `signal` strategy) e.g., `SIGHUP`.

### Targeting (`targets`)
- `containers`: List of specific service names.
- `labels`: Map of exact labels to match on target containers.

---

## 🤖 Global Defaults (`defaults`)

Reduce repetition by defining shared settings.

```yaml
defaults:
  inject:
    type: env
  rotation:
    enabled: true
    strategy: restart
```

---

## 📡 Agent Settings (`agent`)

| Field | Description |
| :--- | :--- |
| `cache` | `true` or `false` (Enable in-memory RAM cache). |
| `watch.polling_interval` | frequency of checks (e.g. `5m`, `1h`). |

---

## 📈 Logging (`logging`)

| Field | Description |
| :--- | :--- |
| `level` | `debug`, `info`, `warn`, `error`. |
| `format` | `json` or `text`. |
