#!/usr/bin/env bash
# m0-lib.sh -- step bookkeeping, verdict computation, go-test result
# classification, refusal checks and summary generation for run-all.sh.
#
# Sourced by run-all.sh and by selftest.sh. Sourcing has NO side effects and never
# invokes docker. Everything here is fail-closed: anything unknown, missing or
# skipped is never "passed".
#
# Step statuses (the ONLY values): passed | failed | skipped | incomplete
# Step kinds: step | external_prerequisite
#
# Independent verdicts (never collapsed into one; see m0_compute_verdicts):
#   automated_integration   PASS | FAIL | INCOMPLETE   executable steps only
#       FAIL        any executable step failed
#       INCOMPLETE  nothing failed, but a required executable step was skipped,
#                   did not run, or is missing (a zero-test scenario is "failed")
#       PASS        every required executable step passed, none skipped
#       External prerequisites NEVER influence it.
#   environment_attestation VERIFIED | UNVERIFIED      identity/endpoint/daemon/foreign-resource gates + marker
#   security_review         OPEN | CLOSED              operator-owned; default OPEN; read from a file, never decided here
#   cleanup                 PASS | FAIL                ownership-scoped cleanup step
#   vm_teardown             PENDING | DONE             always PENDING in runner output (external)
#   release_readiness       PASS | FAIL | INCOMPLETE   INCOMPLETE unless every one of the above is
#                                                      green/closed/done; the runner can never reach PASS
#   verdict                 alias of release_readiness (compatibility only; not an approval)

# ---- globals set by the caller (run-all.sh / selftest.sh) ----------------------
#   M0_STEPS_FILE  TSV: name start end exit_code status kind note
#   M0_LOGS        directory for per-step logs
#   M0_SENTINEL    synthetic secret value (redacted from every note/summary)

m0_now() { date -u +%Y-%m-%dT%H:%M:%SZ; }

m0_redact() {
  if [[ -n "${M0_SENTINEL:-}" ]]; then sed -e "s/${M0_SENTINEL//\//\\/}/<redacted>/g"; else cat; fi
}

m0_sanitize() { tr '\t\r\n' '   ' | m0_redact | cut -c1-600; }

# m0_record NAME STATUS EXIT_CODE START END KIND NOTE
m0_record() {
  local name=$1 status=$2 rc=$3 start=$4 end=$5 kind=$6 note=${7:-}
  case "$status" in
    passed | failed | skipped | incomplete) ;;
    *) note="invalid status '$status' coerced to incomplete; $note"; status=incomplete ;;
  esac
  note="$(printf '%s' "$note" | m0_sanitize)"
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$name" "$start" "$end" "$rc" "$status" "$kind" "$note" >>"$M0_STEPS_FILE"
  printf '%-38s %-10s %s\n' "$name" "$status" "$note"
  M0_LAST_STATUS=$status
}

# m0_step NAME CMD...  -- run CMD (function or program) with errexit ACTIVE inside
# it, log to $M0_LOGS/NAME.log, record passed/failed. Always returns 0; inspect
# $M0_LAST_STATUS. (A subshell started with `set -e` outside any `if` keeps errexit;
# calling the function from an `if` would silently disable it.)
m0_step() {
  local name=$1 start end rc had_e=0
  shift
  [[ $- == *e* ]] && had_e=1
  start=$(m0_now)
  set +e
  (
    set -Eeuo pipefail
    "$@"
  ) >"$M0_LOGS/$name.log" 2>&1
  rc=$?
  ((had_e)) && set -e
  end=$(m0_now)
  if [[ $rc -eq 0 ]]; then
    m0_record "$name" passed 0 "$start" "$end" step ""
  else
    m0_record "$name" failed "$rc" "$start" "$end" step "see logs/$name.log"
  fi
  return 0
}

# m0_external NAME REASON -- a step that cannot be automated.
m0_external() {
  local t
  t=$(m0_now)
  m0_record "$1" incomplete 0 "$t" "$t" external_prerequisite "EXTERNAL_PREREQUISITE: $2"
}

# m0_verdict FILE REQUIRED_NAME... -> PASS|FAIL|INCOMPLETE on stdout (generic engine).
# Every row counts, whatever its kind.
m0_verdict() {
  local file=$1 name start end rc status kind note failed=0 open=0 rows=0 r
  shift
  local -A seen=()
  if [[ -f $file ]]; then
    while IFS=$'\t' read -r name start end rc status kind note || [[ -n ${name:-} ]]; do
      [[ -n $name ]] || continue
      rows=$((rows + 1))
      seen[$name]=1
      case "$status" in
        passed) ;;
        failed) failed=$((failed + 1)) ;;
        *) open=$((open + 1)) ;;
      esac
    done <"$file"
  fi
  for r in "$@"; do [[ -n ${seen[$r]:-} ]] || open=$((open + 1)); done
  if ((failed > 0)); then echo FAIL
  elif ((open > 0 || rows == 0)); then echo INCOMPLETE
  else echo PASS; fi
}

# m0_automated_integration FILE REQUIRED... -> PASS|FAIL|INCOMPLETE
# Only executable steps count: rows of kind external_prerequisite are ignored and a
# REQUIRED name that is recorded as an external prerequisite is not required here.
m0_automated_integration() {
  local file=$1 tmp v r
  local -a req=()
  shift
  tmp=$(mktemp)
  awk -F'\t' '$6 != "external_prerequisite"' "$file" >"$tmp" 2>/dev/null || true
  for r in "$@"; do
    if awk -F'\t' -v n="$r" '$1==n && $6=="external_prerequisite" {f=1} END {exit !f}' "$file" 2>/dev/null; then continue; fi
    req+=("$r")
  done
  v=$(m0_verdict "$tmp" "${req[@]}")
  rm -f "$tmp"
  echo "$v"
}

# Steps whose success (with the marker) attests that the run happened on the
# designated, dedicated, unshared test daemon.
M0_ATTEST_STEPS=(environment-identity docker-endpoint daemon-identity foreign-resources-pre foreign-resources-post)

# m0_step_all_passed FILE NAME -- true iff NAME has >=1 row and every row is passed.
m0_step_all_passed() {
  awk -F'\t' -v n="$2" '$1==n {t++; if ($5=="passed") p++} END {exit !(t>0 && t==p)}' "$1" 2>/dev/null
}

# m0_environment_attestation FILE MARKER_OK(ok|anything) -> VERIFIED|UNVERIFIED
m0_environment_attestation() {
  local file=$1 marker=${2:-} s
  [[ $marker == ok ]] || { echo UNVERIFIED; return 0; }
  for s in "${M0_ATTEST_STEPS[@]}"; do
    m0_step_all_passed "$file" "$s" || { echo UNVERIFIED; return 0; }
  done
  echo VERIFIED
}

# m0_security_review [FILE] -> OPEN|CLOSED. The runner never decides this: OPEN
# unless an operator-provided file says so explicitly. Accepted file: regular, not a
# symlink, <= 4 KiB, owned by root or the current user, not group/other writable,
# containing EXACTLY ONE line  DSO_SECURITY_REVIEW_STATUS=CLOSED  (or =OPEN) and no
# other DSO_SECURITY_REVIEW_STATUS line. Anything else (absent, malformed, ambiguous) is OPEN.
m0_security_review() {
  local f=${1:-} owner n v size
  [[ -n $f && -f $f && ! -L $f ]] || { echo OPEN; return 0; }
  owner=$(stat -c '%U' "$f" 2>/dev/null) || { echo OPEN; return 0; }
  [[ $owner == root || $owner == "$(id -un)" ]] || { echo OPEN; return 0; }
  m0_mode_not_group_other_writable "$f" || { echo OPEN; return 0; }
  size=$(stat -c '%s' "$f" 2>/dev/null || echo 99999)
  ((size <= 4096)) || { echo OPEN; return 0; }
  n=$(grep -c '^DSO_SECURITY_REVIEW_STATUS=' "$f" || true)
  [[ $n == 1 ]] || { echo OPEN; return 0; }
  v=$(sed -n 's/^DSO_SECURITY_REVIEW_STATUS=//p' "$f" | tr -d '\r')
  if [[ $v == CLOSED ]]; then echo CLOSED; else echo OPEN; fi
}

# m0_cleanup_status FILE -> PASS|FAIL  (the ownership-scoped cleanup step; missing/not passed => FAIL)
m0_cleanup_status() {
  if m0_step_all_passed "$1" cleanup; then echo PASS; else echo FAIL; fi
}

# m0_release_readiness AUTOMATED ATTESTATION SECURITY_REVIEW CLEANUP TEARDOWN -> PASS|FAIL|INCOMPLETE
m0_release_readiness() {
  local auto=$1 att=$2 sec=$3 cln=$4 td=$5
  if [[ $auto == FAIL || $cln == FAIL ]]; then echo FAIL
  elif [[ $auto == PASS && $att == VERIFIED && $sec == CLOSED && $cln == PASS && $td == DONE ]]; then echo PASS
  else echo INCOMPLETE; fi
}

# m0_compute_verdicts STEPS_FILE MARKER_OK SECURITY_REVIEW_FILE TEARDOWN -- REQUIRED_AUTOMATED_STEP...
# -> JSON object with every independent verdict on stdout. TEARDOWN is "DONE" only
# for an operator-confirmed teardown (the runner always passes PENDING).
m0_compute_verdicts() {
  local steps=$1 marker=$2 secfile=$3 td=$4 auto att sec cln rel secsha=""
  shift 4
  if [[ ${1:-} == -- ]]; then shift; fi
  [[ $td == DONE ]] || td=PENDING
  auto=$(m0_automated_integration "$steps" "$@")
  att=$(m0_environment_attestation "$steps" "$marker")
  sec=$(m0_security_review "$secfile")
  cln=$(m0_cleanup_status "$steps")
  rel=$(m0_release_readiness "$auto" "$att" "$sec" "$cln" "$td")
  if [[ -n $secfile && -f $secfile && ! -L $secfile ]]; then secsha=$(sha256sum "$secfile" | cut -d' ' -f1); fi
  jq -n --arg auto "$auto" --arg att "$att" --arg sec "$sec" --arg cln "$cln" --arg td "$td" --arg rel "$rel" \
    --arg secfile "$secfile" --arg secsha "$secsha" '
    {automated_integration: $auto, environment_attestation: $att, security_review: $sec, cleanup: $cln,
     vm_teardown: $td, release_readiness: $rel,
     security_review_source: (if $secfile == "" then null else {file: $secfile, sha256: (if $secsha == "" then null else $secsha end)} end)}'
}

# m0_fill_missing REQUIRED... -- record every required step that never ran.
m0_fill_missing() {
  local r t
  t=$(m0_now)
  for r in "$@"; do
    if ! awk -F'\t' -v n="$r" '$1==n {f=1} END {exit !f}' "$M0_STEPS_FILE" 2>/dev/null; then
      m0_record "$r" incomplete "" "$t" "$t" step "not run (an earlier gate failed or the run was interrupted)"
    fi
  done
}

# ---- go test -json classification ---------------------------------------------
m0__events() { jq -R -c 'fromjson? | select(type=="object")' "$1"; }

# m0_classify_go_json JSON_FILE GO_EXIT ALLOW_SKIP MIN_PASS [REQUIRED_TEST...]
# stdout (TSV): status pass fail skip reason
# Never reports passed when: go exited non-zero, any test/package failed, zero
# tests ran, fewer than MIN_PASS passed, a REQUIRED test did not pass, or (unless
# ALLOW_SKIP=1) any test was skipped.
m0_classify_go_json() {
  local f=$1 rc=$2 allow_skip=$3 min=$4 t
  shift 4
  if [[ ! -s $f ]]; then printf 'failed\t0\t0\t0\tno test output\n'; return 0; fi
  m0__events "$f" | jq -r 'select(.Action=="pass" and .Test!=null) | .Test' | sort -u >"$f.pass"
  m0__events "$f" | jq -r 'select(.Action=="fail" and .Test!=null) | .Test' | sort -u >"$f.fail"
  m0__events "$f" | jq -r 'select(.Action=="skip" and .Test!=null) | .Test' | sort -u >"$f.skip"
  local npass nfail nskip pkgfail
  npass=$(grep -c . "$f.pass" || true)
  nfail=$(grep -c . "$f.fail" || true)
  nskip=$(grep -c . "$f.skip" || true)
  pkgfail=$(m0__events "$f" | jq -r 'select(.Action=="fail" and .Test==null) | .Package' | grep -c . || true)
  local status=passed reason=""
  if ((nfail > 0 || pkgfail > 0)); then
    status=failed reason="failed tests=$nfail, failed packages=$pkgfail"
  elif [[ $rc -ne 0 ]]; then
    status=failed reason="go test exit code $rc"
  elif ((npass + nskip == 0)); then
    status=failed reason="zero tests ran (the -run pattern matched nothing)"
  fi
  if [[ $status == passed ]]; then
    for t in "$@"; do
      if grep -qxF -- "$t" "$f.pass"; then continue; fi
      if grep -qxF -- "$t" "$f.skip"; then
        status=skipped reason="required test skipped: $t"
      else
        status=failed reason="required test did not run: $t"
        break
      fi
    done
  fi
  if [[ $status == passed && $allow_skip != 1 && $nskip -gt 0 ]]; then
    status=skipped reason="$nskip test(s) skipped: $(head -n 5 "$f.skip" | tr '\n' ' ')"
  fi
  if [[ $status == passed ]] && ((npass < min)); then
    status=failed reason="only $npass test(s) passed, at least $min required"
  fi
  printf '%s\t%s\t%s\t%s\t%s\n' "$status" "$npass" "$nfail" "$nskip" "$reason"
}

# m0_uncovered LIST_FILE REGEX... -- print every listed test no REGEX matches.
m0_uncovered() {
  local list=$1 t r hit
  shift
  while IFS= read -r t; do
    [[ $t == Test* ]] || continue
    hit=0
    for r in "$@"; do if [[ $t =~ $r ]]; then hit=1; break; fi; done
    ((hit)) || echo "$t"
  done <"$list"
}

# ---- go-test report extraction -------------------------------------------------
m0_output_lines() {
  m0__events "$1" | jq -r 'select(.Action=="output") | .Output' | sed -e 's/\r$//' -e '/^$/d' \
    -e 's/^[[:space:]]*[A-Za-z0-9_.]*\.go:[0-9]*: //'
}

# m0_parse_traffic JSON_FILE -> JSON array on stdout (one object per TRAFFIC REPORT).
m0_parse_traffic() {
  m0_output_lines "$1" | awk '
    /=== TRAFFIC REPORT: / { n=$0; sub(/.*TRAFFIC REPORT: /,"",n); sub(/ ===.*/,"",n); name=n; next }
    name=="" { next }
    /^rotation phase duration:/ {
      v=$0; sub(/^rotation phase duration: /,"",v); d=v; sub(/ \(.*/,"",d); x=v; sub(/^[^(]*\(/,"",x); sub(/\)$/,"",x)
      printf "%s\t-\trotation_duration\t%s\n%s\t-\trotation_note\t%s\n", name, d, name, x; next }
    /^\[(pre|during|post)\] / {
      ph=$1; gsub(/[\[\]]/,"",ph); rest=$0; sub(/^\[[a-z]+\] /,"",rest)
      if (rest ~ /^failure kinds=/) { sub(/^failure kinds=/,"",rest); printf "%s\t%s\tfailure_kinds_and_http_status\t%s\n", name, ph, rest; next }
      if (rest ~ /^backends observed=/) { split(rest, a, " "); v=a[2]; sub(/observed=/,"",v); printf "%s\t%s\tbackends_observed\t%s\n", name, ph, v; next }
      pre=""; if (rest ~ /^latency /) pre="latency_"
      m=split(rest, a, " ")
      for (i=1;i<=m;i++) { if (a[i] ~ /=/) { k=a[i]; v=a[i]; sub(/=.*/,"",k); sub(/^[^=]*=/,"",v); printf "%s\t%s\t%s%s\t%s\n", name, ph, pre, k, v } }
    }' | jq -R -s '
      [split("\n")[] | select(length>0) | split("\t")]
      | reduce .[] as $r ({};
          (($r[3] | tonumber? ) // $r[3]) as $v
          | if $r[1]=="-" then .[$r[0]][$r[2]] = $v else .[$r[0]].phases[$r[1]][$r[2]] = $v end)
      | to_entries | map({name: .key} + .value)'
}

# m0_parse_recovery JSON_FILE... -> JSON array of {line, metrics{k:v}} for log lines
# that mention recovery (t.Logf lines only; go-test framing lines are ignored).
m0_parse_recovery() {
  local f
  for f in "$@"; do
    m0__events "$f" | jq -r 'select(.Action=="output") | .Output' | sed -e 's/\r$//' -e '/^$/d' |
      grep -E '^[[:space:]]*[A-Za-z0-9_.]+\.go:[0-9]+: ' | sed -e 's/^[[:space:]]*[A-Za-z0-9_.]*\.go:[0-9]*: //' |
      grep -iE 'recover' || true
  done | jq -R -s '
      [split("\n")[] | select(length>0)
       | {line: ., metrics: ([scan("([A-Za-z_][A-Za-z0-9_.-]*)=([^ ,;]+)")] | map({key: .[0], value: (.[1] as $v | ($v | tonumber?) // $v)}) | from_entries)}]'
}

# ---- refusal / precondition checks (pure; arguments, not hidden globals) ------
m0_mode_not_group_other_writable() { # PATH
  local mode
  mode=$(stat -c '%a' "$1") || return 1
  (((8#$mode & 8#022) == 0))
}

# m0_check_no_docker_env -- ambient Docker/Compose configuration is refused.
m0_check_no_docker_env() {
  local v bad=0
  for v in DOCKER_HOST DOCKER_CONTEXT DOCKER_CONFIG DOCKER_TLS_VERIFY DOCKER_CERT_PATH DOCKER_API_VERSION \
    COMPOSE_FILE COMPOSE_PROJECT_NAME COMPOSE_PROFILES COMPOSE_ENV_FILES COMPOSE_PATH_SEPARATOR; do
    if [[ -n ${!v:-} ]]; then echo "refused: $v is set (the endpoint is configured explicitly; unset it)" >&2; bad=1; fi
  done
  return $bad
}

# m0_check_marker FILE EXPECTED_OWNER -- sets M0_ENV_ID and M0_DAEMON_ID.
m0_check_marker() {
  local f=$1 owner=$2 d
  [[ -f $f && ! -L $f ]] || { echo "refused: $f missing or not a regular file: this is not a provisioned test VM" >&2; return 1; }
  [[ "$(stat -c '%U' "$f")" == "$owner" ]] || { echo "refused: $f is not owned by $owner" >&2; return 1; }
  m0_mode_not_group_other_writable "$f" || { echo "refused: $f is group/other writable" >&2; return 1; }
  d=$(dirname "$f")
  [[ "$(stat -c '%U' "$d")" == "$owner" ]] || { echo "refused: $d is not owned by $owner" >&2; return 1; }
  m0_mode_not_group_other_writable "$d" || { echo "refused: $d is group/other writable" >&2; return 1; }
  M0_ENV_ID=$(sed -n 's/^DSO_TEST_ENVIRONMENT_ID=//p' "$f" | head -n 1)
  M0_DAEMON_ID=$(sed -n 's/^DSO_TEST_DAEMON_ID=//p' "$f" | head -n 1)
  [[ $M0_ENV_ID =~ ^[A-Za-z0-9][A-Za-z0-9._-]{7,63}$ ]] || { echo "refused: marker has no valid DSO_TEST_ENVIRONMENT_ID" >&2; return 1; }
  [[ -n $M0_DAEMON_ID && $M0_DAEMON_ID =~ ^[A-Za-z0-9:._-]{8,128}$ ]] || { echo "refused: marker has no valid DSO_TEST_DAEMON_ID" >&2; return 1; }
}

# m0_check_clean_tree DIR -- tracked changes AND untracked files refuse the run
# (artifacts/ is the evidence directory and is excluded).
m0_check_clean_tree() {
  local dir=${1:-.} out
  git -C "$dir" rev-parse --git-dir >/dev/null 2>&1 || { echo "refused: $dir is not a git work tree" >&2; return 1; }
  out=$(git -C "$dir" status --porcelain --untracked-files=all -- . ':(exclude)artifacts') || return 1
  if [[ -n $out ]]; then echo "refused: the source tree is not clean:" >&2; echo "$out" | head -n 20 >&2; return 1; fi
}

m0_check_sha() { # SHA [EXPECTED]
  [[ $1 =~ ^[0-9a-f]{40}$ ]] || { echo "refused: not a full commit SHA: $1" >&2; return 1; }
  if [[ -n ${2:-} && $1 != "$2" ]]; then echo "refused: HEAD $1 != DSO_IT_EXPECT_SHA $2" >&2; return 1; fi
}

m0_check_run_id() { [[ ${1:-} =~ ^dso-it-[0-9a-f]{8,16}$ ]] || { echo "invalid run id: ${1:-}" >&2; return 1; }; }

# m0_check_endpoint ENDPOINT -- only a local unix socket is accepted by the runner.
m0_check_endpoint() {
  [[ ${1:-} =~ ^unix:///[A-Za-z0-9._/-]+$ ]] || { echo "refused: endpoint must be unix:///absolute/path, got '${1:-}'" >&2; return 1; }
}

# ---- image content ------------------------------------------------------------
# m0_image_offenders -- stdin: one layer path per line; stdout: offending paths.
# .pem is forbidden except the base image's CA bundle locations.
m0_image_offenders() {
  local in
  in=$(sed -e 's#^\./##' -e 's#^/##')
  {
    printf '%s\n' "$in" | grep -E \
      -e '(^|/)\.git(/|$)' \
      -e '(^|/)data(/|$)' \
      -e '\.(db|db-wal|db-shm|db-journal|rvf|rvf\.lock|sqlite|sqlite3)$' \
      -e '\.(key|p12|pfx|jks)$' \
      -e '(^|/)id_(rsa|dsa|ecdsa|ed25519)$' \
      -e '(^|/)\.(claude-flow|claude|swarm)(/|$)' \
      -e '(^|/)\.env(\.|$)' \
      -e '(^|/)local-secrets(/|$)' \
      -e '\.go$' || true
    printf '%s\n' "$in" | grep -E '\.pem$' | grep -vE '^(etc/ssl/cert\.pem|etc/ssl/certs/|usr/share/ca-certificates/)' || true
  } | sort -u
}

# ---- summary generation ---------------------------------------------------------
m0_steps_json() {
  jq -R -s '[split("\n")[] | select(length>0) | split("\t")
    | {name: .[0], started: .[1], finished: .[2],
       exit_code: (.[3] | tonumber? // null), status: .[4], kind: .[5], note: (.[6] // "")}]' "$M0_STEPS_FILE"
}

# m0_write_summary_json OUT META_JSON TRAFFIC_JSON RECOVERY_JSON FINDINGS_FILE CLEANUP_DETAIL_JSON VERDICTS_JSON
# VERDICTS_JSON is the output of m0_compute_verdicts. `verdict` and `automated_verdict`
# are compatibility aliases (of release_readiness and automated_integration).
m0_write_summary_json() {
  local out=$1 meta=$2 traffic=$3 recovery=$4 findings=$5 cleanup=$6 verdicts=$7
  local steps
  steps=$(m0_steps_json)
  jq -n \
    --slurpfile meta "$meta" --slurpfile traffic "$traffic" --slurpfile recovery "$recovery" \
    --slurpfile cleanup "$cleanup" --slurpfile verdicts "$verdicts" --argjson steps "$steps" \
    --rawfile findings "$findings" '
    ($meta[0]) + ($verdicts[0]) + {
      verdict: $verdicts[0].release_readiness,
      verdict_alias_of: "release_readiness",
      automated_verdict: $verdicts[0].automated_integration,
      exit_code_basis: "automated_integration (0 PASS, 1 FAIL, 2 INCOMPLETE; 3 = refused before any run)",
      scenarios: $steps,
      traffic: $traffic[0],
      recovery: $recovery[0],
      cleanup_detail: $cleanup[0],
      findings: ($findings | split("\n") | map(select(length>0))),
      external_prerequisites: ($steps | map(select(.kind=="external_prerequisite") | {name, reason: .note})),
      counts: {
        passed: ($steps | map(select(.status=="passed")) | length),
        failed: ($steps | map(select(.status=="failed")) | length),
        skipped: ($steps | map(select(.status=="skipped")) | length),
        incomplete: ($steps | map(select(.status=="incomplete")) | length)
      }
    }' >"$out"
}

# m0_write_summary_txt OUT SUMMARY_JSON
m0_write_summary_txt() {
  local out=$1 js=$2
  {
    jq -r '"DSO M0 runtime verification\n\nautomated_integration:   \(.automated_integration)   (executable steps only; external prerequisites do not affect it)\nenvironment_attestation: \(.environment_attestation)   (identity, endpoint, daemon, foreign-resource gates + marker)\nsecurity_review:         \(.security_review)   (operator-owned; the runner never decides it)\ncleanup:                 \(.cleanup)   (ownership-scoped)\nvm_teardown:             \(.vm_teardown)   (external; destroy the VM after the evidence is copied off)\nrelease_readiness:       \(.release_readiness)   (needs automated PASS + attestation VERIFIED + security review CLOSED + cleanup PASS + teardown DONE)\n\nsource sha:        \(.source.sha)\nsource mode:       \(.source.mode)   tree \(.source.tree_sha)   tree clean: \(.source.tree_clean)\nrun id:            \(.run_id)\nstarted/finished:  \(.started) / \(.finished)\nvm:                \(.vm.hostname) (\(.vm.os), kernel \(.vm.kernel), \(.vm.cpus) cpu, \(.vm.mem_mb) MB, machine-id#\(.vm.machine_id_sha256_12))\nenvironment id:    \(.docker.environment_id)\ndaemon id:         \(.docker.daemon_id)\ndocker:            client \(.docker.client) / server \(.docker.server)\ncompose:           \(.docker.compose)\ngo:                \(.go.version)\ncounts:            passed=\(.counts.passed) failed=\(.counts.failed) skipped=\(.counts.skipped) incomplete=\(.counts.incomplete)\n"' "$js"
    printf '%-36s %-10s %-5s %-21s %s\n' STEP STATUS EXIT STARTED NOTE
    jq -r '.scenarios[] | [.name, .status, (.exit_code // "-" | tostring), .started, (.finished + "  " + .note)] | @tsv' "$js" |
      awk -F'\t' '{printf "%-36s %-10s %-5s %-21s %s\n", $1, $2, $3, $4, $5}'
    echo
    echo "traffic measurements:"
    jq -r '(.traffic | if length==0 then ["  (none recorded)"] else map(
        "  " + .name + ": rotation " + ((.rotation_duration // "?")|tostring) + " (" + ((.rotation_note // "")|tostring) + ")",
        (.phases // {} | to_entries[] | "    [" + .key + "] requests=" + (.value.requests|tostring) + " ok=" + (.value.ok|tostring) + " failed=" + (.value.failed|tostring)
          + " conn_failures=" + (.value.connection_failures|tostring) + " timeouts=" + (.value.timeouts|tostring)
          + " backends=" + (.value.backends_observed|tostring)
          + " latency p50=" + (.value.latency_p50|tostring) + " p95=" + (.value.latency_p95|tostring) + " p99=" + (.value.latency_p99|tostring) + " max=" + (.value.latency_max|tostring))
      ) | flatten end)[]' "$js"
    echo
    echo "recovery measurements:"
    jq -r '(.recovery | if length==0 then ["  (none recorded)"] else map("  " + .line) end)[]' "$js"
    echo
    echo "cleanup detail: $(jq -r '.cleanup_detail.status + " - " + .cleanup_detail.detail' "$js")"
    echo
    echo "findings:"
    jq -r '(.findings | if length==0 then ["  (none)"] else map("  - " + .) end)[]' "$js"
    echo
    echo "external prerequisites (they never change automated_integration; they keep release_readiness INCOMPLETE):"
    jq -r '(.external_prerequisites | if length==0 then ["  (none)"] else map("  - " + .name + ": " + .reason) end)[]' "$js"
    echo
    echo "AUTOMATED_INTEGRATION: $(jq -r '.automated_integration' "$js")"
    echo "RELEASE_READINESS: $(jq -r '.release_readiness' "$js")"
    echo "VERDICT: $(jq -r '.verdict' "$js")   (compatibility alias of release_readiness; a passing automated run is NOT a release approval)"
  } >"$out"
}

# ---- source provenance (archive without .git) -------------------------------------
# m0_tree_hash DIR -> prints the git tree object id of DIR's contents (names, modes,
# bytes; top-level artifacts/ excluded). Host/global git config is ignored; ignore
# rules and attributes are neutralised so the result depends on the files only.
m0_tree_hash() {
  local dir=$1 g t="" rc=0
  g=$(mktemp -d) || return 1
  if git init -q --object-format=sha1 "$g/w" >/dev/null 2>&1; then
    t=$(
      export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_DIR="$g/w/.git" GIT_WORK_TREE="$dir"
      git -c core.autocrlf=false -c core.safecrlf=false -c core.fileMode=true -c core.symlinks=true \
        -c core.attributesFile=/dev/null -c core.excludesFile=/dev/null \
        add -A -f -- . ':(exclude)artifacts' >/dev/null 2>&1 && git write-tree
    ) || rc=1
  else
    rc=1
  fi
  rm -rf "$g"
  [[ $rc -eq 0 && $t =~ ^[0-9a-f]{40}$ ]] || return 1
  echo "$t"
}

# m0_check_source_provenance PROVENANCE_JSON DIR [EXPECTED_SHA]
# Accepts only when DIR has no .git and the tree hash recomputed from DIR's files equals
# the tree_sha recorded in the provenance. Sets M0_SHA, M0_TREE, M0_PROV_SHA256.
# NOTE: the provenance file is self-asserted; its integrity comes from the SHA256SUMS
# check done at transfer time and from the out-of-band EXPECTED_SHA.
m0_check_source_provenance() {
  local prov=$1 dir=$2 expect=${3:-} sha tree actual
  [[ -f $prov && ! -L $prov ]] || { echo "refused: provenance file '$prov' missing or not a regular file" >&2; return 1; }
  [[ ! -e $dir/.git ]] || { echo "refused: $dir contains .git; provenance mode is only for history-free archives" >&2; return 1; }
  jq -e 'type=="object"' "$prov" >/dev/null 2>&1 || { echo "refused: provenance is not a JSON object" >&2; return 1; }
  [[ "$(jq -r '.schema // empty' "$prov")" == 1 ]] || { echo "refused: unsupported provenance schema" >&2; return 1; }
  sha=$(jq -r '.sha | if type=="string" then . else "" end' "$prov")
  tree=$(jq -r '.tree_sha | if type=="string" then . else "" end' "$prov")
  m0_check_sha "$sha" "$expect" || return 1
  [[ $tree =~ ^[0-9a-f]{40}$ ]] || { echo "refused: provenance has no valid tree_sha" >&2; return 1; }
  actual=$(m0_tree_hash "$dir") || { echo "refused: could not compute the tree hash of $dir" >&2; return 1; }
  [[ $actual == "$tree" ]] || { echo "refused: the extracted files do not match the archive's recorded tree (recomputed $actual, recorded $tree)" >&2; return 1; }
  M0_SHA=$sha
  M0_TREE=$tree
  M0_PROV_SHA256=$(sha256sum "$prov" | cut -d' ' -f1)
}

# m0_resolve_source ROOT -- decide the source mode and record the exact revision.
#   .git present      -> mode git: clean work tree, SHA = HEAD (old behaviour)
#   .git absent       -> mode provenance: DSO_IT_SOURCE_PROVENANCE required AND DSO_IT_EXPECT_SHA
#                        required (the SHA is pinned out-of-band); tree hash verified
# Sets M0_SOURCE_MODE, M0_SHA, M0_TREE, M0_PROV_SHA256.
m0_resolve_source() {
  local root=$1
  M0_PROV_SHA256=""
  if [[ -e $root/.git ]]; then
    [[ -z ${DSO_IT_SOURCE_PROVENANCE:-} ]] || { echo "refused: both a .git and DSO_IT_SOURCE_PROVENANCE are present; use exactly one source mode" >&2; return 1; }
    m0_check_clean_tree "$root" || return 1
    M0_SOURCE_MODE=git
    M0_SHA=$(git -C "$root" rev-parse HEAD) || return 1
    M0_TREE=$(git -C "$root" rev-parse 'HEAD^{tree}') || return 1
    m0_check_sha "$M0_SHA" "${DSO_IT_EXPECT_SHA:-}" || return 1
  else
    [[ -n ${DSO_IT_SOURCE_PROVENANCE:-} ]] || { echo "refused: no .git in $root and no DSO_IT_SOURCE_PROVENANCE: the source revision cannot be established" >&2; return 1; }
    [[ -n ${DSO_IT_EXPECT_SHA:-} ]] || { echo "refused: provenance mode requires DSO_IT_EXPECT_SHA (pin the commit out-of-band; the provenance file is self-asserted)" >&2; return 1; }
    m0_check_source_provenance "$DSO_IT_SOURCE_PROVENANCE" "$root" "$DSO_IT_EXPECT_SHA" || return 1
    M0_SOURCE_MODE=provenance
  fi
}

# m0_source_tree_unchanged ROOT -- the source tree must equal what was verified at start.
m0_source_tree_unchanged() {
  local root=$1 now
  case "${M0_SOURCE_MODE:-}" in
    git) m0_check_clean_tree "$root" ;;
    provenance)
      now=$(m0_tree_hash "$root") || { echo "could not recompute the source tree hash" >&2; return 1; }
      [[ $now == "${M0_TREE:-}" ]] || { echo "source tree changed during the run: $now != ${M0_TREE:-unset}" >&2; return 1; }
      ;;
    *) echo "unknown source mode '${M0_SOURCE_MODE:-}'" >&2; return 1 ;;
  esac
}
