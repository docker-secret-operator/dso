# DSO Trust Gaps

**Date:** 2026-06-23
**Ranking:** by how badly it could mislead an operator *during a production
incident*. A wrong number at 2 AM is worse than a missing one.

---

## SEV-1 — Could mislead during an incident (fix first)

### 1. "Drifted" is a proxy mislabeled as drift
- **Where:** dashboard estate hero + Needs-attention queue.
- **Reality:** "drifted" = `secret.status === 'error'` (`lib/dashboard/rotation.ts`,
  `isDrifted`). That is *secret error state*, not configuration drift vs the provider.
- **Why it's dangerous:** during an incident an operator reads "3 drifted" and
  investigates drift; the real signal is "3 secrets are erroring." Different
  problem, wrong investigation path. Worse: the `/drift` page uses a *separate*
  in-memory engine, so the dashboard count and the Drift page can disagree.
- **Fix:** rename to **"Secret errors"** everywhere on the dashboard, OR wire the
  dashboard to the real drift engine. Do not call it drift until it is drift.

### 2. Rotation band can show a false-green estate
- **Where:** dashboard "Secret estate" band + posture.
- **Reality:** buckets derive from `next_rotation`. Secrets with no
  `next_rotation` are bucketed **Fresh**. If a provider doesn't populate
  `next_rotation`, the whole estate reads green/"Secured" while rotation is
  actually unknown.
- **Why it's dangerous:** a green "Secrets secured" hero implies rotation is
  healthy when it may simply be *unmeasured*.
- **Fix:** when `next_rotation` is absent across the estate, show **"Rotation
  data unavailable"** instead of green; bucket unknowns as "Unknown," not "Fresh."

### 3. Drift & Policy data is lost on restart (silent data loss)
- **Where:** `/drift`, `/policies`. Backends are **in-memory**
  (`rest.go:1199-1200`).
- **Why it's dangerous:** an operator creates a policy rule or acknowledges a
  drift finding; on the next agent restart it silently vanishes. Silent loss of
  operator-entered state is a trust-killer.
- **Fix:** add SQLite stores (persist) OR mark Experimental and disable writes /
  warn that state is non-persistent.

---

## SEV-2 — Empty features that *look* finished

### 4. Polished-but-hollow pages imply capability that isn't there
- **Where:** Incidents, Recommendations, Autonomy, Dependency Graph (empty);
  Forecasts (estimative). They render finished-looking tables, metric cards, and
  zero-counts — indistinguishable from "all clear."
- **Why it's dangerous:** "Incidents: 0" reads as "no incidents," when the truth
  is "incident detection isn't running." False reassurance.
- **Fix:** Experimental banner + "no production data is generated yet" empty
  state; nav badge. Never let "no pipeline" look like "all clear."

### 5. Forecasts presented without an authority caveat
- **Where:** `/forecasts`.
- **Reality:** estimates from a trend model over (possibly sparse) metrics.
- **Fix:** label as estimates/Beta; never present predicted values with the same
  visual weight as measured values.

---

## SEV-3 — Scale honesty

### 6. "Load-all" + no virtualization implies unlimited scale
- **Where:** dashboard loads the **entire** secret list (`getSecrets()`, no
  pagination); `/secrets` renders every row (no virtualization); global search
  refetches **all** secrets/containers/events every 60s
  (`components/global-search.tsx`).
- **Reality:** fine at ~50; unvalidated at 500; likely sluggish at 5000.
- **Fix:** server-side pagination/filter + list virtualization; document tested
  scale honestly rather than implying it's unbounded.

---

## Resolved / not actually a gap

- **Operational badge** — REAL (`topbar.tsx` → `/health`). Not decorative.
  (Earlier review was wrong.) Minor improvement only: make it a richer aggregate
  than binary up/down.
- **Recent activity / dashboard sections** — now distinguish loading from empty
  (skeletons added), so they no longer flash false "all clear" during load.

---

## Severity summary

| # | Gap | Sev | Type |
|---|---|---|---|
| 1 | "Drifted" proxy mislabeled | SEV-1 | proxy / misleading |
| 2 | Rotation false-green when `next_rotation` absent | SEV-1 | misleading |
| 3 | Drift/Policy lost on restart | SEV-1 | non-persistent |
| 4 | Empty pages look finished | SEV-2 | fake completeness |
| 5 | Forecasts shown as authoritative | SEV-2 | estimate-as-fact |
| 6 | Load-all / no pagination | SEV-3 | scale overclaim |
