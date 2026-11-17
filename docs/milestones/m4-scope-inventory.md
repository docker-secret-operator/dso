# M4 scope inventory (operator diagnostics and usability)

Base: frozen M3 checkpoint (`docs/milestones/m3-verification-record.md`). Branch `feature/m4-development`, local only.

## 1. Naming
"M4" means different things in different documents. The product-evolution roadmap's M4 is the proxy lifecycle (PE-10, ADR-006, a separate `dso proxy` process): the highest-risk item, with approval items M4-1 and M4-2 still unticked and an S12 soak that needs real Docker, which is deferred. The audit roadmap's M4 is integrations and DX (OpenAPI, SDK, Terraform, webhooks); OpenAPI and metrics were pulled into M3. Neither is adopted wholesale. This inventory adopts **M4 = "Operator diagnostics and usability" on existing DSO functionality**, using only changes that are Docker-free testable and need no new trust model. PE-10 stays unstarted until ADR-006 is approved and Docker qualification is possible.

## 2. Existing implementation (reused)
Durable audit file and export (M3); `dso doctor`, `dso status`, `dso central audit verify` (exit codes 10/11); OpenAPI spec and drift test; metrics catalogue; Web UI pages for secrets, containers, audit, sessions, integrations.

## 3. Missing or partial
| Area | Gap |
|---|---|
| Audit usability | No CLI to verify the agent's durable audit file offline (only the central store had one). |
| Audit usability | Web UI audit page shows the in-memory view, with no indication of whether the durable file is active, and no export link. |
| Diagnostics | `dso doctor` does not report the durable audit file's state (present, chain valid, failure counter). |
| Diagnostics | Secret-delivery and recovery states are visible only through logs and `status`; a single read-only diagnostic summary is missing. |
| Docs | Operator docs lack a troubleshooting page for the audit file and its failure modes. |

## 4. Authorized work (first slices, in order)
1. `dso audit verify` for the durable file (read-only, JSON, exit codes shared with central verify). **Done.**
2. Doctor check for the durable audit file (status, record count, chain result), read-only and opt-out of network calls.
3. Web UI: durable-audit status banner and export link on the audit page, based on `/api/audit/export` availability.
4. Operator troubleshooting doc for audit and secret-delivery diagnostics.

## 5. Not authorized
RBAC, SSO or MFA, multi-tenancy, per-person identity, a read-only/operate credential split (D1), a remote vault API, central-store agent consumption, KMS, multi-host, keyed audit chain (G025), fail-closed audit, PE-10 proxy separation, SDK or Terraform work. Any feature needing a new authorization or identity model stops and is reported.

## 6. Dependencies and security boundaries
Read-only commands must not modify or create state, must not print secret values, credentials or paths in structured output, and must treat file content as untrusted for display. No new ports, daemons or keys.

## 7. Acceptance criteria
Each slice has deterministic Docker-free tests, regenerated CLI reference with no drift, vet and gosec clean, and a tracker entry. A full immutable Docker-free verification runs once at the end of M4.

## 8. Owner decisions required
None for slices 1 to 4. PE-10 (ADR-006) and any D1, D4 or G025 work need separate decisions.

## 9. Implementation tracker
| Slice | Status | Commit | Notes |
|---|---|---|---|
| 1 `dso audit verify` | Done | this commit | `internal/cli/audit_cmd.go`; exit 0/10/11; never modifies the file; refuses non-regular files; reason strings do not leak paths |
| 2 Doctor audit-file check | Done | next commit | `DSO-DOCTOR-032` (`internal/setup/doctor_audit.go`): agent mode only; INFO absent/unreadable, WARN for non-regular or group/other-accessible file, FAIL (high) on broken chain, PASS with record count; read-only; default path pinned equal to the agent's |
| 3 Web UI audit status | Done | next commit | `GET /api/audit/status` (cheap, does not verify the chain; in OpenAPI and the route matrix); audit page states whether the durable file is active, shows persistence failures and an export link, and says nothing is persisted when status is unknown. Embedded assets rebuilt and parity-checked |
| 4 Troubleshooting doc | Done | next commit | `docs/operations/audit-and-delivery-troubleshooting.md`, limited to existing, verified commands and checks |
| 5 `dso audit export` | Done | next commit | Offline verified NDJSON export (stdout or new 0600 file, never overwritten); verifies the whole chain first and writes nothing on exit 10/11; CLI reference regenerated; same record format as `GET /api/audit/export` |

Deferred release gates are unchanged (real-Docker N19, integration and crash harness, live systemd, hosted CI, signing; N7/N8, Docker advisories and release approval are owner items). M4 is not production-ready.
