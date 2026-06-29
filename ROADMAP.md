# DSO Roadmap — v4.0 Production Readiness

**Last Updated**: June 2026  
**Current Version**: v3.5.18  
**Status**: Active — transitioning from feature development to production hardening

---

## Where We Are

DSO has a working, well-tested foundation:

| Subsystem | Status |
|-----------|--------|
| Local mode (encrypted vault) | ✅ Complete |
| Agent mode (cloud providers: AWS, Azure, Vault, Huawei) | ✅ Complete |
| Zero-downtime rolling rotation | ✅ Complete |
| Crash recovery and state persistence | ✅ Complete |
| Setup engine (Detect → Validate → Plan → Preview → Apply → Rollback) | ✅ Complete |
| Doctor diagnostics (17+ named checks) | ✅ Complete |
| Automated repair engine | ✅ Complete |
| Security hardening (panic isolation, permission validation, rollback safety) | ✅ Complete |
| Unit + integration tests (426+) | ✅ Complete |
| CI/CD pipeline with vulnerability scanning | ✅ Complete |

The setup subsystem is **feature complete**. Further investment there provides diminishing returns.

---

## What Comes Next — v4.0 Roadmap

The following phases move DSO from a well-built internal tool to a production-grade, community-ready project. They are ordered by impact.

---

### Phase A — CLI Polish ⭐⭐⭐⭐

Every command should feel consistent and complete.

Target commands:
```
docker dso setup
docker dso doctor
docker dso repair
docker dso status
docker dso version
docker dso logs
docker dso config
docker dso provider
```

Each command should have:
- `--json` for machine-readable output
- `--verbose` for debugging
- Consistent error messages (same format, same exit codes)
- Help text with working examples
- `--dry-run` where it makes sense

**Why now**: The engine is solid but the surface is uneven. A polished CLI reduces support burden and makes the project look credible.

---

### Phase B — Configuration Management ⭐⭐⭐⭐

Configuration is currently a first-class feature of the setup engine but not of the daily workflow. Build it out:

```bash
docker dso config show       # Pretty-print current config with section headers
docker dso config validate   # Validate YAML schema + connectivity checks
docker dso config edit       # Open in $EDITOR with validation on save
docker dso config export     # Export sanitized config (secrets redacted)
docker dso config import     # Apply a config file
docker dso config reset      # Restore defaults (with confirmation)
```

**Why now**: Configuration mistakes are the most common source of support requests. Making them easy to catch and fix early reduces friction significantly.

---

### Phase C — Provider Plugin Framework ⭐⭐⭐⭐

The current provider system works but adding a new provider requires forking the core binary. Define a clean interface:

```go
type Provider interface {
    Detect(ctx context.Context) (bool, error)
    Validate(ctx context.Context, cfg ProviderConfig) error
    Health(ctx context.Context) (*HealthResult, error)
    Watch(ctx context.Context, paths []string) (<-chan SecretEvent, error)
    Fetch(ctx context.Context, path string) ([]byte, error)
    Rotate(ctx context.Context, path string) error
}
```

Target layout:
```
providers/
    aws/
    vault/
    azure/
    gcp/          ← new
    bitwarden/    ← new
    onepassword/  ← new
```

**Why now**: Provider extensibility is frequently requested. A clean plugin interface makes community contributions viable.

---

### Phase D — Watcher Engine ⭐⭐⭐⭐⭐

This is what makes DSO genuinely differentiated. The full reactive pipeline:

```
Secret changed (provider)
        ↓
Event received (polling / webhook / provider push)
        ↓
Determine affected containers
        ↓
Rolling update (blue-green swap)
        ↓
Health check
        ↓
Complete or rollback
```

Support three watch modes:
- **Polling** — timer-based, configurable interval (existing)
- **Webhook** — provider pushes change events to DSO
- **Provider events** — native provider event streams (AWS EventBridge, Azure Event Grid)

**Why now**: This is the technical differentiator. Without a solid watcher, DSO is a one-shot injector, not a secret operator.

---

### Phase E — Runtime Intelligence ⭐⭐⭐⭐

`docker dso status` should give operators a clear operational picture:

```
$ docker dso status

Containers (3 managed)
  ✓ app          healthy    secrets: 2    last rotation: 4m ago
  ✓ postgres     healthy    secrets: 1    last rotation: 4m ago
  ⚠ redis        degraded   secrets: 0    no secrets configured

Secrets (3 active)
  ✓ database_credentials    provider: aws    age: 12d    next rotation: 18d
  ✓ api_keys                provider: aws    age: 3d     next rotation: 27d
  ✓ tls_cert                provider: vault  age: 89d    expires: 1d  ⚠

Provider Health
  ✓ aws     reachable    latency: 42ms
  ✓ vault   reachable    latency: 18ms
```

**Why now**: Operators need to see system state at a glance. This also makes the doctor/repair workflow much more useful.

---

### Phase F — Observability ⭐⭐⭐⭐

Expose structured data that operations teams can actually use:

**Prometheus metrics**:
```
dso_setup_duration_seconds
dso_secret_fetch_duration_seconds
dso_rotation_duration_seconds
dso_rotation_total{status="success|failed"}
dso_provider_errors_total{provider="aws|vault|azure"}
dso_doctor_check_status{check_id="DSO-DOCTOR-001",status="pass|warn|fail"}
dso_secret_age_seconds{secret="name"}
```

**Structured logs** (JSON):
```json
{"level":"info","event":"rotation_complete","secret":"db_creds","duration_ms":1240,"strategy":"rolling"}
```

**OpenTelemetry traces** for rotation workflows.

**Why now**: Without metrics, operators are flying blind. This is also what enterprise users need before they'll adopt DSO.

---

### Phase G — Web Dashboard ⭐⭐⭐

The event system and status engine provide everything a dashboard needs. Build a lightweight read-only web UI that consumes the existing REST API:

```
Overview  →  Containers  →  Secrets  →  Providers  →  Health  →  Events
```

Serve it from the agent on a configurable port. No external dependencies.

**Why now**: Dashboards lower the barrier for non-CLI users and make the project more approachable. Deferred until Phase E and F are solid because the dashboard is only as good as its data sources.

---

### Phase H — Cross-Distribution Integration Tests ⭐⭐⭐⭐⭐

The setup engine and repair workflows need to be validated on real Linux distributions under real privilege boundaries:

Target distributions:
- Ubuntu 22.04
- Ubuntu 24.04
- Debian 12
- Fedora 40
- Rocky Linux 9 / RHEL
- Amazon Linux 2023

Test scenarios per distribution:
```
Fresh install (local mode)
Fresh install (agent mode)
Upgrade from previous version
Rollback after failed apply
Doctor → detect issue
Repair → fix issue
Rotation (provider mock)
Docker restart
System reboot
```

Run these in CI against fresh VMs or containers using GitHub Actions with matrix builds.

**Why now**: This is the highest-confidence signal that DSO actually works. No amount of unit tests replaces this.

---

### Phase I — Security & Supply Chain ⭐⭐⭐⭐⭐

This directly addresses the feedback received from the last security review:

**Signed releases** (Cosign):
```bash
cosign verify-blob --certificate dso-linux-amd64.cert --signature dso-linux-amd64.sig dso-linux-amd64
```

**SBOM** (CycloneDX format):
```bash
docker dso version --sbom
```

**SLSA Build Provenance** — generate verifiable provenance for every release artifact.

**Reproducible builds** — same source + same toolchain = byte-identical binary.

**Automated scanning in CI**:
- `govulncheck` — Go vulnerability database
- `gosec` — static security analysis
- `trivy` — image and dependency scanning
- Dependency review on every PR

**Why now**: Supply chain security is increasingly a prerequisite for enterprise adoption and sandbox program consideration. This is the single highest-leverage investment after cross-distribution testing.

---

### Phase J — Documentation ⭐⭐⭐

Produce documentation that a new user can follow without asking questions:

- **Architecture Guide** — how the components fit together, with diagrams
- **Developer Guide** — how to build, test, and contribute
- **Provider SDK** — how to write a custom provider plugin
- **CLI Reference** — every command, flag, and exit code documented
- **Troubleshooting Guide** — the 20 most common failure modes and their solutions
- **Security Model** — what DSO protects, what it doesn't, and why
- **Upgrade Guide** — how to move between versions safely
- **Migration Guide** — moving from local mode to agent mode

**Why now**: Good documentation is the single biggest force multiplier for a solo maintainer.

---

### Phase K — Community ⭐⭐⭐⭐

Without community, technical quality alone doesn't build adoption:

- Respond to every issue within 48 hours
- Label issues clearly (`good-first-issue`, `help-wanted`, `bug`, `enhancement`)
- Write tutorials (blog posts, videos) showing real use cases
- Create example projects that people can clone and run
- Post in Docker community forums, Reddit, and relevant Slack communities
- Track which providers and platforms users care about

**Why now**: Community feedback tells you which Phase C providers to build first, which Phase H distributions matter most, and which Phase J docs are missing. Without community signal, you're guessing.

---

### Phase L — Release Engineering ⭐⭐⭐

Automate everything around a release:

- GitHub Actions release pipeline (triggered by tag)
- Nightly builds with automated test matrix
- Homebrew formula for macOS
- Official Docker images (`docker pull docker-secret-operator/dso`)
- APT repository for Debian/Ubuntu
- RPM repository for Fedora/RHEL
- Checksums and signatures for every artifact
- GitHub Release notes auto-generated from conventional commits

**Why now**: Manual releases are error-prone and don't scale. Release automation also enables the nightly test matrix for Phase H.

---

## Priority Order

| Priority | Phase | Impact |
|----------|-------|--------|
| ⭐⭐⭐⭐⭐ | D — Watcher Engine | Makes DSO a true secret operator, not just an injector |
| ⭐⭐⭐⭐⭐ | H — Cross-distribution E2E tests | Production confidence; no substitute for real environments |
| ⭐⭐⭐⭐⭐ | I — Security & supply chain | Signed releases, SBOM, SLSA provenance |
| ⭐⭐⭐⭐ | A — CLI polish | Consistent, professional user experience |
| ⭐⭐⭐⭐ | F — Observability | Metrics, traces, structured logs |
| ⭐⭐⭐⭐ | C — Provider plugin framework | Extensibility; enables community contributions |
| ⭐⭐⭐⭐ | K — Community | Adoption and real-world feedback |
| ⭐⭐⭐⭐ | B — Config management | Reduces support burden |
| ⭐⭐⭐ | E — Runtime intelligence | Better operational visibility |
| ⭐⭐⭐ | J — Documentation | Force multiplier for solo maintainer |
| ⭐⭐⭐ | G — Web dashboard | Accessibility for non-CLI users |
| ⭐⭐⭐ | L — Release engineering | Scales the release process |

---

## What Is Not On This Roadmap

To stay focused, DSO explicitly does **not** plan to:

- **Kubernetes-native support** — DSO is for Docker Compose on a single host. Kubernetes has ExternalSecrets Operator.
- **Multi-tenancy / RBAC** — Out of scope for the single-host model.
- **SSL/TLS certificate management** — Use Cert-Manager or your provider's certificate service.
- **GitOps / infrastructure management** — DSO manages secrets at runtime, not deployments.
- **Secrets generation** — DSO injects secrets; it does not create them.

---

## Completed Work (June 2026)

### Setup Engine (Phases 1–10)
- Detect → Validate → Plan → Preview → Apply → Rollback pipeline
- Immutable `InstallPlan` with declarative operations
- Transactional apply with before/after snapshots
- Automatic rollback on failure
- Doctor engine: 17 named checks across 6 categories
- Repair engine: risk-gated actions (safe / moderate / destructive)
- Post-repair verification loop
- 426+ unit + integration tests
- 13 performance benchmarks
- Panic-safe event system

### Security Hardening (Tracks A & B)
- Panic/crash fixes across core packages
- Permission validation on all filesystem operations
- Rollback safety for partial failures
- Rate limiting middleware on REST API
- Vulnerability scanning in CI (`govulncheck`)

---

## Resources

- **Issues**: [github.com/docker-secret-operator/dso/issues](https://github.com/docker-secret-operator/dso/issues)
- **Discussions**: [github.com/docker-secret-operator/dso/discussions](https://github.com/docker-secret-operator/dso/discussions)
- **Security**: md.umair@antiersolutions.com
- **Contributing**: [CONTRIBUTING.md](CONTRIBUTING.md)

---

**Maintained by**: Umair (Project Lead)  
**Last Updated**: June 2026  
**Next Review**: September 2026
