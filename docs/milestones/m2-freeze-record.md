# M2 freeze record

**M2 IMPLEMENTATION CHECKPOINT COMPLETE — PRODUCTION QUALIFICATION DEFERRED**

Not production-ready. Not merged to main. Not published. See `m2-checkpoint.md` for scope and `deferred-production-qualification.md` for the gates.

| Field | Value |
|---|---|
| Branch | `feature/m2-development` |
| Commit | `8b12a819b14ab9b730d6fba5a095f63b267e765e` |
| Tree | `4254b9eb53f0d7d01c96f800edecb5939bc80961` |
| Source archive | `git archive --format=tar 8b12a81`, sha256 `06573610231e8f49f061fe2f0ccca23643b98ed8e8959bd992d6888dabb68591` (recomputed after verification; deterministic for the commit) |
| Toolchain | Go 1.26.11, Node v24.22.3, npm 11.19.1, ShellCheck 0.11.0 |

## Verification (Docker-free, fourth full run, quiet machine)

- 42 steps exit 0, including build, vet, drift/installer/guard self-tests, frontend lint/tsc/test/build, shell and docs-reference checks.
- Race run exit 0. Coverage run exit 0, total 76.1% against a 60% floor.
- Evidence: `m0-tools/evidence/verify-8b12a81-run4-quiet/` (outside the repository, retained).
- Earlier runs failed for demonstrated reasons (two source defects fixed; one load-sensitive `pkg/vault` timeout under overlapping runs) and are retained as evidence.

## Scanner findings (recorded, not accepted or suppressed)

govulncheck exit 3, OSV-scanner exit 1, gitleaks exit 1. These are the known baselines: Docker module advisories and the N7/N8 credential findings. Nothing was suppressed or accepted.

## Not executed (deferred, not passed)

Real-Docker N19 qualification, real-Docker integration tests, the fault-injection crash harness, live systemd, hosted CI, keyless signing. Exploratory real-Docker attempts on a local test daemon were aborted by owner instruction and are not qualification evidence.

## Open blockers and owner items

- N19: IMPLEMENTED — REAL DOCKER QUALIFICATION DEFERRED. Production-release blocker remains open.
- N7/N8 credential verification: OWNER ACTION REQUIRED.
- Docker advisory acceptance, production release approval: OWNER DECISION REQUIRED.

## ADR-011 scope

Local-only encrypted central store. Deferred: HTTP API, RBAC, purge, root-key rotation, richer audit metadata, agent consumption of the central store.

## Accepted implementation limitations

The ledger contract is at-least-once with idempotent reconciliation. Legacy file-mode journal records are not delivery evidence. `internal/polling` is retained but not wired.

## Process notes

A usage-limit interruption caused loss of session visibility and a temporary documentation-ignore reversal, later corrected. A stash was involved in that period; stash `9bfcc225…` was verified intact with read-only git (6 entries). No further stash operations were performed.
