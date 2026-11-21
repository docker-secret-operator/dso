# Draft decision record: central audit-failure policy (Gate 6)

Status: **Draft; proposed position; not ratified.** Written 2026-10-30. The owner confirmed on 2026-10-30 that "Retain both" (this position and the audit-chain / audit-failure counterpart) was the owner's own selection. It is an **owner-selected proposal and is not ratified**; the selection is recorded in working-session conversation, not in a repository decision record, and ratification requires the owner's explicit approval of a separate durable decision record (see `README.md`). This is a **new** draft record, not an approval, and does not reproduce the missing Step 5.3 decision resolution. Gate
numbers are taken from source comments and are unverified history.

Reading guide: see `README.md` ("Reading guide and option labels"). Statements marked *verified* describe current
code; *proposed* items are recommendations; *owner-selected* means chosen in a working-session discussion with no
repository record; *ratified* means formally approved. Nothing in this directory is ratified.

## Proposed position (scope: the current milestone only; not ratified)

Proposed: retain the **availability-first** policy: a failure to write an audit event for a read or a failure outcome never
changes the operation's result. Mutations that already run in a transaction record their success event atomically.

**Current CLI behavior: best-effort audit failures are not logged.** The service logs them at error level
(`appendAuditBestEffort`), but the CLI builds the service with a nil logger (`internal/cli/central.go:154` and `:332`),
and `centralsvc.New` replaces a nil logger with `zap.NewNop()` (`internal/centralsvc/service.go:51-55`). In the CLI a
failed best-effort audit write is therefore **silently discarded**. Only the `init` event uses a real logger
(`centralAuditLogger`, zap production), so only its persistence failure is reported. Service-level tests inject a
capturing logger (`observer.New`), so they do not exercise this CLI path, and no CLI test of that path was found in the test sources reviewed.

Current behavior:

- Atomic with the change (`Store.WithTx`): `CreateProject`, `CreateEnvironment`, `CreateSecret`, `AddVersion`,
  `DisableVersion`, `DeleteSecret`. If the audit insert fails, the mutation rolls back.
- Best-effort, **silently discarded in the CLI**: failure events and the secret-reveal event (`GetValue`).
- Best-effort, reported through a real logger: the `init` event (`recordInitEvent`).
- Appended after the fact: the `restore` event (after the replaced database is reopened); if it cannot be recorded,
  the restore is reported as usable with an unrecorded event.
- Not audited: `backup`, metadata listings, and `audit verify`.
- Gaps (no event is written): a failed restore, a dry-run restore, and a wrong-root-key attempt (every service
  operation calls `Bind` first and returns before any event; restore's key check fails before the event). The
  restore event is written only on the success path (`restore.go`).
- Failure paths that return **without** any event (verified in `internal/centralsvc/service.go`; details and line
  references in the per-operation draft, sections 1 and 4a): `CreateSecret` ID generation, AAD construction and
  sealing, which run before the transaction; `CreateProject` ID, key generation, key-context and wrapping errors;
  `CreateEnvironment` ID generation; the `GetValue` AAD-build error ("building secret context"); and every
  operation's `Bind` failure (wrong root key). `AddVersion` differs from `CreateSecret`: its AAD and sealing run
  inside the transaction, so those errors are followed by a best-effort failure event.
- Missing actor attribution is a **separate** gap from missing events: `project_create`, `environment_create`,
  `secret_get_reveal` and `init` events are written without an actor label even when they are recorded. This record
  proposes no schema or actor-field change.

## Alternatives considered

| Option | Gain | Cost |
|---|---|---|
| AUDITFAIL-A. Availability-first (**owner-selected proposal; not ratified**) | Operations are never blocked by audit trouble | Reads and failures can go unrecorded |
| AUDITFAIL-B. Fail closed for secret reveal only (corresponds to REVEAL-B in the per-operation draft) | A reveal always leaves a record | A reveal fails whenever the audit write fails; needs a separately specified and tested policy |
| AUDITFAIL-C. Fail closed for all operations | Strongest evidence | Worst availability; large behavior change |

## Security limitations

An attacker or fault that prevents audit writes can read secrets without a record. The log is therefore not a
complete record of reads.

Design tension: the code describes the reveal event as "the highest-value event in the whole design" because it is
the one place a secret value is disclosed (`GetValue`, `service.go`), yet under this policy it is best-effort and, in
the CLI, can be lost without any trace. Log-only reporting is also, by the code's own comment, "not durable or
tamper-proof evidence on its own".

Making failures **visible** (supplying a real logger) and making them **fail closed** are separate decisions; the
first changes observability only, the second changes availability.

## Deferred work (not authorized by this record)

Stricter reveal policy; audit events for `backup` (undecided: it changes event semantics and needs success and
failure behavior specified); auditing metadata listings.

## Code and tests

- `internal/centralsvc/service.go`: `appendAuditBestEffort`, and the `WithTx` call sites
- `internal/cli/central.go`: `recordInitEvent`
- `internal/centralsvc/restore.go`: `Restore` (restore event handling)
- Tests: `internal/centralsvc/audit_test.go` (`TestAudit_BestEffortWriteFailureIsLoggedAndDoesNotBlockTheRead`,
  `TestAudit_TransactionalWriteFailureRollsBackAndIsLogged`, `TestAudit_EachOperationCreatesExactlyOneEvent`),
  `internal/store/sqlite/audit_test.go` (`TestWithTx_AuditInsertFailureRollsBackBusinessMutation`),
  `internal/centralsvc/restore_test.go` (audit-append failure after activation)

## Related drafts

`2026-10-30-central-audit-failure-per-operation-DRAFT.md` lists every operation individually with options. It is not
approved.

## Superseded or related drafts

`docs/design/m2-identity-authorization-audit-design.md` §5.7 (audit-write failure semantics) differs and is not
authorized.

## Unresolved

Whether `backup` should produce audit events; whether any operation should fail closed; whether the CLI should
supply a real logger (observability only); how failed and dry-run restores and wrong-root-key attempts are treated.
