# Changelog

All notable changes to this project will be documented in this file.

## [v3.2.0] - 2026-04-28

### Added

- **Dual-Mode Execution**: `dso up` now automatically detects and routes to Local Mode (Native Vault)
  or Cloud Mode (legacy systemd agent) based on flags, environment variables, and system state.
- **Native Vault** (`dso init`): Local AES-256-GCM encrypted vault at `~/.dso/vault.enc`.
  No cloud provider, no root, no systemd required.
- **Compose Resolver**: AST-level `dso://` and `dsofile://` secret injection into Docker Compose files.
- **Inline Agent**: In-process Docker event agent for Local Mode — no background process needed.
- **`dso system setup`**: Root-only command that installs the cloud-mode systemd service, downloads
  and SHA256-validates provider plugins, and activates the daemon automatically.
- **`dso system doctor`**: Read-only diagnostics showing binary path, detected mode, vault/config
  presence, systemd status, and per-plugin availability + version.
- **Selective plugin installation**: `dso system setup` supports installing specific providers via `--providers aws,vault` or interactive prompts.
- **Full Cloud Providers**: `aws`, `azure`, and `huawei` providers are fully implemented with official SDKs (replacing earlier stubs).
- **Plugin distribution**: All four providers are shipped as a unified, version-aligned tarball (`dso-plugins-{os}-{arch}-{version}.tar.gz`).
- **GoReleaser pipeline**: Multi-arch prebuilt binaries (Linux/macOS × amd64/arm64).
  No Go installation required on end-user machines.
- **SHA256 integrity validation**: Both `install.sh` and `dso system setup` verify checksums
  before writing any files to disk.
- **Atomic rollback**: `dso system setup` removes partial state on failure without destroying preserved plugins.

### Changed

- `install.sh` is now a thin delivery-only script (download + chmod + path placement).
  All system configuration is delegated to `dso system setup` and `dso init`.
- Mode detection priority: `--mode` flag → `DSO_FORCE_MODE` env → `/etc/dso/dso.yaml`
  existence → systemd service existence → default `local`.
- `dso init` now enforces non-root execution (vault must be user-owned, never root-owned).

### Fixed

- Legacy agent command kept as `dso agent` (unchanged) for zero-touch V2 systemd upgrades.
- PATH shadowing detection in `install.sh` warns when a global binary conflicts with local install.

---

## [v3.1.0] - Production Ready
### Added
- **Global CLI Migration**: Completed the transition to `docker dso` as the primary interface.
- **Multi-Provider V3.1 Schema**: Introduced the `providers` map and structured `defaults`/`rotation` blocks.
- **Reliability Enhancements**: Exponential backoff with jitter and checksum-based rotation are now standard.

## [v3.0.0] - Docker-Native Era
### Added
- **Docker CLI Plugin**: Renamed binary to `docker-dso` and added `docker-cli-plugin-metadata` compliance.
- **Native Compose Integration**: Added `docker dso up` which parses `docker-compose.yml`, safely fetches, and mounts custom `dso:` attributes natively.
- **Deep Rotation Support**: Secret rotation natively monitors events and automatically runs Best-Effort Rolling Restarts.
- **Restructured Repository**: Code architecture strictly aligned into `/cmd/docker-dso`, `/internal/cli`, `/internal/core`, `/internal/providers`, and `/internal/watcher`.

### Deprecated
- **Standalone `dso` command**: Kept temporarily for CI backward compatibility but will be removed in v4. Use `docker dso` instead.

---

## [v2.0.0] - Event-Driven Trigger Engine
### Added
- **Trigger Engine**: Shifted to hybrid event-driven Webhook APIs mapping (`POST /api/events/secret-update`) removing severe polling delays.
- **Rolling Restarts**: Fully integrated Docker-native health checks safely substituting environments seamlessly.
- **Real-Time WebSockets**: Live event streaming exposed formally.
- **Tmpfs Overlay File Bounds**: Enabled `inject: file` securely.

### Changed
- Refactored core cache and background agent logic natively.

---

## [v1.0.0] - Initial Release
### Added
- **Core CLI Runtime**: Standalone `dso format` wrapping `docker compose` calls correctly.
- **Multi-Cloud Backends**: Formal capabilities parsing AWS Secrets Manager, Azure Key Vault, HashiCorp Vault securely.
- **In-Memory Buffer Security**: Bypassed disk utilization natively mounting strings functionally.
