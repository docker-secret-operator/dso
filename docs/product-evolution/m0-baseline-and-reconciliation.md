# M0 — Baseline, Historical Finding Reconciliation, and New Findings

**Date:** 2026-11-08
**Milestone:** M0 (investigation and plans only — no production code, test, schema or workflow changed)
**Inputs:** the 8 documents in `docs/product-evolution/` (2026-11-07). Finding IDs `PE-xx` continue that register; this pass adds `PE-28`..`PE-34`.
**Evidence labels used in every M0 document:**
- **Confirmed (code)** — traced in source with file:line.
- **Confirmed (probe)** — reproduced by an M0 diagnostic unit probe (see §4). Probes ran at unit level, not against a live agent or Docker daemon.
- **Inferred (needs test)** — follows from code plus documented Docker/OS behaviour, not executed.
- **Unverified assumption** — stated, not checked.

---

## 1. Repository baseline (read-only)

| Check | Result |
|---|---|
| Branch / HEAD | `feature/webui-phase1` @ `5bcb8bb` (unchanged from the 2026-11-07 pass) |
| `git log --oneline -20` | Top 20 commits are all `dso central` / sqlite / restore work (`5bcb8bb` … `62ab4b2`) |
| Uncommitted, modified | `internal/cli/central.go`, `internal/store/sqlite/centrallock.go`, `internal/store/sqlite/centrallock_crossprocess_test.go`, `internal/store/sqlite/preserve_test.go`, `.claude-flow/*` (10 files) |
| Untracked | `internal/centralsvc/restore_recovery{,_test}.go`, `internal/cli/central_recovery_test.go`, `internal/store/sqlite/{recovery_scan,recovery_scan_test,sharedlock_test}.go`, `docs/central-store-{audit-log,backup-restore,crash-recovery}.md`, 10 × `docs/reviews/2026-10-2*` files, `docs/product-evolution/`, `docs/product-evolution.zip` |
| `git diff --stat` | 14 files, +1987 / −67 (the growth vs. 2026-11-07 is `.claude-flow/logs/daemon.log`, written by an external daemon, not by this pass) |
| `docs/product-evolution/` | All 8 prior files present; `docs/product-evolution.zip` contains the same 8 files |
| Staged changes | None |

**Untouched by M0:** no file above was edited, staged, stashed, reset, cleaned or committed. One harmless difference from the session-start snapshot: `.claude-flow/daemon.pid` changed from "deleted" to "modified" — the external claude-flow daemon rewrote it; M0 did not touch `.claude-flow/`.

**Files M0 added (new, untracked):** the seven `docs/product-evolution/m0-*.md` documents. **No test file was added to the repository** (see §4).

## 2. Historical finding reconciliation: F-E1, F-H1, AF-2, AF-7, PA-8

Search performed: `grep -rI` of the whole working tree (including untracked files, excluding `.git` and `node_modules`) for each literal and for spacing/hyphen variants; regex search for each *prefix scheme* (`PA-[0-9]+`, `AF-[0-9]+`, `F-[A-Z][0-9]+`); `git log --all -S` and `--grep` for each literal; `git log --all -G` for each scheme; the single stash (`stash@{0}`); `docs/product-evolution.zip`; the project auto-memory; and the claude-mem session-observation index for this project.

### 2.1 Result per ID

| ID | In repo / history? | Provenance found | Definition available? | Status |
|---|---|---|---|---|
| **F-E1** | No (only in `docs/product-evolution/*`, which copy the ID as "unlocated") | claude-mem observation #833 (session of 2026-11-07, "production security readiness audit", conversation-only, never written to a file) | **Yes (from that observation):** *High* — `auth.Authenticator.Verify` returns nil when no token is configured and the startup guard blocks only non-loopback binds, so the REST API on loopback is unauthenticated by default | **Located (conversation record) — code re-checked this pass: still true.** See §2.2 |
| **F-H1** | No | Same observation #833 | **Yes:** *Medium, low confidence* — independent reproducibility of release binaries was not verified despite cosign + syft in `release.yml` | **Located (conversation record) — not re-verified (needs a reproducible-build check); open** |
| **AF-2** | No. Sibling IDs **AF-1, AF-3, AF-4, AF-5, AF-6** exist as section labels in `internal/store/sqlite/displace_test.go:1159,1243,1402,1539,1800` (commit `12cfc54`) | claude-mem #701/#735: AF-2, AF-7, PA-5..PA-10 are items of the **`dso central` displacement/restore review series**; AF-2, AF-7, PA-8 were "flagged as requiring owner decisions" and explicitly not started | **No.** Only the series and status are known; the text of AF-2 was never persisted | **Unlocated definition — open, owner must supply source** |
| **AF-7** | No (same series; not in `displace_test.go`) | Same | **No** | **Unlocated definition — open, owner must supply source** |
| **PA-8** | No. Siblings **PA-1, PA-2, PA-4** exist in `internal/store/sqlite/displace_test.go:799,1070` | Same series (PA-5..PA-10 pending) | **No** | **Unlocated definition — open, owner must supply source** |

None of the five is marked resolved. No definition was invented.

### 2.2 Mapping against PE-xx

| ID | Overlap | Notes |
|---|---|---|
| F-E1 | **Adjacent to PE-11, not a duplicate.** PE-11 is the IPC Unix socket (gated to root/agent-UID/`dso` group). F-E1 is the REST API: in legacy mode (no `DSO_AUTH_TOKEN`, WebUI disabled) `authenticate` returns `authOpen` (`internal/server/auth_gate.go:46-65`, decision table `:37-45`), and the startup guard only refuses non-loopback binds (`internal/server/rest.go:1275-1316`; warning `internal/server/access_wiring.go:69`). Any local UID — wider than the `dso` group, and including containers using `network_mode: host` — can then call `POST /api/secrets/*/rotate` (`rest.go:245`) and read metadata routes. Secret-value exposure over REST was **not** re-traced this pass. | Recommend tracking F-E1 inside the M1 PE-11 work as "local authorization, REST leg" (see `m0-pe-11-analysis.md` §6). |
| F-H1 | Related to PE-20 (plugin authenticity) and S-6 only in theme (supply chain). Not a duplicate. | Stays open; low priority relative to M1. |
| F-D1 / F-E2 (same source, not in the owner's list) | F-D1 = PE-20 (duplicate). F-E2 ("no SO_PEERCRED found in internal/server") is **superseded**: SO_PEERCRED exists for the IPC socket (`internal/agent/server.go:479-509`, `internal/agent/peercred.go:59-87`); REST is a TCP listener where it does not apply — F-E1 covers the REST gap. | Informational. |
| AF-2, AF-7, PA-8 | **No overlap with PE-xx.** They belong to `dso central` (frozen by owner decision). | Because `dso central` is frozen, these do not block M1–M4; they block only any future work on central restore/displacement. |

**Owner action:** supply the document/conversation that defined the AF-/PA- displacement review series (AF-1..AF-7, PA-1..PA-10), so AF-2, AF-7 and PA-8 can be re-checked. Persist F-E1/F-H1 into `security-improvement-plan.md` §1 when that file is next edited (not done here — M0 does not modify the 8 prior files).

## 3. Corrections to the 2026-11-07 documents

| Item | Prior statement | Corrected (evidence) |
|---|---|---|
| **PE-16** | "Real-Docker tests never run in CI (`-short`)" | **Partly wrong.** `ci.yml:62` uses `-short`, but `.github/workflows/coverage.yml:30-31` runs `go test -race -covermode=atomic ./...` **without `-short`** on every push/PR to `main` on `ubuntu-latest` (Docker preinstalled). The run of 2026-08-14 (`gh run 31774710721`) shows `ok test/integration 76.329s`. So the existing `test/integration` suite (testcontainers + real Docker) *does* execute in CI. What remains true: it runs as a side effect of coverage (not a named, required integration gate), without `-v` (per-test skip/pass invisible), with the default 10-minute package timeout, and **none of its tests covers PE-01, PE-03, PE-05, PE-08, PE-11, agent restart, provider failure or daemon interruption.** Severity stays High for production claims; the fix is smaller than "build a Docker lane from zero". |
| **PE-08** consequence | "either the secret persists in the writable layer, or the post-start probe fails and rotation rolls back" | The probe is `test -s <fs.Path>` where `fs.Path` is the **mount directory** (`internal/rotation/health_check.go:88`, called with `fs.Path` at `rolling_strategy.go:291`). `test -s` on a non-empty-sized directory succeeds even when no secret file is visible, so a hidden file would most likely **not** trigger rollback. Inferred; see `m0-pe-08-evidence-plan.md` and PE-32. |
| PE-08 scope | tmpfs only | When the target service has **no** tmpfs at the path (any user-written Compose file in agent mode; `core/compose.go:257-258,291-292` adds tmpfs only in the generated local-mode compose), file secrets are written into the container's writable layer **by construction** — the existing test `test/integration/rotation_test.go:91-199` does exactly this (alpine, no tmpfs) and asserts only absence from `docker inspect`. |

## 4. M0 diagnostic probes (how "Confirmed (probe)" was obtained)

To avoid adding files to the repository, the probes were compiled with Go's build overlay: the test files live in the session scratchpad and are mapped into the package directories virtually.

```
go test -overlay overlay.json -count=1 -v -run M0Probe \
  ./internal/watcher/ ./internal/providers/ ./internal/agent/
```

| Probe | Package | Result |
|---|---|---|
| `TestM0Probe_PE01_BusyServiceReportsSuccess` | watcher | `onComplete(nil)` although 0 containers rotated (service lock `db` held by another project) |
| `TestM0Probe_PE01_LockTimeoutLosesUpdate` | agent | new hash retained after secret-lock timeout; next poll with same value = "No change detected" |
| `TestM0Probe_PE11_FileBackendPathIsCallerControlled` | providers | `file` backend read a file from a caller-chosen directory |
| `TestM0Probe_PE11_SecondLookupOfNativeBackendPanics` | providers | nil-pointer panic on second `GetProvider` for a native (`env`/`file`) backend |
| `TestM0Probe_PE11_CacheKeyIgnoresConfig` | providers | entry created from caller-supplied config occupies the provider name |

All five passed (i.e. each finding reproduced). `git status` afterwards showed no new repository files. Probe sources are reproduced in `m0-pe-01-analysis.md` Appendix A and `m0-pe-11-analysis.md` Appendix A so they can be adopted as real regression tests in M1 (owner approval needed to add test files).

## 5. New findings from this pass

| ID | Sev | Finding | Evidence | Label |
|---|---|---|---|---|
| **PE-28** | **Critical** (local privilege escalation) | IPC `GetSecret` "legacy one-off" path builds a provider from caller input when `req.Provider` is not a configured name (`internal/agent/server.go:257-264`). Types `file` and `env` are **in-process backends in the root agent** (`pkg/provider/load.go:274-287`). `file` takes its base directory from caller config (`pkg/backend/file/file.go` `Init`: `p.basePath = config["path"]`; `IsSafePath` only constrains to that caller-chosen base). ⇒ any `dso`-group member can read any root-readable file (e.g. `/etc/shadow`, `/etc/dso/dso.yaml` provider credentials) and, via `env`, any agent environment variable. | Code + probe (backend level) | IPC end-to-end not executed — needs integration test |
| **PE-29** | **High** (availability) | `SecretStoreManager` stores native backends with `Client == nil` (`load.go:283,286` → `store.go:131-137`); the cache-hit path calls `entry.Client.Exited()` (`store.go:58`), which dereferences nil (`go-plugin v1.7.0 client.go:478-481`). ⇒ (a) any configured `type: file`/`env` provider (documented in `docs/providers.md:171`, `docs/CONFIG_REFERENCE.md:37,345,364`) crashes the agent on its **second** poll; (b) any `dso`-group member can crash the agent with two IPC calls (`net/rpc` does not recover handler panics — no `recover()` in `$GOROOT/src/net/rpc/server.go`); (c) `Shutdown` calls `Client.Kill()` on nil (`store.go:201`). Combined with PE-03, every crash-restart re-rotates all containers. | Code + probe (store level) | End-to-end crash not executed |
| **PE-30** | Medium | Service rotation lock key is the bare Compose service name (`controller.go:799-805,808,819`), not project+service: rotating `db` in project A silently skips `db` in project B (and feeds PE-01's false success). | Code + probe | — |
| **PE-31** | Medium | `ExecuteRotation`'s hash check (`trigger.go:262-267`) and store (`:284`) are not atomic; the poll goroutine, `HandleWebhook` (`:489-525`) and IPC `TriggerReconciliation` (`server.go:147`) can call it concurrently for the same key ⇒ duplicate dispatches, or a stored hash that does not match the cached value that was dispatched. | Code | Inferred — needs `-race` stress test |
| **PE-32** | Medium | File-secret probe `test -s <dir>` is vacuous for directories (`health_check.go:88`) — does not prove the secret files exist. | Code | Inferred — part of the PE-08 test |
| **PE-33** | Low-Med | Restart path: file-secret injection failure is only logged and the container is started anyway (`controller.go:1393-1395`), then success is reported. | Code | — |
| **PE-34** | Low-Med | When `sec.Provider` is empty and several providers exist, the controller picks the provider by Go map iteration order (`controller.go:851-856`, `:936-939`), while `StartAll` polls such a secret under **every** provider (`trigger.go:232-237`) ⇒ nondeterministic choice of which provider's cached value is injected. | Code | Inferred impact |
