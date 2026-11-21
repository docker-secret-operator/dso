# G024 disposition: dead-code and duplicate-path decisions

Status: DECIDED 2026-11-16 (owner-approved reading of D1). Scope: documentation plus
package doc comments only. No production code was removed.

> Provenance note: citations of `docs/audit/...` in this document point to local, intentionally untracked working notes (repository policy, commit 1bf9f6c). Every requirement and decision this document relies on is stated here; the citations only record where it came from.

## 1. G024 versus G027

G024 (`docs/audit/gap-backlog.json`, id `DSO-G024`; `docs/audit/10-PRIORITIZED-GAP-BACKLOG.md:30`) is an
architecture item: "Decide fate of internal/ledger, internal/polling, pkg/backend, duplicate
installers/injection paths". Acceptance: "Dead code removed or wired; one installer". It says nothing about a
vault. The encrypted-vault product track is a different item, G027 (`DSO-G027`, "Encrypted vault product track
(projects/envs/versioning/RBAC)"). The roadmap row for M2 ("G024 groundwork") conflated the two. The owner
decision is: G024 means the disposition below; the limited local vault work is a separate "G027-lite" track
(see `docs/design/m2-control-plane-scope.md`). The backlog JSON has no status field, so it was not edited.

Module path: `github.com/docker-secret-operator/dso` (`go.mod`). Everything under `pkg/` is importable by path.
README and `docs/*.md` do not document `pkg/` as a stable public API and DSO ships as a binary, but because
external importability cannot be disproved, nothing under `pkg/` is deleted on importer count alone.

## 2. Dispositions

| Item | Disposition | Action |
|---|---|---|
| `internal/ledger` | retain-active | none |
| `internal/polling` | retain-approved-future (documented, unwired) | package doc comment `internal/polling/doc.go` |
| `pkg/backend` (root) | retain-public-compat | package doc comment `pkg/backend/doc.go` |
| `pkg/backend/env`, `pkg/backend/file` | retain-active | none |
| Duplicate installer paths | retain-active; no duplicate exists | none (installer architecture not reopened) |
| Duplicate injection paths | retain-active (`internal/injector`); legacy `StreamSecretToContainer` retained for diagnostics; `InjectedSecretSync` is an unreferenced wrapper, removed in a follow-up commit (zero callers) | none in this change (see 2.5) |

### 2.1 internal/ledger: retain-active
- Non-test importers: `internal/agent/ledger_wiring.go`, `ledger_bridge.go` (plus their tests and
  `internal/ledger/reference_guard_test.go`). `go list -deps ./cmd/...` includes `internal/ledger`.
- Docs: ADR-003 makes the persisted ledger the source of truth for what is running.
- Replacement/obsolescence: none. Wired in M1.

### 2.2 internal/polling: retain-approved-future
- Importers: no production importer. Test-only: its own tests and
  `internal/integration_test/smart_polling_integration_test.go`. Not in `go list -deps ./cmd/...`.
- Docs: `docs/SMART_POLLING.md` (status banner) states it is implemented and unit-tested but not wired, and
  keeps it as a design reference for adoption inside `TriggerEngine.StartPolling`
  (`internal/agent/trigger.go`); `CHANGELOG.md` records the same and that `Agent.poller` was removed;
  `docs/product-evolution/current-architecture-audit.md:33` lists it as not on a production path.
- Replacement: `TriggerEngine.StartPolling` has its own interval-with-backoff logic. SmartPoller is not
  replaced, just not adopted.
- Why not removed: it has a documented future-use commitment and an integration test importer, so it fails the
  "no importers, no doc commitments" bar. Caveat: the future use is documented, not scheduled; no ADR or
  milestone commits to adoption. This is a retain, not a promise to wire it.
- Action: `internal/polling/doc.go` states "retained, not wired" so nobody assumes API-call savings exist.

### 2.3 pkg/backend: retain-public-compat (root), retain-active (subpackages)
- The root package has no production file, only `watch_firstdelivery_test.go` (package `backend_test`, imports
  the two subpackages).
- `pkg/backend/env` and `pkg/backend/file` are imported by `pkg/provider/load.go` as the in-process "env" and
  "file" providers, and are in the `cmd` dependency graph. They are active. (They are also the subject of
  PE-11/PE-28 security analysis in `docs/product-evolution/m0-*`; not a disposition matter here.)
- The root directory is empty of production code, harmless, and under `pkg/`; deleting it would only move a
  test and gain nothing, and would break a would-be external import path. Retained.
- Action: `pkg/backend/doc.go` records the status.

### 2.4 Duplicate installer paths: retain-active, no duplicate
- `scripts/install.sh` is the installer. `scripts/install.ps1` fails closed on Windows and installs nothing
  (header: refuses to install, M0-05). `internal/cli/setup.go` and `internal/cli/system_mgmt.go` plus
  `internal/bootstrap` are post-install configuration/bootstrap, a different responsibility. "One installer" is
  already satisfied. Installer architecture is not reopened.

### 2.5 Duplicate injection paths
- Active path: `internal/injector` (hardened exec injector), imported by `internal/cli/{inject,apply,sync,...}.go`,
  `internal/rotation/gate.go`, `internal/setup/doctor_rotation.go`.
- Legacy path: `StreamSecretToContainer` / `buildSecretTar` in `internal/rotation/tar_streamer.go`.
  `docs/design/n19-managed-delivery.md` section 1 states no managed path calls it; it remains for N19
  diagnostic scenarios G-I (`test/docker`) and tests. No non-test production caller was found by grep.
  Retained as diagnostic/test support.
- `InjectedSecretSync` (same file, `tar_streamer.go:111`) was a one-line wrapper with zero callers anywhere
  (`grep -rn InjectedSecretSync --include=*.go .`: definition only). It was clearly obsolete internal code and a
  safe removal candidate.
  It has since been removed, in a change coordinated with the concurrent `internal/rotation` workstream
  (commit `a3b2089`, "chore(rotation): remove InjectedSecretSync, a zero-caller dead wrapper (G024)");
  `go build ./... && go vet ./...` stayed green.

## 3. Verification (this change)
`go build ./...`, `go vet` and `go test -short` for `internal/polling` and `pkg/backend` pass; doc comments
were added, and (per §2.5, as a coordinated follow-up) `InjectedSecretSync` was subsequently removed from
`internal/rotation/tar_streamer.go` as a zero-caller dead wrapper (commit `a3b2089`).
