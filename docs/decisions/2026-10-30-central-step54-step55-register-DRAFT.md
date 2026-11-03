# DRAFT: Step 5.4 decisions and Step 5.5 questions (register)

Status: **Register only. Nothing here is ratified.** On 2026-10-30 the owner clarified that the six Step 5.4 decisions were substantively approved earlier; formal ratification is pending. Reading guide: see `README.md` ("Reading guide and option labels"). Written 2026-10-30. The original Step 5.4 design and
decision-resolution documents are unrecoverable (see `README.md`). Evidence below is limited to what the repository and the owner's responses in the working sessions contain (those responses are not stored in the repository). No decision is invented or reconstructed.

## Step 5.4: the six decisions, individually

"Evidence kind": *code comment* = a numbered comment in source written in an earlier working session (not a primary record); *prior owner response* = something the owner wrote in a working-session prompt (recorded only in that session, not in the repository); *behavior* = verified by reading code and tests. A prior owner response is evidence of what the owner said at the time. It is not by itself a durable decision record, and this register does not treat it as ratification.

| # | Decision (as summarized by the owner) | Evidence kind and location | Verified current behavior | Primary record |
|---|---|---|---|---|
| 1 | No external rollback anchor in this step | prior owner response; code comments `restore.go:15-17` and the Gate 1 comment in `audit.go` | No anchor exists; rollback to an older valid backup is not detected | Missing |
| 2 | Validate integrity and root-key binding; do not claim every secret version is decrypted | code comment `restore.go:57-58` ("decision 2"); prior owner response | `Restore` validates the staged candidate through `sqlite.ValidateStagedCandidate` (`internal/store/sqlite/restore_validate.go:322`) and `validateStagedCandidateKey` (`internal/centralsvc/restore.go:83`, called at `:273`); these check structure, schema, audit chain and the binding canary only. `ValidateCandidate` (`restore_validate.go:311`) and `ValidateCandidateKey` (`restore.go:72`) remain as path-based wrappers that stage a private copy and delegate to the staged variants | Missing |
| 3 | CLI names and flags: `backup --out`, `restore --from`, `--dry-run`, `--force` with confirmation | prior owner response; `internal/cli/central.go` | Present and tested | Missing |
| 4 | No automatic backup pruning | code comment `backup.go:64` ("Step 5.4 decision 4") | No pruning exists | Missing |
| 5 | Caller restrictions are policy intentions, not enforced | code comment `restore.go:239-240` ("decision 5"); prior owner response | No caller restriction exists on any command | Missing |
| 6 | Step 5.3 audit-retention decision not reopened | **prior owner response only**; no code comment | No retention or pruning logic exists; the nearest code fact is append-only (Gate 5 comment) | Missing |

**Status (owner clarification, 2026-10-30):** the owner clarified that these six decisions were **substantively approved** earlier. This register records that approval history and preserves each decision's scope and caveats (next section). They are **not yet formally ratified**: ratification happens only when the owner explicitly approves a separate durable decision record listing each decision, its scope, rationale, caveats and status (see `README.md`). Until then, changes to this behavior require explicit owner approval. The restore-specific limitations found later (concurrent writers, an unopenable active
database, stale `.restoring-*` files) were never part of these six and are separately deferred.

## Recorded scope and unverified caveats (Step 5.4)

The owner's prompts in the Step 5.4 verification sessions list the six decisions in two phrasings; both are recorded.
They cite design documents dated by filename 2026-10-25 and 2026-10-29, which are unrecoverable, so nothing below
can be checked against an original. Where scope cannot be verified from surviving records it is flagged, not guessed.

| # | Wording in the owner's prompts | Caveat or unverified scope |
|---|---|---|
| 1 | "External rollback anchor remains deferred." / "No external rollback anchor in this step." | Scoped to this step. Whether the deferral has a target step or expiry is not recorded. |
| 2 | "Restore validates SQLite integrity and root-key binding without claiming full decryption of every secret version." / "Validate SQLite integrity and root-key binding; do not claim every secret version is decrypted." | The exact set of checks the original approved is not recoverable. The code also validates schema version and the audit chain (see the table above). |
| 3 | "CLI names and flags remain `dso central backup --out`, `dso central restore --from`, `--dry-run`, and the approved explicit confirmation/`--force` behavior." / "...`--force` with the approved confirmation behavior." | The exact approved confirmation behavior (prompt wording, defaults, non-interactive rules) is defined only in the unrecoverable documents. Current code (a y/N prompt; `--force` skips it) is not verified to equal what was approved. |
| 4 | "Automatic backup pruning remains absent." / "Do not implement automatic backup pruning." | Scope is *automatic* pruning. Manual retention is left to the operator per a code comment; safety snapshots and backups accumulate. |
| 5 | "Caller-#1-only restrictions remain documented policy intentions, not falsely claimed as enforced authorization." / "Do not claim caller-#1-only authorization is technically enforced." | The term "Caller-#1" is not defined in any surviving record (it belongs to the unrecoverable authentication/authorization design). Its meaning **cannot be verified** here and is not guessed. |
| 6 | "Step 5.3 audit-retention decisions have not been silently reopened or changed." / "Do not silently change the existing audit-retention decision." | The content of the Step 5.3 audit-retention decision is not recorded in any surviving document. The code shows append-only triggers (a Gate 5 comment) and no retention logic; whether that is the decision's content is unverified. |

## Cross-references to the authorization and audit drafts (no decision changed)

These do not ratify, amend or reject any item above; they record dependencies only.

- Decision 3 (`backup --out`, `restore --from`) and decision 5 (caller restrictions are intentions, not enforced):
  if a service is ever built, caller-supplied paths become a confused-deputy risk (authorization draft, section 4,
  path 5), and `backup` and `audit verify` are direct-file commands (paths 2 and 3). The current
  behavior is unchanged and unaffected by this note.
- Decision 1 (no external rollback anchor): restoring an older valid backup drops newer events from the active
  chain and the chain does not record which candidate was restored (authorization draft path 6; audit draft
  section 4c). Nothing here proposes an anchor.
- Decision 4 (no automatic pruning): safety snapshots and backups accumulate and each holds the full database and
  history (authorization draft, section 4, path 8).
- Audit-policy questions on backup, restore failure, dry run and wrong-key attempts are tracked in
  `2026-10-30-central-audit-failure-per-operation-DRAFT.md`, section 6; they are separate from these six items.
- Step 5.5 destructive operations depend on the authorization architecture (undecided) and on whether events gain
  an actor or identifiers (a schema-version question, not proposed).

## Step 5.5: lifecycle questions (unresolved; no schema is proposed)

Current facts: projects have an id, a unique slug, a wrapped project DEK and `dek_version`; environments have an id,
a project id and a slug unique within the project, with no DEK of their own; secrets already soft-delete; there is
no archive, rename or delete for projects or environments. Encryption contexts bind **ids**, not slugs
(`crypto.SecretAAD`, `crypto.DEKAAD`); audit events carry **slugs only**.

| Question | Open choices | Notes (not decisions) |
|---|---|---|
| Soft delete / archive versus hard purge | Archive only for now; or also purge | `0001_init.sql` says purge needs its own explicit, audited migration |
| Slug mutability | Immutable; or mutable with immutable ids | Rename does not touch ciphertext, but audit history would become ambiguous unless events gain ids (a schema version bump) |
| Key scope | Project DEK only; or per-environment DEKs | Per-environment keys need a schema change and a `DEKAAD` change |
| Audit identifiers and rename compatibility | Slugs only; or add project/environment ids | New event schema version and migration required |
| Restore of a backup older than a lifecycle change | Define whether archive/rename state reverts | An older backup silently reverts such state; the same concern applies to a quarantine flag if one is ever stored in the database |
| Authorization of destructive lifecycle operations | Depends on the authorization architecture | Cannot be finalized before that decision |
| Compatibility with local mode | Undetermined | `docs/design/central-server-design.md` plans a shared `dso secret` grammar; local-mode integration was not audited |

Suggested non-goals for a first version (a proposal for the owner): hard purge, per-environment keys, moving secrets
between environments.

## Owner decisions required

Formal ratification of each Step 5.4 item (through the separate decision record); each Step 5.5 row above; and the order in which Step 5.5 follows the
authorization decision.
