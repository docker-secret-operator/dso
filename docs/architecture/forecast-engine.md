# Forecasts — P9

## Purpose

P9 provides operational risk forecasting: statistical, explainable, evidence-based estimates
of what is *likely* to become a problem. The goal is for operators to answer:

> "What is likely to become a problem next?"

without confusing forecasts with facts.

**Every forecast is:**
- Derived from observable evidence (version history, drift events, compliance records)
- Deterministic — the same evidence produces the same forecast
- Stateless — computed at query time, never persisted
- Ephemeral — disappears when the underlying evidence resolves

**No forecast is:**
- AI-generated
- LLM-assisted
- Autonomous
- Based on hidden state or opaque heuristics

---

## Beta Status

All P9 forecasts are permanently Beta. They are displayed with a **Beta** badge in the UI.
Predictions must never visually outrank measurements. The dashboard Labs section explicitly sits
below operational health cards, and the UI enforces the framing: *"Statistical estimates — not measurements."*

---

## Architecture

```
insights.OperationalForecaster       (internal/insights/forecaster.go)
  versionStore (sqlite.SecretVersionStore)
  driftStore   (drift.Store)
  complianceEngine (compliance.Engine)
    │
    │ ForecastAll(ctx, secrets)
    ▼
[]forecast.OperationalForecast       (internal/forecast/operational.go)
    │
    ▼
api.ForecastHandler                  (internal/api/forecast_handler.go)
GET /api/forecasts
```

`OperationalForecaster` lives in `internal/insights/` — not `internal/forecast/` — to avoid
an import cycle: `sqlite` → `forecast` → `compliance` → `sqlite`. This is the same pattern
used for the P8 `Evaluator`.

---

## Forecast Struct

```go
type OperationalForecast struct {
    ID          string              // Deterministic — same evidence → same ID
    Category    OperationalCategory // rotation | drift | compliance | operational
    Severity    ForecastSeverity    // critical | high | medium | low | info
    Title       string
    Description string
    Reason      string              // WHY the evidence leads to this prediction
    Resource    string              // secret name or resource identifier
    Confidence  float64             // [0,1] statistical probability
    PredictedAt time.Time
    Evidence    []string            // Observable facts that produced the forecast
}
```

---

## Algorithms

### 1. Rotation Forecasts

**Source:** `secret_versions` table via `SecretVersionStore.ListBySecret`

**Algorithm:**

| Versions | Rule | Confidence |
|----------|------|-----------|
| 0 | Emit medium warning: never rotated | 0.70 fixed |
| 1 | If last rotation > 30 days ago: emit low aging warning | 0.55 fixed |
| ≥2 | Compute average interval; if elapsed/interval ≥ 0.70 emit approaching-overdue | f(count, CoV) |

**Interval statistics:**

```
intervals[i] = versions[i].CreatedAt − versions[i+1].CreatedAt  (days)
avgInterval  = mean(intervals)
consistency  = 1 − stddev(intervals) / avgInterval   (coefficient of variation basis)
fraction     = (now − lastRotation) / avgInterval
```

**Severity thresholds:**

| Fraction | Severity |
|----------|----------|
| 0.70–0.89 | Low |
| 0.90–0.99 | Medium |
| ≥ 1.00 | High (overdue) |

**Confidence formula:**

```
base = 0.50  (< 3 versions)
base = 0.65  (3–4 versions)
base = 0.75  (5–9 versions)
base = 0.85  (≥ 10 versions)
confidence = base + consistency × 0.10
clamped to [0.10, 0.95]
```

The higher the version count and the more regular the interval, the higher the confidence.

**Deterministic ID:** `forecast:rotation:approaching:{secretName}` or `forecast:rotation:never:{secretName}`

---

### 2. Drift Recurrence Forecasts

**Source:** `drift_findings` table via `DriftStore.ListFindings`

**Algorithm:**

1. Filter findings to the rolling 14-day window (`DetectedAt ≥ now − 14 days`)
2. Group by `Resource`
3. For any resource with `count ≥ 3`: emit a recurrence forecast

**Confidence formula:**

```
confidence = min(count / 7.0, 0.95)
```

7 findings in a 14-day window → 95% confidence. The threshold of 7 is chosen because it represents
one finding every 2 days — a clear systemic pattern, not noise.

**Severity thresholds:**

| Count | Severity |
|-------|----------|
| 3–6 | Medium |
| ≥ 7 | High |

**Deterministic ID:** `forecast:drift:recurrence:{resource}`

**Disappear-when-fixed:** When a drift finding is resolved and removed from the detected state, the
count in the 14-day window decreases. If it drops below 3, the forecast disappears on the next evaluation.

---

### 3. Compliance Forecasts

**Source:** `ComplianceEngine.EvaluateAll` (derived from versions + drift + audit — never from stored compliance state)

**Two rules:**

**Warning pool:**
- Secrets currently in `StatusWarning` are at risk of becoming `StatusNonCompliant`
- Confidence scales with the fraction of the estate at risk: `0.50 + atRisk/total × 0.45`
- ID: `forecast:compliance:warning-pool`

**Mass non-compliance:**
- If ≥ 20% of the estate is already `StatusNonCompliant`
- Fixed confidence 0.95 (the measurement is real; the forecast is that it will persist without systemic action)
- Severity: High
- ID: `forecast:compliance:mass-noncompliant`

---

## Confidence Rules

Confidence values in P9 always have an explicit derivation:

| Rule | Basis |
|------|-------|
| `rotation:never` | Fixed 0.70 — absent evidence, moderate risk |
| `rotation:aging` | Fixed 0.55 — single data point, low signal strength |
| `rotation:approaching` | `f(versionCount, intervalCoV)` — rises with evidence quality |
| `drift:recurrence` | `min(count/7, 0.95)` — linear with event count |
| `compliance:warning-pool` | `0.50 + atRiskFraction × 0.45` — linear with estate fraction |
| `compliance:mass` | Fixed 0.95 — the 20% threshold is already measured, not predicted |

**No confidence value is arbitrary.** Every number has a named derivation that is reproducible from the evidence.

---

## Evidence Sources

| Forecast | Evidence Source | Fields Used |
|----------|----------------|------------|
| Rotation | `secret_versions` | `created_at`, `secret_name` |
| Drift | `drift_findings` | `detected_at`, `resource`, `severity` |
| Compliance | `secret_versions` + `drift_findings` + `audit_events` | Derived rotation/drift/audit status |

---

## API

### `GET /api/forecasts`

Query params:
- `category` — `rotation | drift | compliance | operational`
- `severity` — `critical | high | medium | low | info`
- `page` — page number (default 1)
- `pageSize` — items per page (default 50, max 200)

Response envelope includes `"beta": true` at the top level — so consumers know they are
receiving predictions, not measurements.

```json
{
  "forecasts": [
    {
      "id": "forecast:rotation:approaching:db-password",
      "category": "rotation",
      "severity": "medium",
      "title": "db-password approaching rotation due (82% of inferred cycle elapsed)",
      "description": "Based on 5 historical rotations, db-password rotates every 30 days on average...",
      "reason": "82% of inferred rotation cycle elapsed. At ≥70% the probability of missing the rotation SLA increases materially.",
      "resource": "db-password",
      "confidence": 0.78,
      "predicted_at": 1750640000,
      "evidence": [
        "5 total rotation events",
        "average rotation interval: 30 days",
        "last rotation: 2026-06-10 (25 days ago)",
        "interval consistency: 81% (coefficient of variation basis)"
      ],
      "beta": true
    }
  ],
  "count": 1,
  "total": 1,
  "page": 1,
  "pageSize": 50,
  "beta": true
}
```

---

## Dashboard Integration

The Labs Forecasts section appears on the dashboard **below** all measurement-based sections
(estate hero, needs attention, operational health, recent activity). This ordering is intentional:
predictions never visually outrank measurements.

It shows at most 3 Critical/High forecasts with:
- Severity dot
- Title (truncated)
- Confidence percentage
- Link to `/forecasts` for full detail

The section is hidden entirely when no Critical/High forecasts exist.

---

## Limitations

1. **`RotationOverdue` is not yet detected.** The config has no rotation interval field, so the
   rotation threshold is inferred from version history. Secrets with only manual/ad-hoc rotation
   may not produce meaningful interval signals.

2. **Drift recurrence uses `DetectedAt` only.** If findings are backdated or imported, the 14-day
   window may miscount.

3. **Compliance forecasts use the current snapshot.** If a large batch of secrets was recently
   rotated, the warning pool will shrink on the next evaluation — but the forecast may show an
   inflated number for one cycle.

4. **No operational category forecasts.** `CatOperational` is defined but no rules currently emit
   it. Reserved for future capacity or execution trend signals.

5. **All forecasts are O(secrets)** per request. Large estates should cache at the API gateway layer.

6. **No history.** Forecasts are not stored, so trend lines ("this has been high for 3 weeks")
   cannot be shown. If historical trending is needed, a time-series sink must be added.
