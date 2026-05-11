# DSO V3.2 Project Structure Analysis
This document provides a clean overview of the updated file structure generated after implementing the Native Vault, CLI plugins, and documentation across the `docs-update` branch. 

### Core Implementation (V3.2 Architecture)
```text
.
├── cmd/
│   ├── docker-dso/main.go               # Official Docker CLI Plugin Entrypoint
│   └── plugins/                         # Pluggable backends
│       └── dso-provider-vault/main.go
├── internal/
│   ├── agent/                           # Native Vault Runtime Agent (Zero-persistence injection)
│   │   ├── agent.go
│   │   └── cache.go
│   ├── cli/                             # CLI Commands (Native Vault UI)
│   │   ├── secret.go                    # 'dso secret set/get/list' & 'dso env import'
│   │   ├── up.go                        # Orchestrates AST resolution & Agent boot
│   │   └── stubs.go                     # Fallback commands
│   ├── compose/
│   │   └── ast.go                       # Docker Compose YAML AST parsing engine
│   ├── injector/
│   │   └── inject.go                    # Tar-stream builder and direct memory injector
│   └── resolver/
│       └── resolve.go                   # Resolves dso:// and dsofile:// URI patterns
├── pkg/
│   └── vault/                           # Local Encrypted Native Vault 
│       ├── crypto.go                    # AES-256-GCM + Argon2id encryption logic
│       └── vault.go                     # Atomic state management & deduplication
└── scripts/
    └── install.sh                       # Updated Installer (Systemd components removed for Native Vault)
```

### Documentation Structure (New)
```text
.
└── docs/
    ├── getting-started.md               # Quickstart guide & installation
    ├── concepts.md                      # Explains dsofile://, URIs, and the Agent model
    ├── cli.md                           # Exhaustive reference for native vault CLI commands
    ├── docker-compose.md                # Explains file vs env injection in compose yaml
    ├── security.md                      # Threat model & zero-persistence architecture rationale
    └── examples/                        # Real-world integration use cases
        ├── postgres.md                  # Secure DB password file-injection
        ├── redis.md                     # Custom command interception for legacy images
        ├── node.md                      # Node.js fs.readFileSync strategy
        ├── django.md                    # Environment injection for Python/Django settings
        └── fullstack.md                 # Complete multi-tier orchestration
```
