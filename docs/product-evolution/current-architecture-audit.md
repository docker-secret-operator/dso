# Current Architecture Audit

**Date:** 2026-11-07
**Branch / HEAD:** `feature/webui-phase1` @ `5bcb8bb`
**Method:** read-only static review of source on disk. No code, test, config or schema was modified. `go build`, `go test` and the web test suite were **not executed** in this pass (to leave the working tree and system paths such as `/var/lib/dso` untouched). Every finding below is either *traced in code* (file:line given) or explicitly labelled *inferred* / *needs test*. A passing unit-test suite would not by itself prove any of the runtime behaviour discussed here.
**Finding IDs:** `PE-xx` (product-evolution). They are referenced with the same IDs in every other document in `docs/product-evolution/`.

---

## 1. Repository state at time of review

- `git status`: uncommitted modifications to `internal/cli/central.go`, `internal/store/sqlite/centrallock.go`, `internal/store/sqlite/centrallock_crossprocess_test.go`, `internal/store/sqlite/preserve_test.go`, `.claude-flow/*`; untracked central-store recovery files and docs (`internal/centralsvc/restore_recovery*.go`, `internal/store/sqlite/recovery_scan*.go`, `docs/central-store-*.md`, `docs/reviews/2026-10-2*-*.md`). **Left untouched.**
- `git diff --stat`: 14 files, +1821/−67 (of which 1392 lines are `.claude-flow/logs/daemon.log`).
- 269 commits total; 55 since 2026-10-01. Of those, the 23 most recent (2026-10-29 → 2026-11-06) are all `dso central` work (backup/restore/audit chain/locking/displacement).
- Code size (non-test Go): `internal/centralsvc` 3,914 lines + `internal/store` 6,498 lines = **10,412 lines** for the central store, versus `internal/rotation` 1,344 + `internal/watcher` 2,545 + `internal/agent` 3,711 + `internal/injector` 419 + `internal/proxy` 991 = **9,010 lines** for the entire runtime delivery/rotation path.
- Test files: 261 `_test.go` files outside `web/`.

## 2. Implemented vs partial vs planned

| Capability | Status | Evidence |
|---|---|---|
| Provider plugins: AWS SM, Azure KV, Vault KV, Huawei CSMS | **Implemented** (separate go-plugin binaries, real SDK calls) | `cmd/plugins/dso-provider-*/main.go`; loader `pkg/provider/load.go:272-356` |
| Local encrypted vault (local mode) | **Implemented** | `pkg/vault/vault.go`, `pkg/vault/crypto.go` |
| Provider polling + change detection | **Implemented** (content hash, in-memory baseline) | `internal/agent/trigger.go:404-487`, `:257-285` |
| Inbound change webhook (HMAC, replay guard) | **Implemented** | `internal/server/webhook_auth.go:27-206` |
| Rotation strategies `rolling`, `restart`, `signal`, `none`, `auto` | **Implemented** with gaps PE-01..PE-09 | `internal/watcher/controller.go:754-1611`, `internal/rotation/rolling_strategy.go:185-440` |
| In-process TCP proxy for host-port handoff | **Implemented**, in the agent process (PE-10) | `internal/proxy/*`, wired `internal/cli/agent.go:130-144` |
| Crash recovery of orphaned shadow/backup containers | **Implemented** | `internal/agent/recovery.go`, `trigger.go:161-191` |
| Prometheus metrics | **Implemented** on a separate localhost listener | `pkg/observability/metrics.go:122-160`, `internal/cli/agent.go:224` |
| Readiness endpoint reflecting provider/Docker state | **Missing** (`/health` is static) | `pkg/observability/metrics.go:149-153` (PE-26) |
| Web UI (read-mostly, opt-in) | **Implemented** | `internal/server/rest.go`, `internal/webuiauth/*`, `web/` |
| Event Reactor ("Tier 2", local mode) | **Stub** — callback only logs | `internal/agent/agent.go:309-328` (PE-18) |
| SmartPoller adaptive polling | **Not on any production path** (self-documented) | `docs/SMART_POLLING.md:1-25` |
| Docker V2 secret-driver socket | **Likely non-functional** — not verified | `internal/agent/server.go:557-560` (PE-19) |
| `dso central` store (versions, envelope encryption, audit chain, backup/restore) | **Experimental**, not wired into delivery, no authn/authz/network API | `docs/central-store-m1.md:3-7`; no `centralsvc` import outside central packages |
| Dynamic secrets, RBAC, multi-host, K8s | **Not implemented / out of scope** | `ROADMAP.md:188-195` |
| Integration tests against real Docker in CI | **Not run in CI** (`-short` skips them) | `.github/workflows/ci.yml:62`; 7 of 8 files in `test/integration/` call `t.Skip` under `testing.Short()` (PE-16) |

---

## A. Secret providers

**Interface.** `pkg/api/plugin.go:19-33`: `Init(map[string]string)`, `GetSecret(name) (map[string]string, error)`, `WatchSecret(...)`, optional `GetSecretWithContext`. There is **no version identifier, no metadata (created/rotated-at, version stage), no batch fetch, and no capability negotiation** (PE-14). Consequences: change detection must hash full content (`trigger.go:259`), DSO cannot record "which version is running", and providers that expose cheap version metadata (AWS `DescribeSecret` VersionIdsToStages, Vault KV v2 metadata, Azure version id) cannot be polled cheaply.

**Process isolation.** Each provider is a separate binary started by `hashicorp/go-plugin` with an allow-listed environment (`pkg/provider/load.go:151-168`; verified-good F-04 from the prior audit). Binary must be in a fixed directory set (`load.go:38-62`) and match a SHA-256 manifest (`load.go:173-262`). The manifest lives next to the plugins, so it gives **integrity against corruption, not authenticity against someone who can write the plugin directory** (PE-20). Release signing (cosign) covers the download, not the installed manifest.

**Connection management.** `internal/providers/store.go:40-141`: cached per provider *name*, reconnect if `LastHealthy` > 10 min or plugin process exited, init retries with exponential backoff + crypto jitter. The cache key is the name only — the config passed on a later call is ignored if an entry exists (relevant to PE-11).

**Timeouts.** The polling loop calls `GetSecretWithContext(t.ctx, ...)` with the agent's root context and **no per-request deadline** (`trigger.go:449-453`). The RPC layer only forwards a deadline when the context has one (`pkg/provider/provider.go:62-95`). A hung provider call therefore stalls that secret's polling goroutine until the SDK's own transport timeouts fire (PE-13). The IPC fetch path does use a 30s timeout (`internal/agent/server.go:279`).

**Per provider:**
- **AWS** (`cmd/plugins/dso-provider-aws/main.go`): default credential chain (good, supports IAM role). Rejects empty/`{}` payloads (`:94-105`, the P-03 guard). Calls `DescribeSecret` on every fetch to merge tags (`:116-119`) — doubles API calls per poll (PE-25). Binary secrets unsupported (`:81-86`). `WatchSecret` ignores ctx for the fetch itself (`:158`) — unused by the agent, which polls via `GetSecret*`.
- **Azure** (`dso-provider-azure/main.go:22-44`): `DefaultAzureCredential` (managed identity etc.). Good.
- **Vault** (`dso-provider-vault/main.go:27-60`): **static token only** — no AppRole, JWT/OIDC, Kubernetes auth, token renewal or lease handling; enforces HTTPS for non-loopback (`:33-39`, good). KV v2 version pin via `?version=` (`:84-115`). **Doc bug:** `README.md:332-339` puts `address`/`token` under `auth.params`, which `internal/providers/store.go:99-103` forwards as `auth_param_address`/`auth_param_token`; the plugin reads `config["address"]`/`config["token"]` (`:28`, `:41`), so the README example would fail with "vault token is required" (static trace, not executed). `examples/hashicorp-vault/dso.yaml:7-9` uses the working `config:` form but with `http://example.com:8200`, which `requireSecureVaultAddr` rejects. (PE-15)
- **Huawei** (`dso-provider-huawei/main.go:48-67`): AK/SK from config or env, or metadata service.
- **Missing providers relevant to the strategy:** Doppler, Infisical, GCP Secret Manager, 1Password Connect, Bitwarden Secrets Manager.

**Extensibility verdict:** the plugin process boundary is sound and worth keeping. The *interface* is too thin for version-aware rotation and must grow (ADR-005), in a backward-compatible way (optional interface, like `SecretProviderWithContext` already is).

## B. Secret injection engine

Three delivery mechanisms exist:

1. **Env injection** — values merged into a recreated container's `Config.Env` (`internal/rotation/container_clone.go:113-142`; restart path `internal/watcher/controller.go:1243+`). Visible in `docker inspect` by design and documented as such (`README.md:221-226`). Stale-value stripping for managed names is implemented (SEC-P0, `container_clone.go:117-127`) — good.
2. **File injection via `docker exec` + stdin** (`internal/injector/inject.go:40-96`) — used by local mode (`internal/agent/agent.go:216-228`). Value never in argv (base64 over stdin), file name regex-restricted against shell injection (`inject.go:21`, `:48-50`), atomic `mv`, `chmod 0400`. Requires `/bin/sh` and `base64` inside the image (`inject.go:55-60`) — **distroless/scratch images cannot use file mode** (PE-17). Injection happens on the Docker `start` event, i.e. *after* the entrypoint has started (`agent.go:183-206`); correctness depends on an in-container wait loop (PE-17).
3. **File injection via `CopyToContainer` tar stream before start** (`internal/rotation/tar_streamer.go:28-63`), used by the rolling path (`rolling_strategy.go:250-259`) and restart path. Docker documents that `docker cp` "isn't possible ... tmpfs, and mounts created by the user in the container" [X1 in competitive-analysis]. DSO's generated compose mounts a tmpfs at the secret path (`internal/core/compose.go:258`, `:291-292`). Therefore, for a created-not-started shadow, the archive is written into the **container's writable layer on host disk**, and once the container starts the tmpfs is mounted over it, hiding the file — so either the secret persists on host disk under `/var/lib/docker`, or the post-start probe (`rolling_strategy.go:290-299`) fails and rotation rolls back. **Inferred from Docker's documented behaviour; no test in the repo exercises tmpfs + rolling file secrets. Severity High until disproved** (PE-08). The doc comment on `StreamSecretToContainer` ("uploads secrets into a container's tmpfs mount") contradicts the comment on `InjectFiles` (`inject.go:24-26`), which correctly says CopyToContainer cannot reach tmpfs.

**Leakage review:** no secret values in process args or logs on the traced paths (prior F-05 holds for `trigger.go` and `controller.go` log calls read this pass). Env mode leaks to `docker inspect` (documented). Local mode temp compose file (`internal/cli/up.go:285-370`, accepted F-01). PE-08 is the significant undocumented leakage candidate.

## C. Rotation engine

### C.1 Lifecycle as implemented

```
StartPolling (per provider×secret goroutine, trigger.go:404)
  └─ GetSecretWithContext(t.ctx)            no deadline (PE-13)
  └─ ExecuteRotation (trigger.go:257)
       ├─ hash compare vs in-memory secretHashes      (PE-03, PE-14)
       ├─ 30s cooldown (returns without storing hash)
       ├─ store new hash + cache value BEFORE outcome is known
       └─ goroutine:
            LockManager.AcquireLock(secret, 5s) ──fail──> return (hash already stored → change lost) (PE-01)
            StateTracker.StartRotation
            Reloader.TriggerReload(ctx, secret, onComplete)   returns early
            defer ReleaseLock  ← runs when TriggerReload returns, not when async work ends (PE-02)
TriggerReload (controller.go:754)
  for each target container using the secret:
     service lock in rotationLocks (in-memory, 5-min stale reclaim)  (PE-23)
        busy → "skipping", target not rotated, no error recorded       (PE-01)
     strategy: auto→analyzer | signal | rolling (async) | restart (async, compose-deduped)
  onComplete(firstErr) after asyncWG.Wait()   → nil when nothing ran ⇒ RotationSucceeded
RollingStrategy.Execute (rolling_strategy.go:185)
  inspect → clone config (env, volumes, mounts, host ports, aliases verbatim)
  create shadow → stream file secrets (PE-08) → start → WaitHealthy (PE-09)
  → file probe → OnReady proxy SwapBackend → rename old→backup, new→original (non-cancellable)
  → verify names → drain delay (proxy only) → stop/remove old with retry
  failure (proxy-owned) → keep old, mark degraded
  failure (not proxy-owned) → executeSimpleRestart fallback = downtime (PE-06)
```

The *inner* swap (`RollingStrategy.Execute`) is careful and was verified fail-safe in the prior register (P-02, `docs/reviews/2026-10-24-phase0-phase1-security-findings-register.md:63-79`). That verdict stands **for its scope**. The new findings below are in the *orchestration around it* (trigger engine and reloader), which P-02 did not cover.

### C.2 Findings

| ID | Severity | Finding | Evidence | Status |
|---|---|---|---|---|
| **PE-01** | **High** | **Lost update and false success.** The new hash is stored before rotation runs (`trigger.go:284`). If the secret-level lock cannot be acquired in 5s (`:339-342`) the goroutine returns without deleting the hash, so the changed value is never applied until the provider changes again. Separately, if a service's in-memory rotation lock is busy, `TriggerReload` skips that target with only a debug log (`controller.go:808-816`); if no async job was started, `onComplete(nil)` fires (`controller.go:1594-1610`) and the trigger records `CompleteRotation` + `RotationSucceeded` (`trigger.go:382-387`) although nothing rotated. | Traced in code | Needs a regression test to confirm end-to-end |
| **PE-02** | Medium | The secret-level `LockManager` lock is released by `defer` when `TriggerReload` returns (`trigger.go:343`, `:390`), while rolling/restart work continues in detached goroutines (`controller.go:1079`, `:1189`). The lock therefore serialises *dispatch*, not *rotation*. Only the in-memory per-service map provides real exclusion. | Traced | — |
| **PE-03** | **High** | **Every agent start re-rotates every rotation-enabled secret.** The baseline hash map is in memory only (`trigger.go:29`); the first poll fires immediately (`:425-428`) with `oldHash == ""`, so `ExecuteRotation` proceeds; targets are populated before polling starts so the rotation hits real containers (`internal/cli/agent.go:227-236`). With the code default strategy `restart` (PE-04) this means an agent restart or upgrade recreates every managed container. | Traced | Needs a test; partly by design ("catches rotations that happened while the agent was down", `trigger.go:425-427`), but there is no record of what is actually running to compare against |
| **PE-04** | Medium | Default strategy in code is `restart` (`pkg/config/config.go:431-432`, `trigger.go:324-326`); `README.md:571` and `:34` present `rolling` as the default and `docs`/examples imply zero-downtime by default. | Traced | Doc/behaviour mismatch |
| **PE-05** | **High** | **Explicit `rolling` on a stateful service runs two instances against the same volumes.** The stateful guard exists only in the `auto` decision path (`internal/strategy/decision_engine.go`, `controller.go:825-834`). `PrepareShadowConfig` copies `Volumes` and `Mounts` verbatim (`container_clone.go:69-86`). `README.md:381-392` shows `mysql:8` with `dso.update.strategy: "rolling"`. Most databases will refuse (lock file) → health fails → rollback; some will not, risking corruption. | Traced; data-integrity impact inferred | Needs integration test per common DB image |
| **PE-06** | Medium | Zero-downtime is conditional on host ports being DSO-proxy-owned. The shadow inherits `HostConfig` including `PortBindings` (`container_clone.go:79`), so a service with fixed host ports and no `dso.host_ports` label fails to start, and the non-proxy fallback is `executeSimpleRestart` (`controller.go:1153-1163`) — a stop/recreate with downtime. | Traced | — |
| **PE-07** | Medium | Shadow joins the same networks with the same aliases (`container_clone.go:159-172`). Docker's embedded DNS can return the shadow's IP for the service name to *other* containers while the shadow is still in its health window. The proxy-pending marker only protects host-port traffic, not east-west traffic. | Inferred from Docker DNS behaviour | Needs test |
| **PE-08** | **High** | File-mode secrets delivered by `CopyToContainer` before start cannot reach tmpfs (Docker docs); they land in the writable layer or are hidden by the tmpfs mount. Contradicts the "never written to host disk" claim for the rotation path. | Inferred (code + Docker docs) | Needs integration test; treat as open |
| **PE-09** | Medium | Without a Docker `HEALTHCHECK`, the health gate passes after two "running" polls ~2s apart (`internal/rotation/health_check.go:50-56`). No TCP/HTTP readiness probe, and nothing checks that the new credential actually works. | Traced | — |
| **PE-23** | Low-Med | Per-service lock is reclaimed after 5 minutes (`controller.go:808-813`) regardless of whether the holder is still running; a long health timeout plus drain can produce overlapping rotations of one service. | Traced | — |

### C.3 Is zero-downtime guaranteed?

**No. It is conditional.** It holds only when **all** of the following are true:
1. Strategy resolves to `rolling` (not the code default — PE-04).
2. The service is stateless or tolerates two concurrent instances on its volumes (PE-05).
3. All host-published ports are declared in `dso.host_ports` and owned by the proxy (PE-06), and the agent process stays up for the whole window (PE-10).
4. The image defines a meaningful `HEALTHCHECK` (otherwise the gate is ~2s of "running", PE-09).
5. Peers that reach the service over the Compose network tolerate briefly resolving to a not-yet-ready instance (PE-07), or the app is fast to become ready.
6. Secrets are env-mode, or file-mode delivery is fixed (PE-08).
7. The upstream credential overlap is handled by the *provider* (old and new credentials both valid during the swap). DSO does not coordinate this; with single-credential rotation (Infisical calls this "single-phase" and warns about interruptions — competitive-analysis I1) the old containers break as soon as the provider rotates, before DSO even polls.

**Compose limitations to state explicitly in user docs:** container renaming leaves Compose's `com.docker.compose.*` labels (including `config-hash` and container number) on the new container unchanged, so a later `docker compose up` may see drift or recreate it with the compose-file env; `depends_on` ordering is not consulted during rotation; named volumes are shared by both instances; host-port binding is exclusive per host; there is no load balancer other than DSO's own proxy; stateful services (DBs, queues, anything holding a file lock) need `restart` with a maintenance window.

## D. Security architecture

| Area | State | Evidence |
|---|---|---|
| Agent identity / privilege | Runs as `User=root` with `NoNewPrivileges`, `PrivateTmp`, `ProtectSystem=strict` | `internal/bootstrap/systemd.go:41-64` |
| IPC socket authn | `SO_PEERCRED`; allow root, agent UID, or `dso` group (primary or supplementary); fail-closed on unknown NSS users | `internal/agent/peercred.go`, `server.go:476-505` |
| IPC authz | **All-or-nothing.** Any authorised peer can fetch any secret (`server.go:225-300`) and can make the root agent spawn a provider with caller-supplied type/config (`CheckProviderConnectivity`, `server.go:204-223`; legacy fallback `:257-266`). Because the provider cache is keyed by name and ignores later config (`internal/providers/store.go:40-68`), a caller-supplied config registered under a not-yet-cached name is reused by later lookups of that name. Possible consequences: provider-config poisoning (secrets sourced from an attacker-controlled Vault address) and SSRF from the agent host. | **PE-11, High — traced; exploitability depends on name collision timing; needs a test** |
| Config file perms | Bootstrap installs `/etc/dso/dso.yaml` `0640 root:dso` (`internal/bootstrap/permissions.go:288`); loader is advisory-only (P-01 accepted). `README.md:417-418` and `:431` tell operators the file is `0664` and group-editable — group members editing a root agent's config is a privilege boundary the README invites operators to open. | **PE-12, Medium (doc)** |
| REST API | Loopback default; refuses public bind without `DSO_AUTH_TOKEN` (prior F-02); Host-header guard, origin check, rate limits, body cap | `internal/server/rest.go`, `auth_gate.go` |
| WebUI sessions | HttpOnly/SameSite=Strict/Secure cookies, bcrypt, idle+absolute caps, login limiter | `internal/webuiauth/session.go`, `loginlimiter.go` |
| CSRF | Origin allow-list is the real control; the dead client CSRF token flagged in the prior audit is **gone** (`web/lib/csrf.ts` absent) | `internal/server/auth_gate.go:75-87` |
| Inbound webhook | HMAC-SHA256 over timestamp+body, tolerance window, bounded replay table that fails closed when full; legacy bearer mode warned | `internal/server/webhook_auth.go` (**PE-24, verified good**) |
| Plugin verification | Path allow-list + SHA-256 manifest beside plugins; `DSO_PLUGIN_SKIP_HASH_VERIFY` escape hatch | `pkg/provider/load.go:28-62`, `:238-262` (PE-20) |
| Encryption at rest | Local vault AES-GCM; central store envelope encryption with AAD binding (prior F-06 verified) | `pkg/vault/crypto.go`, `internal/crypto/envelope.go` |
| Audit | Rotation/fetch events (metadata only); central hash chain (not on delivery path) | `internal/audit/audit.go`; `internal/store/sqlite/audit*.go` |
| Supply chain | cosign keyless, syft SBOM, gosec + blocking govulncheck with allow-list | `.github/workflows/release.yml:42-57`, `ci.yml:129-185` |
| Backup/restore | Central store only (in-progress uncommitted work) | `internal/centralsvc/restore*.go` |
| Dead code | `newSystemDoctorCmd` still unreachable | `internal/cli/utils.go:148-155` (**PE-22**) |

### D.1 Requested re-examination of F-E1, F-H1, AF-2, AF-7, PA-8

These identifiers **do not appear anywhere in the repository**: not in any `.md` or `.go` file, not in any branch's history (`git log --all -S'F-E1'`, `-S'AF-7'`, `-S'PA-8'` return nothing), and not in the project auto-memory directory. Documents that do exist use other schemes (`F-01..F-28`, `P-01..P-05`, `V-*`, `R-1..R-7`, `B-1`, `SEC-*`, `REVEAL-B`, …).

| ID | Status | Reason |
|---|---|---|
| F-E1 | **Unlocated — not resolved** | No source found; cannot confirm, refute or update |
| F-H1 | **Unlocated — not resolved** | Same |
| AF-2 | **Unlocated — not resolved** | Same |
| AF-7 | **Unlocated — not resolved** | Same |
| PA-8 | **Unlocated — not resolved** | Same |

**None of them is marked resolved.** The owner needs to supply the source document (it may live outside this repository or in a prior conversation). Until then, `security-improvement-plan.md` tracks them as open placeholders. The closest code-grounded open items found this pass are PE-11 (IPC authz), PE-08 (file-secret persistence), PE-01/PE-03 (rotation correctness) and PE-20 (plugin authenticity); these are **not** asserted to be the same findings.

## E. Operational architecture

| Area | State | Evidence / finding |
|---|---|---|
| Logging | zap structured logs; some paths still use `log.Printf` with emoji (local-mode agent) | `internal/agent/agent.go:106-153` |
| Metrics | Prometheus, localhost-only listener | `pkg/observability/metrics.go:122-160` |
| Health / readiness | `/health` always returns `{"status":"healthy"}`; no readiness that reflects provider health, Docker connectivity, degraded services or pending rotations | **PE-26** |
| Supervision | systemd `Restart=on-failure` | `internal/bootstrap/systemd.go:54` |
| Data plane coupling | The TCP proxy runs inside the agent; agent crash/restart/upgrade drops every proxied listener and connection, and PE-03 then rotates everything on start | **PE-10, High** (`internal/cli/agent.go:130-144`) |
| Docker daemon outage | Event stream reconnect with backoff; local-mode agent exits after 20 consecutive failures (`agent.go:93-140`); docs say agent terminates after 100 attempts (`docs/OPERATIONAL_LIMITATIONS.md:83`) — inconsistent numbers between two runtimes | PE-18 |
| Provider outage | Fail-safe: no rotation on fetch error, no stale/empty injection (prior P-03 verified); no per-call deadline (PE-13) | `trigger.go:439-461` |
| Crash mid-rotation | Orphan cleanup + name restoration at start (`internal/agent/recovery.go`); rotation state file best-effort (`trigger.go:119-125` continues without it) | — |
| Two runtimes | `dso up` (local) uses `agent.Agent` + `injector.InjectFiles` + stub reactor; `dso agent` uses `TriggerEngine` + `ReloaderController`. Different injection mechanisms, different reconnect policies, different docs | **PE-18, Medium (maintenance)** |
| Secret-driver socket | Serves `POST /SecretDriver.Get` (`server.go:557-560`); swarmkit's secret driver protocol uses `/SecretProvider.GetSecret` and is Swarm-managed (search result, not independently verified). Likely dead | **PE-19, Low — needs verification** |
| CI realism | Real-Docker integration tests skipped by `-short`; single distro (`ubuntu-latest`) | **PE-16, High** for any production-readiness claim |

## 3. Consolidated finding register

| ID | Sev | Area | One-line summary |
|---|---|---|---|
| PE-01 | High | Rotation | Changed value lost / false "succeeded" when locks busy |
| PE-02 | Med | Rotation | Secret lock released before async rotation finishes |
| PE-03 | High | Rotation | Agent start re-rotates every managed container |
| PE-04 | Med | Docs/Rotation | Code default `restart` vs documented `rolling` |
| PE-05 | High | Rotation | Explicit `rolling` on stateful service shares volumes |
| PE-06 | Med | Rotation | Fixed host ports without proxy → fallback restart downtime |
| PE-07 | Med | Rotation | Shadow reachable via network alias before healthy (inferred) |
| PE-08 | High | Injection | Pre-start CopyToContainer cannot reach tmpfs (inferred) |
| PE-09 | Med | Rotation | No-HEALTHCHECK gate ≈ 2s running; no readiness/credential check |
| PE-10 | High | Ops | Proxy (data plane) lives in agent process |
| PE-11 | High | Security | IPC all-or-nothing; caller-supplied provider config cached by name |
| PE-12 | Med | Docs/Security | README says dso.yaml is 0664 group-editable |
| PE-13 | Med | Providers | Polling fetch has no deadline |
| PE-14 | Med | Providers | No version/metadata in provider interface; no applied-version record |
| PE-15 | Med | Providers | Vault static-token only; README/example Vault configs broken |
| PE-16 | High | Testing | Real-Docker tests never run in CI |
| PE-17 | Med | Injection | exec file injection needs sh/base64 and races app start |
| PE-18 | Med | Architecture | Two runtimes; stub reactor; "<5s detection" overclaim (`README.md:32`) |
| PE-19 | Low | Architecture | Secret-driver endpoint likely dead (unverified) |
| PE-20 | Low-Med | Supply chain | Plugin manifest = integrity, not authenticity |
| PE-21 | Strategic | Product | 10.4k lines of central store not on delivery path; absorbing all recent effort |
| PE-22 | Low | CLI | Dead `system doctor` command persists |
| PE-23 | Low-Med | Rotation | 5-min stale service-lock reclaim can overlap rotations |
| PE-24 | Info (+) | Security | Webhook HMAC/replay design verified good |
| PE-25 | Low | Providers | AWS DescribeSecret on every poll |
| PE-26 | Med | Ops | No real readiness endpoint |
| PE-27 | Info | Security | Agent is root + Docker socket (documented trust boundary) |

## 4. What this audit did not verify

- No tests executed; PE-01, PE-03, PE-05, PE-07, PE-08, PE-11, PE-19 require integration tests against a real Docker daemon to confirm.
- Proxy internals (`internal/proxy/router.go`) not line-traced beyond `SwapBackend` usage.
- Web UI page-level behaviour not re-audited (prior audit covered it).
- Uncommitted central-store recovery work was deliberately not reviewed in depth (in progress, out of scope, not to be touched).
