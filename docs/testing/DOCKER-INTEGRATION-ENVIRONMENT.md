# Docker integration test environment

Docker integration tests create, rename and remove containers. They must **never**
run against a developer's or production Docker daemon. This document describes
the only supported way to run them.

## Safety model (`test/dockerguard`)

Every Docker integration package installs the guard in `TestMain`:

```go
func TestMain(m *testing.M) { dockerguard.Main(m) }
```

| Condition | Result |
|---|---|
| `DSO_INTEGRATION_TESTS` not `1` (plain `go test ./...`) | tests skip; no Docker client is ever built |
| opted in, but endpoint / identity missing or ambiguous | the whole package **fails** (exit 1), it does not skip |
| `DOCKER_HOST` set and different from `DSO_TEST_DOCKER_HOST`, or `DOCKER_CONTEXT` set | fail |
| daemon ID ≠ `DSO_TEST_DAEMON_ID`, or ID on `DSO_TEST_PROHIBITED_DAEMON_IDS` | fail |
| daemon lacks label `dso.test.environment=<DSO_TEST_ENVIRONMENT_ID>` | fail |
| daemon already hosts a container without that label | fail (not a dedicated daemon) |
| a package without `dockerguard.Main` while opted in | its tests fail |

Verification is read-only (daemon info + container list); no container is created
to find out whether a daemon is safe. Socket paths, hostnames and context names
are never accepted as proof. The daemon label and daemon ID are provisioned by the
VM bootstrap, not by the test suite. All resources created by a run carry
`dso.test.environment` and `dso.test.run=<run id>`; cleanup removes only resources
carrying **both**, and never uses `docker prune`.

Variables: `DSO_INTEGRATION_TESTS=1`, `DSO_TEST_DOCKER_HOST` (unix:// or tcp://),
`DSO_TEST_DAEMON_ID`, `DSO_TEST_ENVIRONMENT_ID`, optional `DSO_TEST_RUN_ID`
(`dso-it-<8-16 hex>`), optional `DSO_TEST_PROHIBITED_DAEMON_IDS` (comma list).

Legacy suite: `test/integration` Docker tests predate the guard and are behind the
build tag `dockerint_legacy`; they are excluded from every normal build and must not
be run until migrated (tracked under M0-11).

## Environment

Use a **dedicated disposable Linux VM with standard rootful Docker Engine**, with the
DSO agent and the test runner both *inside* the VM (so bind mounts, local paths, Unix
sockets, permissions and networking behave as in production; a remote `DOCKER_HOST`
from a laptop gives misleading filesystem semantics).

Suggested (not measured): Ubuntu 24.04 LTS x86_64, 4 vCPU, 8-16 GB RAM, 80 GB disk,
Docker Engine + Compose v2, SSH restricted to your IP / SSM, no public application ports,
synthetic credentials only, destroyed or stopped after use. Rootless Docker may be used
later as an *additional* compatibility run; it does not replace rootful verification.

## Steps (on the VM)

1. Provision: `sudo env DSO_BOOTSTRAP_DISPOSABLE_VM=yes DOCKER_APT_VERSION=<exact apt version> ./scripts/integration/bootstrap-test-vm.sh --install-docker`
   (pinned Docker apt version, key fingerprint verified; refuses if the daemon has
   containers or `/etc/docker/daemon.json` exists). **Never run on your workstation.**
2. Install the Go version in `go.mod` from the official tarball and verify its SHA256
   (`https://go.dev/dl/`); add the test user to the `docker` group.
3. Sync the source (e.g. `git clone` the branch under test; record the commit SHA).
4. Run: `./scripts/integration/run-integration.sh`. It checks the root-owned marker
   `/etc/dso-integration/environment`, unsets ambient Docker config, runs
   `go test -tags dockerint -json ./test/docker/...`, fails on any skip or zero passes,
   writes logs to `artifacts/integration-<run id>/`, and cleans up its own resources.
5. Collect logs from `artifacts/`; cleanup of a specific run:
   `DSO_TEST_RUN_ID=<id> ./scripts/integration/cleanup-integration.sh`.
6. Tear down by destroying the VM.

Image-build verification (`docker build .`) belongs in this environment too and has
not yet been run for M0-09.
