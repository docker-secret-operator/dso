# DSO Documentation Index (v3.4.2)

Complete guide to all Docker Secret Operator documentation.

---

## Quick Navigation

### 📖 First Time? Start Here

1. **[SETUP_GUIDE.md](docs/SETUP_GUIDE.md)** — Complete step-by-step setup for all scenarios
2. **[QUICKREF.md](docs/QUICKREF.md)** — Essential commands at a glance
3. **[getting-started.md](docs/getting-started.md)** — Beginner-friendly introduction

### 🏃 In a Hurry?

See [QUICKREF.md](docs/QUICKREF.md) for the most essential commands.

---

## Documentation by Use Case

### For Local Development

1. **[SETUP_GUIDE.md - Local Development Setup](docs/SETUP_GUIDE.md#local-development-setup)**
   - Install DSO
   - Initialize local environment
   - Create and manage secrets
   - Deploy with docker-compose

2. **[getting-started.md - Local Mode Setup](docs/getting-started.md#local-mode-setup)**
   - Quick setup steps
   - Example configurations

3. **[docker-compose.md](docs/docker-compose.md)**
   - How to integrate DSO with docker-compose.yaml
   - `dso://` vs `dsofile://` syntax

### For Production Deployment

1. **[SETUP_GUIDE.md - Production Setup](docs/SETUP_GUIDE.md#production-setup)**
   - Agent initialization
   - Cloud provider configuration (AWS, Azure, Vault, Huawei)
   - Systemd service management
   - Non-root access setup

2. **[operational-guide.md](docs/operational-guide.md)**
   - Day-2 operations
   - Monitoring and observability
   - Maintenance tasks
   - Disaster recovery

3. **[providers.md](docs/providers.md)**
   - Detailed provider setup guides
   - Authentication methods for each provider
   - Provider-specific best practices

### For Cloud-Specific Setup

- **AWS Secrets Manager** → [SETUP_GUIDE.md - AWS Section](docs/SETUP_GUIDE.md#aws-secrets-manager)
- **Azure Key Vault** → [SETUP_GUIDE.md - Azure Section](docs/SETUP_GUIDE.md#azure-key-vault)
- **HashiCorp Vault** → [SETUP_GUIDE.md - Vault Section](docs/SETUP_GUIDE.md#hashicorp-vault)
- **Huawei Cloud KMS** → [SETUP_GUIDE.md - Huawei Section](docs/SETUP_GUIDE.md#huawei-cloud-kms)

---

## Documentation Structure

```
📚 DSO Documentation

├── 🚀 Getting Started
│   ├── README.md                    # Main project overview
│   ├── SETUP_GUIDE.md              # Complete setup guide (ALL scenarios)
│   ├── QUICKREF.md                 # Quick command reference
│   ├── getting-started.md          # Beginner's introduction
│   └── DOCUMENTATION_INDEX.md       # This file
│
├── 📖 Core Guides
│   ├── cli.md                      # Complete CLI reference (all commands)
│   ├── configuration.md            # Configuration schema & options
│   ├── docker-compose.md           # Docker Compose integration
│   ├── providers.md                # Provider-specific setup
│   └── installation.md             # Detailed installation guide
│
├── 🏗️ Architecture & Design
│   ├── architecture.md             # System architecture & design
│   ├── concepts.md                 # Core concepts explained
│   ├── docker-plugin.md            # Docker plugin integration details
│   └── OPERATIONAL_LIMITATIONS.md  # Known limits & constraints
│
├── 📋 Operations & Maintenance
│   ├── operational-guide.md        # Day-2 operations, monitoring
│   └── CNCF_SANDBOX_APPLICATION.md # CNCF compliance details
│
├── 📚 Examples & Reference
│   ├── examples/                   # Working example configurations
│   │   ├── postgres.md            # PostgreSQL setup
│   │   ├── redis.md               # Redis setup
│   │   ├── node.md                # Node.js setup
│   │   ├── django.md              # Django setup
│   │   └── fullstack.md           # Full-stack example
│   └── examples/README.md          # Examples overview
│
└── 🔒 Security & Compliance
    ├── ../SECURITY.md              # Security model
    ├── ../THREAT_MODEL.md          # Threat analysis
    └── ../LICENSE                  # MIT License
```

---

## All Documentation Files

### Quick Reference & Getting Started

| File | Purpose | Audience |
|---|---|---|
| **[README.md](README.md)** | Project overview, key features | Everyone |
| **[DOCUMENTATION_INDEX.md](DOCUMENTATION_INDEX.md)** | Navigation guide (this file) | Everyone |
| **[SETUP_GUIDE.md](docs/SETUP_GUIDE.md)** | Complete step-by-step setup | New users |
| **[QUICKREF.md](docs/QUICKREF.md)** | Essential commands | Experienced users |
| **[getting-started.md](docs/getting-started.md)** | Beginner introduction | New to DSO |

### Core Documentation

| File | Purpose | Audience |
|---|---|---|
| **[cli.md](docs/cli.md)** | Complete command reference | All users |
| **[configuration.md](docs/configuration.md)** | Configuration schema & options | DevOps/Admin |
| **[installation.md](docs/installation.md)** | Detailed installation guide | New users |
| **[docker-compose.md](docs/docker-compose.md)** | Docker Compose integration | Developers |
| **[providers.md](docs/providers.md)** | Provider setup guides | Cloud users |

### Architecture & Design

| File | Purpose | Audience |
|---|---|---|
| **[architecture.md](docs/architecture.md)** | System design & internals | Developers/Contributors |
| **[concepts.md](docs/concepts.md)** | Core concepts explained | Everyone |
| **[docker-plugin.md](docs/docker-plugin.md)** | Docker plugin details | Contributors |
| **[OPERATIONAL_LIMITATIONS.md](docs/OPERATIONAL_LIMITATIONS.md)** | Known limits | DevOps/Admin |

### Operations & Examples

| File | Purpose | Audience |
|---|---|---|
| **[operational-guide.md](docs/operational-guide.md)** | Day-2 operations | DevOps/Admin |
| **[examples/](docs/examples/)** | Working configurations | Everyone |
| **[CNCF_SANDBOX_APPLICATION.md](docs/CNCF_SANDBOX_APPLICATION.md)** | CNCF compliance | Compliance/Architects |

---

## How to Use This Documentation

### "How do I...?"

| Task | Guide | Quick Link |
|---|---|---|
| Install DSO | [SETUP_GUIDE.md](docs/SETUP_GUIDE.md#installation) | [Link](docs/SETUP_GUIDE.md#installation) |
| Set up local dev | [SETUP_GUIDE.md](docs/SETUP_GUIDE.md#local-development-setup) | [Link](docs/SETUP_GUIDE.md#local-development-setup) |
| Set up production | [SETUP_GUIDE.md](docs/SETUP_GUIDE.md#production-setup) | [Link](docs/SETUP_GUIDE.md#production-setup) |
| Configure AWS | [SETUP_GUIDE.md](docs/SETUP_GUIDE.md#aws-secrets-manager) | [Link](docs/SETUP_GUIDE.md#aws-secrets-manager) |
| Configure Azure | [SETUP_GUIDE.md](docs/SETUP_GUIDE.md#azure-key-vault) | [Link](docs/SETUP_GUIDE.md#azure-key-vault) |
| Configure Vault | [SETUP_GUIDE.md](docs/SETUP_GUIDE.md#hashicorp-vault) | [Link](docs/SETUP_GUIDE.md#hashicorp-vault) |
| Use docker-compose | [docker-compose.md](docs/docker-compose.md) | [Link](docs/docker-compose.md) |
| Manage secrets | [cli.md](docs/cli.md) | [Link](docs/cli.md) |
| Monitor operations | [operational-guide.md](docs/operational-guide.md) | [Link](docs/operational-guide.md) |
| Troubleshoot issues | [SETUP_GUIDE.md - Troubleshooting](docs/SETUP_GUIDE.md#troubleshooting) | [Link](docs/SETUP_GUIDE.md#troubleshooting) |
| Understand architecture | [architecture.md](docs/architecture.md) | [Link](docs/architecture.md) |

### By Experience Level

**Beginner (New to DSO):**
1. Read [README.md](README.md) for overview
2. Follow [SETUP_GUIDE.md](docs/SETUP_GUIDE.md) step-by-step
3. Use [QUICKREF.md](docs/QUICKREF.md) for common commands
4. Try [examples/](docs/examples/) for your use case

**Intermediate (Using DSO):**
1. Reference [QUICKREF.md](docs/QUICKREF.md) for commands
2. Check [configuration.md](docs/configuration.md) for advanced options
3. Review [docker-compose.md](docs/docker-compose.md) for integration patterns
4. Use [operational-guide.md](docs/operational-guide.md) for day-2 tasks

**Advanced (Contributing/Extending):**
1. Study [architecture.md](docs/architecture.md) for design
2. Review [OPERATIONAL_LIMITATIONS.md](docs/OPERATIONAL_LIMITATIONS.md) for constraints
3. Check [providers.md](docs/providers.md) for plugin development
4. See [../SECURITY.md](SECURITY.md) for security considerations

---

## Setup Workflows

### Local Development (5 minutes)

```
README.md (overview)
    ↓
SETUP_GUIDE.md #Quick Start
    ↓
SETUP_GUIDE.md #Local Development Setup
    ↓
QUICKREF.md (commands)
    ↓
examples/ (try a config)
```

### Production with AWS (15 minutes)

```
README.md (overview)
    ↓
SETUP_GUIDE.md #Installation
    ↓
SETUP_GUIDE.md #Production Setup → AWS Secrets Manager
    ↓
QUICKREF.md (commands)
    ↓
operational-guide.md (monitoring)
```

### Production with Azure (15 minutes)

```
README.md (overview)
    ↓
SETUP_GUIDE.md #Installation
    ↓
SETUP_GUIDE.md #Production Setup → Azure Key Vault
    ↓
QUICKREF.md (commands)
    ↓
operational-guide.md (monitoring)
```

### Production with Vault (20 minutes)

```
README.md (overview)
    ↓
SETUP_GUIDE.md #Installation
    ↓
providers.md (Vault auth setup)
    ↓
SETUP_GUIDE.md #Production Setup → HashiCorp Vault
    ↓
QUICKREF.md (commands)
    ↓
operational-guide.md (monitoring)
```

---

## Documentation Versions

- **Current:** v3.4.2 (May 14, 2026)
- **Previous:** v3.4.1, v3.4.0, v3.3.0

See [CHANGELOG.md](CHANGELOG.md) for version history.

---

## Key Features by Topic

### Secret Management
- Storage: [configuration.md](docs/configuration.md)
- Retrieval: [cli.md](docs/cli.md#secret-commands)
- Rotation: [operational-guide.md](docs/operational-guide.md)

### Deployment
- Docker Compose: [docker-compose.md](docs/docker-compose.md)
- Setup: [SETUP_GUIDE.md](docs/SETUP_GUIDE.md)
- Monitoring: [operational-guide.md](docs/operational-guide.md)

### Providers
- Configuration: [configuration.md](docs/configuration.md)
- Setup: [providers.md](docs/providers.md)
- AWS: [SETUP_GUIDE.md](docs/SETUP_GUIDE.md#aws-secrets-manager)
- Azure: [SETUP_GUIDE.md](docs/SETUP_GUIDE.md#azure-key-vault)
- Vault: [SETUP_GUIDE.md](docs/SETUP_GUIDE.md#hashicorp-vault)
- Huawei: [SETUP_GUIDE.md](docs/SETUP_GUIDE.md#huawei-cloud-kms)

### Operations
- Day-2: [operational-guide.md](docs/operational-guide.md)
- Troubleshooting: [SETUP_GUIDE.md#troubleshooting](docs/SETUP_GUIDE.md#troubleshooting)
- Monitoring: [operational-guide.md](docs/operational-guide.md)

### Security
- Model: [SECURITY.md](SECURITY.md)
- Threat Analysis: [THREAT_MODEL.md](THREAT_MODEL.md)
- Design: [architecture.md](docs/architecture.md)

---

## Search Guide

**Looking for:**

- "How do I install?" → [SETUP_GUIDE.md#installation](docs/SETUP_GUIDE.md#installation)
- "What commands are available?" → [QUICKREF.md](docs/QUICKREF.md) or [cli.md](docs/cli.md)
- "How do I configure X?" → [configuration.md](docs/configuration.md)
- "How does it work?" → [architecture.md](docs/architecture.md) or [concepts.md](docs/concepts.md)
- "What are the limits?" → [OPERATIONAL_LIMITATIONS.md](docs/OPERATIONAL_LIMITATIONS.md)
- "How do I troubleshoot?" → [SETUP_GUIDE.md#troubleshooting](docs/SETUP_GUIDE.md#troubleshooting)
- "Is it secure?" → [SECURITY.md](SECURITY.md)
- "Show me examples" → [examples/](docs/examples/)

---

## Contributing

Documentation improvements are welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

---

## Support

- **Issues:** [GitHub Issues](https://github.com/docker-secret-operator/dso/issues)
- **Discussions:** [GitHub Discussions](https://github.com/docker-secret-operator/dso/discussions)
- **Security:** security@docker-secret-operator.org

---

## Version Info

| Component | Version | Release Date |
|---|---|---|
| **DSO** | v3.4.2 | 2026-05-14 |
| **Docs** | v3.4.2 | 2026-05-14 |
| **Go** | 1.21+ | Required for building |

---

**Last Updated:** 2026-05-14  
**Documentation Version:** 3.4.2
