# DSO Honesty Audit

**Date:** 2026-06-23
**Question for every page:** would an SRE who trusted this number/page at 2 AM be misled?

Legend:
- **Production** — real data, persists across restart, useful, safe to trust.
- **Experimental** — logic exists but the data is a proxy, in-memory (lost on
  restart), has no generation pipeline, or is otherwise not server-authoritative.
- **Empty** — UI shell; no meaningful backend data.

Evidence is cited to source where verified.

---

## Per-route classification

| Route | Status | Reason / evidence |
|---|---|---|
| `/dashboard` | **Production*** | Real: health, operations, audit, secrets, discovery. *Two embedded trust gaps: the **"Drifted" tile is a proxy** (`status === 'error'`, not drift detection) and the **rotation band reads Fresh when `next_rotation` is unset**. See TRUST_GAPS #1, #3. |
| `/secrets` | **Production** | Real `getSecrets`/`rotateSecret`; search + sort + per-secret rotate. No bulk/history/diff, and loads all rows (scale — TRUST_GAPS #5). |
| `/operations` | **Production** | Real `OperationsDashboard` (queue/worker/execution/DLQ). |
| `/audit` | **Production** | Persistent audit store; real events. |
| `/configuration` | **Production** | Real config file; editor persists with backup + atomic write + rollback. No live reload (restart required) — documented, not hidden. |
| `/events` | **Production** | Real event store. Live WS works in prod (not under `next dev`); HTTP fallback added. |
| `/discovery` | **Production** | Real Docker discovery ("Containers"). |
| `/alerts` (+ `/rules`) | **Production** | Real alert store/service. |
| `/users`, `/admin/sessions` | **Production** | Real user/session stores. |
| `/security` (`/events`, `/sessions`, `/suspicious`) | **Production** | Real auth/session/security data. |
| `/profile`, `/settings` | **Production** | Real user/profile + UI settings. |
| `/backups` (+ `/recovery`) | **Production** | Real backup service. |
| `/plugins` | **Production** | Real registry + persistent metadata; enable/disable works. |
| `/integrations` | **Production** | Real integration manager + persistence. |
| `/timeline` | **Production** | Reads the real, persistent event store. |
| `/executions` | **Production** | Real execution store. |
| `/analytics` | **Production (thin)** | Reads real metrics history; mostly charts over real data. |
| `/scheduler` | **Production (empty until jobs exist)** | Real scheduler; metrics now aggregate correctly. No jobs are registered by default, so it legitimately shows zero — that is honest, not fake. |
| `/drift` | **Experimental** | Engine `RunScan` exists but the store is **in-memory** (`rest.go:1199`) → findings vanish on restart. The dashboard's "drift" doesn't even use this engine (it's the status proxy), so the two can disagree. |
| `/policies` | **Experimental** | **In-memory store** (`rest.go:1200`) → rules an operator creates are lost on restart. |
| `/incidents` | **Experimental** | Correlation handler wired (sqlite-capable) but **no incident-generation pipeline** → empty in practice. |
| `/recommendations` | **Experimental** | Persistable, but **nothing generates recommendations** → empty in practice. |
| `/forecasts` | **Experimental** | Has a real generation pipeline (manual "Run" + background ticker) but depends on metrics input; output is **unverified/estimative** and should not be treated as authoritative. |
| `/autonomy` | **Experimental** | Persistable, but **no action-generation pipeline** → empty in practice. |
| `/graph` (Dependency Graph) | **Experimental / Empty** | **In-memory graph** (`rest.go:1201`), starts empty, no populator. |
| `/changesets`, `/review`, `/workspace`, `/remediation` | **Experimental** | Read real `/discovery` + `/secrets` + `/events`, but the "analysis"/plan is **computed client-side**; the draft/review/approval persistence exists but the end-to-end governance flow is unverified. |

\* Production with caveats — the page is real but contains specific misleading elements listed in TRUST_GAPS.

---

## Summary buckets

```
Production (trust it)
---------------------
Dashboard*  Secrets  Operations  Audit  Configuration  Discovery(Containers)
Events  Timeline  Executions  Alerts  Users  Security  Profile  Settings
Backups  Plugins  Integrations  Analytics  Scheduler(empty-but-honest)

Experimental (label clearly / move to Labs)
-------------------------------------------
Drift  Policies  Incidents  Recommendations  Forecasts  Autonomy
Dependency Graph  Changesets  Review  Workspace  Remediation

Does not exist as a page
------------------------
"Providers" (lives under Configuration)   "Hosts" (no page)
```

\* Dashboard is Production but ships two misleading elements (drift proxy, rotation false-green) that must be fixed for it to be fully trustworthy.

---

## Corrections to the earlier verbal review

- The header **"Operational/Degraded" pill is REAL**, not decorative: `topbar.tsx`
  queries `/health` every 10s and derives the label from `health.status`. My
  earlier "always green / decorative" claim was wrong. It is, however, a binary
  up/down — not a rich aggregate of subsystem health.
