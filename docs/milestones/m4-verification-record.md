# M4 verification record (Docker-free)

**M4 DOCKER-FREE VERIFICATION PASSED for commit `1c215e6054232a841e054487b027bfa5326a36df` (tree `f4558c88420b84157459bdca759448c5d6306ce2`), branch `feature/m4-development`. PRODUCTION QUALIFICATION DEFERRED. Not production-ready.** Commits after it that touch only `docs/milestones/` are this record.

Earlier run `verify-a8f1286-m4` (commit `a8f1286`, same result: 42 steps, 76.3%) predates a small behaviour-preserving refactor of agent audit startup (`startDurableAudit`) with a new test; the whole run was repeated on `1c215e6` instead of reusing it.

Scope: operator diagnostics and usability (`docs/milestones/m4-scope-inventory.md`): `dso audit verify`, doctor check `DSO-DOCTOR-032`, `GET /api/audit/status` with the Web UI audit status, the audit and delivery troubleshooting doc, and a tested best-effort startup path for the audit file.

- Evidence (outside the repo, retained): `m0-tools/evidence/verify-1c215e6-m4b/`.
- 42 steps exit 0 (build, vet for all tag sets, race 396 s, coverage, frontend, shell, drift incl. embedded Web UI assets and CLI reference, installer, guard, self-test, docs-reference, gosec). Coverage 76.3% against a 60% floor.
- Scanner baselines, recorded and not accepted or suppressed: govulncheck exit 3, OSV-scanner exit 1, gitleaks exit 1.
- Toolchain: Go 1.26.11, Node v24.22.3, npm 11.19.1, ShellCheck 0.11.0.

Not built (need owner decisions): read-only/operate credential split (D1), per-person tokens (D4), keyed audit chain (G025), proxy lifecycle (PE-10, ADR-006). The durable audit file remains best effort and unkeyed.

Deferred, not executed: real-Docker N19 qualification and integration tests, crash harness, live systemd, hosted CI, keyless signing. N7/N8 credential verification: owner action. Docker advisories and production release: owner decision. N19 production-release blocker remains open.
