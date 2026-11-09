# M0 — PE-08 Evidence Plan: Where Do File-Mode Secrets Actually Land?

**Date:** 2026-11-08 · **Status:** current behaviour is **inferred, not verified**. Nothing was executed against Docker in M0. Labels as in `m0-baseline-and-reconciliation.md`.

## 1. What the code does today

Two different file-delivery mechanisms exist:

| Mechanism | Used by | How | Claims |
|---|---|---|---|
| **M-A `injector.InjectFiles`** — `docker exec /bin/sh -c` with base64 over stdin, `mv` atomic, `chmod 0400`, fixed dir `/run/secrets/dso/` | Local mode (`dso up`) on container `start` event (`internal/agent/agent.go:183-228`) | Runs **inside** the container's mount namespace after start | Doc comment: "correctly targets tmpfs mounts … (not accessible via CopyToContainer from the host overlay layer)" (`internal/injector/inject.go:24-26`) |
| **M-B `rotation.StreamSecretToContainer`** — in-memory tar, `CopyToContainer(ctx, id, "/", …)`, entries `filepath.Join(fs.Path, base(name))`, mode `0400` | Agent rolling path **before start** (`internal/rotation/rolling_strategy.go:245-259`); agent restart path after create (`internal/watcher/controller.go:1393`); fallback restart (`controller.go:1913-1920`); CLI `docker dso inject` on a **running** container (`internal/cli/inject.go:136`) | Docker archive API, i.e. the daemon extracts into the container's root filesystem from the host side | Doc comment: "uploads secrets into a container's tmpfs mount" (`internal/rotation/tar_streamer.go:26`) — **contradicts** M-A's comment |

Where the tmpfs comes from: only the generated local-mode compose adds `tmpfs: [<inject.path>]` (`internal/core/compose.go:257-258,291-292`). In agent mode with a user-written Compose file, a tmpfs exists only if the user declared one. The shadow/recreated container inherits `HostConfig` (including `Tmpfs` and `Mounts`) from the original (`internal/rotation/container_clone.go:69-86`).

Post-start verification: `ExecProbe` runs `test -s <fs.Path>` (`internal/rotation/health_check.go:88`) with `fs.Path` = the **directory** (`rolling_strategy.go:291`). `test -s` is true for a directory whose reported size is non-zero (on tmpfs and overlayfs, an empty directory typically reports a non-zero size) ⇒ the probe likely passes even when no secret file is visible (PE-32, inferred).

## 2. Hypotheses to decide

| ID | Case | Expected per Docker's documented behaviour ("copying into tmpfs is not possible") | Consequence if true |
|---|---|---|---|
| H1 | M-B, target path **without** tmpfs (agent mode, typical user compose; also `test/integration/rotation_test.go:91-199`) | File written to the container's writable layer = host disk under `/var/lib/docker/overlay2/<id>/diff/…` for the container's lifetime; remains in freed blocks after removal | "Never written to host disk" claim is false for this configuration — **near-certain by construction**, still to be measured |
| H2 | M-B **before start**, path **is** a tmpfs mount (rolling/restart in local-mode-generated services) | Archive lands in writable layer; at start the tmpfs is mounted over the directory and **hides** it | Secret on host disk **and** app sees an empty directory; probe passes vacuously (PE-32) ⇒ rotation "succeeds", app fails to read secret |
| H3 | M-B into a **running** container's tmpfs (`docker dso inject`) | Daemon extracts outside the container's mount namespace ⇒ writable layer, invisible to the app | Same as H2 |
| H4 | Docker returns an error for H2/H3 | — | Rotation fails and rolls back (availability bug, no leak) |
| H5 | M-A into tmpfs | File only in tmpfs (RAM), not on host disk | Guarantee holds for local mode |

## 3. Test design (Docker lane scenario S9; Linux runner; root needed to read `/var/lib/docker`)

Use a unique high-entropy sentinel per run, e.g. `DSO_PE08_<random 32 hex>`; never a real secret.

**Fixtures** (one test per row, table-driven):

| Fixture | Image | Mount at `/run/secrets/dso` | Delivery |
|---|---|---|---|
| F1 | `alpine:3.20` (`sleep infinity`) | none | M-B rolling (`RollingStrategy.Execute`) |
| F2 | `alpine:3.20` | `HostConfig.Tmpfs{"/run/secrets/dso": "mode=0700"}` | M-B rolling |
| F3 | `alpine:3.20` | tmpfs via `Mounts[{Type: tmpfs}]` (long syntax) | M-B rolling |
| F4 | `alpine:3.20` | tmpfs | M-B restart path (`executeSimpleRestart`) |
| F5 | `alpine:3.20` running | tmpfs | M-B on running container (CLI inject path) |
| F6 | `alpine:3.20` running | tmpfs | M-A `InjectFiles` |
| F7 | `gcr.io/distroless/static` | tmpfs | M-A (expect explicit failure: no `/bin/sh`, PE-17) |

**Observations per fixture (all recorded, then asserted):**

1. **Delivery result** — error returned or not; rotation outcome.
2. **App view** — `docker exec <c> cat /run/secrets/dso/<name>`: sentinel present? (`ls -la` too; record mode/uid.)
3. **Mount view** — `docker exec <c> cat /proc/self/mountinfo | grep /run/secrets/dso`: confirm tmpfs actually mounted (fs type `tmpfs`).
4. **Writable layer** — `docker inspect -f '{{.GraphDriver.Data.UpperDir}}' <c>`; on the host: `grep -rl --binary-files=text "$SENTINEL" "$UPPERDIR"`; for removed shadows also scan the backup/old container's UpperDir before cleanup.
5. **Whole Docker root** (strongest check, after the scenario): `grep -rl --binary-files=text "$SENTINEL" /var/lib/docker/overlay2 /var/lib/docker/containers 2>/dev/null` — must be empty for tmpfs fixtures. (Freed-block remanence is out of scope; note it.)
6. **`docker diff <c>`** — lists `A /run/secrets/dso/<name>` when the file is in the writable layer (cheap, no root needed; corroborates 4).
7. **`docker inspect`** JSON — sentinel absent (existing assertion, keep).
8. **Probe honesty** — with F2, run `ExecProbe` on the path and record whether it passes while (2) shows no file ⇒ decides PE-32.
9. **Persistence across restart** — `docker restart <c>` then repeat 2 and 4: tmpfs content must be gone; writable-layer copy (if any) still present.

**Pass criteria (defines the fixed behaviour for M3):** for every tmpfs fixture, (2) shows the sentinel **and** (4)(5)(6) find nothing. For F1 (no tmpfs) the test documents the leak and the product must either refuse file mode without a tmpfs/secret mount (`dso validate` error) or create one; owner decision.

**Decision table for the result:**

| Observed | Conclusion |
|---|---|
| F2/F3 app view empty, UpperDir has sentinel | H2 confirmed ⇒ PE-08 High confirmed, PE-32 confirmed if probe passed |
| F2/F3 app view has sentinel, UpperDir clean | PE-08 refuted for this Docker version — record version, keep the test as regression |
| Error from CopyToContainer | H4 ⇒ file-mode rolling never works with tmpfs; availability defect |
| F6 app view has sentinel, UpperDir clean | H5 confirmed — M-A is the safe primitive to reuse |

Run on at least the CI runner's Docker version and one older supported Engine (e.g. 24.x) because archive/tmpfs handling is daemon-version dependent (unverified assumption).

## 4. Proposed fix direction (M3, after evidence)

Deliver file secrets **after start, inside the container**, before traffic switch: start shadow → `InjectFiles`-style exec delivery into the tmpfs (generalised to `fs.Path` with the same filename regex) → probe that checks **each file** (`test -s <dir>/<name>`) → readiness → switch. For images without a shell: fail closed with guidance (env mode or a static helper), never fall back to `CopyToContainer`. Remove or rename the misleading `StreamSecretToContainer` doc comment.
