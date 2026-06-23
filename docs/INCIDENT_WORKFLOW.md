# Incident Workflow — Operational Debugging Guide

**Goal:** At 2 AM an operator can move from Failure → Execution → Secret → Container → Audit within two clicks.

---

## Navigation flows

### 1. Failure → Execution (1 click)

**Operations Console** (`/operations`) surfaces a "Recent Failures" section at the top whenever `OperationsDashboard.recent_failures` is non-empty.

Each failure card shows:
- Failure reason
- Truncated execution ID
- Worker ID (if present)
- Timestamp

Click **Investigate →** — calls `getExecution(f.execution_id)` and opens the `ExecutionDetailsDrawer` inline. No page navigation needed.

**URL entry point:** `/operations?exec=<execution_id>` — the page reads this param on mount and opens the drawer directly. Use this to deep-link from alerts, Slack, or CI.

---

### 2. Execution → Audit (1 click from drawer)

Inside `ExecutionDetailsDrawer`, expand the **Audit** section (lazy-loaded when opened). It renders the last 10 events from the correlation chain.

Each row shows the same amber `exec:<id>…` chip that appears in `AuditTable`. Clicking it calls `router.push('/operations?exec=<id>')`.

From the **Audit Log** page (`/audit`):

- Every row with an `execution_id` shows an amber `exec:<id>… →` chip.
- Clicking it navigates to `/operations?exec=<id>`.
- URL entry point: `/audit?execution_id=<correlation_or_exec_id>` — pre-opens the correlation timeline panel. `/audit?q=<search_term>` — pre-fills the search box.

---

### 3. Execution → Secret (1 click from drawer)

Inside `ExecutionDetailsDrawer`, expand the **Resources** section. Affected secrets are shown as blue chips derived from correlation-chain audit events where `resource_type = 'secret'`.

Click any chip → navigates to `/secrets?name=<secret_name>`.

---

### 4. Execution → Container (1 click from drawer)

Same **Resources** section, containers shown as violet chips (`resource_type = 'container'`).

Click any chip → navigates to `/discovery?container=<container_name>`. The discovery page pre-fills its search box from the `?container=` param.

---

### 5. Secret → Containers (1 click from drawer)

Open a secret in the **Secrets** page (`/secrets`). The drawer's "Containers" row is a live link: `N containers →` navigates to `/discovery?secret=<secret_name>`. The discovery page pre-fills search from `?secret=`, surfacing containers that match the secret name.

The secret drawer also shows **Recent Activity** (last 3 audit events for that secret, filtered by `resource_id=<name>&resource_type=secret`). "View all →" links to `/audit?q=<secret_name>`.

---

### 6. Container → Secrets (1 click from drawer)

Open a container in the **Discovery** page (`/discovery`). The drawer's **DSO Awareness → Managed Secrets** field renders each secret name as a clickable blue chip → `/secrets?name=<secret_name>`.

The container drawer also shows **Recent Activity** (last 3 audit events, filtered by `resource_id=<container_name>&resource_type=container`). "View all →" links to `/audit?q=<container_name>`.

---

### 7. Timeline → Execution (1 click)

Events in the **Timeline** page (`/timeline`) that carry an `execution_id` (either top-level or in `metadata.execution_id`) show an amber link in the expanded details panel: `<exec_id_prefix>… →` → `/operations?exec=<id>`.

---

## Backend filter additions

`GET /api/audit/events` now accepts two additional query params:

| Param | Effect |
|---|---|
| `resource_id` | Filter to events where `resource_id = ?` |
| `resource_type` | Filter to events where `resource_type = ?` |

These are used by the secret and container drawers to load inline audit previews without loading the full audit log.

---

## Click-count summary

| Flow | Clicks |
|---|---|
| Failure card → Execution drawer | 1 |
| Execution drawer → Audit page | 1 |
| Execution drawer → Secret drawer | 1 |
| Execution drawer → Container drawer | 1 |
| Secret drawer → Discovery (containers) | 1 |
| Container drawer → Secrets | 1 |
| Audit row → Execution drawer | 1 |
| Timeline event → Execution drawer | 1 |

All flows are ≤ 2 clicks from any entry point, satisfying the P3 goal.

---

## Known gaps

- **Execution → Container** relies on correlation-chain audit events having `resource_type = 'container'`. If events are not written with this field, the Resources section will show nothing.
- **Timeline execution_id** is only populated when the backend event carries `execution_id` at top-level or inside `metadata`. Events without this field show no execution link.
- **Secret drawer audit** uses `resource_id = <secret_name>`, which assumes secrets are identified by name in audit events. If the backend uses a UUID instead, these events will not match.
- No AI, recommendations, autonomy, forecasting, or new pages were added in this phase.
