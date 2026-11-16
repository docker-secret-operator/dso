# 10 — Prioritized Gap Backlog

IDs `DSO-G001..` are consolidated here (the workstream-local G300-G307 items are folded in; see `workstreams/04`). Dependencies column lists security finding IDs. Priority: P0 release-blocking, P1 required for reliable production, P2 maturity, P3 optional/strategic. Effort is relative (XS<S<M<L<XL), not calendar time.

| Gap | Title | Category | Priority | Effort | Related findings | Components | Acceptance criteria | Release-blocking |
|---|---|---|---|---|---|---|---|---|
| DSO-G001 | Preserve compose bind IP/protocol in proxy rewrite | Security | P0 | M | S100 | internal/core/compose.go, internal/proxy | Proxy listener for 127.0.0.1:5432:5432 binds loopback; /udp not proxied as TCP (reject or support) | YES |
| DSO-G002 | Make signal strategy deliver new secret before SIGHUP and surface kill failures | Rotation | P0 | M | S101 | internal/watcher/controller.go | File secret re-injected before SIGHUP; kill failure marks rotation failed; test with fake docker client | YES |
| DSO-G003 | Fix crash-recovery ordering (inspect original before deleting _dso_* containers; never delete only running copy; split multi-replica IDs) | Reconciliation | P0 | L | S102,S103 | internal/agent/recovery.go, trigger.go | Fault-injection tests for crash after swap and between renames leave a running container and correct state | YES |
| DSO-G004 | Bump toolchain to go1.26.11 and keep govulncheck gate green | Security | P0 | XS | S300 | go.mod, .govulncheck-allowlist.txt | govulncheck exits 0 in CI | YES |
| DSO-G005 | Verify cosign signature in install.sh | Deployment | P0 | S | S301 | scripts/install.sh | Installer fails on tampered checksum/signature; test with bad artifact | YES |
| DSO-G006 | Add explicit --reveal gate to `dso secret get`; stop plaintext compose temp file in `up` local mode | Security | P1 | M | S400,S401 | internal/cli/secret.go, up.go | Default output masked; temp file replaced by stdin/tmpfs; regression tests | no |
| DSO-G007 | Atomic 0400 secret write (create with restrictive mode, cleanup on failure) | Security | P1 | S | S105 | internal/injector/inject.go | No file with broader mode ever exists; no .tmp left after failure | no |
| DSO-G008 | Fix exec health probe to wait for exec completion | Rotation | P1 | S | S104 | internal/rotation/health_check.go | Probe waits on ExecInspect; test where file absent fails swap | no |
| DSO-G009 | Introduce RBAC / scoped tokens and roles on REST | Security | P1 | L | S200 | internal/server, internal/auth | Read-only token cannot rotate/revoke; tests per route | no |
| DSO-G010 | Audit login/logout/session revoke/rotate with actor, IP, request ID; persistent audit store | Security | P1 | M | S201 | internal/server, internal/audit | Each event produces an audit record; export endpoint | no |
| DSO-G011 | Harden WebSocket (revalidate on logout/expiry), CSRF missing-header case, add CSP/HSTS, method enforcement | Backend API | P1 | M | S202-S205 | internal/server/rest.go, auth_gate.go | Tests for each case | no |
| DSO-G012 | Plugin verification: ownership/dir perms, single-open hash+exec | Security | P1 | M | S108 | pkg/provider/load.go | TOCTOU test cannot swap binary | no |
| DSO-G013 | Provider GetSecret retry/backoff/circuit breaker | Secret provider | P1 | M |  | internal/providers | Transient provider errors retried with jitter; outage tests | no |
| DSO-G014 | Persist change-detection hashes across agent restart | Rotation | P1 | S |  | internal/agent/trigger.go | No spurious rotation after restart | no |
| DSO-G015 | Real Docker integration tests (rotation, recovery, proxy) in isolated CI job | Testing | P1 | L |  | test/docker | dockerint suite runs in CI with rotation/crash scenarios | no |
| DSO-G016 | Tests for Azure and Huawei plugins; raise AWS/Vault plugin coverage | Testing | P1 | M |  | cmd/plugins | Per-plugin coverage >=60% with mocked SDK | no |
| DSO-G017 | Remove tracked binary and sqlite WAL/SHM; gitignore ruvector/.swarm/agentdb artifacts; add .dockerignore | Deployment | P1 | S | S305,S303 | repo root, Dockerfile | git ls-files clean; image context excludes local-secrets/data | no |
| DSO-G018 | Run systemd units non-root with hardening in all templates | Security | P2 | M | S302 | internal/bootstrap, internal/setup | systemd-analyze security score improved; all templates hardened | no |
| DSO-G019 | Pin CI actions by SHA, pin govulncheck, add dependabot, gate release on tests+vuln scan, sign/scan image | Deployment | P2 | M | S303,S304 | /.github | Release workflow blocked by failing tests/vulns | no |
| DSO-G020 | Fix `dso logs --api` flag binding | CLI | P2 | XS |  | internal/cli/logs.go | --api uses REST path; unit test | no |
| DSO-G021 | Add --json and distinct exit codes across commands; non-interactive setup | CLI | P2 | M |  | internal/cli | All status-like commands support --json | no |
| DSO-G022 | OpenAPI spec and contract tests for REST | Backend API | P2 | M |  | internal/server | Spec validated against handlers in CI | no |
| DSO-G023 | Metrics endpoint wired in REST/agent; rotation/reconcile/provider health metrics | Observability | P2 | M |  | pkg/observability | Prometheus scrape shows rotation and provider metrics | no |
| DSO-G024 | Dead-code/architecture disposition: decide fate of internal/ledger, internal/polling, pkg/backend, duplicate installers/injection paths (see `docs/design/g024-disposition.md`) | Architecture | P2 | M |  | multiple | Dead code removed or wired; one installer | no |
| DSO-G025 | Keyed (HMAC) audit chain and external export | Security | P2 | M | S111 | internal/store/sqlite/audit.go | Tamper of chain detected with key; SIEM export | no |
| DSO-G026 | Multi-host agent and HA design | Architecture | P3 | XL |  | new | Design doc + prototype | no |
| DSO-G027 | Encrypted vault product track (projects/envs/versioning/RBAC) per hybrid direction | Enterprise feature | P3 | XL |  | central store | Phased; see 11 | no |
| DSO-G028 | Fix umask-dependent test TestCentralLock_OpenBlockedWhileAnotherProcessHoldsExclusive | Testing | P2 | XS |  | internal/store/sqlite | Passes under umask 0002 | no |
