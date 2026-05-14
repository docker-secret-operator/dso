---
title: Documentation Home
nav_order: 1
description: "DSO is a Docker CLI plugin for cloud-native secret lifecycle management in Docker Compose environments."
permalink: /
---

# DSO Documentation

**Docker Secret Operator (DSO)** is a cloud-native infrastructure platform for managing secrets in Docker Compose environments—operating as a Docker CLI plugin.

- **Cloud-native**: Bootstrap, diagnose, monitor, operate—four phases of functionality
- **Docker plugin**: Runs as `docker dso <command>` seamlessly integrated with Docker
- **Production-ready**: Systemd integration, event-driven rotation, comprehensive monitoring
- **No Kubernetes required**: Works on any Docker host

---

## Quick Start

### 1. Install DSO as a Docker Plugin

```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | bash
```

### 2. Bootstrap Your Environment

**Local development:**
```bash
docker dso bootstrap local
```

**Production with systemd:**
```bash
sudo docker dso bootstrap agent
```

### 3. Check Health

```bash
docker dso doctor
```

### 4. View Status

```bash
docker dso status
```

For detailed setup, see [Getting Started](getting-started.md).

---

## Documentation Index

### Getting Started

- **[Getting Started](getting-started.md)** — Installation and setup  
  Install, bootstrap, verify health, configure, and deploy in minutes

### Architecture & Design

- **[System Architecture](architecture.md)** — Design overview, component architecture, operational modes  
  Understand how DSO integrates with Docker and secret providers

- **[Docker Plugin Integration](docker-plugin.md)** — Docker plugin discovery, installation, troubleshooting  
  How DSO works as a Docker CLI plugin with proper installation paths

- **[Runtime Operation](runtime.md)** — Agent lifecycle, systemd service, directory structures  
  Deep dive into how DSO runs as a long-lived service

### Operations & Maintenance

- **[CLI Reference](cli.md)** — Complete command reference with examples for all 4 phases  
  All `docker dso` commands documented

- **[Operational Guide](operational-guide.md)** — Day-2 operations, troubleshooting, maintenance  
  Health checks, monitoring, backup/recovery, scaling, performance tuning

### Configuration & Providers

- **[Configuration Reference](configuration.md)** — YAML configuration options  
  All settings documented with examples

- **[Providers](providers.md)** — Secret provider details  
  AWS, Azure, Vault, local file backend

- **[Docker Compose Integration](docker-compose.md)** — Using DSO with docker-compose.yaml  
  Secret injection, environment mapping, examples

### Security

- **[Security Model](../SECURITY.md)** — Security guarantees, encryption, access control  
  Threat model, trust boundaries, and security controls

### Examples

- **[Examples Directory](examples/)** — Ready-to-use configurations  
  AWS, Azure, Vault, local development examples

---

## The Four Phases

DSO provides four phases of functionality:

### Phase 1: Bootstrap

Initialize DSO environment for local development or production.

```bash
docker dso bootstrap [local|agent]
```

- **Local**: Create `~/.dso/` for non-root development
- **Agent**: Create `/etc/dso/` and systemd service for production

**See**: [CLI Reference - Phase 1](cli.md#phase-1-bootstrap)

### Phase 2: Doctor (Diagnose)

Check system health and provider connectivity.

```bash
docker dso doctor [--level full] [--json]
```

- Environment validation
- Provider connectivity checks
- System resource verification

**See**: [CLI Reference - Phase 2](cli.md#phase-2-doctor-environment-diagnostics)

### Phase 3: Status & Config (Monitor)

View real-time metrics and manage configuration.

**Status:**
```bash
docker dso status [--watch] [--json]
```

**Configuration:**
```bash
docker dso config [show|edit|validate]
```

**See**: [CLI Reference - Phase 3](cli.md#phase-3-status-real-time-monitoring)

### Phase 4: System (Operate)

Manage the systemd service (agent mode).

```bash
docker dso system [status|enable|disable|restart|logs]
```

- Enable/disable the service
- View live logs
- Monitor service health

**See**: [CLI Reference - Phase 4](cli.md#phase-4-system-systemd-service-management)

---

## Common Tasks

### Set Up Local Development

1. Install: `curl -fsSL https://... | bash`
2. Bootstrap: `docker dso bootstrap local`
3. Check: `docker dso doctor`
4. Configure: `docker dso config edit`
5. Deploy: `docker compose up`

**Full guide**: [Getting Started](getting-started.md)

### Set Up Production with Systemd

1. Install: `curl -fsSL https://... | sudo bash`
2. Bootstrap: `sudo docker dso bootstrap agent`
3. Check: `docker dso doctor`
4. Configure: `docker dso config edit`
5. Enable: `sudo docker dso system enable`
6. Deploy: `docker compose up`

**Full guide**: [Getting Started](getting-started.md)

### Monitor Running System

```bash
# Real-time metrics
docker dso status --watch

# Check health
docker dso doctor --level full

# Follow logs
docker dso system logs -f

# View last hour of errors
docker dso system logs --since 1h -p err
```

**Full guide**: [Operational Guide](operational-guide.md)

### Troubleshoot Issues

1. Check installation: `docker dso version`
2. Run diagnostics: `docker dso doctor --level full`
3. Review configuration: `docker dso config show`
4. Validate syntax: `docker dso config validate`
5. View logs: `docker dso system logs -f`

**Full guide**: [Operational Guide - Troubleshooting](operational-guide.md#troubleshooting)

---

## Documentation Organization

```
docs/
├── index.md                          # This file - navigation hub
├── getting-started.md                # Installation and setup
├── cli.md                            # CLI reference for all 4 phases
│
├── architecture.md                   # System design & components
├── runtime.md                        # Agent lifecycle & systemd
├── docker-plugin.md                  # Docker plugin details
│
├── operational-guide.md              # Day-2 operations
├── configuration.md                  # YAML config reference
├── providers.md                      # Secret provider details
├── docker-compose.md                 # Compose integration
│
├── concepts.md                       # Core concepts
├── examples/                         # Example configurations
│   ├── aws-compose/
│   ├── azure-compose/
│   ├── hashicorp-vault/
│   └── huawei-compose/
```

---

## Need Help?

- **New to DSO?** Start with [Getting Started](getting-started.md)
- **Troubleshooting?** See [Operational Guide - Troubleshooting](operational-guide.md#troubleshooting)
- **Want details?** Check [System Architecture](architecture.md)
- **Command reference?** See [CLI Reference](cli.md)
- **Configuration questions?** See [Configuration Reference](configuration.md)

---

## Key Concepts

- **Bootstrap**: Initialize DSO runtime (Phase 1)
- **Doctor**: Diagnose environment health (Phase 2)
- **Status**: Monitor metrics and manage configuration (Phase 3)
- **System**: Operate systemd service (Phase 4)
- **Local Mode**: Non-root, development-focused (`~/.dso/`)
- **Agent Mode**: Production, systemd-managed (`/etc/dso/`)
- **Provider**: External secret backend (AWS, Azure, Vault, etc.)
- **Rotation**: Automatic secret refresh from provider
- **Injection**: Placing secrets into container environment

For detailed concepts, see [Concepts](concepts.md).

---

## Support & Contributing

- **Bug reports**: [GitHub Issues](https://github.com/docker-secret-operator/dso/issues)
- **Discussions**: [GitHub Discussions](https://github.com/docker-secret-operator/dso/discussions)
- **Contributing**: See [CONTRIBUTING.md](../CONTRIBUTING.md)

---

## License

DSO is licensed under the [MIT License](../LICENSE).
