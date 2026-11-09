# Security Improvement Plan

**Date:** 2026-11-07
**Scope:** runtime delivery/rotation path, agent IPC, providers, supply chain. `dso central` is out of scope here (frozen per ADR-002; its own design/decision records under `docs/reviews/2026-10-2*` and `docs/decisions/` remain authoritative for it).
**Finding IDs:** `PE-xx` from `current-architecture-audit.md`. Backlog IDs `BL-xx` from `implementation-backlog.md`.
**Rule followed:** no finding is marked resolved without current source/test evidence. Nothing in this plan has been implemented.

---

## 1. Status of previously named findings

| ID | Source | Status on 2026-11-07 | Evidence |
|---|---|---|---|
| **F-E1** | Not found | **Unlocated — open, not resolved** | No match in any repo file, any branch history (`git log --all -S`), or project memory |
| **F-H1** | Not found | **Unlocated — open, not resolved** | Same |
| **AF-2** | Not found | **Unlocated — open, not resolved** | Same |
| **AF-7** | Not found | **Unlocated — open, not resolved** | Same |
| **PA-8** | Not found | **Unlocated — open, not resolved** | Same |
| P-01 | Phase 0/1 register | Accepted risk (advisory-only `dso.yaml` perms); policy acceptance still pending owner confirmation per register | `pkg/config/permissions.go:23-38`; `docs/reviews/2026-10-24-phase0-phase1-security-findings-register.md:149` |
| P-02 | Register | Verified safe **for `RollingStrategy.Execute`'s scope**. Orchestration-level gaps PE-01/PE-02/PE-23 are new and outside that verdict | `rolling_strategy.go:185-440` |
| P-03 | Register | Verified safe (no stale/empty injection on provider error). PE-13 (missing deadline) is an availability gap, not a reversal | `trigger.go:439-461` |
| P-04 | Register | Scope correction stands (no Docker-API mediation layer exists) | `internal/proxy/*` |
| P-05 | Register | Codecov fork-PR guard in place; live fork-PR run still unverified | `.github/workflows/coverage.yml` |
| Dead CSRF token (prior audit DSO-P2-001) | Prior audit | **No longer present** — `web/lib/csrf.ts` does not exist; Origin check is the CSRF control | `internal/server/auth_gate.go:75-87` |
| Dead `system doctor` (DSO-P2-002) | Prior audit | **Still present** (PE-22) | `internal/cli/utils.go:148-155` |

**Owner action required:** provide the document that defines F-E1, F-H1, AF-2, AF-7 and PA-8. Once located, each must be re-checked against current code and recorded here with evidence.

## 2. Threat summary for the target product

Assets: secret values in memory, tmpfs and container env; provider credentials in `dso.yaml`; the Docker socket (root-equivalent); the integrity of *which* value a container runs.
Actors: local non-root users in the `dso` group; local users outside it; containers on the host; network attackers reaching REST/webhook; malicious or compromised provider plugin binary; compromised upstream secret manager account (out of DSO's control).

## 3. Prioritised security work

Each item: evidence → problem → solution → benefit → complexity → risk → acceptance criteria.

### S-1 (High) IPC authorization tiers and removal of caller-supplied provider config — PE-11, ADR-007, BL-06

- **Evidence:** `internal/agent/server.go:204-223` (`CheckProviderConnectivity` builds a `ProviderConfig` from request fields and calls `Store.GetProvider`); `:257-266` (legacy one-off provider from `req.Config`); `internal/providers/store.go:40-68` (cache keyed by name, later config ignored); peer authz `internal/agent/peercred.go` admits any `dso` group member.
- **Problem:** any `dso` group member can (a) read every secret value, (b) cause the root agent to start a provider with arbitrary config — SSRF from the host, and, if it lands under a name the agent has not yet cached (or after the failure threshold evicts it, `store.go:154-170`), later polls for that name use the caller's config.
- **Solution:** (1) never cache a provider created from IPC input: connectivity checks use a throw-away client that is killed after the check; (2) key the cache by `name + sha256(canonical config)`; (3) delete the legacy one-off provider path or restrict it to root; (4) tiered authz: `status`/`trigger` for dso group, secret-value reads restricted to root or a configured per-project group, connectivity checks root-only.
- **Benefit:** the `dso` group stops being equivalent to "all secrets + provider redirection".
- **Complexity:** Medium (≈1 week incl. tests). **Risk:** breaks `docker dso apply` pre-flight for non-root users and local-mode `fetch` flows → keep behaviour behind a config flag for one minor release with a deprecation warning.
- **Acceptance:** unit tests prove (a) a connectivity check never inserts into the store, (b) two configs with the same name yield different cache entries, (c) a non-root dso-group peer receives `permission denied` for `GetSecret` unless allowed by config; an integration test with a fake Vault address shows no subsequent poll reaches it.

### S-2 (High) File-mode secret persistence in the rotation path — PE-08, BL-04

- **Evidence:** `internal/rotation/tar_streamer.go:28-63` (CopyToContainer before start), `rolling_strategy.go:250-259`; tmpfs mount generated at the same path `internal/core/compose.go:258,291-292`; Docker docs: copying into tmpfs is not possible (competitive-analysis X1).
- **Problem:** secret may be written to the container writable layer on host disk, or hidden by the tmpfs → contradicts the documented guarantee, or breaks rotation.
- **Solution:** first, an integration test that proves the actual behaviour. Then deliver file secrets **after start** into the tmpfs via the existing exec/stdin injector (`internal/injector/inject.go`), gated before traffic switch; for images without a shell, offer a documented alternative (env-mode, or a tiny static `dso-secret-writer` helper copied into the image) rather than silently falling back.
- **Benefit:** restores "never on host disk" for file mode.
- **Complexity:** Medium (≈1 week). **Risk:** app may read the file before it exists → readiness probe and file probe must both pass before SWITCHING.
- **Acceptance:** integration test on Linux: after a rolling rotation, `find /var/lib/docker/overlay2 -path '*run/secrets*'` (or the container's `UpperDir` from inspect) contains no secret file; the app container reads the new value from tmpfs.

### S-3 (High) Rotation correctness as a security property — PE-01, PE-03, BL-02, BL-03

- **Problem:** a rotation triggered by credential *revocation* that is silently dropped (PE-01) leaves a compromised credential in service while the system reports success. PE-03 causes mass restarts on agent restart, which operators will "fix" by disabling rotation.
- **Solution / acceptance:** see BL-02/BL-03; security acceptance is that `RotationSucceeded` is only emitted when the ledger shows `applied_version == desired_version` for every targeted container, and that a revocation scenario test (change value while a rotation is in flight) ends with all containers on the latest version.
- **Complexity:** Medium-High. **Risk:** behavioural change in event semantics → document in CHANGELOG.

### S-4 (Medium) Config file permission documentation — PE-12, BL-12

- **Evidence:** `README.md:417-418,431` (0664, "read and edit") vs `internal/bootstrap/permissions.go:288` (0640).
- **Solution:** correct README; state that editing `dso.yaml` is a root-equivalent action because the root agent acts on it.
- **Complexity:** Trivial. **Acceptance:** README and `docs/security-hardening.md` state 0640 root:dso; `dso doctor` warns on group-write (it already warns on mode drift, `permissions.go:398-471`).

### S-5 (Medium) Vault authentication — PE-15, BL-10

- **Evidence:** `cmd/plugins/dso-provider-vault/main.go:41-60` (static token only).
- **Solution:** AppRole and JWT/OIDC auth with token renewal/re-login; reject `token` in plaintext config when `auth.method` is set; fix README/example.
- **Complexity:** Medium. **Acceptance:** plugin test against a dev Vault (container) for AppRole login and renewal across token TTL expiry.

### S-6 (Medium) Plugin authenticity — PE-20, BL-15

- **Evidence:** manifest co-located with binaries (`pkg/provider/load.go:248-262`).
- **Solution:** verify plugin binaries against the cosign bundle shipped with the release (or a manifest signed by the release key) at install time and on agent start; keep SHA-256 as a fast check.
- **Complexity:** Medium. **Risk:** offline installs → support a pinned public key bundled in the main binary. **Acceptance:** tampering with a plugin *and* its manifest causes load failure.

### S-7 (Medium) Per-call deadlines on provider requests — PE-13, BL-05

- **Solution:** `context.WithTimeout(t.ctx, provider.timeout)` (default 30s) around every poll fetch.
- **Acceptance:** test with a blocking fake provider shows the loop recovers within timeout + backoff.

### S-8 (Medium) Stateful-workload guard applies to explicit strategies — PE-05, BL-07

- **Solution:** `Assess()` (target-architecture §8) runs for every strategy; explicit `rolling` on a stateful service is BLOCKED unless `dso.rotation.allow_shared_volumes: "true"` is set.
- **Acceptance:** integration tests with `postgres`, `mysql`, `redis` images confirm blocked by default.

### S-9 (Low) Remove dead/unused attack surface — PE-19, PE-22, PE-18, BL-16

- Secret-driver socket (after verification), `system doctor`, SmartPoller, stub reactor callback.
- **Acceptance:** `go build`, `go vet`, tests pass; socket no longer created.

### S-10 (Low) Pin GitHub Actions to SHAs — from register P-05 note

- `actions/checkout@v4` etc. → full SHAs with Dependabot updates.

## 4. Security testing requirements (gate for any "production-ready" claim)

| Test | Pass criterion |
|---|---|
| Secret leakage scan after each integration scenario | No secret value in: agent logs, `journalctl`, `docker inspect` for file-mode secrets, `/proc/*/cmdline`, host overlay dirs, `/tmp` after `dso up` exits normally |
| IPC authz matrix | Every RPC method × {root, agent UID, dso member, other user} matches the table in ADR-007 |
| Webhook | Replay, stale timestamp, wrong signature, table-full all rejected (existing tests; keep in CI) |
| Provider poisoning | S-1 tests |
| Fuzz | `go test -fuzz` on webhook signature parsing, label parsers (`dso.host_ports`, `dso.readiness`), config loader |
| Race | `go test -race` on trigger/controller/proxy packages, including the new queue |
| Supply chain | govulncheck blocking (exists), SBOM attached (exists), plugin signature verification (S-6) |

## 5. Things this plan deliberately does not do

- Does not change authentication defaults, config formats or schemas now; all proposals are additive and flag-guarded until approved.
- Does not add RBAC, SSO or multi-user identity (ADR-010).
- Does not attempt to defend against a root or Docker-group attacker on the host (documented trust boundary, PE-27).
