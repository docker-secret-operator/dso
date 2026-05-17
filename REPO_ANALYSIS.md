# Repository Analysis: Docker Secret Operator (DSO)

**Analysis Date:** May 17, 2026  
**Current Version:** v3.5.10  
**Language:** Go 1.25.0  
**Status:** Production-ready

---

## 📌 Executive Summary

**Docker Secret Operator (DSO)** is a production-grade runtime secret injection daemon for Docker and Docker Compose environments. It solves the critical operational problem of **rotating secrets in containerized applications safely without exposing them to the host filesystem or Docker's metadata layers**.

### Core Problem Solved
- **Secrets in environment variables** → Visible to `docker inspect`, debuggers, logs
- **Secrets on filesystem** → Persistent on disk, recovery risk, audit challenges
- **Manual rotation** → Error-prone, requires container restarts, downtime

### DSO Solution
- Secrets held **only in process memory and tmpfs** (never written to disk)
- **Automatic rotation** with blue-green deployment (zero downtime)
- **Multi-provider support** (Vault, AWS Secrets Manager, Azure Key Vault, local)
- **Deterministic rollback** on failed rotations
- **Event-driven** with webhook/polling support
- **No Kubernetes/Swarm required** — works with plain Docker Compose

---

## 📂 Repository Structure

```
dso/
├── cmd/
│   ├── dso/                      # Main CLI entrypoint
│   └── plugins/                  # Provider plugin definitions
│
├── internal/                      # Core application logic
│   ├── agent/                     # Secret rotation agent
│   ├── analyzer/                  # Configuration analysis
│   ├── audit/                     # Audit logging
│   ├── auth/                      # Authentication handling
│   ├── bootstrap/                 # Setup & initialization
│   ├── cli/                       # CLI command implementations
│   ├── compose/                   # Docker Compose parsing/handling
│   ├── core/                      # Core types & interfaces
│   ├── daemon/                    # Daemon/background service
│   ├── events/                    # Event handling system
│   ├── injector/                  # Secret injection engine
│   ├── providers/                 # Secret backend abstractions
│   ├── resolver/                  # Configuration resolution
│   ├── rotation/                  # Secret rotation logic
│   ├── runtime/                   # Runtime management, recovery
│   ├── server/                    # API server
│   ├── strategy/                  # Rotation strategies
│   ├── testing/                   # Test utilities
│   └── watcher/                   # File/secret change monitoring
│
├── pkg/                           # Reusable libraries
│   ├── api/                       # API definitions
│   ├── backend/                   # Backend implementations (file, vault)
│   ├── config/                    # Configuration management
│   ├── observability/             # Logging, metrics, tracing
│   ├── provider/                  # Provider interfaces
│   ├── schema/                    # Configuration schema
│   ├── security/                  # Encryption, redaction
│   └── vault/                     # Vault-specific logic
│
├── examples/                       # Working examples
│   ├── aws-compose/
│   ├── azure-compose/
│   ├── hashicorp-vault/
│   └── huawei-compose/
│
├── test/                           # Integration tests
│   ├── integration/
│   └── testutil/
│
├── scripts/                        # Install/setup scripts
├── docs/                           # Complete documentation
├── plugin/                         # Docker plugin definition
├── .github/                        # GitHub Actions CI/CD
├── Dockerfile                      # Container image
├── go.mod / go.sum                 # Dependency management
├── Makefile                        # Build targets
├── .goreleaser.yml                 # Release automation
└── CHANGELOG.md                    # Release notes
```

---

## 🏗️ Architecture Overview

### High-Level Design

```
Secret Backends
(Vault/AWS/Azure/Huawei/Local)
        ↓
    ┌─────────────────────────────────────┐
    │    DSO Agent (Single per Host)      │
    │                                     │
    │  • Event Watcher                    │
    │  • Rotation Engine                  │
    │  • Health Checks                    │
    │  • State Tracker                    │
    │  • Plugin Manager                   │
    │  • Container Orchestrator           │
    └─────────────────────────────────────┘
        ↓
    Docker Containers
    (with environment/file injection)
```

### Key Components

#### 1. **CLI Layer** (`internal/cli/`)
- Command routing and execution
- User-facing command implementations
- Setup wizards (local, agent, cloud-specific)
- Status/doctor commands
- Configuration management

#### 2. **Agent System** (`internal/agent/`)
- Core runtime daemon
- Manages secret lifecycle
- Coordinates container operations
- Handles rotation events

#### 3. **Rotation Engine** (`internal/rotation/`)
- Blue-green deployment strategy
- Health verification
- Rollback on failure
- Atomic container swap

#### 4. **Provider Abstraction** (`internal/providers/`, `pkg/provider/`)
- Plugin-based provider system
- Implementations: Local, Vault, AWS, Azure, Huawei
- Dynamic loading of provider plugins

#### 5. **Injection System** (`internal/injector/`)
- Environment variable injection
- File-based injection (tmpfs)
- Secret redaction in logs

#### 6. **Event Watcher** (`internal/watcher/`)
- Polling-based monitoring
- Webhook support
- Change detection and debouncing

#### 7. **Configuration System** (`pkg/config/`)
- YAML configuration parsing
- Validation and schema enforcement
- Multi-provider configuration
- Config discovery (search order)

#### 8. **Security Layer** (`pkg/security/`)
- AES-256-GCM encryption for local vault
- Log redaction/sanitization
- Credential masking

#### 9. **Observability** (`pkg/observability/`)
- Structured logging (Zap-based)
- Prometheus metrics
- Audit logging

#### 10. **Runtime Management** (`internal/runtime/`)
- Crash recovery
- State persistence
- Health monitoring

---

## 🔄 Data Flow: Secret Rotation

```
1. Secret Change Detection
   └─ Watcher polls backend or receives webhook

2. Validation
   └─ Verify secret format and dependencies

3. Pre-Rotation Preparation
   └─ Create tmpfs mount for new secret
   └─ Verify backend connectivity

4. New Container Deployment (Blue-Green)
   └─ Create new container with updated secret
   └─ Container waits for health checks

5. Health Verification
   └─ Run health checks (HTTP, TCP, custom)
   └─ Verify application readiness

6. Atomic Swap
   └─ Stop routing to old container
   └─ Rename old → stopped, new → active
   └─ Update Docker labels (blue/green tracking)

7. Cleanup
   └─ Remove old container
   └─ Clean tmpfs mounts
   └─ Update state tracking

8. Rollback on Failure
   └─ Any step failure → restore old container
   └─ Automatic retry with exponential backoff
```

---

## 🔐 Security Model

### Core Principle
**Plaintext secrets never touch the host filesystem.**

### Guarantees
1. **Memory-Only Secrets**: Held only in process memory and container tmpfs
2. **Encryption at Rest**: AES-256-GCM for local vault
3. **No Docker Metadata Exposure**: Secrets invisible to `docker inspect`
4. **Log Redaction**: Secrets never appear in logs
5. **Atomic Cleanup**: Secrets removed on container stop/restart
6. **Audit Trail**: All secret access logged (with redaction)

### Assumptions
- Docker daemon is trusted and secure
- Host filesystem is adequately secured
- Network communication uses TLS (for cloud providers)

---

## 📋 Configuration System

### Config Discovery Order (Priority)
1. CLI flag: `-c path/to/config.yaml`
2. System-wide: `/etc/dso/dso.yaml`
3. Current directory: `./dso.yaml`
4. Default fallback: `dso.yaml`

### Core Configuration Structure
```yaml
providers:
  vault:
    address: https://vault.example.com:8200
    auth:
      method: token
      token_env: VAULT_TOKEN
  
  aws:
    region: us-east-1

agent:
  watch:
    polling_interval: 5m
  cache:
    ttl: 1h
    max_size: 500Mi
  health:
    timeout: 30s
    
  rotation:
    strategy: rolling
    enabled: true
    
secrets:
  app/db_password:
    container: postgres
    env_var: POSTGRES_PASSWORD_FILE
```

---

## 🚀 Installation Modes

### 1. **Local Mode** (Development)
- User-level installation
- No root required
- Local encrypted vault
- Single-host operation

```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | bash
docker dso setup
```

### 2. **Agent Mode** (Production)
- System-wide installation (requires sudo)
- Systemd integration
- Cloud provider support
- Multi-user access

```bash
curl -fsSL https://raw.githubusercontent.com/docker-secret-operator/dso/main/scripts/install.sh | sudo bash
sudo docker dso bootstrap agent --enable-nonroot
```

### 3. **Cloud Provider Modes**
- Automatic provider detection
- Pre-configured for AWS/Azure/Vault/Huawei
- Provider plugin auto-installation

---

## 📦 Key Dependencies

### Core Libraries
- **Docker Client**: `github.com/docker/docker` (28.5.2+)
- **Cobra CLI**: Command-line framework
- **HashiCorp**: Vault API, plugin system
- **Cloud SDKs**: 
  - Azure: `azure-sdk-for-go`
  - AWS: `aws-sdk-go-v2`
  - Huawei: `huaweicloud-sdk-go-v3`
- **Encryption**: `golang.org/x/crypto` (AES-256-GCM)
- **Logging**: `go.uber.org/zap` (structured logging)
- **Observability**: `prometheus/client_golang` (metrics)
- **Config**: `gopkg.in/yaml.v3` (YAML parsing)

### Testing
- `testcontainers-go` (container-based tests)
- `stretchr/testify` (assertions)

---

## 🔧 Build & Release

### Build System
- **Build Tool**: Go native (`go build`)
- **Release Tool**: GoReleaser v2
- **Artifacts**: Multi-platform binaries
  - Linux (amd64, arm64)
  - macOS (amd64, arm64)
- **Provider Plugins**: Separate binaries for each provider

### Latest Release (v3.5.10)
```
Binaries:
  - docker-dso (main CLI)
  - dso-provider-aws (AWS plugin)
  - dso-provider-azure (Azure plugin)
  - dso-provider-vault (Vault plugin)
  - dso-provider-huawei (Huawei plugin)
```

---

## 📚 Documentation Structure

| Document | Purpose | Audience |
|----------|---------|----------|
| **README.md** | Project overview, quick start | Everyone |
| **docs/getting-started.md** | Installation & first deployment | New users |
| **docs/cli.md** | Complete command reference | Users |
| **docs/configuration.md** | YAML schema & options | Operators |
| **docs/providers.md** | Provider-specific setup | Cloud ops |
| **docs/architecture.md** | System design & internals | Developers |
| **docs/operational-guide.md** | Day-2 operations & monitoring | SREs |
| **SECURITY.md** | Threat analysis & guarantees | Security teams |
| **CHANGELOG.md** | Version history & changes | Release notes |

---

## 🧪 Testing

### Test Coverage
- **Unit Tests**: Config parsing, crypto, vault operations
- **Integration Tests**: Docker operations, rotation flows
- **Benchmarks**: Performance profiling

### Running Tests
```bash
make test           # Run all tests
make lint          # Linting with golangci-lint
make fmt           # Code formatting
make vet           # Go vet analysis
```

---

## 📝 Recent Changes (Last 10 Commits)

1. **v3.5.10** (2026-05-17): Setup wizard config generation fixes
2. **v3.5.8** (2026-05-17): Provider plugin build hooks refinement
3. **v3.5.7** (2026-05-17): Provider plugin release critical fix
4. **v3.5.6** (2026-05-17): Simplified setup wizard (2-3 min setup)
5. **v3.5.5** (2026-05-17): System setup command for provider installation
6. **v3.5.4** (2026-05-17): Complete automation verification
7. **v3.5.0** (2026-05-16): Crash recovery, observability, circuit breakers
8. **v3.4.2** (2026-05-16): Cloud provider auto-detection
9. **v3.4.1** (2026-05-15): Production hardening security/stability fixes
10. **v3.3.0** → **v3.4.0**: Version bump

---

## 🎯 Current State & Known Constraints

### Strengths
✅ Zero-persistence security model  
✅ Multi-provider architecture  
✅ Automatic crash recovery  
✅ Comprehensive observability  
✅ Production-ready systemd integration  
✅ Rich CLI with setup wizards  
✅ Blue-green rotation strategy  
✅ Event-driven architecture  

### Design Constraints (By Design)
⚠️ Single Docker host per agent (no multi-host coordination)  
⚠️ Local state persistence only (no distributed consensus)  
⚠️ File-based locking (~100s secrets max)  
⚠️ Not recommended for Kubernetes (use ExternalSecrets Operator)  
⚠️ Scales to single-host Docker Compose environments  

### Operational Notes
- Setup complexity reduced from 10-15 min to 2-3 min (v3.5.6+)
- Provider plugins auto-installed during setup
- Non-root support with automatic group configuration
- Comprehensive error messages and troubleshooting guidance

---

## 🚨 Next Steps for Issue Analysis

To proceed with identifying and fixing issues, I recommend:

1. **Run automated tests** to identify failing tests
2. **Check git status** for uncommitted changes or branches
3. **Review open GitHub issues** (if available)
4. **Validate configuration** with `docker dso config validate`
5. **Review error logs** from recent operations
6. **Check CI/CD pipeline** status (GitHub Actions)
7. **Identify specific pain points** or feature requests

**Current Analysis**: Ready to pivot to detailed issue investigation based on:
- Test failures
- Build errors
- Configuration issues
- Feature gaps
- Security concerns
- Performance bottlenecks
- Documentation gaps

---

**End of Repository Analysis**
