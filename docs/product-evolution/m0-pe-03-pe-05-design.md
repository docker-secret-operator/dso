# M0 — Design Prep: PE-03 Applied-Version Ledger and PE-05 Rolling Eligibility

**Date:** 2026-11-08 · **Status:** design proposal only — no schema, migration or code was created. Needs owner approval (see `m0-owner-approval-checklist.md`). Labels as in `m0-baseline-and-reconciliation.md`.

---

## Part A — PE-03: persisted per-container applied-version ledger

### A.1 Problem (confirmed in code)

The only record of "what was delivered" is `TriggerEngine.secretHashes`, an in-memory `sync.Map` (`internal/agent/trigger.go:29`). On start, targets are populated first (`internal/cli/agent.go:226-229`), then each poller fires immediately (`trigger.go:425-428`) with `oldHash == ""` (`:262-267`), so every rotation-enabled secret is dispatched to every target. With the code default strategy `restart` (`trigger.go:324-326`, PE-04) an agent restart, upgrade, or crash (PE-29) recreates every managed container. Runtime test not yet executed — the startup no-op test is the first item in the Docker lane (`m0-docker-ci-plan.md` scenario S4).

### A.2 Where the ledger lives — recommendation

| Option | Pros | Cons | Verdict |
|---|---|---|---|
| **L1 — new file `/var/lib/dso/state/ledger.json` (owned by a new small `internal/ledger` package, same dir as `StateTracker`)** | Tiny dataset (rows = services × secrets); no new dependency; matches ADR-003; human-inspectable; dir already `0700` (`state_tracker.go:36`) | Must add fsync discipline (today's `persistState` renames without fsyncing file or directory, `state_tracker.go:189-207`) | **Recommended** |
| L2 — separate SQLite file `/var/lib/dso/state/runtime.db` via the already-vendored `modernc.org/sqlite` (`go.mod:26`), own package and schema | Transactions, crash safety for free, queryable | Heavier for ~100 rows; schema migrations to maintain | Acceptable alternative if the owner prefers SQL |
| L3 — reuse the `dso central` SQLite store (`internal/store/sqlite`) | Existing code | Couples the runtime to a **frozen** component (owner decision, ADR-002); central DB has its own locking/restore/audit semantics; would be a central schema change (forbidden) | **Rejected** |
| L4 — container labels only | Survives ledger loss; travels with the container | Labels are immutable after create and visible in `docker inspect`; a raw content hash in a label enables offline guessing of low-entropy secrets | Use only as a *secondary* hint (A.6), never with an unkeyed hash |

### A.3 Record shape (proposed)

Key: `(compose_project, compose_service, secret_ref)` where `secret_ref = provider + ":" + secret`. Container ID is a field, not the key, because every rolling/restart rotation creates a new container ID.

```json
{
  "schema": 1,
  "hmac_key_id": "k1",
  "secrets": {
    "aws:db_password": { "desired": "v:AWSCURRENT:3f2a…", "observed_at": "2026-11-08T10:00:00Z" }
  },
  "applied": {
    "shop|api|aws:db_password": {
      "container_id": "9c1e…", "container_name": "shop-api-1",
      "applied": "h:6b1d…",          // provider version id ("v:…") or keyed digest ("h:" + HMAC-SHA256(host_key, canonical(value)))
      "outcome": "succeeded",        // succeeded | rolled_back | blocked | failed | adopted
      "strategy": "rolling", "reason": "", "at": "2026-11-08T10:01:12Z"
    }
  }
}
```

- **Never the value.** Use the provider version id when one exists (future `VersionedProvider`, ADR-005); otherwise an **HMAC** with a host-local random key stored `0600` in the state dir — not a plain SHA-256, so the ledger (or a label, A.6) does not become an offline oracle for weak secrets. `ComputeHash` (`internal/agent/cache.go:37`) stays the in-memory comparator.
- Writes: write temp → `fsync(file)` → `rename` → `fsync(dir)`. One writer goroutine (the rotation controller) — no cross-process access except read-only CLI (`dso rotation status`, later).
- Corruption or unknown schema ⇒ log, rename aside as `ledger.json.corrupt-<ts>`, start with an **empty** ledger in **adopt** mode (A.5). Fail-safe direction = do not rotate.

### A.4 Write path (during rotation)

1. Observe: provider value fetched → compute `d = version-or-HMAC`. If `d != secrets[ref].desired` ⇒ update `desired` (persist) and enqueue for each `(project, service)` that references `ref`. (Replaces the `secretHashes.Store` at `trigger.go:284`.)
2. Per-target outcome (from the rolling/restart goroutine, not from `onComplete`'s first error): on success write `applied[...] = {container_id: new ID, applied: d, outcome: succeeded}`; on rollback write `outcome: rolled_back` **without** changing `applied`; on BLOCKED (Part B) write `outcome: blocked`.
3. Event: `RotationSucceeded` only if every targeted `(project,service)` now has `applied == desired` (ties into the PE-01 fix).

### A.5 Read path (agent startup)

1. Load ledger (or empty).
2. `populateInitialTargets` (unchanged) gives the running containers; join on compose project/service labels (`com.docker.compose.project` / `.service`).
3. First poll per secret: compute `d`.
   - Row exists, `applied == d` and row's `container_id` is still the running container ⇒ **no action** (fixes PE-03).
   - Row exists, `applied != d` ⇒ enqueue (genuine change while the agent was down).
   - Row exists but container ID differs (someone ran `docker compose up` outside DSO) ⇒ the container was created from the Compose file's env/`.env`, so its value is unknown ⇒ policy `on_unknown: adopt|rotate` (default **rotate once**, because the outside recreate may have dropped DSO-injected values — to confirm with the owner).
   - **No row** (first run after upgrade, or ledger lost) ⇒ **adopt**: record `applied = d, outcome: adopted`, no rotation; opt-in `agent.rotation.rotate_on_start: true` restores today's behaviour. Rationale: avoiding a fleet-wide restart outweighs catching a change that happened exactly during the upgrade window; document this trade-off.
4. Crash mid-rotation: existing recovery (`internal/agent/recovery.go`, `trigger.go:161-191`) runs first; any row whose last outcome is not terminal is treated as "applied unknown" ⇒ re-enqueue once.

### A.6 Optional secondary marker (owner decision)

Stamp the new container at create time with `dso.applied.<secret>=<version-or-HMAC>` (labels are copied by `container_clone.go:64`). Lets the agent recover the applied state if the ledger is lost and detects outside recreation. Requires the HMAC (never plain hash). Not needed for the first M2 cut.

### A.7 Tests (M2)

- Unit: encode/decode, fsync path with fault injection, corrupt-file adopt, HMAC stability.
- Docker lane: restart agent with unchanged provider ⇒ **0** containers recreated (compare container IDs before/after); change provider while agent stopped ⇒ exactly the affected services rotate once; delete ledger ⇒ adopt, 0 recreated; `kill -9` mid-rotation ⇒ one re-enqueue, no orphans.

---

## Part B — PE-05: when is `rolling` safe?

### B.1 Problem (confirmed in code)

The stateful guard exists only when the strategy is `auto`/empty (`internal/watcher/controller.go:825-834` → `internal/analyzer/container_analyzer.go:85-115` → `internal/strategy/decision_engine.go:119-133`). An explicit `rolling` (label `dso.update.strategy` or config) goes straight to `RollingStrategy.Execute`, which clones `HostConfig` (binds, mounts, port bindings) verbatim (`internal/rotation/container_clone.go:69-86`) and starts the shadow while the original still runs. Consequences: two DB processes on one data directory (most refuse via lock file → health fail → rollback; some may not), fixed host ports clash (PE-06), and non-proxy rolling failure silently falls back to stop-and-recreate (`controller.go:1153-1163`). Data-integrity impact is **inferred** — scenario S7 in the Docker lane will measure it per image.

### B.2 Detection signals available to DSO (from `docker inspect`, no Compose file parsing needed)

| Signal | Source | Already computed? |
|---|---|---|
| S-VOL: any `Mounts[]` of `Type` volume or bind with `RW == true` | inspect `.Mounts` | Partly (prefix heuristic `container_analyzer.go:102-113`) |
| S-IMG: image name hints (mysql, postgres, mariadb, mongo, redis, memcached, …) | `.Config.Image` | Yes (`:90-100`) |
| S-PORT: `HostConfig.PortBindings` with non-zero host port | inspect | Yes (`:74-83`) |
| S-PROXY: every bound host port is declared in `dso.host_ports` and the proxy is running | label + `r.ProxyManager != nil` | Yes (`controller.go:999-1008`) |
| S-HC: image/container has a `HEALTHCHECK` (`.Config.Healthcheck` non-nil, not `NONE`) | inspect | Yes (`HasHealthCheck` in analyzer) |
| S-READY: `dso.readiness` probe label (proposed BL-09) | label | No |
| S-NAME: `container_name:` set explicitly (rename semantics) | `looksExplicitlyNamed` | Yes (`:32`) |
| S-SCALE: Compose replicas >1 / `deploy` | labels `com.docker.compose.container-number` | No |
| S-OVERRIDE: `dso.rotation.allow_shared_volumes: "true"` (proposed) | label | No |

Compose-level information (`volumes:` long syntax, `depends_on`) is already reflected in inspect; DSO does not need to parse the Compose file for eligibility.

### B.3 Eligibility rules (proposed `Assess()` — applied to **every** strategy)

`rolling` is **ELIGIBLE (zero-downtime)** only if all hold:

1. **No shared writable state:** no RW volume/bind mount, *except* paths the operator declares safe via `dso.rotation.shared_ok: "/var/log/app,/cache"`; tmpfs and read-only mounts are fine.
2. **Not a known stateful image** (S-IMG) unless S-OVERRIDE is set.
3. **Ports:** no fixed host-port binding, **or** every fixed host port is proxy-owned (S-PROXY).
4. **Readiness:** S-HC or S-READY present. Without either, the gate is ~2 s of "running" (`internal/rotation/health_check.go:50-56`, PE-09) — not a zero-downtime contract.
5. **Traffic switch:** for host-port traffic, the proxy handoff path (`controller.go:1018-1070`); for east-west traffic, acknowledge PE-07 (shadow joins aliases before healthy) — until BL-22 lands, report `zero_downtime: "host-ports-only"` rather than `true`.
6. **File secrets:** only after PE-08 is resolved (otherwise `rolling` with file-mode secrets is reported as `degraded-guarantee`).

Outcomes:

| Situation | Result |
|---|---|
| All rules hold | `rolling`, `zero_downtime: true` (or `host-ports-only`) |
| Rule 1 or 2 fails (stateful) | **BLOCKED for `rolling`** — do **not** start a second instance |
| Rule 3 fails (fixed port, not proxy-owned) | BLOCKED for `rolling` (it cannot bind anyway) |
| Rule 4 fails only | `rolling` allowed, `zero_downtime: false`, warning "no readiness gate" |

### B.4 Fallback behaviour when `rolling` is not eligible

- **Default:** refuse the rolling attempt *before creating anything*, and apply the explicit policy `on_rolling_ineligible` (default **`restart`**, i.e. stop → recreate → start with the new secret; downtime expected), logged at WARN, audited, event `rotation_strategy_downgraded` with the blocker list, and surfaced in `GetStatus`/WebUI read-only view. Rationale: secret delivery (possibly a revocation) must still happen; silently doing nothing is PE-01 again.
- `on_rolling_ineligible: block` — do nothing, mark the service `blocked` in the ledger (`outcome: blocked`), emit a failure event; for operators who require a maintenance window.
- Remove the **implicit** post-failure fallback to restart for non-proxy services (`controller.go:1153-1163`); make it `on_rolling_failure: keep-old (default) | restart`.
- `dso rotation plan` (BL-08) prints the same `Assess()` result so operators see the downgrade before it happens.

Owner decision needed: default `restart` vs `block` (recommendation: `restart` + loud warning, because a missed revocation is worse than a planned restart for most Compose users; stateful DBs with `restart` are the documented safe path).

### B.5 Tests (M3)

- Unit: `Assess()` table over synthetic inspect fixtures (each rule, each override).
- Docker lane (S7): `postgres:16`, `mysql:8`, `redis:7` with a named volume + explicit `rolling` ⇒ BLOCKED/downgraded, **no second container ever created** (watch Docker events for `create`), data directory intact (`pg_controldata`/`mysqlcheck`), downtime measured; stateless `nginx` + HEALTHCHECK + `dso.host_ports` ⇒ rolling, 0 failed requests (`test/integration/proxy_rotation_downtime_test.go` harness).
