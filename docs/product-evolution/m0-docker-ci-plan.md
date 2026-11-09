# M0 — Docker Integration CI: Implementation Plan (proposal)

**Date:** 2026-11-08 · **Status:** PROPOSAL. No workflow file was created or run. **Creating or running this workflow requires separate owner approval** (`m0-owner-approval-checklist.md`, item M0-3).

## 1. Current state (verified)

| Workflow | Trigger | Docker-backed tests? | Evidence |
|---|---|---|---|
| `ci.yml` → `build` | push/PR to `main` | No — `go test -race -short` (`ci.yml:62`) | file |
| `ci.yml` → `stability` | push to `main` only | No Docker; runs `internal/testing` long tests (`ci.yml:66-89`) | file |
| `coverage.yml` | push/PR to `main` | **Yes, implicitly** — `go test -race -covermode=atomic ./...` without `-short` (`coverage.yml:30-31`), so `test/integration` (testcontainers + daemon socket) runs on `ubuntu-latest` | Run `31774710721` (2026-08-14): `ok test/integration 76.329s` |
| `release.yml` | tags | No tests against Docker | file |

Gaps: the Docker run is a side effect of coverage (not named, not required as a status check — branch protection settings **unverified**), not verbose, default 10-minute package timeout, no artefacts on failure, and no scenario for PE-01/03/05/08/11, agent restart, provider failure, daemon interruption. Branch `feature/webui-phase1` triggers neither workflow until a PR to `main` is opened. Action versions are tag-pinned, not SHA-pinned (BL-25).

## 2. Runner and Docker capabilities

- **Runner:** GitHub-hosted `ubuntu-24.04` (pin explicitly rather than `ubuntu-latest`). It is a full VM with Docker Engine + Compose v2 preinstalled, the runner user in the `docker` group, and passwordless `sudo`. That gives everything needed: real `dockerd`, overlay2, tmpfs, `systemctl stop docker` (daemon interruption), `useradd`/`groupadd` (IPC authz matrix), root reads of `/var/lib/docker` (PE-08).
- **No Docker-in-Docker, no `--privileged` service containers.** DinD would change exactly what we need to observe (storage driver, tmpfs/mount namespaces, daemon restarts) and requires privileged mode. Rejected.
- **Agent under test runs on the host as root** (`sudo ./docker-dso agent …`): the agent hardcodes `/var/lib/dso/state` and `/var/lib/dso/locks` (`internal/agent/trigger.go:119,129`) and is designed to run as root (`internal/bootstrap/systemd.go:41-64`). A later small seam (`DSO_STATE_DIR`) would let unit-level harnesses avoid root; not required for the lane.
- **Self-hosted runner:** not needed. Cross-distro (BL-24) stays a separate nightly matrix later.
- **Record versions** in the job summary (`docker version`, `docker info --format '{{.Driver}} {{.CgroupVersion}}'`, kernel) — PE-08 behaviour may depend on Engine version.

## 3. Proposed workflow shape (`.github/workflows/integration.yml`, not created)

```yaml
# PROPOSAL — not committed
name: Docker Integration
on:
  pull_request: { branches: [main] }
  push:         { branches: [main] }
  workflow_dispatch: {}
permissions: { contents: read }
concurrency: { group: docker-int-${{ github.ref }}, cancel-in-progress: true }
jobs:
  docker-int:
    runs-on: ubuntu-24.04
    timeout-minutes: 45
    strategy:
      fail-fast: false
      matrix:
        suite: [rotation, agent-lifecycle, security, disruptive]   # disruptive = daemon stop; runs last, own VM
    steps:
      - uses: actions/checkout@<sha>
      - uses: actions/setup-go@<sha>  (go-version-file: go.mod)
      - run: docker version && docker info
      - run: make build && go build -o bin/plugins/dso-provider-vault ./cmd/plugins/dso-provider-vault
      - run: ./test/docker/prepare.sh          # plugin dir + sha256 manifest, pull pinned images, create users for authz matrix
      - run: sudo -E go test -tags dockerint -v -count=1 -timeout 30m -run "Suite_${{ matrix.suite }}" ./test/docker/... 2>&1 | tee test.log
      - if: failure()
        run: ./test/docker/collect.sh          # docker ps -a, inspect, events, agent log, journal excerpt
      - if: always()
        run: ./test/docker/leak-scan.sh "$SENTINEL_PREFIX" test.log artifacts/   # fail if sentinel appears in logs
      - if: failure()
        uses: actions/upload-artifact@<sha>
```

Design rules:
- New tests behind a build tag (`//go:build dockerint`) in a new package (`test/docker/`) so `go test ./...` in `ci.yml`/`coverage.yml` is unaffected; existing `test/integration` stays where it is.
- **Deterministic waits only**: wait on Docker events (`/events` filtered by container label) and on agent state (IPC `GetStatus`, later ledger), with explicit deadlines — no fixed sleeps except where the test is about time (cooldown).
- **Synthetic secrets only**, random per run with a known prefix; the leak scan greps every artefact for it.
- **Provider under test:** `hashicorp/vault` in dev mode as a container (pinned digest), driven via its HTTP API to change values, return errors (`sys/seal`), or disappear (`docker pause`/`stop`). Uses the real Vault plugin path. (The in-process `file` provider cannot be used until PE-29 is fixed — it crashes the agent on the second poll.)
- **Compose fixtures** under `test/docker/fixtures/<scenario>/compose.yaml` with images pinned by digest.
- Required status check for `main` once green for ~2 weeks; quarantine list (`t.Skip` with issue link) for known-failing *finding* tests so the lane can be required while PE fixes land — failing-first tests are marked `expected-fail` and **flip to required** in the milestone that fixes them.

## 4. Scenario catalogue

| # | Scenario (owner's list) | Fixture / action | Assertions | Initially expected |
|---|---|---|---|---|
| S1 | **Compose service creation** | `docker compose up` stateless `nginx` + `dso.secrets` label; start agent | Agent adopts target (IPC `GetStatus`), no rotation on first sight once ledger exists | Pass (baseline) |
| S2 | **Secret injection (env)** | Vault value v1 → v2 | New container env has v2; old removed; `docker inspect` shows env (documented) | Pass |
| S3 | **Container rotation (rolling, restart, signal)** | same with each strategy | strategy-specific: rolling keeps name, new ID, 0 failed requests with proxy port (`proxy_rotation_downtime_test.go` harness); restart recreates; signal delivers SIGHUP (marker file written by trap) | Pass |
| S4 | **Agent restart (PE-03)** | record container IDs; `sudo kill -TERM` agent; restart with unchanged Vault | **0** containers recreated (compare IDs, Docker `create`/`destroy` events) | **Fail today** (confirms PE-03) |
| S5 | **Provider failures** | Vault sealed / paused / 500 / empty value | No rotation, no empty injection (P-03), agent keeps polling, recovers within backoff after unseal; with a hung provider the loop recovers within timeout (PE-13) | Pass for P-03; **fail** for hang (PE-13) |
| S6 | **Health-check failures + rollback** | image whose HEALTHCHECK fails with the new value | Old container keeps serving, names restored, no orphan after 60 s, `RotationFailed` emitted, service marked degraded; with no proxy, record whether implicit fallback restart happened (PE-06) | Pass with documented downtime on fallback |
| S7 | **Stateful service (PE-05)** | `postgres:16`, `mysql:8`, `redis:7` + named volume + explicit `rolling` | Today: record whether a second instance started on the same volume and data-dir integrity (`pg_controldata`, `mysqlcheck`); after M3: **no** second `create` event, downgrade/BLOCKED event | **Fail today** (confirms PE-05) |
| S8 | **Concurrent rotation (PE-01/PE-30/PE-31)** | slow-start service (HEALTHCHECK `start_period` 20 s); v1→v2, at +5 s v3; second project with same service name | All targets end on v3 within deadline; ≤2 rotations; no `RotationSucceeded` for 0-container dispatch; project B rotated | **Fail today** (confirms PE-01) |
| S9 | **File-mode secret persistence (PE-08)** | fixtures F1–F7 from `m0-pe-08-evidence-plan.md` | app view, mountinfo, UpperDir scan, `/var/lib/docker` scan, `docker diff`, probe honesty | Unknown — this is the evidence |
| S10 | **Docker daemon interruption** (`disruptive` suite) | `sudo systemctl stop docker.socket docker.service` mid-poll and mid-rotation; restart after 20 s | Agent does not crash-loop; no half-swapped names after daemon return (recovery runs); no rotation started while down; event stream reconnects | Unknown |
| S11 | **IPC authorization (PE-11/PE-28/PE-29)** | users `alice` (in `dso`), `mallory` (not); call each RPC via a tiny client | matrix per `m0-pe-11-analysis.md` §6; `alice` cannot read `/etc/shadow`/agent env; two `env` calls do not crash the agent | **Fail today** (confirms PE-28/29) |
| S12 | **Proxy lifecycle (PE-10)** — for M4, listed now | 50 rps through proxy port; restart agent | 0 failed requests (expected to fail until M4) | **Fail today** |

## 5. Cost and stability

- Estimated wall time: rotation ~8–12 min, agent-lifecycle ~6 min, security ~4 min, disruptive ~6 min (parallel jobs). Image pulls cached via pinned digests; consider `docker save` cache only if pulls become flaky.
- Flake controls: per-test unique Compose project names, `t.Cleanup` removing containers by label, `-count=1`, no shared ports (ephemeral host ports except proxy tests), event-driven waits.
- Disruptive suite must run on its own VM (matrix leg) because stopping `dockerd` affects everything on the runner.

## 6. Rollout steps (each needs approval at its milestone)

1. (M0-3 approval) Add `integration.yml` + `test/docker/` scaffold + S1–S3 baseline (all green) — proves the harness.
2. Add failing-first S4, S7, S8, S9, S11 as `expected-fail` with issue links (evidence for PE-01/03/05/08/11/28/29).
3. Make the lane a required check; flip each `expected-fail` to required as M1–M3 land.
4. Add `-v` and a JSON test report to `coverage.yml` or exclude `test/integration` from it to avoid running Docker tests twice (owner choice).

## 7. Status update — rollout step 1 implemented (M1 final increment, 2026-11-09)

`.github/workflows/integration.yml` and `test/docker/scenarios_test.go` now exist (section 3's "not created" note is historical). What they actually cover differs from the plan above in one important way, recorded here so the lane is not over-read.

**What the S1–S3 baseline exercises:** DSO's real rotation engine (`watcher.ReloaderController`: label discovery, the `dso.owner` ownership gate, the restart / rolling / signal strategies, the health gate, container rename/create/remove) running in-process against a real Docker Engine and real `docker compose`.

**What it does NOT exercise (synthetic cache seeding):** the secret value is written straight into the controller's cache interface by the test. The scenarios therefore do **not** cover the Vault provider plugin or any provider polling, the root `dso-agent` / `docker-dso agent` binary and its startup wiring, the Unix IPC socket, SO_PEERCRED peer-credential authorization, the REST API, or the proxy. The S1/S2/S3 "Expected outcome" column above (IPC `GetStatus`, Vault v1→v2, proxy 0-failed-requests) describes the planned end-to-end harness, not this baseline.

**Gating:** the package carries `//go:build dockerint`; without the tag `go list ./test/docker/...` matches no packages, so `go test ./...` in `ci.yml` / `coverage.yml` never compiles or runs it.

**Workflow fixes made while validating it:** the leak scan now also scans the uploaded `artifacts/` and never echoes matching lines (file name and count only); `cancel-in-progress` is limited to pull requests so pushes to `main` always get a completed run. Exit codes propagate (`set -o pipefail` on the `go test | tee` step, no `continue-on-error`, no `|| true` on the test step). Names are randomized per run (`uniqueName`: 4 random bytes) and cleanup is registered before resources are created (`compose down` per project, label-based removal for `docker run` workloads).

**Execution status:**

- Executed locally on 2026-11-09 against Docker Desktop 29.7.2 (linuxkit 7.0.12 kernel, overlayfs, cgroup v2, Compose v5.4.0) on macOS: `TestS1_ComposeServiceAdoption`, `TestS2_EnvSecretInjection`, `TestS3_RotationStrategies/{rolling,restart,signal}` all PASS; leak scan over the test log found 0 sentinel occurrences; no containers or networks left behind.
- **Not yet executed on GitHub Actions `ubuntu-24.04`.** The first real workflow run (open a PR to `main`, or `workflow_dispatch`) is still required before the lane can be called green on its target runner, and making it a required check remains a branch-protection action for a repository admin.

Reproduce locally (needs a reachable Docker daemon with Compose v2 and Go per `go.mod`):

```bash
docker pull busybox:1.36.1
export DSO_IT_SENTINEL="dso-it-local-$(date +%s)"
set -o pipefail
go test -tags dockerint -v -count=1 -timeout 20m ./test/docker/... 2>&1 | tee test.log
grep -cF -- "$DSO_IT_SENTINEL" test.log   # must print 0
```

## 8. Test environment policy

**Scope of the Docker lane.** The S1–S3 scenarios seed a synthetic secret value directly into the rotation engine's cache. That synthetic-provider Docker integration is **not** equivalent to full Vault / root-agent / provider-to-container validation, and a passing run must never be described as validating Vault, root-agent IPC, the Unix socket, or `SO_PEERCRED`.

### Linux with Docker

- The full Docker integration suite (`go test -tags dockerint ./test/docker/...`, plus the unit suite and `test/integration`) is expected to run.
- Linux is the authoritative environment for networking and container-lifecycle integration tests: a failure there is treated as a regression until shown otherwise.

### macOS with Docker Desktop

- Run the unit tests and the Docker integration scenarios that are compatible with Docker Desktop (S1–S3 have passed there).
- Known networking limitation: Docker Desktop runs containers in a VM, so container IP addresses are not routable from the host. Tests that dial a container IP directly — the proxy tests in `test/integration` are the known example — can fail for that reason alone.
- Do not silently skip or suppress such failures: they may hide a genuine regression. Report each one, name it as environment-specific with the reason, and confirm it on Linux before dismissing it.

### Hosted Linux GitHub Actions

- The required validation environment for the Docker CI lane (`.github/workflows/integration.yml`, `ubuntu-24.04`, host Docker).
- S1–S3 must be preserved; do not weaken, skip or remove a scenario to get a green run.
- When the lane runs, record the workflow run URL, the commit SHA, the job status and the per-scenario results.
