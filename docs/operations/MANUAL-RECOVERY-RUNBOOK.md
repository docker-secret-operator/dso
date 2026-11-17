# Manual recovery runbook: unresolved rotation operations

Audience: an operator on the host that runs `dso agent` (systemd unit `dso-agent.service`, binary `dso agent`).
Grounded in: `internal/agent/state_tracker.go`, `recovery.go`, `operation_guard.go`, `internal/rotation/journal.go`, `internal/watcher/signal_rotation.go`, `docs/design/m0-04-recovery-design.md`.
This runbook supersedes the older "State File is Corrupted" scenario in `docs/RECOVERY_PROCEDURES.md`, which tells the operator to delete the state file; do not follow that step.

## Ground rules (read first)

1. DSO deliberately has **no force or acknowledge command**. A block is released by recovery re-evaluating it on a start, or by an operator-validated edit (section 11).
2. **If you cannot establish X, preserve everything and escalate to a maintainer.** X is: which container is the original, which is the candidate, which one actually serves traffic, and whether the secret delivered to the serving container is complete and current.
3. **Never** delete `rotations.json`, delete containers named `*_dso_backup_*`, `*_dso_new_*`, `*_old_<digits>`, or set a `critical_error` record to `recovered`, until the decision table in section 9 says it is safe for that exact state.
4. Never print secret values. Do not paste unfiltered `docker inspect` output into a ticket (section 6).
5. Everything below that is marked **[UNVERIFIED]** could not be confirmed from the code; see the list at the end.

### Section 0: build caveat for this document

Two defects found in review (`docs/audit/28-STATETRACKER-IDENTITY-REVIEW.md`, F1 and F2) let the agent itself discard an unresolved record: a new dispatch of the same secret replaced a `critical_error` record at the same key, and the startup sweep marked `critical_error` / inherited `in_progress` records older than 24 hours as `recovered`. A later review (`docs/audit/34-ROLLBACK-REQUIRED-REVIEW.md`) found the same sweep also expired `rollback_required` after 24 hours without recovery examining it, and that a failed dispatch could downgrade `critical_error` to `rollback_required`; both are fixed in the working tree. The fixes are in the working tree but **uncommitted**. On a build WITHOUT those fixes, a `critical_error` record can disappear by itself, so an absent record is not proof that the operation was resolved: check workload and routing anyway. This runbook describes the fixed behaviour; where it matters it says so.

## 1. Identify an unresolved operation

What exists (verified in code):

* `dso status` (and `dso status --json`) shows **only a count**: `Rotations.Pending`, from `StateTracker.GetPendingRotations()` (records that are `critical_error`, `rollback_required`, or `in_progress` with `last_update` older than 5 minutes). It does not name the service. An `in_progress` record inherited from a previous process less than 5 minutes old is NOT counted but still blocks rotation.
* `dso doctor` runs DSO-DOCTOR-018 (pending count, same source), DSO-DOCTOR-019 (services marked degraded, with reason), DSO-DOCTOR-020 (leftover `_dso_backup_` / `_dso_new_` / `_old_<ts>` containers). The advice text of 018 says "run `dso status` for detail"; status only has the count, so use the journal and logs below.
* `docker dso system status`, `docker dso system logs [-f] [-n N] [-p err]`, `docker dso system restart` wrap `systemctl show/restart dso-agent` and `journalctl -u dso-agent`.
* There is **no** `dso` command that lists, inspects, acknowledges or clears individual rotation records (checked: `internal/cli` has setup, bootstrap, doctor, status, config, system, agent, compose, fetch, init, apply, inject, sync, migrate, notifications, up, down, watch, version, validate, export, inspect, diff, logs, secret, central...; none touches `rotations.json`). `docker dso inspect` and `docker dso logs` are unrelated to the journal [UNVERIFIED in detail: only command names and help text were read].

Journal file:

* Path: `/var/lib/dso/state/rotations.json` (constant `defaultTriggerStateDir`; no config option reaches it in a production build). Directory mode 0700, file mode 0600, owned by the user the agent runs as (the shipped unit sets no `User=`, so root).
* Format: one JSON object, `{ "<key>": { <record> }, ... }`. Keys: `provider:secret:originalIDs` for rotations started by the trigger engine, `journal:rot-<hex>` for journal-only records and for unresolved records that a later dispatch displaced (F1 fix), `preserved:...` for a displaced legacy record with no rotation_id.
* Record fields (`RotationState`): `provider_name`, `secret_name`, `original_container_id` (comma-joined legacy form), `original_container_ids[]`, `new_container_id`, `rotation_id`, `phase`, `service_id` (the real container name), `owner_id`, `strategy` (`rolling`, `restart`, `signal`), `candidate_container_ids[]`, `secret_version` (opaque, never a value), `transition`, `pending_intent`, `expected_proxy_target`, `observed_proxy_target`, `committed`, `cleanup_status`, `seq`, `operation_state`, `manual_recovery`, `detail`, `status`, `start_time`, `last_update`, `error_message`.
* `status` values: `in_progress`, `completed`, `rollback_required`, `recovered`, `critical_error`. Only `critical_error` and an `in_progress` inherited from a previous process block the service (`operation_guard.go`). `rollback_required` does not block.
* `operation_state` named conditions (signal strategy): `nothing_published`, `partially_published`, `published_verification_failed`, `published_signal_not_sent`, `signal_sent_outcome_unknown`, `reload_requested_adoption_unverified` (terminal, completed, adoption never verified).
* `transition` values are the constants in `internal/rotation/journal.go` (`intent_create_candidate`, `candidate_ready`, `intent_handoff`, `handoff_done`, `intent_swap`, `swap_committed`, `intent_cleanup`, `completed`, `intent_rollback`, `rolled_back`, `intent_rename_original`, `intent_stop_original`, `intent_start_replacement`, `intent_signal_publish`, `signal_published`, `intent_signal_reload`, `signal_reload_sent`, `signal_done`, `signal_failed`, ...). An `intent_*` in `pending_intent` means "the action may or may not have happened".

Read the journal without printing anything secret (it contains none, but use a read-only copy anyway):

```
sudo ls -l /var/lib/dso/state/
sudo python3 - <<'PY'
import json
d = json.load(open('/var/lib/dso/state/rotations.json'))
for k, r in d.items():
    if r.get('status') in ('critical_error', 'in_progress', 'rollback_required'):
        print(k)
        for f in ('rotation_id','service_id','strategy','status','transition','pending_intent','operation_state',
                  'manual_recovery','committed','seq','start_time','last_update','error_message',
                  'original_container_ids','candidate_container_ids','expected_proxy_target','observed_proxy_target'):
            print('   ', f, '=', r.get(f))
PY
```

Agent logs (`docker dso system logs -n 200`, or `sudo journalctl -u dso-agent --no-pager --since "2 hours ago"`). Messages the code emits, exact start of text:

* `Detected unfinished rotations, performing safe recovery`
* `Recovery cannot establish a safe state; all workloads preserved, manual recovery required` (field `reason` lists what was preserved)
* `Interrupted signal rotation left uncertain; no automatic re-delivery or re-signal`
* `Recovery complete: a verified healthy workload exists for every service`
* `Preserving rotation artifact: ...` (two variants: no verified healthy holder, or the journal does not authorise retirement)
* `Rotation refused: the service has an unresolved operation; it is not mutated until that is reconciled`
* `Persisted rotation state is unreadable: automatic recovery is DISABLED ...`

If the journal is unreadable: `rotations.json` is renamed `rotations.json.corrupt-<unix seconds>` in the same directory.

## 2. Inspect the affected service and the journal

For each record from section 1 note: `service_id`, `strategy`, `transition`, `pending_intent`, `operation_state`, `committed`, `original_container_ids`, `candidate_container_ids`, `rotation_id`. Then list everything related to the service (names only, no env):

```
docker ps -a --no-trunc --filter "name=<service_id>" \
  --format 'table {{.ID}}\t{{.Names}}\t{{.Status}}\t{{.Label "dso.owner"}}\t{{.Label "dso.rotation.id"}}'
```

## 3. Identify original versus candidate

* Original: the container ID(s) in `original_container_ids` (legacy field `original_container_id` is the same IDs comma-joined).
* Candidate: the ID(s) in `candidate_container_ids`, and any container carrying label `dso.rotation.id=<rotation_id>` (set at creation, so it exists even if the crash happened before the ID was journalled).
* Rolling strategy artifacts: candidate `<service>_dso_new_<ts>` before the swap; after a swap the previous workload is `<service>_dso_backup_<ts>`; the restart strategy leaves `<service>_old_<unix ts>`. The real service name `<service_id>` is held by exactly one container, or by none (dangerous).
* A container with none of these identities is not part of the operation: do not touch it.

## 4. Verify ownership and exact container identity

Use FULL container IDs (64 hex), never name or short prefix, in every command below.

```
docker inspect --format '{{.Id}} name={{.Name}} running={{.State.Running}} status={{.State.Status}} health={{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}} started={{.State.StartedAt}} owner={{index .Config.Labels "dso.owner"}} rotation={{index .Config.Labels "dso.rotation.id"}} host_ports={{index .Config.Labels "dso.host_ports"}}' <full-id>
```

Ownership rule used by recovery (`ownedByMe`): `dso.owner` must equal the agent's `agent.owner_id` (config). Show the agent's value with `dso config show` (check that the owner id is printed there) [UNVERIFIED: the exact output of `dso config show` for owner_id]. A container whose owner differs or is absent is NOT owned: DSO will not act on it and neither should you without a maintainer.

## 5. Inspect actual routing: what an operator can and cannot see

* The DSO proxy is **embedded in the `dso agent` process**. Its backend table is **in memory only**; it is not persisted and there is no CLI or documented endpoint to dump it (design doc 3a, 4). After an agent restart, the table is rebuilt from running containers that carry the `dso.host_ports` label; the previous routing is gone.
* Observable: which process listens on the proxied host port: `sudo ss -ltnp 'sport = :<host_port>'` (should be `dso`, not Docker's own published port). A TCP request through the port shows which workload answers only if the application reveals it (for example a version or hostname endpoint): that is application-specific and not guaranteed.
* Journal hints (`expected_proxy_target`, `observed_proxy_target`, `committed`) record what DSO intended and observed at some point. They are **not** proof of current routing.
* For unproxied services (no `dso.host_ports`), DSO has no routing signal at all. A healthy container holding the real name is not proof that your load balancer or service discovery sends traffic to it.
* Therefore: if you cannot show which container receives traffic, treat routing as **unknown** and use the "cannot establish" rule.

## 6. Verify readiness and secret-delivery status without exposing secret values

Readiness (health status is safe to print): `docker inspect --format '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{else}}no healthcheck{{end}}' <full-id>`.

Secret files (file-mode secrets live by default under `/run/secrets/dso` on a tmpfs in the container; the injector writes them mode 0400 and refuses to write if the directory is not tmpfs). Check existence, mode, owner, size, mtime, and that the mount is tmpfs, never contents:

```
docker exec <full-id> sh -c 'stat -c "%a %U:%G %s bytes mtime=%y %n" /run/secrets/dso/*; grep " /run/secrets/dso " /proc/mounts | cut -d" " -f1-3'
```

Do not use `cat`, `less`, `head`, or `env` on those files. If the image has no `sh`/`stat` the check fails: that is "cannot establish", not "ok". If you must compare two containers' files, compare checksums computed inside the container (`sha256sum`) and compare the hashes only; do not paste or store them next to the secret name in a shared ticket if the hash could be matched against known values [UNVERIFIED: whether `sha256sum` exists in a given image].

Environment-mode secrets: **`docker inspect` prints `Config.Env` in plaintext.** Always use `--format` with explicit fields as above. Never run bare `docker inspect`, `docker compose config`, or `docker inspect | jq` into a log or ticket.

A "complete" file set means every file listed under the service's file-secret configuration exists with the expected mode and a fresh mtime; for `partially_published` it is by definition not guaranteed.

## 7. When to stop the agent to prevent concurrent mutation

The journal is rewritten by the running agent on every state change and on shutdown (`Close()` persists the in-memory state). Editing it while the agent runs is unsafe: the next persist overwrites your edit. **Stop the agent before any inspection step that could mutate, before backup, and before any edit.**

```
sudo systemctl stop dso-agent        # unit name verified in internal/setup/planner.go (dso-agent.service); Restart=on-failure does not restart after a manual stop
systemctl is-active dso-agent         # expect "inactive"
pgrep -a -f 'dso agent'               # expect nothing
```

Effects of stopping: DSO stops rotating and its embedded proxy stops, so traffic that goes **through the DSO proxy is interrupted** until the agent is back (design doc section 4). Containers keep running. If the service is proxy-managed and live, weigh this against the risk, and prefer escalation to a maintainer before stopping. Stopping the agent does not by itself clear any block: the guard rebuilds from the journal on the next start.

Read-only observation (sections 1 to 6) does not need the agent stopped. A stopped agent has no journal writes, so stop it before you take the backup in section 8 so the copy is consistent.

## 8. Back up the journal securely

```
sudo install -d -m 0700 -o root -g root /root/dso-recovery
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
sudo cp -a /var/lib/dso/state/rotations.json /root/dso-recovery/rotations.json.$STAMP
sudo cp -a /var/lib/dso/state/rotations.json.corrupt-* /root/dso-recovery/ 2>/dev/null   # if present
sudo chmod 0600 /root/dso-recovery/rotations.json.*
sudo sha256sum /var/lib/dso/state/rotations.json /root/dso-recovery/rotations.json.$STAMP
```

The two hashes must match. Record the hash (section 10). Keep the backup in a root-only directory: the file holds no secret values by design (opaque version ids only), but it contains service names, container IDs and the host layout. Also save, for the ticket, the agent log window (`sudo journalctl -u dso-agent --since "<start>" > /root/dso-recovery/agent.$STAMP.log`, mode 0600) after reading it for anything sensitive.

## 9. Decide whether manual intervention is safe

Establish, for the record: A) original container ID and state; B) candidate container ID(s) and state; C) which one serves traffic (section 5); D) readiness (section 6); E) for signal rotations, the secret-file state (section 6).

| Observed record / state | What DSO already did | Safe operator action | If you cannot establish A-E |
|---|---|---|---|
| `strategy=signal`, `operation_state=partially_published` (`critical_error`) | Nothing in Docker touched; no re-delivery, no re-signal. The service is flagged degraded and blocked. | Secret set is inconsistent (some files new, some old). **Do not blindly resend the signal and do not re-run the rotation.** Re-deliver the complete, correct secret set by the service's normal deployment path (or recreate the container), confirm all files, mode, mtime (section 6), reload or restart the container yourself, verify the application works, then release per section 11. | Preserve, escalate. |
| `signal` with `operation_state` `published_verification_failed`, `published_signal_not_sent`, `signal_sent_outcome_unknown` | Same | Files may be new and the application may not have reloaded (or did, with unknown result). Check application behaviour, reload or restart once yourself after confirming files; a repeated SIGHUP is not known to be safe for every application and an older credential may already be revoked. | Preserve, escalate. |
| `signal`, interrupted at `intent_signal_publish`, `signal_published`, `intent_signal_reload`, `signal_reload_sent` (record still `in_progress`, becomes `critical_error` at next start) | Same | As the two rows above, by the last transition. | Preserve, escalate. |
| `signal`, `operation_state=nothing_published`, status `recovered` | Previous value intact | None needed. | n/a |
| `rolling` or `restart` interrupted before any swap: `transition` in `intent_create_candidate`, `candidate_created`, `candidate_ready` | Recovery retires the unverified candidate only when an owned, healthy original holds the real name. | Usually auto-resolved on restart. If `critical_error`: confirm the original holds the real name, is healthy, owned, serves traffic; then the candidate (`_dso_new_` / `dso.rotation.id`) may be removed by full ID, one at a time, after you record it. | Preserve both, escalate. |
| `rolling`, `intent_swap` **without** `swap_committed` (or `between_renames`) | Recovery stops: the names may be half-renamed. Both workloads preserved; the guard blocks. | **No safe self-service procedure.** Which container holds the real name, which holds the backup name and which receives traffic depends on exactly which rename ran. | **Maintainer-assisted recovery required.** Do not rename, remove or restart either container. |
| `rolling`, `intent_handoff` / `handoff_done` with no committed evidence, proxied | Both workloads preserved unless live proxy table proves the holder. | Only a maintainer should decide (the proxy table is gone after a restart). | **Maintainer-assisted recovery required.** |
| `rolling`, `swap_committed` / `intent_cleanup` (candidate serves, backup remains) | Recovery retires the backup only if the candidate holds the name healthy and live routing is established | If `critical_error`: confirm candidate holds the real name, healthy, owned, receiving traffic; then the `_dso_backup_` container may be removed by full ID. | Preserve, escalate. |
| `restart` strategy, original renamed away, no healthy holder | Preserved; not auto-renamed back (design doc 3b) | Rename-back / start of the original is a manual, maintainer-assisted action: it can create a duplicate that claims the same ports. | **Maintainer-assisted recovery required.** |
| Original container missing (`Original container not found` in the log) and no committed swap | Preserved as critical | Do not recreate by guess. | **Maintainer-assisted recovery required.** |
| Ownership mismatch (`original container ... is not owned by this agent`) | Preserved | Not DSO's to resolve: check `agent.owner_id` and the container's `dso.owner` label; another agent or an unmanaged container may own it. | Escalate. |
| Journal unreadable: `rotations.json.corrupt-<ts>` exists, log says `automatic recovery is DISABLED` | No container touched; **all rotation blocked** | Keep the quarantined file untouched (copy per section 8). Establish workload and routing state for every DSO service by sections 2 to 6. **Do not just restart**: on a clean stop the agent writes a new empty journal, and the next start resumes rotation with no memory of the interrupted operation. | Preserve, escalate. |
| `rollback_required` | Written by the engine for any dispatch error; it proves only that an attempt failed, not that its rollback finished. Does not block by itself, but it is never expired by age (no 24h discard): at every start recovery examines it like an interrupted operation and moves it to `recovered` only when a verified healthy workload exists, otherwise to `critical_error` (blocks). | Normally nothing: it settles at the next start. If it persists across restarts or the log says `Failed ... rollback FAILED` / service degraded, treat the service as unresolved (sections 2 to 6). | Preserve, escalate if workload is unclear. |
| Missing record but leftover `_dso_*` / `_old_*` containers | Recovery removes artifacts only if an owned, verified healthy container holds the real name; proxy-managed artifacts with no journal record are never removed | Leave them (see build caveat, section 0). | Preserve, escalate. |

General rule: a container may be removed or renamed manually only when all five of A to E are established AND the table row allows it. Removing a container is irreversible; stop at "preserve" if in doubt.

## 10. Record the operator's decision

Copy this into the incident ticket before acting on any row of the table.

```
Operator:                     <name, role>
Date/time (UTC):              <timestamp>
Host / agent unit:            <host> dso-agent.service
Agent build:                  <dso version output>; commit <sha>; includes F1/F2 fixes: yes/no
Journal key / rotation_id:    <key> / <rotation_id>
Service (service_id):         <name>
Strategy / transition:        <rolling|restart|signal> / <transition>   pending_intent: <..>
operation_state/manual_rec.:  <..> / <true|false>
status / error_message:       <..>
Original container (full ID): <64 hex>   state: <..>   owner label ok: yes/no
Candidate container(s):       <64 hex>   state: <..>   rotation label match: yes/no
Which container serves traffic, and how established: <evidence, or "UNKNOWN">
Readiness evidence:           <health status, application check>
Secret-file evidence:         <file list, modes, mtimes, tmpfs yes/no (no values, no hashes of values)>
Journal backup:               <path>  sha256 <hash>  (copy verified identical: yes/no)
Quarantined file (if any):    <path> sha256 <hash>
Decision:                     <preserve+escalate | action taken>
Exact commands run:           <list>
Result and verification:      <logs, status output>
Escalated to / approved by:   <name or "not required">
```

## 11. Restart the agent and verify; how the block is released

### 11a. Release by recovery (preferred, no edit)

When the operator has repaired the service (or nothing was wrong), start the agent and let recovery re-evaluate:

```
sudo systemctl start dso-agent
docker dso system status
docker dso system logs -n 100
```

Startup order (design doc 3c): the journal loads (`Loaded rotation states`), `BeginStartup` blocks every dispatch path, the embedded proxy rebuilds its table from running labelled containers, recovery runs (`Detected unfinished rotations...`), then the gate is released and pollers start. Expected outcomes per record: `Recovery complete: a verified healthy workload exists...` then the record becomes `recovered` and the service is no longer blocked; or `Recovery cannot establish a safe state ...` and the record stays `critical_error`, the service is re-flagged degraded, and a rotation attempt logs `Rotation refused: the service has an unresolved operation ...`.

How the OperationGuard releases: `RotationBlocked` checks, per target, (1) startup recovery not yet settled, (2) journal unreadable, (3) a record for the service (matched by `service_id`, original or candidate container IDs) with `status=critical_error`, or `in_progress` inherited from a previous process. It has no timer and no override; it releases only when the record's status changes to something else (or the record is gone). **Signal-strategy records can never be released by recovery**: `recoverInterruptedSignal` always ends manual, by design, and the only rotation that could supersede the record is itself refused by the guard. For these, release is by the validated edit below.

### 11b. Release by editing the journal (only when 11a cannot release it)

Preconditions, all required:

1. Sections 2 to 6 completed and recorded (section 10): the service is serving correctly, the secret set is complete and current, the application has loaded it (or you restarted it yourself), and you can state which container serves.
2. The table row for the state allows manual release; the state is NOT in a "maintainer-assisted" row (`intent_swap` without commit, handoff without evidence, restart-strategy renamed-away original, missing original, ownership mismatch, unreadable journal). For those, stop here and escalate.
3. Agent stopped (section 7), journal backed up and hashed (section 8).

Procedure (change exactly one field of exactly one record):

```
sudo python3 - <<'PY'
import json, os, sys
P = '/var/lib/dso/state/rotations.json'
RID = '<rotation_id of the record>'
d = json.load(open(P))
hits = [k for k, r in d.items() if r.get('rotation_id') == RID]
if len(hits) != 1:
    sys.exit('expected exactly one record with that rotation_id, found %d' % len(hits))
r = d[hits[0]]
if r.get('status') != 'critical_error':
    sys.exit('record is %r, refusing' % r.get('status'))
r['status'] = 'recovered'
r['detail'] = 'released by operator <name> <UTC time>, ticket <id>'
tmp = P + '.edit'
with open(tmp, 'w') as f:
    json.dump(d, f, indent=2)
os.chmod(tmp, 0o600)
os.rename(tmp, P)
PY
sudo python3 -m json.tool /var/lib/dso/state/rotations.json >/dev/null && echo valid
sudo ls -l /var/lib/dso/state/rotations.json     # expect -rw------- root
```

Why this edit is accepted (read from `loadStates`): the file is decoded with `json.Unmarshal` into `map[string]*RotationState`; there is no schema or enum validation of `status`, `detail` or any field; only an `in_progress` status makes a record "inherited"; recovery considers only `in_progress`, `rollback_required` and `critical_error`; the guard blocks only `critical_error` and inherited `in_progress`; the startup re-flag loop looks only at `critical_error`. So a record with `status: "recovered"` is not examined or blocked, and it is aged out by `CleanupOldStates` after the retention period. Keep `error_message` and the other fields: they are the evidence.

Limits and hazards:

* **Invalid JSON is worse than the problem.** If the file does not parse, the agent quarantines it, disables recovery and blocks all rotation. Always validate (command above) before start. Keep the exact key/structure; do not reformat other records.
* Do not change `rotation_id`, `service_id`, `original_container_ids`, or `seq`. Do not delete the record: deletion destroys the evidence and the rotation identity.
* Do not edit while the agent runs (section 7); a `rotations.json.tmp` left by a killed agent is ignored and overwritten, leave it.
* **After release the first poll runs a rotation.** The agent keeps no secret baseline across restarts, so on start it dispatches an initialisation rotation for every secret. For a signal-strategy service that means files are re-delivered and the reload signal sent again, immediately. Make sure that is acceptable (the secret set is correct at the source) before you start the agent, or do the release in a maintenance window.
* Released records are not re-flagged on restart; if the application is in fact still broken, DSO will not tell you.

Then:

```
sudo systemctl start dso-agent
docker dso system logs -n 100         # expect: Loaded rotation states; no "Recovery cannot establish a safe state" for the record; no "Rotation refused" for the service
dso status                       # Pending should drop; dso doctor: DSO-DOCTOR-018/019/020
```

Verify the service independently of DSO (health check, application endpoint, secret-file state per section 6). Write the outcome into the section 10 record.

## M1 backlog item: recovery inspection, reconciliation and acknowledgement interface

(The existing backlog, `docs/audit/10-PRIORITIZED-GAP-BACKLOG.md` and `gap-backlog.json`, is maintained by another workstream and was not edited here; add this as a new DSO-G entry there when convenient.)

Proposed item, not implemented:

* **Title:** operator interface for unresolved rotation operations.
* **Need:** today the operator has a count (`dso status`), raw JSON and logs; releasing a block means hand-editing `rotations.json` with the agent stopped.
* **Scope:** (1) `dso recovery list|show <rotation_id>` over the agent socket: records with transition, pending intent, `operation_state`, identities, and a live comparison with Docker (original, candidate, ownership, health) plus the proxy's live routing table; (2) `dso recovery acknowledge <rotation_id> --reason --expect-state <status>` performed by the agent: compare-and-set on `seq`, audited with actor and reason, refuses states that need a maintainer, never edits other fields; (3) `dso recovery reconcile <rotation_id>` to re-run recovery for one record; (4) persisted proxy routing hints, or an endpoint to dump the in-memory table; (5) identity-based (RotationID) finish of rotations, replacing the key-based `CompleteRotation`/`MarkRollback`/`DeleteState`; (6) per-file progress for signal rotation in the journal; (7) a persisted secret baseline so a release does not trigger an immediate re-delivery.
* **Acceptance:** a block can be listed, inspected and released without touching the file; every release is audited; tests cover stale acknowledge, concurrent acknowledge, and the refused states.
* **Priority:** P1, release-blocking for any claim of operator-manageable recovery; depends on the M0 candidate.

## Claims in this runbook not verified from code

1. Exact text and fields printed by `dso doctor`, `dso status`, `dso config show` (only the command wiring, the status struct and the doctor check source were read, not executed); whether `dso config show` prints `agent.owner_id`.
2. That `docker dso inspect` / `docker dso logs` do not read the journal (only names and Use lines were read).
3. The agent runs as root in all installs: the repo's unit has no `User=`; other templates (bootstrap, the older `repair_service.go` unit using `/usr/local/bin/dso-agent`) differ in binary name, and I did not read the bootstrap templates. Unit name `dso-agent.service` and `ExecStart=/usr/local/bin/dso agent` are from `planner.go`.
4. `sha256sum`, `stat`, `sh` exist inside the target container images; the secret directory is `/run/secrets/dso` only when the service uses the default (`spec.Path` is configurable).
5. Behaviour of a real Docker daemon for the `docker inspect --format` / `docker ps --format` templates (written from Docker CLI knowledge, not run here).
6. The statement that the proxy rebuilds from `dso.host_ports`-labelled running containers after restart and that traffic through it is interrupted while the agent is stopped: taken from the design document and `proxy_reconstruction.go`, not tested against a real daemon (the design document itself lists real-process and real-Docker behaviour as UNVERIFIED).
7. That the `partially_published` / signal rows' operator actions (re-deliver by normal deployment path, restart the application) are safe for a given application: depends on the application; the code only guarantees DSO will not do it automatically.
8. That no code path other than the ones read can mark a record `recovered` (a full call-graph audit of `status` writes was not done; the writers found are in `state_tracker.go`, `recovery.go` and `trigger.go`).
9. The edit procedure was validated by reading `loadStates`, `GetUnfinishedRotationsAtStartup`, `unresolvedFor`, the re-flag loop in `StartAll` and `CleanupOldStates`, not by running the agent (not permitted here). No automated test loads a hand-edited file.
10. The F1/F2 fixes are uncommitted and tested only with the fake-Docker unit tests in `internal/agent`; nothing was exercised against a real daemon.
