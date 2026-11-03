# DRAFT: ARCH-B (key-holding local service) design input for dso central

Status: **Design-input draft for owner review. Nothing here is ratified or authorized for implementation. ARCH-B is owner-selected but not ratified.**
Written 2026-10-30 at repository HEAD `3645b89`.

Owner position (conversation, not a repository decision record): on 2026-10-30 the owner confirmed analysis and design of the key-holding
service (ARCH-B); on 2026-10-31 the owner selected ARCH-B as the architecture direction. That selection is **owner-selected but not
ratified**. This document does **not** ratify it, does **not** authorize implementation, and changes no other decision status.
Studying or designing ARCH-B does not authorize implementing it. ARCH-A (direct CLI with an OS-user boundary), ARCH-B
(key-holding local service) and ARCH-C (defer the decision) keep the meanings given in
`2026-10-30-central-authz-architecture-threat-model-DRAFT.md`; POLICY-HYBRID is defined in
`2026-10-30-central-authn-authz.md`. No new option meanings are introduced.

**Implementation prohibitions that apply to this draft:** no service, socket, listener, authorization layer, schema
change, actor field, hardening code or CLI change is to be built from this document. It is analysis input only.

Labels: **[verified]** = read from the code or tests at `3645b89` (path and line or symbol given). **[inference]** =
reasoned from verified facts, not directly demonstrated. **[proposal]** = a design option for discussion.
**[open]** = an owner decision is needed. **[external]** = general operating-system behavior not demonstrated by this
repository. Unless a path is given, `central.go` means `internal/cli/central.go`, `service.go` means
`internal/centralsvc/service.go`, `restore.go` means `internal/centralsvc/restore.go`. Line numbers can drift; the
symbol is the durable reference.

Related records (all unratified): `2026-10-30-central-authz-architecture-threat-model-DRAFT.md` (bypass paths,
invariants I-1 to I-12), `2026-10-30-central-audit-failure-per-operation-DRAFT.md` (audit behavior),
`2026-10-30-central-step54-step55-register-DRAFT.md`, `2026-10-30-central-ratification-record-PROPOSED.md`, and the
verification plan `2026-10-30-central-security-verification-plan-DRAFT.md`.

## 1. Current trust boundary (what exists today)

**Process and file access [verified].**
- Every `dso central` command runs in the invoking user's own process and opens files by path. The database is opened
  by `sqlite.Open` (`central.go` `open` at :150, `init` at :327, `backup` at :665; `restore.go:287`
  for the active database during restore and `:339` for the reopen). `sqlite.Open` first runs `prepareFile`
  (`internal/store/sqlite/sqlite.go`, currently line 155), which creates a missing database file (mode 0600) and missing parent
  directories (0700), then runs `Store.migrate` (`sqlite.go:141`, `:194`).
- Migration side effects: a database whose schema is newer than the binary is refused; one already at the latest schema
  is not changed; one with an older non-zero schema first gets a verified pre-migration `.bak` copy
  (`backupBeforeMigration`, `sqlite.go:221`) and is then migrated; a version-0 database is migrated with no backup.
- The root key is loaded in the CLI process: `keyProvider` in `central.go` calls `crypto.NewFileKeyProvider`
  (`internal/crypto/keyprovider.go:182`) or `crypto.NewPassphraseKeyProvider` (`:259`). The `KeyProvider` interface is at
  `keyprovider.go:28` with `Wrap` (`:59`) and `Unwrap` (`:70`). The passphrase comes from a hidden prompt or a private
  `--passphrase-file`. The key file is installed mode 0400 (`keyprovider.go:121`).
- Encryption and decryption happen in the CLI process: `Service.unwrapDEK` (`service.go:272`) unwraps a project key;
  `crypto.Seal`/`crypto.Open` (`internal/crypto/envelope.go:66`, `:82`) seal and open each secret version; `crypto.Zero`
  (`:98`) clears buffers after use. `Service.Bind` (`internal/centralsvc/binding.go:48`) is called first by every
  service method and rejects a wrong root key before any read or write.
- Paths are caller-chosen: `--db`, `--key-provider`, `--key-file`, `--passphrase-file` (`central.go:62-65`); `backup
  --out` and `restore --from` are caller-supplied paths.
- `backup` opens the database and loads no key. `audit verify` loads no key, never opens the live database through
  SQLite, and verifies a private staged copy (`internal/store/sqlite/audit_verify.go`, `restore_validate.go`
  `StageCandidate`).

**Authorization and attribution [verified].** No authentication or authorization check exists in `central.go`,
`service.go` or the store. The `--reveal` flag is a usability guard. Actor labels are caller-supplied strings:
`CreateSecret`, `AddVersion`, `DisableVersion` and `DeleteSecret` take an `actor` parameter, `Restore` takes
`RestoreOptions.Actor`, and the CLI fills both from `actorName()` (`central.go:157`, `"local-cli:<user>"`).
`project_create`, `environment_create`, `init` and `secret_get_reveal` events carry no actor (`GetValue` has no actor
parameter, `service.go:450`).

**Existing socket and peer-credential code is agent-only [verified].** `internal/agent/peercred.go` (`peerAuthorized`,
`resolveAuditUser`), `peercred_linux.go` (`readPeerIdentity` using `GetsockoptUcred`/`SO_PEERCRED`) and
`peercred_other.go` (non-Linux fallback: file-permission gating only) are used by the agent daemon at
`internal/agent/server.go:482-496`. Nothing in `centralsvc`, `store` or the central CLI uses them. The agent's socket
setup (`server.go:370-425`) tightens the umask around `net.Listen` (`umask_linux.go`), then applies path-based
`os.Chown`/`os.Chmod`, and treats a socket that fails a 500 ms `Dial` as stale and removes it. It has no lock proving
single-instance ownership.

**Service units and platform [verified].** The only systemd unit template is the agent's
(`internal/bootstrap/systemd.go`, with `NoNewPrivileges`, `ProtectSystem=strict`, `PrivateDevices` and similar at
`:62-75` and `:294-335`); no `LimitCORE`, `LimitMEMLOCK` or `MemoryDenyWriteExecute` was found in
`internal/bootstrap`. There is no unit, socket or listener for central. `internal/securefs` supports unix-like systems
and fails closed elsewhere (`securefs_other.go`). The M1 document states Linux is the supported and tested platform.

**What the OS-user boundary means today [verified/inference].** The effective boundary is: other local users are
blocked by file modes (database 0600, key 0400, backups 0600) and a directory-chain check (`internal/securefs`).
**Any process running as the same user, and root, can read the key and database or run any command.** There is no
boundary between the operator, other same-user processes and the key. Backups hold ciphertext and wrapped keys, so a
thief of backups alone does not obtain plaintext. The root key is never stored in the database.

## 2. Proposed service boundary [proposal; ARCH-B is owner-selected but not ratified]

Responsibilities that **would move into** a key-holding service for the key boundary to be real:
- Loading and holding the root key (and the passphrase-derived key, if that provider is kept).
- All database access (open, migrate, read, write), so no other process needs read access to the database.
- All encryption, decryption and key unwrapping.
- Audit-event construction and appending, with the actor derived inside the service.
- Authorization decisions, evaluated before any decryption or mutation.
- Execution of backup, restore and audit verification, or a defined offline substitute (sections 6 and 8).

Responsibilities that **could remain** in the CLI (as a client):
- Argument parsing and output formatting; hidden-prompt and stdin input of secret values and the passphrase;
  interactive confirmation for restore; exit-code mapping (`exitCodeFor`, `internal/cli/exitcode.go`).
- A client must not open the database or key file, and must not accept `--db`/`--key-file` for service-backed
  operation. **[open]** whether any direct (non-service) mode is retained; see break-glass (section 8) and
  the local-mode relationship (section 12).

Key point carried from the architecture draft: **a socket alone does not create a boundary.** The boundary exists only
if the key and database are readable solely by the service's account (or a separate container/VM) and no command
outside the service opens them.

**[open] Passphrase provider under a service:** the passphrase would have to be supplied to the service at each
start (the M1 document says the passphrase provider has no unattended restart), or the service would use the file
provider. This is a design decision with an availability trade-off.

## 3. Caller identity

- **What the service could use [proposal]:** the uid, gid and pid reported by `SO_PEERCRED` for each connection,
  following the agent's pattern (`peercred_linux.go`). Authorize on numeric uid/gid, not on pid, and not on any
  client-supplied string. Record the numeric uid in audit events. Numeric-uid-only identity is **an option, not a
  decision**.
- **What `SO_PEERCRED` establishes [external]:** the credentials of the process that called `connect()` on the socket.
  Limits [external, to be confirmed against OS documentation before reliance]: processes sharing a UID are
  indistinguishable; a connected socket passed to or inherited by another process keeps the original credentials;
  container and user-namespace uid mapping can make distinct principals look equal or the reverse; a pid can be reused
  and is not a durable identity; a username can be renamed or reassigned and a uid reused after an account is deleted.
- **Repository evidence about group resolution [verified]:** the agent resolves a peer's supplementary groups by
  looking up the user in the local account database (`peercred.go`), not from the connecting process's own
  credentials; with CGO disabled it reads `/etc/passwd` and `/etc/group` directly and does not consult NSS sources
  (LDAP/SSSD), so such users are denied.
- **Same-UID processes:** a service cannot distinguish individual users or processes that share a UID.
  Distinguishing operators requires separate OS accounts. Even with correct use of peer credentials, same-UID callers
  stay one principal, root and a compromised service are unconstrained, and identity is only as good as the host's
  account management.
- **Today the identity is not verified [verified]:** `actorName()` is derived from the invoking user's name by the
  client itself.

### 3A. Proposed requirement, pending owner approval: client-side authentication of the service [proposal; not approved]

Status: raised during a read-only review of this draft. It is **not** an adopted requirement, is **not** part of
invariants I-1 to I-12, and is separate from the ARCH-B selection. Whether it is required, and the mechanism that would satisfy it, are
undecided; nothing is implemented.

- **Threat [inference]:** if a process other than the real service can listen on the socket path the client uses (it
  binds first, the real service is down or restarting, or the directory is writable by the wrong account), the client
  could send secret values, prompts or the passphrase to it and accept forged replies.
- **Proposed requirement [proposal]:** before sending a secret value, a passphrase or any other sensitive request, the
  client verifies that the peer is the expected service (an expected identity, for example the service account's numeric
  uid, not merely "something is listening") and that the socket path's directory chain has the expected ownership and
  permissions. If verification fails the client refuses to proceed, with no fallback to direct mode (consistent with
  proposed invariant I-7).
- **Assumptions and limits [open]:** checking directory ownership alone does **not** authenticate the peer; ownership
  describes the path, not the process that answers. A check of the connected peer's credentials, read by the client
  (the client-side counterpart of the `SO_PEERCRED` use described above), is OS-specific **[external]**: whether it exists
  and is sufficient on each supported platform is unverified, and Linux is the only platform with peer-credential code in
  this repository (the agent's server side). Such a check identifies an operating-system account only: a compromised
  service account, root, or another process under the service's UID would still pass. A check made before `connect()` does
  not protect the connection made afterwards (the path can be replaced in between), so what is verified is better tied to
  the connected socket; the final mechanism is undecided and is not selected here.
- **Proposed acceptance criterion 14 and register entry DR-19:** see sections 13 and 12; both are proposals pending
  approval. Verification-plan entry V-10.7 lists scenarios and is blocked on a service.

## 4. Authorization (unresolved)

No authorization model is selected. Open design questions **[open]**:
- Principal model: numeric uid only; uid-to-role table; group-based roles; other mechanisms (the unauthorized M2
  drafts in `docs/design/` describe operators, sessions and tokens and are **not** adopted).
- Roles versus explicit per-operation permissions, who assigns them, and how policy changes and service upgrades are
  applied atomically.
- Operation policy for: list, create project/environment, create secret, add version, reveal, disable version, delete
  secret, backup, restore, audit verify, initialization. The architecture draft (section 3, ARCH-B) lists an
  illustrative table of possible minimum requirements; it is a discussion proposal, not a decision.
- Enforcement points: authorization evaluated in the service before decryption, mutation or file access. A denied
  attempt is audited or not **[open]** (audit draft section 6, item 7).
- Output hygiene: error and log text must not carry plaintext, keys or unintended sensitive metadata. In the code
  reviewed, no `s.log` call in `service.go` includes a secret value, key or ciphertext, and the `New` doc comment states
  that intent; this covers the reviewed code paths only.

## 5. Secret operations (create, read, reveal, versioning, disable, delete)

Current behavior **[verified]** (`service.go`): create/add-version/disable/delete append their success event in the same
transaction as the change; reveal (`GetValue`, `:450`) appends a standalone best-effort event after decryption and
returns plaintext regardless of that append; lists and metadata reads write no event. A disabled latest version is not
readable and there is no fallback to an older version (M1 behavior).

Under a service **[proposal / open]**:
- The client would send a secret value to the service over the socket and receive plaintext back for a reveal, so
  plaintext transits socket buffers and the client's stdout; the plaintext exposure surface grows from one process to
  two plus the socket.
- Authorization precedes decryption (invariant I-4).
- Reveal audit behavior is **not** selected here: REVEAL-A (keep best-effort), REVEAL-B (persist the event before
  disclosure, fail closed) and REVEAL-C (configurable) are defined in the audit draft and remain an owner decision.
  Trade-offs documented there: REVEAL-B makes reveal a write, couples secret reads to database availability, and needs a
  correlation id for client retries; none of the options makes the unkeyed chain tamper-proof.
- Versioning, disable and delete semantics are unchanged by a service; destructive-operation authorization depends on
  the (undecided) principal model.

## 6. Backup and restore under a service

Current behavior **[verified]**: `backup` writes a `VACUUM INTO` snapshot (mode 0600, never overwrites, verified,
no key needed, no audit event, `internal/store/sqlite/backup.go`). `restore` is an offline operator action that takes no
cross-process lock, stages and validates a private copy, writes a verified safety snapshot beside the database,
activates, reopens and then appends the restore event to the restored chain; failure modes are the three
post-activation cases and the pre-activation cases listed in the audit draft section 4c.

Design questions **[open]**:
- **Access control:** backup reads all ciphertext and audit history; restore replaces the database. Who may do each is
  an operation-policy decision.
- **Caller-supplied paths:** a service that opens `restore --from` or writes `backup --out` with its own privileges is
  a confused deputy (file-existence and format oracle, creation of 0600 files wherever the service can write).
  Options: stream data through the socket; use a service-owned directory; keep restore offline and restricted to the
  service account. None is selected.
- **Validation and recovery:** staged validation (structure, schema, audit chain, root-key binding) already exists; how
  it runs inside the service, and how the pre-restore safety snapshot is stored and protected, are open. Snapshots hold
  the full database and history and accumulate (Step 5.4 decision 4 as recorded: no automatic pruning; its exact scope
  is unverified).
- **Audit events:** whether backup, failed/dry-run restore and wrong-key attempts are audited is open; the restore event
  is not atomic with replacement today. Restoring an older backup drops newer events from the active chain.
- **Service unavailable:** options include the CLI failing closed with no database access, or an offline restore/recovery
  procedure performed as the service account. Which is acceptable is open and ties to break-glass.
- **Migration on open [verified]:** under a service, migrations and any pre-migration `.bak` become the service's side
  effect at start-up or first use.

## 7. Audit behavior

Facts **[verified]**: unkeyed SHA-256 chain, `event_hash = SHA-256(prev_hash || canonical(fields))`
(`internal/store/sqlite/audit.go`); append-only triggers (`migrations/0003_audit_events.sql`); a standalone append reads
the previous tip and inserts inside one immediate transaction (`internal/store/sqlite/tx.go:189`; `_txlock=immediate`,
`sqlite.go:118`; default busy timeout 5 s, `sqlite.go:96-97`); an in-process concurrent-append test exists
(`internal/store/sqlite/audit_test.go:250`). Multi-process audit-append behavior has no dedicated test **[unverified]**.
`created_at` comes from the writer's clock.

Attribution: today the actor is a caller-supplied string and several event types carry none. Under a service, the
actor could be derived inside the service. Adding an actor or principal column to events is an event-schema change
(`supportedAuditEventSchemaVersion` and a migration) and is **not proposed or approved**.

Failure handling: best-effort versus fail-closed is **not** selected here. The current-milestone position (AUDITFAIL-A,
availability-first) is an owner-selected proposal that remains unratified. A real logger (visibility only) is a separate
decision from fail-closed behavior. Integrity limits that remain with any architecture: no detection of tail deletion or
rollback to an older valid copy, and anyone with database write access can recompute the chain. Keyed MACs and external
anchoring are future options only and are **not** proposed here.

## 8. Break-glass access

Options for discussion **[open; none selected]**:
1. No break-glass: if the service cannot run, no one can access secrets until it is repaired.
2. Offline direct mode usable only by the service account (or root), with the service stopped.
3. Escrowed root key (separate custody) usable by a recovery procedure.
Risks common to any option: a retained direct path is itself a bypass for anyone who can become the service account
(`sudo -u`, shared admin, root); use should be audited, but audit writing may not be possible in the failure being
recovered; the procedure must not re-introduce a general-purpose direct CLI. Recovery procedures, who may invoke
break-glass, how it is logged, and its testing are unresolved decisions. The M1 note stands: **losing the root key makes
all data unrecoverable.**

## 9. Process hardening

Facts **[verified]**: key and DEK buffers are zeroed after use (`crypto.Zero`), which narrows but does not eliminate
exposure. A bounded text search of the repository's `*.go` and `*.s` files, excluding tests and `vendor/`, found no use
of `mlock`/`mlockall`, `MADV_DONTDUMP`, `prctl`, dumpable controls, `RLIMIT_CORE`/`Setrlimit`, or `ptrace`; this does not
prove absence from other file types, vendored code or other platforms. The agent unit's sandbox directives are partial
precedent for the agent only and are not core-dump protection or key-memory isolation.

A long-lived service holds the root key and unwrapped DEKs far longer than the short-lived CLI. Questions **[open]**:
- Core dumps and dumpability: can the service disable dumps for itself; how do system `core_pattern` and the unit's
  settings interact?
- Swap and `mlock`: is memory locking usable (it needs a raised limit or privilege), and does Go's runtime copy or move
  buffers so a locked page is not the only copy?
- Same-UID access: a process with the service's UID can normally read `/proc/<pid>/mem` or `ptrace` it unless
  restrictions such as `kernel.yama.ptrace_scope` apply **[external]**.
- Filesystem permissions: service-private key file (0400) and database (0600), a private socket directory created before
  bind and verified, no path-based permission changes after bind, single-instance lock.
- Supported operating systems: Linux is the only tested platform; macOS/FreeBSD cross-compile but are untested;
  Windows fails closed. Peer-credential support exists only on Linux in the agent code.
- Supported filesystems: local POSIX only; network and layered filesystems are unverified for locking, rename
  atomicity, `fsync` semantics and ownership reporting, and are treated as unsupported until verified.

**No single mechanism is claimed to isolate the key.** Each covers one route and each can be defeated by root or a
compromised service.

## 10. Threat model summary

Assets: root key/passphrase, database (ciphertext, wrapped DEKs, audit chain, clear metadata), plaintext in memory and
on stdout, backups and safety snapshots, the audit log, the socket.

Adversaries: operator (intended); same-UID process; different local user; root; backup/disk-image thief; remote
attacker (no listener exists today); compromised service process (ARCH-B only).

| Attack path | ARCH-A today | ARCH-B if unmitigated |
|---|---|---|
| Process running as the **operator's** UID reads the key and database directly | Succeeds | Blocked by file modes **if** ownership and modes are moved to the service account and the CLI no longer opens the files (an assumption, not a current fact) |
| Process running as the **operator's** UID calls the service | Succeeds (it can run the CLI) | Allowed or denied by the (undecided) policy for that uid. It holds the operator's authority, so a compromised operator account keeps that authority through the service, and it cannot be told apart from the operator's legitimate processes |
| Process running as the **service's** UID | n/a | Reads the key, the database and the service's memory directly; equivalent to a compromised service |
| Direct-file commands (`backup`, `audit verify`, flag-selected paths) | Work as designed | Bypass or break unless redesigned |
| Caller-supplied `restore --from` / `backup --out` | Run with the caller's privileges | Confused deputy (section 6) |
| Break-glass / direct mode | n/a | Bypass for anyone who can become the service account |
| Root | Succeeds | Succeeds (out of scope) |
| Compromised service | n/a | Full access to key and decrypted secrets |
| Rollback to an older valid backup / tail deletion | Not detected | Not detected unless the format changes (not proposed) |
| Database writer drops triggers and recomputes the chain | Possible | Possible for anyone with write access; limited by making the database service-private |
| Socket impersonation, stale-socket removal, symlink/path races | n/a | Needs a private directory, verified creation and a lock (section 9). A spoofed service receiving secrets is a separate risk: see the proposed requirement in section 3A (pending approval) |

Mitigations are listed in section 13 as proposed acceptance criteria. **Residual risks that remain even with a service and
socket:** same-UID callers are one identity; root and a compromised service are unconstrained; the unkeyed chain; clear
metadata (names, slugs, timestamps, actor strings); plaintext in service memory and on the socket and stdout; path-based
races inherent to path APIs; local-clock timestamps. Out of scope: network access, HSM/KMS, multi-host operation,
HMAC/external anchoring, quarantine and its release path, session/token systems.

## 11. Alternatives (existing definitions only)

| Label | Meaning (from the architecture draft) | Protects against | Does not protect against | Cost |
|---|---|---|---|---|
| ARCH-A | Direct CLI/database/key access under an OS-user boundary (current) | Other local users; a backup thief without the key | Any same-user process; root | None; honest documentation |
| ARCH-B | Key-holding local service with an IPC boundary | Callers without service-account access, subject to policy; a real boundary only if the CLI no longer opens key or database | Root; same-UID callers (one identity); service compromise; memory extraction unless hardened | Service account, unit, socket lifecycle, ownership migration, redesign of backup/restore/verify, API change |
| ARCH-C | Defer the decision, keep the current implementation | As ARCH-A | As ARCH-A, and leaves reveal/delete/restore open to any same-user process | Lowest now |

POLICY-HYBRID (peer credentials plus per-operation policy) is a policy layer that is meaningful only with a key-holding
process; it is not a fourth architecture. **ARCH-B is owner-selected but not ratified**; ARCH-A and ARCH-C are retained as comparison context.

## 12. Decision register (all unresolved unless stated)

| ID | Decision needed | Status and evidence | Owner decision needed |
|---|---|---|---|
| DR-1 | Is the same-user threat in scope; ARCH-A, B or C? | ARCH-B owner-selected on 2026-10-31 (conversation, not a repository decision record); **not ratified**. | Yes (ratification only, through the owner's separate record process) |
| DR-2 | Service account model (dedicated UID, container, other) | Open | Yes (if ARCH-B) |
| DR-3 | Break-glass: none, offline service-account mode, escrow | Open; no design | Yes |
| DR-4 | Principal and role model; who administers it | Open | Yes |
| DR-5 | Operation-level policy (reveal, disable, delete, backup, restore, verify) | Open | Yes |
| DR-6 | Backup/restore/verify under a service; removal of caller-supplied paths | Open | Yes |
| DR-7 | Passphrase provider vs file provider under a service | Open | Yes |
| DR-8 | Process-hardening requirements; supported platforms and filesystems | Open; no hardening code found in the searched scope | Yes |
| DR-9 | Real CLI logger: whether, destination, format, level, allowed metadata | Open; CLI currently discards best-effort audit failures | Yes |
| DR-10 | Reveal audit failure: REVEAL-A, B or C | Open | Yes |
| DR-11 | Actor attribution and event-schema change | Open; not proposed | Yes |
| DR-12 | Audit of backup, failed/dry-run restore, wrong-key, denied attempts | Open | Yes |
| DR-13 | Rollback detection, external anchoring, keyed MAC | Open; future options only | Yes |
| DR-14 | Relationship to local mode and the planned shared secret-command grammar (`docs/design/central-server-design.md`) | Open; local-mode integration not audited | Yes |
| DR-15 | Six historical Step 5.4 decisions and the Step 5.5 lifecycle | Step 5.4: substantively approved in earlier discussion per the owner's clarification; formal ratification is pending, and exact scope and caveats are unconfirmed where the original wording is not recoverable (see the register). Step 5.5: unresolved | Yes (ratification and scope confirmation; Step 5.5 choices) |
| DR-16 | Quarantine and the B-1 release path | Open; not designed | Yes |
| DR-17 | Integration base `origin/feature/m1-webui-reconcile` | Proposed only; not confirmed | Yes |
| DR-18 | Ratification of any record | Requires the owner's explicit approval of the separate durable record | Yes |
| DR-19 | Client-side authentication of the service before sending sensitive material (proposed requirement, section 3A) and the verification mechanism | Proposed during review; not approved; mechanism undecided; not part of I-1 to I-12 | Yes |

## 13. Acceptance criteria (proposed; none approved)

Properties that would have to hold, with the test class, before any ARCH-B implementation is considered complete. They
extend invariants I-1 to I-12 in the architecture draft (section 8); none is approved. Test classes are U = automated unit
test, I = automated integration test, M = manual procedure, as defined in the verification plan (section 0). Section 11 of
the plan maps every criterion and invariant to its verification entries.

**Status today:** no criterion is satisfied by an implemented service, because none exists. Under ARCH-A some related
properties have partial existing evidence in test source (for example, no plaintext in service logs or CLI metadata
output, criterion 10; plan V-9.1); that evidence is source-inspected, not executed. The remaining criteria are
unverified requirements or blocked on implementation, and criterion 14 is a proposal awaiting approval.

1. Under ARCH-B no command outside the service opens the key or database, including `backup`, `audit verify`, `restore`
   and flag-selected paths (I-1). A non-service-UID test alone does **not** show protection against same-UID callers or
   root. (I, M)
2. No break-glass path usable by an ordinary caller; retained paths restricted to the service account or root and
   audited where possible (I-2). (I, M)
3. The principal is derived inside the service; a forged client actor is ignored (I-3). (U, I)
4. Authorization precedes decryption, mutation and file access (I-4); the operation-by-principal matrix is tested. (U)
5. The service opens or creates no caller-supplied path (I-5). (I)
6. Hardening is applied as decided and its residual risk stated; no claim of complete key isolation (I-6). (M)
7. Socket unavailable: the client fails closed with no direct fallback (I-7). (I)
8. Private socket directory, verified creation, no path-based permission change after bind, single-instance lock,
   symlink/replacement races tested (I-8). (I)
9. The reveal-audit policy the owner selects is enforced on success and failure paths and records the verified principal
   (I-9). (U, I)
10. No plaintext, key bytes or unintended metadata in output, logs or errors (I-10). (U, M)
11. Restore/backup are service-side or offline with explicit locking (I-11). (I)
12. Policy and upgrades are atomic and fail closed (I-12). (I)
13. Migration from direct access to the service has a documented ownership change and rollback, and the direct path stays
    until the migration is validated (no matching invariant in I-1 to I-12; plan V-10.6). (M)
14. **Proposed, pending owner approval (section 3A):** the client verifies the service's identity per the mechanism the
    owner approves before transmitting any secret value, passphrase or other sensitive request (no matching invariant;
    plan V-10.7). (I, M)

A passing set of criteria would **not** remove the residual risks in section 10.
