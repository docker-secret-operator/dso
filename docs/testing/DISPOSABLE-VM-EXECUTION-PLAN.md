# Disposable-VM execution plan (M0 runtime verification)

Status: **runner implemented, not executed.** No isolated rootful Docker environment exists yet; nothing below has been run against Docker. The development machine and its Docker daemon are never used. What *has* been run (without any daemon): `bash -n` on every script, `go vet ./scripts/integration/...`, and `scripts/integration/selftest.sh` (273 checks, with a failing `docker` stub first in `PATH` that is proven never invoked). ShellCheck was **not** run (not installed and not fetched; see "Static analysis" below).

## 1. Getting the source onto the VM (the branch is NOT pushed)
The branch has not been pushed, so `git clone <repo> && git checkout <sha>` on the VM is **not valid**. **Do not push the branch for this purpose, and do not transfer full history (a clone, a bundle, or `.git`) to the VM while the N7/N8 findings are unresolved.** The VM receives one commit's tree only, as a deterministic archive plus a provenance record.

On the **development host** (no Docker needed; the work tree must be clean, including untracked files):

```bash
SHA=$(git rev-parse HEAD)                       # the exact committed SHA under test; write it down out-of-band
scripts/integration/make-source-archive.sh "$SHA" /tmp/dso-transfer
# -> dso-<sha12>.tar.gz   (git archive, tree only, no history, fixed mtime = commit time, gzip -n)
# -> provenance.json      (sha, tree_sha = git rev-parse SHA^{tree}, branch, commit time, creation time, tool versions, tarball sha256)
# -> SHA256SUMS           (tarball + provenance.json)
```
It refuses a dirty tree, a short/unknown/non-commit SHA, an output directory inside the work tree, an existing output file, and an archive whose extracted files do not reproduce the commit's tree hash. Running it twice yields a byte-identical tarball.

Transfer (SSH only from the operator's address; never an unauthenticated channel) and verify **before** extracting:

```bash
scp /tmp/dso-transfer/{dso-*.tar.gz,provenance.json,SHA256SUMS} <vm-user>@<vm>:/home/<vm-user>/transfer/
# on the VM:
cd ~/transfer && sha256sum -c SHA256SUMS          # must print OK for both files; stop otherwise
# compare the SHA256SUMS line of the tarball with the value you noted on the dev host (read over a second channel)
mkdir -p ~/dso-src && tar -xzf dso-<sha12>.tar.gz -C ~/dso-src --no-same-owner
cd ~/dso-src/dso-<sha12>                          # no .git here, by design
```

Run (provenance mode; `DSO_IT_EXPECT_SHA` is **mandatory** without `.git` because the provenance file is self-asserted, so the commit is pinned out-of-band):

```bash
DSO_IT_SOURCE_PROVENANCE=~/transfer/provenance.json \
DSO_IT_EXPECT_SHA=<the SHA noted on the dev host> \
DSO_IT_DISPOSABLE_VM_CONFIRM=<environment id from /etc/dso-integration/environment> \
./scripts/integration/run-all.sh
```
What the runner does without `.git`: refuses (exit 3) if there is neither `.git` nor `DSO_IT_SOURCE_PROVENANCE`, or if both are present, or if `DSO_IT_EXPECT_SHA` is missing/different; recomputes the git tree hash of the extracted files (names, modes, bytes; top-level `artifacts/` excluded; host git config, ignore rules and attributes neutralised) and refuses unless it equals the provenance `tree_sha` (a tampered, added, removed or chmod-ed file, or a planted `.git`, fails); records SHA, tree SHA and the provenance sha256 in `summary.json` (`source`) and copies the provenance into `evidence/`; re-checks the tree hash at the end (`source-tree-unchanged`). With a `.git` present the original checks apply (clean work tree including untracked files, SHA = HEAD).

Single entry point (git work tree variant, if the VM ever has a legitimately obtained checkout):

```bash
DSO_IT_DISPOSABLE_VM_CONFIRM=<environment id from /etc/dso-integration/environment> ./scripts/integration/run-all.sh
```

## 1A. Transfer and run - ordered runbook (every command: NOT YET RUN)
Supersedes the shorter snippets in section 1. Interfaces cited as file:line were read from the scripts. The archive is created on the dev host: `/home/md.umair/dso-vm-transfer-<SHA7>/` (`dso-<SHA12>.tar.gz`, `provenance.json`, `SHA256SUMS`). Out-of-band pins (record on a second channel): commit `<CANDIDATE_SHA>`, tree `<TREE_SHA>`, tarball sha256 `<TARBALL_SHA256>`.

### Prerequisites checklist
- [ ] VM: Ubuntu 24.04 x86_64, 4 vCPU, 16 GB RAM, 80-100 GB disk, freshly created, no public ports (only SSH from the operator address), outbound access for: Docker Hub (`golang:1.26.11-alpine`, `alpine:3.20`, busybox), `download.docker.com`, Go module proxy (no `vendor/`; `go build` and `docker build` fetch modules), Go tarball host, cosign source.
- [ ] Rootful Docker Engine + Compose v2 (either pre-installed, or via `bootstrap-test-vm.sh --install-docker` with `DOCKER_APT_VERSION`, bootstrap-test-vm.sh:43-44).
- [ ] Tools required by run-all.sh pre-flight: `git jq go timeout sha256sum tar awk sed ss` (run-all.sh:73; note `git` is needed even without `.git`, because the tree hash uses it, m0-lib.sh:463-471); plus `gcc` (`-race`), `cosign` (installer-tests; absent => step fails, run-all.sh:514-517), `curl gpg dpkg systemctl` for bootstrap. Go must equal go.mod `toolchain go1.26.11` (run-all.sh:188-193, `GOTOOLCHAIN=local`).
- [ ] A non-root user able to use the Docker socket (docker group = root-equivalent on the VM), `/var/tmp` writable (run-all.sh:98).
- [ ] No `DOCKER_*` / `COMPOSE_*` variables in the shell (refused, m0-lib.sh:320-327).
- [ ] No production credentials or workloads on the VM.

### Operator decisions before running
VM provider/destroy owner; SSH key and how the VM host-key fingerprint is obtained (console/provider API, not from the first connection); exact `DOCKER_APT_VERSION` string (must be known before bootstrap since the repo is added by the script) or pre-install Docker by hand; source and checksum of the Go toolchain and cosign; docker-group user vs root; optional `DSO_IT_EXPECT_DOCKER_VERSION` pin (run-all.sh:142), `DSO_IT_ARTIFACTS`; leave `DSO_IT_SECURITY_REVIEW_FILE` unset while N7/N8 are open (security_review stays OPEN, m0-finalize.sh:144); accept that the run exercises Docker only inside that VM; note `scenario-crash-recovery` needs `TestCrash` (present: test/docker/crash_recovery_test.go:41).

### Steps
1. **Generate the archive (dev host).** `scripts/integration/make-source-archive.sh <CANDIDATE_SHA> <outdir>` (usage make-source-archive.sh:28; needs git jq tar gzip sha256sum mktemp, :32; refuses a dirty tree :39, an output dir inside the work tree :44, existing files :47-49). The work tree must be clean (commit first). The pins for the current candidate (SHA, tree, tarball sha256) are recorded OUTSIDE the archive in `docs/audit/33-FROZEN-CANDIDATE.md` and in the checkpoint report, because writing them into a file inside the archive would change the tree they describe; replace the `<...>` placeholders below with those values.
2. **Verify locally.** `cd /home/md.umair/dso-vm-transfer-<SHA7> && sha256sum -c SHA256SUMS` (two OK lines; observed OK when this section was written) and `echo "<TARBALL_SHA256>  dso-<SHA12>.tar.gz" | sha256sum -c`; `jq -r '.sha,.tree_sha' provenance.json`.
3. **Secure transfer** (host key pinned in a dedicated known_hosts file after comparing `ssh-keygen -lf` with the provider-reported fingerprint; no agent forwarding; ignore ambient ssh config):
   `scp -F /dev/null -i ~/.ssh/<key> -o IdentitiesOnly=yes -o ForwardAgent=no -o StrictHostKeyChecking=yes -o UserKnownHostsFile=$HOME/.ssh/known_hosts_dsovm /home/md.umair/dso-vm-transfer-<SHA7>/{dso-<SHA12>.tar.gz,provenance.json,SHA256SUMS} <vm-user>@<vm-host>:/home/<vm-user>/transfer/`
4. **Verify on the VM.** `cd ~/transfer && sha256sum -c SHA256SUMS && echo "<TARBALL_SHA256>  dso-<SHA12>.tar.gz" | sha256sum -c` (the second line compares against the out-of-band pin; stop on any mismatch).
5. **Extract into an isolated directory (not a git repo; do not `git init`).** `mkdir -p ~/dso-run && tar -xzf ~/transfer/dso-<SHA12>.tar.gz -C ~/dso-run --no-same-owner && cd ~/dso-run/dso-<SHA12> && test ! -e .git`. Keep `provenance.json` and any console logs OUTSIDE this directory (only top-level `artifacts/` is excluded from the tree hash, m0-lib.sh:471).
6. **Verify source provenance.** The real mechanism is run-all.sh pre-flight `m0_resolve_source` (run-all.sh:78; m0-lib.sh:508-525): `.git` absent => `DSO_IT_SOURCE_PROVENANCE` and `DSO_IT_EXPECT_SHA` are both mandatory (:519-520), SHA must equal the pin, and the recomputed tree must equal provenance `tree_sha` (m0-lib.sh:486-502). Dry check without Docker: `bash -c 'source scripts/integration/m0-lib.sh; m0_check_source_provenance "$HOME/transfer/provenance.json" "$PWD" <CANDIDATE_SHA> && echo "$M0_SHA $M0_TREE"'` expecting the pinned SHA and tree `<TREE_SHA>`.
7. **Bootstrap (as root; once per VM).** Preconditions (bootstrap-test-vm.sh): `DSO_BOOTSTRAP_DISPOSABLE_VM=yes` (:36), `EUID=0` (:37), docker installed (:61, or `--install-docker`, Ubuntu 24.04 only, needs `DOCKER_APT_VERSION`, :43-46), daemon has zero containers (:65-67), no `/etc/dso-integration/environment` (:70), no non-empty `/etc/docker/daemon.json` (:74). The script has no sudo, so: `sudo env DSO_BOOTSTRAP_DISPOSABLE_VM=yes [DOCKER_APT_VERSION='<exact apt string>'] ./scripts/integration/bootstrap-test-vm.sh [--install-docker]`. Accepted flags only `--install-docker`, `--ci-ephemeral` (:26-31; the latter is for GitHub-hosted runners only - do not use). Privileged changes: apt key/repo/packages (with `--install-docker`), `/etc/docker/daemon.json` label, `systemctl restart docker`, `/etc/dso-integration/environment` (root:root 0644); full list in the "Privileged operations" section below. Record the printed `Test environment env-... provisioned.` line (:101) - that id is the confirmation value. Then (operator, not scripted) install Go/cosign/jq/gcc/git/iproute2 and add the test user to the `docker` group (re-login).
8. **Run the complete suite (as the unprivileged docker-group user, from the extracted dir):**
   `cd ~/dso-run/dso-<SHA12> && env | grep -E '^(DOCKER_|COMPOSE_)' ; DSO_IT_SOURCE_PROVENANCE="$HOME/transfer/provenance.json" DSO_IT_EXPECT_SHA=<CANDIDATE_SHA> DSO_IT_DISPOSABLE_VM_CONFIRM=<env-id from step 7> ./scripts/integration/run-all.sh 2>&1 | tee ~/run-all.console.log`
   (the `env | grep` must print nothing.) Variables: `DSO_IT_SOURCE_PROVENANCE`/`DSO_IT_EXPECT_SHA` m0-lib.sh:511-520; `DSO_IT_DISPOSABLE_VM_CONFIRM` run-all.sh:131 (must equal the marker's environment id). Optional: `DSO_IT_EXPECT_DOCKER_VERSION` :142, `DSO_IT_ARTIFACTS` :89, `DSO_IT_ALLOWED_PORTS` :464 (default 22), `DSO_IT_GOTEST_TIMEOUT_S` :204 (default 2700 per go test step), `DSO_IT_CRASH_MIN_TESTS` :65, `DSO_IT_CRASH_TAGS` :64, `DSO_TEST_DOCKER_HOST` :81 (default `unix:///var/run/docker.sock`; only `unix:///` accepted), `DSO_TEST_PROHIBITED_DAEMON_IDS` :147, `DSO_IT_SECURITY_REVIEW_FILE` m0-finalize.sh:144 (leave unset). Exit: 0 PASS, 1 FAIL, 2 INCOMPLETE, 3 refused. Even exit 0 leaves `release_readiness` INCOMPLETE (teardown pending, security review OPEN).
9. **Retrieve evidence.** Directory: `~/dso-run/dso-<SHA12>/artifacts/m0-<UTC time>-<SHA8>/` (run-all.sh:88-89) or `$DSO_IT_ARTIFACTS`. On the VM: `cd <artifact dir> && sha256sum -c SHA256SUMS` (covers every file except itself, m0-finalize.sh:159-160), then `tar -czf ~/evidence-<SHA7>.tar.gz -C ~/dso-run/dso-<SHA12>/artifacts . && sha256sum ~/evidence-<SHA7>.tar.gz`. Copy back with the same hardened `scp` options as step 3 (reverse direction), recompute the tarball sha256 locally, extract into a new empty directory, and re-run `sha256sum -c SHA256SUMS` there; read `summary.json` fields `automated_integration`, `environment_attestation`, `security_review`, `cleanup`, `release_readiness`.
10. **Teardown.** Destroy the VM through the provider after the evidence is verified locally (external prerequisite E3; the runner cannot do it and nothing reverts the bootstrap). Record teardown DONE outside the runner. Do not reuse the VM: a second bootstrap is refused (:70).

### Interface mismatches found (reported, not fixed)
- bootstrap-test-vm.sh:102 tells the operator to run `scripts/integration/run-integration.sh`, whereas this plan and run-all.sh header name `run-all.sh` as the single entry point.
- Section "Environment" below lists `jq, ss, gcc, cosign, Go` but omits `git` (run-all.sh:73 requires it even in provenance mode) and says only base-image pulls need outbound; Go module downloads (no `vendor/`) also need it.
- bootstrap-test-vm.sh cannot be given `DOCKER_APT_VERSION` discovery (the repo is added by the script itself), so the exact string must be obtained beforehand.

## 2. Results are independent fields, never one verdict
`summary.json` / `summary.txt` carry separate fields. A passing automated run is **not** a release approval.

| Field | Values | Decided by |
|---|---|---|
| `automated_integration` | `PASS` / `FAIL` / `INCOMPLETE` | executable steps only. `FAIL`: any failed step (a scenario that matched zero tests is `failed`). `INCOMPLETE`: nothing failed but a required step was skipped, did not run, or is missing. `PASS`: every required executable step passed, none skipped. External prerequisites never change it. |
| `environment_attestation` | `VERIFIED` / `UNVERIFIED` | marker check passed (pre-flight) **and** `environment-identity`, `docker-endpoint`, `daemon-identity`, `foreign-resources-pre`, `foreign-resources-post` all passed. Covers only facts observable from inside the VM. |
| `security_review` | `OPEN` / `CLOSED` | **never decided by the runner.** Default `OPEN`. `CLOSED` only if `DSO_IT_SECURITY_REVIEW_FILE` names a regular, non-symlink file (<= 4 KiB, owned by root or the runner's user, not group/other writable) containing exactly one line `DSO_SECURITY_REVIEW_STATUS=CLOSED`. Absent, malformed, ambiguous, lower-case or commented-out status is `OPEN` (and a finding is recorded if the variable was set). The file's sha256 is recorded in `security_review_source`. |
| `cleanup` | `PASS` / `FAIL` | the ownership-scoped `cleanup` step (labels: environment id AND run id); missing/skipped/failed is `FAIL`. The detail is in `cleanup_detail`. |
| `vm_teardown` | `PENDING` | always `PENDING` in runner output; destroying the VM is external. |
| `release_readiness` | `INCOMPLETE` / `FAIL` / `PASS` | `FAIL` if automated `FAIL` or cleanup `FAIL`; `PASS` only if automated `PASS` **and** attestation `VERIFIED` **and** security review `CLOSED` **and** cleanup `PASS` **and** teardown `DONE`; otherwise `INCOMPLETE`. The runner always passes teardown `PENDING`, so its own output is never `PASS`; `DONE` is an operator confirmation recorded outside the runner. |
| `verdict` | = `release_readiness` | compatibility alias only (`automated_verdict` aliases `automated_integration`). |

Exit codes derive from `automated_integration` only: `0` automated PASS, `1` automated FAIL, `2` automated INCOMPLETE, `3` refused (nothing was run, no artifacts). Because the finalisation steps (`cleanup`, `foreign-resources-post`, `source-tree-unchanged`, `evidence-leak-scan`) are executable steps, a cleanup failure, a changed foreign resource, a modified source tree or a leaked secret also makes the automated result `FAIL` (exit 1).

* A required Docker scenario that matched **zero tests**, or whose tests were **all/any skipped**, or where a named required test did not pass, is never `passed` (classified from `go test -json`; see `m0_classify_go_json`). The package-level failure produced by the isolation guard (wrong daemon) is a failure, not a skip.
* Three steps are `EXTERNAL_PREREQUISITE` by nature (E1-E3 below). They are recorded as `incomplete` rows of kind `external_prerequisite`, listed under `external_prerequisites`, and keep `release_readiness` `INCOMPLETE`; they do not affect `automated_integration`.
* The runner always finalises (EXIT/INT/TERM trap, `m0-finalize.sh`): evidence is collected, only the run's own labelled resources are removed, the summary is written, the leak scan runs, and `SHA256SUMS` (covering every file except itself, summaries included) is produced, even on failure or Ctrl-C.

## Outputs (`artifacts/m0-<UTC time>-<sha8>/`)
`summary.txt` (human), `summary.json` (machine: exact source SHA/tree/mode, VM identity, Docker/Compose/Go versions, run ID, the independent verdict fields above, per-scenario `name/started/finished/exit_code/status/kind` with status `passed|failed|skipped|incomplete`, traffic and recovery measurements, `cleanup_detail`, findings, limitations, external prerequisites), `logs/<step>.log|.json|.err` (raw `go test -json` per scenario), `evidence/` (host, docker, container logs - redacted, image file list, traffic report lines, source provenance), `state/`, `SHA256SUMS`. `artifacts/` is not in `.gitignore`; the runner's clean-tree checks exclude it, but add it to `.gitignore` or keep it out of commits.

Secrets: the runner generates a synthetic sentinel (`DSO_IT_SENTINEL`, shared with the Go tests); it is never printed, is redacted from notes and evidence logs, and the final `evidence-leak-scan` step fails the run (and redacts in place) if it appears anywhere under the artifact directory; the summaries are redacted again after they are written.

## Environment
Ubuntu 24.04 x86_64, 4 vCPU, 8-16 GB RAM, 80 GB disk, rootful Docker Engine + Compose v2 (apt-pinned version, key fingerprint verified by the bootstrap), Go matching `go.mod`'s `toolchain` line (official tarball, SHA-256 verified; the runner sets `GOTOOLCHAIN=local` and fails on a mismatch), `git` (the runner requires it even in archive/provenance mode), `jq`, `ss` (iproute2), `gcc` (for `-race`), `cosign` (installer tests), outbound network for base-image pulls (`golang`, `alpine`, `busybox`) AND for Go module downloads (there is no `vendor/`), and the exact `DOCKER_APT_VERSION` string obtained beforehand (the bootstrap adds the apt repo itself). No production credentials, workloads or public application ports. Rootless Docker may be added later as an extra compatibility run only.

Optional runner variables: `DSO_IT_SOURCE_PROVENANCE` (archive mode, see section 1), `DSO_IT_SECURITY_REVIEW_FILE` (operator review status, default OPEN), `DSO_IT_EXPECT_SHA` (commit must equal it; mandatory without `.git`), `DSO_IT_EXPECT_DOCKER_VERSION` (server version pin), `DSO_IT_ARTIFACTS`, `DSO_IT_ALLOWED_PORTS` (default `22`), `DSO_IT_GOTEST_TIMEOUT_S`, `DSO_IT_CRASH_MIN_TESTS` (default 1), `DSO_IT_CRASH_TAGS` (default `dockerint`; the crash harness adds `dsofault` itself when it builds the agent).

## Steps and implementation status
Legend - **implemented**: automated by `run-all.sh`, step name in the third column. **depends on test X**: the step is a guarded `go test -tags dockerint ./test/docker -run ...` scenario and is only as strong as the named tests. **external prerequisite**: not automatable.

| # | Requirement | Runner step(s) | Status | How it is checked |
|---|---|---|---|---|
| 1 | Independent environment identity | `environment-identity` (+ pre-flight) | implemented | marker `/etc/dso-integration/environment`: regular file, root-owned, not group/other writable, parent dir likewise, valid IDs; `/etc/docker/daemon.json` root-owned with the same label (no Docker API involved); operator confirmation `DSO_IT_DISPOSABLE_VM_CONFIRM=<env id>`. Pre-flight refuses with exit 3 before anything else |
| 2 | Explicit Docker endpoint | `docker-endpoint` | implemented | any `DOCKER_*`/`COMPOSE_*` variable set is refused; only `unix:///...` accepted; every Docker call goes through `dk` (`DOCKER_HOST` set per call); socket must exist; optional version pin |
| 3 | Expected daemon identity | `daemon-identity` | implemented | `docker info` ID must equal the marker, label `dso.test.environment=<env id>` present, ID not in `DSO_TEST_PROHIBITED_DAEMON_IDS`. Gate: failure aborts before any resource is created. The Go suites re-verify through `test/dockerguard` |
| 4 | Foreign-resource checks | `foreign-resources-pre`, `foreign-resources-post` | implemented | any container/network/volume without the environment label (default `bridge/host/none` excepted) fails the run before; the same snapshot is diffed after cleanup. The guard additionally fails the Go packages on a foreign container |
| 5 | Unique run identity | `run-identity` | implemented | `dso-it-<12 hex>` from `/dev/urandom`, shape-validated, must own no existing resource; exported as `DSO_TEST_RUN_ID` so tests, the image, compose projects and cleanup share it |
| 6 | Source revision, clean tree | `source-revision`, `source-tree-unchanged` | implemented | full 40-hex SHA recorded (and compared with `DSO_IT_EXPECT_SHA`). With `.git`: `git status --porcelain --untracked-files=all` (excluding `artifacts/`) must be empty before and after the run. Without `.git`: the provenance tree hash is recomputed from the files before and after (section 1) |
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
`./scripts/integration/selftest.sh` (273 checks) puts a failing `docker` stub (and a `go` stub) first in `PATH` and fails if the docker stub is ever invoked. It covers:
* pure logic of `m0-lib.sh`: step bookkeeping, the step verdict engine, `go test -json` skip/zero-test classification, refusal conditions, image-content scan, traffic/recovery parsers;
* **verdict separation**: `automated_integration` (required skipped/missing/zero-test => not PASS; external prerequisites open + all executable passed => PASS), `environment_attestation` (each gate missing/failed/skipped, marker not ok), `security_review` (default OPEN; CLOSED only from an acceptable explicit file; symlink/group-writable/oversized/ambiguous/commented => OPEN), `cleanup`, every one of the 48 combinations of `release_readiness` inputs, and `m0_compute_verdicts` end to end (including "automated PASS + external prerequisites open => readiness INCOMPLETE");
* **finalisation dry-run**: the real `finalize()` (from `m0-finalize.sh`) is run in a subshell against a fake artifact directory and a temp git repo, with a fake step set (pass/fail/skip/missing/external) and stubbed `docker`/`go`. It asserts exit codes, `summary.json`/`summary.txt` fields, `SHA256SUMS` content and verification (and that tampering breaks it), cleanup-result reporting (real path without a verified daemon; stubbed failure), the sentinel leak scan (a sentinel planted in a log and an evidence file fails the run, is redacted in place and absent from the summaries, `SHA256SUMS` matches the redacted files), a source tree modified during the run, and the operator security-review file;
* **source transfer**: `make-source-archive.sh` against a temp repo (refusals: short/unknown/non-commit SHA, dirty tracked/untracked tree, output inside the tree, overwrite; determinism; provenance content; no `.git` in the tarball) and the provenance check (pristine accepted; tampered/extra/deleted/chmod-ed file, changed symlink, planted `.git`/`.gitignore`, wrong SHA, altered tree_sha, bad schema, non-JSON, missing/symlinked provenance refused; `artifacts/` ignored), `m0_resolve_source` (git vs provenance mode, no `.git` + no provenance refused), and `run-all.sh` itself from copies/extractions refusing with exit 3 (dirty git tree, no marker, no provenance, no pinned SHA, tampered archive) without reaching Docker.

## Static analysis
`bash -n` on all scripts and `go vet ./scripts/integration/...` pass. **ShellCheck has not been run**: it is not installed on the development host and was not downloaded (a download needs explicit operator approval; the upstream release is `koalaman/shellcheck` v0.11.0 `linux.x86_64.tar.xz`, 2,559,196 bytes, sha256 `8c3be12b05d5c177a04c29e3c78ce89ac86f1595681cab149b65b97c4e227198` per the GitHub release API, which is not an independent checksum source). Run it in CI or after an approved install, then fix findings without blanket disables.

## Privileged operations in `bootstrap-test-vm.sh` (review)
The script refuses unless `DSO_BOOTSTRAP_DISPOSABLE_VM=yes`, `EUID=0`, and (for `--ci-ephemeral`) a GitHub-hosted runner. It contains no `sudo` (it must already be root). Complete list of privileged operations:

| Operation | Where | Guard / note |
|---|---|---|
| `install -m 0755 -d /etc/apt/keyrings` | `--install-docker` | creates a directory |
| `curl -fsSL https://download.docker.com/linux/ubuntu/gpg` to a `mktemp` file | `--install-docker` | key fingerprint `9DC858229FC7DD38854AE2D88D81803C0EBFCD88` must match before use; temp file removed by an EXIT trap |
| `gpg --dearmor` -> `/etc/apt/keyrings/docker.gpg`, `chmod a+r` | `--install-docker` | only after the fingerprint check |
| write `/etc/apt/sources.list.d/docker.list` | `--install-docker` | Ubuntu 24.04 only (`/etc/os-release` checked) |
| `apt-get update -y`; `apt-get install -y docker-ce=$DOCKER_APT_VERSION docker-ce-cli=... containerd.io docker-compose-plugin` | `--install-docker` | exact pinned version required, no default. `containerd.io` and `docker-compose-plugin` are unpinned (follow the apt repo); `docker-buildx-plugin` is not listed (pulled in only as a recommends) |
| `docker ps -aq`: refuse if it fails **or** is non-empty | always | reads the daemon; prevents repurposing a used host. Re-review fix: previously an unreachable daemon produced empty output and read as "no containers" (fail-open); the query failure is now fatal |
| refuse if `/etc/dso-integration/environment` already exists | always | re-review addition: a second bootstrap must not replace the trust anchor of an existing environment (destroy and recreate the VM instead) |
| refuse if `/etc/docker/daemon.json` exists and is non-empty | always | never merges or overwrites; **does not** detect daemon flags set via a systemd drop-in/`dockerd -H` |
| write `/etc/docker/daemon.json` (`labels: dso.test.environment=<env-xxxxxxxxxxxxxxxx>`) with umask 022 | always | env id from `/dev/urandom` |
| `systemctl restart docker` | always | safe only because no containers exist (checked above) |
| `docker info` ID read-back and label check | always | fails if the label did not apply |
| `install -d /etc/dso-integration`; write `/etc/dso-integration/environment` (env id + daemon id); `chown root:root`; `chmod 0644` | always | the root-owned, non-world-writable marker is the trust anchor the runner and the guard verify |

Review notes (current): the script needs `curl`, `gpg`, `dpkg`, `systemctl` and root; `--ci-ephemeral` only adds the GitHub-hosted-runner guard and changes nothing else; the pre-existing-container check cannot see containers of a *different* daemon, and volumes/networks that exist without containers are caught later by the runner's `foreign-resources-pre`; the marker is written after the daemon label is verified, so a failed run leaves no marker. This script was **not executed** in this review (static reading plus `bash -n` only).

Not done by the script (and not done by the runner): adding users to the `docker` group (an operator step, equivalent to root on the VM), installing Go/cosign/jq, firewall configuration. Observations: the marker is world-readable by design (IDs are not secrets); `--install-docker` downloads over HTTPS from Docker's site, so the run needs outbound network; nothing in the bootstrap reverts changes - destroy the VM.

## What this plan does not claim
Passing the VM run would be the first real-Docker evidence for M0-02/03/04/07/08/09/11/13. It would not establish zero downtime beyond the tested conditions, survival of long-lived connections (not measured), behaviour under host power loss, or production readiness. The runner has been syntax-checked and its pure logic self-tested only; the Docker-dependent step bodies (image inspection, tmpfs/health/network/compose checks, finalisation against a real daemon) have not been executed and may need adjustment on first run in the VM.

## Embedded frontend check

Why: the `Dockerfile` has no Node stage; the image serves whatever `internal/webui/assets/` held in the transferred source (`go:embed`). A source tree whose assets are stale, or missing a chunk (for example the `secrets/` chunk directory hidden by `.gitignore`'s `secrets/` rule), builds fine and only fails in the browser. `run-all.sh` has no step for this today (steps `image-build` and `image-content-inspection` only list layer contents), so these are manual, read-only checks on the VM. They do not start a container and need no flags beyond what `run-all.sh` already uses; starting the web UI itself needs `webui.enabled` config plus a reachable Docker socket for config validation (`pkg/config/config.go`), which is not exercised here.

1. Before the run, on the VM in the transferred tree (no Docker needed): `go test -count=1 ./internal/webui/...` must pass; `TestEmbeddedAssets_EveryReferencedChunkIsEmbeddedAndServed` fails if any `/_next/static/...` file referenced by an embedded `*.html` is absent from the embedded filesystem.
2. After `image-build`, extract the shipped binary the same way `run-all.sh` does for the hash check (`dk run --rm "${LBL[@]}" --network none --entrypoint cat "$IMAGE" /usr/local/bin/docker-dso > "$W/docker-dso"`, run-all.sh:257), or read the artifact copy under `evidence/`, then confirm every chunk the pages reference is embedded in that binary:
   `cd <source tree> && for n in $(grep -rhoE '/_next/static/[A-Za-z0-9_@~./-]+\.(js|css)' internal/webui/assets/*.html | sort -u | sed 's#^/##'); do grep -qaF "$n" "$W/docker-dso" || echo "MISSING from image binary: $n"; done` expects no output; also `grep -qaF 'chunks/app/secrets/page-' "$W/docker-dso"` expects success.
3. Record the sha256 of `internal/webui/assets/index.html` and `secrets.html` from the transferred tree next to the image digest in the evidence, so a later asset regeneration can be tied to the image that was tested.

## Phase A — N19/N12/N16 diagnostic run (exact commands)
Status: **every command in this section is NOT YET RUN.** Nothing here was executed against Docker; the new test files were only compiled (`go vet -tags dockerint ./test/...`, `go test -tags dockerint -list 'TestN19Diag' ./test/docker`) and the runner logic only exercised by `scripts/integration/selftest.sh` (no daemon). Interfaces are cited as file:line from the files as they stand in the working tree that adds the diagnostic.

**Purpose.** Finding N19 (owner: release blocker until runtime verification): file-mode managed delivery uses `rotation.StreamSecretToContainer` (the Docker archive endpoint) on created-not-started containers whose `/run/secrets/dso` is a declared tmpfs (`internal/rotation/rolling_strategy.go:329`, `internal/watcher/controller.go:1679,2394`). Docker Engine source (v28.5.2 `setupMounts` skips tmpfs) suggests the file lands in the writable layer and is hidden once the tmpfs mounts. This is **source-supported, not observed** (docs/audit/40). Phase A reproduces or disproves it with unique synthetic canaries, and runs the `docker dso inject` security scenarios (N12) and, once written, the N16 ownership scenarios. Design and verdict logic: `docs/audit/42-N19-DIAGNOSTIC-PLAN.md` (git-ignored).

### A.0 The baseline archive does NOT contain the diagnostic
- The archive in `~/dso-vm-transfer-4134b4c` (tarball sha256 `432c5853aca0e3a59435544c1db7e8d0fef17d3160bbd67bd365083cd550b2f9`, tree `933b67692e38d244a1c3f94fff7a70bf0094aa53`, commit `4134b4c8a330061b612a08a40bc611798358932e`) is the **frozen candidate**. It has **no** `scenario-n19-diagnostic` step, no `test/docker/n19_diag_*_test.go`, no `m0_n19_*` functions and no `n19_verdict` field. **Running Phase A from it is impossible.**
- The branch head is `443880d` (= `4134b4c` + a CI race-test timeout change). It does not contain the diagnostic either.
- Phase A therefore needs a **NEW archive** of a commit that contains the uncommitted changes (`test/docker/n19_diag_helpers_test.go`, `n19_diag_basic_test.go`, `n19_diag_managed_test.go`, and the edits to `scripts/integration/{run-all.sh,m0-lib.sh,m0-finalize.sh,selftest.sh}` and this document). That archive has NOT been generated by the author of this section. The **owner** decides whether to commit it and whether the successor commit is a **"diagnostic candidate"**: a commit whose only purpose is to run the diagnostic. It is **not release-approved**, does not supersede the frozen candidate, and a green run of it approves nothing (see A.8).
- Pins for the diagnostic archive are placeholders until the owner creates it: `<DIAG_SHA>`, `<DIAG_TREE>`, `<DIAG_TARBALL_SHA256>`, `<DIAG_SHA7>`, `<DIAG_SHA12>`. Record them on a second channel exactly as in section 1A.

### A.1 Generate the diagnostic archive (dev host; AFTER the owner commits; NOT YET RUN)
```bash
cd /data/umair_atr1123/All_Data/Antigravity_Work/dso
git status --porcelain --untracked-files=all          # must print nothing (make-source-archive.sh:39 refuses otherwise)
SHA=$(git rev-parse HEAD)                              # <DIAG_SHA>, write it down out-of-band
scripts/integration/make-source-archive.sh "$SHA" "$HOME/dso-vm-transfer-${SHA:0:7}"   # usage: make-source-archive.sh:28; outdir must be outside the work tree (:44)
git rev-parse "$SHA^{tree}"                            # <DIAG_TREE>
( cd "$HOME/dso-vm-transfer-${SHA:0:7}" && sha256sum -c SHA256SUMS && sha256sum dso-${SHA:0:12}.tar.gz )   # <DIAG_TARBALL_SHA256>
```
`make-source-archive.sh` refuses a dirty tree, a short or unknown SHA, an output directory inside the work tree and an archive that does not reproduce the tree hash; it must not be run before the owner commits.

### A.2 Transfer and verify (NOT YET RUN)
Same procedure as section 1A steps 2-6 with the `<DIAG_*>` pins: `sha256sum -c SHA256SUMS`; `echo "<DIAG_TARBALL_SHA256>  dso-<DIAG_SHA12>.tar.gz" | sha256sum -c`; hardened `scp` with a pinned host key; extract into an empty directory (`tar -xzf ... --no-same-owner`, `test ! -e .git`); `m0_check_source_provenance` dry check expecting `<DIAG_SHA>` / `<DIAG_TREE>`.

### A.3 VM prerequisites (NOT YET RUN)
- Fresh disposable Ubuntu 24.04 x86_64 VM, 4 vCPU / 16 GB RAM / 80-100 GB disk, no public ports, SSH only from the operator address, synthetic data only. Never the development machine or its daemon.
- **Rootful Docker Engine at a verified patched version >= 29.5.1** (docs/audit/30 cites the `docker-v29.5.1` release notes; the exact fixed version for every advisory is for the owner to confirm). Pin it explicitly: `bootstrap-test-vm.sh --install-docker` requires `DOCKER_APT_VERSION` (bootstrap-test-vm.sh:43-44) and installs `docker-ce=${DOCKER_APT_VERSION}`, `docker-ce-cli=${DOCKER_APT_VERSION}` (:58). The script adds the apt repository itself, so the exact string cannot be discovered through it: obtain it beforehand (on a separate scratch VM after adding Docker's apt repository: `apt-cache madison docker-ce | grep 29.5.1`; the form is `5:<version>-1~ubuntu.24.04~noble`; this string was NOT verified here) or pre-install Docker by hand at that version and run the bootstrap **without** `--install-docker`.
- After install, verify (NOT YET RUN): `docker version --format 'server={{.Server.Version}} api={{.Server.APIVersion}}'` (server must be >= 29.5.1) and `docker info --format 'driver={{.Driver}} status={{json .DriverStatus}}'`. **Record the storage driver and whether the containerd snapshotter is in use** (`DriverStatus` contains `driver-type io.containerd.snapshotter.v1`): N19 is a storage-layout question and the answer is specific to the driver. The evidence lines record both again (`storage_driver`, `snapshotter`).
- Pin the exact version in the run so a wrong Engine fails the run (run-all.sh:160-161, exact string equality with `.Server.Version`): `DSO_IT_EXPECT_DOCKER_VERSION=<exact server version printed above>`.

### A.4 Capacity and toolchain preflight (NOT YET RUN)
```bash
nproc; free -m; df -h /var/lib/docker /var/tmp                         # >= 4 CPU, >= 16 GB RAM, >= 40 GB free under /var/lib/docker
go version                                                              # must equal go.mod "toolchain go1.26.11" (GOTOOLCHAIN=local; run-all.sh s_go_toolchain)
for t in git jq go timeout sha256sum tar awk sed ss gcc cosign; do command -v "$t" >/dev/null || echo "MISSING: $t"; done   # run-all.sh pre-flight list (+ gcc for -race, cosign for installer-tests)
env | grep -E '^(DOCKER_|COMPOSE_)'                                     # must print nothing (m0_check_no_docker_env)
id; docker ps -aq                                                       # user is in the docker group; daemon has ZERO containers (bootstrap-test-vm.sh:65-67)
```

### A.5 Bootstrap (root; once per VM; NOT YET RUN)
```bash
cd ~/dso-run/dso-<DIAG_SHA12>
sudo env DSO_BOOTSTRAP_DISPOSABLE_VM=yes ./scripts/integration/bootstrap-test-vm.sh                 # Docker pre-installed at the pinned version
# or, installing Docker through the script (Ubuntu 24.04 only):
sudo env DSO_BOOTSTRAP_DISPOSABLE_VM=yes DOCKER_APT_VERSION='<exact apt string>' ./scripts/integration/bootstrap-test-vm.sh --install-docker
cat /etc/dso-integration/environment                                                                # DSO_TEST_ENVIRONMENT_ID=env-..., DSO_TEST_DAEMON_ID=...  (note the environment id)
```
Preconditions are the ones in section 1A step 7 (`DSO_BOOTSTRAP_DISPOSABLE_VM=yes` :36, root :37, zero containers :65-67, no existing marker :70, no non-empty `/etc/docker/daemon.json` :74). A second bootstrap is refused.

### A.6 Run the diagnostic (NOT YET RUN)
`run-all.sh` has **no step-selection mechanism** (it always runs every step; no flag or variable selects a subset; the step list is run-all.sh:39-50 and the run body starts at the `gate environment-identity` line). Do not invent one. Two supported ways:

**A.6.a Targeted run (recommended first: it answers N19, N12 and the evidence question in minutes, without the image build).** Directly with `go test`, using exactly the guard interface that `scripts/integration/run-integration.sh:25-31` uses (`test/dockerguard/guard.go:41-46`): the marker is sourced for `DSO_TEST_ENVIRONMENT_ID` / `DSO_TEST_DAEMON_ID`, the run id is generated, and cleanup is scoped to the run labels.
```bash
cd ~/dso-run/dso-<DIAG_SHA12>
env | grep -E '^(DOCKER_|COMPOSE_)'                                     # prints nothing
set -a; . /etc/dso-integration/environment; set +a                       # DSO_TEST_ENVIRONMENT_ID, DSO_TEST_DAEMON_ID (run-integration.sh:25)
export DSO_INTEGRATION_TESTS=1 DSO_TEST_DOCKER_HOST=unix:///var/run/docker.sock GOTOOLCHAIN=local
export DSO_TEST_RUN_ID="dso-it-$(head -c 6 /dev/urandom | od -An -tx1 | tr -d ' \n')"     # must match dso-it-<8-16 hex>
export DSO_IT_SENTINEL="dso-it-sentinel-$(head -c 8 /dev/urandom | od -An -tx1 | tr -d ' \n')"   # optional: the canaries derive from it; the leak scan below greps for it
mkdir -p ~/phaseA
# 1. N19 diagnostic: 14 tests (A-M, R), each records N19-EVIDENCE lines and is expected to PASS even when it demonstrates the failure
go test -json -tags dockerint -count=1 -timeout 40m -run '^TestN19Diag_' ./test/docker >~/phaseA/n19.json 2>~/phaseA/n19.err; echo "go test exit: $?"
# 2. N12: the 18 standalone `docker dso inject` scenarios (audit 38); must all RUN and pass
go test -json -tags dockerint -count=1 -timeout 30m -run '^TestInjectSecurity_' ./test/docker >~/phaseA/inject.json 2>~/phaseA/inject.err; echo "go test exit: $?"
# 3. scoped cleanup of THIS run only (cleanup-integration.sh:7-12; no prune)
./scripts/integration/cleanup-integration.sh
docker ps -a --filter "label=dso.test.run=$DSO_TEST_RUN_ID" -q | wc -l        # must print 0
# 4. extract and judge the evidence with the runner's own pure functions (sourcing m0-lib.sh has no side effects)
bash -c 'source scripts/integration/m0-lib.sh
  m0_n19_extract ~/phaseA/n19.json > ~/phaseA/n19-evidence.jsonl
  m0_n19_validate ~/phaseA/n19-evidence.jsonl; echo "validate exit: $?"
  echo "n19_verdict: $(m0_n19_verdict ~/phaseA/n19-evidence.jsonl)"'
# 5. leak scan: the canary must appear nowhere in the captured output
grep -c "$DSO_IT_SENTINEL" ~/phaseA/n19.json ~/phaseA/n19.err ~/phaseA/inject.json ~/phaseA/inject.err        # expect 0 for each
```
The `go test` exit status of step 1 must be 0 and the JSON must show every `TestN19Diag_*` as `pass` (a `skip` means the guard was not opted in; a missing test means the pattern matched less than the 14 expected). Count with: `jq -r 'select(.Action=="pass" and .Test!=null and (.Test|startswith("TestN19Diag_")) and (.Test|contains("/")|not)) | .Test' ~/phaseA/n19.json | sort -u | wc -l` (expect 14). Note the direct path does not perform the runner's marker-ownership and `DSO_IT_DISPOSABLE_VM_CONFIRM` checks (the guard still verifies daemon id, label and absence of foreign containers).

**A.6.b Full runner (required for the evidence package; includes scenario-n19-diagnostic and scenario-inject).** Section 1A step 8 with the diagnostic archive. The new step `scenario-n19-diagnostic` (run-all.sh, after `scenario-inject`) runs `go test -json -tags dockerint -timeout ${DSO_IT_N19_GO_TIMEOUT:-40m} -run '^TestN19Diag_' ./test/docker`, requires every one of the 14 named tests to run and pass (ALLOW_SKIP=0, so a skip makes the step `skipped`, never `passed`), captures the evidence lines to `evidence/n19-evidence.jsonl` and summarises them in `summary.json` under `n19_evidence`.
```bash
cd ~/dso-run/dso-<DIAG_SHA12>
env | grep -E '^(DOCKER_|COMPOSE_)'                                     # prints nothing
DSO_IT_SOURCE_PROVENANCE="$HOME/transfer/provenance.json" \
DSO_IT_EXPECT_SHA=<DIAG_SHA> \
DSO_IT_EXPECT_DOCKER_VERSION=<exact server version from A.3> \
DSO_IT_DISPOSABLE_VM_CONFIRM=<environment id from /etc/dso-integration/environment> \
DSO_IT_N19_GO_TIMEOUT=40m \
./scripts/integration/run-all.sh 2>&1 | tee ~/run-all.console.log
```
Variables: `DSO_IT_SOURCE_PROVENANCE`/`DSO_IT_EXPECT_SHA` (m0-lib.sh m0_resolve_source), `DSO_IT_EXPECT_DOCKER_VERSION` (run-all.sh:160), `DSO_IT_DISPOSABLE_VM_CONFIRM` (:149), `DSO_IT_N19_GO_TIMEOUT` (new, at the `scenario-n19-diagnostic` step), `DSO_IT_GOTEST_TIMEOUT_S` (:222, default 2700; the outer `timeout` for each go test step must exceed `DSO_IT_N19_GO_TIMEOUT`, 40m = 2400 s < 2700 s). Exit codes are unchanged (0 automated PASS, 1 FAIL, 2 INCOMPLETE, 3 refused). **A confirmed N19 does not fail the step or change the exit code**: it sets `n19_verdict: confirmed`, adds findings, and keeps `release_readiness` INCOMPLETE.

### A.7 Standalone `docker dso inject` (N12) and N16 ownership scenarios in Phase A
- **N12, 18 scenarios, implemented:** `TestInjectSecurity_01_ManagedContainerVerifiedTmpfs` ... `TestInjectSecurity_18_PatchedEngineNormalBehaviour` (`test/docker/inject_security_test.go`). Run in A.6.a step 2 and, in the full runner, by step `scenario-inject` (run-all.sh `INJECT_TESTS`, requires all 18 by name). Not-yet-run.
- **N16 ownership, 10 scenarios, NOT YET IMPLEMENTED as tests.** docs/audit/41 section 5 defines them; they are source analysis only. They are added to the Phase A list as **to be added after the N16 review is accepted and the proposed correction (if any) is implemented**; until then they cannot be run. Scenarios (fixture: `tmpfs: [/run/secrets/dso]`, running container, shell image, throw-away secrets): (1) root reads; (2) non-root numeric UID reads; (3) non-root with explicit GID reads; (4) wrong identity cannot read; (5) unrelated user cannot read; (6) ownership preserved across repeated injection; (7) correct immediately after publication (no observable interim state); (8) correct after rotation; (9) delivery failure leaves no less-restrictive file; (10) destination stays on verified tmpfs (layer, bind, named volume, volume under tmpfs, tmpfs under volume: refusal except true tmpfs). Existing tests that touch the same ground and may be partly reusable once mapped: `TestInjectSecurity_12_NonRootFileAccess`, `_13_RestrictivePermissions`, `_14_PartialInjectionFailure`, `_15_RepeatedInjection`. Treat that mapping as unverified.

### A.8 Evidence retrieval and what to look for (NOT YET RUN)
Retrieve as in section 1A step 9 (`sha256sum -c SHA256SUMS` in the artifact directory, tar, hardened `scp` back, re-verify locally). For A.6.a copy `~/phaseA/` the same way. Then, in the **fresh local directory** (never inside the repo):
```bash
jq '{n19_verdict, automated_integration, release_readiness, n19: .n19_evidence | {verdict, evidence_valid, lines, scenarios, docker_server, storage_driver, snapshotter}}' summary.json
jq -r '.findings[] | select(startswith("n19_verdict") or startswith("N19"))' summary.json
jq -r '.scenarios[] | select(.name=="scenario-n19-diagnostic" or .name=="scenario-inject") | [.name,.status,.note] | @tsv' summary.json
jq -c 'select(.scenario=="B" or .scenario=="D" or .scenario=="E" or .scenario=="I" or .scenario=="K") | {scenario,observation,location,lifecycle_state,file_visibility,canary_integrity,layer_inspection}' evidence/n19-evidence.jsonl
```
How to read it (the hypothesis predicts the left column; each line is evidence, the verdict is only a summary):

| Question | Hypothesis N19 predicts | Evidence line |
|---|---|---|
| Does the archive inject into a created-not-started container reach the tmpfs? | No: it lands in the layer | B `layer_read_unstarted` (`location layer`, `canary_integrity match`); C `diff_before_start` lists the file |
| Is it visible after start? | No (hidden by the tmpfs) | D `running_tmpfs_after_start` `absent` and mountinfo `fstype=tmpfs`; E `application_view` `absent` |
| Does the plaintext persist in the layer? | Yes | I `hypothesis_path_after_stop` `match`; H `stopped_layer_read` `match`; I control `control_exec_inject_then_stop` `absent` (the exec path leaves nothing) |
| Archive into a running tmpfs | may land in the layer or be refused | F `running_tmpfs_after_archive_inject`, `diff_after_running_inject` |
| Hardened exec injector | file in tmpfs, nothing in layer | G `running_tmpfs_after_exec_inject` `match`, `diff_after_exec_inject` empty |
| Managed rolling rotation, file mode | replacement lacks the file; the post-start probe rejects it (fail closed on hand-over) | K `rotation_outcome` (`error: ... file-mode secret probe failed`), `replacement_file_state` `absent`, `original_after_rotation` |
| Managed recreate rotation | same | L (same lines) |
| Is the readiness gate fooled? | health gate passes (`saw_healthy=true`), the file probe is what rejects | M `gate_vs_secret_presence` |

`n19_verdict` (computed by `m0_n19_verdict` from scenarios B, C, I (layer lines) and D, E, K (running lines); R, F, G, H, J, L, M are reported but do not feed it): `confirmed` = the canary digest is present on the layer at all; `not_confirmed` = present in the running tmpfs, absent from the layer, no running view absent; `inconclusive` = anything else, including missing or malformed evidence (which also fails the step). **`release_readiness` requires `n19_verdict == not_confirmed`**; it is never `PASS` from the runner in any case. A `not_confirmed` result holds for that Engine and storage driver only. Check `n19_evidence.storage_driver` / `snapshotter` and, if the target deployments differ, repeat on them. An unexpected result (for example D `match` and I `match`: plaintext in both) is a finding, not an error of the diagnostic.

### A.9 Crash-recovery interaction (scenario N, read-only description; not tested here)
If the agent is killed between candidate creation and start, a created-not-started replacement can exist with the archive-injected file in its writable layer. The recovery path (`internal/rotation` journal; tests `TestRecovery_RealDaemon_*` and the real-process `TestCrash*` matrix in `docs/testing/REAL-PROCESS-CRASH-TESTS.md`) removes unacknowledged candidates; whether the plaintext layer copy is removed with the container, and how long it exists, is **not** measured by Phase A and is not duplicated here. If A.8 shows a layer copy (verdict `confirmed`), add the question "is a layer copy ever left behind after crash recovery" to the crash-matrix review.

### A.10 Teardown
Destroy the VM through the provider after the evidence is verified locally (external prerequisite; section 1A step 10). The diagnostic leaves nothing on the daemon (each test removes its own containers by label and records the result in its `cleanup` evidence line; the runner's ownership-scoped `cleanup` step and `foreign-resources-post` verify it). Local, never in the repo: the evidence archive, which contains no canary value (only sha256 digests, sizes, modes, names, and the Engine/storage identity).
