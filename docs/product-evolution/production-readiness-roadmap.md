# Production-Readiness Roadmap and Validation Strategy

**Date:** 2026-11-07
**Status:** proposed — needs owner approval. No dates are given; sizes are effort estimates for one maintainer (see `implementation-backlog.md` for per-item reasoning). Calendar time depends on review latency and availability, which this document cannot know.
**Cross-references:** findings `PE-xx` (`current-architecture-audit.md`), backlog `BL-xx`, security `S-x`, ADRs.

> **A passing unit-test suite is not proof of production readiness.** DSO's riskiest behaviour (container replacement, tmpfs delivery, proxy handoff, crash recovery, daemon outages) only exists against a real Docker Engine on a real Linux host, over time. Today none of that runs in CI (PE-16). Every phase below has exit criteria that require real-daemon evidence.

---

## Phase overview

| Phase | Objective | Backlog | Effort (sum, rough) | Exit = |
|---|---|---|---|---|
| 0 Truth & baseline | Make claims match code; get real-Docker tests into CI; freeze scope | BL-01, BL-11, BL-20 | ~1.5–2 weeks | Honest README; failing/passing evidence for PE-01/03/05/08 |
| 1 Rotation correctness | Rotation never loses an update, never lies, never surprises | BL-02, BL-03, BL-05, BL-07, BL-08, BL-09, BL-04 | ~7–9 weeks | Rotation test suite green on real Docker; restart = no-op |
| 2 Security & provider foundation | Least-privilege IPC; production-grade provider auth; version-aware providers; Doppler/Infisical/GCP | BL-06, BL-12, BL-10, BL-13, BL-14, BL-15 | ~7–9 weeks | S-1, S-5, S-6 acceptance met; three new providers with examples |
| 3 Operability & resilience | Observable, survives its own restart, cross-distro | BL-17, BL-26, BL-18, BL-22, BL-24 | ~7–9 weeks | Agent restart under load drops 0 proxied requests; 14-day nightly matrix green |
| 4 Ecosystem & simplification | Push triggers, single runtime, less surface | BL-21, BL-19, BL-16, BL-23, BL-25, BL-27 | ~5–6 weeks | One runtime; vendor webhooks; dead code gone |

BL-06 (IPC authz) is independent of Phase 1 and is in the recommended first five tasks; it may be pulled forward without affecting other dependencies.

**v1.0 ("production-credible") = exit of Phase 2 plus BL-17 and BL-24 from Phase 3.** Before that, the README should describe DSO as beta for production rotation.

---

## Phase 0 — Truth & baseline

- **Objectives:** stop over-claiming; turn the high-risk inferences into test evidence; stop scope drift.
- **Deliverables:** CI job running `test/integration` without `-short`; four new regression tests (PE-01, PE-03, PE-05, PE-08); corrected README/ROADMAP (defaults, zero-downtime conditions, detection latency, config perms, Vault example); ADR-002 recorded.
- **Code areas:** `.github/workflows/ci.yml`, `test/integration/*`, `README.md`, `ROADMAP.md`, `docs/central-store-m1.md` (banner only).
- **Dependencies:** none.
- **Security considerations:** tests must use synthetic secrets only; CI logs scanned for test secret strings.
- **Acceptance:** CI job required for merge; each of PE-01/03/05/08 is either confirmed by a failing test (kept as `t.Skip` with issue link until fixed, or marked expected-fail) or refuted and closed with evidence; README "Core Features" lines each cite code or say Planned.
- **Testing:** the new CI lane itself.
- **Complexity:** Low-Medium. Main cost is writing deterministic Docker tests.

## Phase 1 — Rotation correctness

- **Objectives:** the rotation engine is correct under concurrency, restarts and failures; its behaviour is predictable before it runs.
- **Deliverables:** version ledger (BL-02); per-service queue with coalescing (BL-03); provider deadlines (BL-05); eligibility for all strategies (BL-07); `rotation plan` (BL-08); readiness probes (BL-09); post-start tmpfs delivery (BL-04).
- **Code areas:** `internal/agent/trigger.go`, `internal/agent/state_tracker.go`, `internal/watcher/controller.go`, `internal/rotation/*`, `internal/analyzer`, `internal/strategy`, `internal/injector`, `internal/cli` (new subcommand).
- **Dependencies:** Phase 0 CI lane.
- **Security considerations:** ledger stores version ids or hashes, never values; file mode must not touch host disk (S-2); event semantics change (no false success) documented as a security fix.
- **Acceptance criteria (all on real Docker in CI):**
  - Agent restart with unchanged provider: **0** containers recreated.
  - 3 changes during an in-flight rolling rotation: final state = latest value on **100%** of targets; **≤2** rotations executed; **0** `RotationSucceeded` events for no-op runs.
  - Stateful images (`postgres`, `mysql`, `redis` with volumes) + explicit `rolling`: **BLOCKED** with reason; nothing started.
  - Stateless HTTP service with HEALTHCHECK and proxy-owned port, 50 rps during rotation: **0** failed requests over 20 consecutive rotations (extends `proxy_rotation_downtime_test.go`).
  - File-mode rotation: secret readable in tmpfs; **no** secret bytes under the container's `UpperDir`.
  - Hung provider: polling loop recovers within `timeout + backoff`.
- **Complexity:** High (concurrency redesign in the hottest path). Biggest risk is regression; mitigated by Phase 0 tests landing first.

## Phase 2 — Security & provider foundation

- **Objectives:** close the IPC privilege gap; make Vault production-usable; make version-aware polling possible; deliver the "alongside Doppler/Infisical" story.
- **Deliverables:** BL-06, BL-12, BL-10, BL-13, BL-14, BL-15.
- **Code areas:** `internal/agent/server.go`, `internal/agent/peercred.go`, `internal/providers/store.go`, `pkg/api/plugin.go`, `pkg/provider/*`, `cmd/plugins/*` (+3 new), `examples/*`, release workflow (plugin signatures).
- **Dependencies:** BL-02 ledger (to store real version ids).
- **Security considerations:** new plugins get the same env allow-list and hash/signature checks; vendor tokens only from root-owned config or files; no new network listeners.
- **Acceptance:** S-1, S-5, S-6 acceptance tests pass; old plugin binaries still load (backward compatibility test); each new provider has fixtures-based tests and a working example validated in CI.
- **Complexity:** Medium-High (breadth rather than depth).

## Phase 3 — Operability & resilience

- **Objectives:** operators can monitor and alert on DSO; DSO's own restart does not take down traffic; works on the distros users run.
- **Deliverables:** BL-17, BL-26, BL-18, BL-22, BL-24.
- **Code areas:** `pkg/observability`, `internal/server`, `internal/proxy` (+ new `dso proxy` command and unit), `internal/rotation/container_clone.go`, CI nightly.
- **Dependencies:** Phase 1 (state machine is what metrics describe).
- **Security considerations:** proxy process runs without Docker socket and without secret values; control socket root-only.
- **Acceptance:** agent `systemctl restart` during 50 rps load → **0** failed proxied requests; `/readyz` goes not-ready within 10s of Docker stop and recovers within 10s of start; nightly matrix (Ubuntu LTS, Debian stable, a RHEL-family, Amazon Linux) green 14 consecutive nights; 72-hour soak (below) passes.
- **Complexity:** High (BL-18 is the riskiest single item; keep in-process proxy as fallback until soak passes).

## Phase 4 — Ecosystem & simplification

- **Objectives:** lower latency via push; one runtime; smaller surface.
- **Deliverables:** BL-21, BL-19, BL-16, BL-23, BL-25, BL-27.
- **Acceptance:** vendor-signed webhook triggers rotation < 5s in test; local and agent mode share one controller; dead surface removed with green build/tests.
- **Complexity:** Medium.

---

## Testing and production-validation strategy

| Layer | What | Where / how | Measurable acceptance |
|---|---|---|---|
| Unit | Pure logic: hashing/version compare, `Assess()`, label parsers, ledger encode/decode, queue coalescing | `go test -race ./...` (existing CI) | Coverage of new packages ≥ 80% lines; all new code `-race` clean |
| Integration (real Docker) | Rotation strategies, proxy handoff, tmpfs delivery, crash recovery | New non-`-short` CI job; testcontainers already a dependency | Phase 1 acceptance list |
| End-to-end | `install.sh` → `setup` → `migrate` → `validate` → `up` → provider change → rotation → `status` | Nightly VM per distro | All steps exit 0; `rotation status` shows new version on all targets |
| Provider | Per-plugin contract tests with recorded fixtures; live tests against provider sandboxes behind a manual/nightly gate (AWS SM, Azure KV, Vault dev container, Doppler, Infisical) | `cmd/plugins/*` tests; nightly secrets in CI environment | Contract: empty payload rejected, not-found = error, version id returned when supported |
| Rotation failure | Shadow fails to start; unhealthy; readiness timeout; file probe fails; rename fails (injected via a Docker API fault proxy) | Integration with fault injection | Old container always still serving; ledger `rolled_back`; event emitted; no orphan after 1 minute |
| Rollback | Rollback path itself fails (double fault) | Fault injection | FATAL reported, service marked degraded, recovery on next start restores names |
| Crash | `kill -9` agent at each state of the lifecycle (§4 of target-architecture) | Integration, loop over states | After restart: no orphan shadow/backup containers, original name serving, request re-queued once |
| Docker daemon outage | Stop `dockerd` mid-poll and mid-rotation | VM-based nightly | No partial rotations started while down; recovery on daemon return; `/readyz` reflects it |
| Provider outage / timeout | Fake provider: hang, 5xx, auth error, empty body | Unit + integration | No rotation; no stale/empty injection (P-03 kept); loop recovers within timeout + backoff |
| Concurrency | Many secrets × many services; changes arriving during rotations | `-race` stress (extend `internal/watcher/concurrency_stress_test.go`) | No data race; no lost update; bounded goroutines |
| Secret leakage | After every integration scenario, scan logs, journald, `docker inspect` (file mode), `/proc/*/cmdline`, overlay `UpperDir`, `/tmp` | Helper in `test/testutil` | Zero occurrences of the synthetic secret marker outside allowed sinks |
| Race | `go test -race` on all packages | Existing CI | Zero reports |
| Fuzz | Webhook signature/timestamp parsing, `dso.host_ports`/`dso.readiness` parsers, config loader | `go test -fuzz`, nightly 10 min per target | No crash; corpus committed |
| Cross-distro | Install/upgrade/rotate on Ubuntu LTS, Debian, RHEL-family, Amazon Linux; Docker 24+ and current | Nightly | 14 consecutive green nights before v1.0 |
| Long-running stability | 72h soak: 50 services, 20 secrets rotating every 10 min, steady load through proxy | Dedicated VM, pre-release | RSS growth < 10%; goroutine count bounded; 0 lost updates; proxied error rate < 0.01% excluding BLOCKED services |
| Upgrade | Upgrade agent N-1 → N with running services | Nightly | 0 containers recreated by the upgrade itself (depends on BL-02); 0 proxied request failures (depends on BL-18) |

**What counts as production evidence (in addition to tests):** at least one external deployment running Phase 2 code for ≥30 days with rotation enabled, and published incident notes for any failed rotation (ROADMAP's own "first production deployment outside the founding team" goal).
