#!/usr/bin/env bash
# run-all.sh -- the single entry point for the M0 runtime verification, to be run
# INSIDE the provisioned disposable test VM (see
# docs/testing/DISPOSABLE-VM-EXECUTION-PLAN.md). It refuses to run anywhere else.
#
# It does NOT replace review: it runs, in order, the checks that need a real
# rootful Docker daemon and records evidence under artifacts/m0-<run id>/.
# Steps that are not implemented yet are listed as PENDING in the summary and make
# the overall result INCOMPLETE; they are never reported as passed.
set -euo pipefail

die() { echo "run-all refused: $*" >&2; exit 1; }

ENV_FILE=/etc/dso-integration/environment
[[ -f "$ENV_FILE" ]] || die "$ENV_FILE missing: this is not a provisioned test VM"
[[ "$(stat -c '%U' "$ENV_FILE")" == "root" ]] || die "$ENV_FILE is not owned by root"
for v in DOCKER_HOST DOCKER_CONTEXT DOCKER_CONFIG DOCKER_TLS_VERIFY DOCKER_CERT_PATH; do
  [[ -z "${!v:-}" ]] || die "$v is set; unset it (the endpoint is configured explicitly)"
done
[[ -z "$(git status --porcelain)" ]] || die "the source tree must be clean and committed (record the commit under test)"

SHA="$(git rev-parse HEAD)"
RUN="m0-$(date -u +%Y%m%dT%H%M%SZ)-${SHA:0:8}"
ART="${DSO_IT_ARTIFACTS:-artifacts/$RUN}"
mkdir -p "$ART"
SUMMARY="$ART/summary.txt"
: >"$SUMMARY"
STATUS=0

record() { printf '%-46s %s\n' "$1" "$2" | tee -a "$SUMMARY"; }
step() { # step NAME CMD...  -- runs CMD, logs, records PASS/FAIL
  local name="$1"; shift
  if "$@" >"$ART/$name.log" 2>&1; then record "$name" PASS; else record "$name" "FAIL (see $name.log)"; STATUS=1; fi
}
pending() { record "$1" "PENDING (not implemented; M0 stays incomplete)"; STATUS=1; }

{ echo "commit: $SHA"; go version; docker version --format 'server={{.Server.Version}} client={{.Client.Version}}'; uname -a; } >"$ART/environment.txt" 2>&1

# 1-4. clean-commit unit verification inside the VM
step go-build            go build ./...
step go-vet              go vet ./...
step go-vet-dockerint    go vet -tags dockerint ./...
step go-test-short       env -u DSO_INTEGRATION_TESTS go test -short -count=1 ./...
step installer-tests     go test -count=1 ./test/installer

# 5-6. real image build and content inspection (runs on the VM's own daemon only)
step image-build         docker build -t "dso-it-image:${SHA:0:12}" --label "dso.test.environment=$(. "$ENV_FILE"; echo "$DSO_TEST_ENVIRONMENT_ID")" .
pending image-contents-inspection   # no secrets/.git/data/*.db in layers; non-root user; plugin hashes manifest

# 7-18. guarded Docker suites (isolation guard verifies the daemon identity first)
step docker-guarded-suites ./scripts/integration/run-integration.sh
pending compose-up-stdin-deployment      # M0-13: real `docker compose up` via stdin, cancellation, failure, profiles
pending network-bind-verification        # M0-02: loopback-only unreachable from a second netns; UDP not proxied
pending continuous-traffic-rotation      # migrated legacy traffic tests (rolling/restart/signal) with request accounting
pending real-agent-kill-and-recovery     # real DSO agent terminated at controlled transitions, fresh agent recovers

record "RESULT" "$([[ $STATUS -eq 0 ]] && echo COMPLETE || echo INCOMPLETE)"
echo "evidence: $ART"
exit "$STATUS"
