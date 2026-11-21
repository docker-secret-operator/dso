#!/usr/bin/env bash
# m0-finalize.sh -- evidence collection, ownership-scoped cleanup, foreign-resource
# diff, leak scan and the one-shot finalisation (summary + SHA256SUMS + exit code) of
# run-all.sh. Sourced by run-all.sh and by selftest.sh (which drives `finalize` with
# stubbed docker/go and a fake artifact directory). Sourcing has NO side effects.
#
# Globals provided by the caller: ROOT, M0_ART, M0_LOGS, M0_STATE, M0_STEPS_FILE, M0_WORK,
# M0_SHA, M0_SOURCE_MODE, M0_TREE, M0_PROV_SHA256, M0_ENV_ID, M0_DAEMON_ID, M0_ENDPOINT,
# M0_SENTINEL, M0_STARTED, M0_RUN_DIR, M0_MARKER_OK, RUN, IMAGE, FINALIZED,
# REQUIRED_STEPS (executable steps), EXTERNAL_STEPS (informational).
# shellcheck disable=SC2154  # globals above are defined by the sourcing script

dk() { timeout "${DK_TIMEOUT:-300}" env "DOCKER_HOST=$M0_ENDPOINT" docker "$@"; }
finding() { printf '%s\n' "$*" >>"$M0_STATE/findings.txt"; }
daemon_verified() { [[ -f $M0_STATE/daemon_verified ]]; }

foreign_snapshot() {
  {
    dk ps -a --no-trunc --format $'{{.ID}}\t{{.Names}}\t{{.Label "dso.test.environment"}}' |
      awk -F'\t' -v e="$M0_ENV_ID" '$3 != e {print "container " $2}'
    dk network ls --format $'{{.ID}}\t{{.Name}}\t{{.Label "dso.test.environment"}}' |
      awk -F'\t' -v e="$M0_ENV_ID" '$2!="bridge" && $2!="host" && $2!="none" && $3 != e {print "network " $2}'
    dk volume ls --format $'{{.Name}}\t{{.Label "dso.test.environment"}}' |
      awk -F'\t' -v e="$M0_ENV_ID" '$2 != e {print "volume " $1}'
  } | sort
}
# ---- evidence / cleanup ----------------------------------------------------------------
s_collect_evidence() {
  local E="$M0_ART/evidence" c
  {
    go version; uname -a; cat /etc/os-release 2>/dev/null; nproc; free -m; df -h /var/lib/docker /var/tmp 2>/dev/null
    ss -tlnp 2>/dev/null
  } >"$E/host.txt" 2>&1 || true
  if ! daemon_verified; then echo "daemon never verified: no Docker evidence collected" >"$E/docker-skipped.txt"; return 0; fi
  {
    dk version
    dk compose version
    dk info --format 'id={{.ID}} labels={{json .Labels}} storage={{.Driver}} cgroup={{.CgroupVersion}}'
  } >"$E/docker.txt" 2>&1 || true
  dk ps -a --filter "label=dso.test.run=$RUN" --format $'table {{.Names}}\t{{.Image}}\t{{.Status}}' >"$E/containers.txt" 2>&1 || true
  dk network ls --filter "label=dso.test.run=$RUN" >"$E/networks.txt" 2>&1 || true
  dk volume ls --filter "label=dso.test.run=$RUN" >"$E/volumes.txt" 2>&1 || true
  for c in $(dk ps -a -q --filter "label=dso.test.run=$RUN" 2>/dev/null); do
    dk logs --tail 300 "$c" 2>&1 | m0_redact >"$E/container-$c.log" || true
  done
  journalctl -u docker -n 300 --no-pager 2>/dev/null | m0_redact >"$E/dockerd-journal.txt" || true
}
leftovers() { # prints "kind:ids" for each remaining resource of THIS run in THIS environment
  local f=(--filter "label=dso.test.environment=$M0_ENV_ID" --filter "label=dso.test.run=$RUN") out
  for out in "container:$(dk ps -aq "${f[@]}")" "network:$(dk network ls -q "${f[@]}")" \
    "volume:$(dk volume ls -q "${f[@]}")" "image:$(dk image ls -q "${f[@]}")"; do
    [[ -z ${out#*:} ]] || echo "$out" | tr '\n' ' ' | sed 's/$/\n/'
  done
}
s_cleanup() {
  local id left
  if ! daemon_verified; then echo "daemon never verified: this run created no Docker resources"; return 0; fi
  if [[ -x $M0_WORK/cleanup ]]; then
    # Only resources labelled with BOTH the environment id and THIS run id (CleanupRun re-verifies the daemon first).
    DSO_INTEGRATION_TESTS=1 timeout 300 "$M0_WORK/cleanup"
  else
    echo "cleanup tool not built: only verifying that this run created nothing"
  fi
  for id in $(dk image ls -q --no-trunc --filter "label=dso.test.environment=$M0_ENV_ID" --filter "label=dso.test.run=$RUN"); do
    dk image rm "$IMAGE" >/dev/null 2>&1 || true
    dk image rm "$id" >/dev/null 2>&1 || true
  done
  left="$(leftovers)"
  [[ -z $left ]] || { echo "remaining resources of this run: $left"; return 1; }
  echo "all resources of run $RUN removed (ownership-scoped, label-filtered removal only)"
}
s_foreign_post() {
  daemon_verified || { echo "daemon never verified"; return 1; }
  foreign_snapshot >"$M0_STATE/foreign.post"
  diff -u "$M0_STATE/foreign.pre" "$M0_STATE/foreign.post" || { echo "foreign resources changed during the run"; return 1; }
  echo "foreign/unmanaged resources identical before and after (none existed)"
}
s_leak_scan() {
  local hits f
  hits="$(grep -rlF -- "$M0_SENTINEL" "$M0_ART" 2>/dev/null || true)"
  if [[ -n $hits ]]; then
    echo "the synthetic secret value appears in evidence (redacted in place):"
    echo "$hits"
    while IFS= read -r f; do sed -i "s/$M0_SENTINEL/<redacted>/g" "$f"; done <<<"$hits"
    return 1
  fi
  echo "no plaintext synthetic secret in any evidence file"
}

# ---------------------------------------------------------------------------
# Finalisation: runs exactly once on every exit path (success, failure, signal).
# ---------------------------------------------------------------------------
m0_write_meta() {
  local dcl dsv dco gov clean=false pv=""
  if daemon_verified; then
    dcl="$(dk version --format '{{.Client.Version}}' 2>/dev/null || echo unknown)"
    dsv="$(dk version --format '{{.Server.Version}}' 2>/dev/null || echo unknown)"
    dco="$(dk compose version --short 2>/dev/null || echo unknown)"
  else
    dcl=unknown dsv=unknown dco=unknown
  fi
  gov="$(go version 2>/dev/null || echo unknown)"
  if m0_step_all_passed "$M0_STEPS_FILE" source-tree-unchanged; then clean=true; fi
  pv="${M0_PROV_SHA256:-}"
  jq -n --arg sha "$M0_SHA" --arg clean "$clean" --arg smode "${M0_SOURCE_MODE:-unknown}" --arg tree "${M0_TREE:-}" --arg pv "$pv" \
    --arg run "$RUN" --arg dir "$M0_RUN_DIR" --arg st "$M0_STARTED" --arg fin "$(m0_now)" \
    --arg host "$(hostname)" --arg os "$( (. /etc/os-release 2>/dev/null && echo "${PRETTY_NAME:-unknown}") || echo unknown)" \
    --arg kern "$(uname -r)" --arg cpus "$(nproc)" \
    --arg mem "$(awk '/MemTotal/ {print int($2/1024)}' /proc/meminfo 2>/dev/null || echo 0)" \
    --arg mid "$(sha256sum /etc/machine-id 2>/dev/null | cut -c1-12)" \
    --arg envid "$M0_ENV_ID" --arg did "$M0_DAEMON_ID" --arg dcl "$dcl" --arg dsv "$dsv" --arg dco "$dco" --arg gov "$gov" '
    {schema: 1, source: {sha: $sha, mode: $smode, tree_sha: $tree, provenance_sha256: (if $pv == "" then null else $pv end), tree_clean: ($clean == "true")}, run_id: $run, run_dir: $dir,
     started: $st, finished: $fin,
     vm: {hostname: $host, os: $os, kernel: $kern, cpus: ($cpus | tonumber), mem_mb: ($mem | tonumber), machine_id_sha256_12: $mid},
     docker: {client: $dcl, server: $dsv, compose: $dco, environment_id: $envid, daemon_id: $did},
     go: {version: $gov},
     limitations: ["traffic reports cover NEW-connection clients only; survival of long-lived connections is not measured by any test",
                   "no claim of zero downtime beyond the measured conditions; no host power-loss testing"]}' >"$M0_STATE/meta.json"
}
# fstep: m0_step, then make sure errexit stays off inside the EXIT trap.
fstep() { m0_step "$@"; set +e; }
finalize() {
  local code=$? auto rel
  trap - EXIT INT TERM
  [[ $FINALIZED -eq 0 ]] || exit "$code"
  FINALIZED=1
  set +e
  [[ $code -eq 0 ]] || echo "run ended early (exit $code); finalising with ownership-scoped cleanup" >&2
  fstep evidence-collection s_collect_evidence
  fstep cleanup s_cleanup
  fstep foreign-resources-post s_foreign_post
  fstep source-tree-unchanged m0_source_tree_unchanged "$ROOT"
  fstep evidence-leak-scan s_leak_scan
  m0_external external-provisioning-provenance \
    "Docker apt pin/key fingerprint and Go tarball SHA-256 were verified when the VM was provisioned, before this runner existed; they cannot be re-proven from inside the VM (installed versions are recorded in summary.json)."
  m0_external external-network-perimeter \
    "Cloud firewall/SSH source restriction and absence of public ingress are properties of the VM's network, not observable from inside it (local listening sockets are checked by host-listeners)."
  m0_external external-vm-teardown \
    "The VM must be destroyed after the evidence is copied off; a runner cannot destroy its own host."
  m0_fill_missing "${REQUIRED_STEPS[@]}"
  m0_write_meta
  jq -n --arg s "$(awk -F'\t' '$1=="cleanup" {s=$5} END {print s}' "$M0_STEPS_FILE")" \
    --arg d "$(awk -F'\t' '$1=="cleanup" {s=$7} END {print s}' "$M0_STEPS_FILE")" '{status: $s, detail: $d}' >"$M0_STATE/cleanup.json"
  if [[ -n ${DSO_IT_SECURITY_REVIEW_FILE:-} && "$(m0_security_review "$DSO_IT_SECURITY_REVIEW_FILE")" != CLOSED ]]; then
    finding "DSO_IT_SECURITY_REVIEW_FILE is set but does not state DSO_SECURITY_REVIEW_STATUS=CLOSED in an acceptable file: security_review stays OPEN."
  fi
  # The N19 verdict is a FINDING computed by the scenario-n19-diagnostic step from its evidence; absent => inconclusive.
  M0_N19_VERDICT="$(cat "$M0_STATE/n19_verdict" 2>/dev/null || echo inconclusive)"
  # The runner never decides security_review (operator file only) and never sees a teardown (always PENDING).
  m0_compute_verdicts "$M0_STEPS_FILE" "${M0_MARKER_OK:-}" "${DSO_IT_SECURITY_REVIEW_FILE:-}" PENDING -- "${REQUIRED_STEPS[@]}" >"$M0_STATE/verdicts.json"
  if ! m0_write_summary_json "$M0_ART/summary.json" "$M0_STATE/meta.json" "$M0_STATE/traffic.json" "$M0_STATE/recovery.json" \
    "$M0_STATE/findings.txt" "$M0_STATE/cleanup.json" "$M0_STATE/verdicts.json" "$M0_STATE/n19.json"; then
    echo "FAILED to write summary.json" >&2
    jq '.automated_integration="FAIL" | .release_readiness="FAIL"' "$M0_STATE/verdicts.json" >"$M0_STATE/verdicts.fail.json" && mv "$M0_STATE/verdicts.fail.json" "$M0_STATE/verdicts.json"
  else
    m0_write_summary_txt "$M0_ART/summary.txt" "$M0_ART/summary.json" || echo "FAILED to write summary.txt" >&2
  fi
  auto="$(jq -r '.automated_integration' "$M0_STATE/verdicts.json")"
  rel="$(jq -r '.release_readiness' "$M0_STATE/verdicts.json")"
  sed -i "s/$M0_SENTINEL/<redacted>/g" "$M0_ART/summary.txt" "$M0_ART/summary.json" 2>/dev/null
  # SHA256SUMS covers every file (including summary.*) except itself.
  (cd "$M0_ART" && find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum >SHA256SUMS) 2>/dev/null
  rm -rf "$M0_WORK"
  echo "automated_integration: $auto   release_readiness: $rel   (a passing automated run is not a release approval)"
  echo "evidence: $M0_ART"
  case "$auto" in PASS) exit 0 ;; FAIL) exit 1 ;; *) exit 2 ;; esac
}

