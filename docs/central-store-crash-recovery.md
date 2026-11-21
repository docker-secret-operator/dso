# Central store — crash-recovery artifact scanner (Phase D)

Status: **experimental, read-only, detection and reporting only.** This
document describes `dso central recovery-status` and the library it is built
on (`sqlite.ScanRecoveryArtifacts`, `centralsvc.ScanActiveRecoveryArtifacts`).
It assumes the reader already knows the central lock, `InspectDatabase`,
raw preservation and displacement behavior described in
[`central-store-m1.md`](central-store-m1.md); this document does not restate
that material, only what Phase D adds on top of it.

## Purpose and limitations

After a crash (process kill, power loss) during `dso central restore`, the
database directory can hold leftover artifacts: a raw preservation of the
original files, a displacement "aside" directory, or a prepared-replacement
temporary file. `recovery-status` looks for these, reports exactly what it
can prove about them from the filesystem, and states plainly what it cannot
prove. It never recovers or cleans up anything.

**What it never does**, regardless of what it finds:

- Open the active database through SQLite, trigger WAL recovery, or create
  or modify a `-wal`, `-shm` or `-journal` sidecar.
- Call `sqlite.Open`, `InspectDatabase`, or `CheckInitialized` against the
  active database.
- Delete, rename, move, repair or rewrite any artifact it finds, or
  automatically restore a displaced database or install a `.restoring-*`
  file.
- Infer transaction completion, install outcome, or file identity from a
  filename, a timestamp, or directory shape alone.
- Use `os.SameFile` against an identity that was never persisted to disk —
  no displacement or replacement-temp artifact carries one; only a
  published preservation has a manifest, and that manifest proves
  self-consistency with itself, never that the file it describes is still
  the legitimate original.
- Claim that no process anywhere is using the database, or that a database
  is safe to open solely from this scan's output.
- Treat age alone, or a filename match alone, as proof that an artifact is
  abandoned, disposable, or safe to restore.
- Write an audit event, or include a recovery-artifact path in one.

## Artifact naming conventions

| Pattern | Written by | Meaning |
|---|---|---|
| `<db>.pre-restore-raw-<UTC>-<hex>.partial` | `sqlite.PreserveActiveFiles` | A raw preservation still being written; never published. |
| `<db>.pre-restore-raw-<UTC>-<hex>` (no `.partial`) | `sqlite.PreserveActiveFiles` | A published, atomically-renamed raw preservation, with `manifest.json` recording each file's name, source path, size and SHA-256. |
| `<db>.replaced-<UTC>-<hex>/` | `sqlite.ReplaceActiveDatabase` | A displacement "aside" directory holding the original `main`/`wal`/`journal`/`shm` files moved out of the way before (or during) installing a replacement. No manifest is written for this directory. |
| `<db>.restoring-<hex>` | `sqlite.ReplaceActiveDatabase` (via `PrepareReplacement`) | A prepared-replacement temporary file, beside the database, before (or possibly during) its install rename. |

None of these are ever deleted automatically by any part of this system,
including this scanner.

## Crash-state classification matrix

Ten points of interruption during a displacement, and what the scanner can
and cannot say about each (see the Phase D.1 design report for the full
derivation):

| # | Interruption point | Confidently stated | Ambiguous | DB unsafe to open? |
|---|---|---|---|---|
| 1 | Before displacement begins | No artifact of this attempt found | Whether a restore was ever attempted | No |
| 2 | Aside dir created, nothing moved | The aside directory is empty | None | No |
| 3 | Main moved, companions not | A file exists at the aside path; no main file at the active path | Whether that file is genuinely the pre-restore original | **Yes** |
| 4 | Some companions moved | Which files are at which path | Provenance of any individual file | **Yes** |
| 5 | All displaced, before install | Aside dir complete; a `.restoring-*` file exists; no active-path main file | Whether the `.restoring-*` file is still valid | **Yes** |
| 6 | During install rename | Facts about what is at each path | Which path holds "the" database | **Yes (highest severity)** |
| 7 | Installed, pre-durability-sync | Content hash match against an operator-supplied reference, if any | Whether the install is crash-durable | Not necessarily, but never claimed safe |
| 8 | Installed + verified (success) | Normal completed state; aside dir retained for the record | None | No |
| 9 | During rollback | Per-file facts only | Sequencing/completeness of the rollback | **Yes** |
| 10 | After rollback, before cleanup | Files exist at original paths; aside dir may be empty | Whether restoration was byte-perfect | Not provably safe without an independent reference |

Rows 3, 4, 5, 6 and 9 remain permanently ambiguous without a persisted
recovery marker. **Phase D does not introduce one.** If one is ever added, it
would be a separate, owner-approved architectural change — not an assumption
this scanner relies on.

## Stale-entry classification matrix

| Entry type | Confidence ceiling | Notes |
|---|---|---|
| `.partial` preservation | Inconclusive | Reported as "incomplete, never published" — never "abandoned." The writing process may still be running; the shared lock only excludes another `dso central` process. |
| Published preservation (manifest matches) | Fact | Reported as "internally self-consistent with its manifest" — **never** "disposable," regardless of age. Whether it is still needed depends on whether the restore it preceded completed, which it does not record about itself. |
| Published preservation (manifest mismatch/missing/invalid) | Manual intervention | Missing or unexpected files, or an unparseable manifest, are always reported, never silently accepted. |
| Aside directory, empty | Fact | Reported as empty; never removed by this tool. |
| Aside directory, non-empty, main file present at active path | Fact (limited) | A file exists at the active path; the scanner does not verify it is the legitimate replacement. |
| Aside directory, non-empty, main file absent at active path | Manual intervention | Matches crash-state rows 3/4/9; database unsafe to open. |
| `.restoring-*` temp file | Manual intervention | Always requires reference-based comparison by the operator; never auto-installed. |
| Symlink / non-regular file anywhere in scope | Manual intervention (anomaly) | Never followed or opened. |
| Ownership/permission anomaly | Reported, confidence unaffected beyond manual-intervention tier | States only what was observed, not why. |
| Age alone | Never raises confidence | Explicit rule; age is reported where relevant but never used as evidence of abandonment. |

## Shared-lock behavior and its side effect

`recovery-status` acquires `sqlite.AcquireSharedCentralLock` before reading
the filesystem. This is compatible with any number of other shared holders
(ordinary `dso central` commands) and incompatible only with a concurrent
**exclusive** `Restore` critical section — matching the lock modes already
described in `central-store-m1.md`.

**Side effect, disclosed plainly:** acquiring the lock, in either mode,
creates the sidecar file `<db>.central.lock` if it does not already exist
(the same thing every other `dso central` command already does on first use).
This is the one filesystem write this scanner can ever cause. It never opens,
creates, or modifies the database file itself or any of its SQLite sidecars.

**What holding the lock proves, and what it does not:**

- It proves no other `dso central` process held a conflicting lock on this
  path for the duration of the scan.
- It does **not** prove no process anywhere is using the database: the lock
  is advisory, local to this host, and invisible to a process that opens the
  database, its sidecars, or the lock sidecar directly (the `sqlite3` CLI, a
  script, or anything on another host).

**If the lock cannot be acquired** (for example because an exclusive
`Restore` is in progress, or the timeout expires): `recovery-status` does
**not** scan the filesystem and does **not** classify any artifact. No
filesystem fact is reported for that run; the result is explicitly
inconclusive, and the command says so plainly rather than scanning unlocked
and reporting as if it had coordination it did not have. Retry once the
competing `dso central` operation has released the lock. (Recall from above
that even a successful acquisition only ever proves no other `dso central`
process held a conflicting lock — never that no process anywhere, including
on another host, is using the database.)

## Observed facts vs. unresolved questions

Every artifact in the command's output is reported as two separate lists:
**Facts** (statements the scanner can support directly from what it
observed) and **Unresolved** (the specific questions the available evidence
cannot answer). The two are never blended into one sentence, and a
`Confidence` value (`fact`, `inconclusive`, `manual-intervention`) never
means more than what it says — in particular, `fact` never means "this state
is fine," only "this specific statement is true."

## Manual recovery guidance

This tool performs no recovery. If an artifact is reported with confidence
`manual-intervention` and the active database is flagged unsafe to open:

1. Stop every process that might use the database.
2. Copy every file this scan reported (the aside directory's contents, the
   `.restoring-*` file, any preservation directory) to a safe location before
   doing anything else.
3. Compare content hashes (reported by this scan) against any independent
   reference you hold — the original restore candidate file, a separate
   backup, or operator notes from before the restore was attempted.
4. Only once you are confident which file is which, decide how to proceed by
   hand. This tool will not do it for you, and no future version of it will
   silently start doing so without a separate, explicit design change.

## Explicit prohibitions, restated

This scanner — and any future change to it — must never delete, rename,
move, repair or rewrite a recovery artifact; must never open the active
database through SQLite or trigger WAL recovery; must never claim an
artifact is safe to delete or safe to restore; and must never claim broader
protection from the central lock than "no other `dso central` process held a
conflicting lock during this scan."
