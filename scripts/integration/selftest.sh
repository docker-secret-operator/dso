#!/usr/bin/env bash
# selftest.sh -- proves the fail-closed behaviour of the pure parts of run-all.sh
# (m0-lib.sh) with stub commands. It touches NO Docker: a failing `docker` stub is
# placed first in PATH and the run aborts if it is ever invoked.
#
#   ./scripts/integration/selftest.sh
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT

# ---- no-Docker guarantee -------------------------------------------------------
mkdir -p "$T/bin"
cat >"$T/bin/docker" <<EOF
#!/bin/sh
echo "docker invoked: \$*" >>"$T/docker-was-invoked"
exit 99
EOF
chmod +x "$T/bin/docker"
export PATH="$T/bin:$PATH"
unset DOCKER_HOST DOCKER_CONTEXT DOCKER_CONFIG DOCKER_TLS_VERIFY DOCKER_CERT_PATH DOCKER_API_VERSION \
  COMPOSE_FILE COMPOSE_PROJECT_NAME COMPOSE_PROFILES COMPOSE_ENV_FILES COMPOSE_PATH_SEPARATOR

PASS=0
FAIL=0
ok() { PASS=$((PASS + 1)); printf '  ok   %s\n' "$1"; }
bad() { FAIL=$((FAIL + 1)); printf '  FAIL %s\n' "$1"; }
expect() { # DESC CMD...   -- command must succeed
  local d=$1
  shift
  if "$@" >/dev/null 2>&1; then ok "$d"; else bad "$d"; fi
}
refuses() { # DESC CMD...  -- command must FAIL
  local d=$1
  shift
  if "$@" >/dev/null 2>&1; then bad "$d (should have refused)"; else ok "$d"; fi
}
eq() { if [[ "$2" == "$3" ]]; then ok "$1"; else bad "$1 (got '$2', want '$3')"; fi; }

# shellcheck source=scripts/integration/m0-lib.sh
source "$HERE/m0-lib.sh"
command -v jq >/dev/null || { echo "selftest needs jq"; exit 1; }

new_run() {
  M0_STEPS_FILE="$T/steps.$RANDOM.tsv"
  M0_LOGS="$T/logs"
  mkdir -p "$M0_LOGS"
  : >"$M0_STEPS_FILE"
}

echo "== step bookkeeping =="
new_run
M0_SENTINEL=dso-it-sentinel-abc123
two_cmds_first_fails() { false; touch "$T/second-ran"; }
good_step() { echo hello; }
m0_step good good_step >/dev/null
eq "passing step recorded passed" "$M0_LAST_STATUS" passed
m0_step bad two_cmds_first_fails >/dev/null
eq "failing step recorded failed" "$M0_LAST_STATUS" failed
expect "errexit is active inside step functions (second command did not run)" test ! -e "$T/second-ran"
m0_record leaky passed 0 t1 t2 step "value dso-it-sentinel-abc123 leaked" >/dev/null
expect "sentinel redacted from recorded notes" bash -c "! grep -q dso-it-sentinel-abc123 '$M0_STEPS_FILE'"
m0_record weird bogus 0 t1 t2 step "x" >/dev/null
eq "invalid status coerced to incomplete" "$M0_LAST_STATUS" incomplete

echo "== verdict computation =="
new_run
m0_record a passed 0 t t step "" >/dev/null
m0_record b passed 0 t t step "" >/dev/null
eq "all required passed => PASS" "$(m0_verdict "$M0_STEPS_FILE" a b)" PASS
eq "required step never ran => INCOMPLETE" "$(m0_verdict "$M0_STEPS_FILE" a b c)" INCOMPLETE
m0_record c skipped 0 t t step "skipped" >/dev/null
eq "skipped step => INCOMPLETE (never PASS)" "$(m0_verdict "$M0_STEPS_FILE" a b c)" INCOMPLETE
m0_record d failed 1 t t step "" >/dev/null
eq "any failed step => FAIL" "$(m0_verdict "$M0_STEPS_FILE" a b c d)" FAIL
new_run
m0_record a passed 0 t t step "" >/dev/null
m0_external ext "needs a human" >/dev/null
eq "external prerequisite => INCOMPLETE" "$(m0_verdict "$M0_STEPS_FILE" a ext)" INCOMPLETE
eq "automated verdict ignores external prerequisites" "$(m0_verdict_automated "$M0_STEPS_FILE" a)" PASS
new_run
eq "no steps at all => INCOMPLETE" "$(m0_verdict "$M0_STEPS_FILE")" INCOMPLETE
m0_fill_missing x y >/dev/null
eq "missing required steps are recorded incomplete" "$(awk -F'\t' '$5=="incomplete"' "$M0_STEPS_FILE" | wc -l | tr -d ' ')" 2
eq "…and keep the verdict INCOMPLETE" "$(m0_verdict "$M0_STEPS_FILE" x y)" INCOMPLETE

echo "== go test result classification (skip / zero-test detection) =="
ev() { # ACTION TEST [PKG]
  if [[ -n ${2:-} ]]; then printf '{"Action":"%s","Package":"p","Test":"%s"}\n' "$1" "$2"; else printf '{"Action":"%s","Package":"p"}\n' "$1"; fi
}
cls() { m0_classify_go_json "$@" | cut -f1; }
J="$T/j.json"
{ ev pass TestA; ev pass TestB; ev pass; } >"$J"
eq "all pass => passed" "$(cls "$J" 0 0 1)" passed
eq "required tests present => passed" "$(cls "$J" 0 0 2 TestA TestB)" passed
eq "required test absent => failed" "$(cls "$J" 0 0 1 TestZ)" failed
eq "fewer passes than minimum => failed" "$(cls "$J" 0 0 3)" failed
eq "go exit non-zero => failed" "$(cls "$J" 1 0 1)" failed
{ printf '{"Action":"output","Package":"p","Output":"testing: warning: no tests to run\\n"}\n'; ev pass; } >"$J"
eq "ZERO tests matched => failed (never passed)" "$(cls "$J" 0 0 1)" failed
{ ev skip TestA; ev skip TestB; ev pass; } >"$J"
eq "EVERY test skipped => skipped (never passed)" "$(cls "$J" 0 0 1)" skipped
{ ev pass TestA; ev skip TestB; ev pass; } >"$J"
eq "one skip in a required scenario => skipped" "$(cls "$J" 0 0 1)" skipped
eq "required test skipped => skipped" "$(cls "$J" 0 0 1 TestB)" skipped
eq "skips tolerated only where explicitly allowed (unit tests)" "$(cls "$J" 0 1 1)" passed
{ ev pass TestA; ev fail TestB; ev fail; } >"$J"
eq "a failed test => failed" "$(cls "$J" 1 0 1)" failed
eq "failure wins even when allow_skip" "$(cls "$J" 1 1 1)" failed
{ ev pass TestA; ev fail; } >"$J"
eq "package-level failure (e.g. TestMain guard refusal) => failed" "$(cls "$J" 0 0 1)" failed
: >"$J"
eq "empty output => failed" "$(cls "$J" 0 0 1)" failed
{ echo "garbage not json"; ev pass TestA; ev pass; } >"$J"
eq "non-JSON noise is ignored, not fatal" "$(cls "$J" 0 0 1)" passed
{ ev pass TestS3; ev pass TestS3/rolling; ev pass; } >"$J"
eq "subtest names are matched as required tests" "$(cls "$J" 0 0 1 TestS3/rolling)" passed
printf 'TestA\nTestCrash_X\nTestNew\n' >"$T/list"
eq "coverage check names an unexecuted test" "$(m0_uncovered "$T/list" '^TestA$' '^TestCrash')" TestNew

echo "== refusal conditions =="
refuses "DOCKER_HOST set is refused" env DOCKER_HOST=tcp://x:1 bash -c "source '$HERE/m0-lib.sh'; m0_check_no_docker_env"
refuses "DOCKER_CONTEXT set is refused" env DOCKER_CONTEXT=prod bash -c "source '$HERE/m0-lib.sh'; m0_check_no_docker_env"
refuses "COMPOSE_PROFILES set is refused" env COMPOSE_PROFILES=x bash -c "source '$HERE/m0-lib.sh'; m0_check_no_docker_env"
expect "clean environment is accepted" bash -c "source '$HERE/m0-lib.sh'; m0_check_no_docker_env"
ME="$(id -un)"
mkdir -p "$T/etc" && chmod 755 "$T/etc"
printf 'DSO_TEST_ENVIRONMENT_ID=env-0123456789abcdef\nDSO_TEST_DAEMON_ID=ABCD:EFGH:IJKL:MNOP\n' >"$T/etc/environment"
chmod 644 "$T/etc/environment"
expect "valid marker accepted" m0_check_marker "$T/etc/environment" "$ME"
refuses "missing marker refused" m0_check_marker "$T/etc/none" "$ME"
refuses "marker owned by someone else refused" m0_check_marker "$T/etc/environment" definitely-not-a-user
chmod 666 "$T/etc/environment"
refuses "world-writable marker refused" m0_check_marker "$T/etc/environment" "$ME"
chmod 644 "$T/etc/environment"
ln -sf "$T/etc/environment" "$T/etc/link"
refuses "symlinked marker refused" m0_check_marker "$T/etc/link" "$ME"
printf 'DSO_TEST_ENVIRONMENT_ID=x\nDSO_TEST_DAEMON_ID=ABCD:EFGH:IJKL\n' >"$T/etc/badid"
refuses "marker with an invalid environment id refused" m0_check_marker "$T/etc/badid" "$ME"
chmod 777 "$T/etc"
refuses "marker in a world-writable directory refused" m0_check_marker "$T/etc/environment" "$ME"
chmod 755 "$T/etc"

git init -q "$T/repo" && git -C "$T/repo" -c user.email=a@b -c user.name=t commit -q --allow-empty -m init
expect "clean tree accepted" m0_check_clean_tree "$T/repo"
mkdir -p "$T/repo/artifacts/x" && echo e >"$T/repo/artifacts/x/summary.txt"
expect "artifacts/ (evidence dir) does not count as dirty" m0_check_clean_tree "$T/repo"
echo x >"$T/repo/untracked.go"
refuses "untracked file => dirty tree refused" m0_check_clean_tree "$T/repo"
rm "$T/repo/untracked.go"
echo x >"$T/repo/f" && git -C "$T/repo" add f && git -C "$T/repo" -c user.email=a@b -c user.name=t commit -q -m f && echo y >>"$T/repo/f"
refuses "modified tracked file => dirty tree refused" m0_check_clean_tree "$T/repo"
refuses "non-repository refused" m0_check_clean_tree "$T"
SHA="$(git -C "$T/repo" rev-parse HEAD)"
expect "full SHA accepted" m0_check_sha "$SHA"
refuses "short SHA refused" m0_check_sha "${SHA:0:8}"
refuses "SHA != DSO_IT_EXPECT_SHA refused" m0_check_sha "$SHA" "$(printf '0%.0s' {1..40})"
expect "valid run id" m0_check_run_id dso-it-0123456789ab
refuses "invalid run id" m0_check_run_id "dso-it-XYZ"
expect "unix endpoint accepted" m0_check_endpoint unix:///var/run/docker.sock
refuses "tcp endpoint refused by the runner" m0_check_endpoint tcp://10.0.0.1:2375
refuses "empty endpoint refused" m0_check_endpoint ""

echo "== image content scan =="
printf '%s\n' usr/local/bin/docker-dso usr/local/lib/dso/plugins/hashes.txt etc/ssl/cert.pem etc/ssl/certs/ca.pem usr/share/zoneinfo/UTC >"$T/ok.lst"
eq "clean layer listing => no offenders" "$(m0_image_offenders <"$T/ok.lst" | wc -l | tr -d ' ')" 0
for bad_path in app/.git/config home/x/data/state.db opt/id_rsa srv/server.key etc/foo.pem app/.claude-flow/x app/local-secrets/a var/lib/x.db-wal app/.env.prod app/main.go; do
  printf '%s\n' "$bad_path" >"$T/one.lst"
  if [[ -n "$(m0_image_offenders <"$T/one.lst")" ]]; then ok "offender detected: $bad_path"; else bad "offender missed: $bad_path"; fi
done

echo "== report extraction =="
{
  printf '{"Action":"output","Package":"p","Test":"T","Output":"    traffic_test.go:229: === TRAFFIC REPORT: rolling rotation (20 clients) ===\\n"}\n'
  printf '{"Action":"output","Package":"p","Test":"T","Output":"    traffic_test.go:230: rotation phase duration: 4.2s (rolling)\\n"}\n'
  for ph in pre during post; do
    printf '{"Action":"output","Package":"p","Test":"T","Output":"    traffic_test.go:1: [%s] requests=100 ok=98 failed=2 connection_failures=1 timeouts=1\\n"}\n' "$ph"
    printf '{"Action":"output","Package":"p","Test":"T","Output":"    traffic_test.go:2: [%s] failure kinds=map[reset:1] http_status=map[200:98]\\n"}\n' "$ph"
    printf '{"Action":"output","Package":"p","Test":"T","Output":"    traffic_test.go:3: [%s] backends observed=2 map[a:60 b:40]\\n"}\n' "$ph"
    printf '{"Action":"output","Package":"p","Test":"T","Output":"    traffic_test.go:4: [%s] latency p50=1ms p95=2ms p99=3ms max=9ms\\n"}\n' "$ph"
  done
  printf '{"Action":"output","Package":"p","Test":"TestCrash_A","Output":"    crash_test.go:9: recovery completed in recovery_ms=1234 attempts=2 owner=kept\\n"}\n'
  printf '{"Action":"output","Package":"p","Test":"TestCrash_A","Output":"--- PASS: TestRecovery_X (0.1s)\\n"}\n'
} >"$T/rep.json"
TR="$(m0_parse_traffic "$T/rep.json")"
eq "traffic: one report parsed" "$(jq length <<<"$TR")" 1
eq "traffic: name" "$(jq -r '.[0].name' <<<"$TR")" "rolling rotation (20 clients)"
eq "traffic: requests (number)" "$(jq '.[0].phases.during.requests' <<<"$TR")" 100
eq "traffic: failed" "$(jq '.[0].phases.post.failed' <<<"$TR")" 2
eq "traffic: backends" "$(jq '.[0].phases.pre.backends_observed' <<<"$TR")" 2
eq "traffic: latency p99" "$(jq -r '.[0].phases.pre.latency_p99' <<<"$TR")" 3ms
eq "traffic: rotation duration" "$(jq -r '.[0].rotation_duration' <<<"$TR")" 4.2s
eq "traffic: no report lines => empty array" "$(m0_parse_traffic "$J" | jq length)" 0
RC="$(m0_parse_recovery "$T/rep.json")"
eq "recovery: only t.Logf recovery lines (go framing ignored)" "$(jq length <<<"$RC")" 1
eq "recovery: key=value metrics parsed" "$(jq '.[0].metrics.recovery_ms' <<<"$RC")" 1234

echo "== summary.json / summary.txt generation =="
new_run
m0_record go-build passed 0 2026-01-01T00:00:00Z 2026-01-01T00:00:05Z step "" >/dev/null
m0_record scenario-crash-recovery skipped 0 2026-01-01T00:00:06Z 2026-01-01T00:00:07Z step "zero tests ran" >/dev/null
m0_external external-vm-teardown "destroy the VM" >/dev/null
printf '{"schema":1,"source":{"sha":"%s","tree_clean":true},"run_id":"dso-it-0123456789ab","run_dir":"m0-x","started":"s","finished":"f","vm":{"hostname":"h","os":"Ubuntu","kernel":"6","cpus":4,"mem_mb":8000,"machine_id_sha256_12":"abc"},"docker":{"client":"27","server":"27","compose":"2.29","environment_id":"env-x","daemon_id":"d"},"go":{"version":"go1.26"}}\n' "$SHA" >"$T/meta.json"
echo "$TR" >"$T/traffic.json"
echo "$RC" >"$T/recovery.json"
echo "a finding" >"$T/findings.txt"
echo '{"status":"passed","detail":"all removed"}' >"$T/cleanup.json"
V="$(m0_verdict "$M0_STEPS_FILE" go-build scenario-crash-recovery external-vm-teardown)"
m0_write_summary_json "$T/summary.json" "$T/meta.json" "$T/traffic.json" "$T/recovery.json" "$T/findings.txt" "$T/cleanup.json" "$V" "$(m0_verdict_automated "$M0_STEPS_FILE" go-build scenario-crash-recovery)"
expect "summary.json is valid JSON" jq -e . "$T/summary.json"
eq "summary.json: verdict INCOMPLETE (skipped scenario + external)" "$(jq -r .verdict "$T/summary.json")" INCOMPLETE
eq "summary.json: exact source sha" "$(jq -r .source.sha "$T/summary.json")" "$SHA"
eq "summary.json: scenario status skipped" "$(jq -r '.scenarios[] | select(.name=="scenario-crash-recovery") | .status' "$T/summary.json")" skipped
eq "summary.json: scenario has start/finish/exit code" "$(jq -r '.scenarios[0] | [.started,.finished,(.exit_code|tostring)] | join(",")' "$T/summary.json")" "2026-01-01T00:00:00Z,2026-01-01T00:00:05Z,0"
eq "summary.json: traffic measurements embedded" "$(jq '.traffic[0].phases.during.requests' "$T/summary.json")" 100
eq "summary.json: recovery measurements embedded" "$(jq '.recovery[0].metrics.recovery_ms' "$T/summary.json")" 1234
eq "summary.json: external prerequisite listed" "$(jq -r '.external_prerequisites[0].name' "$T/summary.json")" external-vm-teardown
eq "summary.json: cleanup result" "$(jq -r .cleanup.status "$T/summary.json")" passed
m0_write_summary_txt "$T/summary.txt" "$T/summary.json"
expect "summary.txt has the verdict line" grep -q '^VERDICT: INCOMPLETE' "$T/summary.txt"
expect "summary.txt shows the skipped scenario" grep -q 'scenario-crash-recovery *skipped' "$T/summary.txt"

echo "== run-all.sh static + refusal behaviour (docker stub must never be invoked) =="
RA="$HERE/run-all.sh"
expect "bash -n run-all.sh" bash -n "$RA"
expect "bash -n m0-lib.sh" bash -n "$HERE/m0-lib.sh"
expect "no PENDING placeholders remain" bash -c "! grep -q PENDING '$RA'"
expect "no docker prune anywhere in the runner or cleanup" bash -c "! grep -hv '^[[:space:]]*#' '$RA' '$HERE/cleanup-integration.sh' '$HERE/m0-lib.sh' | grep -Eq 'prune'"
expect "runner never calls sudo/systemctl/apt/usermod (host privileged ops)" bash -c "! grep -hv '^[[:space:]]*#' '$RA' '$HERE/m0-lib.sh' | grep -Eq '(^|[;&|(]|then |do )[[:space:]]*(sudo|systemctl|apt-get|apt|usermod)( |$)'"
if command -v ss >/dev/null 2>&1; then
  out="$(DOCKER_HOST=tcp://127.0.0.1:1 bash "$RA" 2>&1)"
  rc=$?
  eq "run-all refuses with DOCKER_HOST set (exit 3)" "$rc" 3
  expect "…and says why" grep -q 'DOCKER_HOST is set' <<<"$out"
  if [[ ! -e /etc/dso-integration/environment ]]; then
    out="$(bash "$RA" 2>&1)"
    rc=$?
    eq "run-all refuses without the provisioning marker (exit 3)" "$rc" 3
    expect "…and says why" grep -q 'not a provisioned test VM' <<<"$out"
  else
    echo "  skip marker-missing refusal: /etc/dso-integration/environment exists on this host"
  fi
else
  echo "  skip run-all refusal tests: 'ss' not installed"
fi
expect "the docker stub was never invoked" test ! -e "$T/docker-was-invoked"

echo
echo "selftest: $PASS passed, $FAIL failed"
[[ $FAIL -eq 0 ]]
