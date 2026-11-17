# M3 verification record (Docker-free)

**M3 DOCKER-FREE VERIFICATION PASSED for commit `0bd01ace0c925c1bd20aa8381a5e1fc85b23a260` (tree `ccc3da95b5144d7c020a710c8b359917b9b08f61`), branch `feature/m3-development`. PRODUCTION QUALIFICATION DEFERRED. Not production-ready.**

- Toolchain: Go 1.26.11, Node v24.22.3, npm 11.19.1, ShellCheck 0.11.0.
- Result: 42 steps exit 0 (build, vet for all tag sets, race, coverage, frontend, shell, drift, installer, guard, self-test, docs-reference, gosec). Race run 400 s, exit 0. Coverage 76.3% against a 60% floor.
- Scanner baselines, recorded and not accepted or suppressed: govulncheck exit 3, OSV-scanner exit 1, gitleaks exit 1.
- Evidence (outside the repo, retained): `m0-tools/evidence/verify-0bd01ac-m3c/`.
- Earlier runs, retained: `verify-89dd73e-m3` (interrupted after 6 steps by a wrapper fault; not a result); `verify-89dd73e-m3b` (gosec G117 false positive on the audit record's secret-name field, fixed in `0bd01ac`; `pkg/vault` `TestConcurrentWriters_DistinctKeys_AllPreserved` timed out under load, a known load-sensitive test in a package unchanged since M2, which passed on the quiet run).
- Commits after `0bd01ac` are documentation only.

## Not built (owner decisions or stop conditions)
Read-only/operate credential split (D1), per-person tokens (D4). The durable audit file is an unkeyed hash chain (tamper-evident only; keyed chain G025 not implemented); D2 was resolved locally by default, not owner-ratified.

## Deferred, not executed
Real-Docker N19 qualification, real-Docker integration tests, crash harness, live systemd, hosted CI, keyless signing. N7/N8 credential verification: owner action. Docker advisories and production release: owner decision. N19 production-release blocker remains open.
