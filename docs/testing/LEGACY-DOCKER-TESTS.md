# Legacy Docker tests: migration inventory (M0-11)

## Status

All Docker tests that used to live behind the build tag `dockerint_legacy` (they created and
removed containers through `testcontainers` or a `client.FromEnv` client, i.e. on whatever
daemon the environment or default socket selected) have been migrated to the isolation guard
(`test/dockerguard`) or, where they never needed Docker, turned into ordinary unit tests. The
legacy files were deleted. **Nothing remains under `dockerint_legacy`**, and no references to
that tag remain in the Makefile, CI workflows, scripts or docs.

**Compiled is not verified.** Every migrated Docker test was compiled/vetted only. None has
been executed, because no isolated daemon exists yet (the dev daemon must not be used). Their
assertions are therefore *unproven against a real engine*: some may fail on first run
(timing, image behaviour, daemon version differences) and that would be a finding, not a
regression of this migration. The only tests in this inventory that were executed are the
Docker-free unit tests (marked below).

Migration pattern (as in `test/docker/scenarios_test.go`): package `docker`, tag `dockerint`,
client via `dockerClient(t)` / `dockerguard.Current(t)`, containers created with `docker run`
plus `Session.LabelArgs()` (or `ContainerCreate` with `Session.Labels()`), unique names via
`uniqueName`, cleanup by run label (`cleanupByLabel`), pinned `busybox:1.36.1`. No
`testcontainers`, no `client.FromEnv`, no fixed container names, no prune.

Commands used for "compiled":

- C1 = `go vet -tags dockerint ./test/... ./internal/...`
- C2 = `go vet -tags dockerint_legacy ./test/... ./internal/agent/...` (now matches no files)
- C3 = `go build ./... && go vet ./...`
- U = `go test -race -run 'TestStress_|TestRace_' ./internal/watcher ./internal/agent` (executed; Docker-free)

All of C1, C2, C3 exit 0.

## Per-test inventory

Executed column: **NO — no isolated daemon** for anything needing Docker.

### test/integration/rotation_test.go -> test/docker/legacy_rotation_test.go

| Original test | Original purpose | New guarded test | Coverage retained | Coverage expanded | Coverage removed | Compiled | Executed |
|---|---|---|---|---|---|---|---|
| `TestRotation_BlueGreenSwap_Success` | rolling swap of a healthy container succeeds | `TestRotation_BlueGreenSwap_Success` | `Execute` returns nil | name now held by a NEW running container, original id gone, rotated env var present | none | yes (C1) | NO — no isolated daemon |
| `TestRotation_FileModeSecret_NeverInDockerInspect` | P0: file-mode secret never in `docker inspect`, but delivered to the filesystem | `TestRotation_FileModeSecret_NeverInDockerInspect` | sentinel absent from raw inspect JSON, `Config.Env`, `Config.Cmd`, `Config.Labels`; present in `/run/secrets/dso/db-password` | `Config.Entrypoint` checked (original doc listed it, code did not); pre-rotation inspect also checked; non-secret env applied; failure messages never contain the value (original printed the file content on mismatch and the env entry on leak) | none | yes (C1) | NO — no isolated daemon |
| `TestRotation_Concurrent_MultipleContainers` | 3 concurrent rotations are safe | `TestRotation_Concurrent_MultipleContainers` | zero failures across 3 concurrent rotations | each name replaced, carries its own `ROTATION_ID` | none | yes (C1) | NO — no isolated daemon |
| `TestRotation_HealthCheck_Timeout` | rotation of a container without a HEALTHCHECK | `TestRotation_HealthCheck_Timeout` | stated expectation (rotation succeeds on process stability) | now a hard assertion: the original only logged either outcome and could not fail; container is replaced | none (the original asserted nothing) | yes (C1) | NO — no isolated daemon |
| (same subject, new) | candidate that never becomes healthy | `TestRotation_HealthCheck_Timeout_FailingHealthcheckRollsBack` | n/a (new) | failing candidate hits the 5s health timeout, rotation errors, original keeps id/name/running, no leftover candidate | n/a | yes (C1) | NO — no isolated daemon |
| `TestRotation_EventDriven_Detection` | container stop is observable | `TestRotation_EventDriven_Detection` | running before stop; stop attempted (error only warned); not running after | despite its name the original observed no event; now a daemon event subscription opened before the stop must deliver the container `die` event | none | yes (C1) | NO — no isolated daemon |

Deviation applying to the concurrent tests: the originals shared one `RollingStrategy`
between goroutines. A strategy holds per-`Execute` state (`rid`) and production builds one per
rotation (`internal/watcher/controller.go`), so the migrated tests build one per goroutine.
Image: `alpine:latest` -> pinned `busybox:1.36.1`; containers are named uniquely and labelled.

### test/integration/race_test.go

| Original test | Original purpose | New test | Coverage retained | Coverage expanded | Coverage removed | Compiled | Executed |
|---|---|---|---|---|---|---|---|
| `TestRace_ContainerClone_ConcurrentMutations` | config cloning prevents shared-slice races | `test/docker/legacy_race_test.go: TestRace_ContainerClone_ConcurrentMutations` | 10 goroutines x (inspect + append to Env); any inspect error fails | the REAL `ContainerCloner.PrepareShadowConfig` is called concurrently with distinct env; no cross-leak; original config unchanged (run with `-race`) | none. Note: the original never called the cloner, so it could not catch the defect it names | yes (C1) | NO — no isolated daemon |
| `TestRace_DebounceWindow_ConcurrentUpdates` | debounce window must not extend on duplicates | `internal/watcher/stress_legacy_test.go: TestRace_DebounceWindow_ConcurrentUpdates` (no Docker, no tag) | first record fresh; fresh again after 110ms | duplicates inside the window must all be duplicates | none. The original tested a private COPY of the debouncer defined in the test file; the new one tests the real `EventDebouncer` | yes (C1, C3) | **YES** (U, `-race`, pass) |
| `TestRace_ContainerRename_AtomicSwap` | rename-based swap | `test/docker/legacy_race_test.go: TestRace_ContainerRename_AtomicSwap` | final name of the new container is the base name | base name resolves to new container, backup name to original, both running. Fixed names `atomic-test*` and the pre-run removal of those names are gone | none | yes (C1) | NO — no isolated daemon |
| `TestRace_SocketPermission_Concurrent` | agent socket permissions under concurrency | RETIRED from `test/docker` (no skip remains). Replaced by `internal/agent/socket_permission_race_test.go: TestUmaskTightenRestorePairsAreSerialised`, `TestConcurrentSocketCreationNeverExposesWiderMode` (no Docker, no tag, run with `-race`) | n/a: the original asserted nothing (`t.Log` only) and always "passed" | The contract it pointed at is real: agent sockets are created under a process-global `umask(077)` then `chmod`'d (`internal/agent/server.go: StartSocketServer/StartDriverServer`). `tightenUmask/restoreUmask` was an unsynchronised read-modify-write: concurrent creators could restore a wider umask under another creator's `net.Listen`, creating a socket 0755 until its `chmod`. Fixed with a lock held across the tighten/restore pair (`internal/agent/umask_linux.go`); the first test fails on the old code (verified) and passes now; the second asserts no concurrently created socket is ever observable with an "other" bit or group-execute and that final modes are 0600/0660 and the umask is restored | yes (C1) | **YES** (U, `-race`) |

### test/integration/shadow_clone_network_test.go

| Original test | Original purpose | New guarded test | Coverage retained | Coverage expanded | Coverage removed | Compiled | Executed |
|---|---|---|---|---|---|---|---|
| `TestShadowClone_DoesNotDuplicateNetworkIdentity` | shadow clone must not copy MAC/IP/EndpointID; original undisturbed while clone runs | `test/docker/legacy_shadow_clone_network_test.go: TestShadowClone_DoesNotDuplicateNetworkIdentity` | all assertions: no MacAddress/IP/EndpointID in the shadow network config, no deprecated container MAC, running clone does not share the original MAC, >=100 requests and zero failures to the original during the clone | clone IP must differ from the original's | none. Workload nginx -> busybox httpd (only "listens on 80" mattered); traffic uses the shared generator (new connection per request, 2 workers, as in the traffic file) instead of the deleted `hammerProxy` | yes (C1) | NO — no isolated daemon |

### test/integration/stress_test.go

| Original test | Original purpose | New test | Coverage retained | Coverage expanded | Coverage removed | Compiled | Executed |
|---|---|---|---|---|---|---|---|
| `TestStress_ConcurrentRotations` | 5 simultaneous rotations | `test/docker/legacy_stress_test.go: TestStress_ConcurrentRotations` | all 5 succeed | each name replaced, own `ROTATION_NUM`, original ids gone; one `RollingStrategy` per rotation (see above) | none | yes (C1) | NO — no isolated daemon |
| `TestStress_EventDebouncer_RapidFire` | debouncer under rapid events | `internal/watcher/stress_legacy_test.go: TestStress_EventDebouncer_RapidFire` (no Docker, no tag) | unchanged (>=90% of the intended 80% duplicates detected) | none | none | yes (C1, C3) | **YES** (U) |
| `TestStress_EventQueue_Backpressure` | event queue backpressure | `internal/watcher/stress_legacy_test.go: TestStress_EventQueue_Backpressure` (+ `..._StopReleasesBlockedSubmitter`) | n/a: the original was a placeholder that asserted nothing | real test of the bounded `WorkerPool`: Submit blocks when worker busy and queue full, is admitted when capacity frees, no job dropped; `Stop` releases a blocked submitter | none | yes (C1, C3) | **YES** (U) |
| `TestStress_ConcurrentCacheAccess` | cache under concurrent load | `internal/agent/stress_legacy_test.go: TestStress_ConcurrentCacheAccess` (no Docker, no tag) | operation count assertion unchanged | targets the real `agent.SecretCache` (the original used a mock defined in the test file, i.e. tested nothing in the product); values complete after the storm | none | yes (C1, C3) | **YES** (U) |
| `TestStress_SecretZeroization_UnderLoad` | secrets removed/zeroized | `internal/agent/stress_legacy_test.go: TestStress_SecretZeroization_UnderLoad` (no Docker, no tag) | all 1000 secrets gone after Delete | real `SecretCache`; the backing byte buffers captured before Delete are actually zero afterwards (the original's name promised this but only checked map absence on a mock) | none | yes (C1, C3) | **YES** (U) |

### internal/agent/notification_integration_test.go

| Original test | Original purpose | New guarded test | Coverage retained | Coverage expanded | Coverage removed / changed | Compiled | Executed |
|---|---|---|---|---|---|---|---|
| `TestNotificationIntegration_RealRotationDeliversWebhook` | a real rotation via `TriggerEngine.ExecuteRotation` delivers a real webhook | `test/docker/legacy_notification_test.go: TestNotificationIntegration_RealRotationDeliversWebhook` | seed `rotation_failed` delivered; real rotation yields `rotation_succeeded`; secret/provider metadata; `affected_containers == [container id]` | label discovery (owner gate) instead of a hand-stored target; the application demonstrably loads the new value; payload does not leak the synthetic value | **Changed, not removed:** the original used an env-mode secret with the signal strategy. Since M0-03 that combination is rejected by design, so a faithful copy would now see `rotation_failed`. The migrated test uses the supported shape (file-mode secret to a non-root tmpfs, then SIGHUP) | yes (C1) | NO — no isolated daemon |

Production change required: the test lives outside package `agent` and cannot reach
`TriggerEngine`'s unexported context, and `NewTriggerEngine` hard-codes `/var/lib/dso/...`.
`internal/agent/trigger.go` gained `NewTriggerEngineWithDirs(..., stateDir, lockDir)`;
`NewTriggerEngine` now delegates to it with the unchanged default paths (behaviour-preserving).

### Already migrated earlier: test/integration/proxy_rotation_downtime_test.go -> test/docker/traffic_rotation_test.go

The legacy file was deleted in this change (it was still present). Traffic clients open a NEW
connection per request: this measures availability of new connections, **not** survival of
existing long-lived connections, and a passing run is evidence for this image/daemon/load, not
a general zero-downtime guarantee.

| Original test | Original purpose | New guarded test | Coverage retained | Coverage expanded | Coverage removed | Compiled | Executed |
|---|---|---|---|---|---|---|---|
| `TestProxy_RollingRotation_ZeroDowntime` + `TestProxy_RollingRotation_ConcurrentClients` | no failed requests across a rolling rotation (single and 20 concurrent clients) | `TestTraffic_RollingRotation` | rotation succeeds; baseline clean; zero failures; enough requests | 20-worker load; per-phase stats (pre/during/post), failure kinds, status codes, latency percentiles; response proves WHICH instance answered, post-rotation traffic must come from a new instance | the separate single-client variant is folded into the 20-client run | yes (C1) | NO — no isolated daemon |
| `TestProxy_RollingRotation_HealthCheckFailure_OldKeepsServing` | failed candidate readiness: old keeps serving | `TestTraffic_CandidateReadinessFailure` | rotation fails; zero failures; original still exists and runs | only the original identity ever answered; no leftover running candidate | none | yes (C1) | NO — no isolated daemon |
| `TestProxy_SteadyState_NoRotation_Control` | control: no rotation, no failures | `TestTraffic_SteadyStateControl` | zero failures, enough requests | exactly one backend identity | none | yes (C1) | NO — no isolated daemon |
| `TestProxy_RestartRotation_ZeroDowntime` | restart strategy availability | `TestTraffic_RestartRotation` | rotation succeeds, failures counted | per-phase report; the restart strategy is explicitly NOT claimed zero-downtime | none | yes (C1) | NO — no isolated daemon |
| (new) | rolling on a directly published port must be rejected before mutation | `TestTraffic_UnsupportedDirectPublishing` | n/a | rotation errors, original id/running unchanged, container set unchanged, one serving instance | n/a | yes (C1) | NO — no isolated daemon |

### Untagged files left in test/integration (no Docker use)

`aws_test.go` (needs real AWS credentials, self-skips; a comment mentions testcontainers but
no code uses it), `longrun_stability_test.go`, `shutdown_test.go`. Unchanged.

## Remaining ambient-daemon usage

Under any test tag there is none: no `testcontainers` import and no `client.FromEnv` in any
`_test.go` file. `client.FromEnv` still appears in PRODUCTION code (`internal/cli`,
`internal/watcher`, `internal/injector`); that is intended, and the guard points those at the
verified endpoint by exporting `DOCKER_HOST` and clearing context/TLS variables
(`dockerguard.Main`). The Go module still lists `github.com/testcontainers/testcontainers-go`
in `go.mod` although nothing imports it; `go mod tidy` should drop it (not done here).

## Guarded tests added earlier in M0 (also never run against a real daemon)

`test/docker/signal_rotation_test.go` (file-mode signal reload with a non-root app; ignored
signal pinning), `test/docker/recovery_test.go` (post-crash layouts through real recovery).
Recovery after a real process kill remains unverified.
