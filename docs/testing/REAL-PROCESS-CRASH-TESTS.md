# Real-process crash-recovery tests (M0-04 / M0-11)

Status: **compiled and vetted (`-tags dockerint`, `-tags dsofault`); the Docker part
has NOT been executed.** The Docker-free orchestration (process control, fault
selection, sentinel scanning, invariant checks, availability analysis, report
assembly) is unit-tested and the agent boot / self-kill chain was smoke-tested against
the real agent binary with Docker unreachable. No claim in this document is a
measured crash-recovery result against a real daemon: that run is for the disposable
VM (see `DISPOSABLE-VM-EXECUTION-PLAN.md`).

Files:

| file | role |
|---|---|
| `internal/faultinject/` | the test-only hook (two build-tagged files) |
| `test/harness/` | Docker-free helper package, ordinary unit tests (`go test -short ./...`) |
| `test/docker/crash_recovery_test.go`, `crash_support_test.go` | the `dockerint` harness |

## 1. Production process topology (investigated first)

**The DSO proxy is embedded in the agent process. It is not a separate process, not a
separate container and not independently supervised.**

Evidence:

* `proxy.NewManager` has exactly one non-test call site: `internal/cli/agent.go`
  (`proxyManager := dsoProxy.NewManager(logger)`), inside `dso agent`. The same
  process then owns the REST server, the IPC socket, the Docker Secret Driver socket,
  the watcher/reloader, the trigger engine and the proxy (`reloader.ProxyManager =
  proxyManager`).
* The proxy listener is a goroutine of that process. `proxyManager.Stop(10s)` is
  part of the agent's graceful shutdown.
* The only supervisor is the systemd unit written by `internal/bootstrap/systemd.go`:
  `ExecStart=dso ... agent`, `Restart=on-failure`, `RestartSec=10`. It restarts the
  whole agent; there is no second unit for a proxy.
* `ScanAndRegister` (agent start) rebuilds the proxy's routing table from running
  containers labelled `dso.host_ports`: the routing table is in-memory state of the
  agent and is lost when it dies.

Consequences, stated plainly:

1. **Killing the agent kills the proxy listener.** Traffic to a proxy-fronted host
   port is interrupted for at least: agent death -> supervisor delay (systemd:
   `RestartSec=10`) -> process start -> provider plugin load -> `ScanAndRegister` ->
   startup recovery -> the listener binding again. The harness cannot, and does not,
   split the proxy out of the agent to make traffic pass.
2. This is an **architectural limitation**, not a defect of the harness. "Zero
   downtime" applies to an ORDINARY rotation (the old backend keeps serving until the
   new one is registered), never to unexpected agent death.
3. The workload containers are independent of the agent and keep running when it
   dies; only the path through the proxy is lost.

The reports therefore keep three measurements separate and never merge them:

| | what | how measured | expectation |
|---|---|---|---|
| (a) | availability during an ORDINARY rotation | `Control_OrdinaryRotation`: real agent, uninterrupted rolling rotation, 8 new-connection clients | reported; pre-rotation baseline must be clean |
| (b) | availability during UNEXPECTED agent death | generator keeps running across the SIGKILL; window = death -> first success after restart; also split by failure kind | **interrupted** (connection refused). The suite *fails* if no connection is refused while the agent is dead, because that would mean the topology above no longer holds |
| (c) | recovery time after restart | restart begin -> first success, restart begin -> 25 consecutive successes | reported |

Measurement (b) is **not** zero downtime and is never described as such. The harness
restarts the agent immediately (no `RestartSec`); a real deployment adds the
supervisor delay on top, so the harness window is a lower bound.
`Topology_AgentDeathKillsProxyListener` demonstrates it in isolation (SIGKILL by PID at
steady state: workload still `running healthy`, proxy port refuses, then recovers).

Only the rolling strategy is proxy-fronted in this harness (it is the strategy with a
proxy handoff). Restart and signal cases run against an unproxied workload and report
no availability numbers.

## 2. The real transition points

Docker tells recovery what exists; the transition journal (`internal/rotation/journal.go`,
persisted by `agent.StateTracker.Record`, atomically written and fsynced as
`rotations.json`) tells it what was intended. Intent is journalled before each
destructive step, the acknowledgement after. A crash between the two is "may or may not
have happened".

Recovery (`internal/agent/recovery.go`) inspects Docker as the authority, applies
journal rules (`journalPermits`, `liveRouting`) and preserves anything it cannot account
for. Existing in-process crash tests (`internal/agent/crash_injection_test.go`) already
drive these points with a panicking `FaultHook` against a fake daemon; this harness uses
the **same point names** against a real process and a real daemon.

## 3. The test-only fault hook

* **Build tag:** `dsofault`. Package `internal/faultinject`:
  * `faultinject.go` (`//go:build !dsofault`): stub. `Enabled = false`,
    `Hit(point) bool` returns `false`, `Param(name)` returns `("", false)`. The
    compiler removes the guarded calls.
  * `faultinject_dsofault.go` (`//go:build dsofault`): the hook.
* **No production surface:** no network API, no flag, no environment variable in an
  untagged build. The variable names exist only in the tagged file;
  `TestUntaggedBinaryHasNoFaultHookAndTaggedHasIt` builds both binaries and asserts the
  names are absent from the release binary and present in the tagged one.
* **Tagged contract (read once per process):**

  | variable | meaning |
  |---|---|
  | `DSO_FAULT_POINT` | exact point name to die at; unset/empty disables |
  | `DSO_FAULT_NTH` | die on the Nth hit of that point (default 1) |
  | `DSO_FAULT_MARKER` | file written (`point=`, `pid=`, `nth=`) immediately before dying |
  | `DSO_FAULT_STATE_DIR`, `DSO_FAULT_LOCK_DIR` | per-test state/lock directories (production uses fixed `/var/lib/dso/...`) |
  | `DSO_FAULT_PLUGIN_DIR` | per-test provider-plugin directory (production allows only three system directories) |

  `Hit` fires only on **exact** equality (`"rolling:after_intent_swap"` does not match
  `"after_intent_swap"`, a prefix, another case or a trailing space) and dies by
  `SIGKILL` of **itself** (`syscall.Kill(os.Getpid(), SIGKILL)`): no deferred cleanup,
  no graceful shutdown. Unit tests (`faultinject_test.go`, `faultinject_dsofault_test.go`)
  prove the stub never fires even with every variable set, and that the tagged hook
  fires only on the configured point/hit.
* `dsofault` is Linux/Unix only (`syscall.Kill`); a Windows `-tags dsofault` build is
  not supported.

### Production files touched

| file | change |
|---|---|
| `internal/faultinject/faultinject.go` (new) | no-op stub |
| `internal/faultinject/faultinject_dsofault.go` (new, tagged) | the hook |
| `internal/rotation/rolling_strategy.go` | `fault()` also calls `faultinject.Hit("rolling:"+point)`; one new point `after_intent_rollback` in `discardShadow`; import |
| `internal/watcher/controller.go` | `ReloaderController.fault()` also calls `faultinject.Hit(point)`; import |
| `internal/agent/state_tracker.go` | `state:before_persist_rename`, `state:after_persist_rename` in `persistState`; import |
| `internal/agent/recovery.go` | `recovery:start`, `recovery:before_retire_artifact`, `recovery:after_retire_artifact`; import |
| `internal/agent/trigger.go` | `NewTriggerEngine` reads `Param("state_dir"/"lock_dir")` (stub: unchanged defaults; the pinned constants are untouched) |
| `pkg/provider/load.go` | `validatePluginPath` adds `Param("plugin_dir")` to the allowed plugin directories (stub: unchanged) |
| `internal/agent/umask_linux.go` | **bug fix, not hook**: see section 9 |

The pre-existing `FaultHook func(point string)` fields (used by the in-process tests)
are untouched and still work.

## 4. Harness design

For each case, in a unique temp directory (`harness.Root`; short, because unix socket
paths are limited to ~104 bytes) the harness:

1. builds `dso` with `-tags dsofault` and the real `cmd/plugins/dso-provider-vault`,
   writes a plugin hash manifest (`DSO_PLUGIN_HASH_MANIFEST`), all into a test temp dir;
2. runs a loopback **Vault KV v2 stub** in the test process. The real vault plugin talks
   to it, so the agent uses its production provider path (plugin process, RPC, hash
   verification). The stub is also the **rotation trigger**: a secret that is absent
   answers 404 (the agent retries every second), `Set` makes the next poll see it and
   start a rotation, `Remove` disarms it so a restarted agent cannot start a new
   rotation while recovery is observed;
3. creates the workload through the guard (`dockerguard.Current`, `LabelArgs`, unique
   names, a per-case label): a busybox HTTP workload (random per-start identity so a
   response proves which instance answered) with exactly the labels production uses
   (`dso.reloader`, `dso.owner`, `dso.secrets`, `dso.update.strategy`,
   `dso.host_ports` for the proxy-fronted rolling case), plus an **unrelated** container
   and a **foreign-owned look-alike** (a different `dso.owner`, name `<svc>_dso_backup_999`);
4. starts the real `dso agent` as a child in **its own process group**, per-test state
   directory, explicit sanitised environment (`harness.SanitizedEnv`: an allow-list of
   the parent environment, `DOCKER_HOST` = the guard endpoint, **no other `DOCKER_*`**,
   no `DSO_*` of the parent, no credentials; the harness asserts exactly one `DOCKER_*`
   variable before every launch);
5. starts the independent traffic generator (same measurement as
   `traffic_rotation_test.go`, factored into `harness.Generator`; new connection per
   request, so it measures availability of new connections only). The generator, stub
   and supervisor live in the test process and **survive the agent's death**;
6. waits for `DSO Agent is now running` (printed only after startup recovery),
   starts traffic, arms the trigger;
7. waits for the agent to die at the armed point and **verifies** it: exit status is
   `killed by SIGKILL`, the marker file names the armed point, and the marker's PID
   equals the PID the harness started (`harness.VerifyFault`). Signals are sent only to
   the PID/PGID the harness started, after an identity check (`/proc/<pid>/stat` start
   time), never by process name; after the agent's death only group members observed
   alive at the instant of reaping (an orphaned provider plugin) are SIGKILLed;
8. takes a snapshot, disarms the trigger, starts a fresh agent on the **same state
   directory**, waits for recovery, lets traffic and the proxy settle, snapshots again,
   SIGTERMs that agent, starts a third and snapshots a **second recovery pass**;
9. checks the invariants (section 6), greps everything the agents wrote for the secret
   sentinel, and cleans up only what carries the case label or the case's unique name.

Select a slice with `DSO_CRASH_CASES='<regexp over case IDs>'` and write the report with
`DSO_CRASH_REPORT=<path>` (Markdown, plus JSON next to it). A point that is registered but
never reached is reported **NOT EXECUTED** and fails the run: an unreached point is not a
pass.

```
DSO_INTEGRATION_TESTS=1 DSO_TEST_DOCKER_HOST=... DSO_TEST_DAEMON_ID=... DSO_TEST_ENVIRONMENT_ID=... \
  go test -tags dockerint -run 'TestCrash' -timeout 3h ./test/docker/   # not yet executed
```

## 5. The matrix (strategy x point)

41 cases, from `harness.Matrix()` (`Points` is the registry; `TestRegistryMatchesSource`
fails if a point exists in production code but not in the registry, or the reverse).

**rolling** (proxy-fronted; names are `rolling:<point>`): `after_intent_create_candidate`,
`after_candidate_create_before_ack` (candidate creation); `after_secret_delivery`;
`after_start_before_verify`, `after_candidate_verified` (readiness);
`after_intent_handoff`, `after_handoff_before_ack` (proxy handoff, before/after ack);
`after_intent_swap`, `between_renames`, `after_swap_before_commit_ack`,
`after_swap_committed` (commit); `after_intent_cleanup`, `after_cleanup_before_ack`
(original retirement); `after_intent_rollback` (rollback; scenario: the candidate never
becomes healthy).

**signal** (file mode, **two** secrets so one publication can be genuinely partial;
names `signal:<point>`): `after_intent_publish`, `after_spec_0_published` (**partial
publication**: first file new, second still old), `after_spec_1_published`,
`after_publish_before_ack`, `after_intent_reload`, `after_signal_before_ack` (after
signal, before ack), `during_readiness`, `before_final_ack`.

**restart** (env mode, full recreation; names `restart:<point>`): `after_intent_rename`,
`after_rename`, `after_intent_create`, `after_create_before_ack`, `after_intent_stop`,
`after_stop_before_ack`, `after_intent_start`, `after_start_before_ack`,
`after_intent_cleanup`, `after_cleanup_before_ack`.

**all strategies:** `state:before_persist_rename`, `state:after_persist_rename` (the Nth
persistence of the journal, N=4: before the temp file replaces the journal / right after a
record became durable).

**rolling, crash during recovery:** `recovery:start`, `recovery:before_retire_artifact`,
`recovery:after_retire_artifact`. Stage 1 dies at `rolling:after_candidate_verified`
(leaving a retirable candidate); stage 2 (the restarted agent) dies **during recovery**;
stage 3 must still converge, and a fourth pass must change nothing.

Additional non-matrix cases: `Control_OrdinaryRotation`, `Topology_AgentDeathKillsProxyListener`,
`CorruptJournal_Conservative` (journal truncated after a crash at
`rolling:after_candidate_verified`: both containers preserved, the unreadable file kept as
`rotations.json.corrupt-*`, recovery reported as disabled).

### Points that could not be instrumented

* **Partial publication inside one multi-file secret.** The atomic exec-based injector
  writes the files of one spec in a loop inside `internal/injector`; the signal strategy's
  only hook between specs is `after_spec_N_published`. Partiality is reached with two
  secrets (two specs). A crash between two files of one spec would need a hook inside the
  injector.
* **Restart-strategy rollback** (renaming the original back after a failed replacement):
  the restart strategy has no journal transitions and no fault points for this path
  (`TrIntentRollback`/`TrRolledBack` exist only for the rolling candidate). Not
  instrumented.
* **Between Docker's own actions** (inside `ContainerStop`, `ContainerRename`): not
  instrumentable from DSO; the points sit between DSO's calls.
* **Proxy swap internals** (`proxy.Manager.SwapBackend`): the handoff is covered by
  `after_intent_handoff` / `after_handoff_before_ack` around the `OnReady` callback; there
  is no finer production seam.
* `state:*` use the Nth hit. The exact journal transition at hit 4 depends on how many
  persists the agent performed first (startup cleanup and, in signal mode, the first
  secret's failed attempt shift it). The report records the journal transition that was
  actually durable at the crash ("journal at crash"); if the crash lands before the
  rotation's intent became durable there is nothing to recover and the signal-uncertainty
  check is skipped (and that is stated by the rule, not hidden).

## 6. What is asserted

Pure functions in `test/harness/invariants.go`, each unit-tested with synthetic snapshots
for the passing case and for every violation.

* **workload-survives**: at least one owned container of the service remains; if none is
  running the journal must flag `critical_error` (a stopped/partial workload is reported,
  never silent); recovery must not turn a running service into a stopped one.
* **bystander-untouched**: the unrelated and the foreign-owned container keep their
  name and running state.
* **original-not-retired**: while the journal does not show the swap committed (rolling:
  `committed`/`swap_committed`/`intent_cleanup`/`completed`; restart: `intent_cleanup`/
  `completed`; signal: never) the original must still exist.
* **only-artifacts-removed / no-removal-after-handoff**: recovery may remove only
  rotation artifacts (`_dso_backup_`, `_dso_new_`, `_old_<ts>`), never a name holder, and
  after the proxy handoff intent it may remove nothing (see finding 1).
* **interrupted-not-successful / no-adoption-claim / recovered-needs-holder /
  no-holder-must-be-manual**: an interrupted operation is never `completed`; `recovered`
  requires a running name holder; where nothing holds the real name (`between_renames`,
  restart rename/create/stop/start) it must be `critical_error`; a signal rotation never
  claims a finished reload.
* **routing-consistent / service-reachable** (rolling): every backend identity that
  answered after recovery maps to a running owned container the durable state authorises
  (original before commit; candidate only once the handoff started or the swap committed),
  and the service answers.
* **second-pass-idempotent**: a further recovery pass changes neither the container set
  nor the journal (time-independent digest).
* **corrupt journal**: nothing removed/started/stopped, the unreadable file preserved.
* **signal**: the container is not recreated; the application reload counter does not
  increase across recovery (**reload not blindly repeated**); the secret files are
  identical before and after recovery (**partial delivery stays partial and uncertain**,
  no silent completion or revert); the record stays `critical_error`.
* **restart**: the interrupted rotation never deletes the only recoverable workload, a
  stopped/partial workload is reported accurately, and nothing is marked completed (no
  claim of credential adoption without evidence).
* **no secret in logs or persisted state**: every file under the case directory (agent
  logs, journal, locks, config) is scanned for the synthetic sentinel in plain, hex,
  URL and base64 forms. Findings never echo the value. Env-mode secrets are, by design,
  in the container's environment; Docker inspect output is not scanned here (it is by
  `traffic`/`scenarios` tests).

## 7. Findings from the investigation (for the maintainers; not changed here)

1. **The production agent wires no `RoutingVerifier`.** `agent.AutomaticRecovery.Routing`
   is read by `liveRouting`, but `TriggerEngine.performAutomaticRecovery` never sets it.
   After a restart, any proxied rotation whose journal shows the handoff intent therefore
   preserves everything and is flagged for manual recovery (`routing cannot be
   established`). That is conservative and safe, but it means automatic recovery of a
   proxied rotation after the handoff began is **not possible in production today**. The
   harness encodes this as the expectation (`no-removal-after-handoff`); if a verifier is
   wired later the expectation must be revisited.
2. **`ScanAndRegister` runs before recovery and registers every running owned container
   that carries `dso.host_ports`**, including an unready rotation candidate (cloned
   labels). Between the scan and the retirement of that candidate, new connections may be
   routed to a candidate that was never verified. The `routing-consistent` rule measures
   exactly this after recovery has settled; the death/recovery window in the report shows
   what clients saw meanwhile.
3. **The first poll of a freshly started agent dispatches a rotation** (no baseline hash),
   so a restarted agent with the new secret already visible immediately starts a *new*
   rotation after recovery. The harness disarms the trigger before the restart so recovery
   can be observed in isolation; the convergence behaviour after re-arming is **not**
   covered here.
4. A rotation attempt that fails before reaching a strategy (for example the signal
   strategy before the second secret is cached) leaves a state record with no
   `service_id`; the harness therefore selects the interrupted record by `service_id` and
   falls back to anonymous records only when none names the service.

## 8. What the harness itself needs from the environment

* The disposable VM described in `DISPOSABLE-VM-EXECUTION-PLAN.md` (guard variables set,
  dedicated daemon). The harness binds loopback ports (proxy host port, REST, metrics) and
  creates directories under `os.TempDir()` (set `TMPDIR` to something short; the layout
  fails fast if a socket path would exceed the limit).
* `go` on `PATH` (the harness builds the tagged binary and the plugin), network access
  only to the loopback stub; the busybox image the other integration tests already use.
* No `/var/lib/dso`, `/run/dso`, `/usr/local/lib/dso/plugins` or any other system path is
  touched: the state/lock/plugin directories are redirected by the tagged binary.

## 9. A real defect found while retiring the skipped test

`TestRace_SocketPermission_Concurrent` was a permanent `t.Skip` placeholder. The contract
it pointed at is real: agent sockets are created under a process-global `umask(077)` and
then `chmod`'d. `tightenUmask`/`restoreUmask` were an unsynchronised read-modify-write, so
concurrent socket creators could restore a *wider* umask under another creator's
`net.Listen` (the socket exists 0755 until its `chmod`). A lock held across the
tighten/restore pair (`internal/agent/umask_linux.go`) fixes it;
`internal/agent/socket_permission_race_test.go` (Docker-free, `-race`) fails against the
old code and passes now. See `LEGACY-DOCKER-TESTS.md`.

## 10. Verification status

| item | status |
|---|---|
| `go build ./... && go vet ./... && go vet -tags dockerint ./... && go vet -tags dsofault ./...` | pass |
| `go test -short ./...` (includes `test/harness`, `internal/faultinject`, the socket-permission tests) | pass |
| `go test -tags dsofault` for `internal/faultinject`, `internal/agent`, `internal/rotation`, `internal/watcher`, `pkg/provider`, `test/harness` | pass |
| `go test -race` for `internal/agent`, `test/harness` | pass |
| untagged binary has no hook / tagged has it (builds both) | pass (not `-short`) |
| real tagged agent + real vault plugin, Docker unreachable: boot, plugin load, readiness marker, per-test state dir, graceful SIGTERM, armed point -> SIGKILL -> marker -> `VerifyFault` | pass (not `-short`) |
| the matrix, `Control_OrdinaryRotation`, `Topology_AgentDeathKillsProxyListener`, `CorruptJournal_Conservative` against a real daemon | **NOT EXECUTED** |
| availability numbers (a), (b), (c) | **NOT MEASURED** |
| expectations in section 6 against real recovery behaviour | **unverified**: they come from reading the code and the in-process crash tests; the first real run may legitimately fail on any of them and that is the point of the run |
