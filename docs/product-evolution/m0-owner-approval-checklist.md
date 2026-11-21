# M0 — Implementation Order (M1 onward) and Owner Approval Checklist

**Date:** 2026-11-08 · Owner direction (authoritative, not relitigated): Option C identity via Option B (Compose) surface; `dso central` frozen; WebUI read-mostly with manual rotate + test notification only; local vault expansion deferred; reliability/security before integrations. Owner order: **Docker CI → PE-01 → PE-11 → PE-03 → PE-05 → PE-08 → per-service queue.**

## 1. M0 outputs

| Document | Content |
|---|---|
| `m0-baseline-and-reconciliation.md` | Baseline, F-E1/F-H1/AF-2/AF-7/PA-8 reconciliation, corrections (PE-16, PE-08), new PE-28..PE-34, probe method |
| `m0-docker-ci-plan.md` | Docker integration lane proposal, scenarios S1–S12 |
| `m0-pe-01-analysis.md` | PE-01 root cause, drop points, regression tests, fix direction |
| `m0-pe-11-analysis.md` | IPC authz analysis incl. PE-28/PE-29, minimal authz model |
| `m0-pe-03-pe-05-design.md` | Ledger design; rolling eligibility rules and fallback |
| `m0-pe-08-evidence-plan.md` | File-secret persistence test design |
| this file | Order and approvals |

## 2. Dependency-aware order (recommendation)

| Step | Milestone | Work | Hard dependencies | Notes |
|---|---|---|---|---|
| 0 | M0→M1 gate | Docker lane scaffold + baseline S1–S3, then failing-first S4/S7/S8/S9/S11 | none | Needed to *confirm* PE-03/05/08 and to regression-test everything after |
| 1a | M1 | **PE-29 store fix** (nil `Client` for native backends) | none | ~10 lines + regression test from the M0 probe. Crash bug; lands first inside M1 because a crashing agent re-triggers PE-03 and invalidates every later lane result. *Severity-driven, not a reorder of the owner's list.* |
| 1b | M1 | **PE-01 interim fix**: busy/lock-timeout ⇒ not-applied outcome + hash rollback (retry next poll); no `RotationSucceeded` when 0 containers rotated; lock key = project+service (PE-30); per-key mutex around observe/store (PE-31); file-injection failure is a failure (PE-33) | step 0 for the integration test (S8); unit tests need nothing | Does **not** need the ledger or queue; it removes the false success and the permanent drop |
| 1c | M1 | **PE-11 + PE-28 fix**: no caller-supplied provider config; tiered methods; non-caching connectivity check; config-digest cache key; F-E1 REST loopback opt-in | none (independent of rotation) | Owner order keeps it after PE-01 — fine; **if the owner prefers, 1c can go before 1b** because PE-28 is the only Critical item. No technical dependency either way. |
| 2a | M2 | **PE-03 ledger** (desired/applied split, adopt-on-first-run, fsync) | 1b (shares the desired/applied semantics) | |
| 2b | M2 | **Per-service queue** (single pending slot, coalescing, lock held to terminal state; PE-02, PE-23) | 2a (queue compares desired vs applied) | Owner listed the queue last (7th); **hard dependency on the ledger is the only reason it sits in M2 before PE-05/PE-08** — this matches the owner's own milestone table (M2 = ledger + queue). No reorder of priorities, only grouping. |
| 3a | M3 | **PE-05 eligibility** (`Assess()` for every strategy, `on_rolling_ineligible`, remove implicit fallback) | soft: 2a (records `blocked` outcome); can ship without it | |
| 3b | M3 | **PE-08** fix (post-start exec delivery into tmpfs; per-file probe, PE-32) | **hard: S9 evidence** from step 0 | If S9 refutes PE-08 on current Engines, 3b shrinks to PE-32 + docs |
| 4 | M4 | Proxy lifecycle (PE-10, separate process) | 2b (state machine), S12 | Highest-risk item |
| 5–6 | M5/M6 | Provider integrations (Doppler/Infisical), CLI/WebUI | M1–M3 | Out of scope now |

**Changes vs. the owner's order:** none to priorities. Two notes: (i) PE-29 is pulled to the front of M1 as a crash fix found in M0; (ii) the queue (owner item 7) depends on the ledger (item 4) and is therefore delivered in M2 next to it, as the owner's milestone table already says.

## 3. Owner approval checklist (one line per sign-off)

### M0 — docs and CI plan
- [ ] **M0-1** Accept the M0 documents as the baseline for M1 design (7 `m0-*.md` files).
- [ ] **M0-2** Accept the PE-16 correction (coverage.yml already runs `test/integration` against real Docker) and the new findings PE-28..PE-34 into the register.
- [ ] **M0-3** Approve creating `.github/workflows/integration.yml` + `test/docker/` scaffold (S1–S3 only) on a feature branch — **required before any workflow file is written or run**.
- [ ] **M0-4** Approve adding failing-first finding tests (S4, S7, S8, S9, S11) as `expected-fail`, and approve turning the M0 unit probes into real test files.
- [ ] **M0-5** Supply the source for AF-2, AF-7, PA-8 (central displacement review series) — or accept them as parked with `dso central` frozen.
- [ ] **M0-6** Confirm F-E1 (REST loopback open mode) and F-H1 (reproducible builds) are now tracked; decide whether F-E1 ships with M1.

### M1 — PE-01 + PE-11 fix design
- [ ] **M1-1** Approve the PE-29 store fix landing first in M1.
- [ ] **M1-2** Approve the PE-01 interim design (not-applied ⇒ retry; no success for 0 rotated; project+service lock key; atomic observe/store; file-injection failure = failure) and the new event semantics (CHANGELOG as behaviour change).
- [ ] **M1-3** Approve the PE-11 authorization model: remove caller-supplied provider config (default **deny**, optional one-release `warn` flag — choose), method tiers table, `ipc.secret_readers` opt-in, connectivity check root-only and non-caching.
- [ ] **M1-4** Accept the compatibility impact on `docker dso apply` / `docker dso fetch` for non-root users.
- [ ] **M1-5** Order inside M1: PE-01 before PE-11 (as stated) or PE-11 first (Critical PE-28).

### M2 — ledger + queue
- [ ] **M2-1** Ledger location: L1 JSON file in `/var/lib/dso/state` (recommended) vs L2 separate SQLite file; confirm **not** the central store.
- [ ] **M2-2** First-run/ledger-loss policy: **adopt** (no rotation) by default with `rotate_on_start` opt-in; outside-recreated container policy (`rotate once` vs `adopt`).
- [ ] **M2-3** Digest scheme: provider version id, else HMAC with host-local key (no plain hashes on disk or in labels); optional container label marker yes/no.
- [ ] **M2-4** Queue semantics: one pending slot per project+service, newer replaces older, lock held to terminal state, no time-based reclaim while the worker lives.

### M3 — stateful + file-secret safety
- [ ] **M3-1** Approve eligibility rules (B.3) applied to explicit strategies — a **breaking change** for users running `rolling` on stateful services.
- [ ] **M3-2** Default `on_rolling_ineligible`: `restart` (recommended) or `block`; default `on_rolling_failure`: `keep-old` (recommended) or `restart`.
- [ ] **M3-3** Approve the PE-08 fix direction contingent on S9 evidence; decide policy for file mode **without** a tmpfs (refuse in `dso validate` vs auto-add tmpfs).

### M4 — proxy lifecycle
- [ ] **M4-1** Approve ADR-006 (separate supervised `dso proxy` process, no Docker socket, no secret values) and keep in-process proxy as fallback until S12 soak passes.
- [ ] **M4-2** Accept S12 (0 failed proxied requests across agent restart) as the M4 exit criterion.
