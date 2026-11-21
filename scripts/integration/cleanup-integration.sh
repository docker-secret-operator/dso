#!/usr/bin/env bash
# cleanup-integration.sh -- remove the resources of ONE integration run from the
# verified test daemon. Requires DSO_TEST_RUN_ID. Removes only containers,
# networks and volumes labelled with BOTH the environment id and that run id.
# Never uses docker prune or any global removal.
set -euo pipefail
[[ -n "${DSO_TEST_RUN_ID:-}" ]] || { echo "DSO_TEST_RUN_ID is required" >&2; exit 1; }
source /etc/dso-integration/environment
export DSO_INTEGRATION_TESTS=1
export DSO_TEST_DOCKER_HOST="${DSO_TEST_DOCKER_HOST:-unix:///var/run/docker.sock}"
export DSO_TEST_ENVIRONMENT_ID DSO_TEST_DAEMON_ID
exec go run ./test/dockerguard/cmd/cleanup
