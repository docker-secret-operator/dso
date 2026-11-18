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
| `test/docker/startup_recovery_test.go` | `dockerint` scenarios for startup traffic eligibility and competing-rotation dispatch (section 11) |

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
* The proxy's routing table is rebuilt at agent start from running containers labelled
  `dso.host_ports` (`TriggerEngine.AttachProxy` -> `Manager.ScanAndRegisterGated`): the
  routing table is in-memory state of the agent and is lost when it dies. Candidates of an
  unsettled rotation are HELD out of that scan and admitted after recovery (section 7).

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
  | `DSO_FAULT_LEDGER_DIR` | per-test applied-version-ledger directory (production uses `/var/lib/dso/ledger`; without it an unprivileged agent has an unusable ledger and adopts a first observation without rotating, so no journal record is written) |
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
  after the proxy handoff intent it may remove one ONLY if the measured routing after
  recovery shows the surviving real-name holder receiving the traffic (the agent now wires
  a `RoutingVerifier`, finding 1; a persisted handoff ack alone is never evidence).
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

## 7. Findings from the investigation and their resolution (M0-04 release blocker)

The first three were reported by this harness work as production findings. They are now
fixed or explicitly documented. **None of the fixes has been run against a real daemon**;
the Docker-free regression tests are listed per finding and the real-Docker scenarios are
prepared in section 11 (compiled, not executed).

### 1. No `RoutingVerifier` wired in production (3.1)

*Trace.* `proxy.NewManager` is created once, in `dso agent`. Its backend table (`Registry`)
is in memory and dies with the agent, so previous routing never survives. The only routing
evidence a freshly started agent can have is the table it rebuilds itself (and any swap it
performs afterwards); the journal records only that a dead process once switched routing.
`TriggerEngine.performAutomaticRecovery` never set `AutomaticRecovery.Routing`, so every
proxied rotation past the handoff intent ended in preserve + manual recovery.

*Fix.* `agent.ProxyRoutingVerifier` (`internal/agent/proxy_reconstruction.go`) answers from
`proxy.Manager.RoutingSnapshot()` (`internal/proxy/routing.go`): the non-draining backends of
THIS process's table, `known` only when a startup scan listed Docker successfully. It is wired
by `TriggerEngine.AttachProxy`. Recovery's existing rule is unchanged and still decides: an
artifact is retired only when the journal authorises it AND the snapshot positively shows
the healthy real-name holder routed and the artifact not. Unknown routing, a failed Docker
listing, a holder that is not routed, or an artifact that still is, all preserve both
workloads and flag manual recovery.

*Consistency required before any retirement* (all already enforced by `journalPermits` /
`holderState`, now fed real routing): the journal names the artifact and holder (identity),
the swap/handoff evidence matches the artifact's role (committed state), the holder is
owned by this agent (ownership), the holder is verified healthy (readiness), and the
reconstructed routing agrees.

*Tests.* `internal/agent/startup_reconstruction_test.go`:
`TestStartup_HandoffThenCrash_...` (candidate never routed, retired, rotation recovered),
`TestStartup_SwapCommittedThenCrash_...`, `TestStartup_SwapIntentOnlyThenCrash_Preserves...`
(preserve + manual, nothing routed, held stay held), `TestRecovery_HandoffAckAloneIsNeverEnough...`,
`TestProxyRoutingVerifier_UnknownUnlessPositivelyEstablished`, `TestStartup_ScanListFailure_...`;
`internal/proxy/eligibility_test.go` (draining excluded).

*Limitations (documented, not hidden).* (a) Automatic recovery of a proxied rotation works
for the cases where Docker + journal identify an unambiguous holder (crash before the swap:
original keeps serving; crash after `swap_committed`: promoted candidate serves). A crash
INSIDE the swap (`intent_swap` without commit; `between_renames`) is still manual: the
holder is ambiguous and nothing is routed until an operator acts. (b) If recovery itself
starts a stopped original (rollback), that container is not in the scan-time table, so the
rotation is conservatively left manual until the next restart. (c) Traffic through the
embedded proxy is still interrupted for the whole death/restart window (topology, section 1).
(d) Unproxied services have no routing signal at all (design doc 3a).

### 2. `ScanAndRegister` ran before recovery and routed unready candidates (3.2)

*Trace.* `Manager.ScanAndRegister` bound the host port and registered EVERY running owned
container with `dso.host_ports` immediately: registration makes a container eligible for the
very next accepted connection. Rotation candidates clone the original's labels, so a
candidate that was newly created, partially injected, not health-verified, awaiting handoff,
left by an interrupted restart, or tied to an unresolved/corrupt journal was routed to
before recovery (which runs later in `StartAll`) had decided anything. Two other paths could
also register such a container behind a gate: the watcher's `start` event handler and its
runtime reconciliation.

*Fix.* Discovery is separated from traffic eligibility. `Manager.ScanAndRegisterGated` takes an
`Eligibility` gate; a container the gate refuses is HELD (recorded, not bound, not registered,
logged). The agent's gate (`StateTracker.ProxyEligibility`) holds: rotation artifacts by name
(`_dso_new_`, `_dso_backup_`, `_old_<ts>`); candidates named by an unsettled journal record
(by ID or by `dso.rotation.id` label, which exists before the ID is journalled) unless the
journal shows the swap committed, the container holds the real name and Docker does not
report it unhealthy/starting; and rotation-labelled containers when the journal is
unreadable or unavailable. After recovery `settleProxy` calls `RescanHeld`, admitting held
containers whose operation recovery settled. The watcher's start-event and reconciliation
paths skip held containers (`Manager.IsHeld`). Foreign-owned containers are still refused by
the existing ownership gate (neither routed nor held). There is no deadlock: the gate never
waits for recovery, recovery never waits for the proxy, and the sequence is
`AttachProxy` -> `StartAll` (recovery -> `settleProxy`).

Containers with no `dso.owner` label stay routable (unchanged backward compatibility).

*Tests.* `TestProxyEligibility_CandidateKinds` (18 candidate kinds: plain, newly created,
partially injected, not ready, awaiting handoff, failed leftover, leftover old/backup,
promoted healthy / no healthcheck / unhealthy / starting / wrong name, completed, aged out,
corrupt-journal variants, no journal), the five `TestStartup_*` sequence tests,
`TestStartup_ForeignOwnedContainerNeverRegisteredOrHeld`,
`TestStartup_CorruptJournal_NoCandidateRoutedNothingTouched...`, and
`internal/proxy/eligibility_test.go`.

*Limitation.* With an unreadable/unavailable journal a rotation-labelled container (including
a legitimately promoted one, which keeps its label) is held until the operator verifies the
service and restarts; it is not routed meanwhile. That is the conservative choice and costs
availability of that service in that rare state.

### 3. First poll of a fresh agent dispatched a rotation (3.3)

*Trace.* `StartPolling` fires a timer(0) poll; the engine keeps no persisted baseline hash
(the hash is in memory, and no secret-derived value is persisted), so the first observation
always dispatches. This is **intentional** (it also catches rotations that happened while the
agent was down) and is **kept**: a clean start and a restart after a successful rotation still
perform their initialisation rotation. What was missing: nothing consulted the journal or
service locks (`rotationLocks` are in memory, empty after a restart) before dispatching.
`StartAll` already ran recovery synchronously BEFORE the pollers, but the IPC socket, the REST
API and the watcher were live earlier, and a service recovery left `critical_error` (for
example a partially published signal rotation) was rotated again by the first poll.

*Fix.* Every dispatch path (first/scheduled poll, webhook, IPC `docker dso sync`/`apply`, REST manual
rotate, watcher) reaches `ReloaderController.TriggerReload`. A durable `OperationGuard`
(`internal/watcher/operation_guard.go`, implemented by `internal/agent/operation_guard.go`) is
consulted per target AFTER its service lock is taken (so check-then-start cannot interleave
with another dispatcher journalling an unresolved state) and refuses the target. It blocks when
(1) startup gating is armed (`TriggerEngine.BeginStartup`, called by `dso agent` right after
constructing the engine) and startup recovery has not settled; (2) the journal was unreadable;
(3) the journal holds a `critical_error` record for the service, or an `in_progress` record
inherited from a previous process, matched by service name, compose service name or
original/candidate container ID. An ordinary failed attempt of the running process
(`rollback_required`) does not block the retry. A refused target yields
`ErrRotationBlocked` wrapped with `ErrRotationDeferred`: nothing was applied and the existing
retry-on-next-poll behaviour applies.

*Releasing a block.* Automatic recovery re-evaluates every unresolved record at each start.
Otherwise the operator repairs the service and marks the record `recovered` (or removes it)
in `rotations.json` while the agent is stopped, then restarts. For an unreadable journal the
file is already preserved aside, so a restart after verifying the workloads starts clean.
There is no in-agent "force" path and no CLI to acknowledge a record yet: **that is a known
operational gap** (a `dso` subcommand for it is a follow-up).

*Tests.* `internal/agent/operation_guard_test.go`: clean startup, restart after a successful
rotation (initialisation rotation allowed), unresolved rolling operation (resolved and
unresolvable), partially published signal operation (also across restarts), interrupted
restart operation, corrupted journal (and release after the operator's restart), ordinary
failed attempt does not block, engine wires the guard into the reloader, concurrent readers
during recovery and journal writes (`-race`). `internal/watcher/operation_guard_test.go`:
`TriggerReload` refuses a blocked service but rotates an unrelated one, matches by container ID,
leaks no lock, unchanged without a guard, and 24 concurrent dispatchers (watcher + scheduled +
API) start exactly one rotation over a service whose first rotation journals an unresolved
state (`-race`).

### 4. Unfinished state record without a `service_id`

A rotation attempt that fails before reaching a strategy (for example the signal strategy
before the second secret is cached) leaves a state record with no `service_id`; the harness
therefore selects the interrupted record by `service_id` and falls back to anonymous records
only when none names the service. (Unchanged. The guard also matches such a record when it
names the container ID.)

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
tighten/restore pair (`internal/agent/umask_linux.go`) fixes it. The temporary mask is also
**monotonic** (`previous | 0o077`, so a stricter previous mask such as `0o277` keeps its bits;
the probe installs `0o777` first so no instant is less restrictive than the previous mask) and
the previous mask is restored on every exit path. The remaining process-wide limitation (an
unrelated goroutine creating a file during the microseconds of `net.Listen` gets the same,
only more restrictive, mask; code outside this package calling `Umask` is not serialised) is
documented in the function comment;
`internal/agent/socket_permission_race_test.go` (Docker-free, `-race`) fails against the
old code and passes now. See `LEGACY-DOCKER-TESTS.md`.

## 10. Verification status

(Updated for the M0-04 release-blocker fixes: Docker-free only. No Docker daemon, container,
`dockerint` test or `dsofault` real-process run was executed for them.)

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
| M0-04 fixes (3.1 routing verifier, 3.2 traffic eligibility, 3.3 competing-dispatch guard, umask monotonic): `go build`, `go vet` (plain, `dockerint`, `dsofault`), `go test -short ./...`, `-race` on `internal/agent`, `internal/watcher`, `internal/proxy`, `internal/rotation`, `test/harness` | pass |
| `test/docker/startup_recovery_test.go` scenarios | **compiled only (`go vet -tags dockerint`); NOT EXECUTED** |
| the same fixes against a real daemon, real events and the real embedded proxy | **UNVERIFIED** |

## 11. Startup-recovery scenarios (real Docker, prepared, NOT executed)

`test/docker/startup_recovery_test.go`, `TestStartupRecovery` (build tag `dockerint`; needs
the disposable VM; reuses `crashCase`, the fault points and the Vault stub above):

| scenario | finding | what is asserted |
|---|---|---|
| `CandidateNeverRoutedBeforeRecovery/after_start_before_verify` and `/after_candidate_verified` | 3.2 | agent SIGKILLed with a running candidate (cloned `dso.host_ports`); traffic starts BEFORE the new agent; across restart + recovery no client connection is answered by the candidate's identity, the original serves, and the agent logs that it held the candidate |
| `SignalPartialPublication_NoCompetingRotationAfterRestart` | 3.3 | killed after the first of two secret files was replaced; the new secrets stay visible so the first poll would dispatch; it is refused (`Rotation refused: ...` in the log), files unchanged, reload counter unchanged, journal stays `critical_error` |
| `CorruptJournal_BlocksRotationAfterRestart` | 3.3 + 3.2 | unreadable journal, secret visible: no container created/removed/stopped (`CheckCorruptJournal`), dispatch refused |
| `CleanRestart_InitialisationRotationStillRuns` | 3.3 control | after a completed rotation and a graceful restart the first poll still rotates (second `completed` record) and is never refused |

The existing matrix's `no-removal-after-handoff` expectation was relaxed accordingly (section
6): a removal after the handoff is now accepted only with observed routing to the surviving
holder.
