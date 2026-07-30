# DSO Roadmap

**Last Updated**: 2026-07-29
**Governance**: [GOVERNANCE.md](GOVERNANCE.md) · **Security**: [SECURITY.md](SECURITY.md) · **Contributing**: [CONTRIBUTING.md](CONTRIBUTING.md)

---

## Vision

Docker Compose has no first-class answer to a simple operational question: how do you rotate a secret in a running container without downtime, without writing it to disk, and without standing up an orchestrator you don't otherwise need?

Kubernetes has External Secrets Operator. Swarm has secrets primitives. Plain `docker compose` — what most small and mid-sized teams actually run in production — has neither, so secrets end up sitting in a `.env` file and rotation means a manual restart.

**DSO closes that gap**: cloud-provider secret management (AWS Secrets Manager, Azure Key Vault, HashiCorp Vault) and zero-downtime, health-checked rotation for any single Docker host — secrets held only in memory, never written to disk.

**Long-term vision**: make secret rotation as natural a part of `docker compose up` as networking or volumes are today.

---

## ⭐ Flagship Capability

**Zero-downtime secret rotation, with automatic health verification and rollback.**

When a secret changes, DSO starts a new container with the updated value, verifies it's healthy, swaps it in, and only then removes the old one — rolling back automatically if the new container fails its health check. Every other capability in DSO exists to support this one.

---

## Why DSO?

| Need | Traditional Approach | DSO |
|---|---|---|
| Docker Compose secret rotation | Manual restart | Automatic, health-checked rotation |
| Secret storage | `.env` files on disk | Cloud provider or encrypted local vault, memory-only |
| Applying a secret update | Redeploy | Zero-downtime rolling swap |
| Orchestration requirement | Kubernetes cluster for secret operators | Single Docker host, no orchestrator required |

---

## Current Status

DSO is under active development by a single lead maintainer, with a growing focus on production and security hardening.

### Available today

- **Local encrypted vault mode** — store and inject secrets from an encrypted local vault, no cloud provider required.
- **Cloud provider agent mode** — AWS Secrets Manager, Azure Key Vault, HashiCorp Vault, and Huawei Cloud.
- **Zero-downtime rolling rotation** — blue-green container swap with automatic health verification and rollback on failure.
- **Zero-persistence by design** — plaintext secrets are never written to disk; they live only in process memory and `tmpfs`.
- **Adaptive, event-aware secret change detection** — smart polling combined with Docker event triggers detects changes quickly without hammering provider APIs.
- **Setup, Doctor & Repair** — a guided setup wizard with automatic rollback on failure, 17+ diagnostic checks, and an automated repair engine.
- **Signed releases** — every release binary is signed (Sigstore/cosign, keyless).
- **CLI** — a full command surface (`setup`, `doctor`, `status`, `up`/`down`, `secret`, `apply`, `watch`, and more) with auto-generated, CI-verified reference documentation.

### In active development

- **Security & supply-chain hardening** — a recent, structured hardening pass closed several issues (log redaction, plugin integrity verification, path-validation safety); this work continues, including moving from advisory to enforced security scanning in CI and publishing a software bill of materials (SBOM) for releases.
- **Observability** — metrics, structured logging, and tracing for production operators. Not yet available; see Roadmap Phases below.
- **Provider plugin ecosystem** — the plugin architecture (isolated provider processes) is in place; the contributor-facing experience for adding a new provider is being improved.

### Planned

- Cross-distribution validated testing
- Webhook / provider-native event support for secret changes
- Optional web dashboard

If a capability isn't listed above as "Available," treat it as not yet ready for production reliance.

---

## Who DSO Is For

**Primary users**: platform and DevOps engineers, and small-to-mid engineering teams running production or staging workloads on a single Docker host with plain `docker compose` — not Kubernetes, not Swarm.

**Primary use cases**:
- Rotating database and API credentials in production without downtime or manual restarts.
- Replacing `.env`-file-based secret handling with a provider-backed, zero-persistence model.
- Short-lived secrets for staging and CI environments.
- Small infrastructure teams and self-hosters for whom running Kubernetes solely for secret rotation is disproportionate.

**Target environment**: a single Docker host — a VM, bare metal, or a single cloud instance — running Docker Compose.

**Where DSO fits**: between "nothing" (raw `.env` files, manual restarts) and a full Kubernetes control plane run solely for secret management.

---

## Design Principles

- **Zero persistence by default.** Secrets are never written to disk unless the local vault mode is explicitly chosen, and even then they're encrypted at rest.
- **Fail closed, not silent.** Integrity and verification checks (e.g., plugin binary verification) block the operation rather than degrade quietly.
- **Single host, no orchestrator required.** Features that would compromise this are out of scope.
- **Automatic recovery over manual intervention.** Rotation failures roll back automatically; setup failures unwind automatically.
- **Scope matched to maintenance capacity.** DSO prioritizes doing a small set of things reliably over a large surface area maintained inconsistently.

---

## Roadmap Phases

### Build Trust

The current focus: closing the gap between "works well" and "provably trustworthy in production."

1. **Enforced security scanning & supply-chain transparency** — move automated vulnerability and static-analysis scanning from advisory to build-blocking; publish a software bill of materials (SBOM) with every release. *Why now: this is the single highest-leverage investment for enterprise trust, and the tooling is already largely in place.*
2. **Cross-distribution validated testing** — automated install/upgrade/rotation testing across major Linux distributions (Ubuntu, Debian, Fedora, RHEL, Amazon Linux). *Why now: no amount of unit testing substitutes for proof that setup and rotation work on real target environments.*
3. **CLI & configuration consistency** — consistent flags (`--json`, `--verbose`, `--dry-run`) and error handling across every command; a single authoritative configuration guide. *Why now: consistency reduces support burden and is the fastest way to make the project feel mature.*

### Expand Ecosystem

4. **Observability** — Prometheus metrics, structured JSON logs, and distributed tracing for rotation and secret-fetch operations. *Why now: operators cannot run DSO with confidence in production without visibility into what it's doing.*
5. **Event-native secret detection** — webhook and provider-native event support (e.g., AWS EventBridge, Azure Event Grid) alongside today's adaptive polling, for near-instant rotation without waiting on a poll cycle.
6. **Provider plugin ecosystem** — a clearer path for the community to contribute new secret-provider integrations: a starter template, a documented local-development workflow, and a lower-friction registration process.
7. **Runtime intelligence** — richer `docker dso status` output: secret age, time to next rotation, and provider health at a glance.

### Scale Adoption

8. **Web dashboard** — an optional, read-only visual view of the same operational data already available via the CLI and REST API. Deferred until the observability work above lands.
9. **Broader release distribution** — Homebrew, Linux package repositories, and container images, in addition to today's install script.
10. **Community growth** — clear contribution paths, timely issue triage, and governance that scales with the size of the contributor community as it grows beyond a single maintainer.

---

## Release Philosophy

- **Patch releases** — bug fixes and security patches only, no breaking changes.
- **Minor releases** — new features, backward compatible.
- **Major releases** — the only place breaking changes happen, with a documented migration path.
- **Security fixes** — treated as out-of-band patch releases, prioritized above all other work.
- **Compatibility** — `dso.yaml` and CLI flags are kept stable within a major version; deprecations are announced at least one minor release ahead of removal.

---

## Enterprise Readiness

An honest snapshot for teams evaluating DSO for production or enterprise use:

| Capability | Status |
|---|---|
| Signed release artifacts | **Available** |
| Zero-persistence secret handling | **Available** |
| Automatic rotation with health-checked rollback | **Available** |
| Automated vulnerability & static-analysis scanning | **Available** (advisory today; enforced/build-blocking is a near-term goal) |
| Software bill of materials (SBOM) | **Planned** |
| Cross-distribution validated testing | **Planned** |
| Metrics, structured logs, tracing | **Planned** |
| Multi-maintainer governance | **Future** — DSO is currently maintained by a single lead maintainer; formal multi-maintainer governance will be established as the contributor base grows. See [GOVERNANCE.md](GOVERNANCE.md) for the target model. |

We'd rather list a capability as "Planned" than claim it's ready before it is.

---

## Success Metrics

Signals we track as indicators of project health, not marketing numbers:

- Rotation success rate in real-world usage
- Zero data-loss or secret-exposure regressions
- CI reliability (build/test pass rate on `main`)
- Growth in supported provider plugins
- Growth in active contributors beyond the founding maintainer
- Real-world reference deployments

---

## Adoption Goals

Goals for 2026, as a young, single-maintainer project — not commitments on a fixed timeline, but the outcomes we're working toward:

- First production deployment outside the founding team
- A second maintainer with commit access
- 10 community contributors
- 5 supported provider plugins
- At least one published reference architecture
- Cross-distribution validation running in CI

Progress will be reported as it happens.

---

## Community Goals

- Aim to respond promptly to issues and discussions, prioritizing security reports and bugs.
- Label issues clearly (`good-first-issue`, `help-wanted`, `bug`, `enhancement`) to make it easy to find a way to contribute.
- Publish real-world example stacks and short guides showing DSO solving concrete problems.
- Let community feedback — not guesswork — decide which providers, platforms, and roadmap items to prioritize next.
- Grow deliberately: DSO would rather have a small number of engaged contributors than a large, inactive one.

---

## What DSO Is Not

- **A Kubernetes-native tool** — DSO is for Docker Compose on a single host. If you're running Kubernetes, use External Secrets Operator.
- **A multi-tenancy or RBAC platform** — out of scope for the single-host model.
- **A certificate manager** — use Cert-Manager or your provider's certificate service.
- **A GitOps or deployment tool** — DSO manages secrets at runtime; it does not manage deployments.
- **A secrets generator** — DSO injects and rotates secrets; it does not create them.

---

## Resources

- **Issues**: [github.com/docker-secret-operator/dso/issues](https://github.com/docker-secret-operator/dso/issues)
- **Discussions**: [github.com/docker-secret-operator/dso/discussions](https://github.com/docker-secret-operator/dso/discussions)
- **Security disclosures**: md.umair@antiersolutions.com — see [SECURITY.md](SECURITY.md)
- **Contributing**: [CONTRIBUTING.md](CONTRIBUTING.md)

For a detailed internal engineering review (architecture, code quality, and technical-debt findings behind this roadmap's priorities), see `docs/audit/COMPREHENSIVE_REVIEW.md` in the repository — that document is written for contributors and maintainers, not as a public roadmap.

---

**Maintained by**: Umair (Project Lead)
**Last Updated**: 2026-07-29
**Next Review**: October 2026
