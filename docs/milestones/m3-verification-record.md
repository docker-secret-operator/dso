# M3 verification record (Docker-free)

**M3 IMPLEMENTATION CHECKPOINT COMPLETE — PRODUCTION QUALIFICATION DEFERRED. Not production-ready.**

Branch `feature/m3-development`. Verified implementation commit: `6ec71f74ccae4b5b2e9388e0436755c2d1d493df`, tree `de103d45cb50426c0de20254dbfe572426e285b6`. The freeze commit is the documentation-only commit that adds this record on top of it (a document cannot contain the hash of the commit that contains it); `git diff 6ec71f7 <freeze>` touches only `docs/`.

Scope: control-plane security and operator experience on the existing single-operator REST API and Web UI. Decisions and limitations: `docs/design/m3-control-plane-decisions.md`.

## Verification of 6ec71f7 (final)
- Evidence (outside the repo, retained): `m0-tools/evidence/verify-6ec71f7-m3d/`.
- 42 steps exit 0 (build, vet for all tag sets, race 381 s, coverage, frontend, shell, drift, installer, guard, self-test, docs-reference, gosec). Coverage 76.3% against a 60% floor.
- Scanner baselines, recorded and not accepted or suppressed: govulncheck exit 3, OSV-scanner exit 1, gitleaks exit 1.
- Toolchain: Go 1.26.11, Node v24.22.3, npm 11.19.1, ShellCheck 0.11.0.

## Earlier runs (retained)
- `verify-89dd73e-m3`: interrupted after 6 steps by a wrapper fault; not a result.
- `verify-89dd73e-m3b`: gosec G117 false positive on the audit record's secret-name field (annotated in `0bd01ac`); `pkg/vault` `TestConcurrentWriters_DistinctKeys_AllPreserved` timed out under load (known load-sensitive test).
- `verify-0bd01ac-m3c`: passed (42 steps, 76.3%). `6ec71f7` then changed production code (audit append rollback on failed writes, a failure counter metric), so the whole run was repeated on `6ec71f7` rather than reusing this one.
- An unidentified "exited with code 1" printed by one early targeted test run was not traced; it predates the passing runs and no evidence suggests an unresolved defect.


## Not built (owner decisions or stop conditions)
Read-only/operate credential split (D1), per-person tokens (D4). The durable audit file is an unkeyed hash chain (tamper-evident only; keyed chain G025 not implemented); D1, D4 deferred and D2 conditionally accepted by the owner (see the decisions doc); G025 deferred.

## Deferred, not executed
Real-Docker N19 qualification, real-Docker integration tests, crash harness, live systemd, hosted CI, keyless signing. N7/N8 credential verification: owner action. Docker advisories and production release: owner decision. N19 production-release blocker remains open.
