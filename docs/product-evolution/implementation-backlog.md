# Implementation Backlog

**Date:** 2026-11-07
**Status of every item: proposed — not started — needs owner approval before implementation.**
Findings `PE-xx`: `current-architecture-audit.md`. Security items `S-x`: `security-improvement-plan.md`. ADRs: `architecture-decisions.md`. Phases: `production-readiness-roadmap.md`.
Sizes are effort for one experienced maintainer including tests and docs: **S** ≤ 3 days, **M** ≈ 1 week, **L** ≈ 2 weeks, **XL** ≥ 3 weeks.

## First five engineering tasks (recommended order)

1. **BL-01** — real-Docker CI lane and failing regression tests for PE-01, PE-03, PE-05, PE-08.
2. **BL-02** — persisted version ledger; no rotation on agent start when nothing changed.
3. **BL-03** — per-service rotation queue whose lock spans the whole rotation; no dropped updates, no false success.
4. **BL-07** — eligibility assessment applied to every strategy (stateful/host-port/readiness blockers).
5. **BL-06** — IPC authorization tiers; no caller-supplied provider configs in the shared cache.

In parallel (documentation only, S): **BL-11** truth pass on README/ROADMAP claims.

---

## Phase 0 — Truth and baseline

| ID | Title | Findings | Problem | Proposed solution | Benefit | Size | Risks | Acceptance criteria |
|---|---|---|---|---|---|---|---|---|
| **BL-01** | Real-Docker CI lane | PE-16 | `test/integration` is skipped by `-short` (`.github/workflows/ci.yml:62`; 7/8 files) so no rotation test against a real daemon runs in CI | New CI job (no `-short`) on `ubuntu-latest` with Docker; add failing-first tests: PE-01 (change during in-flight rotation), PE-03 (agent restart with unchanged secret), PE-05 (`rolling` on `postgres:16` with named volume), PE-08 (file secret + tmpfs + rolling) | Converts the four highest-risk inferences into facts | M | CI time (+10–15 min); flaky timing → use deterministic waits on Docker events, not sleeps | Job runs on every PR to `main`; the four new tests exist and either fail (confirming the finding) or pass (refuting it, finding closed with evidence) |
| **BL-11** | Documentation truth pass | PE-04, PE-12, PE-15, PE-18 | README states `rolling` default (`README.md:571`), "<5s detection" (`:32`), "Production-Ready" (`:42`), `dso.yaml` 0664 (`:417`), broken Vault example (`:332-339`) | Correct each claim; add "zero-downtime conditions" box (audit §C.3); mark Event Reactor/SmartPoller status | Restores trust; reduces support load | S | None technical | Every claim in README "Core Features" has a code reference or is labelled Planned |
| **BL-20** | Freeze `dso central` | PE-21 | Effort flowing to Option A | Decision record + `docs/central-store-m1.md` banner: "frozen at experimental scope; not on the delivery path". Land or park the in-progress recovery work at owner's discretion | Focus | S | Sunk-cost pressure | ADR-002 accepted; no new central features merged without a new ADR |

## Phase 1 — Rotation correctness

| ID | Title | Findings | Problem | Proposed solution | Benefit | Size | Risks | Acceptance criteria |
|---|---|---|---|---|---|---|---|---|
| **BL-02** | Version ledger + startup no-op | PE-03, PE-14 | Baseline hash is in memory (`trigger.go:29`); first poll rotates everything (`:425-428`) | Persist `AppliedRecord` (target-architecture §8) in the existing state dir; compare provider version (or `sha256:` of value) against `applied_version`; on first ever run with no record, *adopt* the current value as applied (opt-in `rotate_on_start: true` for the old behaviour) | Agent restarts and upgrades become non-events | M | Ledger corruption → fail safe = treat as "unknown", log, do not mass-rotate | Restart test: 0 containers recreated when provider unchanged; change while agent down → exactly the affected services rotate once |
| **BL-03** | Per-service rotation queue | PE-01, PE-02, PE-23 | Hash stored before outcome (`trigger.go:284`); lock released before async work ends (`:343`); busy service skipped silently (`controller.go:808-816`); success reported when nothing ran (`:1594-1610` + `trigger.go:382-387`) | One worker per service; lock held until terminal state; newer request replaces queued one; ledger `desired` advances only on enqueue, `applied` only on success; success event requires ≥1 container on new version; no time-based reclaim while the holder goroutine is alive | No lost revocations, truthful events | L | Deadlocks/regressions in a hot path → `-race` stress tests, existing `concurrency_stress_test.go` extended | Test: 3 changes in 5s during a 30s rolling rotation end with every container on change #3 and exactly 2 rotations executed; no `RotationSucceeded` when 0 containers rotated |
| **BL-05** | Provider call deadlines | PE-13 | `GetSecretWithContext(t.ctx)` has no deadline (`trigger.go:449-453`) | Per-provider `timeout` (default 30s) wrapping each call | Hung provider no longer freezes a poll loop | S | Too-short default for slow Vault → configurable | Blocking fake provider: loop resumes within timeout + backoff |
| **BL-07** | Eligibility assessment for all strategies | PE-05, PE-06, PE-04 | Stateful guard only for `auto` (`controller.go:825-834`); shadow copies volumes/ports verbatim (`container_clone.go:69-86`); non-proxy rolling failure falls back to restart (`controller.go:1153-1163`) | `Assess()` runs before every rotation; `rolling` on stateful or non-proxy fixed host port → BLOCKED with reason unless explicit override label; remove implicit fallback-to-restart unless `on_rolling_failure: restart` | No surprise data risk or surprise downtime | M | Existing users of explicit `rolling` on DBs get BLOCKED → clear message + override; CHANGELOG | Integration: `postgres`, `mysql`, `redis` with volume → BLOCKED; stateless nginx with HEALTHCHECK + proxy port → rolling succeeds with 0 failed requests in the existing downtime harness (`test/integration/proxy_rotation_downtime_test.go`) |
| **BL-08** | `docker dso rotation plan` | PE-04..PE-09 | Operators cannot predict behaviour | CLI prints per service: resolved strategy, zero-downtime yes/no, blockers, readiness source, proxy ownership; `--json` | Onboarding clarity; CI gate | S | — | Output matches `Assess()` for fixtures; documented in `docs/cli-reference` |
| **BL-09** | Readiness probes | PE-09 | No-HEALTHCHECK gate is ~2s of "running" (`health_check.go:50-56`) | `dso.readiness: tcp:5432 | http:8080/healthz | exec:<cmd>`; zero-downtime requires HEALTHCHECK or readiness | Real readiness gate | M | Probe from agent host vs network namespace → run TCP/HTTP probes from inside the target network via the container IP | Rotation of a slow-starting app waits until the probe passes; timeout rolls back |
| **BL-04** | Post-start tmpfs delivery for rotation path | PE-08 | CopyToContainer before start cannot target tmpfs | Use `injector.InjectFiles` after start, before SWITCHING; file probe already exists (`rolling_strategy.go:290-299`) | File-mode guarantee holds | M | Shell-less images → explicit error + guidance (PE-17) | S-2 acceptance in `security-improvement-plan.md` |

## Phase 2 — Security and provider foundation

| ID | Title | Findings | Problem | Proposed solution | Benefit | Size | Risks | Acceptance criteria |
|---|---|---|---|---|---|---|---|---|
| **BL-06** | IPC authorization tiers | PE-11 | All-or-nothing IPC; caller config cached by name (`server.go:204-266`, `store.go:40-68`) | See S-1 and ADR-007 | `dso` group ≠ all secrets | M | Breaks non-root pre-flight → flag + deprecation | S-1 acceptance |
| **BL-12** | Config permission docs/doctor | PE-12 | README invites group-write | Fix docs; doctor warns on group-write | — | S | — | S-4 acceptance |
| **BL-10** | Vault AppRole/JWT + renewal | PE-15 | Static token only | Add auth methods + renewal; fix examples | Production Vault usable | M | Vault API differences → test with official dev image | S-5 acceptance |
| **BL-13** | `VersionedProvider` interface | PE-14, PE-25 | No version metadata; AWS double-calls | Optional interface (ADR-005); AWS `DescribeSecret` only for version (drop tag merge from hot path or make opt-in), Vault KV v2 metadata, Azure version id | Cheaper polling; ledger stores real versions | M | Plugin/agent version skew → optional interface, fallback to hash | Old plugin binaries still work (test with previous release plugin); new ones report version ids |
| **BL-14** | Doppler, Infisical, GCP Secret Manager providers | strategy | "Integrates with existing platforms" not true for the two named competitors | Three new plugins using official APIs and machine identities (Doppler service token / service account; Infisical machine identity universal-auth/OIDC; GCP ADC) | Core of the "alongside" story | L (≈1–1.5 wk each) | API changes, rate limits; vendor ToS for automation → use documented APIs only | Each plugin: unit tests with recorded fixtures, an `examples/<provider>-compose/` that passes `dso validate`, documented auth setup |
| **BL-15** | Plugin signature verification | PE-20 | Manifest = integrity only | S-6 | Authenticity | M | Offline installs | S-6 acceptance |

## Phase 3 — Operability and data-plane resilience

| ID | Title | Findings | Problem | Proposed solution | Benefit | Size | Risks | Acceptance criteria |
|---|---|---|---|---|---|---|---|---|
| **BL-17** | `/readyz` and rotation metrics | PE-26 | `/health` is static (`metrics.go:149-153`) | `/readyz` = Docker reachable ∧ providers healthy ∧ ledger loaded ∧ no BLOCKED-without-ack; metrics: rotations by outcome/strategy, rotation duration, time-since-applied per secret, pending queue depth | Operators can alert on DSO | M | Cardinality → no secret names as labels by default | Prometheus scrape shows metrics; readiness flips on Docker stop in an integration test |
| **BL-26** | Runtime audit with version ids | PE-14 | Audit lacks version | Audit record per container transition includes `desired/applied` version ids (never values) | Answers "is anything still running the old credential?" | S | — | `dso rotation status --json` lists per-container applied version |
| **BL-18** | Proxy as separate process | PE-10 | Proxy dies with agent (`internal/cli/agent.go:130-144`) | `dso proxy` systemd unit, control socket, persisted backend table; agent restarts do not drop listeners | Agent upgrades without traffic loss | XL | New moving part; port ownership races → keep in-process mode as default until soak-tested | Downtime harness: restart agent mid-traffic → 0 failed requests through proxy |
| **BL-22** | Compose label & alias policy | PE-07 | Shadow shares aliases while unhealthy; renamed container keeps old compose labels | Join networks *without* service aliases until VERIFYING passes, then connect with aliases (`NetworkDisconnect/Connect`); document compose `config-hash` interaction | East-west traffic not routed to unready shadow | M | Docker API version differences | Integration test: peer container resolving the service name never reaches the shadow before it is healthy |
| **BL-24** | Cross-distro matrix | ROADMAP item | Single distro CI | Ubuntu LTS, Debian, Fedora/RHEL-family, Amazon Linux via VMs or containers-with-systemd; install + rotate + restart scenario | Real-environment proof | L | CI cost → nightly | Nightly matrix green for 14 consecutive days |

## Phase 4 — Ecosystem and simplification

| ID | Title | Findings | Problem | Proposed solution | Size | Acceptance criteria |
|---|---|---|---|---|---|---|
| **BL-21** | Provider webhook adapters (Doppler, Infisical, AWS EventBridge) | PE-18 | Polling latency | Verify vendor signatures, enqueue RotationRequest | M | Signed vendor webhook triggers rotation within 5s in an integration test; unsigned rejected |
| **BL-19** | Single runtime | PE-18, PE-17 | Two runtimes (`agent.Agent` vs `TriggerEngine`) | Local mode uses the same controller with the local-vault provider | L | `dso up` and `dso agent` share one rotation code path; old `agent.Agent` deleted |
| **BL-16** | Remove dead surface | PE-19, PE-22, PE-18 | Secret-driver socket (verify first), `system doctor`, SmartPoller, stub reactor | Delete after verification | S | Build/vet/tests pass; socket not created |
| **BL-23** | Reduce AWS API calls | PE-25 | `DescribeSecret` per poll | Folded into BL-13 | S | One API call per poll when unchanged |
| **BL-25** | Pin Actions to SHAs | P-05 note | Major-version tags | SHA pins + Dependabot | S | All `uses:` pinned |
| **BL-27** | Dual-credential coordination guide | audit §C.3 item 7 | Single-credential provider rotation breaks old containers before DSO polls | Docs + `rotation plan` warning when provider rotation is single-phase; recommend provider dual-credential/two-user patterns (competitive-analysis D1, I1) | S | Docs page + warning text exists |

## Explicitly not in the backlog (evaluated and rejected for v1)

RBAC/SSO, approval workflows, multi-host coordination, Kubernetes support, dynamic secret *issuing*, a policy engine, a hosted control plane, further `dso central` milestones (M2 network API, remote callers). Rationale: ADR-001, ADR-002, ADR-010.
