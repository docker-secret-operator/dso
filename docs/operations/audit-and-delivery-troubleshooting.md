# Troubleshooting: audit file and secret delivery

Read-only first steps. None of the commands below changes agent state.

## 1. Is the durable audit file working?

| Question | How to check |
|---|---|
| Is a durable file active? | Web UI Audit page note, or `GET /api/audit/status` (`durable_enabled`, `write_failures`). This does not verify the chain. |
| Is the chain consistent? | `dso audit verify` (add `--json` for a stable document; `--file PATH` to check a copy). |
| Is it in a healthy state on the host? | `dso doctor` check `DSO-DOCTOR-032` (agent mode; run as root or the agent user so the file can be read). |
| Can I get the records? | `GET /api/audit/export` (verified NDJSON). It answers 503 if the file is not active and 500, with no content, if the chain fails. |

`dso audit verify` exit status: `0` verified, `10` integrity failure, `11` verification unavailable (missing, unreadable or non-regular file; not evidence of tampering), `1` any other failure.

### Symptoms

| Symptom | Likely cause | What to do |
|---|---|---|
| Audit page says no durable file is active | The agent could not open `/var/lib/dso/state/audit.jsonl` (permissions, a broken chain, or a full disk) | Read the agent log for "Durable audit file unavailable", fix the cause, restart `dso-agent`. Until then events are logged only. |
| `write_failures` or `dso_audit_durable_write_failures_total` is above 0 | Disk full, I/O error or fsync failure while appending | Free space or fix the device. Those events exist only in the log stream. Failed appends are rolled back, so the file stays verifiable. |
| `dso audit verify` exits 10 | A record was edited, reordered, removed mid-file, or the file was damaged | Keep the file as evidence, move it aside and restart `dso-agent` to start a new chain. Do not edit it. |
| `DSO-DOCTOR-032` WARN: mode accessible to group or other | The file was created or changed with loose permissions | `chmod 600` the file and restart; the agent refuses to open a file that group or other can access. |
| Doctor shows INFO "cannot be read as this user" | The file is 0600 and owned by the agent | Re-run doctor as root or the agent user. |

### What a "verified" result means

The chain is unkeyed and best effort. A verified file is internally consistent. It does not prove who wrote it, and it cannot show that the newest records were not deleted, that it was not replaced by an older valid copy, or that no event was lost while persistence was unavailable. The audit "user" for bearer-token callers is a credential fingerprint, not a verified person. See `docs/design/m3-control-plane-decisions.md`.

## 2. Secret delivery and rotation state

Existing read-only tools:

- `dso status`: running agent, pending rotations and ledger state.
- `dso doctor`: checks `DSO-DOCTOR-018` to `DSO-DOCTOR-024` cover rotations awaiting recovery, leftover rotation containers, proxy port conflicts, file-mode destination reachability, unmanaged containers and the applied-version ledger.
- Web UI Events, Logs and Containers pages for the live rotation event stream.
- `docs/RECOVERY_PROCEDURES.md` and `docs/operations/MANUAL-RECOVERY-RUNBOOK.md` for recovery steps.

Remember that a rotation request accepted over REST (HTTP 202) means it started, not that it succeeded; the outcome arrives as `rotation_succeeded` or `rotation_failed` events.

## 3. Not covered

Real-Docker delivery qualification, live systemd behaviour and hosted CI have not been executed; this page describes what the code and its Docker-free tests establish.
