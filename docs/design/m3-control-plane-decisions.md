# M3 control-plane decisions and limitations

Scope of M3: control-plane security and operator experience on the existing agent REST API and Web UI, under the single-operator model. M3 is evaluated against that local scope only. It does not claim the original audit-roadmap identity and authorization requirements.

## Decisions

| Id | Decision |
|---|---|
| D1 | No read-only/operate credential classes in M3. A permission split is a new authorization trust model and needs its own ADR: permission boundaries, credential issuance/storage/rotation/revocation, REST and WebSocket authorization, session behaviour, audit attribution, CLI compatibility, backward compatibility, default-deny, and permission-aware UI. No permission-aware UI controls are shown, because no authorization system exists to back them. |
| D4 | No per-person tokens, accounts, organizations, RBAC, SSO, MFA or identity providers. The single-operator model stays authoritative. |
| D2 | The durable audit trail is a dedicated append-only JSON-lines file, kept separate from the encrypted central store. Accepted with the limitations below. |
| G025 | A keyed audit chain is deferred; it needs a key-custody and recovery design. |

## Identity vocabulary

- **Authenticated credential:** the bearer token or WebUI session that passed the gate.
- **Session identifier:** an opaque, non-reversible id derived from the session token (`sess-...`); safe to show, grants nothing.
- **Credential fingerprint:** a short digest recorded as the audit "user" for bearer callers. It identifies a credential, not a person.
- **Verified human identity:** NOT provided by DSO. Audit records must never be read as proof of who a person was.

## Durable audit file: guarantees and limits

What it does: every `audit.Log` event is appended to `audit.jsonl` (mode 0600, one record per line, fsync per record, SHA-256 hash chain, strictly increasing sequence). Open verifies the chain and refuses a broken one; a half-written final line is discarded. Export (`GET /api/audit/export`) verifies the whole chain first and answers 500 without content on failure, 503 when the file is not enabled.

What it does not do:
- It is unkeyed. It does not authenticate records against anyone who can write the file and recompute the chain.
- It does not detect truncation of the tail to a valid prefix, or replacement by an older valid copy.
- It does not detect events that happened while persistence was unavailable.
- It is best effort. It is not fail-closed and gives no guaranteed persistence. Operations are not blocked when persistence fails.
- The log stream and the in-memory `/api/audit` view are separate, non-durable outputs; an event seen there is not thereby persisted.

Observed failure behaviour (covered by tests in `internal/audit/durable_test.go`):

| Situation | Behaviour |
|---|---|
| File cannot be opened at startup | Agent logs an error and runs without the file; `/api/audit/export` answers 503. |
| Write error, ENOSPC, short write or fsync failure | The record is not acknowledged; the file is truncated back to the last acknowledged offset so later records stay verifiable; the event is still logged; `dso_audit_durable_write_failures_total` is incremented. |
| Rollback itself fails | The sink disables itself (all later appends fail and are counted) rather than corrupt the chain; a restart discards the torn tail. |
| Broken chain at startup | Open is refused; the agent runs without the file. |
| Broken chain at export | 500, no content. |
| Concurrent appends | Serialized by one mutex; a single verifiable chain. |
| Shutdown during an append | Close takes the same mutex, so it never interleaves with a half-done append. |

A future milestone may decide that persistence must succeed before a high-risk operation is authorized. That is a separate ADR and is not part of M3.

## Other recorded limits

- The bearer token is read from `DSO_AUTH_TOKEN` at process start; replacing it requires a restart. Live token rotation is not implemented, and WebSocket behaviour under live rotation is not tested.
- `pkg/vault` `TestConcurrentWriters_DistinctKeys_AllPreserved` is load-sensitive: it passes on a quiet machine and has timed out when verification runs overlapped. It is a known limitation, not investigated further.
