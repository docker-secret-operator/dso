# Draft decision record: central authentication and authorization

Status: **Draft; proposed direction for study; not ratified; the architecture is NOT decided.** Written
2026-10-30. On 2026-10-30 the owner confirmed: proceed with analysis and design of the key-holding service (ARCH-B) only. Architecture selection and implementation remain pending. This confirmation is recorded in working-session conversation, not in a repository decision record. This is a **new** draft record, not an
approval. The original Step 5.2
authentication/authorization design (`docs/reviews/2026-10-24-central-store-phase2-authn-authz-design.md`) could not
be recovered; its content is not reproduced or assumed here.

Reading guide: see `README.md` ("Reading guide and option labels"). Statements marked *verified* describe current
code; *proposed* items are recommendations; *owner-selected* means chosen in a working-session discussion with no
repository record; *ratified* means formally approved. Nothing in this directory is ratified.

## Current behavior (verified in code)

`dso central` performs **no authentication or authorization**. It opens the SQLite database and the root key directly
as the invoking OS user; file permissions are the only control. `--reveal` on `secret get` is a usability guard, not
authorization. The audit actor label (`actorName()`, `local-cli:<username>`) is self-asserted attribution. No
listener exists in the central code path. Peer-credential checks exist only for the agent daemon
(`internal/agent/peercred.go`, `peerAuthorized`) and are not used by `centralsvc`.

## Direction for study (owner-confirmed: analyze and design ARCH-B only; selection and implementation pending; not ratified)

The proposed next step is to produce a **design and threat model for a key-holding service with enforced authorization**, covering CLI and API
consequences, **before** any implementation. A socket alone is insufficient while the CLI can still read the
database and key directly. Studying or designing ARCH-B does not authorize implementing it.

Option labels in this record are the same as in the architecture draft: ARCH-A, ARCH-B and ARCH-C (defer) are defined
there. The "hybrid" idea below is a separate **policy** proposal, labeled POLICY-HYBRID, not a fourth architecture.

## Alternatives considered

| Option | Gain | Cost |
|---|---|---|
| ARCH-A. Direct local access with an explicit OS-user trust boundary | No new component; honest about the model | No protection against any process running as the same user |
| ARCH-B. Key-holding service with enforced authorization (**proposed for design study only; implementation not authorized**) | A real boundary | Large change: the CLI can no longer open the database or key; requires identity, authorization matrix, failure behavior |
| POLICY-HYBRID. Peer credentials plus per-operation policy (a policy layer, not an architecture; compare ARCH-C, which means deferring the decision) | Reuses the agent's pattern | Only meaningful with a key-holding process |

## Security limitations until a design is approved and built

Any process able to run as the owning user can reveal, disable, delete, back up and restore. Restore and backup have
no caller restrictions.

Where the no-authorization statement appears in the CLI (verified by inspection of `internal/cli/central.go`): the
`central` group help (`:53` `Short`, `:58` `Long`); the `restore` long help (`:737`, repeated in a code comment at `:716`), which says "no caller-specific authorization today"; and the `audit verify` long help (`:888-889`), which says only that the command "is not authentication or authorization". The `backup` command's help does **not** carry any such statement.

## Deferred work (not authorized by this record)

Implementation of a service, tokens, sessions, roles, quarantine and its release path (B-1), and authorization of
project/environment lifecycle operations.

## Code and tests

- `internal/cli/central.go`: `actorName`, command help text; `internal/centralsvc/service.go`
- Related drafts (not approved): `2026-10-30-central-authz-architecture-threat-model-DRAFT.md`
- `internal/agent/peercred.go`, `internal/agent/server.go` (agent daemon, for comparison only);
  tests `internal/agent/peercred_test.go`, `server_audit_test.go`

## Superseded or related drafts

`docs/design/m2-identity-authorization-audit-design.md` (operators, sessions, tokens, `Principal`/`Handle`) and
`docs/design/m2-audit-resolution-matrix.md` (incl. blocked item B-1) are unreviewed drafts; the M2 plan states M2.2
onward is not authorized. They are **not** adopted.

## Unresolved

The identity model, whether the CLI is retained as a client of the service, the operation-level authorization matrix,
and the quarantine release path.
