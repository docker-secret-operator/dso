# N19: managed file delivery through a startup gate (design and implementation note)

Status: implemented on `feature/m2-development`, uncommitted at the time of writing. **N19 is NOT closed.**
The premise (Docker's archive endpoint may write into a created container's persistent writable layer instead of
the tmpfs) is **RUNTIME-UNVERIFIED**; this change removes the archive endpoint from the managed path and makes the
replacement fail closed. Real Docker qualification is deferred (no approved isolated Docker environment).
Labels: **[S]** read in source, **[T]** covered by a Docker-free test, **[U]** runtime-unverified.

Builds on `docs/audit/45-N19-STARTGATE-REUSE-REVIEW.md` (section 12 design F1+F2) and `docs/design/m0-04-recovery-design.md`
(section 6, N21). Owner authorisation (N19 + N22) covers exactly this: reuse the local-mode `dsofile://` gate idea and
the hardened exec injector inside the existing managed lifecycle. No sidecar, daemon, runtime, second journal or external
orchestration was added.

## 1. What changed in one paragraph

For config-driven `inject.type: file` secrets, the three managed paths (rolling `RollingStrategy.Execute`, restart
branch of `TriggerReload`, rolling fallback `executeSimpleRestart`) no longer call `StreamSecretToContainer`
(archive PUT). The candidate is **created with its ENTRYPOINT wrapped by a fixed shell gate**, started, **verified**
(identity, gate process, tmpfs at every destination), **delivered through the injector** (`injectOneFileOpts`: stdin,
atomic rename, `RequireTmpfs`), **probed**, its delivery evidence (keyed versions) is made **durable in the rotation-ID
journal**, and only then a release flag is written so the gate `exec`s the original process. Readiness, routing handoff,
retirement and ledger reconciliation are unchanged and still follow the existing managed-operation contract.
`StreamSecretToContainer` remains in the tree (used by the N19 diagnostic scenarios G-I and tests) but no managed path calls it.

## 2. Lifecycle phases and journal transitions

Phases are not interchangeable. Reused transitions are the existing `Tr*`; six are additive (no existing name or meaning
changed). Every row is journalled in the same rotation-ID record (`StateTracker.Record`); no second journal exists.

| # | Phase | Transition (write style) | Meaning, written when | Crash here: recovery treatment |
|---|---|---|---|---|
| 1 | container created | `candidate_created` + `Gated=true` (ack) | candidate exists, gate armed, **not started** | unverified candidate; created-not-started |
| 2 | gate started | `gate_started` **new** (ack) | started AND verified by inspect: ID, `dso.rotation.id`, `dso.gate`, `Path==/bin/sh`, wrapped ENTRYPOINT, running, tmpfs/mount per destination | unverified; application never ran |
| 3 | delivery requested | `intent_deliver_secrets` **new** (intent: write failure aborts) | before the first byte is delivered | unverified; "delivery may or may not have happened" |
| 4 | partial delivery | `delivery_partial` **new** (ack, best effort) | a later spec failed after an earlier one was written | unverified; never promoted |
| 5 | delivery verified | `secrets_delivered` **new** (intent style: write failure aborts, gate stays closed) + `DeliveredVersions` | every file delivered AND probed; keyed versions recorded | still unverified until candidate_ready; sets `DeliveryVerified` |
| 6 | gate released | `intent_release_gate` **new** (intent) then `gate_released` **new** (ack) | flag written; the application may now start | unverified: released but readiness not acknowledged |
| 7 | application started | not separately journalled | not distinguishable from "released" without an application signal | folded into 6 (documented, honest) |
| 8 | application ready | `candidate_ready` (existing) | WaitReady/WaitHealthy + post-readiness probe passed | verified **only if** `DeliveryVerified` (rule below) |
| 9 | traffic handoff verified | `intent_handoff`/`handoff_done` (existing, rolling; restart proxy swap as before) | `RoutingVerifier`/proxy handoff, unchanged | existing rules |
| 10 | operation committed | `swap_committed`/`intent_cleanup`/`completed` (existing) | unchanged | existing rules |
| 11 | ledger reconciled | ledger bridge (existing; outside the journal) | `RecordRotationOutcome` / startup reconcile | see section 8 |

**Verification rule (the one place evidence is decided).** `candidateVerified(transition)` is unchanged (transition-based).
`recordCandidateVerified(rec)` (`internal/agent/recovery.go`) = `candidateVerified(rec.Transition)` AND (`!rec.GatedDelivery`
OR `rec.DeliveryVerified`). It is the single predicate used by recovery (`unverifiedCandidate`, `journalPermits`), the N21
rollback (`planRollback`) and the ledger bridge (`journalEvidenceKind`, `JournalRecordFor`). `GatedDelivery` is set by the
`Gated` flag on `candidate_created`; `DeliveryVerified` and `DeliveredVersions` only by `secrets_delivered`;
`intent_create_candidate` resets all three (a fallback candidate never inherits another candidate's evidence). Records written
before this change have none of the fields (omitempty) and keep the plain rule. `Record` refuses (before merging anything)
a `DeliveredVersions` entry that is not `hmac:<key id>:<mac>` (never plaintext, never an unkeyed hash).

`rollbackTransition` (N21) lists the six new transitions as pre-ready, so the controlled rollback applies to them exactly as
to `replacement_started`. The table-driven ledger test (`lbTransitions`) gained six rows, all `false`.

## 3. Arming the gate for a candidate created by rotation

`rotation.PrepareGatedCandidate(cfg, hostConfig, specs, timeout)` runs before any Docker call or journal write (a refusal costs
nothing): `ValidateFileSecretSpecs` -> `VerifyDeclaredTmpfs` -> `VerifyUserOwnership` -> `ArmGate`. It operates on the clone that
`PrepareShadowConfig`/`cloneContainerConfig` produced (new slices assigned, inputs never mutated).

**Contract preservation.** Docker's inspect reports the *effective* ENTRYPOINT and CMD (image defaults merged with overrides), so
no image inspect is needed. The wrapper is `Entrypoint = [/bin/sh -c GATE dso-gate <timeout> <flag> <N> <file1..N> <orig ENTRYPOINT...>]`
and `Cmd` is left exactly as it was. At runtime argv is `Entrypoint ++ Cmd`, so `exec "$@"` re-creates exactly
`origENTRYPOINT ++ CMD` with every argument boundary intact (no re-quoting, no interpolation). `User`, `WorkingDir`, `Env`,
`HostConfig` (including `ReadonlyRootfs`, restart policy, caps) are not touched. Cases [T]: CMD only, ENTRYPOINT only, both, compose
overrides (they are already reflected in inspect), arguments containing spaces/quotes/newlines/`$`/`;`/`--`. Both empty is **refused**
(the original process cannot be preserved). An ENTRYPOINT that is already a gate (clone of a promoted container) is unwrapped and
re-wrapped for the current file set, so rotations never nest.

**Gate script** (`rotation.GateScript()`, a fixed constant; no secret and no user value is ever in its text [T]): traps TERM
(exit 143) and INT (130); waits until the release flag **and** every expected file exist; bounded by the timeout (default 120 s,
exit 75 with a message); then `shift N; exec "$@"`. **PID 1 and signals:** `exec` replaces the shell, the application is PID 1 (or
`tini`'s child if `HostConfig.Init`), no fork; before release PID 1 is `sh` and `docker stop` ends it immediately through the trap.
The wait polls `sleep 0.1`, falling back to `sleep 1` ticks if fractional sleep is unsupported.

**What the agent writes:** secret files first (through the injector), the flag last (also through the injector, name
`.dso-gate-release`, content `released`, no secret), into the first sorted destination directory. The gate does not trust the flag alone.

**Health checks and restart policies.** Docker's first HEALTHCHECK probe runs one `interval` after start; delivery normally finishes
in well under a second, so no probe observes the gate. If delivery hangs, the container may report unhealthy and `WaitReady` rejects
it; the candidate is discarded. A candidate cloned with `always`/`unless-stopped`/`on-failure` that exits at the gate timeout is
restarted by the daemon and the gate re-arms on an empty tmpfs: the Docker `start` event handler (`redeliverGated`, section 10) re-delivers from the cache. [U]

## 4. Verifying the running container and the mount before delivery

Before the candidate exists (`VerifyDeclaredTmpfs`, from the *declared* config): every destination must be covered by a declared tmpfs
(`HostConfig.Tmpfs` or a `Mounts` entry of type tmpfs), no more specific bind mount, volume or `Config.Volumes` entry may shadow it,
and a read-only tmpfs is refused. DSO does **not** add a mount or change runtime settings on the operator's behalf: a destination
without a tmpfs makes the rotation fail closed with a message naming the fix (`tmpfs: [<path>]`). Cloud-mode compose generation already
declares `/run/secrets/dso`.

After start (`VerifyRunningGate`, from `ContainerInspect`): container ID equals the created ID; label `dso.rotation.id` equals this
operation; `dso.gate` present; ENTRYPOINT is the gate; `inspect.Path == /bin/sh` (the process Docker started is the gate shell, not the
application); running and not restarting; the gate's flag path equals the one that will be written; for every destination the
longest-prefix entry of `inspect.Mounts` has `Type == tmpfs` and `RW`. A destination covered by a bind/volume/none is refused (no
writable-layer path exists). The in-container injector still re-checks `/proc/mounts` (`RequireTmpfs`, exit 66) as the authoritative
runtime check. How Docker reports tmpfs in `Mounts` for every version is **[U]**; when `Mounts` lacks an entry the declared config is
re-checked instead.

## 5. Delivery: the hardened injector, ownership, users, images

Reused unchanged: `injector.InjectFilesWithOptions(Dir: spec.Path, RequireTmpfs: true)`: per file one exec as the container's
configured user, secret on stdin (base64 + sha256 line), `mktemp` O_EXCL temp, sha256 verified before publish, `chown` then `chmod
0400`, atomic `mv`, symlink/dir destinations refused. Names are validated twice and **a name that is not already a plain base name
is refused** (the injector would normalise it while the gate waits for the original). Reserved name `.dso-gate-release`.
Only exported helpers `injector.CheckFileName`/`CheckDir` were added. Mode is 0400 (the injector does not support 0440).

- **Ownership.** The exec runs as the container user and cannot chown to another uid. Root (or unset) user: chown to `inject.uid:gid`
  works with `CAP_CHOWN`; a numeric non-root user must equal `inject.uid` (checked **before** any change, `VerifyUserOwnership`);
  a named non-root user is left to the injector, which fails closed on a chown it cannot do. This is a behaviour change from the
  archive path where the daemon applied the header ids [S].
- **Root entrypoint that drops privileges:** supported (exec as root writes, app uid reads).
- **Read-only root filesystem:** supported; only the tmpfs is written.
- **Supported images:** `/bin/sh`, and for the injector `awk mktemp base64 sha256sum cut chmod mv rm` (+`chown` for non-root ids), and for the probe `/bin/sh`.
  busybox/alpine/debian/ubuntu families satisfy this [S][U].
- **UNSUPPORTED, fail closed, no plaintext fallback ever:** images without `/bin/sh` (distroless, scratch): `ContainerStart` of the gate fails
  (or the gate cannot run), the candidate is discarded, nothing is delivered; images lacking the injector tools: the gate stays closed, delivery
  fails (exit 127), the candidate is discarded; containers with neither ENTRYPOINT nor CMD; containers with no declared tmpfs at the destination;
  destinations on a bind mount/volume/`/proc`/`/sys`/`/dev`; a numeric non-root user that differs from `inject.uid`. Supporting shell-less images
  needs a static helper binary (design alternative F4) and is **not authorised** here.

## 6. Where plaintext never goes

Persistent container layers (no archive upload; destination must be a tmpfs, verified twice); host temp files (none: stdin stream only);
journal and ledger (only keyed `hmac:` identities and counts; file names and values are not journalled); environment variables
introduced by delivery (none; file secrets are excluded from `Config.Env` as before); command-line arguments (gate args hold only paths,
counts and the timeout; the injector passes the secret on stdin); generated startup scripts (the gate script is a constant); logs and
metrics (error values carry exit reasons, never content). The candidate's `docker inspect` shows the wrapped ENTRYPOINT (paths only).
Residual: `docker inspect` of a promoted container keeps the wrapper; the probe checks existence/non-empty, not readability by the app uid. [T for the constant script, create body, journal; U for runtime]

## 7. Strategy differences and honest downtime

- **Rolling** (`RollingStrategy.Execute`): the original keeps serving untouched until the swap; delivery, probe, gate release and readiness all happen
  on the candidate beside it; a failure discards the candidate. Zero-downtime only when the DSO proxy owns the ports (existing rule); file delivery adds
  no downtime. Without a HEALTHCHECK readiness is "process stable" only (existing honesty).
- **Restart** (primary branch) and **fallback** (`executeSimpleRestart`): NOT zero downtime unless the DSO proxy owns the ports. Without the proxy the
  original is stopped before the candidate starts, so downtime now **includes** gate start, verification, delivery, probe and readiness (typically seconds,
  bounded by the health timeout). A failure after the original was stopped rolls back (rename back, restart the original) and the service is down for that time.
- **Agent crash mid-rotation is not zero-downtime**: a crash after the original was stopped leaves the service down until recovery or an operator acts. The
  gate means the application is never running without its secret in that window. N21 rollback restores the original when its safety conditions hold; otherwise
  recovery preserves everything and escalates (`critical_error`).
- Rolling failures that are deterministic refusals (`ErrDeliveryRefused`) skip the fallback restart (it would be refused identically); health-class failures
  (`ErrUnhealthy`, including a probe rejection) already skipped it.

## 8. Recovery, rollback and the ledger

Recovery treats every phase 1-7 as an **unverified candidate** (never the name holder that justifies retiring the original), through the existing machinery: `holderState`
refuses it, `journalPermits` refuses retirement of the original, `planRollback` includes the six transitions. With `AutomaticRollback` off (default) it preserves and
escalates; with it on, the N21 rollback moves the candidate aside and restores the original (nothing deleted) and records exactly what was not rolled back. A gated
record that shows `candidate_ready` without `DeliveryVerified` is never verified. Repeated recovery is idempotent; `critical_error` is never downgraded by
recovery. Rolling candidates (`_dso_new_`) are retired by the existing `retireSafeArtifacts` rules.

Ledger "delivery verified" only for a record that passes `recordCandidateVerified`, so a gated operation needs the complete chain (`secrets_delivered` before
`candidate_ready`). `RecordRotationOutcome`/startup reconcile are unchanged (`startManagedOperation`/`finishRotation`, the evidence rule is strengthened, not weakened).
`DeliveredVersions` are recorded for audit and cross-checking; the ledger still applies the dispatch's own `SecretVersion` (O-2). Interrupted reconciliation converges
(W6) from the journal exactly as before [T]. The per-spec identity is computed from the exact map handed to the injector with the ledger's key
(`ReloaderController.DeliveredVersion`, wired in `openLedgerWith`); with no ledger the field is empty and the ledger cannot apply a version (existing behaviour).

## 9. N22 (fixed in the same change)

Defect (confirmed in source): the restart branch skipped file injection silently when `Cache.Get` missed, starting a candidate without its file secret (rolling:
`if !found { continue }` had the same shape and then created/started the candidate); and the primary restart probe tested `Inject.Type == "file"`, not the
effective type, so defaulted file secrets were never probed.
Fix: `resolveFileSpecs` resolves every secret whose **effective** inject type is `file` (own type, else `Defaults.Inject`) **before any change**; a cache miss, empty
value or missing `inject.path` returns `ErrFileSecretUnavailable` (rotation rejected, no rename, no candidate, nothing started). The same specs drive delivery and the
post-readiness probe of **every** delivered file in all three paths. Regression tests `TestN22_*` fail on the old code (verified against a `git archive` of `HEAD`).

## 10. Migration, compatibility, remaining work

- Env-mode secrets, signal strategy, `dsofile://` local mode, `dso inject`, compose-project recreation: unchanged. Rotations without file secrets: no gate, no new journal phases.
- Existing configs: `inject.type` values unchanged. **Behaviour changes for file secrets:** the destination must be a declared tmpfs (previously silently accepted, and
  possibly persistent); the container needs `/bin/sh` and the injector tools; a numeric non-root user must equal `inject.uid`; both ENTRYPOINT and CMD empty is refused;
  provider file names must be plain names; downtime of the restart strategy grows by the delivery time; a missing cache value now aborts instead of starting a secretless candidate.
- Existing journals load unchanged; records without the new fields keep the legacy rule. A journal written by this version and read by an older binary would not know the
  new transitions (older recovery preserves unknown transitions: fail closed).
- **Re-delivery after a later restart of a promoted gated container** (restart policy, `docker restart`, host reboot): the gate re-arms on an empty tmpfs.
  `ReloaderController.redeliverGated` (Docker `start` event, label `dso.gate`) resolves the cached values (`resolveFileSpecs`), verifies identity/gate/tmpfs
  (`VerifyRunningGate`), skips when the release flag already exists, skips candidates a rotation owns (`gateOwned`/proxy-pending: it must never release a gate
  before that rotation's evidence is durable), delivers and releases. It is a re-delivery, not a rotation: **no journal record, no ledger claim**. Any failure
  (cache empty after an agent restart, not a registered target, delivery error) leaves the gate closed; the gate exits 75 after its bound and the restart policy
  retries. After an agent restart the cache is empty until the next provider fetch, so a container restarted in that window stays down (fail closed) until the cache is
  warm. [T for the handler decisions; U at runtime]
- **Not implemented (remaining work):** (1) real Docker qualification (every [U] fact, including the premise itself); (2) shell-less image support (static helper
  binary, design alternative F4, not authorised); (3) a per-config gate timeout (`DefaultGateTimeout` is a constant); (4) an application-level "started" signal
  (phase 7 stays folded into "released"); (5) provider fetch on re-delivery when the cache is empty.
