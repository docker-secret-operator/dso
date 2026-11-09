# Competitive Analysis — Doppler vs Infisical vs DSO

**Research date:** 2026-11-07 (all web sources accessed this day)
**Branch / HEAD:** `feature/webui-phase1` @ `5bcb8bb` (plus uncommitted central-store work that this analysis did not touch)
**Companion documents:** `product-strategy.md` (direction decision), `current-architecture-audit.md` (finding IDs `PE-xx`), `architecture-decisions.md` (ADRs).

## 1. Method and evidence rules

- Every competitor claim carries a source URL and one of four labels:
  - **Confirmed** — stated on an official vendor page or doc page fetched on 2026-11-07.
  - **Partial** — officially supported, but with stated limits (plan gating, platform restriction, "may cause downtime", etc.).
  - **Not found in official docs** — searched for and not found on the official pages reached. This is *not* a claim that the vendor cannot do it.
  - **Not independently verified** — seen only in a search snippet, a third-party page, or an aggregator, or the official page could not be fetched.
- DSO claims are grounded in source code (file:line) or explicitly marked as documentation-only. A DSO feature is only "Implemented" if the code path is reachable from the shipped binary.
- Nothing here says "X cannot do Y" without a source. Where a page is silent, the cell says so.
- Pricing pages change often. Plan gating below is as rendered on 2026-11-07 and was read through a page-to-text conversion; comparison-table checkmarks can be lost in that conversion (Doppler's page in particular — see §6).

## 2. Sources

| Ref | URL | What it was used for |
|---|---|---|
| D1 | https://docs.doppler.com/docs/secrets-rotation | Rotation model, two-secret strategy, plan, how apps pick up rotations |
| D2 | https://docs.doppler.com/docs/dynamic-secrets | Dynamic secrets / leases |
| D3 | https://www.doppler.com/pricing | Plan gating |
| D4 | https://docs.doppler.com/docs/accessing-secrets | `doppler run`, `--mount` named pipe, env-var warnings |
| D5 | https://docs.doppler.com/docs/automatic-restart | `doppler run --watch`, production-downtime caveat |
| D6 | https://www.doppler.com/integrations/docker-compose | Official Compose integration options |
| D7 | https://docs.doppler.com/docs/versioning | Versioning, log-based rollback |
| D8 | https://docs.doppler.com/changelog/june-2026 | "Doppler On-prem released" |
| D9 | https://www.doppler.com/guides/managing-secrets-at-scale/teams-managing-secrets | Service-account identities / OIDC (via search snippet) |
| D10 | https://docs.doppler.com/docs/dockerfile , https://docs.doppler.com/docs/kubernetes-doppler-cli-in-docker | CLI-in-image pattern |
| I1 | https://infisical.com/docs/documentation/platform/secret-rotation/overview.md | Dual-phase vs single-phase rotation |
| I2 | https://infisical.com/docs/integrations/secret-rotations | Rotation provider list (via search snippet) |
| I3 | https://infisical.com/pricing | Plan gating |
| I4 | https://infisical.com/docs/integrations/platforms/infisical-agent | Agent: auth methods, templates, polling, `execute` |
| I5 | https://infisical.com/docs/integrations/platforms/docker-compose | Official Compose integration |
| I6 | https://infisical.com/docs/documentation/platform/audit-logs | Audit log classes, streaming, plan |
| I7 | https://infisical.com/docs/integrations/secret-syncs/overview | Secret syncs |
| I8 | https://infisical.com/docs/cli/commands/run | `infisical run --watch` (via search snippet) |
| I9 | https://infisical.com/docs/integrations/platforms/docker-swarm-with-agent | Sidecar-agent pattern (search snippet only; page returned HTTP 404 on fetch) |
| I10 | https://github.com/Infisical/infisical (and release aggregators) | Release cadence (aggregator data only — Not independently verified) |
| X1 | https://docs.docker.com/reference/cli/docker/container/cp/ | Docker cannot copy into tmpfs mounts (used for finding PE-08) |

## 3. How each product actually reaches a running Docker Compose container

This is the dimension that matters most for DSO, so it is separated out.

| Question | Doppler | Infisical | DSO (code-grounded) |
|---|---|---|---|
| Official Compose pattern | (a) `doppler run -- docker compose up` (host-side env injection, then Compose `${VAR}` interpolation) or (b) bake the CLI into the image and run `doppler run -- app` as the container command. **Confirmed** [D6, D10] | Bake CLI into each image and wrap the start command with `infisical secrets-manager run`; machine-identity token passed via env. **Confirmed** [I5]. Sidecar agent writing to a shared volume: **Not independently verified** [I9 snippet only] | Host-side agent mutates containers via the Docker API: env injection into a recreated container's `Config.Env` or file injection; no app-image changes required (`internal/watcher/controller.go:754+`, `internal/rotation/container_clone.go:38-145`). **Implemented** |
| Requires changing the application image | Pattern (b) yes; pattern (a) no but values land in container env (visible in `docker inspect`) | Yes for the documented pattern [I5] | No |
| What happens when a secret changes | `doppler run --watch` restarts the wrapped *process*; Team/Enterprise only; docs warn it "can result in downtime" in production and recommend the Kubernetes Operator instead. **Partial** [D5]. Webhooks on rotation so you can restart apps yourself. **Confirmed** [D1] | `infisical run --watch` restarts the command with new env. **Not independently verified** (snippet) [I8]. Agent `templates[].config.execute.command` runs a command on change. **Confirmed** [I4]. Compose page: "doesn't describe live secret updates"; restart required. **Confirmed (by silence on official page)** [I5] | Agent polls provider, detects change by content hash, recreates the container (strategies `rolling`/`restart`/`signal`/`none`) with health gate and rollback (`internal/agent/trigger.go:257-402`, `internal/rotation/rolling_strategy.go:185-440`). **Implemented, with correctness caveats PE-01..PE-10** |
| Zero-downtime container swap for Compose | **Not found in official docs** | **Not found in official docs** | Implemented **conditionally**: only stateless services, with Docker HEALTHCHECK, and with host ports owned by DSO's in-process TCP proxy. See `current-architecture-audit.md` §C |
| Secret material on the host filesystem | `--mount` uses a named pipe cleaned up on exit; docs call it the only secure file method. **Confirmed** [D4] | Agent renders templates to files on a destination path; permissions are operator's job. **Confirmed** [I4] | Cloud mode: no host file by design; local mode writes resolved compose to a `0600` temp file (accepted risk F-01). File-mode secrets in the rotation path may land in the container writable layer — **PE-08, unverified, high** |

**Takeaway:** neither competitor ships a Compose-aware *container lifecycle controller*. Both stop at "get the value into the process at start time" and "restart something when it changes". That gap is real, narrow, and is DSO's only defensible wedge. It is also exactly the part of DSO that has the most open correctness findings.

## 4. Feature-by-feature comparison

Legend for DSO column: **Impl** = implemented and reachable; **Partial** = implemented with material gaps; **Exp** = experimental / not wired into delivery; **Missing**; **Out of scope** = deliberately excluded in `ROADMAP.md`.
"DSO could improve on it?" = whether DSO can *meaningfully* do better for its target user, not whether it could copy the feature.

| # | Capability | Doppler | Infisical | DSO today | DSO could meaningfully improve on it? |
|---|---|---|---|---|---|
| 1 | Central secret store (projects/envs) | Confirmed — core SaaS product; On-prem Enterprise-only since 2026 [D3, D8] | Confirmed — MIT core, self-hostable [I3] | **Exp** — `dso central` local SQLite store, no network API, no authn/authz (`docs/central-store-m1.md:3-7`); not used by the agent (no reference to `centralsvc` outside `internal/centralsvc`, `internal/store`, `internal/cli/central.go`) | **No.** Both competitors are years ahead and fully staffed. See ADR-002 |
| 2 | Secret versioning & rollback | Confirmed — every version kept; log-entry rollback for add/update/delete [D7] | Partial — "versioning and point-in-time recovery" on Pro+ [I3] | **Partial** — provider-side only (Vault `?version=` pin, `cmd/plugins/dso-provider-vault/main.go:84-115`); DSO keeps no applied-version record per container (PE-14). `dso central` has versions but is not on the delivery path | **Yes, narrowly:** "which secret version is running in which container, and roll the *container* back to the previous version" is something neither competitor tracks for Compose |
| 3 | Scheduled secret rotation (generating new credentials) | Partial — Team+; two-secret (dual-credential) strategy; proxied rotation via AWS Lambda on Enterprise [D1, D3] | Partial — dual-phase for most providers, single-phase warned as disruptive; Pro "public databases only", full on Advanced+ [I1, I3] | **Out of scope** — "DSO injects and rotates secrets; it does not create them" (`ROADMAP.md:194`). DSO "rotation" = rolling *delivery* of a changed value | **No** — consume provider/competitor rotation instead (ADR-001) |
| 4 | Delivery of rotated values into running workloads | Partial — K8s Operator redeploys; webhooks; `--watch` restart with downtime caveat [D1, D5] | Partial — K8s operator (not researched in depth); agent `execute.command`; `run --watch` (Not independently verified) [I4, I8] | **Partial** — real Compose-aware recreate with health gate/rollback; correctness gaps PE-01..PE-10 | **Yes — this is the wedge.** Health-gated, rollback-capable, Compose-aware delivery |
| 5 | Dynamic secrets / leases | Partial — Enterprise only; CLI requests leases, default TTL 30m [D2, D3] | Partial — Advanced+ [I3]; agent has `dynamicSecret` template function [I4] | **Missing** | Possibly later, as *consumer* of competitor leases (renew/re-deliver), not as issuer. Not v1 |
| 6 | Machine identity / workload auth | Confirmed — service tokens; service accounts + OIDC identity on Team+ [D3, D9] | Confirmed — universal-auth, kubernetes, azure, gcp-id-token, gcp-iam, aws-iam in agent [I4] | **Partial** — DSO authenticates *to* providers with whatever the plugin supports (AWS default chain incl. IAM role; Azure `DefaultAzureCredential`; Vault **static token only**, PE-15). DSO has no identity model of its own for consumers; any `dso` group member can read any secret over IPC (PE-11) | Yes, for the host: least-privilege per-project scoping of the local IPC socket. Not a cloud IdP |
| 7 | RBAC | Partial — Team+ roles; custom roles add-on / Enterprise [D3] | Partial — Pro+; custom roles Enterprise-enforced from 2026-06-01 per aggregator (Not independently verified) [I3, I10] | **Out of scope** (`ROADMAP.md:191`) | No |
| 8 | Approval workflows / change requests | Partial — Team "Change Requests", Enterprise policies [D3] | Partial — Enterprise for secrets access requests [I3] | **Out of scope** | No |
| 9 | Audit logs | Partial — 3 days Developer, 90 days Team, custom Enterprise [D3] | Partial — Pro+; self-host needs Enterprise license; SIEM streaming [I6] | **Partial** — rotation/fetch events via `internal/audit/audit.go` (metadata only, verified-good F-05) and in-memory event stream; `dso central` has a hash-chained audit log, not on the delivery path | **Yes, narrowly:** a *runtime* audit ("container X started serving secret version V at T, health result H") that the store-side audit logs cannot see |
| 10 | Webhooks / change notification | Confirmed — webhooks on rotation/change [D1, D3] | Not researched in depth | **Impl** — outbound notification webhooks (`internal/notify`); inbound HMAC-signed, replay-protected change webhook (`internal/server/webhook_auth.go`) — verified good (PE-24) | Yes: consume Doppler/Infisical webhooks as a push trigger instead of polling |
| 11 | Secret sync to other stores | Confirmed — integration syncs [D1] | Confirmed — secret syncs [I7] | **Missing** — DSO is a sink, not a source | No (not DSO's job) |
| 12 | CLI | Confirmed [D4] | Confirmed [I5, I8] | **Impl** — large CLI (`internal/cli`, 34 files) incl. setup/doctor/repair/validate/migrate | Partially — Compose-specific `validate` / `migrate` / `doctor` are genuinely useful and not offered by either |
| 13 | SDKs | Confirmed (not enumerated here) | Confirmed (not enumerated here) | **Missing** — not needed for DSO's model (no app changes) | No |
| 14 | Docker Compose integration depth | Partial — env interpolation or CLI-in-image [D6] | Partial — CLI-in-image [I5] | **Impl/Partial** — labels, compose generation with tmpfs (`internal/compose/ast.go:62-90`, `internal/core/compose.go:202-292`) | **Yes** |
| 15 | Kubernetes | Confirmed — operator [D1] | Confirmed — operator (not researched in depth) | **Out of scope** (`ROADMAP.md:190`) | No |
| 16 | Self-hosting | Partial — On-prem, Enterprise only, released mid-2026 [D8] | Confirmed — MIT core free; Enterprise features need license [I3] | **Impl** — everything is self-hosted; no control plane | Yes — "no vendor control plane in the data path" remains true and is structural |
| 17 | HA / multi-host | Not researched | Not researched | **Out of scope** — single host per agent (`README.md:600-610`) | No for v1 |
| 18 | Observability (metrics) | Not researched | Not researched | **Impl** — Prometheus on separate localhost listener (`pkg/observability/metrics.go:122-160`, wired at `internal/cli/agent.go:224`); readiness is not modeled (PE-26) | Yes: per-rotation metrics are a natural DSO output |
| 19 | Release integrity / supply chain | Not found in official docs (not searched in depth) | Not found in official docs (not searched in depth) | **Impl** — cosign keyless signing, syft SBOM v1.48.0 pinned (`.github/workflows/release.yml:42-57`); plugin SHA-256 manifest (integrity, not authenticity — PE-20) | Modest |
| 20 | Pricing posture for rotation-into-runtime | Team+ for rotation and `--watch` [D3, D5] | Pro (public DB only) / Advanced+ for full rotation [I3] | Free, Apache-2.0 | Yes, but price is not a moat by itself; correctness is |

## 5. What each competitor does better than DSO (honest list)

**Doppler does better at:** being a managed, multi-user source of truth (projects/configs/environments, change requests, roles, SSO/SCIM); credential *generation* with a dual-credential strategy [D1]; dynamic secret leases [D2]; a hardened file-delivery primitive (`--mount` named pipe) [D4]; a polished SaaS experience with on-prem for Enterprise [D8].

**Infisical does better at:** an open-source self-hostable store with versioning/PITR [I3]; a broad rotation catalogue with explicit dual-phase semantics [I1, I2]; machine-identity auth methods across clouds [I4]; audit logs with SIEM streaming [I6]; outbound secret syncs [I7]; an agent with template rendering and on-change hooks [I4].

**DSO does better at (today, with caveats):** acting on the *container* rather than the process — recreating a Compose service with new values, gating on Docker health, rolling back, and owning host ports so traffic can be moved. Neither competitor's official docs describe this for Compose. DSO also requires no image changes and no outbound vendor dependency.

## 6. Research limitations

- Doppler's pricing page comparison-table checkmarks did not survive text extraction; "Not listed" in §4 means not seen in the extracted text.
- Infisical Kubernetes operator, Doppler Kubernetes operator, and both SDK catalogues were not researched in depth because they are outside DSO's target.
- I9 (Infisical sidecar-agent guide) returned HTTP 404 when fetched; only its search snippet was seen.
- Release cadence data for Infisical came from aggregators (Not independently verified).
- No product was installed or exercised; all competitor statements are documentation statements.

## 7. Implication for DSO (summary; full reasoning in `product-strategy.md`)

DSO should not compete on rows 1, 3, 5, 7, 8, 11, 13, 15. It should be the best answer to rows 2 (runtime version tracking), 4, 9 (runtime audit), 10 (push triggers), 14 — and it should add **Doppler and Infisical as providers** so that a team already paying for either can use DSO as its Compose delivery layer. That is the "alongside, not instead of" position recommended in ADR-001.
