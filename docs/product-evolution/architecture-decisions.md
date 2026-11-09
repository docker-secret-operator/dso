# Architecture Decisions (proposed)

**Date:** 2026-11-07
**Status of every ADR below: PROPOSED — needs owner approval.** None has been implemented. Findings `PE-xx`: `current-architecture-audit.md`. Backlog `BL-xx`: `implementation-backlog.md`.

---

## ADR-001 — Product identity: runtime delivery controller for existing secret managers, Compose-first

- **Context:** Doppler and Infisical own the store/rotation-generation/RBAC/audit layers and gate rotation behind paid tiers; neither documents Compose-aware container replacement (`competitive-analysis.md` §3). DSO's unique code is the Compose lifecycle path; its newest code (central store) competes head-on (PE-21).
- **Decision:** DSO is a **runtime secret delivery and rotation controller for Docker Compose** that consumes existing secret managers (Option C identity, Option B surface; scores 66 vs 56 vs 21 in `product-strategy.md` §2). DSO does not become a system of record, a credential generator, an IdP or an RBAC system.
- **Consequences:** provider breadth (incl. Doppler, Infisical) becomes a priority (BL-14); correctness of rotation is the product (Phase 1); README positioning changes (`product-strategy.md` §7).
- **Alternatives rejected:** A (full platform) — differentiation, security responsibility and single-maintainer sustainability all lowest. Pure B with its own store — duplicates competitors' core and increases secret custody.

## ADR-002 — Freeze `dso central` at its experimental scope

- **Context:** 10,412 non-test lines, all recent commits, not on the delivery path; M2 (network API, authn/authz for remote callers) would make DSO a secret server (`docs/central-store-m1.md:3-7`).
- **Decision:** keep the code and its docs; finish or park the in-progress crash-recovery work at the owner's discretion (it is untouched by this exercise); **no further central milestones** (no network API, no remote callers, no RBAC) without a new ADR. Label it experimental and "not used by the agent" in docs. Do not remove it.
- **Consequences:** frees roughly half of recent engineering capacity for Phase 0–1. Users wanting a store are pointed to Vault/Infisical/Doppler or the local vault.
- **Revisit when:** a concrete user need appears that no supported provider can meet.

## ADR-003 — Persisted runtime version ledger is the source of truth for "what is running"

- **Context:** in-memory hash baseline causes mass re-rotation on start (PE-03) and cannot answer "which version is in which container" (PE-14).
- **Decision:** extend the existing state directory (`/var/lib/dso/state`, `internal/agent/state_tracker.go`) with `AppliedRecord`s keyed by (service, container, secret). Store version ids or `sha256:` digests, never values. Change detection compares provider version to `applied_version`. First run with no record adopts current values (no rotation) unless `rotate_on_start: true`.
- **Consequences:** agent restart/upgrade becomes a no-op; enables `rotation status` and runtime audit (BL-26).
- **Not chosen:** reuse the central SQLite store — couples the runtime to a frozen experimental component and adds a heavy dependency for a tiny dataset.

## ADR-004 — "Zero-downtime" is a per-service, computed contract, not a default claim

- **Context:** PE-04..PE-09 and audit §C.3 show zero-downtime holds only under specific conditions.
- **Decision:** introduce `Assess()` (eligibility) applied to every strategy; `rolling` on stateful volumes or non-proxy fixed host ports is BLOCKED unless explicitly overridden; implicit fallback from failed `rolling` to `restart` is removed unless configured; README and `rotation plan` state the conditions.
- **Consequences:** some existing explicit-`rolling` configurations become BLOCKED (breaking behaviour, announced in CHANGELOG with override). Fewer surprises in production.

## ADR-005 — Provider interface v2 is additive and optional

- **Context:** `pkg/api/plugin.go:19-33` has no version metadata; the precedent `SecretProviderWithContext` shows optional interfaces work across the go-plugin boundary (`pkg/provider/provider.go:62-95` handles unknown-method fallback).
- **Decision:** add optional `VersionedProvider.GetSecretVersion(ctx, name) (VersionInfo, error)`; agent falls back to content hash when unsupported. Every call carries a deadline. No change to `Init`/`GetSecret` signatures; old plugin binaries keep working.
- **Consequences:** cheaper polling, real version ids in the ledger, webhooks can carry version ids.

## ADR-006 — Separate the traffic proxy from the controller process

- **Context:** the proxy lives in the agent (`internal/cli/agent.go:130-144`); any agent restart is a data-plane outage for proxy-owned ports (PE-10).
- **Decision:** move the proxy into a separately supervised `dso proxy` process with no Docker socket and no secret values, controlled over a root-only local socket and persisting its backend table. Keep in-process mode as a fallback until the Phase 3 soak passes.
- **Consequences:** a second systemd unit and protocol to maintain; the agent can then be upgraded under load. Highest-risk item in the roadmap (XL).
- **Not chosen:** leave as-is and document — acceptable only as an interim state (documented in Phase 0).

## ADR-007 — Tiered IPC authorization; IPC can never inject provider configuration

- **Context:** PE-11 (`internal/agent/server.go:204-266`, `internal/providers/store.go:40-68`).
- **Decision:** IPC methods are tiered:

| Method class | root / agent UID | `dso` group | others |
|---|---|---|---|
| status, list (names only), events | allow | allow | deny |
| trigger rotation (audited) | allow | allow | deny |
| read secret value | allow | only if listed in `ipc.secret_readers` for that secret/project | deny |
| provider connectivity check, reload config | allow | deny | deny |

  Provider clients created for checks are never cached; the cache key includes a config digest; the legacy one-off provider path is removed or root-only.
- **Consequences:** non-root `docker dso apply` pre-flight changes behaviour → one-release deprecation flag.

## ADR-008 — Local vault stays, positioned for development and self-hosting

- **Context:** constraint: do not remove the local vault; it serves the `.env`-replacement persona.
- **Decision:** keep `pkg/vault` and local mode; document it as dev/self-host; in Phase 4 run it through the same controller as agent mode (ADR-009) as just another provider.

## ADR-009 — One runtime

- **Context:** two runtimes with different injection mechanisms and failure policies (PE-18, PE-17).
- **Decision:** converge local mode onto `TriggerEngine`/rotation controller; delete `agent.Agent`'s stub reactor path once parity tests pass (BL-19).

## ADR-010 — Explicitly out of scope for v1

RBAC/SSO/SCIM, approval workflows, multi-host coordination, Kubernetes, dynamic-secret issuing, policy engine, hosted control plane, remote API for `dso central`. Each was evaluated against ADR-001; each either duplicates what the upstream secret manager already provides or breaks the single-host, single-maintainer model. Consuming dynamic-secret *leases* from a provider may be revisited after v1.

---

## Owner decision table

| Decision | Recommendation | Status |
|---|---|---|
| Product identity (ADR-001) | Option C identity via Option B Compose surface | Needs approval |
| `dso central` (ADR-002) | Freeze at experimental scope; no M2 | Needs approval |
| Version ledger (ADR-003) | Extend state tracker; no rotation on start by default | Needs approval |
| Zero-downtime contract (ADR-004) | Computed per service; block unsafe `rolling` | Needs approval |
| Provider interface v2 (ADR-005) | Additive optional interface | Needs approval |
| Proxy process split (ADR-006) | Separate supervised process, Phase 3 | Needs approval |
| IPC tiers (ADR-007) | Tiered authz, no caller provider configs | Needs approval |
| Local vault (ADR-008) | Keep, dev/self-host positioning | Needs approval |
| Single runtime (ADR-009) | Converge in Phase 4 | Needs approval |
| v1 exclusions (ADR-010) | As listed | Needs approval |
| Rename "Docker Secret Operator" | Keep name now; change tagline; revisit at 1.0 after trademark check | Needs approval |
| Unlocated findings F-E1, F-H1, AF-2, AF-7, PA-8 | Owner to supply source document; tracked as open | Needs owner input |
| P-01 policy (from register) | Still awaiting explicit owner acceptance | Needs approval |
