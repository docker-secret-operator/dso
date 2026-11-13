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
# Verdict: PASS | FAIL | INCOMPLETE
#   FAIL        any step failed
#   INCOMPLETE  nothing failed, but a step was skipped/incomplete/not run, a
#               required step is missing, or an external prerequisite is open
#   PASS        every required step passed, none skipped, no external prerequisite

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

# m0_verdict FILE REQUIRED_NAME... -> PASS|FAIL|INCOMPLETE on stdout.
# Statuses of rows whose kind is external_prerequisite are included.
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

# m0_verdict_automated FILE REQUIRED... -- same, ignoring external_prerequisite rows.
m0_verdict_automated() {
  local file=$1 tmp v
  shift
  tmp=$(mktemp)
  awk -F'\t' '$6 != "external_prerequisite"' "$file" >"$tmp" 2>/dev/null || true
  v=$(m0_verdict "$tmp" "$@")
  rm -f "$tmp"
  echo "$v"
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

# m0_write_summary_json OUT META_JSON TRAFFIC_JSON RECOVERY_JSON FINDINGS_FILE CLEANUP_JSON VERDICT AUTO_VERDICT
m0_write_summary_json() {
  local out=$1 meta=$2 traffic=$3 recovery=$4 findings=$5 cleanup=$6 verdict=$7 auto=$8
  local steps
  steps=$(m0_steps_json)
  jq -n \
    --slurpfile meta "$meta" --slurpfile traffic "$traffic" --slurpfile recovery "$recovery" \
    --slurpfile cleanup "$cleanup" --argjson steps "$steps" \
    --rawfile findings "$findings" --arg verdict "$verdict" --arg auto "$auto" '
    ($meta[0]) + {
      verdict: $verdict,
      automated_verdict: $auto,
      scenarios: $steps,
      traffic: $traffic[0],
      recovery: $recovery[0],
      cleanup: $cleanup[0],
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
    jq -r '"DSO M0 runtime verification\nverdict:           \(.verdict)\nautomated verdict: \(.automated_verdict)  (ignores external prerequisites)\nsource sha:        \(.source.sha)\ntree clean:        \(.source.tree_clean)\nrun id:            \(.run_id)\nstarted/finished:  \(.started) / \(.finished)\nvm:                \(.vm.hostname) (\(.vm.os), kernel \(.vm.kernel), \(.vm.cpus) cpu, \(.vm.mem_mb) MB, machine-id#\(.vm.machine_id_sha256_12))\nenvironment id:    \(.docker.environment_id)\ndaemon id:         \(.docker.daemon_id)\ndocker:            client \(.docker.client) / server \(.docker.server)\ncompose:           \(.docker.compose)\ngo:                \(.go.version)\ncounts:            passed=\(.counts.passed) failed=\(.counts.failed) skipped=\(.counts.skipped) incomplete=\(.counts.incomplete)\n"' "$js"
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
    echo "cleanup: $(jq -r '.cleanup.status + " - " + .cleanup.detail' "$js")"
    echo
    echo "findings:"
    jq -r '(.findings | if length==0 then ["  (none)"] else map("  - " + .) end)[]' "$js"
    echo
    echo "external prerequisites (verdict stays INCOMPLETE while any is open):"
    jq -r '(.external_prerequisites | if length==0 then ["  (none)"] else map("  - " + .name + ": " + .reason) end)[]' "$js"
    echo
    echo "VERDICT: $(jq -r '.verdict' "$js")"
  } >"$out"
}
