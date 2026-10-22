# Release Notes

## DSO Container Ownership

Each `dso.yaml` now carries a generated `agent.owner_id`, which is stamped as a `dso.owner` Docker label on containers managed by that configuration. This ownership identity — not the Docker network, Compose project, or secret name — determines which DSO agent can discover, rotate, restart, and recover a container.

Containers without a matching `dso.owner` label are never discovered, rotated, restarted, or recovered by an agent they don't belong to. This is default-deny by design: DSO does not guess ownership from any other signal.

### `owner_id` semantics

`agent.owner_id` is a **configuration/management-domain identity**, not a process ID and not a transient runtime value. It is generated once and persisted automatically into `dso.yaml` — it does not change across agent restarts, and it is not derived from the container ID, socket path, or Docker network.

### ⚠️ Copied configuration warning

If you copy a `dso.yaml` to a new, unrelated environment, **clear the existing `agent.owner_id`** before starting DSO so a new identity is generated. Otherwise, two configurations pointed at the same Docker daemon can share the same management identity and potentially manage overlapping containers.

Leave the identity unchanged only when the shared configuration is intentionally used for an HA/failover management pair — sharing the identity across such a pair is correct and expected.

### Upgrading existing installations

When upgrading from a pre-ownership DSO version, DSO automatically generates and persists an `agent.owner_id` in your configuration. Containers created by the previous version do not have the new `dso.owner` label and are therefore **not automatically adopted**.

These legacy containers will not receive secret rotations, restarts, or recovery from the upgraded agent until they are recreated/re-labeled. Re-run the appropriate DSO `apply`, `up`, or `compose` workflow to create containers carrying the new ownership label.

DSO intentionally does not automatically adopt ownerless containers — this is a security property, not a gap to be worked around.

After upgrading, run:

```
dso doctor
```

`DSO-DOCTOR-023` reports containers that look DSO-managed but are ownerless or belong to another management identity, with actionable guidance. This diagnostic is read-only — it never modifies, adopts, or mutates a container.

### Known limitations

1. A copied configuration file preserves `agent.owner_id`; clear it manually for unrelated environments (see warning above).
2. Legacy (pre-upgrade) ownerless containers require recreation or re-labeling before DSO will manage them again.
3. No `dso adopt` command exists yet for claiming legacy containers in place.
4. **Config-driven targets.** A container named in `secrets[].targets.containers` (matched by name, without the `dso.reloader` label) is an explicit operator opt-in: an ownerless container named there is still managed, but a container carrying a *different* agent's `dso.owner` is never adopted.
5. **Proxy discovery.** Every proxy registration path honors ownership: the startup scan (`ScanAndRegister`) and the Docker start-event path skip a container whose `dso.owner` names a different agent. As with config-driven targets, an ownerless container that is otherwise eligible (carries `dso.host_ports`) is still proxied for backward compatibility. Scans only read labels; they never write or claim ownership.
6. `SignalContainers` (`internal/injector`) has no callers and is unchanged. It is dormant legacy code: it signals every `dso.reloader=true`, `dso.update.strategy=signal` container without owner or secret filtering, so owner-aware filtering must be added before it is ever used.
7. When `EnsureOwnerID` has to write `agent.owner_id` into an existing `dso.yaml`, the file is rewritten tighten-only: owner/group read bits are kept, but group-write, world and execute bits are removed (`0664` becomes `0640`, `0600` stays `0600`).
