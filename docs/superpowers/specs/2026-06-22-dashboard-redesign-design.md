# Dashboard Redesign Specification – Security Operations Focus

**Date:** 2026-06-22
**Status:** Approved (spec authored by product owner)

Redesign the DSO dashboard to feel like a professional security platform (Vault,
Doppler, Datadog), not a marketing page. Calm, information-dense, operator-focused.
Keep the existing dark Tech Noir palette — this is about hierarchy and usability,
not rebranding.

---

## Design Philosophy

Remove decorative effects and visual noise:
- `bg-mesh`, `bg-dashboard-orbs`, excessive glow, glass blur panels, gradient text,
  decorative indigo highlights.

Replace with:
- Hairline borders, subtle elevation, flat surfaces, strong spacing, clear hierarchy.

Color communicates status only — Healthy / Warning / Critical. Never decoration.

---

## Typography

- Inter → primary UI font.
- JetBrains Mono → machine identifiers: secret names, paths, versions, digests,
  container IDs, timestamps, technical identifiers. Identifiers must feel visually
  distinct from descriptive text.

---

## Layout Structure (top → bottom by operator priority)

### 1. Posture Summary
Calm status overview. Only the four numbers that matter: Managed Secrets,
Need Rotation, Drifted, Coverage %. No hero sections, no marketing copy, no
"Operations Command Center" title. Compact, immediately readable.

### 2. Rotation Health Strip (Signature Component)
Full-width horizontal strip representing the entire secret estate.
Categories: Fresh / Aging / Overdue / Drifted. Color encodes freshness,
percentage distribution visible, immediate understanding of rotation posture.
Unique to a secrets-management platform.

### 3. Needs Attention
Operator action queue, prioritized: (1) Overdue rotations, (2) Drift detected,
(3) Failed syncs, (4) Provider issues. Answers "What requires my attention right
now?" Replaces scattered alerts with one prioritized queue.

### 4. Operational Health
Keep existing API data. Translate infrastructure metrics into human language.
Instead of Goroutines / Agent Load → Workers Active, Queue Status, Tasks Executing,
Success Rate, Processing Latency. Hide implementation details operators don't care about.

### 5. Recent Activity
Clean audit stream. JetBrains Mono for timestamps, versions, IDs, secret names.
Layout: timestamp → action → target → result. Compact and highly scannable.

```
06:23:18Z  ROTATED  database-prod-password  SUCCESS
06:18:42Z  SYNCED   redis-token             SUCCESS
06:15:07Z  FAILED   vault-provider          ERROR
```

---

## Implementation Rules

### Preserve Existing APIs
No mock data. Keep all existing integrations: health, metrics, operations, alerts,
audit, execution status. Reuse existing hooks and services. If rotation or drift
counts are unavailable, use nearest real fields and clearly document missing backend
endpoints. Never fabricate numbers.

### Files To Modify
- Primary: `app/dashboard/page.tsx`
- Supporting: widgets used by the dashboard, reusable cards, tables, health components
- CSS: `globals.css` — remove obsolete decorative utilities

### Routes
Do not touch the remaining routes. Only redesign the dashboard experience.

### Cleanup
Delete redundant dashboard variants: `dashboard-modern/`, `dashboard-simple/`.
There should be a single production dashboard.

---

## Visual References
Target the professionalism and density of HashiCorp Vault, Doppler, Datadog,
Grafana, GitHub Actions. Avoid crypto landing pages, gaming UIs, neon gradients,
glowing spheres, oversized hero sections, marketing aesthetics.

## North Star
This is a security platform. It earns trust through calm layouts, density, clarity,
hierarchy, and actionability. Answer "What is the state of my secrets? What needs
attention? Can I trust this system?" — not "How visually impressive is this?"
