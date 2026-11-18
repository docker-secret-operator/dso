#!/usr/bin/env bash
# run-integration.sh -- run the Docker integration suites against the designated
# test daemon, from INSIDE the disposable test VM (see
# docs/testing/DOCKER-INTEGRATION-ENVIRONMENT.md). Never run it against a
# development daemon: the isolation guard (test/dockerguard) will refuse, and
# this script checks the same prerequisites first.
set -euo pipefail

die() { echo "integration run refused: $*" >&2; exit 1; }

ENV_FILE=/etc/dso-integration/environment
[[ -f "$ENV_FILE" ]] || die "$ENV_FILE missing: not a provisioned test VM"
# The marker must be root-owned and not writable by anyone else, so the test
# user cannot forge it.
[[ "$(stat -c '%U' "$ENV_FILE")" == "root" ]] || die "$ENV_FILE is not owned by root"
perm="$(stat -c '%a' "$ENV_FILE")"
[[ "${perm: -1}" != "2" && "${perm: -1}" != "3" && "${perm: -1}" != "6" && "${perm: -1}" != "7" && "${perm:1:1}" != "2" && "${perm:1:1}" != "3" && "${perm:1:1}" != "6" && "${perm:1:1}" != "7" ]] \
  || die "$ENV_FILE is group/other writable"

# No ambient Docker configuration may influence the run.
for v in DOCKER_HOST DOCKER_CONTEXT DOCKER_CONFIG DOCKER_TLS_VERIFY DOCKER_CERT_PATH; do
  [[ -z "${!v:-}" ]] || die "$v is set; unset it (the endpoint is configured explicitly)"
done

# shellcheck disable=SC1090
source "$ENV_FILE"
export DSO_INTEGRATION_TESTS=1
export DSO_TEST_DOCKER_HOST="${DSO_TEST_DOCKER_HOST:-unix:///var/run/docker.sock}"
export DSO_TEST_ENVIRONMENT_ID DSO_TEST_DAEMON_ID
export DSO_TEST_RUN_ID="dso-it-$(head -c 6 /dev/urandom | od -An -tx1 | tr -d ' \n')"

ART="${DSO_IT_ARTIFACTS:-artifacts/integration-${DSO_TEST_RUN_ID}}"
mkdir -p "$ART"
echo "run id: ${DSO_TEST_RUN_ID}  artifacts: ${ART}"

cleanup() {
  go run ./test/dockerguard/cmd/cleanup || echo "WARNING: cleanup failed; remove resources labelled dso.test.run=${DSO_TEST_RUN_ID} manually" >&2
}
trap cleanup EXIT

set +e
go test -tags dockerint -json -count=1 -timeout "${DSO_IT_TIMEOUT:-30m}" ./test/docker/... >"$ART/test.json" 2>"$ART/test.err"
rc=$?
set -e

# An explicit integration run must not pass vacuously: count outcomes from the
# JSON stream and fail on any skip or when nothing passed.
pass=$(grep -c '"Action":"pass","Package":"[^"]*","Test":' "$ART/test.json" || true)
fail=$(grep -c '"Action":"fail","Package":"[^"]*","Test":' "$ART/test.json" || true)
skip=$(grep -c '"Action":"skip","Package":"[^"]*","Test":' "$ART/test.json" || true)
echo "passed=${pass} failed=${fail} skipped=${skip} go-test-exit=${rc}"
# Per-test outcomes (and tests a timeout cut off) go to the job log, which is kept whatever happens.
"$(dirname "$0")/summarize-results.sh" "$ART/test.json" || true
[[ $rc -eq 0 ]] || { echo "go test failed (see $ART/test.err and test.json)" >&2; exit 1; }
[[ "$skip" -eq 0 ]] || { echo "tests were skipped: an explicit integration run must not skip" >&2; exit 1; }
[[ "$pass" -gt 0 ]] || { echo "no test passed: refusing to report success" >&2; exit 1; }
