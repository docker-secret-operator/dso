# DSO Cleanup Plan — Close the Honesty Gap

**Date:** 2026-06-23
**Goal:** make the UI never imply more than the backend guarantees. Trust > feature count.
**Ordering principle:** anything that could mislead an operator during an incident comes first.

---

## P0 — Stop misleading operators (do this first; ~1–2 days, mostly frontend)

1. **Rename the "Drifted" proxy → "Secret errors."**
   Dashboard estate hero + Needs-attention. It is `status === 'error'`, not drift.
   (TRUST_GAPS #1) — Frontend-only; quick.

2. **Rotation band: no false green.**
   If no secret has `next_rotation`, render **"Rotation data unavailable"** and
   bucket unknowns as "Unknown" (new bucket) instead of "Fresh." Status line must
   not say "Secured" when rotation is merely unmeasured. (TRUST_GAPS #2)

3. **Mark Experimental, in place.**
   Add an `Experimental` badge in the nav and a banner on these pages —
   Drift, Policies, Incidents, Recommendations, Forecasts, Autonomy, Dependency
   Graph: *"Experimental — no production data is generated yet; state may not
   persist."* (TRUST_GAPS #3, #4) — Frontend-only.

4. **Empty ≠ all-clear.**
   Replace zero-count cards / empty tables on the Experimental pages with an
   explicit empty state ("under development / no production data"), so "Incidents
   0" can't be read as "no incidents." (TRUST_GAPS #4)

5. **Forecasts: label as estimates/Beta.** Visually subordinate predicted values
   to measured ones. (TRUST_GAPS #5)

## P1 — Structural honesty (~3–5 days)

6. **Group Experimental routes under a "Labs" nav section** so the primary nav is
   the ~18 Production pages; Labs holds the 7+ experimental ones. Reduces the
   "28 pages, a third hollow" problem.

7. **Persist or disable.** For Drift & Policy: either add SQLite stores +
   migrations (preferred — they already have engines), or disable writes and
   state plainly that data is non-persistent. No silent loss. (TRUST_GAPS #3)

8. **Make the dashboard's drift signal real** (optional but high-value): point the
   "Secret errors"/drift tile at the real drift engine once it persists, and
   reconcile it with the `/drift` page so they never disagree.

## P2 — Scale honesty + incident workflow (~1 week)

9. **Scale:** server-side pagination + filter for `/api/secrets`; virtualize the
   secrets table and any 50+ row list; stop global-search from refetching the
   whole dataset every 60s. (TRUST_GAPS #6)

10. **Document tested scale honestly** in the README/UI: "validated at N secrets;
    untested beyond." Don't imply unbounded scale.

11. **Incident user-journey links** (see Phase 8 findings below): wire
    failure → execution trace → secret → container → audit so the chain is ≤2 clicks.

---

## Phase 8 — User-journey audit (current reality)

- **Secret rotation failure → root cause:** ❌ not ≤2 clicks today. A failed sync
  appears in Needs-attention/Operations, but there is no cross-link from the
  failure to its execution trace, then to the secret, container, and audit
  entry. Each is a separate page reached by manual navigation. **Gap to close (P2 #11).**
- **Provider outage → impact:** ⚠️ partial. Provider-typed alerts surface in
  Needs-attention, but there is no "which secrets/containers depend on this
  provider" impact view. **Gap.**
- **Drift → affected secrets:** ❌ not usable. Drift is in-memory/Experimental and
  the dashboard signal is a proxy. Cannot reliably identify drift-affected
  secrets today. **Blocked on P1 #7/#8.**

---

## What this plan deliberately does NOT do

- It does not add new features. Every item either tells the truth about an
  existing one, hides it, or makes an existing real feature scale/persist.
- It does not touch the Production pages' behavior (only the dashboard's two
  misleading elements).

## Definition of done

- No metric on a Production page is a proxy, estimate, or non-persistent value
  presented as authoritative.
- Every Experimental page is labeled and cannot be mistaken for "all clear."
- Tested scale is stated; nothing implies unlimited scale.
- The two SEV-1 dashboard elements (drift proxy, rotation false-green) are fixed.
