
# Central store audit log (Phase 2 Step 5.3)

Status: **implemented in this milestone, as approved in
`docs/reviews/2026-10-25-central-store-phase2-audit-chain-decision-resolution.md`**.
Builds on `docs/central-store-m1.md` and the Phase 2 design/review documents
under `docs/reviews/` — read those for the full rationale; this page states
only what is actually implemented and what it does and does not guarantee.

## What generates an audit event

| `dso central` operation | Event type | Recorded on |
|---|---|---|
| `init` | `init` | success, and failure **once a database exists to record it in** — see the note below |
| `project create` | `project_create` | success and failure |
| `environment create` | `environment_create` | success and failure |
| `secret create` | `secret_create` | success and failure |
| `secret add-version` | `secret_add_version` | success and failure |
| `secret get --reveal` | `secret_get_reveal` | **every call**, success and failure (not sampled) |
| `secret disable-version` | `secret_disable_version` | success and failure |
| `secret delete` | `secret_delete` | success and failure |

**`init`'s failure coverage is necessarily partial, by construction, not by
omission**: `init` can only write an audit event into a database that already
exists and opened successfully. A failure during preflight checks, key
installation, or opening the database itself happens **before** any database
exists to record an event into, and cannot be recorded there — there is
nothing this milestone (or any audit mechanism stored in that same database)
could do about that. Only a failure during the final `Bind` step, and a
successful `init` overall, can produce an `init` audit event, because those
are the only two points where an opened database already exists. `init`'s own
command output and returned error already report every failure in full to the
operator; only the *audit trail specifically* is unavailable for the earlier
failure modes.

`project list`, `environment list`, `secret list`, and `secret versions` are
metadata-only reads and **do not** generate individual events, by design
(they would flood the log with low-value events; see the Step 5.3 design
§4 for the full rationale).

## What is stored, and what is deliberately excluded

Each event records: an event id, schema version, event type, outcome
(success/failure), a closed failure-category (never a raw error string, so
an audit row can never leak implementation detail or an error message
built from sensitive input), the caller-supplied actor label (see below),
project/environment/secret/version identifiers, a UTC timestamp, and the
hash-chain fields (`prev_hash`, `event_hash`).

**Never recorded**: a secret's plaintext value, the encryption key or any
wrapped key material, or a raw error string. This was verified by automated
test (`TestAudit_RevealEventNeverContainsPlaintext`,
`internal/centralsvc/audit_test.go`).

**Actor label vs. verified identity**: the `actor_label` field is the same
caller-supplied attribution string DSO has always recorded (what used to be
`created_by`/`disabled_by`). It is **never treated as authenticated
identity** — there is no socket or credential mechanism in this milestone
(Option B, Step 5.2) that could actually verify who is calling, so there is
nothing today to populate a stronger "verified identity" field with. Do not
read `actor_label` as proof of who performed an action.

## How chain verification works

Each event's `event_hash` is `SHA-256(prev_hash || canonical(event fields))`.
`sqlite.VerifyAuditChain(events)` (exported from `internal/store/sqlite`)
recomputes every hash and checks every link; it returns a non-nil error at
the first row whose hash doesn't match its own fields or whose `prev_hash`
doesn't match the previous row's `event_hash`. **No CLI verification
subcommand was implemented in this milestone** — the approved design did not
require one, and adding one was out of this task's scope.

## What a failed audit write means for the triggering operation

- For `project create`, `environment create`, `secret create`,
  `secret add-version`, `secret disable-version`, and `secret delete`: the
  business mutation and its audit event share one database transaction. **If
  the audit write fails, the business mutation is rolled back too** — the
  operation fails as a whole, and a separate, best-effort failure event is
  then recorded outside that (now-rolled-back) transaction.
- For `secret get --reveal` (a read): there is no business mutation to roll
  back. The secret value is returned to the caller **regardless of whether
  the audit write for that read succeeds or fails** (availability-first, as
  approved in Gate 6). A failed audit write for a reveal is logged at error
  level and nothing else — the caller still gets their value.

## Log-only reporting is not durable evidence

Audit-write failures are reported synchronously, at error level, through the
repository's existing structured (`zap`) logging convention — uniformly
across every covered operation, including `init`, which constructs its own
minimal `zap` logger since it runs before any `Service` exists to share one
with. (An earlier version of this behavior had `init` report failures as
plain stderr text instead; an independent audit found that did not satisfy
this requirement, and it was corrected to use `zap` like every other
operation.) **This depends on the configured log sink being available and
retained.** No persistent
audit-health flag, sentinel file, or external service was implemented in
this milestone (approved Gate 6) — if that log output is lost, dropped, or
rotated away, there is currently no other record that an audit write
failed.

## What the chain cannot detect

Verified by an explicit, automated negative test
(`TestAuditChain_TailDeletionNotDetected`,
`internal/store/sqlite/audit_test.go`): **a hash chain whose most recent
events have been deleted is indistinguishable, under this verification
alone, from a chain that simply hasn't grown further yet.** The same applies
to the whole database file being replaced by an older, internally-valid
backup — the restored chain is fully self-consistent and passes verification
cleanly. **No external trust anchor was implemented in this milestone**
(approved Gate 1, deferred) — closing this gap requires a reference point
outside the database file, which this milestone does not provide.

## Retention

Audit history is **unbounded and append-only** in this milestone (approved
Gate 5) — there is no pruning, archival, or compaction. The approved revisit
triggers for this decision are: (1) measured growth once real deployments
have accumulated meaningful history, (2) an operator reporting a noticeable
storage or backup-time impact, or (3) the Step 5.4 backup/restore design,
when it is proposed.

## Security features not provided in this milestone

- **No external trust anchor** for rollback detection (Gate 1, deferred).
- **No separate MAC/signing key** — the chain uses a plain SHA-256 hash
  chain only (Gate 3, approved).
- **No persistent audit-health status or sentinel file** — failures are
  log-only (Gate 6, approved).
- **No restore-event semantics** — there is no restore command yet in this
  milestone to attach one to (Gate 7, deferred to Step 5.4).
- **No audit-history-specific authorization** beyond today's existing
  OS-level file-permission boundary — deferred to the Step 5.2
  authorization-matrix extension, when Option B (a local Unix-domain socket)
  is implemented (Gate 4, approved).
