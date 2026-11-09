# Product Strategy — Identity, Positioning and Developer Experience

**Date:** 2026-11-07
**Inputs:** `competitive-analysis.md` (research 2026-11-07), `current-architecture-audit.md` (findings `PE-01..PE-27`), prior audit `docs/reviews/2026-10-24-dso-complete-product-audit.md`.
**Decision record:** ADR-001 and ADR-002 in `architecture-decisions.md`. Status of every decision here: **proposed — needs owner approval.**

---

## 1. Re-evaluating the prior recommendation

The prior recommendation (as relayed by the owner; the source document was not found in the repository) was **Option C — runtime secrets delivery agent integrating with existing platforms**, with **Option B — Docker Compose-focused rotation** as the first milestone.

Fresh evidence, examined without assuming it must be kept:

1. **The competitors leave the gap the recommendation targets.** Neither Doppler nor Infisical documents a Compose-aware, health-gated container replacement. Doppler's own restart-on-change (`doppler run --watch`) is Team+ and documented as able to cause downtime in production, with the Kubernetes Operator recommended instead (competitive-analysis D5). Infisical's Compose page describes start-time injection only (I5). → *supports C/B.*
2. **The competitors already own the store, rotation-generation, RBAC, audit and leasing layers** and fund them commercially (D3, I3). → *argues hard against A.*
3. **Actual engineering effort has drifted toward Option A.** The `dso central` store is 10,412 non-test lines (bigger than the whole 9,010-line delivery path) and absorbed every commit from 2026-10-29 to 2026-11-06, yet nothing in the agent reads from it (PE-21). → *the prior recommendation was right but is not being followed in practice.*
4. **The wedge itself is less finished than the README claims.** PE-01, PE-03, PE-05, PE-08, PE-10, PE-16 are all in the rotation/delivery core. → *the first milestone must be correctness of B, not new features.*
5. **"Integrates with existing platforms" is currently untrue for the two platforms DSO is compared with.** There is no Doppler or Infisical provider. → *C needs those providers to be credible.*

**Verdict:** keep the *direction* (C as identity, B as the first product surface), but **change three things** relative to the prior recommendation:
- **Freeze `dso central`** at its current experimental scope (finish/land the in-progress recovery work if the owner wishes, then no M2 network API, RBAC or remote callers). ADR-002.
- **Make the first milestone "rotation you can trust", not "rotation with more features"** — fix PE-01/03/05/08/10/16 and narrow claims before anything new.
- **Add Doppler and Infisical (and GCP) providers early**, because "alongside, not instead of" is the adoption story.

## 2. Option scoring

Scale 1–5, higher is better for DSO (so "engineering complexity 5" = *low* complexity). Weights reflect a single-maintainer OSS project.

| Criterion (weight) | A: Full platform vs Doppler/Infisical | B: Compose secrets + rotation platform (own store optional) | C: Runtime delivery agent for existing platforms |
|---|---|---|---|
| Differentiation (×3) | 1 — same category, 2 years behind | 4 — Compose-native lifecycle is unique | 4 — same wedge, plus "works with what you already pay for" |
| Engineering complexity (×2) | 1 — store, API, IdP, RBAC, HA | 3 | 4 — no server, no tenancy |
| Security responsibility (×2) | 1 — DSO becomes system of record for secrets | 3 — still tempted to hold secrets at rest (local vault, central) | 4 — secrets stay in the provider; DSO holds them only in memory / tmpfs |
| Maintenance burden (×2) | 1 | 3 | 4 — provider plugins are the main growth surface, and they are isolated |
| Adoption potential (×2) | 2 — must displace incumbents | 3 — `.env` users | 4 — Doppler/Infisical/AWS/Vault users on Compose are a ready audience |
| Reuse of existing code (×1) | 3 — central store exists | 5 | 4 — central store largely unused |
| Single-maintainer sustainability (×3) | 1 | 3 | 4 |
| Time to production-credible (×2) | 1 | 3 | 3 — same correctness work as B, plus 2–3 providers |
| **Weighted total (max 85)** | **21** | **56** | **66** |

**Decision (proposed): Option C as the product identity, shipped through Option B's Compose surface.** In one sentence: *DSO is the Compose runtime controller that takes secrets from the secret manager you already use and gets them into running containers safely — including rotations — without image changes.*

What this explicitly is **not**: a secret store, a credential generator, an IdP, an RBAC system, a Kubernetes tool, a multi-host orchestrator.

## 3. Target user

- **Primary:** a platform/DevOps engineer or small team running production or staging on one or a few Docker hosts with plain `docker compose`, who already uses (or is choosing) AWS Secrets Manager, Azure Key Vault, Vault, Doppler or Infisical, and who today handles rotation by hand (restart, redeploy) or not at all.
- **Secondary:** self-hosters and homelab operators replacing `.env` files (served by local mode).
- **Not targeted:** Kubernetes users (use ESO / vendor operators), multi-tenant platforms, regulated environments needing approval workflows (use the store's own controls).

## 4. Value proposition and differentiators

**Value proposition:** "Your secret manager rotates the value. DSO makes sure every Compose service that uses it is running the new value — health-checked, rolled back on failure, recorded — without rebuilding images or adding a sidecar per service."

**Defensible differentiators (after Phase 1–2 fixes):**
1. **Container-level, Compose-aware delivery** with health gating and rollback (unique per competitive-analysis §3).
2. **Runtime version ledger:** which secret version is running in which container, since when, with what health result (needs ADR-003; no competitor tracks this for Compose).
3. **Provider-agnostic, including the two "competitors":** a team on Doppler or Infisical gets Compose rotation without upgrading to the plan that unlocks `--watch`/rotation hooks, and without a vendor CLI inside every image.
4. **No vendor control plane in the data path,** single static binary + isolated provider plugins.
5. **Compose tooling** around it: `migrate` from `.env`, `validate` in CI, `doctor`/`repair`, and (proposed) `rotation plan` that tells you, per service, whether a zero-downtime swap is possible and why not.

**Non-differentiators to stop advertising as such:** free rotation (price is not a moat; correctness is), "Production-Ready" (`README.md:42`) until Phase 2 exits, "<5s detection" (`README.md:32`, PE-18), "zero-downtime by default" (PE-04).

## 5. Use cases (in priority order)

1. Provider-side rotation (AWS SM rotation Lambda, Doppler/Infisical rotated secrets, Vault KV update) → DSO re-delivers to every dependent Compose service with a health gate.
2. Replace `.env` with a provider: `dso migrate` → `dso validate` in CI → `dso up`.
3. Emergency credential revocation: operator updates the secret in the provider, triggers `dso rotate <secret>` (or provider webhook), sees per-container outcome.
4. Audit question "is anything still running the old credential?" answered from the runtime ledger.
5. Local development with the local vault (unchanged, dev-only positioning).

## 6. Features: retain, simplify/defer, remove

| Retain and invest | Simplify / defer | Candidates to remove or freeze (owner decision) |
|---|---|---|
| Provider plugin architecture (isolated processes, env allow-list) | Web UI: keep read-mostly, no new pages until Phase 3 | `dso central` beyond current scope → **freeze** (ADR-002) |
| Rolling/restart/signal/none strategies and the careful swap in `rolling_strategy.go` | `auto` strategy: keep, but make its reasoning visible via `rotation plan` | Docker secret-driver socket (PE-19) → verify, then remove if dead |
| In-process proxy as an opt-in, clearly documented component (PE-10) | SmartPoller / Event Reactor stubs → delete or wire (PE-18) | Dead `system doctor` (PE-22) |
| Crash recovery, ownership labels, owner IDs | Notification webhooks: keep, no new destinations | |
| Inbound HMAC webhook (PE-24) | Local mode: keep for dev, unify with agent runtime over time (ADR-009) | |
| Local vault (dev/self-host; **not** removed — explicit constraint) | Huawei provider: keep, community-maintained | |
| setup / doctor / repair / validate / migrate | Dynamic secrets: consume leases later (Phase 4+), never issue | |
| Signed releases, SBOM, govulncheck gate | | |

## 7. README positioning (proposed copy, not applied)

> **DSO — runtime secret delivery and rotation for Docker Compose.**
> Keep your secrets in AWS Secrets Manager, Azure Key Vault, HashiCorp Vault, Doppler or Infisical. DSO delivers them into your Compose services at runtime and, when they change, replaces the affected containers with health checks and automatic rollback — no image changes, no sidecars, no Kubernetes.
>
> **Zero-downtime rotation is available for stateless services with a health check whose host ports are owned by DSO.** Everything else uses a restart. `docker dso rotation plan` tells you which you'll get, per service, before anything happens.

Plus an honest "When not to use DSO" box (Kubernetes, multi-host, stateful zero-downtime, approval workflows).

## 8. Onboarding / quickstart improvements

| Problem today | Evidence | Improvement |
|---|---|---|
| Three overlapping quickstarts in README | `README.md:47-146` | One quickstart per persona: "Already on a secret manager" (agent mode) and "Just replacing .env" (local mode) |
| Example configs that do not work as written | PE-15 (Vault README form; example uses non-loopback http) | Every example in `examples/` validated by `dso validate` in CI |
| User cannot predict downtime | PE-04/05/06 | `dso rotation plan` (BL-08) with per-service verdict and reason |
| Startup rotates everything | PE-03 | Fixed by ADR-003; quickstart then shows "no rotation needed" on restart |
| Proxy concept appears late and implicitly | `README.md:387` | Explicit "port ownership" step with a diagram and the PE-10 trade-off |
| Default strategy surprise | PE-04 | Make `dso setup` write the strategy explicitly into `dso.yaml` |
| No "try it in 2 minutes with Doppler/Infisical" path | no providers | Ship providers + a `examples/doppler-compose` and `examples/infisical-compose` |

## 9. Name and branding

"Docker Secret Operator" has two problems: "Operator" signals Kubernetes (where "operator" is a term of art, and where DSO explicitly does not go), and "Docker" in a product name carries trademark-use risk (not legally assessed here). The acronym **DSO** and the `docker dso` CLI-plugin command are established and worth keeping.

**Recommendation (needs owner approval):** do **not** rename now — a rename costs docs, install scripts, image names and SEO while the product is still pre-1.0 in correctness terms. Change the **tagline** to "runtime secret delivery and rotation for Docker Compose" immediately, and revisit the long name at 1.0 (e.g. "DSO — Deploy-time Secret Orchestrator" or simply "DSO") after a trademark check.

## 10. Why teams would choose DSO *alongside* Doppler/Infisical

- Their secret manager already stores, versions, audits and (on paid tiers) generates rotated credentials. What it does not do for Compose is replace running containers safely when a value changes; both vendors point to Kubernetes for that (D5) or leave it to the user (I5).
- DSO adds that layer without changing images, without a vendor CLI and token inside every container, and without moving secrets out of the manager they trust.
- DSO adds a runtime record (what is running where) that the store cannot see.
- If they later move to Kubernetes, they keep the secret manager and drop DSO — DSO never becomes the lock-in point. That property is deliberate and is part of the pitch.
