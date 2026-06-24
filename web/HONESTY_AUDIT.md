# Honesty Audit

This file is the source of truth for which routes are production-ready vs. experimental/beta.
It is referenced by `components/app-shell.tsx` to decide which pages get an `ExperimentalBanner`.

Update this file whenever a route graduates from Labs → stable, or when a new experimental
page is added to the sidebar.

---

## Stable (production-ready, persistent, trusted)

These pages surface real, persisted data from the DSO agent. No banner is shown.

| Route | Description |
|---|---|
| `/dashboard` | Operational overview — posture, attention queue, recent activity |
| `/secrets` | Full secrets inventory with rotation, history, compliance drawer |
| `/discovery` | Container awareness and secret mapping |
| `/events` | Real-time event stream from the DSO agent |
| `/audit` | Immutable audit log with search, filters, export, correlation chains |
| `/operations` | Operations console — executions, DLQ, recovery |
| `/executions` | Execution history and journey view |
| `/alerts` | Active alerts list |
| `/scheduler` | Scheduled rotation jobs |
| `/analytics` | Metrics history charts |
| `/configuration` | Agent configuration editor |
| `/backups` | Backup management |
| `/security` | Session and suspicious-activity overview |
| `/security/sessions` | Active session list |
| `/security/suspicious` | Flagged suspicious events |
| `/users` | User management (admin only) |
| `/plugins` | Installed plugins |
| `/integrations` | External integrations |
| `/settings` | User settings and preferences |
| `/profile` | User profile |

---

## Beta (estimates, not authoritative)

These pages show real data but use heuristic or predictive algorithms. An `ExperimentalBanner`
with `variant="beta"` is shown to set expectations.

| Route | Description |
|---|---|
| `/forecasts` | Predicted rotation windows and risk scores derived from historical trends |

---

## Labs / Experimental (not production-ready)

These pages are under active development. Backend may not be fully implemented, data may not
persist across agent restarts, and behaviour may change without notice. An `ExperimentalBanner`
with `variant="experimental"` is shown. They are grouped into the collapsible **Labs** section
of the sidebar and collapsed by default.

| Route | Description | Status |
|---|---|---|
| `/incidents` | Incident tracking and timeline | Backend stub |
| `/recommendations` | Automated remediation suggestions | Backend stub |
| `/autonomy` | Autonomous action execution engine | Backend stub |
| `/graph` | Secret-to-container dependency graph | Backend stub |
| `/changesets` | Proposed change drafts and approval workflow | Backend stub |
| `/review` | Peer review queue for change approvals | Backend stub |
| `/workspace` | Collaborative change workspace | Backend stub |
| `/remediation` | Guided remediation flows | Backend stub |
| `/drift` | Drift detection and resolution | Backend stub |
| `/policies` | Policy engine and rule management | Backend stub |

---

## Graduating a route from Labs → Stable

1. Confirm the backend is production-ready and data persists across restarts.
2. Move the route from `EXPERIMENTAL_ROUTES` in `components/app-shell.tsx` to the stable list above.
3. Remove `experimental: true` from the item in `components/sidebar-premium.tsx` and move it to the appropriate stable nav group.
4. Update this table.
