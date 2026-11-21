# M4 verification record (Docker-free)

**M4 IMPLEMENTATION CHECKPOINT COMPLETE — PRODUCTION QUALIFICATION DEFERRED. Not production-ready.**

Branch `feature/m4-development` (local only, not pushed or merged). Verified implementation commit: `bfd238aa13a62ba5cd587d2aed8b7f9f828f6011`, tree `808c621190d5e369443e9a8e2d3315256342d7d0`, `git archive` sha256 `e33c0b23f1f3070da996e495d4edf199373ece0929df2ca21d8cdfd4f83b32d3`. The freeze commit is the documentation-only commit that adds this record on top of it (a document cannot contain the hash of the commit that contains it); `git diff bfd238a <freeze>` touches only `docs/`. Parent checkpoint: M3 (`docs/milestones/m3-verification-record.md`, implementation `6ec71f7`, freeze record `08aff5f`), unchanged.

Scope: operator diagnostics and usability (`docs/milestones/m4-scope-inventory.md`): `dso audit verify` and `dso audit export` for the durable audit file, doctor check `DSO-DOCTOR-032`, `GET /api/audit/status` with the Web UI audit status, and the audit and secret-delivery troubleshooting guide. M3 decisions D1, D2 and D4 are unchanged (`docs/design/m3-control-plane-decisions.md`).

## Verification of bfd238a (final)
- Evidence (outside the repo, retained): `m0-tools/evidence/verify-bfd238a/`.
- 45 steps, the same step set as the M3 freeze run; 42 exit 0 (build, vet for all tag sets, short and short+dsofault suites, agent race under three umasks, full-tree race 383 s, coverage, frontend, shell, embedded-asset and CLI-reference drift, installer, guard, self-test, docs-reference, gosec). Coverage 76.4% against a 60% floor.
- Scanner baselines, recorded and not accepted or suppressed: govulncheck exit 3 and OSV-scanner exit 1 (advisory IDs identical to the M3 freeze run), gitleaks exit 1 (49 findings, same count), trivy 5 findings (same).
- Toolchain: Go 1.26.11, Node v24.22.3, npm 11.19.1, ShellCheck 0.11.0.

## Earlier runs (retained, not the verifying run)
- `verify-a8f1286-m4`, `verify-1c215e6-m4b`: passed on earlier M4 commits (42 steps); superseded because later commits changed code (`dso audit export`).
- `verify-436b3a9-run1-race-flake`: 41 of 45 steps passed; the full-tree race step failed on one test, `internal/watcher` `TestReconcileRuntimeState_UsesSingleBatchedList`. Cause: the test raced the bounded asynchronous N19 gate pass (`RequestGateReconcile`, a separate `dso.gate` list); load-sensitive test flake, no production defect (watcher code unchanged since M2; 0 of 600 failures at idle, about 6 of 3000 under CPU load, 0 of 3000 after the fix). Fixed in `bfd238a` (test-only), then the whole run was repeated on `bfd238a`.

## Known limitations
- The durable audit file is best effort and an unkeyed hash chain (tamper-evident only); `dso audit export` and `GET /api/audit/export` verify before writing but do not prove authenticity or completeness.
- `pkg/vault` `TestConcurrentWriters_DistinctKeys_AllPreserved` remains a known load-sensitive test (M3 record).
- The bearer token is read at process start; replacing it requires a restart.
- Secret-delivery diagnostics beyond `dso status`, `dso doctor` and the troubleshooting guide are not built.

## Not built (need owner decisions)
Read-only/operate credential split (D1), per-person tokens (D4), keyed audit chain (G025), proxy lifecycle (PE-10, ADR-006).

## Deferred, not executed
Real-Docker N19 qualification and integration tests, crash harness, live systemd, hosted CI, keyless signing. N7/N8 credential verification: owner action. Docker advisories and production release: owner decision. The N19 production-release blocker remains open.
