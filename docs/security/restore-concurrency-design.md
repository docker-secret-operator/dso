# DSO Central restore: concurrency model and the TOCTOU window

## 1. What actually opens this database

Grep for every caller of `sqlite.Open` in the repository turns up exactly
three call sites, and they are all part of the same feature:

- `internal/cli/central.go:150, 327, 665` (`dso central init`, `dso central
  bind`-style commands, `dso central backup`)
- `internal/centralsvc/restore.go:295, 376` (`Restore`'s own safety-snapshot
  open and post-replace reopen)

Nothing in `internal/server` (the long-running REST/webhook server) or
`internal/agent` references `centralsvc` or `sqlite.Open` at all. **DSO
Central has no daemon.** It is a one-shot CLI administration surface ("the
M1 local administration surface of the central secrets store", per
`central.go`'s own doc comment) — every `dso central <subcommand>` invocation
opens the database, does its work, and closes it.

This matters because it changes the actual threat model from what the
restore code's own doc comment implies ("Run restore only while no other
process has the database open" reads as if a long-running server is the
risk). The realistic race is narrower and more mundane: **an operator (or a
script, or a second terminal) runs two `dso central` commands against the
same database file at the same time** — e.g. `dso central restore` racing a
concurrent `dso central backup`, or two restores racing each other.

## 2. Why SQLite's own locking does not cover this

- `sqlite.Open` sets `journal_mode(WAL)` and `_txlock=immediate`. WAL gives
  concurrent readers no interference from a writer, and `immediate` makes a
  writer grab SQLite's own write lock up front. This protects **SQL-level
  operations against each other** -- two `*Store`s issuing queries/writes
  against the same open file.
- It does **not** protect `replaceDatabaseFile`'s filesystem-level work
  (`os.Remove` of `-wal`/`-shm`, then `os.Rename` of a new file onto
  `dbPath`). That operation happens entirely outside any SQLite connection --
  by the time it runs, `Restore` has already called `active.Close()`. SQLite
  has no opinion about, and no lock over, a file it is not currently holding
  open.
- `Store.CheckpointTruncate` (`PRAGMA wal_checkpoint(TRUNCATE)`) proves no
  *other SQLite connection open at that instant* is blocking the checkpoint.
  It says nothing about a connection opened a moment later.

So the gap is real: between `active.Close()` and `replaceDatabaseFile`
completing, nothing stops a second `dso central` process from calling
`sqlite.Open` on the same path, doing a write, and having that write
silently discarded when the rename lands.

## 3. Options

### Option A -- Advisory flock held for the lifetime of every `sqlite.Open`

Acquire a non-blocking `flock(2)` (e.g. `golang.org/x/sys/unix.Flock`,
already a repo dependency via `internal/rotation`) on a sibling `<path>.lock`
file inside `sqlite.Open`, held until `Store.Close()`. Since every opener of
this specific database is one of the three call sites above, and all three
go through `sqlite.Open`, this is enforceable without touching unrelated
code -- there is no fourth opener to miss.

- **Security/concurrency guarantee:** at most one `dso central` process has
  the database open at any time, full stop. Closes the TOCTOU window
  completely: nothing can open the file during a restore because nothing
  can open it until `Restore`'s own `*Store` handles are closed.
- **Failure scenario:** a second `dso central` invocation while one is
  already running gets an immediate, clear "database is in use by another
  `dso central` process" error instead of blocking or silently racing.
- **Operational impact:** **this is a real behavior change**, not a pure bug
  fix. Today, two read-only `dso central` commands (e.g. two concurrent
  `status`-style reads) can probably run side by side under WAL. After this
  change, the second one fails until the first exits. Any operator tooling
  or script that runs `dso central` commands in parallel would need to
  serialize them.
- **Backward compatibility:** no on-disk format change; purely an added
  runtime constraint. A crashed process's flock is released by the kernel
  automatically (no stale-lock file to clean up), so this doesn't add a new
  failure mode of "lock stuck forever after a crash."
- **Complexity:** small -- one lock acquire/release pair inside `Open`/
  `Close`, no new dependencies.
- **Recovery:** unaffected; this only changes who may *open* the file, not
  how restore itself recovers from a mid-restore failure (the existing
  safety-snapshot/durability-warning machinery is untouched).

### Option B -- Lock only around `Restore`'s own critical section

Take the flock only inside `Restore`, around the span from the safety
snapshot through `replaceFn`, and release it before returning.

- **Guarantee:** this does *not* actually close the race, because nothing
  requires `dso central backup` or `dso central init` to check the same
  lock before calling `sqlite.Open` directly. A restore running under its
  own lock does not stop a concurrent, lock-ignorant `backup` from opening
  the file mid-restore -- the exact race Option A closes.
- To make Option B meaningful, every other `sqlite.Open` call site would
  need to check the same lock anyway, which converges back to Option A's
  footprint for no benefit. **Not recommended** as a standalone fix.

### Option C -- Explicit "maintenance mode" / operator-coordinated restore

Document (more forcefully than today) that the operator must ensure no
other `dso central` invocation is running before a restore, with no code
enforcement at all -- i.e., the status quo, just with a sharper warning.

- **Guarantee:** none beyond operator discipline. This is what the current
  code already has (the "Concurrency limitation" doc comment on `Restore`).
  It is not a fix; it is documentation, which is exactly what the prior
  pass already added and what this round of review correctly rejected as
  insufficient.

### Option D -- Coordinated shutdown/restart of a daemon

Not applicable: there is no daemon for this feature to coordinate with
(see SS1). This option would only make sense if DSO Central grew a
long-running server component in the future.

## 4. Recommendation

**Option A.** It is the only option that actually closes the gap, it is
enforceable today because every opener is already known and finite, it adds
no new persistent-failure mode (flock releases on crash), and the
complexity is small. Its one real cost -- serializing concurrent `dso
central` invocations that previously could overlap -- is an operational
behavior change to a CLI admin tool that is not documented anywhere today as
supporting concurrent use, so the risk of breaking an existing supported
workflow looks low, but it is still a deliberate trade-off, not a pure bug
fix, and is being surfaced here rather than implemented silently.

## 5. Decision needed

Implementing Option A changes `dso central`'s concurrency model from
"undefined, racy" to "single accessor at a time, enforced." This is exactly
the kind of change the project's own engineering rules reserve for explicit
approval rather than a silent fix. Holding off on implementation pending
that decision.

## 6. Implementation (approved, implemented, independently re-audited)

Option A is implemented:

- `internal/store/sqlite/centrallock.go` / `centrallock_unix.go` /
  `centrallock_other.go` -- the sidecar flock, `LockShared`/`LockExclusive`,
  `ErrCentralLockBusy`, `AcquireExclusiveCentralLock`.
- `internal/store/sqlite/sqlite.go` -- `Open` takes `LockShared` by default
  (reads, writes, backup all go through this unchanged); `Options.LockMode`/
  `LockTimeout` let a caller tune it.
- `internal/centralsvc/restore.go` -- `Restore` takes the exclusive lock
  right after the dry-run check (before `Confirm`), holds it via `defer`
  through safety snapshot, replacement, and reopen, and never through a
  dry run (which never touches `absDB` at all).

### 6.1 `SkipCentralLock` was removed before it ever shipped

An earlier draft exposed `Options.SkipCentralLock bool` so `Restore`'s own
internal `Open` calls (made while it already holds the exclusive lock)
would not self-deadlock. Independent review correctly flagged this as a
footgun: any future caller could set it without holding any lock at all,
silently defeating the whole protocol, and nothing short of a code review
would catch it.

Fixed by removing the field entirely (it is now unexported,
`skipCentralLock`, unreachable from outside the package) and replacing the
bypass with a capability type, `*sqlite.RestoreLock`:
`AcquireExclusiveCentralLock` now returns one instead of a bare `func()
error`, and only `(*RestoreLock).Open` can skip taking a fresh lock --
and only for the exact path that `RestoreLock` was acquired for (it
refuses a mismatched path outright). Possessing the lock object is the only
credential this package accepts; there is no struct field to copy-paste.

### 6.2 Lock identity follows symlinks

`absPathForLock` resolves symlinks (falling back to resolving just the
parent directory when the leaf doesn't exist yet, e.g. a fresh restore
target with no prior active database) before computing the sidecar path,
so two different symlinked routes to the same real database take the same
lock rather than two independent, uncoordinated ones. Covered by
`TestCentralLock_SymlinkedPathToSameDatabaseSharesTheLock`.

### 6.3 Sidecar hardening

The sidecar's own `open(2)` call uses `O_NOFOLLOW` and verifies the result
is a regular file, matching `securefs.OpenPrivateFile`'s existing discipline
for every other sensitive file in this codebase. The sidecar is never
deleted or truncated (see this file's own `centrallock.go` package comment
for why there is no "stale lock" state to clean up).

### 6.4 Candidate-validation ordering (TOCTOU between validation and use)

Not a new gap: `Restore` validates and stages the candidate
(`sqlite.StageCandidate`) into a private, process-owned temporary copy
*before* anything else, and every later step (key validation, activation)
operates on that same staged copy, never reopening or re-reading the
original candidate path. The candidate cannot be swapped out from under
Restore between validation and use, because nothing after staging ever
looks at the original path again.

### 6.5 Verified test coverage

12 lock-specific tests (11 cross-process via subprocess re-exec, 1
same-process path-identity test) plus the full existing `sqlite` and
`centralsvc` suites, all passing under `-race`. See
`centrallock_crossprocess_test.go` for the full list against the approved
lock matrix (shared/shared, backup-vs-backup, backup-vs-restore,
restore-vs-restore, read-vs-restore, write-vs-restore, release-after-close,
release-after-kill, bounded timeout, symlinked-path identity).

### 6.6 Platform and advisory-lock limitations (unchanged from SS2-3 above)

Advisory only, and only within this codebase's own openers; single-host
only (flock is not NFS-safe in general); a process bypassing this package
is not coordinated with. Unsupported platforms (`centrallock_other.go`)
fail closed rather than silently proceeding unlocked.

### 6.7 gosec CI: no gap found (correcting the prior round's claim)

The prior round's "gosec toolchain mismatch" finding was accurate for the
*local development sandbox* (Go 1.27.3 installed, newer than `go.mod`'s
pinned `toolchain go1.26.10`) but does not describe the actual CI
pipeline: `.github/workflows/ci.yml`'s existing "Security Scan" job already
uses `actions/setup-go@v5` with `go-version-file: go.mod`, which installs
exactly `go1.26.10` in CI regardless of what is on a contributor's laptop,
and already pins `gosec@v2.25.0`, scans the full repository (excluding only
a vendored npm package's bundled Go binding under `web/node_modules`), and
fails the build on any unsuppressed finding. No CI change is needed or was
made.

### 6.8 Moby CVEs (GO-2026-4887, GO-2026-4883): already tracked, independently corroborated

`.govulncheck-allowlist.txt` already lists both IDs with reasoning
matching this round's independent assessment almost exactly: both are Docker
*daemon*-side AuthZ-plugin advisories; DSO only uses the Docker client
packages; govulncheck's own reachability traces are package-init and
client-call paths, not daemon code; both remain `Fixed in: N/A` upstream as
of the allowlist's last review (2026-10-23). No dependency upgrade is
available yet, and none was attempted here per the engineering constraint
against unverified dependency bumps. This is tracked, re-verified, residual
risk -- not resolved, and not newly discovered.
