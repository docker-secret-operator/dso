# Disposable-VM execution plan (M0 runtime verification)

Status: **runner implemented, not executed.** No isolated rootful Docker environment exists yet; nothing below has been run against Docker. The development machine and its Docker daemon are never used. What *has* been run (without any daemon): `bash -n` on every script and `scripts/integration/selftest.sh` (97 checks of the fail-closed logic, with a failing `docker` stub first in `PATH`).

Single entry point, run inside the VM from a clean checkout of the commit under test:

```bash
DSO_IT_DISPOSABLE_VM_CONFIRM=<environment id from /etc/dso-integration/environment> ./scripts/integration/run-all.sh
```

Exit codes: `0` PASS, `1` FAIL, `2` INCOMPLETE, `3` refused (nothing was run, no artifacts).

## Verdict rules (fail closed)
* **FAIL** - any step failed.
* **INCOMPLETE** - nothing failed, but a step was skipped, did not run, a required step is missing, or an `EXTERNAL_PREREQUISITE` is open.
* **PASS** - every required step passed, none skipped, no external prerequisite open.
* A required Docker scenario that matched **zero tests**, or whose tests were **all/any skipped**, or where a named required test did not pass, is never `passed` (classified from `go test -json`; see `m0_classify_go_json`). The package-level failure produced by the isolation guard (wrong daemon) is a failure, not a skip.
* Three steps are `EXTERNAL_PREREQUISITE` by nature (below), so the overall verdict of a complete automated run is **INCOMPLETE** until they are discharged by a person. `summary.json` therefore also carries `automated_verdict` (the verdict ignoring external prerequisites): `PASS` there means every automatable step passed with no skips.
* The runner always finalises (EXIT/INT/TERM trap): evidence is collected, only the run's own labelled resources are removed, the summary is written, and the leak scan runs, even on failure or Ctrl-C.

## Outputs (`artifacts/m0-<UTC time>-<sha8>/`)
`summary.txt` (human), `summary.json` (machine: exact source SHA, VM identity, Docker/Compose/Go versions, run ID, per-scenario `name/started/finished/exit_code/status` with status `passed|failed|skipped|incomplete`, traffic and recovery measurements, cleanup result, findings, limitations, external prerequisites, verdict), `logs/<step>.log|.json|.err` (raw `go test -json` per scenario), `evidence/` (host, docker, container logs - redacted, image file list, traffic report lines), `state/`, `SHA256SUMS`. `artifacts/` is not in `.gitignore`; the runner's clean-tree checks exclude it, but add it to `.gitignore` or keep it out of commits.

Secrets: the runner generates a synthetic sentinel (`DSO_IT_SENTINEL`, shared with the Go tests); it is never printed, is redacted from notes and evidence logs, and the final `evidence-leak-scan` step fails the run (and redacts in place) if it appears anywhere under the artifact directory.

## Environment
Ubuntu 24.04 x86_64, 4 vCPU, 8-16 GB RAM, 80 GB disk, rootful Docker Engine + Compose v2 (apt-pinned version, key fingerprint verified by the bootstrap), Go matching `go.mod`'s `toolchain` line (official tarball, SHA-256 verified; the runner sets `GOTOOLCHAIN=local` and fails on a mismatch), `jq`, `ss` (iproute2), `gcc` (for `-race`), `cosign` (installer tests), outbound network for base-image pulls (`golang`, `alpine`, `busybox`). No production credentials, workloads or public application ports. Rootless Docker may be added later as an extra compatibility run only.

Optional runner variables: `DSO_IT_EXPECT_SHA` (commit must equal it), `DSO_IT_EXPECT_DOCKER_VERSION` (server version pin), `DSO_IT_ARTIFACTS`, `DSO_IT_ALLOWED_PORTS` (default `22`), `DSO_IT_GOTEST_TIMEOUT_S`, `DSO_IT_CRASH_MIN_TESTS` (default 1), `DSO_IT_CRASH_TAGS` (default `dockerint`; the crash harness adds `dsofault` itself when it builds the agent).

## Steps and implementation status
Legend - **implemented**: automated by `run-all.sh`, step name in the third column. **depends on test X**: the step is a guarded `go test -tags dockerint ./test/docker -run ...` scenario and is only as strong as the named tests. **external prerequisite**: not automatable.

| # | Requirement | Runner step(s) | Status | How it is checked |
|---|---|---|---|---|
| 1 | Independent environment identity | `environment-identity` (+ pre-flight) | implemented | marker `/etc/dso-integration/environment`: regular file, root-owned, not group/other writable, parent dir likewise, valid IDs; `/etc/docker/daemon.json` root-owned with the same label (no Docker API involved); operator confirmation `DSO_IT_DISPOSABLE_VM_CONFIRM=<env id>`. Pre-flight refuses with exit 3 before anything else |
| 2 | Explicit Docker endpoint | `docker-endpoint` | implemented | any `DOCKER_*`/`COMPOSE_*` variable set is refused; only `unix:///...` accepted; every Docker call goes through `dk` (`DOCKER_HOST` set per call); socket must exist; optional version pin |
| 3 | Expected daemon identity | `daemon-identity` | implemented | `docker info` ID must equal the marker, label `dso.test.environment=<env id>` present, ID not in `DSO_TEST_PROHIBITED_DAEMON_IDS`. Gate: failure aborts before any resource is created. The Go suites re-verify through `test/dockerguard` |
| 4 | Foreign-resource checks | `foreign-resources-pre`, `foreign-resources-post` | implemented | any container/network/volume without the environment label (default `bridge/host/none` excepted) fails the run before; the same snapshot is diffed after cleanup. The guard additionally fails the Go packages on a foreign container |
| 5 | Unique run identity | `run-identity` | implemented | `dso-it-<12 hex>` from `/dev/urandom`, shape-validated, must own no existing resource; exported as `DSO_TEST_RUN_ID` so tests, the image, compose projects and cleanup share it |
| 6 | Source revision, clean tree | `source-revision`, `source-tree-unchanged` | implemented | full 40-hex SHA recorded (and compared with `DSO_IT_EXPECT_SHA`); `git status --porcelain --untracked-files=all` (excluding `artifacts/`) must be empty before and after the run |
| 7 | Go verification | `go-toolchain`, `go-build`, `go-build-dsofault`, `go-vet`, `go-vet-dockerint`, `go-vet-dsofault`, `go-test-short`, `go-test-race-short`, `installer-tests` | implemented | installed Go must match the `go.mod` toolchain; `-short` and `-race -short` via `go test -json` (must run >=1 test, none may fail; skips allowed only here); `installer-tests` must not skip and fails if `cosign` is absent. `go-build-dsofault` proves the fault-tagged binary compiles |
| 8 | Real DSO image build | `image-build` | implemented | `docker build` of the repo `Dockerfile` with both guard labels, tag `dso-it-image:<run id>` |
| 9 | Image-content inspection | `image-content-inspection` | implemented | `docker save` + per-layer `tar -t` (non-vacuous: must list the DSO binary) rejects `.git`, `data/`, `*.db*`, `*.rvf*`, keys/`.pem` (base CA bundle allow-listed), `.claude*`/`.swarm`, `.env*`, `local-secrets`, `*.go`; `docker run --rm` (labelled, `--network none`, read-only) checks uid 10001/`dso-user`, root-owned non-writable plugin dir, every `hashes.txt` entry matches its binary and the counts agree; the shipped binary must not carry the `dsofault` tag. Image has no HEALTHCHECK -> recorded as a finding |
| 10 | Secret injection into a real tmpfs | `secret-tmpfs-injection` | implemented (platform semantics) + depends on `TestSignalRotation_FileMode_ApplicationLoadsNewValue` for the product path | labelled container with `--tmpfs /run/secrets/dso` (uid 10001, nosuid/nodev/noexec): `/proc/self/mountinfo` shows tmpfs; file is `0400` uid 10001; absent from `docker diff`; sentinel absent from `docker inspect`. The product's own injection is covered by the signal scenario |
| 11 | Non-root secret access | `secret-nonroot-access` | implemented | `docker exec -u 10001` reads the delivered file; uid 10002 is denied; the app uid cannot write it |
| 12 | Health-probe behaviour | `health-probe-behaviour` | implemented (platform) + depends on `TestRotation_HealthCheck_Timeout*`, `TestTraffic_CandidateReadinessFailure` | failing HEALTHCHECK -> `unhealthy`, passing -> `healthy`, none -> no health state (process-only evidence) |
| 13 | Network bind / protocol | `network-bind-protocol` | implemented (platform) + depends on `TestTraffic_UnsupportedDirectPublishing` | 127.0.0.1-bound listener unreachable from a second container on the same network while a 0.0.0.0 control is reachable (probe validity proven); published `127.0.0.1::9001/tcp` reachable on host loopback; no UDP mapping and no UDP listener on the host port. `ss` required |
| 14 | Actual Compose deployment | `compose-stdin-deploy`, `compose-profiles`, `compose-failure`, `compose-cancellation` | implemented | `scripts/integration/composedrv` calls the product's real `core.RunComposeUpStdin` against the real `docker compose -f -`: project name, project directory, relative `./data` bind resolved, `.env` interpolation, default vs `COMPOSE_PROFILES=extra`, failing service => non-zero and no secret in output, SIGTERM cancellation stops the project's containers, no `docker-compose-dso-*` file and no secret under the process `TMPDIR`. All compose resources carry the guard labels |
| 15 | Signal rotation | `scenario-signal` | depends on `TestSignalRotation_FileMode_ApplicationLoadsNewValue`, `TestSignalRotation_FileMode_IgnoredSignalIsNotAdoption`, `TestS3_RotationStrategies/signal-rejects-env-secret` | all three must pass; any skip/absence fails |
| 16 | Restart rotation | `scenario-restart` | depends on `TestS2_EnvSecretInjection`, `TestS3_RotationStrategies/restart` | downtime of restart is measured by `TestTraffic_RestartRotation` (step 18) |
| 17 | Rolling rotation | `scenario-rolling` | depends on `TestS3_RotationStrategies/rolling`, `TestRotation_BlueGreenSwap_Success`, `TestRotation_FileMode_SecretNeverInDockerInspect`, `TestRotation_Concurrent_MultipleContainers`, `TestRotation_HealthCheck_Timeout`, `TestRotation_HealthCheck_Timeout_FailingHealthcheckRollsBack`, `TestRotation_EventDriven_Detection` | proxy swap, candidate readiness failure and handoff failure are additionally in step 18 |
| 18 | Continuous traffic | `scenario-traffic` | depends on `TestTraffic_*` (5 tests) | all five must pass; the `=== TRAFFIC REPORT` / `[pre|during|post]` log lines are parsed into `summary.json` `traffic[]` (requests, ok, failed, connection failures, timeouts, backends, latency p50/p95/p99/max, rotation duration) and saved to `evidence/traffic-report.txt`; the step fails if fewer than 5 complete reports (3 phases, >0 requests) are found. Limitation recorded in `summary.json`: only new-connection clients are measured; long-lived connections are not |
| 19 | Real-process termination + recovery | `scenario-recovery-realdaemon`, `scenario-crash-recovery` | depends on `TestRecovery_RealDaemon_*` (3) and `TestCrash*` (`go test -tags dockerint ./test/docker -run '^TestCrash'`; harness under `test/docker` + `dsofault` fault hook, being written separately) | **fails (not passes) if zero tests match/run, any skip, or fewer than `DSO_IT_CRASH_MIN_TESTS`**. Recovery measurements: t.Logf lines containing "recover" (with `key=value` tokens) are parsed into `summary.json` `recovery[]`; if none are found a finding is recorded. Until the crash tests exist this step is FAILED by design |
| 20 | Ownership protection | `scenario-ownership-concurrency`, `foreign-resources-post` | depends on `TestS1_ComposeServiceAdoption`, `TestShadowClone_DoesNotDuplicateNetworkIdentity`, `TestRace_*` (3), `TestNotificationIntegration_RealRotationDeliversWebhook`, `TestStress_ConcurrentRotations` | plus the before/after foreign-resource diff. `docker-suite-coverage` lists every `Test*` in `./test/docker` (`go test -list`) and fails if any is not executed by some step, so a new test cannot be silently left out |
| 21 | Cleanup | `cleanup` | implemented | `test/dockerguard` `CleanupRun` (compiled `cmd/cleanup`, re-verifies the daemon, removes only resources labelled with BOTH environment and run IDs), then removal of this run's image, then verification that nothing carrying both labels remains. No `docker system/container/volume/network/image prune` anywhere (the selftest greps for it). Build cache is not pruned: the VM is destroyed instead |
| 22 | Evidence collection | `evidence-collection`, `evidence-leak-scan` | implemented | host/docker facts, labelled-container logs (redacted), dockerd journal, per-step logs, `SHA256SUMS`, summary files; leak scan for the sentinel |
| - | Host listening sockets | `host-listeners` | implemented | after the scenarios, any TCP listener on a non-loopback address other than `DSO_IT_ALLOWED_PORTS` fails ("no public application ports", from inside the host) |
| E1 | Provisioning provenance | `external-provisioning-provenance` | **EXTERNAL_PREREQUISITE** | Docker apt pin/key fingerprint and Go tarball SHA-256 were verified at provisioning time, before the runner exists; cannot be re-proven from inside the VM (installed versions are recorded) |
| E2 | Network perimeter | `external-network-perimeter` | **EXTERNAL_PREREQUISITE** | cloud firewall / SSH source restriction / no public ingress is a property of the VM's network, not observable from inside it |
| E3 | VM teardown | `external-vm-teardown` | **EXTERNAL_PREREQUISITE** | the VM must be destroyed after evidence is copied off; a runner cannot destroy its own host |

Bootstrap/provisioning (`bootstrap-test-vm.sh`) is a separate, manually-run root script (not part of `run-all.sh`); its result is what steps 1-3 verify.

## Self-test (no Docker)
`./scripts/integration/selftest.sh` sources `m0-lib.sh` (all pure logic: step bookkeeping, verdict computation, `go test -json` skip/zero-test classification, refusal conditions, image-content scan, traffic/recovery parsers, `summary.json`/`summary.txt` generation) with a failing `docker` stub first in `PATH`, and also runs `run-all.sh` itself to prove it refuses (exit 3) with `DOCKER_HOST` set or without the marker, never reaching Docker. It fails if the stub is ever invoked.

## Privileged operations in `bootstrap-test-vm.sh` (review)
The script refuses unless `DSO_BOOTSTRAP_DISPOSABLE_VM=yes`, `EUID=0`, and (for `--ci-ephemeral`) a GitHub-hosted runner. It contains no `sudo` (it must already be root). Complete list of privileged operations:

| Operation | Where | Guard / note |
|---|---|---|
| `install -m 0755 -d /etc/apt/keyrings` | `--install-docker` | creates a directory |
| `curl -fsSL https://download.docker.com/linux/ubuntu/gpg` to a `mktemp` file | `--install-docker` | key fingerprint `9DC858229FC7DD38854AE2D88D81803C0EBFCD88` must match before use; temp file removed by an EXIT trap |
| `gpg --dearmor` -> `/etc/apt/keyrings/docker.gpg`, `chmod a+r` | `--install-docker` | only after the fingerprint check |
| write `/etc/apt/sources.list.d/docker.list` | `--install-docker` | Ubuntu 24.04 only (`/etc/os-release` checked) |
| `apt-get update -y`; `apt-get install -y docker-ce=$DOCKER_APT_VERSION docker-ce-cli=... containerd.io docker-compose-plugin` | `--install-docker` | exact pinned version required, no default. `containerd.io` and `docker-compose-plugin` are unpinned (follow the apt repo); `docker-buildx-plugin` is not listed (pulled in only as a recommends) |
| refuse if `docker ps -aq` is non-empty | always | reads the daemon; prevents repurposing a used host |
| refuse if `/etc/docker/daemon.json` exists and is non-empty | always | never merges or overwrites; **does not** detect daemon flags set via a systemd drop-in/`dockerd -H` |
| write `/etc/docker/daemon.json` (`labels: dso.test.environment=<env-xxxxxxxxxxxxxxxx>`) with umask 022 | always | env id from `/dev/urandom` |
| `systemctl restart docker` | always | safe only because no containers exist (checked above) |
| `docker info` ID read-back and label check | always | fails if the label did not apply |
| `install -d /etc/dso-integration`; write `/etc/dso-integration/environment` (env id + daemon id); `chown root:root`; `chmod 0644` | always | the root-owned, non-world-writable marker is the trust anchor the runner and the guard verify |

Not done by the script (and not done by the runner): adding users to the `docker` group (an operator step, equivalent to root on the VM), installing Go/cosign/jq, firewall configuration. Observations: the marker is world-readable by design (IDs are not secrets); `--install-docker` downloads over HTTPS from Docker's site, so the run needs outbound network; nothing in the bootstrap reverts changes - destroy the VM.

## What this plan does not claim
Passing the VM run would be the first real-Docker evidence for M0-02/03/04/07/08/09/11/13. It would not establish zero downtime beyond the tested conditions, survival of long-lived connections (not measured), behaviour under host power loss, or production readiness. The runner has been syntax-checked and its pure logic self-tested only; the Docker-dependent step bodies (image inspection, tmpfs/health/network/compose checks, finalisation against a real daemon) have not been executed and may need adjustment on first run in the VM.
