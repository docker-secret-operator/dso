# DRAFT: security verification plan for dso central

Status: **Plan only. Nothing here is ratified, and no test was run or written to prepare it.** Written 2026-10-30 at
repository HEAD `3645b89`, revised after a coverage re-inventory. The scenarios apply to the current implementation
(ARCH-A behavior) and, where marked, to a future key-holding service (ARCH-B), which is not built and not selected.
Naming a scenario does not authorize building the code it would test. No proposed requirement in this plan is approved.

## 0. Method, terminology and classes

**Inventory method.** For every scenario below, the test files named in the "Existing" column were opened and the test
bodies (and, where relevant, their helpers) were read to see what each test actually asserts. Where only a name was
read, the row says "name only". Searches for the absence of a test covered the test sources under
`internal/centralsvc`, `internal/store/sqlite`, `internal/cli`, `internal/securefs` and `internal/crypto`, by function
name and by keyword in test bodies; the keywords are given in the row. "No test found" means no match within that scope,
not proof that none exists anywhere.

**Source inspection is not execution.** No test was run. Every "Existing" label means "a test in source appears to assert
this", not that it passes. Establishing that a test passes requires running it.

**Coverage labels.**
- **Existing, direct:** an inspected test asserts the expected property for the stated case.
- **Existing, partial:** an inspected test covers part of the scenario; the gap is stated.
- **No test found:** no match after the stated search.
- **Manual:** requires a manual procedure (given, with objective criteria) because it needs processes, accounts, OS
  settings or infrastructure.
- **Blocked:** cannot be verified until the thing it tests exists or an owner decision is made.
- **Proposed new test:** a test that would need to be written; none is written or authorized by this plan.

**Classes** (used by the ARCH-B design draft, section 13): **U** = automated unit test inside one package; **I** =
automated integration test (several components, real files, subprocesses or, later, sockets); **M** = manual procedure.

**Existing test seams.** `centralsvc.Restore` has unexported fields `testReplaceDatabaseFile`, `testReopen` and
`testAppendRestoreEvent` (`internal/centralsvc/restore.go`); tests in package `centralsvc` can set them to inject
failures or run code at those three points. No seam exists for: event-ID generation, the safety snapshot, the pre-replace
checkpoint and close, or staging. Adding hooks would be a code change and is not authorized here.

**General prerequisites and evidence.** Use a disposable scratch directory (mode 0700, local filesystem), a fresh
database per scenario and the key file in a separate directory (the CLI refuses a key in the database's directory unless
`--allow-colocated-key`). Never use real data or a real key. Run as a non-root user unless the scenario needs root, in a
disposable VM or container. Killed `restore` or `audit verify` runs can leave `dso-restore-*` or
`dso-restore-validate-*` directories in the temporary directory; record them and remove only artifacts the scenario
created. Capture for every scenario: command lines, exit codes, stdout and stderr, `ls -l` of the scenario directory
before and after, `PRAGMA integrity_check`, the audit event list and the `audit verify` result before and after, the
Git commit of the binary, the OS and the filesystem type.

## 1. Concurrent audit appends from multiple processes

| ID | Scenario | Existing | Label | Prerequisites, expected outcome, evidence |
|---|---|---|---|---|
| V-1.1 | N processes append audit events to one database | None. Searched test bodies for `exec.Command`/helper processes: only `TestMultiProcessWritersNoLostUpdate` and `TestHelperProcessWriter` (`internal/store/sqlite/sqlite_test.go`, :688 and :661; they perform `AddVersion` writes, not audit appends) and the staging-signal helper | No test found; proposed new test (I) | Helper-process pattern as in `TestHelperProcessWriter`. Expected: `VerifyAuditLog` reports verified, event count equals the total of successful appends, no two events share a `prev_hash`. Evidence: counts, tip hash, verify result. |
| V-1.2 | Many goroutines, one `Store` | `TestAppendAuditEvent_ConcurrentStandaloneAppendsStayLinked` (`internal/store/sqlite/audit_test.go:250`): 20 goroutines call `AppendAuditEvent`; asserts no error, 20 events and `VerifyAuditChain` success | Existing, direct for in-process; says nothing about several processes | — |
| V-1.3 | Busy-timeout expiry on an append | `TestAC_TX_04c_StandaloneWriteTimesOutWithRawBusyError` (`internal/store/sqlite/sqlite_tx_test.go:450`): one `Store`, a `WithTx` holds the write lock, a standalone `CreateProject` waits 200 ms and gets the raw busy error with no row written | Existing, partial: it tests a project write in one process, not an audit append, not the best-effort path, not the CLI | Proposed new test (U/I): hold the write lock, call a best-effort audit append, assert the business result is unchanged and the event is absent. |
| V-1.4 | Concurrent first initialisers / bind | `TestBinding_ConcurrentFirstInitSameKeyAllSucceedWithOneBinding` (`internal/centralsvc/binding_test.go:295`), `TestBinding_ConcurrentFirstInitDifferentKeysExactlyOneWins` (:342; goroutines each open their own `Store` on one file), `TestMeta_ConcurrentInitialisersHaveExactlyOneWinner` (`internal/store/sqlite/sqlite_meta_test.go:55`) | Existing, direct for in-process concurrency; not multi-process | — |

## 2. Audit-chain modification, tail deletion and rollback

| ID | Scenario | Existing | Label | Notes |
|---|---|---|---|---|
| V-2.1 | Modify an event field, break a link, empty or invalid hash | `TestAuditChain_TamperedFieldDetected` (`audit_test.go:88`, in-memory events); `TestVerifyAuditLog_DetectsTampering` (`audit_verify_test.go:61`): four database-level cases (modified field, broken link, empty hash, invalid hash), asserts integrity-failure status, failure index, reason and event id; `TestCentralAuditVerify_IntegrityFailure_Exit10` (`internal/cli/central_audit_verify_test.go:119`): exit code 10, text and JSON document | Existing, direct | The tamper helper (`audit_verify_test.go:16`) drops the append-only triggers before modifying rows. |
| V-2.2 | Delete the newest events | `TestAuditChain_TailDeletionNotDetected` (`audit_test.go:146`, in-memory); `TestVerifyAuditLog_TailDeletionIsNotDetected` (`audit_verify_test.go:217`): deletes the last event at the database level and asserts status verified with one event | Existing, direct (documents the limitation) | — |
| V-2.3 | Delete or insert a middle event | `TestAuditChain_BrokenPrevHashLinkDetected` (`audit_test.go:106`) and `TestAuditChain_MissingEventHashDetected` (:124), both on in-memory slices; the "broken link" case of V-2.1 | Existing, partial: no test deletes or inserts a middle row in the database | Proposed new test (U). |
| V-2.4 | Drop the triggers and recompute a valid chain | `TestAuditEvents_AreImmutable` (`audit_test.go:443`) shows UPDATE and DELETE are rejected while the triggers exist; the tamper helper shows a writer can drop them | Existing, partial: nothing recomputes a valid chain over modified events | Proposed new test (U) that documents the limitation: verification reports verified. |
| V-2.5 | Restore an older valid backup | `TestRestore_OlderValidBackupPassesChainVerification_DocumentedLimitation` (`internal/centralsvc/restore_test.go:175`): backup, later activity, restore; asserts the restored chain verifies and environments added after the backup are gone. `TestRestore_SuccessfulRoundTripAppendsLinkedRestoreEvent` (:117): restore event is last, carries the caller-supplied actor, and the chain verifies | Existing, direct for chain acceptance; not asserted: that the newer events survive in the safety snapshot | Proposed extension (I): open the snapshot and assert it holds the newer events. |
| V-2.6 | Unsupported audit schema version | `TestVerifyAuditLog_UnsupportedAuditSchemaVersionIsUnavailableNotTampering` (`audit_verify_test.go:88`): sets version 99, asserts unavailable naming version 99; `TestAuditSchemaVersion_SupportedFutureAndInvalid` (`audit_test.go:181`, name only); CLI exit 11 is asserted for a missing database by `TestCentralAuditVerify_Unavailable_Exit11_DistinctFromIntegrity` (:139) | Existing, direct at store level; CLI exit 11 for a schema-version cause is not asserted | — |

## 3. Wrong key and database/root-key mismatch

| ID | Scenario | Existing | Label | Notes |
|---|---|---|---|---|
| V-3.1 | Second root key against a bound database | `TestBinding_SecondKeyCannotCreateProjectsOrAnythingElse` (`binding_test.go:77`): every service operation returns `ErrWrongRootKey`; table row counts unchanged; `TestBinding_EmptyBoundDatabaseCannotBeTakenOverByAnotherKey` (:108) | Existing, direct. The row-count helper covers projects, environments, secrets, secret_versions and store_meta; it does not count audit_events. | — |
| V-3.2 | Edited key-id label, corrupted or deleted binding rows | `TestBinding_TamperingWithTheKeyIdFieldCannotBypassTheCheck` (:123), `TestBinding_CorruptedMetadataFailsClosed` (:159; deleted rows, flipped or truncated canary, unknown format), `TestBinding_DeletedBindingCannotBeAdoptedByAWrongKey` (:226) | Existing, direct | — |
| V-3.3 | Restore candidate with a wrong key | `TestValidateCandidateKey_WrongKeyRejectedBeforeActiveDatabaseModified` (`restore_test.go:29`); `TestRestore_WALCandidate_WrongKeyDoesNotReplaceActiveAndCleansStaging` (:818): `ErrWrongRootKey`, active bytes unchanged, no safety snapshot, no leaked staging; `TestRestore_FailedKeyValidationLeavesActiveDatabaseUntouched` (:661, name only); CLI `TestCentralRestoreCmd_WrongKey_NoReplacement` (`central_backup_restore_test.go:221`): error mentions the root key, active bytes unchanged | Existing, direct | — |
| V-3.4 | A wrong-key attempt writes no audit event (current behavior) | None: the binding tests' row-count helper omits `audit_events`, and the restore wrong-key tests assert bytes and snapshots, not events | No test found; proposed new test (U) | Whether such attempts should be audited is an owner decision (audit draft section 6). |

## 4. Restore interrupted at each significant activation stage

Stages (`restore.go`): stage and validate; confirmation; safety snapshot; checkpoint and close; replace (copy, fsync,
rename, directory sync); reopen; append the restore event; durability confirmation.

| ID | Scenario | Existing | Label | Notes |
|---|---|---|---|---|
| V-4.1 | Replacement fails before activation | `TestReplaceDatabaseFile_OrderingAndFailureLeavesOldMainInPlace` (`restore_test.go:290`; fake files, not a database); `TestRestore_ReplacementFailureBeforeActivation_LeavesActiveDatabaseUsable` (:533; seam `testReplaceDatabaseFile`): error says "before activation" and names the snapshot, candidate untouched, active database opens with its project, snapshot opens | Existing, direct. The test notes the active file's bytes legitimately change because of the pre-replace checkpoint. | — |
| V-4.2 | Checkpoint cannot complete | `TestRestore_IncompleteCheckpointAbortsBeforeSidecarsOrMainFileTouched` (:875, name and header comment only) | Existing, direct by its stated purpose; body not fully read | — |
| V-4.3 | Stale or pending `-wal`/`-shm` | `TestRestore_StaleWALAndSHMDoNotCorruptRestoredDatabase` (:334), `TestRestore_WALCandidate_ActivatedStateEqualsValidatedState` (:754, name only), `TestValidateCandidate_WALModeCandidateWithPendingFramesIsNeverModified` (`restore_validate_test.go:129`, name only) | Existing, direct | — |
| V-4.4 | Reopen fails after activation | `TestRestore_ReopenFailureAfterReplacement_DoesNotPromiseAutomaticRecovery` (:605; seam `testReopen`): error names the snapshot and says "not automatically recovered", no event id, active file equals the candidate, snapshot opens | Existing, direct | — |
| V-4.5 | Process killed (`SIGKILL`) at a stage | None. Related pattern: `TestCrashDuringWritesLeavesConsistentDatabase` (`sqlite_test.go:717`) kills a writer helper with `SIGKILL` and reopens the database | No test found; see the method below | **Method using existing seams only (proposed new in-package helper-process test, no production change):** the helper sets `testReplaceDatabaseFile` to exit before replacing, or to call `replaceDatabaseFile` and exit immediately after; sets `testReopen` or `testAppendRestoreEvent` to exit instead of returning. This reproduces kills at: before activation, after rename and before reopen, after reopen and before the event. **Not reproducible without a new hook:** kills during staging or validation, during the safety snapshot, during checkpoint and close, and between the real event append and return. Those stages are **Blocked** on a hook (a code change that is not authorized) or can only be hit by non-deterministic external timing, which this plan does not accept as a pass/fail test. **Objective criteria after each kill:** (1) the database at the active path opens and `PRAGMA integrity_check` = ok; (2) its content equals exactly the pre-restore content or exactly the candidate's (compare project slugs and the audit event count and tip), never a mixture; (3) if the kill came after the safety snapshot stage, the snapshot file exists and opens; (4) list all leftover `<db>.restoring-*` files and `dso-restore-*` directories; (5) a second `restore` or `audit verify` run succeeds or fails with a defined error. |
| V-4.6 | `SIGTERM`/`SIGINT` during staging | `TestStaging_GracefulSIGTERMRemovesStagingAndStillTerminates` (`audit_verify_unix_test.go:191`): the parent starts a subprocess that runs the helper `TestStagingSignalHelperProcess` (:175), which calls `InstallStagingSignalCleanup` and `StageCandidate`; the parent sends `SIGTERM`, asserts the child dies by that signal and its staging directory is removed | Existing, partial: it exercises the `sqlite` package handler in isolation. It does not run the `restore` or `audit verify` command, and `SIGINT` is not tested. | Proposed new test (I) for the command wiring and `SIGINT`. |
| V-4.7 | Concurrent writer during restore (documented: no lock, writes may be lost) | None | Manual characterization | Run `restore` while a helper loop (as `TestHelperProcessWriter`) writes; record which writes survive. There is no security pass/fail, only a recorded outcome that must match the documented limitation. |
| V-4.8 | Candidate path equals the active database | `TestRestore_CandidateAndActivePathsCannotBeConfused` (:67): `Restore` returns an error | Existing, direct | — |
| V-4.9 | Confirmation declined | `TestRestore_ConfirmationDeclinedLeavesActiveDatabaseUntouched` (:234, name only); CLI `TestCentralRestoreCmd_WithoutForce_DeclinesAndLeavesUnchanged` (:93), `…ExplicitNo…` (:306) (names only) | Existing by name; bodies not read | — |

## 5. Restore audit append and durability failures

| ID | Scenario | Existing | Label | Notes |
|---|---|---|---|---|
| V-5.1 | Audit append fails after activation | `TestRestore_AuditEventAppendFailureAfterActivation_ReportsUsableButUnrecorded` (:477; seam `testAppendRestoreEvent`): error wraps the injected failure and says the restore completed, the database is usable, the event could not be recorded; no event id; snapshot exists; the active database opens with the restored project | Existing, direct at service level. The non-zero CLI exit is not asserted by a CLI test (the seam is unexported); it follows from the returned error. | — |
| V-5.2 | Event-ID generation fails after activation | None; there is no seam for it (`newID` is not injectable) | Blocked: needs a seam or an RNG-failure injection (a code change) | — |
| V-5.3 | Durability unconfirmed (`unconfirmedDurabilityError`) | None. Searched test bodies in `internal/centralsvc`, `internal/cli` and `internal/store/sqlite` for `unconfirmedDurability` and `durabil`: no match | No test found; proposed new test (U) | Feasible without production change: a test in package `centralsvc` can have `testReplaceDatabaseFile` return an `*unconfirmedDurabilityError`. Expected: reopen and append proceed; the final error says the event was recorded but durability could not be confirmed; if the append also fails, the append error is returned instead. |
| V-5.4 | Power loss after rename, before directory sync | None | Manual, specialized infrastructure | Disposable VM with a hard power-off (hypervisor "destroy") or a write-dropping block device (for example device-mapper `flakey`/`log-writes`). Timing relative to the rename is not deterministic, so the result is a recorded characterization. Criteria after reboot: `integrity_check` = ok and content equals exactly the old or the new database. If the stage cannot be hit reproducibly, mark the run inconclusive; do not count it as a pass. |

## 6. Backup and restore with unexpected or invalid paths

| ID | Scenario | Existing | Label | Notes |
|---|---|---|---|---|
| V-6.1 | Backup destination already exists | `TestBackup_DestinationAlreadyExists` (`internal/store/sqlite/backup_test.go:106`): a pre-existing regular file; asserts `ErrBackupDestinationExists` and unchanged content; CLI `TestCentralBackupCmd_DestinationAlreadyExists` (`central_backup_restore_test.go:35`, name only) | Existing, direct for a regular file; directory and symlink destinations: no test found (searched `backup_test.go` and CLI tests) | Proposed new test (U) for those two cases. |
| V-6.2 | Backup destination in an unsafe directory | `TestBackup_UnsafeDestinationDirectoryRejected` (`backup_test.go:126`): a 0777 directory; asserts an error and that nothing was written. A foreign-owner directory is covered only at checker level by `TestCheckDirChain_ForeignOwnerIsRejected` (`internal/securefs/securefs_unix_test.go:87`) | Existing, direct for the writable-directory case | — |
| V-6.3 | Backup with `--db` naming a path that does not exist | None. The backup command checks only that the flag is set (`central.go`); only two CLI backup tests exist | No test found; proposed new test (I) | Documents current behavior: opening creates an empty database (0600) and any missing parent directories, migrates it and backs up the empty database. |
| V-6.4 | Backup / open on a database with an older schema | Migration backups are tested at `Open` level: `TestMigrationBackup_*` (`sqlite_backup_test.go`: fresh snapshot per attempt, private and complete, restore then migrate again, fresh database needs none, failure prevents migration) and `sqlite_test.go` (about :218, name checks) | Existing, partial: the `dso central backup` command on an older-schema database is not tested | Proposed new test (I). |
| V-6.5 | Backup during an in-flight write; failure leaves no partial file | `TestBackup_ConsistentDuringInFlightWriteTransaction` (`backup_test.go:186`), `TestBackup_FailureDuringVacuumLeavesNoPartialFile` (:147; cancelled context) | Existing, direct | — |
| V-6.6 | Invalid restore candidate | `TestValidateCandidate_InvalidSQLiteDatabaseRejected` (`restore_validate_test.go:44`), `…EmptyFileRejected` (:55), `…CorruptedAuditChainRejected` (:66), `…UnsupportedNewerSchemaRejected` (:102) (names read; bodies not all read); `TestRestore_InvalidCandidateCleansStaging` (`restore_test.go:853`): non-SQLite file, error, no leaked staging directories; CLI `TestCentralRestoreCmd_InvalidCandidate_NoReplacement` (:191): active bytes unchanged; `…NeverPromptsEvenWithYes` (:332, name only) | Existing, direct | — |
| V-6.7 | FIFO or symlink `-wal`/`-shm` sidecars | `TestSidecars_FIFOAndSymlinkAreRejectedAsUnavailable` (`audit_verify_unix_test.go:48`) and `TestCentralAuditVerify_UnavailableOutputIsSanitized` (`internal/cli/central_audit_verify_test.go`, which creates a FIFO `-wal` at :217): both go through `audit verify`, asserting an unavailable status | Existing, direct for `audit verify`. For **restore candidates**: no test found (`Mkfifo` appears only in the three audit-verify tests; searched all `*_test.go` under `internal/`). Restore shares the staging code but is not tested through it for this case. | Proposed new test (U). |
| V-6.8 | Symlinked database path or directory | `TestOpenRefusesInsecureOrInvalidFiles` (`sqlite_test.go:103`): loose mode, garbage file, a directory and a symlink are refused; `TestOpen_SymlinkedDatabasePathIsRefused` and `TestOpen_DatabaseUnderSafeSymlinkedDirectoryWorks` (`sqlite_dirsafety_test.go`, names only); `TestCheckDirChain_Symlinks` (`securefs_unix_test.go:98`, name only) | Existing, direct for the cases in `TestOpenRefusesInsecureOrInvalidFiles`; others by name | — |
| V-6.9 | Dry run changes nothing | `TestRestore_DryRunDoesNotMutateActiveDatabase` (`restore_test.go:76`; read in part: records the active bytes, runs with `DryRun`, asserts the dry-run flag and no safety snapshot); CLI `TestCentralRestoreCmd_DryRun` (:50, name only) | Existing, direct in part | — |
| V-6.10 | Unsafe restore destination directory; no active database yet | `TestRestore_UnsafeDestinationDirectoryRejectedWithNoActiveDatabase` (:384), `TestRestore_NoActiveDatabaseYetSkipsSnapshotAndStillWorks` (:415) (names only) | Existing by name | — |

## 7. Audit failures during mutations and secret reveal

| ID | Scenario | Existing | Label | Notes |
|---|---|---|---|---|
| V-7.1 | Audit insert fails inside a mutation transaction | `TestAudit_TransactionalWriteFailureRollsBackAndIsLogged` (`internal/centralsvc/audit_test.go:100`): `CreateProject` against a store whose audit write fails; asserts an error, the project is not found, and an error-level log entry; `TestWithTx_AuditInsertFailureRollsBackBusinessMutation` (`internal/store/sqlite/audit_test.go:281`, name only) | Existing, direct for project creation; the other five mutations: no individual test read | Proposed (U): extend to each mutation if the owner wants per-operation evidence. |
| V-7.2 | Best-effort audit append fails during reveal | `TestAudit_BestEffortWriteFailureIsLoggedAndDoesNotBlockTheRead` (`audit_test.go:55`): a failing audit store and a capturing logger; `GetValue` succeeds with the plaintext; an error-level log mentions the audit failure; no log field contains the plaintext | Existing, direct at service level | — |
| V-7.3 | Same through the CLI (nil logger) | None. The only CLI logger tests are in `central_audit_logger_test.go` and cover `init`'s logger | No test found; proposed new test (I) | Needs a way to make the audit append fail from the CLI without code changes (for example a database where the audit table is made unwritable in the test); if that is not possible, mark Blocked. |
| V-7.4 | Reveal event carries no plaintext | `TestAudit_RevealEventNeverContainsPlaintext` (`audit_test.go:239`): lists all events and asserts the plaintext is absent from id, actor, slugs, name, reason and hash fields | Existing, direct | — |
| V-7.5 | Early error paths that write no event (secret-creation ID/AAD/seal, project/environment ID/key/wrap, reveal AAD-build) | None; these paths have no injection seam | Blocked: needs a seam or an RNG/crypto failure injection (a code change) | Behavior is established by reading `service.go`, not by a test. |
| V-7.6 | One event per operation | `TestAudit_EachOperationCreatesExactlyOneEvent` (`audit_test.go:142`, name only) | Existing by name | — |
| V-7.7 | Fail-closed reveal (REVEAL-B/C) | None | Blocked until the owner decides the policy | — |

## 8. Unauthorized access and same-UID access paths

| ID | Scenario | Existing | Label | Notes |
|---|---|---|---|---|
| V-8.1 | Another local user reads or replaces key, database or backups | `internal/securefs` tests with a uid-simulation seam (`currentUID` is overridden by tests): `TestCheckDirChain_ForeignOwnerIsRejected` (`securefs_unix_test.go:87`), `TestOpenPrivateFile_ForeignOwnerFileIsRefused` (:207), `TestOpenPrivateFile_ReplacementAfterSetupIsDetected` (:181) (names only); `TestOpenRefusesInsecureOrInvalidFiles` (V-6.8) | Existing, partial: these simulate ownership; they do not run as a second OS account | — |
| V-8.2 | A real second OS account | None | Manual | Two accounts in a disposable VM. As account A create the database, key and backup (0700 directories). As account B run each `dso central` command against A's paths. Pass criterion: every attempt fails with a permission or securefs error and no file of A changes (compare checksums). |
| V-8.3 | Same-UID process reads the key/database or runs the CLI | None | Manual characterization of the documented limitation | A second shell as the same user reads the key file and runs `secret get --reveal`. Record that it succeeds; there is no security pass criterion, the record demonstrates the ARCH-A boundary. |
| V-8.4 | Under ARCH-B: non-service UID cannot open key/database (I-1); no command outside the service opens them; forged actor ignored (I-3); authorization before decryption (I-4); break-glass unusable by ordinary callers (I-2) | — | Blocked (no service) | The I-1 test shows only that different-UID callers are blocked; it does not show protection against same-UID callers of the service, the service UID or root. |

## 9. Root-key exposure and process memory / core dumps

| ID | Scenario | Existing | Label | Notes |
|---|---|---|---|---|
| V-9.1 | Key, DEK or plaintext in logs, errors, events, stdout | `TestLogsNeverContainPlaintextOrKeys` (`internal/centralsvc/service_test.go:279`): runs creation, version, reveal, a forced integrity failure and deletion with a capturing logger and asserts no log entry contains the plaintext or the raw key; `TestCentralCLIEndToEnd` (`internal/cli/central_test.go:42`): metadata commands' stdout and stderr do not contain the plaintext; `TestPlaintextNeverOnDisk` (`service_test.go:252`) and `TestMetadataResponsesContainNoPlaintextOrKeyMaterial` (:219) (names only); `TestAudit_RevealEventNeverContainsPlaintext` (V-7.4) | Existing, partial: no test scans error text, backup or restore output, or CLI stderr on failure paths; the CLI currently supplies no logger, so CLI log lines cannot leak | Proposed new test (I) once a CLI logger exists or for failure-path output. |
| V-9.2 | Core dump of the CLI contains plaintext or key | None | Manual, reproducible without code changes | In a disposable VM, set `ulimit -c unlimited` and a known `kernel.core_pattern` directory. Start `dso central secret create … --value-stdin` with a marker value written to a stdin pipe that is **kept open**: the CLI loads the key and opens the database before it reads the value and then blocks reading stdin, so the process holds the key material and the marker. Capture with `gcore <pid>`. Search the core for the marker and for the raw key bytes. **Record the result; no outcome is predicted or counted as a pass or fail.** Evidence: the exact search commands, byte offsets, the `core_pattern` and ulimit values. |
| V-9.3 | Same-UID read of process memory | None | Manual characterization | Same blocked process as V-9.2. As the same user attempt `gdb -p` or a read of `/proc/<pid>/mem`; record `kernel.yama.ptrace_scope` and whether each attempt succeeded. |
| V-9.4 | Locked pages | None | Manual | Read `VmLck` in `/proc/<pid>/status` of the blocked process and record it. Separately, the source search found no `mlock` use. |
| V-9.5 | Under ARCH-B: hardening applied as decided (I-6) | — | Blocked (no service, hardening list undecided) | Passing would show only that the listed routes are narrowed. |

## 10. CLI and service behavior when the socket or service is unavailable, and other ARCH-B-only checks

All **Blocked**: there is no central service, socket or listener. Today the CLI has no service mode and always opens files
directly.

| ID | Check | Related requirement | Blocked on |
|---|---|---|---|
| V-10.1 | Socket absent or permission denied: the client fails closed with no direct fallback | I-7; criterion 7 | A service; owner decision on break-glass |
| V-10.2 | Socket directory and path creation: private, verified, no path-based permission change after bind, single-instance, stale-socket and symlink/replacement races | I-8; criterion 8 | A service |
| V-10.3 | The service opens or creates no caller-supplied path (foreign, symlink and sidecar-bearing paths for restore and backup) | I-5; criterion 5 | A service and the owner's backup/restore design |
| V-10.4 | Restore and backup are service-side or offline with explicit locking; concurrent writers are refused | I-11; criterion 11 | A service; owner decision. Today's characterization is V-4.7. |
| V-10.5 | Policy and upgrade changes are atomic and fail closed; a missing or corrupt policy denies | I-12; criterion 12 | A policy model (undecided) |
| V-10.6 | Migration from direct access: ownership change documented, rollback possible, direct path kept until validated | Criterion 13 | A service. Schema-migration tests (V-6.4) do not cover ownership migration. |
| V-10.7 | **Proposed, pending owner approval:** the client verifies the service's identity (and relevant socket-path and directory ownership) before sending any secret value or passphrase; scenarios would include a decoy listener bound to the expected path by another UID and by the same UID, a socket path replaced after check, and a stale socket | Proposed criterion 14 (design draft section 3A); not part of I-1 to I-12 | A service; owner approval of the requirement; a decision on the verification mechanism |

## 11. Traceability: invariants and acceptance criteria to this plan

Invariants I-1 to I-12 are in `2026-10-30-central-authz-architecture-threat-model-DRAFT.md` section 8; criteria 1 to 13
(and the proposed criterion 14) are in `2026-10-30-central-archb-design-input-DRAFT.md` section 13. All are proposed;
none is approved. Status today separates existing partial evidence under ARCH-A from requirements that are unverified or
blocked because no service exists.

| Invariant / criterion | Plan entries | Status today |
|---|---|---|
| I-1 / 1 | V-8.4 | Blocked (no service). |
| I-2 / 2 | V-8.4 | Blocked. |
| I-3 / 3 | V-8.4 | Blocked. Related fact: today's actor is caller-supplied (V-2.5 shows the restore event keeps the caller-supplied actor). |
| I-4 / 4 | V-8.4 | Blocked; the operation-by-principal matrix test (U) is proposed. |
| I-5 / 5 | V-10.3 | Blocked. Today's CLI uses caller paths directly (V-6.1 to V-6.7). |
| I-6 / 6 | V-9.2 to V-9.5 | Characterization procedures exist for the CLI (manual); hardened service blocked. |
| I-7 / 7 | V-10.1 | Blocked. |
| I-8 / 8 | V-10.2 | Blocked. |
| I-9 / 9 | V-7.1, V-7.2, V-7.7 | Current best-effort and transactional behavior has direct service-level tests; fail-closed variants are blocked on the owner's decision; "records the verified principal" is blocked (no principal). |
| I-10 / 10 | V-9.1, V-7.4 | Partial existing evidence under ARCH-A (service log scan, CLI metadata output scan, reveal-event scan); failure-path and backup/restore output are unverified; service-era requirement is blocked. |
| I-11 / 11 | V-4.7, V-10.4 | Today: documented limitation, characterization only. Requirement blocked. |
| I-12 / 12 | V-10.5 | Blocked (no policy). |
| Criterion 13 | V-10.6 (with V-6.4 for schema migration only) | Blocked. |
| Proposed criterion 14 | V-10.7 | Proposed, pending approval; blocked. |

## 12. Separate issue recorded, not part of this plan: the CLI Guard script

`scripts/validate-cli.sh` is a CI-style check, reported earlier as failing with 26 pre-existing hits unrelated to the
central-store work. This task did not run it, did not change it, and did not remediate any hit. From reading the script:
it uses `git grep` over **tracked** files, minus an exclusion list, for two patterns (a bare `dso-agent` command, and a
standalone `dso <init|apply|sync|inject|diff|env|secret|system|up|down|logs|watch|fetch|compose|inspect|legacy-agent>`
command), and fails when either matches.

Observation for the owner (read-only): a simulation of the second pattern with `grep -E` over `docs/decisions/` shows that
three existing lines in the unchanged decision drafts mention the shared secret-command grammar in a form that matches it
(two in `2026-10-30-central-authz-architecture-threat-model-DRAFT.md`, one in
`2026-10-30-central-step54-step55-register-DRAFT.md`). They would add hits **if those files are ever tracked**. They are
untracked today, so the script does not scan them. The new documents from this work, and the updated
`docs/central-store-m1.md`, produce no matches in that simulation. Handling this belongs to the separate task.
