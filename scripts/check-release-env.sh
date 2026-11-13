#!/usr/bin/env bash
# [M0-05] Release-build environment guard.
#
# A release binary must never be built with the test-only `dsofault` crash hook
# (see internal/faultinject). `-tags` can be injected without touching any build
# command: through the GOFLAGS environment variable or through GOFLAGS stored in the
# `go env -w` config file (note: an EMPTY GOFLAGS variable does NOT override the file;
# only GOENV=off or removing the setting does). This script therefore asserts, and
# does not merely reset, that no Go flag source is set.
#
# Fails (exit 1) if GOFLAGS is set/non-empty in the environment, or if `go env GOFLAGS`
# (which also reflects the config file) is non-empty. Prints what it saw.
set -euo pipefail

env_flags="${GOFLAGS-}"
effective="$(go env GOFLAGS)"
echo "release env: \$GOFLAGS='${env_flags}'  go env GOFLAGS='${effective}'  GOENV='${GOENV-}'"

fail=0
if [ -n "${env_flags}" ]; then
    echo "error: GOFLAGS is set in the environment; refusing to build a release (could inject -tags=dsofault)" >&2
    fail=1
fi
if [ -n "${effective}" ]; then
    echo "error: 'go env GOFLAGS' is non-empty (environment or go env config file); refusing to build a release" >&2
    fail=1
fi
if printf '%s %s' "${env_flags}" "${effective}" | grep -Eq '(^|[[:space:]])-{1,2}tags([=[:space:]]|$)'; then
    echo "error: a -tags flag is present in the Go flag sources" >&2
    fail=1
fi
if [ "${fail}" -ne 0 ]; then
    exit 1
fi
echo "release env: OK (no inherited Go build flags)"
