# DRAFT: per-operation audit-failure policy for dso central

Status: **Draft for owner review. Not approved. No runtime behavior, schema or audit format is changed by this
document.** Written and revised 2026-10-30. The original Step 5.3
decision resolution is unrecoverable, so this is a new analysis from the current code, not a reproduction. "Gate"
numbers come from source comments only and are not treated as approvals.

Option labels used here: REVEAL-A/B/C (reveal audit-failure behavior, decision 4 only). The architecture options ARCH-A/B/C are defined in the architecture draft. Reading guide: see `README.md` ("Reading guide and option labels").

Paths: Unless a path is given, `central.go` means `internal/cli/central.go`, `service.go` means `internal/centralsvc/service.go` and `restore.go` means `internal/centralsvc/restore.go`; line numbers are from the tree at `3645b89` and can drift, so the function or symbol is the durable reference.

All statements about behavior are **[current]** (verified by reading `internal/centralsvc/service.go`,
`restore.go`, `internal/store/sqlite/audit.go`, `tx.go`, `internal/cli/central.go`) unless marked **[option]**
(a proposal) or **[unverified]**. No test was run in preparing this document; test references come from reading test source.

## 0. Four separate decisions (keep them apart)

1. **Whether to supply a real logger.** Today the CLI builds the service with a nil logger (`central.go:154`,
   `:332`), which `New` turns into `zap.NewNop()` (`service.go:51-55`). Best-effort audit failures and all
   operational log lines are therefore discarded in the CLI. Only `init` uses a real logger.
2. **Where logs go and in what format and level.** `centralAuditLogger()` is zap production (JSON, Info level,
   stderr). Whether that is acceptable for CLI stderr, scripts that parse output, and any `--json` mode is open.
3. **Which metadata may be logged.** The service logs operation metadata at Info (section 3). Secret names and
   project/environment slugs, and for write operations the actor, would start appearing once a real logger exists.
   In the code reviewed, none of the ten `s.log` call sites in `internal/centralsvc/service.go` (lines 104, 109, 191, 250, 360, 419, 491, 495, 563, 605) includes a secret value, key or ciphertext, and the `New` doc comment states that intent; `TestAudit_RevealEventNeverContainsPlaintext` (`internal/centralsvc/audit_test.go:239`) checks the reveal *audit event* only. This covers the reviewed code paths. It does not guarantee that other or future components, or the text of underlying errors, can never log sensitive data.
4. **Whether an operation fails when its durable audit event cannot be written** (fail-closed). This changes
   availability and security semantics and needs its own tested policy.

The four decisions are independent: selecting an option in one does not select or imply an option in another (in particular, supplying a logger does not select or change any REVEAL option).

A logger is **not** durable or tamper-proof evidence: the code itself says so (`appendAuditBestEffort` comment). It
depends on the sink being available and retained. Supplying one answers decisions 1 to 3 only; it does not
answer 4.

## 1. Facts about attribution

- **Reveal events carry no actor.** `GetValue` has no actor parameter (`service.go:450`) and both its failure
  events and its success event are built without `ActorLabel` (`:458-465`, `:497-501`). The audit record can say
  that a reveal of a given project, environment, secret and version happened or failed; it cannot say who did it.
- **`project_create` and `environment_create` events carry no actor** either (`CreateProject`, `CreateEnvironment`
  take no actor parameter). `init` events carry none (`central.go:296`).
- **Events that do carry an actor** take it from a caller-supplied string: `secret_create`, `secret_add_version`,
  `secret_disable_version`, `secret_delete` (parameter `actor`), and `restore` (`RestoreOptions.Actor`). The CLI
  fills these from `actorName()` (`"local-cli:<username>"`), which is self-asserted and not verified.
- This document does not add an actor field and does not change the schema.
  The hashed field set includes `actor_label` (`audit.go` `canonicalizeAuditEvent`); a new field would be a new
  event schema version and migration (`supportedAuditEventSchemaVersion`), which is not proposed here.

## 2. Concurrent audit appends (what is proven and what is not)

**Proven in code:**
- A standalone `Store.AppendAuditEvent` wraps its read-previous-tip-then-insert in `WithTx`
  (`internal/store/sqlite/tx.go:189-202`), and a call inside `WithTx` runs on the surrounding transaction. The hash-chain tip is read by
  `SELECT event_hash FROM audit_events ORDER BY seq DESC LIMIT 1` and the new row inserted within that same
  transaction (`internal/store/sqlite/audit.go`, `AppendAuditEvent`).
- The connection string sets `_txlock=immediate` (`internal/store/sqlite/sqlite.go:118`), so write transactions begin with the write lock
  ("writers take the lock up front"). SQLite allows one writer at a time, so two concurrent appends are serialized.
- `busy_timeout` defaults to 5 seconds (`internal/store/sqlite/sqlite.go:96-97`; the comment on the `BusyTimeout` option is at `:39-41`); a writer that cannot get the lock in time receives an
  error.
- A test exists for the in-process case: `TestAppendAuditEvent_ConcurrentStandaloneAppendsStayLinked`
  (`internal/store/sqlite/audit_test.go:250`, 20 goroutines, one `Store`, expects a single valid chain). This document did not run it.

**Not established [unverified]:**
- Several **processes** appending **audit events** to one database: no dedicated multi-process audit-append test was found. A separate test, `TestMultiProcessWritersNoLostUpdate` (`internal/store/sqlite/sqlite_test.go:688`, helper `TestHelperProcessWriter` at `:661`), starts 4 helper processes that each perform 15 `AddVersion` calls on one database (30-second busy timeout) and checks that all 61 versions exist without gaps. It covers secret-version writes and exercises cross-process serialization of that write path; it does not append audit events, and it does not by itself prove or disprove that multi-process audit appends are safe. Whether audit appends behave the same rests on SQLite file locking and the driver honoring `_txlock=immediate`. This remains an open verification item.
- Behavior on filesystems where SQLite locking is unreliable (network or layered filesystems).
- What happens to a best-effort append when the busy timeout expires: it fails, and in the CLI the failure is
  discarded (decision 1).

No race is claimed to exist and none is claimed to be impossible; this stays an open verification item.

## 3. Logging that a real logger would enable (metadata exposure)

When a non-nil logger is supplied, the service emits these lines (`service.go`): `project created` (project slug,
root key id), `environment created`, `secret created` (project, environment, secret name, version, **actor**),
`secret version added` (same), `secret version disabled`, `secret deleted`, **`secret read`** (project,
environment, secret name, version; line 495), **`secret failed integrity verification`** (Warn; same names),
and error-level lines when an audit event cannot be persisted (event type, outcome and the underlying error).
Consequences to decide, not decisions:

- Reveal activity would appear in stderr as structured JSON with names, mixed with command output streams.
- Underlying errors (for example from SQLite) may contain filesystem paths **[unverified for every error]**.
- Names may themselves be sensitive; whether they are acceptable in logs, which level, and where the sink lives
  are owner questions (decisions 2 and 3 above).
- Scripts that read stderr or parse structured output may break if log lines appear.

## 4. Per-operation table

Legend. *Atomic*: the success event commits or rolls back with the change (`Store.WithTx`). *Best-effort event*:
`appendAuditBestEffort` (standalone, errors logged only). *No event*: the function returns with nothing recorded.
*Silent* = best-effort failure with the CLI's nil logger.

### 4a. Mutations (service.go)

| Operation | Success (atomic event) | Failed mutation, best-effort failure event | Failed with **no event** | Security consequence of a lost event | Options [option] | Depends on authorization | Owner decision |
|---|---|---|---|---|---|---|---|
| Create project (`:136`) | Yes; no actor on event | Slug validation; transaction failure | `Bind` failure; `newID`, `GenerateKey`, `DEKAAD`, `Wrap`, event-id generation | Unrecorded attempt; a failed creation is not visible | Keep; real logger; consider events for early failures | No | Only for failure-event handling |
| Create environment (`:203`) | Yes; no actor | Slug validation; project lookup; transaction failure | `Bind` failure; `newID`; event-id generation | Same | Same | No | Same |
| Create secret (`:299`) | Yes; actor present | Name/value validation; scope resolve; DEK unwrap; **transaction failure** | `Bind` failure; **`newID`, AAD construction (`crypto.SecretAAD`), `crypto.Seal`, event-id generation** (these run **outside** the transaction, `:322-341`) | Same | Same | No | Same |
| Add version (`:366`) | Yes; actor present | Value validation; lookup; DEK unwrap; **transaction failure, which includes AAD-build and seal errors** (these run inside the transaction callback, `:397-405`) | `Bind` failure; event-id generation | Same | Same | No | Same |
| Disable version (`:528`) | Yes; actor present | Lookup; transaction failure | `Bind` failure; event-id generation | Same | Same | No | Same |
| Delete secret, soft (`:569`) | Yes; actor present | Lookup; transaction failure | `Bind` failure; event-id generation | Same | Same | No | Same |

On a failed audit append inside the transaction, the change rolls back and the error is returned to the caller; the
failure event written afterwards is best-effort. A failure event that itself cannot be written is silent in the CLI.

`CreateSecret` and `AddVersion` differ: for `CreateSecret`, ID generation, AAD construction and sealing run before the transaction and return with no event; for `AddVersion`, AAD construction and sealing run inside the transaction callback, so those errors are followed by a best-effort failure event, and only event-ID generation returns with no event.

### 4b. Reveal and reads

| Operation | Current | Atomic | When audit persistence fails | Visible | Consequence | Options [option] | Authorization | Owner decision |
|---|---|---|---|---|---|---|---|---|
| **Reveal** (`GetValue`, `:450`) | Standalone best-effort event after decryption, success and failure (`:497`), **no actor**. Failure events for lookup, version lookup, DEK unwrap, integrity. **No event** for `Bind` failure and for the AAD-build error (`service.go:487`). | No | Plaintext is still returned; the event is lost | Silent in CLI | Disclosure possible with no durable record; and the record never identifies the actor | REVEAL-A keep best-effort (current behavior); REVEAL-B fail closed: persist before returning plaintext; REVEAL-C configurable. This row is decision 4 only. Supplying a real logger is decision 1 and does not select or change any REVEAL option; allowed metadata is decision 3 (section 3) | Reveal is the main reason authorization matters; REVEAL-B is stronger with an enforced principal | **Yes** |
| Read/list (`ListProjects`, `ListEnvironments`, `ListSecrets`, `ListVersions`) | No event | n/a | n/a | n/a | Enumeration is invisible | Keep unaudited; or audit denied attempts once an authorization layer exists | Denied attempts only | If denied attempts are wanted |

#### Reveal option notes (analysis, not a recommendation)

- **REVEAL-A, keep best-effort (with or without a real logger; the logger is decision 1 and does not change this option):** does not make the event precede or even accompany disclosure; log retention
  decides whether any evidence exists. Lowest availability impact.
- **REVEAL-B, persist before disclosure (fail closed):** make the append succeed before the plaintext is written. A crash after the
  commit and before delivery leaves an event with no disclosure (over-reporting). A client retry creates a second
  event, so a correlation id would be needed to avoid ambiguity. Reveal becomes a write, so a full disk, a locked
  or read-only database, or busy-timeout expiry would make secret reads fail. Authorization would have to precede
  decryption; the event records intent, and an "intent" event before decryption is an alternative to
  decrypt-then-append. The event must never contain plaintext (already tested for the current event).
- **REVEAL-C, configurable:** a setting editable by the caller creates a downgrade path; it would have to be owned by
  whoever controls the service.
- None of the options make the unkeyed chain tamper-proof.

### 4c. Initialization, backup, restore, verify

| Operation | Current | Atomic | When audit persistence fails | Visible | Consequence | Options [option] | Authorization | Owner decision |
|---|---|---|---|---|---|---|---|---|
| `init` | Event after the store exists (`recordInitEvent`, `central.go:296`), real logger; none if it fails before the database exists | No | Error-level log; result unchanged | Yes (real logger) | Init is not durably evidenced if the sink is lost | Keep | No | None proposed |
| Backup success | **No event; no `backup` event type exists** (`store.go:205-226`). The backup is a `VACUUM INTO` snapshot; it contains only events written before it. Opening the database for `backup` can first run migrations and, for an existing database with an older schema, write a pre-migration `.bak` (`internal/store/sqlite/sqlite.go:194-221`); a database already at the latest schema is not changed by the open | n/a | n/a | n/a | Exporting the whole database and history leaves no record | (a) none; (b) a future event type, which would be new code and needs the policy first | Principal would identify who exported | **Yes** |
| Backup failure | No event (partial file is removed, `backup.go:93-96`) | n/a | n/a | Error to caller | Same | Same | Same | **Yes** |
| Restore success | Event appended to the **restored** chain after the replaced database is reopened (`restore.go:366-375`); actor is the caller-supplied `Actor`; no candidate identity in the event | **No**: replacement and event are separate steps | If the append fails, restore reports "completed and the database is usable, but the restore audit event could not be recorded", with the safety-snapshot path, and the CLI exits non-zero (see cases 2 and 3 below) | Returned | The activated database may lack its restore event | Keep; consider recording candidate identity (would change the hashed field set; not proposed) | Yes | Yes |
| Restore failure **before** activation | No event; the active database is untouched | n/a | n/a | Error to caller | Failed or unauthorized attempts are invisible | (a) none; (b) failure event in the intact active database | Yes | **Yes** |
| Restore after activation, case 1: **reopening the replaced database fails** (`restore.go:346-351`) | No event. With a safety snapshot, the error states that the replacement was activated, that the active path now holds the candidate's content, that it is **not** automatically recovered, and names the snapshot for manual recovery. Without a snapshot (no prior active database) the error is a plain reopen failure. No rollback or automatic recovery exists in the code | n/a | n/a | Error to caller | The active path may hold a database that does not open until recovered manually | Define post-activation recovery; any automatic rollback would be new behavior | Yes | **Yes** |
| Restore after activation, case 2: **database usable, but event-ID generation or the audit append fails** (`restore.go:355-358`, `:366-375`) | No restore event is recorded. The error says the restore completed, the database is usable, and the event could not be recorded, and names the safety snapshot. The CLI exits non-zero. No retry or rollback | n/a | n/a | Error to caller | The activated database lacks its restore event | Define whether and how an unrecorded restore is repaired; not implemented | Yes | **Yes** |
| Restore after activation, case 3: **durability of the replacement unconfirmed** (`restore.go:321-327`, `:377-379`) | The rename already happened and the flow continues: reopen succeeds and the restore event is appended. Only then is an error returned saying the restore completed, the event was recorded, and durability could not be confirmed. If the append fails, the case 2 error is returned instead and the durability error is not reported | n/a | n/a | Error to caller | The restored database and its event may not be durable after a crash | Define handling of unconfirmed durability; not implemented | Yes | **Yes** |
| Restore dry run | No event; read-only validation | n/a | n/a | Output | Validation attempts are invisible | Keep none, or record | Yes | Yes |
| Wrong root key | **No event**: every `Service` call returns from `Bind` before any event; restore's key check fails during candidate validation (`restore.go:273`) before any change; `backup` and `audit verify` do not use a key at all | n/a | n/a | Error to caller | Guessing or mistaken-key attempts are invisible | (a) none; (b) failure event, accepting log growth by unauthenticated callers | Strongly | **Yes** |
| Restoring an older database | The restore event is appended **only to the restored chain**. Events newer than the backup exist only in the pre-restore safety snapshot, which the chain does not reference. The older chain verifies cleanly | n/a | n/a | n/a | Loss of newer history from the active chain is not detected by `audit verify`; no external anchor exists | Documentation; future anchoring or recorded candidate/tip identity would need an approved format change | Yes | **Yes** |
| `audit verify` | No event; read-only by design | n/a | n/a | Exit code | Writing to the chain under verification would defeat the check | Keep none | No | None proposed |
| Denied authorization attempts | Not implemented | n/a | n/a | n/a | n/a | Defer until an authorization layer exists | Yes | Deferred |
| Quarantine / release | Not implemented | n/a | n/a | n/a | n/a | Deferred until quarantine itself is approved | Yes | Deferred |

#### What the current design can and cannot guarantee for backup/restore

- **Can (current):** a restore never claims success before the reopen step; the safety snapshot is created and
  verified before the active database is touched; a candidate that fails validation leaves the active database
  unchanged; the restore event is appended to the restored chain when the database reopens.
- **Cannot (current):** replacement and restore-event recording are **not** atomic; the chain does not record which
  candidate was restored or what was discarded; newer history can be lost without a chain-level signal; backup
  leaves no record.
- **Needs an architectural or format change (not proposed or approved):** recording candidate identity, an external
  anchor of the tip, any actor field beyond the caller-supplied string, authenticated principals.

## 5. Cross-cutting points

- **What "visible" can and cannot mean.** Log-only reporting is not durable evidence (section 0).
- **Event schema.** New event types do not change the hashed field set, but any new column (an actor on more events,
  a verified principal, project/environment ids) requires a new `supportedAuditEventSchemaVersion` and migration,
  as the `0003` migration comment already states. Not proposed here.
- **Restore and history.** See the "older database" row.

## 6. Owner decisions required (all unresolved)

1. Supply a real logger in the CLI? (Decision 1.)
2. Destination, format and level, and compatibility with scripts and any JSON output. (Decision 2.)
3. Which metadata may appear in logs: slugs, secret names, actor, key id. (Decision 3.)
4. Reveal: best-effort, fail closed, or configurable; and who owns the setting. (Decision 4.)
5. Should reveal, project/environment create and init events gain actor attribution (a schema decision)?
6. Backup success/failure, restore failure (before and after activation), dry run, and wrong-key attempts:
   audited or not?
7. Are denied attempts audited once an authorization layer exists?
8. Does any operation other than reveal need a stricter policy, including the early-failure paths with no event?
9. Verification item: multi-process audit-append behavior and the 5-second busy timeout (section 2).

## 7. Code references

`internal/centralsvc/service.go` (`New`, `appendAuditBestEffort`, each operation), `internal/cli/central.go`
(`open`, `recordInitEvent`, `centralAuditLogger`, backup and restore commands), `internal/centralsvc/restore.go`
(`Restore`), `internal/store/sqlite/audit.go`, `tx.go`, `sqlite.go`, `backup.go`. Tests:
`internal/centralsvc/audit_test.go` (capturing logger), `internal/store/sqlite/audit_test.go` (including the
concurrent-append test), `internal/centralsvc/restore_test.go`.
