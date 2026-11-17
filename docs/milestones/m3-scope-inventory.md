# M3 scope inventory (proposal, not an approval)

Status: working inventory on `feature/m3-development`, based on the frozen M2 checkpoint, 2026-11-17. Changes no behaviour. Starting M3 requires a separate owner approval (see section 6). Evidence references are file paths in this repository; "UNVERIFIED" marks anything not checked in code.

## 1. Baseline after M2

- M2 checkpoint is complete for the revised, local-only scope; production qualification is deferred and N19 (managed delivery) remains an open production blocker (`docs/milestones/m2-checkpoint.md`).
- `dso central` is a local CLI vault (SQLite, envelope crypto, immutable versions, rollback, rename, non-cascading delete, backup/restore, unkeyed, hash-chained audit trail per ratified decisions (documented as not tamper-proof)). Access control is OS file permissions only; there is no network API for it (ADR-011).
- The agent REST server and embedded Web UI are separate: bearer token (`DSO_AUTH_TOKEN`) or one WebUI operator session; CSRF origin check, baseline CSP/HSTS, per-IP rate limit, login limiter, WebSocket recheck already exist.
- The central store does not feed the agent, and the REST `/api/secrets` is the agent runtime, not the vault.

## 2. M3/M4 terminology conflicts

| Source | "M3" | "M4" |
|---|---|---|
| Audit roadmap (`docs/audit/11-DEVELOPMENT-ROADMAP.md`) | Identity and governance: RBAC, scoped tokens, audit, SSO/MFA, approvals | Integrations/DX: OpenAPI, SDK, Terraform, webhooks |
| Product-evolution roadmap (`docs/product-evolution/m0-owner-approval-checklist.md`) | Stateful and file-secret safety (PE-05 eligibility, PE-08 tmpfs delivery) | Proxy lifecycle (PE-10, separate process) |
| Central-server design (`docs/design/central-server-design.md` s8) | Central secret management (server-sourced secrets, `dso run`) | Web UI for the vault |
| M2 central-store design docs | "M3" = the future network server that consumes the identity/authz/audit machinery | n/a |

This inventory adopts: **M3 = "Control-plane security and operator experience"** on the EXISTING agent REST/Web UI (a narrowed slice of audit-roadmap M3 and G009-G011). Product-evolution PE-05/PE-08 are referred to by their PE ids, never as "M3". Central-server-design M3/M4 are not adopted and stay unauthorized. OpenAPI/metrics (audit-roadmap M4 items) are pulled in only as small contract/observability slices.

## 3. Gaps in the existing control plane (verified against code)

| Area | Status | Evidence and note |
|---|---|---|
| Authentication gate (bearer, session, default-closed, loopback-only open mode) | IMPLEMENTED | `internal/server/auth_gate.go`, `access_wiring.go` |
| CSRF/origin check, method enforcement, WS upgrade origin | IMPLEMENTED | `internal/server/rest.go` ServeHTTP (~L265-300), `methodNotAllowed` |
| CSP/HSTS/security headers; WS recheck on logout/expiry/token rotation | IMPLEMENTED | `internal/server/rest.go` `secureHeadersFor`, `wsRecheck` (G011 largely closed; residual WS edge cases UNVERIFIED) |
| Authorization boundaries (roles, scoped tokens) | MISSING | One static bearer token and one operator; any authenticated caller can hit rotate/config/sessions (`internal/auth/auth.go`; G009) |
| Session management (list, revoke-others, idle/absolute timeout, cap) | PARTIAL | `internal/webuiauth/session.go`; single operator identity by design, no per-session revoke by id, no MFA/password change UI (UNVERIFIED) |
| Audit attribution of REST actions | PARTIAL | `internal/server/audit_actor.go` derives actor server-side for rotate and notification-test only (`rest.go:1281,1333`); bearer actor is a token fingerprint, not a person |
| Audit of login, logout, failed login, revoke-others | MISSING | no `audit.Log` call in the auth/session handlers (`rest.go`; G010) |
| Audit durability and export for REST actions | MISSING | `internal/audit/audit.go` writes to zap and an in-memory event store; `/api/audit` is declared non-durable (`rest.go` ~L1011-1016). A durable hash chain exists only in the central store (`internal/store/sqlite/audit.go`); it is unkeyed (ratified, not tamper-proof) and is not wired to REST. A keyed HMAC chain (G025) is NOT implemented anywhere |
| Operator permissions in Web UI | PARTIAL | `web/app/users` shows identity and sessions only (`web/components/users-content.tsx`); no role-aware UI because no roles exist |
| Web UI workflows | PARTIAL | pages for secrets, containers, drift, alerts, audit, integrations, settings exist (`web/app/*`); mutations limited to manual rotate, notification test, revoke-others; no confirmation/audit-trail workflow (workflow completeness UNVERIFIED) |
| API contract / OpenAPI | MISSING | no spec found in repo; routes are a hand-written switch (`rest.go` ~L300-345); G022 |
| Metrics | PARTIAL | Prometheus server started by the agent (`internal/cli/agent.go` ~L238, `pkg/observability/metrics.go`); REST auth/session/rotation-request metrics and a metric catalogue are UNVERIFIED (G023) |
| Service bypass of central audit (direct DB use) | MISSING | stated and accepted in M2 (risk R19); resolved only by a future server architecture |

## 4. Not authorized (must not start automatically)

Enterprise RBAC; SSO/OIDC/SAML/SCIM/MFA as an identity system; multi-tenancy; any remote or HTTP vault API (ADR-010, ADR-011); a second authentication architecture beside the bearer token and WebUI session; central-secret agent provider; KMS/HSM and root-key rotation; permanent purge (design only); approval workflows; hosted control plane. ADR-002 and ADR-010 say no central network API/RBAC without a new ADR; this inventory does not supply one.

## 5. Recommended M3: control-plane security and operator experience

Principle: harden and complete what exists, on the existing bearer token and single WebUI operator; add no identity system and no vault exposure.

Proposed order:
1. Route inventory and authorization test matrix (every route x {none, bearer, session, open} x method) as a regression guard before any change. (S)
2. Audit all session/auth events (login success/failure, logout, revoke-others, rate-limit trips) with server-derived actor, IP and request id; never log credentials. (M)
3. Route REST audit through one persistent, append-only sink (a separate append-only store; reusing the existing unkeyed hash-chain code is possible, and a keyed chain would be G025, a separate decision), with a read-only export endpoint; `/api/audit` stops being "non-durable". Depends on decision D2. (L)
4. Coarse permission split on existing credentials: read-only vs operate (rotate, notification test, session revoke), e.g. an optional second read-only token; default behaviour unchanged. Depends on decision D1. (M)
5. Residual hardening from G011: WS edge cases, session revoke by id, idle/absolute timeout visibility, token-rotation behaviour documented and tested. (S-M)
6. Web UI operator workflows: confirmations for mutations, show the acting identity and audit trail per action, hide/disable controls by permission from slice 4, error states; vitest plus embedded-asset parity check. (M)
7. OpenAPI spec for the existing `/api/*` routes with a CI contract test that fails on route/spec drift. (M)
8. REST metrics (auth failures, session count, rotate requests, WS clients) and a documented metric catalogue. (S)

Exit gate (proposal): the per-route authorization matrix passes; every state-changing REST action and every auth event produces an attributed durable audit record; spec matches handlers in CI; no new identity store.

## 6. Decisions requiring owner approval

1. **Approve M3 start and the section 2 naming.** Default: yes, named "Control-plane security and operator experience"; audit-roadmap M3 identity items stay out of scope.
2. **Audit sink for REST actions.** Default: a separate local append-only SQLite store in the data directory optionally reusing the existing unkeyed hash-chain code (keyed HMAC, G025, is a separate decision and is not assumed), not the central vault DB, so no coupling to `dso central`.
3. **Permission model.** Default: two credential classes (read-only, operator) on the existing token mechanism, optional and off by default; no named users, no roles table.
4. **Bearer-token attribution.** Default: keep the token fingerprint as actor; do not introduce per-person tokens in M3 (that would be a new identity system).
5. **OpenAPI and metrics inclusion.** Default: include both as the last, small slices; defer SDK/Terraform/webhook work (audit-roadmap M4) entirely.

## 7. Implementation tracker (updated at the end of M3 development)

Authorized scope: sections 5 items 1-2, 5-8 on the existing bearer token and single WebUI operator, per the owner's instruction to proceed with clearly authorized, non-conflicting work. Decisions D1-D4 were NOT taken: no second credential class, no durable audit sink, no per-person tokens.

| Slice | Status | Commit | Notes |
|---|---|---|---|
| 1 Route authorization matrix | Already present | n/a | `TestRouteTableCoverage` and anonymous-401 matrix in `internal/server/auth_gate_test.go`; new routes added to it |
| 2 Audit auth lifecycle | Done | ad90046 | `auth_login`, `auth_logout`, `sessions_revoke_others`; no attempted username, password or address recorded. Events go to the audit logger stream (not durable) |
| 3 Durable REST audit sink | NOT started | n/a | Needs owner decision D2 |
| 4 Read-only vs operate split | NOT started | n/a | Needs owner decision D1 |
| 5 Session hardening | Done (partial) | fe72643, next commit | Stable opaque session ids, `POST /api/sessions/revoke`. WebSocket close on revoke-by-id and absolute lifetime, and survival on unrelated revoke, now have tests (race-clean). Bearer-token rotation closing sockets is by design a process restart (token is read once from env); not separately tested |
| 6 Web UI operator workflows | Done within authorized scope | 8f831bc | Sessions page: per-session sign-out with two-step confirmation. Manual rotation already has a confirmation dialog and tests (`web/__tests__/secrets-rotate.test.tsx`); the notification test is a deliberately unconfirmed synthetic event. Permission-aware controls need slice 4 (D1) and a per-action identity display needs a person-level identity (D4), so neither is built |
| 7 OpenAPI + contract test | Done | b3d7338 | `docs/api/openapi.yaml`; minimal schemas |
| 8 REST metrics | Done | 4cdc32f + next commit | `docs/metrics.md`; `dso_rest_active_sessions` gauge computed at scrape time (non-expired sessions, no timer refresh) |

Still deferred: all production-qualification gates in `docs/milestones/deferred-production-qualification.md`. N19 remains implemented but not runtime-qualified. This milestone is not production-ready.
