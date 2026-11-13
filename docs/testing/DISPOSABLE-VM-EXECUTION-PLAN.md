# Disposable-VM execution plan (M0 runtime verification)

Status: **prepared, not executed.** No isolated rootful Docker environment exists yet; nothing below has been run. The development machine and its Docker daemon are never used.

Single entry point, run inside the VM from a clean checkout of the commit under test:

```bash
./scripts/integration/run-all.sh
```

It refuses to run unless the VM was provisioned by `bootstrap-test-vm.sh` (root-owned marker), no `DOCKER_*` variable is set, and the tree is clean. It writes `artifacts/m0-<time>-<sha>/summary.txt` and per-step logs, and reports `INCOMPLETE` while any step is still `PENDING`.

## Environment
Ubuntu 24.04 x86_64, 4 vCPU, 8-16 GB RAM, 80 GB disk, rootful Docker Engine + Compose v2 (apt-pinned version, key fingerprint verified by the bootstrap), Go from the official tarball with its SHA-256 verified, cosign from a verified release (installer tests need it). No production credentials, workloads or public application ports. Rootless Docker may be added later as an extra compatibility run only.

## Steps and the evidence each must produce
| # | Step | Evidence required | Implemented in `run-all.sh` |
|---|---|---|---|
| 1 | Bootstrap + independent identity | `/etc/dso-integration/environment` root-owned; daemon label and ID recorded | `bootstrap-test-vm.sh` (not run) |
| 2 | Daemon verification | the guard refuses a daemon without the provisioned label/ID or with foreign containers | guard unit tests pass; VM run pending |
| 3 | Source sync | commit SHA recorded, tree clean | checked |
| 4 | Clean-commit verification | build, vet, `-short` tests exit 0 inside the VM | yes |
| 5 | Real image build | `docker build` exits 0 | yes |
| 6 | Image-content inspection | no `.git`, `data/`, `*.db`, keys in layers; non-root; plugin hash manifest present | **PENDING** |
| 7 | Secret injection into a real tmpfs | `/run/secrets/dso` is tmpfs; file 0400, owner = app UID | guarded tests exist (signal), broader suite pending |
| 8 | Non-root application access | app (uid 10001) reads the delivered file | `test/docker/signal_rotation_test.go` |
| 9 | Health verification | HEALTHCHECK vs process-only evidence reported | pending |
| 10 | Network bind/protocol | loopback-only service unreachable from a second network namespace/host; UDP not proxied; rolling rejected for direct ports | **PENDING** |
| 11 | Actual Compose deployment | `compose up` via stdin: project name, relative paths, `.env`, profiles, cancellation, failure; no temp compose file left | **PENDING** |
| 12 | Signal rotation | app loads new value; ignored-signal case leaves old value; env-mode rejected | `test/docker/signal_rotation_test.go` (not run) |
| 13 | Restart rotation | downtime window measured and reported (not zero downtime) | pending |
| 14 | Rolling rotation | proxy swap; candidate readiness failure; handoff failure | legacy `test/integration` suites need migration |
| 15 | Continuous traffic | request count, successes, failures, resets, timeouts, status distribution, latency, backend identity, phase timings, across rotation and recovery; new connections vs long-lived connections reported separately | **PENDING** (migrated `proxy_rotation_downtime_test.go` needed) |
| 16 | Real agent termination + recovery | an actual DSO agent process is killed at controlled transitions (test-only fault hook, never a production control), a fresh agent recovers, ownership protected, idempotent | **PENDING** |
| 17 | Ownership protection | unmanaged and foreign-owned containers untouched under every scenario | guarded tests partly exist |
| 18 | Cleanup | only resources labelled with the run's environment and run IDs removed; no prune | `cleanup-integration.sh` |
| 19 | Evidence collection | `summary.txt`, logs, environment, commit SHA | yes |

## What this plan does not claim
Passing the VM run would be the first real-Docker evidence for M0-02/03/04/07/08/09/11/13. It would not establish zero downtime beyond the tested conditions, survival of long-lived connections (tested separately or not at all), behaviour under host power loss, or production readiness.
