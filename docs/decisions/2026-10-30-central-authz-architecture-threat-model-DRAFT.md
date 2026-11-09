# DRAFT: authorization architecture and threat model for dso central

Status: **Design draft for owner review. Not approved. No architecture is selected. Every invariant, option and
policy below is a proposal.** Written 2026-10-30; revised after a 2026-10-30 security design review. The review's findings are incorporated by topic in sections 1 and 4 to 7 and are not separately numbered here. The original Step 5.2 authentication/authorization design is unrecoverable and is
**not** reproduced here. The M2 drafts in `docs/design/` (operators, sessions, tokens, `Principal`/`Handle`) are
unreviewed and unauthorized ("M2.2 onward not authorized") and are **not** adopted; they are cited only as
reference material. The key-holding service (ARCH-B) is the **design direction the owner confirmed on 2026-10-30 for analysis and design only** (recorded in working-session conversation, not in a repository decision record). Architecture selection and implementation remain pending; it is not an approved architecture, and studying or designing it does not authorize implementing it. ARCH-A and ARCH-C are kept in this draft as comparison context.

Option labels are stable across documents: ARCH-A (direct CLI / OS-user boundary), ARCH-B (key-holding local service), ARCH-C (defer the decision); POLICY-HYBRID (peer-credential policy layer) is a separate proposal in `2026-10-30-central-authn-authz.md`. Reading guide: see `README.md` ("Reading guide and option labels").

Unless a path is given, `central.go` means `internal/cli/central.go`, `service.go` means `internal/centralsvc/service.go` and `restore.go` means `internal/centralsvc/restore.go`; line numbers are from the tree at `3645b89` and can drift, so the function or symbol is the durable reference.

Labels used below: **[current]** = verified in the present code. **[service-only]** = a risk that exists only if a
service were built. **[unverified]** = not established by this review.

## 1. What exists today (verified in code)

- `dso central` is a local CLI. It opens the SQLite database and reads the root key itself, as the invoking OS
  user. File permissions are the only control: the database and backups are created 0600 (`internal/store/sqlite/sqlite.go` `prepareFile`; `internal/store/sqlite/backup.go`), the root key file is installed 0400 (`internal/crypto/keyprovider.go:121`), and the directory chain is checked by `internal/securefs`.
- **The caller chooses the paths.** `--db`, `--key-file`, `--passphrase-file` are command-line flags
  (`internal/cli/central.go:62-65`: `--db` at :62, `--key-provider` at :63, `--key-file` at :64, `--passphrase-file` at :65); `backup --out` and `restore --from` are caller-supplied paths.
- **`backup` and `audit verify` open files directly.** `backup` opens the database and never loads a key
  (`central.go` backup command; `sqlite/backup.go`). `audit verify` copies the database to a private staging file and does not open the live database through SQLite.
- **Opening the database can modify it.** `backup` (`central.go:665`), the other commands (`central.go:150`) and `restore` (active database, `restore.go:287`; reopen, `:339`) open it with `sqlite.Open`, which runs `Store.migrate` (`internal/store/sqlite/sqlite.go:141`, `:194`). Verified conditions: if the stored schema version is newer than the binary supports, the open is refused (`ErrSchemaTooNew`); if it equals the latest, nothing is changed; if it is older and non-zero, a verified pre-migration `.bak` copy is written first (`:221`, `backupBeforeMigration`) and then the migrations are applied; a version-0 database is migrated without a pre-migration backup. So `backup` and `restore` are not strictly read-only in that case. This is current direct-CLI behavior; the service-only implications are in section 3.
- No listener, no authentication, no authorization. `secret get --reveal` is a usability guard.
- **Actor labels are caller-supplied.** The mutating service methods for secrets (`CreateSecret`, `AddVersion`,
  `DisableVersion`, `DeleteSecret`) take an `actor` string parameter (`service.go:299,366,528,569`); `Restore` takes
  `RestoreOptions.Actor` (`restore.go`). The CLI fills both from `actorName()` (`"local-cli:<username>"`,
  `central.go:157`). **Not every event carries an actor:** `project_create`, `environment_create`, `init` and
  **`secret_get_reveal`** events are written without an `ActorLabel` (`CreateProject`, `CreateEnvironment` and `GetValue` in `service.go`; `recordInitEvent`, `central.go:296`).
  `GetValue` has no actor parameter (`service.go:450`). The current record cannot say who revealed a secret.
- Peer credentials (`SO_PEERCRED`) are used only by the agent daemon (`internal/agent/peercred.go`,
  `peerAuthorized`: root, the daemon's own UID, or the `dso` group; deny by default). Nothing in `centralsvc` or the
  central CLI uses it.
- **No memory-hardening code was found in the searched scope.** A case-insensitive text search of the repository's own `*.go` and `*.s` files, excluding `_test.go` files and `vendor/`, found no use of `mlock`/`mlockall`, `MADV_DONTDUMP`, `prctl`, dumpable controls (`PR_SET_DUMPABLE`), `RLIMIT_CORE`/`Setrlimit`, or `ptrace`. This bounds the statement to that scope; it does not prove absence from other file types, vendored or generated code, or other platforms. The agent's systemd unit template sets sandbox directives such as `NoNewPrivileges`, `ProtectSystem=strict` and `PrivateDevices` (`internal/bootstrap/systemd.go:62-75`, `:294-335`), and no `LimitCORE`, `LimitMEMLOCK` or `MemoryDenyWriteExecute` was found in `internal/bootstrap`. That is partial hardening precedent for the agent only; it is not core-dump protection or key-memory isolation, and central has no unit. Key and DEK buffers are zeroed with `crypto.Zero` after use, which narrows the window but is not isolation.
- Help text: the `central` group help states there is no authentication or authorization (`central.go:53` `Short`, `:58` `Long`); the `restore` long help says it has "no caller-specific authorization today" (`:737`; a code comment at `:716` repeats it); the `audit verify` long help says only that the command "is not authentication or authorization" (`:888-889`), not the restore wording; the `backup` help carries no such statement.
- `securefs` supports unix-like systems only and fails closed elsewhere (`securefs_other.go`).

## 2. Assets, actors and trust boundaries

**Assets:** the root key file (or passphrase), the database (ciphertext, wrapped project DEKs, audit chain,
metadata), plaintext secrets in process memory and on stdout, backups and safety snapshots, the audit log.

**Actors:** (1) the intended operator; (2) another process running as the same OS user; (3) a different local user;
(4) root or a process with equivalent power; (5) a thief of backups or disk images; (6) a remote attacker (no
listener exists today); (7) a compromised service process (only under the service options).

**Boundaries that exist today:** OS user ↔ other users (file modes, directory chain). **No boundary** exists between
the operator, any same-user process, and the key.

## 3. Alternatives

### ARCH-A: direct CLI/database/key access with an explicit OS-user trust boundary (current)

- **Principals:** none; the OS user is the only identity. **Policy:** none; every command may do everything.
- **Protects against:** other local users, a backup thief without the key.
- **Does not protect against:** any process running as the same user, root, a compromised operator session.
- **Cost / change:** none; needs only honest documentation (largely present in help).

### ARCH-B: key-holding local service with an IPC boundary (design direction, not approved)

- **Process and key ownership.** A long-running service owns the root key and the database. The boundary is real
  only if the key file and database are readable **only** by the service's account (a dedicated UID, or a separate
  container/VM) **and** no command outside the service opens them. A socket alone does not create a boundary: a CLI
  still running as the service's UID can read the key and bypass it.
- **IPC and identity.** A Unix domain socket in a directory owned by the service. Identity from `SO_PEERCRED`
  (section 6). Details of socket creation are in section 7.
- **Authorization policy (proposal for discussion, not a decision).**

  | Operation | Possible minimum requirement |
  |---|---|
  | List projects/environments/secret metadata | reader |
  | Create project/environment, create secret, add version | writer |
  | Reveal a secret value | reader with explicit reveal permission, audited (see the audit draft) |
  | Disable version, delete secret | administrator |
  | Backup (reads all ciphertext and audit history) | administrator |
  | Restore (replaces the database) | administrator |
  | Audit-chain verification | auditor (read-only) |

  Roles, who assigns them, and how uids map to roles are open.
- **Backup, restore and verify under a service.** These are direct-file commands today (section 4, paths 2 and 5).
  They would have to be redesigned (served through the socket, or kept offline and restricted to the service
  account); neither design exists. Migration on open (section 1) would become a service-side effect, including any pre-migration `.bak`.
- **Failure behavior.** If the socket is unavailable the CLI should fail closed. Whether a break-glass direct mode
  exists is an owner decision, and it is itself a bypass (section 4, path 4).
- **Migration.** Moving key and database ownership, a documented rollback, and the relationship to local mode and
  the planned shared `dso secret` grammar (`docs/design/central-server-design.md`) are undetermined.

### ARCH-C: defer the architecture and keep the current implementation temporarily

Keep ARCH-A's behavior and wording; add no enforcement. Reduces change risk now, but leaves secret reveal, delete
and restore open to any same-user process and defers Step 5.5's destructive-operation controls. One possible sequencing, offered as a recommendation and **not a decision**: ARCH-C now, with a design-only phase for ARCH-B.

## 4. Bypass paths and risks

"Current" rows describe behavior in the repository today. "Service-only" rows arise only if ARCH-B were built.

| # | Path | Kind | ARCH-A (today) | ARCH-B, if unmitigated |
|---|---|---|---|---|
| 1 | Direct read of key file and database outside the service | current | Succeeds for the same user | Blocked only if ownership/modes exclude callers; succeeds as the service UID or root |
| 2 | `backup` and `audit verify` keep opening files directly | current commands; **bypass [service-only]** | Work as designed (`backup` needs no key; it reads the database, and opening it can first migrate it; section 1) | Either still bypass the service, or stop working. Not designed yet |
| 3 | Caller-controlled `--db`, `--key-file`, `--passphrase-file` | current | Intended: any readable database and key can be used | A client that accepts these stays outside the service |
| 4 | Break-glass or direct mode | **[service-only]** | n/a | Anyone able to become the service UID (`sudo -u`, shared admin, root) bypasses policy by running the direct CLI Must be named, restricted and audited if it exists |
| 5 | Service opening caller-supplied `restore --from` / `backup --out` with its own privileges | **[service-only]** | Runs with the caller's own privileges (no escalation) | Confused deputy: file-existence and format oracle on arbitrary readable paths; creation of 0600 files wherever the service can write; candidate sidecars (`-wal`, `-shm`) copied too |
| 6 | Restoring an older valid backup | current | Accepted if the candidate validates under the same root key. The restore event is appended **only to the restored chain**; events newer than the backup survive only in the safety snapshot, which the chain does not reference (`restore.go`) | Same unless anchoring or a recorded candidate identity is added (a hashed-field change; not proposed here) |
| 7 | Database write access vs append-only triggers and the unkeyed chain | current | Triggers (`0003_audit_events.sql`) abort UPDATE/DELETE, but anyone with write access can drop them and recompute a valid chain; tail deletion and rollback are not detected | Unchanged unless the database is service-private |
| 8 | Safety snapshots, backups, temp and staging files | current | Snapshots (`<db>.pre-restore-*.bak`) and backups hold the full database and history, mode 0600, with no automatic pruning; staging copies are private temp files; SIGKILL can leak them (documented) | Same files readable by the service UID; retention and location need a policy |
| 9 | Paths in process arguments | current | `--db`/`--key-file`/`--passphrase-file` are visible in `/proc/<pid>/cmdline` and `ps` to same-user processes (and to others per system settings); secret values are not in arguments | Same for any client invocation |
| 10 | Other local user | current | Blocked by file modes and the directory chain | Blocked by file modes and socket permissions |
| 11 | Root | current | Succeeds | Succeeds (out of scope) |
| 12 | Backup thief | current | Needs the root key | Needs the root key |
| 13 | Compromised service process | **[service-only]** | n/a | Full access: the new high-value target (section 5) |

## 5. Key and service-process hardening (questions, not guarantees)

A long-lived service would hold the root key and unwrapped project DEKs in memory far longer than the short-lived
CLI does today. No hardening code was found in the searched scope (section 1). Open questions for the owner and for a
later design:

- **Core dumps and dumpability.** Can the service disable dumps for itself (for example by clearing the dumpable
  flag and zeroing the core limit), and how do system-wide `core_pattern` and `systemd` settings interact?
- **Swap and memory locking.** Is `mlock` usable (it needs a raised `RLIMIT_MEMLOCK` or privilege), and does Go's
  runtime copy or move buffers so that a locked page is not the only copy?
- **Same-UID access.** A process with the service's UID can normally read `/proc/<pid>/mem` or attach with `ptrace`
  unless `kernel.yama.ptrace_scope`, dumpability and similar controls restrict it. What is the supported baseline?
- **Privileged access.** Root can read process memory regardless. Out of scope unless the owner says otherwise.
- **Service compromise.** An attacker who controls the service has the key and every decrypted secret it handles.
- **Portability.** `securefs` and the agent's peer-credential code are unix/Linux-oriented; the macOS and other
  behavior of each mechanism is **[unverified]**.

**No single mechanism is claimed to provide complete key isolation.** `mlock`, `RLIMIT_CORE`, dumpable controls
and ptrace restrictions each cover one route and each can be defeated by root or a compromised service.

## 6. Principal identity and attribution

- **Today [current]:** the actor is a caller-supplied string (section 1). Reveal, project-create,
  environment-create and init events carry none.
- **If a service were built [service-only]:** the actor must be derived inside the service from a trusted source;
  the `actor` parameters on the service API could not be accepted from the client. Removing them is an API change
  and is not proposed as approved.
- **Repository evidence:** the agent reads `SO_PEERCRED` through `GetsockoptUcred` (`internal/agent/peercred_linux.go`) into uid, gid and pid and authorizes on uid and gid (`peerAuthorized`, `peercred.go`).
- **`SO_PEERCRED` establishes** (external operating-system semantics; not demonstrated by this repository and given here without a citation) the uid, gid and pid of the process that called `connect()` on the socket. Its limits below are likewise **external OS semantics / threat-model assumptions** unless marked as repository evidence; verify them against the operating-system documentation before relying on them:
  - **Shared UID** [external OS semantics]. Processes that share a UID are indistinguishable; a claim that uid and gid are trustworthy identifiers holds only for distinguishing different UIDs.
  - **Passed file descriptors and inheritance** [external OS semantics]. A connected socket passed to or inherited by another process
    keeps the credentials captured at connection time.
  - **Containers and user namespaces** [external OS semantics]. The reported uid is relative to the receiving namespace; uid mapping can
    make different principals appear equal or the reverse.
  - **PID** [external OS semantics]. Advisory only; pids are reused and are not a durable identity.
  - **Names** [external OS semantics / account-management assumption]. Usernames can be renamed or reassigned; a uid can be reused after an account is deleted.
  - **Group membership** [repository evidence: `peercred.go`]. The agent resolves a peer's supplementary groups by looking up the user in the local
    account database (`peercred.go`), not from the connecting process's own credentials; with CGO disabled it does
    not consult NSS sources such as LDAP/SSSD and denies users it cannot resolve.
- **Numeric-uid-only identity** (record the numeric uid, do not authorize on groups) is an **option**, not a
  decision. Others: group-based roles, an explicit uid-to-role table, or separate principals via other mechanisms.
- **Risks that remain even when peer credentials are used correctly:** same-UID callers are one identity; root and
  a compromised service are unconstrained; the identity is only as good as the host's account management.

## 7. Socket lifecycle and filesystem safety

Not implemented for central. The agent's socket code is a possible reference, with these verified properties
(`internal/agent/server.go:370-425`) and cautions:

- The agent tightens the umask to 0077 around `net.Listen` (`tightenUmask`, linux), then sets mode and group with
  **path-based** `os.Chown`/`os.Chmod` after listening. Path-based calls follow symlinks and are exposed to
  path replacement if an untrusted party can write to the socket's directory.
- Stale-socket handling is: `Stat`, then a 500 ms `Dial`, and on failure `os.Remove` of the path. A busy but live
  socket that does not answer in time can be removed; the agent code has no lock proving single-instance ownership.
- Central socket lifecycle and single-instance locking: the central service and its socket are not implemented, so no central socket lifecycle or single-instance lock exists yet. (Separately, `internal/rotation/lock_manager.go` uses `flock`, a precedent in another package that was not reviewed for this purpose.)
- Questions to settle in a design: a private socket directory created before bind and verified (`securefs` helpers
  exist for the database/key directory chain); no path-based permission changes after bind; a lock file for
  single-instance; behavior when the socket path is a symlink or was replaced.
- **Filesystem assumptions [unverified]:** the existing code assumes a local POSIX filesystem with working
  `rename` and `fsync` (restore replaces the database by copy, fsync, rename and directory sync, and reports an
  `unconfirmedDurabilityError` when that cannot be confirmed). NFS, overlay and other network or layered
  filesystems have not been verified for locking, rename atomicity, `fsync` semantics or ownership reporting, and
  should be treated as unsupported until verified. Hard-link behavior is also unreviewed.

## 8. Proposed invariants (all **proposed, not approved**)

Proposed classification: **RB(ARCH-B)** = proposed as a release blocker if ARCH-B is ever implemented. **Rec** = recommendation. These classifications are proposals, not ratified.
None of these blocks anything under ARCH-A or ARCH-C because none is implemented or enforced today.

**Aggregate acceptance rule (proposed):** no invariant is considered satisfied while any bypass in section 4 that
the invariant covers remains open. A passing test of one invariant never substitutes for closing the paths listed
in section 4.

| # | Proposed invariant | Precise test and failure | Unsupported guarantee to avoid | Proposed classification |
|---|---|---|---|---|
| I-1 | Under ARCH-B, key file and database are not readable by ordinary callers, and **no command opens either outside the service** (including `backup`, `audit verify`, `restore`, and flag-selected paths). | Run every `dso central` subcommand as a non-service UID and trace file opens; fail if the key or database is opened, or any command succeeds without the socket. | A non-service-UID test shows only that **different-UID** callers are blocked. It says nothing about same-UID callers of the service, root, or the service UID. Must not be reported as "secure against same-UID callers or root". | RB(ARCH-B) |
| I-2 | No break-glass direct path exists that an ordinary caller can use; any retained path is restricted to the service account or root and is audited. | Attempt the direct path as an ordinary user; assert refusal. Review retained paths against section 4 path 4. | Cannot prevent use by the service UID or root. | RB(ARCH-B) |
| I-3 | The service derives the principal inside the service; the client cannot supply an actor. | Send a request with a forged actor; assert the recorded identity is the kernel-reported uid. | Distinguishes UIDs only; same-UID callers stay one principal (section 6). | RB(ARCH-B) |
| I-4 | Authorization is evaluated before any decryption, mutation, or file access. | Operation × principal matrix with a fake decrypter and store; assert they are never called on deny. | Does not cover decisions made by a compromised service. | RB(ARCH-B) |
| I-5 | The service opens or creates **no caller-supplied path**; backup and restore data move through the socket or a service-owned location. | Pass foreign, symlink and sidecar-bearing paths; assert none is read or created. | Does not cover the service account's own direct use. | RB(ARCH-B) |
| I-6 | Hardening of the service process is applied and documented per section 5, with stated residual risk. | Inspect dumpability, core limit, memory limit and ptrace scope on the running service; attempt a same-UID read of process memory on the supported baseline. | **No guarantee of complete key isolation.** Passing means the listed routes are narrowed, not closed; root and service compromise remain. | RB(ARCH-B) for the stated list; Rec for each individual mechanism |
| I-7 | If the socket is unavailable the CLI fails closed with no fallback to direct mode. | Stop the service; run the CLI; assert non-zero exit and that no database or key file is opened. | None. | RB(ARCH-B) |
| I-8 | The socket and its directory are created private and verified; no path-based permission change after bind; single-instance is enforced; symlink and replacement races are handled. | Race tests on the directory and path; start two instances; replace the path with a symlink. | Platform-dependent; non-Linux behavior unverified. | RB(ARCH-B) |
| I-9 | Whatever reveal-audit policy the owner selects is enforced on success and failure paths, and the event records the verified principal. | Failure-injection on audit append for each outcome; compare with the chosen policy. | Does not make the log tamper-proof. | Depends on the owner decision |
| I-10 | Output, logs and errors contain no plaintext, key bytes, or unintended sensitive metadata (including secret names and paths, as decided). | Capture stdout, stderr and logs for every command and failure mode. | "No sensitive metadata" requires an agreed definition first. | RB(ARCH-B); Rec today |
| I-11 | Restore and backup are service-side or offline with explicit locking, and the concurrency limitation is not silently reintroduced. | Concurrent-writer tests; verify refusal while another process holds the database. | Currently unimplemented: restore takes no cross-process lock (stated in the `restore` help); how to handle this remains undecided and is separately deferred (see the register). | RB(ARCH-B) |
| I-12 | Policy changes and upgrades are atomic and fail closed; a missing or corrupt policy denies. | Failure-injection during policy update and upgrade. | None. | RB(ARCH-B) |

Scope notes: no invariant is claimed as already satisfied; I-1 does not imply same-UID or root protection; I-6 does not imply full key isolation.

## 9. Residual risks and out of scope

Residual: root, same-UID processes sharing a service identity, service compromise, the unkeyed audit chain
(section 4 path 7). Out of scope for this draft: network access, HSM/KMS integration, multi-host operation, HMAC or
external anchoring (future options only; no format change is proposed), quarantine and its release path, session or
token systems.

## 10. Rollout considerations

Introduce behind an explicit opt-in, keep the direct path for existing users until a migration is validated,
document the ownership change, and add the authorization matrix tests before enabling enforcement.

## 11. Owner decisions required (all unresolved)

1. Is the same-user threat in scope? Does central get a key-holding service (ARCH-B), stay on ARCH-A, or defer (ARCH-C)?
2. If ARCH-B: the service account model and whether a break-glass direct mode exists, who may use it, and how it is audited.
3. The principal and role model (numeric uid only, groups, roles), and who administers it.
4. The operation policy table, including who may reveal, disable, delete, back up and restore.
5. How backup, restore and audit verify work under a service, and how caller-supplied paths are removed.
6. Which hardening list (section 5) is required, and the supported platforms.
7. How local mode and the shared `dso secret` grammar relate to the service.
8. Supported filesystems and platforms (section 7).
