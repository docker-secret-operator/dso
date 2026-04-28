# Migration Guide: v3.0/v3.1 → v3.2

This guide covers the breaking changes and behavioral updates between DSO versions.

---

## What Changed in v3.2

### 1. Mode Detection is Now Cloud-First

**Before (v3.0/v3.1):** DSO defaulted to Local Mode. Cloud Mode required explicit `--mode=cloud` or a `dso.yaml` file in the working directory.

**Now (v3.2):** The detection order is:

1. `--mode` flag
2. `DSO_MODE` / `DSO_FORCE_MODE` env var
3. `/etc/dso/dso.yaml` → **Cloud**
4. `./dso.yaml` → **Cloud**
5. `~/.dso/vault.enc` → **Local**
6. Nothing → guided error

**Action required:** If you previously relied on `./dso.yaml` for Cloud Mode without a flag, this now works automatically. If you relied on the default being Local while `dso.yaml` was present in the working directory, you must either rename the file or explicitly pass `--mode=local`.

---

### 2. `docker dso agent` Renamed to `docker dso legacy-agent`

**Before:** `docker dso agent` started the background daemon directly.

**Now:** The command is `docker dso legacy-agent`. It is intended only for use by the systemd service unit. **Users should not call this directly.**

**Action required:** Update any scripts that call `docker dso agent` to use the systemd service lifecycle commands instead:

```bash
# Instead of: docker dso agent
# Use:
sudo systemctl start dso-agent
sudo systemctl stop dso-agent
sudo systemctl status dso-agent
```

---

### 3. Cloud Mode Requires Explicit System Setup

**Before (v3.0/v3.1):** The agent could be started ad-hoc with `docker dso agent`.

**Now (v3.2):** Cloud Mode requires the `dso-agent` systemd service to be installed and running. One-time setup:

```bash
sudo docker dso system setup
```

This downloads plugins, writes the systemd unit, and starts the daemon. It only needs to run once (or after a version upgrade).

---

### 4. `dsofile://` is Local Mode Only

**Before:** `dsofile://` was described as usable with any mode.

**Now:** `dsofile://` is explicitly **Local Mode only**. If you use `dsofile://` in a compose file while DSO is running in Cloud Mode, the deploy will fail immediately with a clear error:

```
Error: dsofile:// protocol is only supported in LOCAL mode.
```

**Action required:** Cloud Mode users should remove `dsofile://` references and rely on native provider injection via `dso.yaml` secret mappings.

---

### 5. Conflict Resolution: Cloud Wins

**Before:** Behavior was undefined if both `~/.dso/vault.enc` and `dso.yaml` existed.

**Now:** Cloud Mode always wins in a conflict. DSO prints a clear warning:

```
[DSO] ⚠️ Both local vault and cloud configuration detected. Defaulting to CLOUD mode.
```

**Action required:** If you want to run Local Mode on a system that also has a `dso.yaml`, pass `--mode=local` explicitly.

---

### 6. `docker dso up` Root Behavior Changed

**Before:** A root check blocked all Cloud Mode runs that were not root.

**Now:** DSO allows non-root Cloud Mode runs. It defers privilege checking to the socket connection — if connecting to `/var/run/dso.sock` returns a permission error, it prints a targeted message:

```
Error: Failed to connect to DSO background agent.
Reason: Permission denied accessing /var/run/dso.sock

Fix: Cloud mode requires elevated permissions to access the daemon.
Run next: sudo docker dso up
```

---

## Removed / Non-Functional Commands

The following commands existed in v3.0/v3.1 documentation but were never fully implemented. They now return `"not yet implemented"` explicitly:

| Command | Status |
|---|---|
| `docker dso apply` | 🚧 Stub — not implemented |
| `docker dso inject` | 🚧 Stub — not implemented |
| `docker dso sync` | 🚧 Stub — not implemented |

Do not use these in scripts or CI pipelines.

---

## New Commands in v3.2

| Command | Description |
|---|---|
| `docker dso system setup` | Install Cloud Mode (plugins + systemd) |
| `docker dso system doctor` | Diagnose entire installation |
| `docker dso env import <file> [project]` | Bulk-import `.env` into Local vault |
| `docker dso logs` | View agent logs (journald or REST API) |
| `docker dso inspect <container>` | Inspect secrets in a running container |
| `docker dso diff [stack]` | Show config vs. deployed stack differences |
| `docker dso export` | Export resolved secrets to a local file |

---

## Legacy Mode Reference (v3.0 / v3.1)

In v3.0/v3.1, DSO used a label-based injection model where containers were tagged with `dso.reloader` labels to control injection behavior. **This label system no longer exists in v3.2.**

In v3.2:
- Injection is controlled entirely by `dso://` and `dsofile://` URI patterns in `docker-compose.yaml` (Local Mode)
- Or by `secrets[].mappings` in `dso.yaml` (Cloud Mode)
- Container targeting uses `secrets[].targets.containers` in `dso.yaml`

If you have legacy compose files with `dso.reloader` labels, you can safely remove them — they are ignored by v3.2.
