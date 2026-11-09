# DSO Complete Product Audit — CLI, GUI, Feature Gaps, and Competitive Roadmap

**Date:** 2026-10-24
**Branch at time of audit:** `feature/webui-phase1` @ `ccfc0b5`
**Method:** Evidence-based, read-only static review (no application code was changed). Four parallel workstreams — CLI, GUI/backend API, security/secrets-lifecycle, and competitive benchmark (Infisical/Doppler) — each independently verified claims against source with file:line citations. `go build ./...` was run as a sanity check (succeeded). `go test ./...`, the web test suite, and a live dev-server session were **not** executed in this pass — see §13 for what remains unverified.

---

## 1. Executive Summary

DSO is a genuinely substantial, security-disciplined Go project, not a prototype. The CLI, provider plugins, rotation/injection/watcher subsystem, and web UI backend show almost no fabricated functionality: honest stubs are labeled as such in both code and docs, and components that are intentionally out of scope say so explicitly in code comments rather than faking a feature. The one real finding that looks like a mistake — a dead client-side CSRF token that the backend never validates — is a latent footgun, not an active vulnerability, because a separate Origin-header check already does the real defense job.

DSO's genuine differentiator is its flagship capability: zero-downtime, health-checked, auto-rollback secret rotation for Docker Compose, shipped free in the OSS core. Both Infisical and Doppler paywall equivalent rotation behind paid tiers even when self-hosted. DSO also has no mandatory vendor control plane, which neither competitor can currently claim without caveats (Infisical's self-hosted core is free but nearly every differentiating capability needs a paid license; Doppler only recently announced a nascent, Enterprise-only on-prem edition).

The most important gap is not a bug — it's incompleteness of newer work. The `dso central` store (projects/environments/versioning — DSO's path to feature parity with competitors on those axes) is explicitly documented as "experimental, local-only, not production-ready," with no network API, no authn/authz, and no audit chain yet. The second most important gap is operational: DSO has zero multi-user governance (no RBAC, no SSO, no approval workflows) by deliberate scope choice — appropriate for a single-operator design, but worth flagging loudly to anyone evaluating it for a team.

**Top 5 verified problems:**
1. `dso central` (the feature that would bring DSO to parity on environments/versioning/projects) has no authn/authz, no audit chain, and no backup/restore command — explicitly self-documented as pre-production (docs/central-store-m1.md:3-7, 224-226).
2. Dead client-side CSRF token machinery (`web/lib/csrf.ts`) is generated and sent but never validated server-side — inert code that misleads anyone reading the frontend into believing double-submit CSRF protection exists (`web/lib/api-client.ts:52-58` vs. zero matches in `internal/server/rest.go`/`internal/webuiauth/*.go`).
3. `dso system doctor` is registered nowhere in the command tree — dead code exercised only by a direct unit test (`internal/cli/utils.go:148-155`, `internal/cli/secret_test.go:53`).
4. Three security-relevant subsystems were not traced deep enough in this pass to close out as verified-safe: rotation fail-open/closed behavior under partial failure, provider-outage fail-safe behavior for secret delivery, and the `internal/proxy` Docker-socket-mediation authorization boundary (P-02, P-03, P-04 in §6).
5. Test-coverage gaps: five web UI pages (`/drift`, `/analytics`, `/alerts`, `/users`, `/settings`) and two CLI commands (`notifications`, `down`) have no dedicated test file, against an otherwise consistently well-tested codebase.

**Top 5 highest-value additions** (see §11/§12 for full roadmap):
1. Finish `dso central`'s authn/authz and audit chain before promoting it out of experimental status — this is the single biggest lever on competitive parity (environments, versioning, projects).
2. Close the three "needs further investigation" security items (P-02/P-03/P-04) with a dedicated deep-dive pass, since they gate confidence in DSO's core safety claims (atomic rotation, fail-closed secret delivery).
3. Remove or wire up the dead CSRF token and the dead `system doctor` command — small, low-risk cleanups that remove maintainer confusion.
4. Add the missing test coverage (5 web pages, 2 CLI commands) before further feature work compounds the gap.
5. Decide and document DSO's stance on dynamic secrets / credential leasing — currently fully missing, and it's a real capability gap vs. both competitors' upper tiers, but may be legitimately out of scope for a static-secret rotation daemon.

**What prevents DSO from being production-ready?** Nothing in the audited surface area is currently broken for its documented, single-host scope — the core CLI, provider plugins, and rotation/injection/watcher are real and heavily tested. The blocker to calling DSO "production-ready" is narrower: the newer `dso central` feature is explicitly pre-production, and three security-relevant code paths need deeper verification before they can be marked clean rather than "no issue found in the time available."

**Where does DSO already differentiate itself?** Free, unpaywalled, Docker Compose-native zero-downtime rotation; no mandatory vendor control plane; unusually candid security/roadmap documentation that separates "Available" from "Planned" from "Future" rather than glossing over gaps; and Docker-socket-level integrity controls (signed releases, fail-closed plugin hash verification) that have no real equivalent surfaced in either competitor's public docs.

**What should DSO deliberately not build?** A general-purpose multi-tenant RBAC/approval-workflow governance platform, Kubernetes-native operation as a primary target, or a mandatory hosted control plane — all of these would dilute the Docker Compose-native, self-hosted identity that is DSO's actual advantage.

**Single best next milestone:** Harden and promote `dso central` from experimental to a documented beta: add authn/authz, a minimal audit chain, and a backup/restore command, gated by closing the three open security questions (P-02/P-03/P-04) first since central-store work will build on the same rotation/injection trust assumptions.

---

## 2. Repository Baseline and Verified Architecture

- **Module:** `github.com/docker-secret-operator/dso`, Go `1.26.0` / toolchain `1.26.10` (go.mod).
- **Entry points:** `cmd/dso/main.go` → `internal/cli.Execute()` → `internal/cli/root.go:NewRootCmd()`. Four provider-plugin binaries under `cmd/plugins/dso-provider-{aws,azure,vault,huawei}`.
- **Primary internal packages** (verified present, not all individually line-traced — see per-section scope notes): `agent, alert, analyzer, audit, auth, bootstrap, centralsvc, cli, compose, core, crypto, events, injector, notify, polling, providers, proxy, resolver, rotation, securefs, server, setup, store, strategy, testing, util, watcher, webui, webuiauth`.
- **Frontend:** `web/` — Next.js App Router, TypeScript, Vitest test suite (`web/__tests__`).
- **`go build ./...` succeeds cleanly** (verified this pass). `go test ./...` and `npm`/`vitest` web tests were **not executed** in this pass — test *existence* was confirmed by file listing only.
- **Existing prior audits found and cross-referenced:** `docs/reviews/2026-10-16-m0-m1-independent-review.md`, `docs/reviews/2026-10-23-phase3-dependency-security-audit.md`. This report does not duplicate their content wholesale; it re-verifies current-state claims where they overlap with this pass's scope.
- **Dead/misleading code found:** `internal/cli/utils.go:148-155` (`newSystemDoctorCmd`, unreachable), `internal/cli/stubs.go` (misleadingly named — contains a complete, correct `version` command, not unfinished work).

---

## 3. CLI Command Inventory

| Command | Status | Evidence | Recommendation |
|---|---|---|---|
| `setup`, `bootstrap`, `doctor`, `status`, `config show/edit/path/validate`, `agent ...`, `compose ...`, `fetch`, `init`, `apply`, `inject`, `sync`, `migrate plan/apply`, `up`/`down`, `watch`, `version`, `validate`, `export`, `inspect`, `logs`, `secret get/set/list`, `env import`, `central ...` | **Complete** | Each traced to a real, non-trivial handler (line counts and test files documented in the full inventory) | OK — no action needed |
| `system status/enable/disable/restart/logs/setup` | Complete | `internal/cli/system_mgmt.go:12-37` | Confirm no unsanitized input reaches `exec.Command` (shells out via `os/exec`) — not confirmed broken, just not verified clean |
| `system doctor` | **Dead code** | `internal/cli/utils.go:148-155` (no-op `RunE`), never registered in `NewSystemCmd()`; only referenced by `internal/cli/secret_test.go:53` | Remove or implement and wire in |
| `diff` | **Explicit, honest stub** | `internal/cli/diff.go:11-46` — always returns `"diff is not implemented"` (line 44); docs/cli-reference/dso_diff.md accurately documents this | Implement, or keep as an honest non-fabricating stub (current behavior is acceptable, not broken) |
| `notifications` | Complete, **no dedicated test file** | `internal/cli/notifications.go` (92 lines) | Add unit tests |
| `down` | Complete, **no dedicated test file found** | `internal/cli/down.go` (53 lines) | Verify/add test coverage |

**Provider plugins (AWS/Azure/Vault/Huawei):** all four verified **genuinely functional** — real vendor SDK calls (`secretsmanager.GetSecretValue`, `azsecrets` client `GetSecret`, Vault `Logical().ReadWithDataWithContext`, Huawei CSMS `ShowSecretVersion`), real ticker-driven `WatchSecret` polling, and in Vault's case real additional input hardening (`requireSecureVaultAddr`, `validateVaultSecretName`). No TODO/FIXME/stub markers found in any provider plugin.

**Rotation / injector / watcher:** all three verified **real, production-grade implementations**, not mocks — `internal/rotation/rolling_strategy.go` performs actual Docker `ContainerCreate`/`ContainerStart`/`ContainerStop`/`ContainerRemove` calls for atomic container swap; `internal/injector` delivers secrets via Docker API with dedicated error/timeout tests; `internal/watcher/controller.go` (1,934 lines) plus 11 dedicated test files including `concurrency_stress_test.go` and `panic_recovery_test.go` make this the most heavily tested subsystem in the repo.

**Explicitly out of scope for the CLI pass** (not individually line-traced): `internal/server`, `internal/webui`, `internal/webuiauth`, `internal/audit`, `internal/centralsvc`, and several other packages — covered instead by the GUI/API and security workstreams below where their scopes overlap.

---

## 4. GUI Page/Workflow Inventory and Backend API Completeness

Scope: `web/` (Next.js App Router) against `internal/server/rest.go` (route table, lines 161-278), `internal/webui/`, `internal/webuiauth/`. **No dev server was started; all findings are static-source evidence, not runtime observation.**

| Page/Route | Goal | Backend status | Test coverage | Notes |
|---|---|---|---|---|
| `/login` | Operator login | Implemented, rate-limited | `login.test.tsx` | Credential never stored client-side; HttpOnly session cookie |
| `/` dashboard | Health + secrets + discovery overview | Implemented (`rest.go:233,247,241`) | `dashboard.test.tsx` | No secret values in response |
| `/secrets` | List secrets, trigger rotation | Implemented (`rest.go:245-248`) | `secrets.test.tsx`, `secrets-rotate.test.tsx` | Confirm dialog before rotate; no reveal UI exists at all |
| `/containers` | Container/binding view | Implemented (`rest.go:257`) | `containers.test.tsx` | — |
| `/audit` | Audit log viewer | Implemented (`rest.go:259`) | `audit.test.tsx` | — |
| `/logs` | Log viewer | Implemented (`rest.go:255`) | `logs.test.tsx` | — |
| `/events` | Event feed + WebSocket | Implemented (`rest.go:249-254`) | `events.test.tsx`, `useWebSocket.test.tsx` | Covers WS reconnect |
| `/drift` | Drift detection (diagnostic only) | Implemented (`rest.go:269`) | **None found** | Add coverage |
| `/analytics` | Analytics dashboard | Implemented (`rest.go:271`) | **None found** | Add coverage |
| `/alerts` | Alert list | Implemented (`rest.go:273`) | **None found** | Add coverage |
| `/integrations` | Webhook/notification integrations + test button | Implemented (`rest.go:265-268`) | `integrations-notification-test.test.tsx` | Test-button naming avoids config leakage in logs |
| `/configuration` | Raw config view | Implemented (`rest.go:243`) | `configuration.test.tsx` | — |
| `/users` ("Access & Authentication") | Single-operator identity + session list/revoke | Implemented (`rest.go:239,261-264`) | **None found** | Explicitly NOT a multi-user admin page (code comment) — honest scope, not a stub |
| `/settings` | Health/discovery/identity display only | Implemented | **None found** | No password-change form because there's no config-rewrite mechanism yet — correctly omitted rather than faked |

**Backend route inventory** (`internal/server/rest.go:161-278`): every route is auth-gated except `/health`, `/api/auth/login` (rate-limited), and `/api/auth/logout`. `/api/auth/session` is deliberately **excluded** from the public-paths allowlist so an invalid session cannot self-validate for free. Global middleware applied before routing: 64KB body cap, DNS-rebinding Host-header guard, per-IP rate limiting, security headers (`X-Frame-Options: DENY`, `Cache-Control: no-store`, `Referrer-Policy: no-referrer`), and origin-based rejection on unsafe methods/WS upgrades.

**Frontend/backend parity: complete.** Every endpoint called from `web/lib/api/*.ts` (11 files) matches a registered backend route, and no backend route lacks a frontend caller.

**Verified stubs/dead/mock controls:**
- **None found** that fake functionality. Components consistently carry explicit scope-disclaiming comments (e.g. "no rotate/edit/delete/reveal UI here," "deliberately NOT a multi-user admin page," "no password-change form... no config-file-rewrite mechanism yet") rather than presenting mock data as real.
- **One dead-but-honest control:** `web/lib/csrf.ts` generates an `X-CSRF-Token` header attached by `web/lib/api-client.ts:52-58` on every state-changing request, but `internal/server/rest.go` and `internal/webuiauth/*.go` never read or validate it (confirmed via grep, zero matches beyond comments). Real CSRF defense is the Origin-header allowlist (`rest.go:221-230`). This isn't currently exploitable, but it's inert code that misrepresents the frontend's defense-in-depth posture and should be removed or wired to a real check.

**Not verified in this pass:** backend Go test pass/fail status (tests exist by filename — `rest_test.go`, `cookie_secure_test.go`, `legacy_auth_token_exposure_test.go`, etc. — but were not executed); per-handler input validation depth beyond the routing/auth/CSRF layer; backend pagination implementation (only the frontend query-string construction was seen).

---

## 5. Backend/API Quality Assessment

- **Authentication:** cookie-based, `HttpOnly`, `SameSite=Strict`, `Secure`-by-default session cookie; bcrypt-hashed password; idle + absolute session lifetime caps; per-operator session limit with oldest-eviction (`internal/webuiauth/session.go`). No client-stored token anywhere.
- **Authorization:** single-operator model by design — there is no RBAC layer to assess because none is claimed to exist (see §6/§8 for the governance gap discussion).
- **CSRF:** real protection is Origin-header allowlisting (`rest.go:221-230`) combined with `SameSite=Strict` cookies; the separate client-side CSRF-token mechanism is dead code (see §4).
- **Rate limiting:** present globally (per-IP token bucket) and specifically on login (`loginlimiter.go`).
- **Webhook auth:** `/api/events/secret-update` uses HMAC authentication (`webhook_auth.go`) for the webhook-triggered path, distinct from session-cookie auth for the UI path.
- **Input validation / pagination / concurrency:** not traced to handler-body depth in this pass — flagged as unverified, not assumed clean.

---

## 6. Secret Lifecycle and Security Assessment

### Verified findings

| ID | Severity | Summary | Status |
|---|---|---|---|
| F-01 | MEDIUM (accepted, documented) | Local Mode writes a fully plaintext-resolved compose file to `/tmp` (`0600`) for the duration of `docker compose up`; `SIGKILL`/crash can leave it behind until the next run's ~1hr stale-sweep. Documented in SECURITY.md:57-64; code matches docs exactly (`internal/cli/up.go:285-370`). | Verified, accepted risk, no drift from docs |
| F-02 | LOW (mitigated) | `internal/auth.Verify` is fail-open (allow) when no token is configured, but `internal/server/rest.go:1311-1327` independently refuses to start if bound publicly without auth. Residual risk only for a hypothetical future caller that constructs `Authenticator` directly and skips the bind check. | Verified, mitigated, latent footgun for future code |
| F-03 | MEDIUM (accepted, documented) | Provider credentials in `dso.yaml` (Vault AppRole secret_id, cloud keys) are stored in cleartext on the host filesystem by design, per SECURITY.md/THREAT_MODEL.md. Credentials are delivered to plugins via RPC, not args/env, so they don't leak via `/proc/<pid>/cmdline` or `docker inspect`. | Verified as documented/intended; `dso.yaml`'s own permission enforcement is a separate open question (P-01) |
| F-04 | LOW (informational, verified-good) | Provider plugin subprocess environment is allow-listed per provider (`pkg/provider/load.go:112-150`) — the daemon's full environment and other providers' credentials are not inherited. | Verified, no issue |
| F-05 | INFORMATIONAL (verified-good) | Audit and injection-event logging uses secret *names/identifiers* only, never values (`internal/audit/audit.go:46-60`, `internal/injector/docker.go:39-80`). | Verified, no issue |
| F-06 | INFORMATIONAL (verified-good) | AES-256-GCM envelope encryption with AAD binding project/environment/secret/version/DEK-version/root-key-id prevents cross-context ciphertext reuse; Argon2id with floor/ceiling parameter validation; atomic/exclusive key-file installation; `securefs` directory-chain/symlink/ownership checks. | Verified, no issue — unusually rigorous for project size |

### Potential / needs further investigation (not yet closed out)

| ID | Area | Why it matters |
|---|---|---|
| P-01 | `dso.yaml` permission enforcement | Not confirmed whether the config file (containing password hash + provider credentials) gets the same `securefs`-style hardening key files get, or only installer-time `chmod`. |
| P-02 | Rotation fail-open/closed behavior under partial failure | Not confirmed whether a health-check failure after the new container starts leaves both containers alive (safe) or tears down the old one regardless; not confirmed what happens if the rotation lock can't be acquired. |
| P-03 | Provider-outage fail-safe secret delivery | Not confirmed that a provider error (timeout, auth failure, corrupted response) always short-circuits before injection, rather than ever injecting a stale/empty secret. |
| P-04 | `internal/proxy` Docker-socket authorization boundary | This package name suggests it mediates Docker daemon access — directly relevant to the audit's Docker-daemon-trust-boundary requirement — but was not read in this pass. Existing tests (`manager_ownership_test.go`, `concurrency_stress_test.go`) suggest prior hardening already happened here. |
| P-05 | CI workflow `permissions:` scoping | Blocks confirmed present in `ci.yml`/`release.yml` (including scoped `id-token: write` for cosign), but not confirmed minimally scoped against `pull_request_target`/`workflow_run` combined with write permissions on untrusted-fork PRs. |

### Verified clean, no further action needed
No hardcoded secrets found in `internal/crypto`, `internal/auth`, `internal/webuiauth`, `internal/providers`, `internal/injector`. No plaintext secret values in process args (`internal/injector/inject.go` uses stdin+base64 specifically to avoid `/proc/<pid>/cmdline` exposure). Supply chain: pinned Go toolchain, documented/time-boxed `.govulncheck-allowlist.txt`, cosign keyless signing + SBOM generation in `release.yml`.

**This workstream's own stated limitation:** these were the deepest-traced packages in the audit but the budget did not allow full line-by-line tracing of `internal/proxy`, the full `rolling_strategy.go` state machine, or `internal/polling`/`internal/watcher`'s provider-error paths — P-02/P-03/P-04 should be treated as a required follow-up, not as implicitly clean.

---

## 7. Verified Bugs, Stubs, Dead Code, and Incomplete Implementations (consolidated)

1. `internal/cli/utils.go:148-155` — `newSystemDoctorCmd`: dead code, unreachable from the binary, exercised only by a direct unit test.
2. `internal/cli/diff.go:11-46` — `dso diff`: honest, declared stub (always errors, never fabricates a result); docs match.
3. `web/lib/csrf.ts` + `web/lib/api-client.ts:52-58` — dead client-side CSRF token, never validated server-side.
4. `docs/central-store-m1.md:224-226` — `dso central`'s audit trail fields are explicitly "not an audit log and not tamper-evident"; auth/authz is explicitly absent ("any process with the OS user's file access can do everything").
5. Test-coverage gaps: CLI `notifications.go`, `down.go`; web pages `/drift`, `/analytics`, `/alerts`, `/users`, `/settings`.
6. `internal/cli/stubs.go` — misleadingly named file containing a complete, correct `version` command (flagged for maintainer clarity only, not a functional issue).

No evidence of mock data standing in for real API responses, no GUI controls wired to nonexistent endpoints, and no silent failures masquerading as success were found in either the CLI or GUI workstreams.

---

## 8. Competitive Comparison — Infisical and Doppler

All DSO claims are sourced from the repo (README.md, ROADMAP.md, SECURITY.md, THREAT_MODEL.md, docs/webui.md, docs/central-store-m1.md, docs/OPERATIONAL_LIMITATIONS.md) with line citations; Infisical/Doppler claims are sourced from their official docs/pricing pages, with important caveats flagged explicitly rather than assumed.

### Selected matrix highlights

| Feature | DSO | Infisical | Doppler | DSO Importance |
|---|---|---|---|---|
| Automatic secret rotation | **Implemented, free, flagship** | Partial — Free=none, Pro=public-DB-only, full rotation Advanced+ | Partial — gated to Team+ | N/A (DSO's core differentiator) |
| No mandatory vendor control plane | **Implemented** | Self-hosted core is free, but most differentiating features need a paid license even self-hosted | Historically SaaS-only; new Enterprise-only, early-stage on-prem edition announced 2026 (pricing/SLA unverified) | N/A (DSO's core differentiator) |
| Environments / projects | Partial — only in experimental `dso central` | Supported (Native) | Supported (SaaS-only) | P1 |
| Dynamic secrets | Missing | Partial — requires Advanced+ license even self-hosted | Supported (Enterprise) | P2 |
| RBAC / multi-user governance | Missing (explicitly out of scope) | Partial, gated by tier | Partial, gated by tier | P2 |
| Approval workflows | Missing (explicitly out of scope) | Enterprise-only | Team+/Enterprise | P3 — intentionally excluded |
| HA / DR | Missing (deliberate scope limit) | Partial, marketing claims only, no concrete runbook found | Unverified for new on-prem edition | P1, but explicitly scoped out |
| Release integrity / signing | **Implemented** — Sigstore/cosign + fail-closed plugin hash verification | Unverified | Unverified | P1 |
| Web UI | Partial — deliberately read-mostly, disabled by default | Full admin dashboard (standard) | Full admin dashboard (standard) | P2 |

### Important caveats surfaced by this research
- **"Is Doppler SaaS-only?"** Historically yes for the vast majority of its install base; a 2026-announced "Doppler On-prem" exists but is Enterprise-only, early-stage, with no pricing/SLA details found — any external claim that "Doppler has no self-hosted option" should carry this caveat.
- **Infisical's "free self-hosted" framing needs a caveat.** The MIT core is genuinely free to self-host, but dynamic secrets, full rotation, RBAC/ABAC, SSO, audit logs, and approval workflows all require a paid license key regardless of hosting location.

---

## 9. DSO's Proposed Product Positioning and Differentiators

**Group A — Core differentiators to protect and double down on:**
- Zero-downtime, health-checked, auto-rollback Docker Compose rotation (already DSO's best feature; keep it free and unpaywalled).
- No mandatory vendor control plane — a structural advantage over both competitors that should remain a hard architectural constraint, not just a current state.
- Candid, scope-labeled documentation (Available/Planned/Future) — a trust-building habit worth preserving explicitly as process, not just a byproduct of current maturity.

**Group B — Essential production capabilities still needing work:**
- Close P-01 through P-05 (security follow-ups) before any further central-store feature work.
- Finish `dso central`'s authn/authz and audit chain.
- Add the missing test coverage identified in §7.

**Group C — Competitive improvements worth considering, not urgent:**
- Broaden the provider ecosystem beyond AWS/Azure/Vault/Huawei.
- Decide explicitly on dynamic secrets / credential leasing — currently missing; may or may not fit DSO's static-rotation model.
- Ship an SBOM and move vulnerability scanning from advisory to enforced (already on DSO's own roadmap as "Planned").

**Group D — Explicitly defer or exclude:**
- General-purpose multi-tenant RBAC/approval-workflow governance platform — both competitors paywall this even at their own upper tiers; building it for free would dilute focus without a clear strategic payoff, and DSO's own ROADMAP.md already excludes it.
- Kubernetes-native operation as a primary target — would dilute the Docker Compose-native identity.
- HA/DR across multiple hosts — a deliberate, documented scope limit; revisit only if user demand is concrete, not speculative.

---

## 10. Testing Gaps and Verification Results

| Area | Verified by automated test | Verified by manual execution | Inferred from code inspection | Not verified |
|---|---|---|---|---|
| CLI command tree builds | `go build ./...` (executed, passed) | — | — | — |
| CLI command correctness | — | — | File-by-file trace + existing `_test.go` file presence | `go test ./...` not executed |
| Provider plugin real SDK calls | — | — | Confirmed via source read | Live provider connectivity not exercised |
| Rotation/injection/watcher behavior | — | — | Confirmed via source read; heavy existing test suite present by filename | Tests not executed in this pass |
| Web UI pages | — | — | Confirmed via source read; most pages have matching `__tests__` files | Dev server not started; 5 pages (`/drift`,`/analytics`,`/alerts`,`/users`,`/settings`) have no test file at all |
| Backend API routing/auth/CSRF | — | — | Confirmed via source read | Handler-body input validation, pagination, concurrency not traced |
| Security findings F-01–F-06 | — | — | Confirmed via source read against docs | — |
| Security findings P-01–P-05 | — | — | — | Explicitly unverified — flagged for follow-up |

**Missing coverage identified:** `internal/cli/notifications.go`, `internal/cli/down.go` (no dedicated unit test found); web `/drift`, `/analytics`, `/alerts`, `/users`, `/settings` (no test file found).

---

## 11. Documentation Discrepancies

- **None found that overstate functionality.** Every discrepancy found runs the opposite direction from the usual failure mode: docs and code comments consistently *underclaim* or explicitly flag incompleteness (`dso diff`'s docs match its stub exactly; `docs/central-store-m1.md` explicitly says "not production-ready"; web component comments explicitly disclaim out-of-scope functionality rather than implying it exists).
- **One naming-only issue:** `internal/cli/stubs.go` is a misleading filename for a complete, correct command — worth a rename for future-maintainer clarity, not a doc fix.
- **Recommended doc additions:** document the provider-plugin environment allow-list behavior in SECURITY.md (currently only in code comments, per F-04); once P-01 through P-05 are resolved, update SECURITY.md/THREAT_MODEL.md with the findings either way (confirmed-safe or confirmed-needs-fix).

---

## 12. Prioritized Remediation Roadmap

| ID | Priority | Component | Problem | Proposed Solution | Evidence |
|---|---|---|---|---|---|
| DSO-P1-001 | P1 | Security | Rotation fail-open/closed behavior under partial failure not confirmed (P-02) | Read full `rolling_strategy.go` state machine + `lock_manager.go` failure path; add explicit tests for health-check-failure-after-new-container-start and lock-acquisition-failure scenarios | security-audit P-02 |
| DSO-P1-002 | P1 | Security | Provider-outage fail-safe secret delivery not confirmed (P-03) | Trace `smart_poller.go`/`event_handler.go` error paths to confirm they always short-circuit before injection | security-audit P-03 |
| DSO-P1-003 | P1 | Security | `internal/proxy` Docker-socket authorization boundary not traced (P-04) | Dedicated read-through of `router.go`/`manager.go`/`server.go` plus existing `manager_ownership_test.go` | security-audit P-04 |
| DSO-P1-004 | P1 | GUI/Backend | `dso central` has no authn/authz or audit chain | Design and implement before promoting out of experimental status | docs/central-store-m1.md:224-226 |
| DSO-P2-001 | P2 | GUI | Dead client-side CSRF token | Remove `web/lib/csrf.ts`/its usage, or implement real double-submit validation server-side | gui-audit §4.1 |
| DSO-P2-002 | P2 | CLI | `system doctor` dead code | Remove, or implement and register in `NewSystemCmd()` | cli-audit §4.2 |
| DSO-P2-003 | P2 | Security | `dso.yaml` permission enforcement not confirmed (P-01) | Trace `pkg/config` loader + `internal/bootstrap/permissions.go`; add a rejecting check for world/group-readable config, matching `securefs`'s key-file handling | security-audit P-01 |
| DSO-P2-004 | P2 | Testing | Missing coverage: `notifications`, `down`, 5 web pages | Add unit/component tests | cli-audit §5.3-4, gui-audit §4.5 |
| DSO-P2-005 | P2 | Security | CI workflow permission scoping unverified (P-05) | Audit `ci.yml`/`release.yml`/`coverage.yml` triggers vs. write permissions | security-audit P-05 |
| DSO-P3-001 | P3 | Product | Dynamic secrets / credential leasing missing | Decide explicitly in/out of scope; document the decision either way | competitive-benchmark §1 |
| DSO-P3-002 | P3 | Docs | SECURITY.md doesn't document provider-plugin env allow-listing | Add a short section referencing F-04 | security-audit F-04 |

Phases: **Phase 0** (this audit) is complete. **Phase 1** = DSO-P1-001..004 (close open security questions, finish central-store governance). **Phase 2** = DSO-P2-001..005 (cleanup + coverage). **Phase 3** = DSO-P3-001..002 (competitive/product decisions). No calendar estimates are given, per audit scope rules — only relative priority and dependency ordering.

---

## 13. Major Architectural Risks and Decisions

- **Single-host, no HA/DR** is a deliberate, documented scope choice (`docs/OPERATIONAL_LIMITATIONS.md:182-185`) — correct for DSO's identity, but must stay a conscious decision rather than default inertia as the product matures.
- **`dso central`'s current lack of authn/authz** is the largest open architectural risk: any feature built on top of it before this is resolved inherits the same gap.
- **Fail-open default in `internal/auth.Verify`** is currently safe only because a second, independent check exists at the bind layer (`rest.go:1311-1327`). This is a correct belt-and-suspenders design today but is fragile to future refactors that add a new listener without going through `rest.go`.

---

## 14. Recommended Next Implementation Milestone

**Milestone: "Central Store Hardening + Security Follow-up Close-out."**

**Acceptance criteria:**
1. P-01 through P-05 are each resolved and re-classified as either "verified clean" (with evidence) or "confirmed issue, fixed" (with a regression test).
2. `dso central` has a documented authn/authz model (even a minimal one) and an append-only, tamper-evident audit log for its own operations.
3. The dead CSRF token and `system doctor` dead code are either removed or made real.
4. The identified test-coverage gaps (2 CLI commands, 5 web pages) have at least smoke-level coverage.
5. SECURITY.md is updated to reflect the outcome of P-01 through P-05, whichever direction they resolve.

---

## 15. Areas That Could Not Be Verified

- `go test ./...` and the web `vitest` suite were not executed — all test-coverage claims are based on file existence, not pass/fail status.
- No dev server was started; all GUI/backend findings are static-source inference, not runtime-observed behavior.
- `internal/proxy`, the full `rolling_strategy.go` state machine, `internal/polling`/`internal/watcher` error paths, and `pkg/config`'s loader/`internal/bootstrap/permissions.go` were not individually line-traced (P-01 through P-04).
- CI workflow permission scoping was confirmed present but not confirmed minimally-scoped (P-05).
- External competitor claims beyond what official docs/pricing pages state were not independently verified (e.g., Infisical's broader integration count, Doppler's exact on-prem pricing/SLA).
- Handler-level input validation, pagination, and concurrency behavior in `internal/server` were not traced beyond the routing/auth/CSRF layer.

---

## 16. Evidence Index

Primary files cited across this audit (non-exhaustive — see each subagent's full working notes for complete citations):

- CLI: `internal/cli/root.go`, `internal/cli/diff.go`, `internal/cli/utils.go`, `internal/cli/system_mgmt.go`, `internal/cli/up.go`, `internal/cli/down.go`, `internal/cli/notifications.go`, `internal/cli/stubs.go`
- Providers: `cmd/plugins/dso-provider-{aws,azure,vault,huawei}/main.go`, `internal/providers/store.go`, `pkg/provider/load.go`
- Rotation/Injection/Watch: `internal/rotation/rolling_strategy.go`, `internal/rotation/lock_manager.go`, `internal/rotation/cleanup.go`, `internal/injector/inject.go`, `internal/injector/docker.go`, `internal/watcher/controller.go`
- GUI/Backend: `internal/server/rest.go`, `internal/server/access_wiring.go`, `internal/webuiauth/session.go`, `internal/webuiauth/middleware.go`, `web/lib/csrf.ts`, `web/lib/api-client.ts`, `web/lib/api-fetch.ts`, `web/contexts/AuthContext.tsx`, `web/lib/api/*.ts`, `web/components/*-content.tsx`, `web/__tests__/*`
- Security: `internal/crypto/envelope.go`, `internal/crypto/keyprovider.go`, `internal/securefs/securefs_unix.go`, `internal/auth/auth.go`, `internal/audit/audit.go`, `.govulncheck-allowlist.txt`, `.github/workflows/release.yml`
- Docs cited: `README.md`, `ROADMAP.md`, `SECURITY.md`, `THREAT_MODEL.md`, `docs/webui.md`, `docs/central-store-m1.md`, `docs/OPERATIONAL_LIMITATIONS.md`, `docs/cli-reference/dso_diff.md`
- External sources: infisical.com (docs, pricing), docs.doppler.com, doppler.com (pricing, blog)
- Prior internal reviews cross-referenced: `docs/reviews/2026-10-16-m0-m1-independent-review.md`, `docs/reviews/2026-10-23-phase3-dependency-security-audit.md`

---

*This audit was produced by four parallel evidence-based research passes and consolidated without implementation. Per the audit's execution rules, no roadmap item in §12 should be started without explicit approval.*
