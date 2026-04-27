# CLI Reference

Full command reference for the DSO CLI.  
All commands are available as `docker dso <command>` or `dso <command>`.

---

## Vault Initialization

### `dso init`

Initializes the local encrypted vault at `~/.dso/vault.enc`.

```bash
docker dso init
```

- Creates the vault directory and a fresh AES-256-GCM encrypted database.
- **Must NOT be run as root.** The vault must be owned by your user account.
  Running with `sudo` will print a clear error and exit.
- If the vault already exists, prints `"Vault already initialized"` and exits cleanly (idempotent).

---

## Secret Management

### `dso secret set <project>/<path>`

Stores a secret in the vault. Prompts for the value interactively (invisible input).

```bash
docker dso secret set app/db_pass
```

Pipe a value from stdin:
```bash
cat ./tls.key | docker dso secret set app/tls_key
```

### `dso secret get <project>/<path>`

Retrieves and prints a secret value to stdout.

```bash
docker dso secret get app/db_pass
```

Pipe to clipboard:
```bash
docker dso secret get app/db_pass | pbcopy
```

### `dso secret list [project]`

Lists all secret keys in the vault (values are never shown).

```bash
docker dso secret list app
```

---

## Environment Import

### `dso env import <file> [project]`

Batch-imports an existing `.env` file into the vault.

```bash
docker dso env import .env myapp
```

- Warns on duplicate keys.
- Validates syntax before importing.
- Original `.env` file is not modified or deleted.

---

## Deployment

### `dso up [docker compose args]`

The primary runtime command. Resolves secrets and deploys your stack.

```bash
docker dso up -d
docker dso up --build
docker dso up --mode=local   # Force local mode
docker dso up --mode=cloud   # Force cloud mode
```

**Mode detection order:**
1. `--mode` flag
2. `DSO_FORCE_MODE` environment variable
3. `/etc/dso/dso.yaml` exists → cloud
4. `dso-agent.service` systemd unit exists → cloud
5. Default → local

In **local mode**, DSO starts an inline in-process agent, resolves `dso://` and `dsofile://` references from the Native Vault, and passes the resolved compose file to Docker.

In **cloud mode**, DSO routes to the running `dso-agent` systemd service, which fetches secrets from your configured cloud provider.

### `dso down [docker compose args]`

Stops the running stack.

```bash
docker dso down
```

---

## System Commands

### `dso system setup`

> **Requires root.** Run with `sudo`.

Configures DSO for **Cloud Mode**. Run once after a global install.

```bash
sudo docker dso system setup
```

What it does, in order:
1. Creates `/etc/dso/` configuration directory.
2. Writes `/etc/systemd/system/dso-agent.service` (idempotent).
3. Downloads the provider plugin bundle for your OS/arch from the GitHub release.
4. Verifies the SHA256 checksum before extracting anything.
5. Cleans and repopulates `/usr/local/lib/dso/plugins/`.
6. Runs `systemctl daemon-reload && systemctl enable dso-agent && systemctl restart dso-agent`.

If any step fails, all written files are rolled back atomically.

Prints on success:
```
[DSO] ✅ Cloud mode configured successfully.
      Agent:   running (dso-agent.service)
      Plugins: installed to /usr/local/lib/dso/plugins
      Monitor: journalctl -u dso-agent -f
```

> **Non-Linux:** This command is only supported on Linux (systemd required).

---

### `dso system doctor`

Read-only diagnostics. Safe to run at any time, does not modify anything.

```bash
docker dso system doctor
```

Example output:
```
DSO System Diagnostics — v3.2.0
════════════════════════════════════════════════════════════════════
Component         Status     Detail
────────────────────────────────────────────────────────────────────
Binary            OK         /usr/local/bin/dso (v3.2.0)
Effective UID     1000
Detected Mode     LOCAL      Reason: default
Config            NOT FOUND  /etc/dso/dso.yaml
Vault             OK         /home/user/.dso/vault.enc
Systemd Service   NOT FOUND  File: ... | Runtime: not supported (non-Linux)
Plugin: vault     MISSING    (cloud mode only)
Plugin: aws       MISSING    (cloud mode only)
Plugin: azure     MISSING    (cloud mode only)
Plugin: huawei    MISSING    (cloud mode only)
════════════════════════════════════════════════════════════════════
```

**Status values:**
- `OK` — present and executable
- `MISSING` — file does not exist
- `INVALID` — file exists but is not executable

Plugin `MISSING` is **expected and normal** for Local Mode users.

---

## Utility Commands

| Command | Description |
| :--- | :--- |
| `dso version` | Print CLI version |
| `dso validate` | Validate a `dso.yaml` config file |
| `dso fetch <name>` | Manually fetch a secret (cloud mode) |
| `dso inspect <container>` | Inspect injected secrets for a running container |
| `dso logs` | View DSO agent logs |
| `dso watch` | Real-time monitor of secret rotations |
| `dso export` | Export secrets for CI/testing (local mode) |
| `dso diff` | Show config diff vs running stack |

---

## Plugin Reference

Plugins are used in Cloud Mode only and live at `/usr/local/lib/dso/plugins/`.

| Plugin binary | Status | Provider |
| :--- | :--- | :--- |
| `dso-provider-vault` | ✅ Fully implemented | HashiCorp Vault (KV v2) |
| `dso-provider-aws` | 🚧 Stub — not yet implemented | AWS Secrets Manager |
| `dso-provider-azure` | 🚧 Stub — not yet implemented | Azure Key Vault |
| `dso-provider-huawei` | 🚧 Stub — not yet implemented | Huawei Cloud DEW |

Stub plugins are distributed with each release so the installation system validates cleanly.  
If you invoke a stub, it exits with a clear error:
```
Error: DSO provider 'aws' is not yet implemented.
       Full AWS Secrets Manager support is planned for a future release.
```
