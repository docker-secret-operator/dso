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
cat >"$T/bin/go" <<'EOF'
#!/bin/sh
echo "go version go0.0.0-selftest linux/amd64"
EOF
chmod +x "$T/bin/go"
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
# shellcheck source=scripts/integration/m0-finalize.sh
source "$HERE/m0-finalize.sh"
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

echo "== step verdict engine =="
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
eq "no steps at all => INCOMPLETE" "$(m0_verdict "$M0_STEPS_FILE")" INCOMPLETE
m0_fill_missing x y >/dev/null
eq "missing required steps are recorded incomplete" "$(awk -F'\t' '$5=="incomplete"' "$M0_STEPS_FILE" | wc -l | tr -d ' ')" 2
eq "...and keep the verdict INCOMPLETE" "$(m0_verdict "$M0_STEPS_FILE" x y)" INCOMPLETE

echo "== automated_integration (executable steps only) =="
new_run
m0_record a passed 0 t t step "" >/dev/null
m0_record b passed 0 t t step "" >/dev/null
m0_external ext "needs a human" >/dev/null
eq "all executable passed + external prerequisite open => automated PASS" "$(m0_automated_integration "$M0_STEPS_FILE" a b)" PASS
eq "...even when the external step is (wrongly) listed as required" "$(m0_automated_integration "$M0_STEPS_FILE" a b ext)" PASS
eq "...while the generic engine still says INCOMPLETE for the external row" "$(m0_verdict "$M0_STEPS_FILE" a b ext)" INCOMPLETE
eq "required executable step never ran => automated INCOMPLETE" "$(m0_automated_integration "$M0_STEPS_FILE" a b c)" INCOMPLETE
m0_record c skipped 0 t t step "required test skipped" >/dev/null
eq "required step skipped => automated INCOMPLETE (not PASS)" "$(m0_automated_integration "$M0_STEPS_FILE" a b c)" INCOMPLETE
m0_record z incomplete "" t t step "not run" >/dev/null
eq "required step incomplete => automated INCOMPLETE" "$(m0_automated_integration "$M0_STEPS_FILE" a b z)" INCOMPLETE
m0_record d failed 1 t t step "" >/dev/null
eq "executable step failed => automated FAIL (beats skipped)" "$(m0_automated_integration "$M0_STEPS_FILE" a b c d)" FAIL
new_run
eq "no rows => automated INCOMPLETE" "$(m0_automated_integration "$M0_STEPS_FILE" a)" INCOMPLETE
new_run
m0_record zt failed 1 t t step "zero tests ran (the -run pattern matched nothing)" >/dev/null
eq "zero-test scenario (recorded failed) => automated FAIL" "$(m0_automated_integration "$M0_STEPS_FILE" zt)" FAIL
{ ev pass; } >"$T/zt.json"
eq "zero-test classification is never 'passed'" "$(m0_classify_go_json "$T/zt.json" 0 0 1 | cut -f1)" failed

echo "== environment_attestation =="
new_run
for st in environment-identity docker-endpoint daemon-identity foreign-resources-pre foreign-resources-post; do m0_record "$st" passed 0 t t step "" >/dev/null; done
eq "all gates passed + marker ok => VERIFIED" "$(m0_environment_attestation "$M0_STEPS_FILE" ok)" VERIFIED
eq "marker not ok => UNVERIFIED" "$(m0_environment_attestation "$M0_STEPS_FILE" "")" UNVERIFIED
for st in environment-identity docker-endpoint daemon-identity foreign-resources-pre foreign-resources-post; do
  grep -v "^$st	" "$M0_STEPS_FILE" >"$T/att.tsv"
  eq "gate '$st' missing => UNVERIFIED" "$(m0_environment_attestation "$T/att.tsv" ok)" UNVERIFIED
  awk -F'\t' -v OFS='\t' -v n="$st" '$1==n {$5="failed"} {print}' "$M0_STEPS_FILE" >"$T/att.tsv"
  eq "gate '$st' failed => UNVERIFIED" "$(m0_environment_attestation "$T/att.tsv" ok)" UNVERIFIED
done
awk -F'\t' -v OFS='\t' '$1=="daemon-identity" {$5="skipped"} {print}' "$M0_STEPS_FILE" >"$T/att.tsv"
eq "gate skipped => UNVERIFIED" "$(m0_environment_attestation "$T/att.tsv" ok)" UNVERIFIED

echo "== security_review (operator-owned, default OPEN) =="
SR="$T/sr"
mkdir -p "$SR"
eq "no file configured => OPEN" "$(m0_security_review "")" OPEN
eq "missing file => OPEN" "$(m0_security_review "$SR/none")" OPEN
printf 'DSO_SECURITY_REVIEW_STATUS=CLOSED\n' >"$SR/closed"
chmod 600 "$SR/closed"
eq "explicit CLOSED in an acceptable file => CLOSED" "$(m0_security_review "$SR/closed")" CLOSED
printf 'DSO_SECURITY_REVIEW_STATUS=OPEN\n' >"$SR/open"
eq "explicit OPEN => OPEN" "$(m0_security_review "$SR/open")" OPEN
printf 'DSO_SECURITY_REVIEW_STATUS=closed\n' >"$SR/lower"
eq "lower-case closed => OPEN (exact match only)" "$(m0_security_review "$SR/lower")" OPEN
printf 'status: done\nreviewed by me\n' >"$SR/junk"
eq "no explicit status line => OPEN" "$(m0_security_review "$SR/junk")" OPEN
printf 'DSO_SECURITY_REVIEW_STATUS=CLOSED\nDSO_SECURITY_REVIEW_STATUS=OPEN\n' >"$SR/two"
eq "ambiguous (two status lines) => OPEN" "$(m0_security_review "$SR/two")" OPEN
printf '# DSO_SECURITY_REVIEW_STATUS=CLOSED\n' >"$SR/commented"
eq "commented-out status => OPEN" "$(m0_security_review "$SR/commented")" OPEN
cp "$SR/closed" "$SR/gw"; chmod 664 "$SR/gw"
eq "group-writable file => OPEN" "$(m0_security_review "$SR/gw")" OPEN
ln -sf "$SR/closed" "$SR/link"
eq "symlinked file => OPEN" "$(m0_security_review "$SR/link")" OPEN
{ printf 'DSO_SECURITY_REVIEW_STATUS=CLOSED\n'; head -c 6000 /dev/zero | tr '\0' 'x'; } >"$SR/big"
eq "oversized file => OPEN" "$(m0_security_review "$SR/big")" OPEN
eq "a directory => OPEN" "$(m0_security_review "$SR")" OPEN

echo "== cleanup verdict =="
new_run
eq "no cleanup row => FAIL" "$(m0_cleanup_status "$M0_STEPS_FILE")" FAIL
m0_record cleanup passed 0 t t step "" >/dev/null
eq "cleanup passed => PASS" "$(m0_cleanup_status "$M0_STEPS_FILE")" PASS
new_run; m0_record cleanup failed 1 t t step "remaining resources" >/dev/null
eq "cleanup failed => FAIL" "$(m0_cleanup_status "$M0_STEPS_FILE")" FAIL
new_run; m0_record cleanup skipped 0 t t step "" >/dev/null
eq "cleanup skipped => FAIL (fail closed)" "$(m0_cleanup_status "$M0_STEPS_FILE")" FAIL

echo "== release_readiness: every combination =="
mismatch=0 combos=0
for au in PASS FAIL INCOMPLETE; do for at in VERIFIED UNVERIFIED; do for sr in OPEN CLOSED; do for cl in PASS FAIL; do for td in PENDING DONE; do for n9 in not_confirmed confirmed inconclusive garbage; do
  combos=$((combos + 1))
  if [[ $au == FAIL || $cl == FAIL ]]; then want=FAIL
  elif [[ $au == PASS && $at == VERIFIED && $sr == CLOSED && $cl == PASS && $td == DONE && $n9 == not_confirmed ]]; then want=PASS
  else want=INCOMPLETE; fi
  [[ "$(m0_release_readiness "$au" "$at" "$sr" "$cl" "$td" "$n9")" == "$want" ]] || { mismatch=$((mismatch + 1)); echo "    mismatch: $au $at $sr $cl $td $n9"; }
done; done; done; done; done; done
eq "all $combos combinations of the six inputs map as specified (PASS needs n19 not_confirmed)" "$mismatch" 0
eq "n19 verdict omitted => treated as inconclusive => never PASS" "$(m0_release_readiness PASS VERIFIED CLOSED PASS DONE)" INCOMPLETE
eq "n19 confirmed does not turn readiness into FAIL (finding, not failure)" "$(m0_release_readiness PASS VERIFIED CLOSED PASS DONE confirmed)" INCOMPLETE
eq "automated PASS alone (everything else open) => readiness INCOMPLETE" "$(m0_release_readiness PASS UNVERIFIED OPEN PASS PENDING)" INCOMPLETE
eq "automated PASS + attestation VERIFIED, review OPEN => INCOMPLETE" "$(m0_release_readiness PASS VERIFIED OPEN PASS PENDING)" INCOMPLETE
eq "...review CLOSED but teardown PENDING => INCOMPLETE" "$(m0_release_readiness PASS VERIFIED CLOSED PASS PENDING)" INCOMPLETE
eq "...review CLOSED + teardown DONE (operator-confirmed) => PASS" "$(m0_release_readiness PASS VERIFIED CLOSED PASS DONE not_confirmed)" PASS
eq "automated FAIL beats everything => FAIL" "$(m0_release_readiness FAIL VERIFIED CLOSED PASS DONE not_confirmed)" FAIL
eq "automated INCOMPLETE never reaches PASS" "$(m0_release_readiness INCOMPLETE VERIFIED CLOSED PASS DONE not_confirmed)" INCOMPLETE

echo "== m0_compute_verdicts (steps -> independent verdict fields) =="
new_run
for st in environment-identity docker-endpoint daemon-identity foreign-resources-pre foreign-resources-post cleanup gobuild; do m0_record "$st" passed 0 t t step "" >/dev/null; done
m0_external external-vm-teardown "destroy the VM" >/dev/null
m0_external external-network-perimeter "firewall" >/dev/null
REQ=(environment-identity docker-endpoint daemon-identity foreign-resources-pre foreign-resources-post cleanup gobuild)
cv() { m0_compute_verdicts "$M0_STEPS_FILE" "$1" "$2" "$3" -- "${REQ[@]}"; }
V="$(cv ok "" PENDING)"
eq "external prereqs open + all automated passed => automated PASS" "$(jq -r .automated_integration <<<"$V")" PASS
eq "...attestation VERIFIED" "$(jq -r .environment_attestation <<<"$V")" VERIFIED
eq "...security_review OPEN by default" "$(jq -r .security_review <<<"$V")" OPEN
eq "...cleanup PASS" "$(jq -r .cleanup <<<"$V")" PASS
eq "...vm_teardown PENDING" "$(jq -r .vm_teardown <<<"$V")" PENDING
eq "...but release_readiness INCOMPLETE" "$(jq -r .release_readiness <<<"$V")" INCOMPLETE
eq "security_review_source is null without a file" "$(jq -r .security_review_source <<<"$V")" null
V="$(cv ok "$SR/closed" PENDING)"
eq "operator CLOSED file: security_review CLOSED" "$(jq -r .security_review <<<"$V")" CLOSED
eq "...but release_readiness is still INCOMPLETE (teardown pending)" "$(jq -r .release_readiness <<<"$V")" INCOMPLETE
eq "...file sha256 recorded" "$(jq -r .security_review_source.sha256 <<<"$V")" "$(sha256sum "$SR/closed" | cut -d' ' -f1)"
V="$(cv ok "$SR/closed" DONE)"
eq "no N19 verdict (M0_N19_VERDICT unset): all green + teardown DONE is still INCOMPLETE" "$(jq -r '[.n19_verdict,.release_readiness]|join(",")' <<<"$V")" "inconclusive,INCOMPLETE"
V="$(M0_N19_VERDICT=confirmed cv ok "$SR/closed" DONE)"
eq "n19 confirmed: field recorded, readiness INCOMPLETE" "$(jq -r '[.n19_verdict,.release_readiness]|join(",")' <<<"$V")" "confirmed,INCOMPLETE"
V="$(M0_N19_VERDICT=bogus cv ok "$SR/closed" DONE)"
eq "n19 unknown value is coerced to inconclusive" "$(jq -r .n19_verdict <<<"$V")" inconclusive
V="$(M0_N19_VERDICT=not_confirmed cv ok "$SR/closed" DONE)"
eq "pure function: CLOSED + operator-confirmed teardown + all green + n19 not_confirmed => PASS" "$(jq -r .release_readiness <<<"$V")" PASS
V="$(M0_N19_VERDICT=not_confirmed cv ok "$SR/closed" garbage)"
eq "any teardown value other than DONE is PENDING" "$(jq -r .vm_teardown <<<"$V")" PENDING
V="$(cv "" "$SR/closed" DONE)"
eq "marker not ok: attestation UNVERIFIED, readiness INCOMPLETE, automated unaffected" "$(jq -r '[.environment_attestation,.release_readiness,.automated_integration]|join(",")' <<<"$V")" "UNVERIFIED,INCOMPLETE,PASS"
awk -F'\t' -v OFS='\t' '$1=="gobuild" {$5="skipped"} {print}' "$M0_STEPS_FILE" >"$T/sk.tsv"
V="$(M0_N19_VERDICT=not_confirmed m0_compute_verdicts "$T/sk.tsv" ok "$SR/closed" DONE -- "${REQ[@]}")"
eq "required test/step skipped => automated INCOMPLETE and readiness INCOMPLETE" "$(jq -r '[.automated_integration,.release_readiness]|join(",")' <<<"$V")" "INCOMPLETE,INCOMPLETE"
awk -F'\t' -v OFS='\t' '$1=="gobuild" {$5="failed"} {print}' "$M0_STEPS_FILE" >"$T/fl.tsv"
V="$(M0_N19_VERDICT=not_confirmed m0_compute_verdicts "$T/fl.tsv" ok "$SR/closed" DONE -- "${REQ[@]}")"
eq "executable step failed => automated FAIL and readiness FAIL" "$(jq -r '[.automated_integration,.release_readiness]|join(",")' <<<"$V")" "FAIL,FAIL"
awk -F'\t' -v OFS='\t' '$1=="cleanup" {$5="failed"} {print}' "$M0_STEPS_FILE" >"$T/cf.tsv"
V="$(M0_N19_VERDICT=not_confirmed m0_compute_verdicts "$T/cf.tsv" ok "$SR/closed" DONE -- "${REQ[@]}")"
eq "cleanup failed => cleanup FAIL, automated FAIL, readiness FAIL" "$(jq -r '[.cleanup,.automated_integration,.release_readiness]|join(",")' <<<"$V")" "FAIL,FAIL,FAIL"

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
m0_record environment-identity passed 0 t t step "" >/dev/null
m0_record docker-endpoint passed 0 t t step "" >/dev/null
m0_record daemon-identity passed 0 t t step "" >/dev/null
m0_record foreign-resources-pre passed 0 t t step "" >/dev/null
m0_record foreign-resources-post passed 0 t t step "" >/dev/null
m0_record cleanup passed 0 t t step "" >/dev/null
m0_record go-build passed 0 2026-01-01T00:00:00Z 2026-01-01T00:00:05Z step "" >/dev/null
m0_record scenario-crash-recovery skipped 0 2026-01-01T00:00:06Z 2026-01-01T00:00:07Z step "zero tests ran" >/dev/null
m0_external external-vm-teardown "destroy the VM" >/dev/null
printf '{"schema":1,"source":{"sha":"%s","mode":"git","tree_sha":"%s","tree_clean":true},"run_id":"dso-it-0123456789ab","run_dir":"m0-x","started":"s","finished":"f","vm":{"hostname":"h","os":"Ubuntu","kernel":"6","cpus":4,"mem_mb":8000,"machine_id_sha256_12":"abc"},"docker":{"client":"27","server":"27","compose":"2.29","environment_id":"env-x","daemon_id":"d"},"go":{"version":"go1.26"}}\n' "$SHA" "$(git -C "$T/repo" rev-parse 'HEAD^{tree}')" >"$T/meta.json"
echo "$TR" >"$T/traffic.json"
echo "$RC" >"$T/recovery.json"
echo "a finding" >"$T/findings.txt"
echo '{"status":"passed","detail":"all removed"}' >"$T/cleanup.json"
m0_compute_verdicts "$M0_STEPS_FILE" ok "" PENDING -- environment-identity docker-endpoint daemon-identity foreign-resources-pre foreign-resources-post cleanup go-build scenario-crash-recovery >"$T/verdicts.json"
m0_write_summary_json "$T/summary.json" "$T/meta.json" "$T/traffic.json" "$T/recovery.json" "$T/findings.txt" "$T/cleanup.json" "$T/verdicts.json"
expect "summary.json is valid JSON" jq -e . "$T/summary.json"
eq "summary.json: automated_integration INCOMPLETE (skipped scenario)" "$(jq -r .automated_integration "$T/summary.json")" INCOMPLETE
eq "summary.json: release_readiness INCOMPLETE" "$(jq -r .release_readiness "$T/summary.json")" INCOMPLETE
eq "summary.json: verdict is an alias of release_readiness" "$(jq -r '.verdict == .release_readiness' "$T/summary.json")" true
eq "summary.json: automated_verdict alias" "$(jq -r '.automated_verdict == .automated_integration' "$T/summary.json")" true
eq "summary.json: independent fields present" "$(jq -r '[.automated_integration,.environment_attestation,.security_review,.cleanup,.vm_teardown,.release_readiness]|join(",")' "$T/summary.json")" "INCOMPLETE,VERIFIED,OPEN,PASS,PENDING,INCOMPLETE"
eq "summary.json: exact source sha" "$(jq -r .source.sha "$T/summary.json")" "$SHA"
eq "summary.json: scenario status skipped" "$(jq -r '.scenarios[] | select(.name=="scenario-crash-recovery") | .status' "$T/summary.json")" skipped
eq "summary.json: scenario has start/finish/exit code" "$(jq -r '.scenarios[] | select(.name=="go-build") | [.started,.finished,(.exit_code|tostring)] | join(",")' "$T/summary.json")" "2026-01-01T00:00:00Z,2026-01-01T00:00:05Z,0"
eq "summary.json: traffic measurements embedded" "$(jq '.traffic[0].phases.during.requests' "$T/summary.json")" 100
eq "summary.json: recovery measurements embedded" "$(jq '.recovery[0].metrics.recovery_ms' "$T/summary.json")" 1234
eq "summary.json: external prerequisite listed" "$(jq -r '.external_prerequisites[0].name' "$T/summary.json")" external-vm-teardown
eq "summary.json: cleanup_detail" "$(jq -r .cleanup_detail.status "$T/summary.json")" passed
m0_write_summary_txt "$T/summary.txt" "$T/summary.json"
expect "summary.txt has automated_integration line" grep -q '^automated_integration: *INCOMPLETE' "$T/summary.txt"
expect "summary.txt has release_readiness line" grep -q '^release_readiness: *INCOMPLETE' "$T/summary.txt"
expect "summary.txt has the verdict alias line" grep -q '^VERDICT: INCOMPLETE' "$T/summary.txt"
expect "summary.txt shows the skipped scenario" grep -q 'scenario-crash-recovery *skipped' "$T/summary.txt"

echo "== N19 diagnostic evidence: extraction, validation, verdict (pure; no Docker) =="
N19D="$T/n19"
mkdir -p "$N19D"
n19ev() { # SCENARIO OBSERVATION LOCATION INTEGRITY  -> one evidence JSON line
  jq -cn --arg s "$1" --arg o "$2" --arg l "$3" --arg i "$4" \
    '{scenario:$s,observation:$o,operation:"selftest",location:$l,docker_server:"29.5.1",api_version:"1.54",storage_driver:"overlayfs",snapshotter:"",container:"abc123def456",run_id:"dso-it-0123456789ab",declared_tmpfs:{"/run/secrets/dso":""},declared_mounts:[],runtime_mountinfo:[],lifecycle_state:"running",file_visibility:"n/a",canary_integrity:$i,layer_inspection:"not_checked",cleanup:"",detail:"selftest"}'
}
base_all() { local sc; for sc in A B C D E F G H I J K L M R; do n19ev "$sc" filler none n/a; done; }
mk() { # NAME  then lines on stdin (appended to a full filler set)
  { base_all; cat; } >"$N19D/$1.jsonl"
}
v() { m0_n19_verdict "$N19D/$1.jsonl"; }

{ n19ev B layer_read_unstarted layer match; n19ev D running_tmpfs_after_start running_tmpfs absent; } | mk conf1
eq "verdict: canary on the layer while absent from the running tmpfs => confirmed" "$(v conf1)" confirmed
{ n19ev I hypothesis_path_after_stop layer match; n19ev D running_tmpfs_after_start running_tmpfs match; } | mk conf2
eq "verdict: present on the layer at all (even also in the tmpfs) => confirmed" "$(v conf2)" confirmed
{ n19ev C file_read_unstarted layer match; } | mk conf3
eq "verdict: layer match from scenario C alone => confirmed" "$(v conf3)" confirmed

{ n19ev B layer_read_unstarted layer absent; n19ev C file_read_unstarted layer absent; n19ev I hypothesis_path_after_stop layer absent
  n19ev D running_tmpfs_after_start running_tmpfs match; n19ev E application_view app_view match; n19ev K replacement_file_state shadow_tmpfs match; } | mk notc
eq "verdict: present in the running tmpfs AND absent from the layer => not_confirmed" "$(v notc)" not_confirmed

{ n19ev B layer_read_unstarted layer unknown; n19ev D running_tmpfs_after_start running_tmpfs match; } | mk inc1
eq "verdict: layer outcome unknown => inconclusive" "$(v inc1)" inconclusive
{ n19ev B layer_read_unstarted layer absent; n19ev D running_tmpfs_after_start running_tmpfs absent; } | mk inc2
eq "verdict: absent from both the layer and the tmpfs (file vanished) => inconclusive" "$(v inc2)" inconclusive
{ n19ev B layer_read_unstarted layer absent; n19ev D running_tmpfs_after_start running_tmpfs match; n19ev K replacement_file_state shadow_tmpfs absent; } | mk inc3
eq "verdict: contradictory running evidence (D match, K absent) => inconclusive" "$(v inc3)" inconclusive
{ n19ev D running_tmpfs_after_start running_tmpfs match; n19ev I control_exec_inject_then_stop control_layer absent; } | mk inc4
eq "verdict: a CONTROL layer line never counts as layer evidence => inconclusive" "$(v inc4)" inconclusive
{ n19ev R cp_out_unstarted layer match; n19ev F running_tmpfs_after_archive_inject running_tmpfs absent; } | mk inc5
eq "verdict: scenarios R/F do not feed the verdict => inconclusive" "$(v inc5)" inconclusive
{ n19ev B layer_read_unstarted layer absent; n19ev D running_tmpfs_after_start running_tmpfs match; n19ev K x shadow_tmpfs mismatch; } | mk inc6
eq "verdict: a mismatching digest in a running view blocks not_confirmed" "$(v inc6)" inconclusive
base_all >"$N19D/onlyfiller.jsonl"
eq "verdict: only filler lines (no decisive observation) => inconclusive" "$(v onlyfiller)" inconclusive

echo "== N19 evidence: missing / malformed =="
eq "verdict: missing file => inconclusive" "$(m0_n19_verdict "$N19D/does-not-exist.jsonl")" inconclusive
: >"$N19D/empty.jsonl"
eq "verdict: empty file => inconclusive" "$(m0_n19_verdict "$N19D/empty.jsonl")" inconclusive
eq "verdict: no argument => inconclusive" "$(m0_n19_verdict)" inconclusive
{ base_all; echo 'this is not json'; n19ev B layer_read_unstarted layer match; } >"$N19D/mal1.jsonl"
eq "verdict: one malformed line poisons the evidence (even with a confirming line) => inconclusive" "$(v mal1)" inconclusive
{ base_all; n19ev B layer_read_unstarted layer match | jq -c 'del(.canary_integrity)'; } >"$N19D/mal2.jsonl"
eq "verdict: a line without canary_integrity => inconclusive" "$(v mal2)" inconclusive
{ base_all; n19ev B layer_read_unstarted layer match | jq -c '.canary_integrity="yes"'; } >"$N19D/mal3.jsonl"
eq "verdict: an out-of-vocabulary canary_integrity => inconclusive" "$(v mal3)" inconclusive
{ base_all; echo '[1,2,3]'; } >"$N19D/mal4.jsonl"
eq "verdict: a non-object JSON line => inconclusive" "$(v mal4)" inconclusive
refuses "validate: empty evidence fails" m0_n19_validate "$N19D/empty.jsonl"
refuses "validate: missing file fails" m0_n19_validate "$N19D/does-not-exist.jsonl"
refuses "validate: a malformed line fails" m0_n19_validate "$N19D/mal1.jsonl"
expect "validate: a complete set (A..M,R) passes" m0_n19_validate "$N19D/notc.jsonl"
grep -v '"scenario":"M"' "$N19D/notc.jsonl" >"$N19D/nom.jsonl"
refuses "validate: a missing scenario (M) fails" m0_n19_validate "$N19D/nom.jsonl"
expect "validate: ...and names the missing scenario" bash -c "source '$HERE/m0-lib.sh'; m0_n19_validate '$N19D/nom.jsonl' | grep -q 'scenario(s): M'"

echo "== N19 evidence: extraction from go test -json =="
{
  printf '{"Action":"run","Test":"TestN19Diag_B_ArchiveInjectBeforeStart"}\n'
  jq -cn --arg o "    n19_diag_basic_test.go:61: N19-EVIDENCE $(n19ev B layer_read_unstarted layer match)"$'\n' '{Action:"output",Test:"TestN19Diag_B_ArchiveInjectBeforeStart",Output:$o}'
  jq -cn --arg o "    n19_diag_basic_test.go:62: N19-EVIDENCE $(n19ev B layer_read_unstarted layer match)"$'\n' '{Action:"output",Test:"TestN19Diag_B_ArchiveInjectBeforeStart",Output:$o}'
  jq -cn --arg o "    n19_diag_basic_test.go:80: N19-EVIDENCE $(n19ev D running_tmpfs_after_start running_tmpfs absent)"$'\n' '{Action:"output",Test:"TestN19Diag_D_StartAndInspectTmpfs",Output:$o}'
  jq -cn '{Action:"output",Test:"TestN19Diag_D_StartAndInspectTmpfs",Output:"=== some other output\n"}'
  printf 'not a json event line\n'
  printf '{"Action":"pass","Test":"TestN19Diag_B_ArchiveInjectBeforeStart"}\n'
} >"$N19D/go.json"
m0_n19_extract "$N19D/go.json" >"$N19D/extracted.jsonl"
eq "extract: two distinct evidence lines (identical duplicate dropped, noise ignored)" "$(wc -l <"$N19D/extracted.jsonl" | tr -d ' ')" 2
expect "extract: every extracted line is valid evidence JSON" m0_n19_lines_valid "$N19D/extracted.jsonl"
eq "extract: go-test log prefix removed (first char is a brace)" "$(head -c1 "$N19D/extracted.jsonl")" "{"
eq "verdict from extracted evidence (B layer match + D absent) => confirmed" "$(m0_n19_verdict "$N19D/extracted.jsonl")" confirmed
echo '{"Action":"pass"}' >"$N19D/go-none.json"
eq "extract: no evidence lines => empty output" "$(m0_n19_extract "$N19D/go-none.json" | wc -c | tr -d ' ')" 0

echo "== N19 step logic (n19_collect from run-all.sh, no Docker) =="
eval "$(sed -n '/^n19_collect() {/,/^}/p' "$HERE/run-all.sh")"
nc_run() { # NAME GO_JSON_BUILDER_FILE
  local d="$N19D/col-$1"
  mkdir -p "$d/art/evidence" "$d/state"
  ( M0_ART="$d/art" M0_STATE="$d/state"; N19_EVIDENCE="$d/art/evidence/n19-evidence.jsonl"; : >"$M0_STATE/findings.txt"
    n19_collect "$2" >"$d/out.txt" 2>&1; echo $? >"$d/rc" )
  NC_D="$d"
}
go_json_from() { # JSONL -> go test -json file on stdout
  local n=1 l
  while IFS= read -r l; do
    n=$((n + 1))
    jq -cn --arg o "    n19_diag_x_test.go:$n: N19-EVIDENCE $l"$'\n' '{Action:"output",Test:"TestN19Diag_X",Output:$o}'
  done <"$1"
}
go_json_from "$N19D/notc.jsonl" >"$N19D/go-notc.json"
nc_run notc "$N19D/go-notc.json"
eq "collect (not_confirmed evidence): rc 0, verdict file, finding string" "$(cat "$NC_D/rc"),$(cat "$NC_D/state/n19_verdict"),$(head -n1 "$NC_D/state/findings.txt")" "0,not_confirmed,n19_verdict: not_confirmed"
expect "collect: evidence captured to evidence/n19-evidence.jsonl" test -s "$NC_D/art/evidence/n19-evidence.jsonl"
eq "collect: n19.json records the verdict, validity and line count" "$(jq -r '[.verdict,(.evidence_valid|tostring),(.lines|tostring)]|join(",")' "$NC_D/state/n19.json")" "not_confirmed,true,$(grep -c . "$N19D/notc.jsonl")"
go_json_from "$N19D/conf1.jsonl" >"$N19D/go-conf1.json"
nc_run conf "$N19D/go-conf1.json"
eq "collect (confirmed evidence): rc 0 (a demonstrated N19 does NOT fail the step), verdict confirmed" "$(cat "$NC_D/rc"),$(cat "$NC_D/state/n19_verdict")" "0,confirmed"
expect "collect (confirmed): a human-readable CONFIRMED finding is added" grep -q '^N19 CONFIRMED' "$NC_D/state/findings.txt"
go_json_from "$N19D/inc1.jsonl" >"$N19D/go-inc1.json"
nc_run inc "$N19D/go-inc1.json"
eq "collect (inconclusive evidence): rc 0, verdict inconclusive" "$(cat "$NC_D/rc"),$(cat "$NC_D/state/n19_verdict")" "0,inconclusive"
echo '{"Action":"pass"}' >"$N19D/go-empty.json"
nc_run none "$N19D/go-empty.json"
eq "collect (no evidence lines): rc 1 (step fails), verdict inconclusive" "$(cat "$NC_D/rc"),$(cat "$NC_D/state/n19_verdict")" "1,inconclusive"
{ go_json_from "$N19D/notc.jsonl"; jq -cn '{Action:"output",Test:"TestN19Diag_X",Output:"    n19_diag_x_test.go:9: N19-EVIDENCE {truncated\n"}'; } >"$N19D/go-mal.json"
nc_run mal "$N19D/go-mal.json"
eq "collect (a malformed evidence line): rc 1, verdict inconclusive even though the rest says not_confirmed" "$(cat "$NC_D/rc"),$(cat "$NC_D/state/n19_verdict")" "1,inconclusive"
grep -v '"scenario":"K"' "$N19D/notc.jsonl" >"$N19D/notc-nok.jsonl"
go_json_from "$N19D/notc-nok.jsonl" >"$N19D/go-nok.json"
nc_run nok "$N19D/go-nok.json"
eq "collect (scenario K evidence missing): rc 1 (step fails), verdict inconclusive" "$(cat "$NC_D/rc"),$(cat "$NC_D/state/n19_verdict")" "1,inconclusive"
expect "collect: the missing scenario is named in the finding" grep -q 'scenario(s): K' "$NC_D/state/findings.txt"

echo "== N19 summary.json integration =="
new_run
for st in environment-identity docker-endpoint daemon-identity foreign-resources-pre foreign-resources-post cleanup gobuild; do m0_record "$st" passed 0 t t step "" >/dev/null; done
NS="$T/n19sum"
mkdir -p "$NS/state"
M0_STATE="$NS/state"
M0_N19_VERDICT=confirmed m0_compute_verdicts "$M0_STEPS_FILE" ok "" PENDING -- "${REQ[@]}" >"$NS/verdicts.json"
printf '{"schema":1,"source":{"sha":"a","mode":"git","tree_sha":"b","tree_clean":true},"run_id":"r","run_dir":"d","started":"s","finished":"f"}\n' >"$NS/meta.json"
echo '[]' >"$NS/traffic.json"; echo '[]' >"$NS/recovery.json"; : >"$NS/findings.txt"; echo '{"status":"passed","detail":"x"}' >"$NS/cleanup.json"
m0_n19_summary "$N19D/conf1.jsonl" confirmed true "ok" >"$NS/n19.json"
m0_write_summary_json "$NS/summary.json" "$NS/meta.json" "$NS/traffic.json" "$NS/recovery.json" "$NS/findings.txt" "$NS/cleanup.json" "$NS/verdicts.json" "$NS/n19.json"
eq "summary.json: n19_verdict field (top level)" "$(jq -r .n19_verdict "$NS/summary.json")" confirmed
eq "summary.json: n19_evidence summarises the evidence (verdict, lines, scenarios)" "$(jq -r '[.n19_evidence.verdict,(.n19_evidence.lines|tostring),(.n19_evidence.scenarios|join(""))]|join(",")' "$NS/summary.json")" "confirmed,16,ABCDEFGHIJKLMR"
eq "summary.json: n19_evidence carries the engine identity" "$(jq -r .n19_evidence.docker_server "$NS/summary.json")" 29.5.1
m0_write_summary_json "$NS/summary2.json" "$NS/meta.json" "$NS/traffic.json" "$NS/recovery.json" "$NS/findings.txt" "$NS/cleanup.json" "$NS/verdicts.json"
eq "summary.json without n19.json: n19_evidence is an invalid/not-run record" "$(jq -r '[.n19_evidence.verdict,(.n19_evidence.evidence_valid|tostring)]|join(",")' "$NS/summary2.json")" "inconclusive,false"
m0_write_summary_txt "$NS/summary.txt" "$NS/summary.json"
expect "summary.txt shows the n19_verdict line" grep -q '^n19_verdict: *confirmed' "$NS/summary.txt"

echo "== finalisation dry-run (real finalize(), stub docker/go, fake artifact dir, no daemon) =="
FIN_SENT=dso-it-sentinel-0badc0ffee
FIN_BASE_REQ=(fake-pass-a fake-pass-b fake-gotest environment-identity docker-endpoint daemon-identity foreign-resources-pre
  evidence-collection cleanup foreign-resources-post source-tree-unchanged evidence-leak-scan)
OMIT=""
base_steps() {
  local st
  for st in fake-pass-a fake-pass-b fake-gotest environment-identity docker-endpoint daemon-identity foreign-resources-pre; do
    [[ " $OMIT " == *" $st "* ]] || m0_record "$st" passed 0 2026-01-01T00:00:00Z 2026-01-01T00:00:01Z step "" >/dev/null
  done
}
stub_foreign_post_ok() { s_foreign_post() { echo "stub: foreign resources identical"; }; }
sc_all_pass() { base_steps; stub_foreign_post_ok; }
sc_step_failed() { OMIT=fake-gotest; base_steps; stub_foreign_post_ok; m0_record fake-gotest failed 1 t t step "failed tests=1" >/dev/null; }
sc_step_skipped() { OMIT=fake-gotest; base_steps; stub_foreign_post_ok; m0_record fake-gotest skipped 0 t t step "required test skipped: TestX" >/dev/null; }
sc_step_missing() { OMIT=fake-gotest; base_steps; stub_foreign_post_ok; }
sc_daemon_unverified() { OMIT=daemon-identity; base_steps; stub_foreign_post_ok; }
sc_real_foreign_post() { base_steps; }
sc_cleanup_fails() { base_steps; stub_foreign_post_ok; s_cleanup() { echo "remaining resources of this run: container:abc"; return 1; }; }
sc_leak() { base_steps; stub_foreign_post_ok; printf 'log line with %s inside\n' "$FIN_SENT" >"$M0_LOGS/fake-pass-a.log"; printf 'x %s y\n' "$FIN_SENT" >"$M0_ART/evidence/note.txt"; }
sc_tree_dirty() { base_steps; stub_foreign_post_ok; echo new >"$ROOT/untracked-by-the-run.go"; }
sc_review_closed() { base_steps; stub_foreign_post_ok; export DSO_IT_SECURITY_REVIEW_FILE="$SR/closed"; }
sc_review_bad() { base_steps; stub_foreign_post_ok; export DSO_IT_SECURITY_REVIEW_FILE="$SR/two"; }

run_fin() { # NAME SETUP_FN  -> FIN_RC, FIN_ART, FIN_OUT
  local n=$1 setup=$2 d="$T/fin/$1"
  FIN_ART="$d/art"
  FIN_OUT="$d/out.txt"
  mkdir -p "$d/art/logs" "$d/art/state" "$d/art/evidence" "$d/work"
  git init -q "$d/repo" && git -C "$d/repo" -c user.email=a@b -c user.name=t commit -q --allow-empty -m init
  (
    ROOT="$d/repo"
    M0_ART="$d/art" M0_LOGS="$d/art/logs" M0_STATE="$d/art/state" M0_STEPS_FILE="$d/art/state/steps.tsv" M0_WORK="$d/work"
    : >"$M0_STEPS_FILE" && : >"$M0_STATE/findings.txt" && echo '[]' >"$M0_STATE/traffic.json" && echo '[]' >"$M0_STATE/recovery.json"
    M0_SOURCE_MODE=git M0_SHA="$(git -C "$ROOT" rev-parse HEAD)" M0_TREE="$(git -C "$ROOT" rev-parse 'HEAD^{tree}')" M0_PROV_SHA256=""
    M0_ENV_ID=env-0123456789abcdef M0_DAEMON_ID=ABCD:EFGH:IJKL:MNOP M0_ENDPOINT=unix:///nonexistent-selftest.sock
    M0_SENTINEL="$FIN_SENT" M0_STARTED=2026-01-01T00:00:00Z M0_RUN_DIR=m0-selftest M0_MARKER_OK=ok
    RUN=dso-it-0123456789ab IMAGE=dso-it-image:selftest FINALIZED=0
    REQUIRED_STEPS=("${FIN_BASE_REQ[@]}")
    unset DSO_IT_SECURITY_REVIEW_FILE
    "$setup"
    true
    finalize
  ) >"$FIN_OUT" 2>&1
  FIN_RC=$?
  FIN_JS="$FIN_ART/summary.json"
}
jf() { jq -r "$1" "$FIN_JS"; }
fields() { jf '[.automated_integration,.environment_attestation,.security_review,.cleanup,.vm_teardown,.release_readiness]|join(",")'; }

run_fin allpass sc_all_pass
eq "all automated pass: exit 0 (automated PASS)" "$FIN_RC" 0
eq "all pass: verdict fields (external prereqs open, review OPEN, teardown PENDING)" "$(fields)" "PASS,VERIFIED,OPEN,PASS,PENDING,INCOMPLETE"
eq "all pass: verdict alias = release_readiness = INCOMPLETE" "$(jf '.verdict')" INCOMPLETE
eq "all pass: three external prerequisites listed" "$(jf '[.external_prerequisites[].name]|sort|join(",")')" "external-network-perimeter,external-provisioning-provenance,external-vm-teardown"
eq "all pass: external prerequisites recorded as incomplete steps of kind external_prerequisite" "$(jf '[.scenarios[]|select(.kind=="external_prerequisite")|.status]|unique|join(",")')" incomplete
eq "all pass: finalisation steps recorded" "$(jf '[.scenarios[]|select(.name=="evidence-collection" or .name=="cleanup" or .name=="foreign-resources-post" or .name=="source-tree-unchanged" or .name=="evidence-leak-scan")|.status]|unique|join(",")')" passed
eq "all pass: source info (git mode, tree clean)" "$(jf '[.source.mode,.source.tree_clean]|join(",")')" "git,true"
expect "all pass: summary.txt written with all fields" grep -q '^environment_attestation: *VERIFIED' "$FIN_ART/summary.txt"
expect "all pass: summary.txt AUTOMATED_INTEGRATION line" grep -q '^AUTOMATED_INTEGRATION: PASS' "$FIN_ART/summary.txt"
expect "all pass: summary.txt says it is not a release approval" grep -q 'NOT a release approval' "$FIN_ART/summary.txt"
expect "all pass: SHA256SUMS lists summary.json and summary.txt" bash -c "grep -q ' ./summary.json\$' '$FIN_ART/SHA256SUMS' && grep -q ' ./summary.txt\$' '$FIN_ART/SHA256SUMS'"
expect "all pass: SHA256SUMS lists logs/ and state/" bash -c "grep -q ' ./state/steps.tsv\$' '$FIN_ART/SHA256SUMS' && grep -q ' ./evidence/host.txt\$' '$FIN_ART/SHA256SUMS'"
expect "all pass: SHA256SUMS does not list itself" bash -c "! grep -q 'SHA256SUMS' '$FIN_ART/SHA256SUMS'"
expect "all pass: sha256sum -c verifies every file" bash -c "cd '$FIN_ART' && sha256sum -c --quiet SHA256SUMS"
expect "all pass: work dir removed by finalize" test ! -e "$T/fin/allpass/work"
eq "all pass: cleanup_detail present" "$(jf '.cleanup_detail.status')" passed
echo tamper >>"$FIN_ART/summary.json"
refuses "all pass: tampering with summary.json breaks SHA256SUMS" bash -c "cd '$FIN_ART' && sha256sum -c --quiet SHA256SUMS"

run_fin failed sc_step_failed
eq "executable step failed: exit 1" "$FIN_RC" 1
eq "failed: fields" "$(fields)" "FAIL,VERIFIED,OPEN,PASS,PENDING,FAIL"
run_fin skipped sc_step_skipped
eq "required step skipped: exit 2 (INCOMPLETE, not PASS)" "$FIN_RC" 2
eq "skipped: fields" "$(fields)" "INCOMPLETE,VERIFIED,OPEN,PASS,PENDING,INCOMPLETE"
run_fin missing sc_step_missing
eq "required step never ran: exit 2" "$FIN_RC" 2
eq "missing: the gap is recorded as incomplete 'not run'" "$(jf '.scenarios[]|select(.name=="fake-gotest")|.status')" incomplete
run_fin daemon sc_daemon_unverified
eq "daemon-identity never recorded: attestation UNVERIFIED, automated INCOMPLETE" "$(fields)" "INCOMPLETE,UNVERIFIED,OPEN,PASS,PENDING,INCOMPLETE"
run_fin realpost sc_real_foreign_post
eq "real foreign-resources-post without a verified daemon fails (never calls docker): exit 1" "$FIN_RC" 1
eq "real foreign-post: attestation UNVERIFIED, automated FAIL" "$(fields)" "FAIL,UNVERIFIED,OPEN,PASS,PENDING,FAIL"
expect "real foreign-post: reason recorded in its log" grep -q 'daemon never verified' "$FIN_ART/logs/foreign-resources-post.log"
eq "real cleanup path with no verified daemon passes without docker" "$(jf '.scenarios[]|select(.name=="cleanup")|.status')" passed
run_fin cleanfail sc_cleanup_fails
eq "cleanup failure: exit 1" "$FIN_RC" 1
eq "cleanup failure: cleanup FAIL, automated FAIL, readiness FAIL" "$(fields)" "FAIL,VERIFIED,OPEN,FAIL,PENDING,FAIL"
eq "cleanup failure: detail reports a failed cleanup step" "$(jf '.cleanup_detail.status')" failed
expect "cleanup failure: remaining resources shown in the step log" grep -q 'remaining resources of this run' "$FIN_ART/logs/cleanup.log"
expect "cleanup failure: summary.txt reports cleanup FAIL" grep -q '^cleanup: *FAIL' "$FIN_ART/summary.txt"
run_fin leak sc_leak
eq "sentinel planted in a log: leak scan fails the run (exit 1)" "$FIN_RC" 1
eq "leak: evidence-leak-scan step failed, automated FAIL" "$(jf '[.scenarios[]|select(.name=="evidence-leak-scan")|.status]|join(",")'),$(jf .automated_integration)" "failed,FAIL"
expect "leak: sentinel redacted in place from the log" bash -c "! grep -rqF '$FIN_SENT' '$FIN_ART'"
expect "leak: redaction marker present where it was" grep -q '<redacted>' "$FIN_ART/logs/fake-pass-a.log"
expect "leak: SHA256SUMS matches the redacted files" bash -c "cd '$FIN_ART' && sha256sum -c --quiet SHA256SUMS"
expect "leak: the run output never prints the sentinel" bash -c "! grep -qF '$FIN_SENT' '$FIN_OUT'"
run_fin dirty sc_tree_dirty
eq "source tree modified during the run: exit 1" "$FIN_RC" 1
eq "dirty: tree_clean false, source-tree-unchanged failed" "$(jf '[.source.tree_clean,(.scenarios[]|select(.name=="source-tree-unchanged")|.status)]|map(tostring)|join(",")')" "false,failed"
run_fin revclosed sc_review_closed
eq "operator CLOSED review + automated PASS: exit 0, readiness STILL INCOMPLETE (teardown pending)" "$FIN_RC,$(fields)" "0,PASS,VERIFIED,CLOSED,PASS,PENDING,INCOMPLETE"
eq "...review file sha256 is recorded" "$(jf '.security_review_source.sha256')" "$(sha256sum "$SR/closed" | cut -d' ' -f1)"
run_fin revbad sc_review_bad
eq "ambiguous review file stays OPEN and is reported as a finding" "$(jf .security_review),$(jf '[.findings[]|select(test("security_review stays OPEN"))]|length')" "OPEN,1"
expect "the docker stub was never invoked by any finalisation scenario" test ! -e "$T/docker-was-invoked"

echo "== source archive + provenance (history-free source transfer) =="
A="$T/arch"
mkdir -p "$A/repo/scripts/integration" "$A/repo/pkg/sub"
cp "$HERE"/*.sh "$A/repo/scripts/integration/"
git init -q "$A/repo"
printf 'package sub\n' >"$A/repo/pkg/sub/a.go"
printf '#!/bin/sh\necho hi\n' >"$A/repo/tool.sh" && chmod 755 "$A/repo/tool.sh"
printf 'module example\n\ngo 1.22\n' >"$A/repo/go.mod"
ln -s pkg/sub/a.go "$A/repo/link.go"
git -C "$A/repo" add -A && git -C "$A/repo" -c user.email=a@b -c user.name=t commit -q -m "snapshot"
ASHA="$(git -C "$A/repo" rev-parse HEAD)"
ATREE="$(git -C "$A/repo" rev-parse 'HEAD^{tree}')"
MK="$A/repo/scripts/integration/make-source-archive.sh"
expect "bash -n make-source-archive.sh" bash -n "$HERE/make-source-archive.sh"
refuses "archive: short SHA refused" bash "$MK" "${ASHA:0:12}" "$A/o"
refuses "archive: unknown (well-formed) SHA refused" bash "$MK" "$(printf 'a%.0s' {1..40})" "$A/o"
refuses "archive: a tree object (not a commit) refused" bash "$MK" "$ATREE" "$A/o"
refuses "archive: output directory inside the work tree refused" bash "$MK" "$ASHA" "$A/repo/out"
echo dirty >>"$A/repo/tool.sh"
refuses "archive: modified tracked file (dirty tree) refused" bash "$MK" "$ASHA" "$A/o"
git -C "$A/repo" checkout -q -- tool.sh
echo x >"$A/repo/untracked.txt"
refuses "archive: untracked file (dirty tree) refused" bash "$MK" "$ASHA" "$A/o"
rm "$A/repo/untracked.txt"
expect "archive: clean tree + exact SHA accepted" bash "$MK" "$ASHA" "$A/out1"
TB="$A/out1/dso-${ASHA:0:12}.tar.gz"
expect "archive: tarball, provenance.json, SHA256SUMS produced" bash -c "test -s '$TB' && test -s '$A/out1/provenance.json' && test -s '$A/out1/SHA256SUMS'"
expect "archive: SHA256SUMS verifies" bash -c "cd '$A/out1' && sha256sum -c --quiet SHA256SUMS"
eq "provenance: sha" "$(jq -r .sha "$A/out1/provenance.json")" "$ASHA"
eq "provenance: tree_sha = git rev-parse SHA^{tree}" "$(jq -r .tree_sha "$A/out1/provenance.json")" "$ATREE"
eq "provenance: branch recorded" "$(jq -r '.branch|length>0' "$A/out1/provenance.json")" true
eq "provenance: tarball sha256 matches the file" "$(jq -r .archive.sha256 "$A/out1/provenance.json")" "$(sha256sum "$TB" | cut -d' ' -f1)"
eq "provenance: creation time, commit time and tool versions present" "$(jq -r '[.created,.commit_time,.tools.git,.tools.gzip,.tools.tar,.tools.sha256sum]|map(length>0)|all' "$A/out1/provenance.json")" true
eq "archive: no .git and no history in the tarball" "$(tar -tzf "$TB" | grep -c '/\.git\(/\|$\)' || true)" 0
sleep 1
bash "$MK" "$ASHA" "$A/out2" >/dev/null 2>&1
eq "archive: deterministic (identical tarball sha256 on a second run)" "$(sha256sum <"$A/out2/dso-${ASHA:0:12}.tar.gz")" "$(sha256sum <"$TB")"
refuses "archive: refuses to overwrite existing output" bash "$MK" "$ASHA" "$A/out1"

mkdir -p "$A/x" && tar -xzf "$TB" -C "$A/x" --no-same-owner
SRC="$A/x/dso-${ASHA:0:12}"
PROV="$A/out1/provenance.json"
expect "extracted archive has no .git" test ! -e "$SRC/.git"
expect "provenance accepted for the pristine extraction" m0_check_source_provenance "$PROV" "$SRC" "$ASHA"
eq "recomputed tree hash equals git's tree sha" "$(m0_tree_hash "$SRC")" "$ATREE"
( m0_check_source_provenance "$PROV" "$SRC" "$ASHA"; echo "$M0_SHA $M0_TREE" >"$T/prov.vars" )
eq "provenance check records sha and tree" "$(cat "$T/prov.vars")" "$ASHA $ATREE"
mkdir -p "$SRC/artifacts/m0-x" && echo e >"$SRC/artifacts/m0-x/summary.txt"
expect "artifacts/ (evidence dir) does not count against the tree hash" m0_check_source_provenance "$PROV" "$SRC" "$ASHA"
rm -rf "$SRC/artifacts"
fresh() { rm -rf "$A/t" && mkdir -p "$A/t" && cp -a "$SRC" "$A/t/src" && TS="$A/t/src"; }
fresh; echo "// tampered" >>"$TS/pkg/sub/a.go"
refuses "tampered file => refused" m0_check_source_provenance "$PROV" "$TS" "$ASHA"
fresh; echo "package x" >"$TS/pkg/extra.go"
refuses "extra file => refused" m0_check_source_provenance "$PROV" "$TS" "$ASHA"
fresh; rm "$TS/go.mod"
refuses "deleted file => refused" m0_check_source_provenance "$PROV" "$TS" "$ASHA"
fresh; chmod 644 "$TS/tool.sh"
refuses "mode change (exec bit) => refused" m0_check_source_provenance "$PROV" "$TS" "$ASHA"
fresh; rm "$TS/link.go"; ln -s /etc/passwd "$TS/link.go"
refuses "changed symlink target => refused" m0_check_source_provenance "$PROV" "$TS" "$ASHA"
fresh; mkdir "$TS/.git"
refuses ".git present in provenance mode => refused" m0_check_source_provenance "$PROV" "$TS" "$ASHA"
fresh; printf 'pkg/sub/\n' >"$TS/.gitignore"
refuses "a planted .gitignore cannot hide a change (extra file)" m0_check_source_provenance "$PROV" "$TS" "$ASHA"
fresh
refuses "SHA != DSO_IT_EXPECT_SHA => refused" m0_check_source_provenance "$PROV" "$TS" "$(printf '0%.0s' {1..40})"
jq '.tree_sha = ("0" * 40)' "$PROV" >"$A/p-badtree.json"
refuses "provenance with a different tree_sha => refused" m0_check_source_provenance "$A/p-badtree.json" "$TS" "$ASHA"
jq '.schema = 2' "$PROV" >"$A/p-schema.json"
refuses "unsupported provenance schema => refused" m0_check_source_provenance "$A/p-schema.json" "$TS" "$ASHA"
jq '.sha = "abc123"' "$PROV" >"$A/p-sha.json"
refuses "provenance with a short sha => refused" m0_check_source_provenance "$A/p-sha.json" "$TS" "abc123"
echo 'not json' >"$A/p-junk.json"
refuses "non-JSON provenance => refused" m0_check_source_provenance "$A/p-junk.json" "$TS" "$ASHA"
refuses "missing provenance file => refused" m0_check_source_provenance "$A/none.json" "$TS" "$ASHA"
ln -sf "$PROV" "$A/p-link.json"
refuses "symlinked provenance file => refused" m0_check_source_provenance "$A/p-link.json" "$TS" "$ASHA"

echo "== m0_resolve_source (git mode vs provenance mode) =="
refuses "no .git and no provenance => refused" bash -c "source '$HERE/m0-lib.sh'; unset DSO_IT_SOURCE_PROVENANCE DSO_IT_EXPECT_SHA; m0_resolve_source '$TS'"
refuses "no .git, provenance but no DSO_IT_EXPECT_SHA => refused" env DSO_IT_SOURCE_PROVENANCE="$PROV" bash -c "source '$HERE/m0-lib.sh'; unset DSO_IT_EXPECT_SHA; m0_resolve_source '$TS'"
expect "no .git + matching provenance + pinned SHA => accepted" env DSO_IT_SOURCE_PROVENANCE="$PROV" DSO_IT_EXPECT_SHA="$ASHA" bash -c "source '$HERE/m0-lib.sh'; m0_resolve_source '$TS'"
eq "...records the SHA and mode from the provenance" "$(env DSO_IT_SOURCE_PROVENANCE="$PROV" DSO_IT_EXPECT_SHA="$ASHA" bash -c "source '$HERE/m0-lib.sh'; m0_resolve_source '$TS' && echo \"\$M0_SOURCE_MODE \$M0_SHA\"")" "provenance $ASHA"
fresh; echo "// tampered" >>"$TS/pkg/sub/a.go"
refuses "no .git + tampered file => refused" env DSO_IT_SOURCE_PROVENANCE="$PROV" DSO_IT_EXPECT_SHA="$ASHA" bash -c "source '$HERE/m0-lib.sh'; m0_resolve_source '$TS'"
refuses ".git + DSO_IT_SOURCE_PROVENANCE together => refused (ambiguous)" env DSO_IT_SOURCE_PROVENANCE="$PROV" bash -c "source '$HERE/m0-lib.sh'; m0_resolve_source '$A/repo'"
expect "with .git the old checks apply (clean tree accepted)" bash -c "source '$HERE/m0-lib.sh'; unset DSO_IT_SOURCE_PROVENANCE DSO_IT_EXPECT_SHA; m0_resolve_source '$A/repo' && [[ \$M0_SOURCE_MODE == git && \$M0_SHA == $ASHA && \$M0_TREE == $ATREE ]]"
echo x >"$A/repo/untracked.txt"
refuses "with .git a dirty tree is still refused" bash -c "source '$HERE/m0-lib.sh'; unset DSO_IT_SOURCE_PROVENANCE DSO_IT_EXPECT_SHA; m0_resolve_source '$A/repo'"
rm "$A/repo/untracked.txt"
refuses "with .git, SHA != DSO_IT_EXPECT_SHA is still refused" env DSO_IT_EXPECT_SHA="$(printf '0%.0s' {1..40})" bash -c "source '$HERE/m0-lib.sh'; m0_resolve_source '$A/repo'"
fresh
expect "source-tree-unchanged (provenance mode) accepts the pristine tree" env M0_SOURCE_MODE=provenance M0_TREE="$ATREE" bash -c "source '$HERE/m0-lib.sh'; m0_source_tree_unchanged '$TS'"
echo x >"$TS/new.file"
refuses "source-tree-unchanged (provenance mode) detects a change made during the run" env M0_SOURCE_MODE=provenance M0_TREE="$ATREE" bash -c "source '$HERE/m0-lib.sh'; m0_source_tree_unchanged '$TS'"
refuses "source-tree-unchanged with an unknown mode is refused" env M0_SOURCE_MODE= bash -c "source '$HERE/m0-lib.sh'; m0_source_tree_unchanged '$TS'"

echo "== run-all.sh static + refusal behaviour (docker stub must never be invoked) =="
RA="$HERE/run-all.sh"
expect "bash -n run-all.sh" bash -n "$RA"
expect "bash -n m0-lib.sh" bash -n "$HERE/m0-lib.sh"
expect "bash -n m0-finalize.sh" bash -n "$HERE/m0-finalize.sh"
expect "bash -n bootstrap-test-vm.sh" bash -n "$HERE/bootstrap-test-vm.sh"
expect "no TODO/FIXME/placeholder markers remain in the runner" bash -c "! grep -qE 'TODO|FIXME|PLACEHOLDER' '$RA' '$HERE/m0-lib.sh' '$HERE/m0-finalize.sh'"
expect "no docker prune anywhere in the runner or cleanup" bash -c "! grep -hv '^[[:space:]]*#' '$RA' '$HERE/cleanup-integration.sh' '$HERE/m0-lib.sh' '$HERE/m0-finalize.sh' | grep -Eq 'prune'"
expect "runner never calls sudo/systemctl/apt/usermod (host privileged ops)" bash -c "! grep -hv '^[[:space:]]*#' '$RA' '$HERE/m0-lib.sh' '$HERE/m0-finalize.sh' | grep -Eq '(^|[;&|(]|then |do )[[:space:]]*(sudo|systemctl|apt-get|apt|usermod)( |$)'"
expect "external prerequisites are not in the automated step list" bash -c "! sed -n '/^REQUIRED_STEPS=(/,/^)/p' '$RA' | grep -q 'external-'"
expect "external prerequisites have their own EXTERNAL_STEPS list" bash -c "grep -q '^EXTERNAL_STEPS=(external-provisioning-provenance external-network-perimeter external-vm-teardown)' '$RA'"
expect "scenario-inject is a required step and requires all 18 named inject tests" bash -c "sed -n '/^REQUIRED_STEPS=(/,/^)/p' '$RA' | grep -q 'scenario-inject' && grep -q 'gotest_step scenario-inject 0 \"\${#INJECT_TESTS\[@\]}\"' '$RA' && [ \"\$(sed -n '/^INJECT_TESTS=(/,/)/p' '$RA' | grep -o 'TestInjectSecurity_[0-9]*_[A-Za-z]*' | sort -u | wc -l)\" = 18 ]"
expect "every required inject test name is defined in test/docker" bash -c "for t in \$(sed -n '/^INJECT_TESTS=(/,/)/p' '$RA' | grep -o 'TestInjectSecurity_[0-9]*_[A-Za-z]*' | sort -u); do grep -q \"^func \$t(t \*testing.T)\" '$HERE/../../test/docker/inject_security_test.go' || { echo missing \$t; exit 1; }; done"
expect "scenario-n19-diagnostic is a required step, runs -run '^TestN19Diag_' with the dockerint tag and requires all 14 named tests" bash -c "sed -n '/^REQUIRED_STEPS=(/,/^)/p' '$RA' | grep -q 'scenario-n19-diagnostic' && grep -q \"gotest_step scenario-n19-diagnostic 0 \\\"\\\${#N19_DIAG_TESTS\\[@\\]}\\\"\" '$RA' && grep -q \"'^TestN19Diag_' ./test/docker\" '$RA' && [ \"\$(sed -n '/^N19_DIAG_TESTS=(/,/)/p' '$RA' | grep -o 'TestN19Diag_[A-Z]_[A-Za-z]*' | sort -u | wc -l)\" = 14 ]"
expect "every N19 diagnostic test name is defined in test/docker" bash -c "for t in \$(sed -n '/^N19_DIAG_TESTS=(/,/)/p' '$RA' | grep -o 'TestN19Diag_[A-Z]_[A-Za-z]*' | sort -u); do grep -qh \"^func \$t(t \*testing.T)\" '$HERE'/../../test/docker/n19_diag_*_test.go || exit 1; done"
expect "the scenario-n19-diagnostic step is not allowed to skip (ALLOW_SKIP=0)" bash -c "grep -q 'gotest_step scenario-n19-diagnostic 0 ' '$RA'"
expect "the n19 diagnostic evidence is captured to evidence/n19-evidence.jsonl" bash -c "grep -q 'evidence/n19-evidence.jsonl' '$RA'"
expect "exit codes derive from automated_integration only" bash -c "grep -q 'case \"\$auto\" in PASS) exit 0' '$HERE/m0-finalize.sh'"
if command -v ss >/dev/null 2>&1; then
  out="$(DOCKER_HOST=tcp://127.0.0.1:1 bash "$RA" 2>&1)"
  rc=$?
  eq "run-all refuses with DOCKER_HOST set (exit 3)" "$rc" 3
  expect "...and says why" grep -q 'DOCKER_HOST is set' <<<"$out"
  # Refusals are exercised from COPIES of the scripts (ROOT is derived from the script location).
  RG="$T/ra-git"
  mkdir -p "$RG/scripts/integration" && cp "$HERE"/*.sh "$RG/scripts/integration/"
  git init -q "$RG" && git -C "$RG" add -A && git -C "$RG" -c user.email=a@b -c user.name=t commit -q -m i
  if [[ ! -e /etc/dso-integration/environment ]]; then
    out="$(env -u DSO_IT_SOURCE_PROVENANCE -u DSO_IT_EXPECT_SHA bash "$RG/scripts/integration/run-all.sh" 2>&1)"
    rc=$?
    eq "run-all (git mode, clean) refuses without the provisioning marker (exit 3)" "$rc" 3
    expect "...and says why" grep -q 'not a provisioned test VM' <<<"$out"
  else
    echo "  skip marker-missing refusal: /etc/dso-integration/environment exists on this host"
  fi
  echo dirty >"$RG/untracked.txt"
  out="$(env -u DSO_IT_SOURCE_PROVENANCE bash "$RG/scripts/integration/run-all.sh" 2>&1)"; rc=$?
  eq "run-all (git mode, dirty tree) refuses (exit 3)" "$rc" 3
  expect "...and says the tree is not clean" grep -q 'not clean' <<<"$out"
  rm "$RG/untracked.txt"
  # extracted archive, no .git
  RX="$A/x/dso-${ASHA:0:12}"
  out="$(env -u DSO_IT_SOURCE_PROVENANCE -u DSO_IT_EXPECT_SHA bash "$RX/scripts/integration/run-all.sh" 2>&1)"; rc=$?
  eq "run-all with no .git and no provenance refuses (exit 3)" "$rc" 3
  expect "...and says why" grep -q 'no .git' <<<"$out"
  out="$(env -u DSO_IT_EXPECT_SHA DSO_IT_SOURCE_PROVENANCE="$PROV" bash "$RX/scripts/integration/run-all.sh" 2>&1)"; rc=$?
  eq "run-all with provenance but no DSO_IT_EXPECT_SHA refuses (exit 3)" "$rc" 3
  expect "...and says the SHA must be pinned" grep -q 'DSO_IT_EXPECT_SHA' <<<"$out"
  rm -rf "$A/tam" && mkdir -p "$A/tam" && cp -a "$RX" "$A/tam/src" && echo "// tampered" >>"$A/tam/src/pkg/sub/a.go"
  out="$(env DSO_IT_SOURCE_PROVENANCE="$PROV" DSO_IT_EXPECT_SHA="$ASHA" bash "$A/tam/src/scripts/integration/run-all.sh" 2>&1)"; rc=$?
  eq "run-all on a tampered extracted archive refuses (exit 3)" "$rc" 3
  expect "...and reports the tree mismatch" grep -q 'do not match the archive' <<<"$out"
  if [[ ! -e /etc/dso-integration/environment ]]; then
    out="$(env DSO_IT_SOURCE_PROVENANCE="$PROV" DSO_IT_EXPECT_SHA="$ASHA" bash "$RX/scripts/integration/run-all.sh" 2>&1)"; rc=$?
    eq "run-all on the pristine archive passes the source check and stops at the missing marker (exit 3)" "$rc" 3
    expect "...the refusal is the marker, not the source" grep -q 'not a provisioned test VM' <<<"$out"
    expect "...and nothing was created in the extracted tree" test ! -e "$RX/artifacts"
  fi
else
  echo "  skip run-all refusal tests: 'ss' not installed"
fi
expect "the docker stub was never invoked" test ! -e "$T/docker-was-invoked"

echo
echo "selftest: $PASS passed, $FAIL failed"
[[ $FAIL -eq 0 ]]
