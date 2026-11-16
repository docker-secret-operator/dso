#!/usr/bin/env bash
# run-all.sh -- the single entry point for the M0 runtime verification. Run it
# INSIDE the provisioned disposable test VM (docs/testing/DISPOSABLE-VM-EXECUTION-PLAN.md).
# It refuses to run anywhere else. NEVER run it on a development machine.
#
# Fail closed: nothing that did not run, was skipped, or matched zero tests is ever
# reported as passed. The result is NOT one verdict but independent fields (summary.json):
#   automated_integration PASS|FAIL|INCOMPLETE   (executable steps only)  -> exit 0 | 1 | 2
#   environment_attestation VERIFIED|UNVERIFIED, security_review OPEN|CLOSED (operator file
#   DSO_IT_SECURITY_REVIEW_FILE; default OPEN), cleanup PASS|FAIL, vm_teardown PENDING,
#   release_readiness (INCOMPLETE unless every field is green AND an operator confirmed
#   teardown; the runner itself can never produce PASS). `verdict` = alias of release_readiness.
# Exit codes: 0 automated PASS, 1 automated FAIL, 2 automated INCOMPLETE, 3 refused (nothing ran).
# A passing automated run is NOT a release approval.
# Evidence: artifacts/m0-<time>-<sha8>/{summary.txt,summary.json,logs/,evidence/,SHA256SUMS}.
#
# Source: either a git work tree (clean; SHA = HEAD) or an extracted `git archive` with NO .git
# plus DSO_IT_SOURCE_PROVENANCE=<provenance.json> and DSO_IT_EXPECT_SHA=<sha> (tree hash is
# recomputed and must match the provenance). See scripts/integration/make-source-archive.sh.
#
# Cleanup is ownership-scoped (test/dockerguard CleanupRun: resources carrying BOTH
# dso.test.environment and dso.test.run; plus the image this run built, verified by
# the same labels). No docker system/container/volume/network/image prune anywhere.
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/integration/m0-lib.sh
source "$ROOT/scripts/integration/m0-lib.sh"
# shellcheck source=scripts/integration/m0-finalize.sh
source "$ROOT/scripts/integration/m0-finalize.sh"

ENV_FILE=/etc/dso-integration/environment
refuse() { echo "run-all refused: $*" >&2; exit 3; }

# ---------------------------------------------------------------------------
# Step registry: EVERY executable step name here must end up recorded; unrecorded => INCOMPLETE.
# ---------------------------------------------------------------------------
REQUIRED_STEPS=(
  environment-identity docker-endpoint daemon-identity foreign-resources-pre run-identity source-revision
  cleanup-tool-build
  go-toolchain go-build go-build-dsofault go-vet go-vet-dockerint go-vet-dsofault go-test-short go-test-race-short installer-tests
  image-build image-content-inspection
  secret-tmpfs-injection secret-nonroot-access health-probe-behaviour network-bind-protocol
  compose-stdin-deploy compose-profiles compose-failure compose-cancellation
  scenario-signal scenario-restart scenario-rolling scenario-traffic
  scenario-recovery-realdaemon scenario-crash-recovery scenario-ownership-concurrency scenario-inject scenario-n19-diagnostic docker-suite-coverage
  host-listeners
  evidence-collection cleanup foreign-resources-post source-tree-unchanged evidence-leak-scan
)
# Not automatable: recorded as EXTERNAL_PREREQUISITE by finalize; they never affect automated_integration.
EXTERNAL_STEPS=(external-provisioning-provenance external-network-perimeter external-vm-teardown)

# Exact top-level tests per scenario (the -run patterns AND the coverage check derive from these).
SIGNAL_TESTS=(TestSignalRotation_FileMode_ApplicationLoadsNewValue TestSignalRotation_FileMode_IgnoredSignalIsNotAdoption)
RESTART_TESTS=(TestS2_EnvSecretInjection)
ROLLING_TESTS=(TestRotation_BlueGreenSwap_Success TestRotation_FileMode_SecretNeverInDockerInspect TestRotation_Concurrent_MultipleContainers
  TestRotation_HealthCheck_Timeout TestRotation_HealthCheck_Timeout_FailingHealthcheckRollsBack TestRotation_EventDriven_Detection)
TRAFFIC_TESTS=(TestTraffic_SteadyStateControl TestTraffic_RollingRotation TestTraffic_RestartRotation TestTraffic_CandidateReadinessFailure TestTraffic_UnsupportedDirectPublishing)
RECOVERY_TESTS=(TestRecovery_RealDaemon_SwapNotYetCommitted_KeepsOriginalRemovesCandidate TestRecovery_RealDaemon_SwapCompleted_KeepsPromotedRemovesBackup TestRecovery_RealDaemon_BetweenRenames_RemovesNothing)
OWNERSHIP_TESTS=(TestS1_ComposeServiceAdoption TestShadowClone_DoesNotDuplicateNetworkIdentity TestRace_ContainerClone_ConcurrentMutations
  TestRace_ContainerRename_AtomicSwap TestRace_SocketPermission_Concurrent TestNotificationIntegration_RealRotationDeliversWebhook TestStress_ConcurrentRotations)
# `dso inject` security scenarios (test/docker/inject_security_test.go; audit 38): all 18 must run and pass.
INJECT_TESTS=(TestInjectSecurity_01_ManagedContainerVerifiedTmpfs TestInjectSecurity_02_UnmanagedContainerExplicitTarget
  TestInjectSecurity_03_MissingDestination TestInjectSecurity_04_WritableLayerDestinationRefused
  TestInjectSecurity_05_BindMountDestinationRefused TestInjectSecurity_06_NamedVolumeDestinationRefused
  TestInjectSecurity_07_StoppedContainerRefused TestInjectSecurity_08_WrongContainerIdentity
  TestInjectSecurity_09_UnrelatedWorkload TestInjectSecurity_10_SymlinkDestination
  TestInjectSecurity_11_UnsafeDestinationPath TestInjectSecurity_12_NonRootFileAccess
  TestInjectSecurity_13_RestrictivePermissions TestInjectSecurity_14_PartialInjectionFailure
  TestInjectSecurity_15_RepeatedInjection TestInjectSecurity_16_NoPlaintextInPersistentLocations
  TestInjectSecurity_17_NoSecretInLogsOrMetadata TestInjectSecurity_18_PatchedEngineNormalBehaviour)
# N19 diagnostic (test/docker/n19_diag_*_test.go; docs/audit/42): every one must RUN (not skip) and record evidence.
# Sections (see the map in test/docker/n19_diag_helpers_test.go): LEGACY ARCHIVE-BEHAVIOUR diagnostics A B C D E F H I J R
# (the Docker archive endpoint; the managed path no longer uses it), GATED-DELIVERY FUNCTIONAL K L M (the real gated
# lifecycle: gate, exec delivery, journalled evidence, release), RUNTIME MOUNT VERIFICATION D G. Crash/recovery,
# restart re-delivery and secret-version reconciliation have NO guarded scenario here (Docker-free tests only). All 14 are
# kept; vetting them (-tags dockerint) compiles them and proves nothing about execution. The N19 premise stays UNVERIFIED
# until this step has really run.
# The tests record observations, they do not assert the hoped-for outcome: a demonstrated N19 failure is a PASSING
# diagnostic; the verdict (n19_verdict) is computed from the evidence by m0_n19_verdict.
N19_DIAG_TESTS=(TestN19Diag_A_CreatedNotStartedWithTmpfs TestN19Diag_B_ArchiveInjectBeforeStart TestN19Diag_C_LayerInspectionWithoutStart
  TestN19Diag_D_StartAndInspectTmpfs TestN19Diag_E_ApplicationReadsSecret TestN19Diag_F_ArchiveInjectIntoRunning
  TestN19Diag_G_ExecInjectRequireTmpfs TestN19Diag_H_StopRestartBehaviour TestN19Diag_I_PersistentPlaintextBeneathTmpfs
  TestN19Diag_J_RemovalAndScopedCleanup TestN19Diag_K_ManagedRollingFileRotation TestN19Diag_L_ManagedRecreateFileRotation
  TestN19Diag_M_ReadinessGateWithAbsentFile TestN19Diag_R_MinimalReproducer)
CRASH_PATTERN='^TestCrash'
CRASH_TAGS="${DSO_IT_CRASH_TAGS:-dockerint}"
CRASH_MIN_TESTS="${DSO_IT_CRASH_MIN_TESTS:-1}"
S3_TEST=TestS3_RotationStrategies

alt() { local IFS='|'; echo "^($*)\$"; }

# ---------------------------------------------------------------------------
# Pre-flight refusals (no artifacts, no Docker): exit 3.
# ---------------------------------------------------------------------------
for tool in git jq go timeout sha256sum tar awk sed ss; do
  command -v "$tool" >/dev/null 2>&1 || refuse "required tool '$tool' is not installed"
done
unset DSO_INTEGRATION_TESTS DSO_TEST_RUN_ID DSO_TEST_DAEMON_ID DSO_TEST_ENVIRONMENT_ID || true
m0_check_no_docker_env || refuse "ambient Docker/Compose configuration"
m0_resolve_source "$ROOT" || refuse "source revision/provenance"
m0_check_marker "$ENV_FILE" root || refuse "environment marker check failed"
M0_MARKER_OK=ok
M0_ENDPOINT="${DSO_TEST_DOCKER_HOST:-unix:///var/run/docker.sock}"
m0_check_endpoint "$M0_ENDPOINT" || refuse "docker endpoint"

# ---------------------------------------------------------------------------
# Run identity and directories.
# ---------------------------------------------------------------------------
M0_STARTED="$(m0_now)"
M0_RUN_DIR="m0-$(date -u +%Y%m%dT%H%M%SZ)-${M0_SHA:0:8}"
M0_ART="${DSO_IT_ARTIFACTS:-$ROOT/artifacts/$M0_RUN_DIR}"
M0_LOGS="$M0_ART/logs"
M0_STATE="$M0_ART/state"
mkdir -p "$M0_LOGS" "$M0_STATE" "$M0_ART/evidence"
M0_STEPS_FILE="$M0_STATE/steps.tsv"
: >"$M0_STEPS_FILE"
: >"$M0_STATE/findings.txt"
echo '[]' >"$M0_STATE/traffic.json"
echo '[]' >"$M0_STATE/recovery.json"
M0_WORK="$(mktemp -d /var/tmp/dso-m0.XXXXXX)"
chmod 700 "$M0_WORK"

M0_SENTINEL="dso-it-sentinel-$(head -c 8 /dev/urandom | od -An -tx1 | tr -d ' \n')"
export DSO_IT_SENTINEL="$M0_SENTINEL"
DSO_TEST_RUN_ID="dso-it-$(head -c 6 /dev/urandom | od -An -tx1 | tr -d ' \n')"
export DSO_TEST_RUN_ID
export DSO_TEST_DOCKER_HOST="$M0_ENDPOINT"
export DSO_TEST_ENVIRONMENT_ID="$M0_ENV_ID"
export DSO_TEST_DAEMON_ID="$M0_DAEMON_ID"
export GOTOOLCHAIN=local
export DOCKER_CLI_HINTS=false
RUN="$DSO_TEST_RUN_ID"
IMAGE="dso-it-image:$RUN"
LBL=(--label "dso.test.environment=$M0_ENV_ID" --label "dso.test.run=$RUN")
FINALIZED=0
M0_LAST_STATUS=""

echo "run: $RUN  artifacts: $M0_ART"

wait_until() { local t=$1 end; shift; end=$((SECONDS + t)); until "$@"; do ((SECONDS < end)) || return 1; sleep 1; done; }
is_running() { [[ "$(dk inspect --format '{{.State.Running}}' "$1")" == true ]]; }

# ---------------------------------------------------------------------------
# Step bodies (each runs in its own errexit subshell via m0_step; communicate via files).
# ---------------------------------------------------------------------------
s_environment_identity() {
  m0_check_marker "$ENV_FILE" root
  echo "environment id: $M0_ENV_ID"
  echo "recorded daemon id: $M0_DAEMON_ID"
  # Independent of the Docker API: the daemon configuration provisioned by the bootstrap.
  [[ -f /etc/docker/daemon.json && "$(stat -c '%U' /etc/docker/daemon.json)" == root ]] || { echo "daemon.json missing or not root-owned"; return 1; }
  grep -qF "dso.test.environment=${M0_ENV_ID}" /etc/docker/daemon.json || { echo "daemon.json lacks the environment label"; return 1; }
  [[ "${DSO_IT_DISPOSABLE_VM_CONFIRM:-}" == "$M0_ENV_ID" ]] || {
    echo "set DSO_IT_DISPOSABLE_VM_CONFIRM=<environment id from $ENV_FILE> to confirm this host is the disposable VM"
    return 1
  }
}
s_docker_endpoint() {
  m0_check_no_docker_env
  m0_check_endpoint "$M0_ENDPOINT"
  [[ -S ${M0_ENDPOINT#unix://} ]] || { echo "no socket at ${M0_ENDPOINT#unix://}"; return 1; }
  dk version --format 'client={{.Client.Version}} server={{.Server.Version}} api={{.Server.APIVersion}}'
  dk compose version --short
  if [[ -n ${DSO_IT_EXPECT_DOCKER_VERSION:-} ]]; then
    [[ "$(dk version --format '{{.Server.Version}}')" == "$DSO_IT_EXPECT_DOCKER_VERSION" ]] || { echo "docker server version != $DSO_IT_EXPECT_DOCKER_VERSION"; return 1; }
  fi
}
s_daemon_identity() {
  local id p ids=${DSO_TEST_PROHIBITED_DAEMON_IDS:-}
  id="$(dk info --format '{{.ID}}')"
  [[ -n $id && $id == "$M0_DAEMON_ID" ]] || { echo "daemon ID does not match the provisioned marker"; return 1; }
  dk info --format '{{json .Labels}}' | grep -qF "dso.test.environment=${M0_ENV_ID}" || { echo "daemon lacks the environment label"; return 1; }
  for p in ${ids//,/ }; do [[ $id != "$p" ]] || { echo "daemon ID is prohibited"; return 1; }; done
  echo "daemon identity verified: id=$id label=dso.test.environment=$M0_ENV_ID"
  : >"$M0_STATE/daemon_verified"
}
s_foreign_pre() {
  foreign_snapshot >"$M0_STATE/foreign.pre"
  if [[ -s $M0_STATE/foreign.pre ]]; then echo "foreign (unlabelled) resources exist: not a dedicated daemon"; cat "$M0_STATE/foreign.pre"; return 1; fi
  echo "no foreign containers, networks or volumes"
  echo "containers of other runs in this environment: $(dk ps -aq --filter "label=dso.test.environment=$M0_ENV_ID" | wc -l)"
}
s_run_identity() {
  m0_check_run_id "$RUN"
  [[ -z "$(dk ps -aq --filter "label=dso.test.run=$RUN")" ]] || { echo "run id already has resources"; return 1; }
  echo "run id: $RUN  image tag: $IMAGE  sentinel: <redacted>"
}
s_source_revision() {
  m0_check_sha "$M0_SHA" "${DSO_IT_EXPECT_SHA:-}"
  echo "source mode: $M0_SOURCE_MODE  sha: $M0_SHA  tree: $M0_TREE"
  case "$M0_SOURCE_MODE" in
    git)
      [[ "$(git rev-parse HEAD)" == "$M0_SHA" ]]
      m0_check_clean_tree "$ROOT"
      git log -1 --format='commit %H%nauthor %an%ndate %cI%nsubject %s'
      ;;
    provenance)
      m0_check_source_provenance "$DSO_IT_SOURCE_PROVENANCE" "$ROOT" "${DSO_IT_EXPECT_SHA:-}"
      cp -- "$DSO_IT_SOURCE_PROVENANCE" "$M0_ART/evidence/source-provenance.json"
      echo "no .git: tree hash recomputed from the extracted files matches the archive provenance (provenance sha256 $M0_PROV_SHA256)"
      ;;
    *) echo "unknown source mode"; return 1 ;;
  esac
  echo "tree clean"
}
s_cleanup_tool_build() {
  go build -o "$M0_WORK/cleanup" ./test/dockerguard/cmd/cleanup
  go build -o "$M0_WORK/composedrv" ./scripts/integration/composedrv
}
s_go_toolchain() {
  local want have
  want="$(awk '$1=="toolchain" {print $2}' go.mod)"
  have="$(go version)"
  echo "$have (go.mod toolchain: ${want:-none})"
  [[ -n $want && $have == *"$want "* ]] || { echo "installed Go does not match the go.mod toolchain (GOTOOLCHAIN=local)"; return 1; }
}
s_go_build_dsofault() { go build -tags dsofault -o "$M0_WORK/dso-dsofault" ./cmd/dso; }

# gotest_step NAME ALLOW_SKIP MIN "REQUIRED..." GO_TEST_ARGS...   (optional: GOTEST_POST=<fn>)
gotest_step() {
  local name=$1 allow=$2 min=$3 req=$4 start end rc cls status np nf ns reason post=""
  shift 4
  local json="$M0_LOGS/$name.json"
  start=$(m0_now)
  set +e
  timeout "${DSO_IT_GOTEST_TIMEOUT_S:-2700}" go test "$@" -json -count=1 >"$json" 2>"$M0_LOGS/$name.err"
  rc=$?
  set -e
  end=$(m0_now)
  # shellcheck disable=SC2086
  cls="$(m0_classify_go_json "$json" "$rc" "$allow" "$min" $req)"
  IFS=$'\t' read -r status np nf ns reason <<<"$cls"
  if [[ $status == passed && -n ${GOTEST_POST:-} ]]; then
    if ! post="$("$GOTEST_POST" "$json" 2>&1)"; then status=failed reason="$post"; fi
  fi
  m0_record "$name" "$status" "$rc" "$start" "$end" step "pass=$np fail=$nf skip=$ns ${reason}"
}

# ---- image ---------------------------------------------------------------------
s_image_build() {
  DK_TIMEOUT=2400 dk build "${LBL[@]}" -t "$IMAGE" .
  dk image inspect --format '{{.Id}}' "$IMAGE" >"$M0_STATE/image.id"
  [[ "$(dk image inspect --format '{{index .Config.Labels "dso.test.run"}}' "$IMAGE")" == "$RUN" ]]
}
IMG_CHECK='set -eu
test "$(id -u)" = 10001; test "$(id -un)" = dso-user
d=/usr/local/lib/dso/plugins; cd "$d"
test "$(stat -c "%U:%a" "$d")" = root:755
test -f hashes.txt; test ! -w hashes.txt; test ! -w "$d"
n=0
while IFS= read -r line; do
  case "$line" in "#"*|"") continue ;; esac
  f=${line%%=*}; h=${line#*=}
  test "$(sha256sum "$f" | cut -d" " -f1)" = "$h" || { echo "hash mismatch: $f"; exit 1; }
  n=$((n+1))
done < hashes.txt
c=$(ls dso-provider-* | wc -l)
test "$n" -ge 1; test "$n" = "$c"
test "$(stat -c %U /usr/local/bin/docker-dso)" = root; test ! -w /usr/local/bin/docker-dso
echo "plugin manifest verified for $n plugin(s); running as uid 10001"'
s_image_contents() {
  local W="$M0_WORK/image" user l
  mkdir -p "$W"
  user="$(dk image inspect --format '{{.Config.User}}' "$IMAGE")"
  echo "image Config.User=$user"
  [[ -n $user && $user != root && $user != 0 && $user != 0:* && $user != root:* ]] || { echo "image does not default to a non-root user"; return 1; }
  dk save -o "$W/image.tar" "$IMAGE"
  tar -xOf "$W/image.tar" manifest.json | jq -r '.[0].Layers[]' >"$W/layers.txt"
  [[ -s $W/layers.txt ]] || { echo "no layers found"; return 1; }
  : >"$W/files.txt"
  while IFS= read -r l; do tar -xOf "$W/image.tar" "$l" | tar -tf - >>"$W/files.txt"; done <"$W/layers.txt"
  grep -q 'usr/local/bin/docker-dso$' "$W/files.txt" || { echo "layer listing does not contain the DSO binary: refusing a vacuous inspection"; return 1; }
  sort -u "$W/files.txt" >"$M0_ART/evidence/image-files.txt"
  echo "layers: $(wc -l <"$W/layers.txt")  files: $(sort -u "$W/files.txt" | wc -l)"
  m0_image_offenders <"$W/files.txt" >"$W/offenders.txt"
  if [[ -s $W/offenders.txt ]]; then echo "forbidden content in image layers:"; head -n 50 "$W/offenders.txt"; return 1; fi
  echo "no .git, data/, *.db, key material, .claude-flow, .env, local-secrets or Go sources in any layer"
  dk run --rm "${LBL[@]}" --network none --read-only --entrypoint sh "$IMAGE" -c "$IMG_CHECK"
  dk run --rm "${LBL[@]}" --network none --entrypoint cat "$IMAGE" /usr/local/bin/docker-dso >"$W/docker-dso"
  chmod 0700 "$W/docker-dso"
  go version -m "$W/docker-dso" >"$W/buildinfo.txt"
  if grep -q dsofault "$W/buildinfo.txt"; then echo "the shipped binary was built with the dsofault tag"; return 1; fi
  echo "shipped binary has no dsofault build tag"
  if [[ "$(dk image inspect --format '{{json .Config.Healthcheck}}' "$IMAGE")" == null ]]; then
    finding "DSO image defines no HEALTHCHECK: readiness for rotation of this image is process-only (PE-09)."
  fi
}

# ---- secret tmpfs / non-root ---------------------------------------------------
SECRET_C="${RUN}-secret"
s_secret_tmpfs() {
  local line
  dk run -d --name "$SECRET_C" "${LBL[@]}" --network none --read-only --user 0 \
    --tmpfs /run/secrets/dso:rw,noexec,nosuid,nodev,size=1m,uid=10001,gid=10001,mode=0750 \
    --entrypoint sleep "$IMAGE" 600 >/dev/null
  wait_until 30 is_running "$SECRET_C"
  printf '%s' "$M0_SENTINEL" | dk exec -i -u 0 "$SECRET_C" sh -c \
    'umask 077; cat > /run/secrets/dso/value; chown 10001:10001 /run/secrets/dso/value; chmod 0400 /run/secrets/dso/value'
  line="$(dk exec "$SECRET_C" grep -E ' /run/secrets/dso .* - tmpfs ' /proc/self/mountinfo)"
  [[ -n $line && $line == *nosuid* && $line == *nodev* && $line == *noexec* ]] || { echo "/run/secrets/dso is not a nosuid,nodev,noexec tmpfs mount"; return 1; }
  echo "/run/secrets/dso is a tmpfs mount (nosuid,nodev,noexec)"
  [[ "$(dk exec "$SECRET_C" stat -c '%a %u:%g' /run/secrets/dso/value)" == "400 10001:10001" ]] || { echo "secret file is not 0400 owned by 10001:10001"; return 1; }
  [[ "$(dk exec "$SECRET_C" stat -c '%a %u' /run/secrets/dso)" == "750 10001" ]] || { echo "secret dir is not 0750 owned by 10001"; return 1; }
  echo "secret file is 0400 uid 10001"
  if dk diff "$SECRET_C" | grep -q 'run/secrets/dso/value'; then echo "secret reached the container filesystem layer"; return 1; fi
  if dk inspect "$SECRET_C" | grep -qF "$M0_SENTINEL"; then echo "secret value visible in docker inspect"; return 1; fi
  echo "secret absent from docker diff and docker inspect"
}
s_secret_nonroot() {
  [[ "$(dk exec -u 10001:10001 "$SECRET_C" id -u)" == 10001 ]]
  [[ "$(dk exec -u 10001:10001 "$SECRET_C" cat /run/secrets/dso/value)" == "$M0_SENTINEL" ]] || { echo "uid 10001 could not read the delivered secret"; return 1; }
  echo "uid 10001 reads the delivered secret"
  if dk exec -u 10002:10002 "$SECRET_C" cat /run/secrets/dso/value >/dev/null 2>&1; then echo "an unrelated uid can read the secret"; return 1; fi
  echo "uid 10002 is denied"
  if dk exec -u 10001:10001 "$SECRET_C" sh -c 'echo x >> /run/secrets/dso/value' >/dev/null 2>&1; then echo "the app uid can modify the secret file"; return 1; fi
  echo "the app uid cannot modify the 0400 file"
}

# ---- health probes ---------------------------------------------------------------
hstate() { dk inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$1"; }
hs_is() { [[ "$(hstate "$1")" == "$2" ]]; }
s_health() {
  local c1="${RUN}-hc" c2="${RUN}-nohc"
  dk run -d --name "$c1" "${LBL[@]}" --network none --health-cmd 'test -f /tmp/ready' --health-interval 1s \
    --health-timeout 2s --health-retries 1 --health-start-period 0s --entrypoint sleep "$IMAGE" 300 >/dev/null
  wait_until 60 hs_is "$c1" unhealthy || { echo "container with a failing HEALTHCHECK never became unhealthy"; return 1; }
  echo "failing HEALTHCHECK => unhealthy (process running: $(dk inspect --format '{{.State.Running}}' "$c1"))"
  dk exec "$c1" touch /tmp/ready
  wait_until 60 hs_is "$c1" healthy || { echo "container never became healthy after readiness"; return 1; }
  echo "HEALTHCHECK success => healthy"
  dk run -d --name "$c2" "${LBL[@]}" --network none --entrypoint sleep "$IMAGE" 300 >/dev/null
  wait_until 30 is_running "$c2"
  [[ "$(hstate "$c2")" == none ]] || { echo "unexpected health state for a container without HEALTHCHECK"; return 1; }
  echo "no HEALTHCHECK => no health state: only process-running evidence exists"
  finding "Platform check: a container without HEALTHCHECK exposes no readiness signal beyond 'running'; DSO health-gate behaviour on top of this is covered by TestRotation_HealthCheck_Timeout* and TestTraffic_CandidateReadinessFailure."
}

# ---- network bind / protocol -----------------------------------------------------
NET="${RUN}-net"
NET_A="${RUN}-bind"
LISTEN='while :; do echo x | nc -l -s 127.0.0.1 -p 9000 >/dev/null 2>&1 || sleep 1; done & while :; do echo x | nc -l -p 9001 >/dev/null 2>&1 || sleep 1; done & wait'
net_probe() { [[ "$(dk run --rm "${LBL[@]}" --network "$NET" --entrypoint sh "$IMAGE" -c "nc -w 2 $1 $2 </dev/null 2>/dev/null")" == x ]]; }
net_loop() { [[ "$(dk exec "$NET_A" sh -c 'nc -w 2 127.0.0.1 9000 </dev/null 2>/dev/null')" == x ]]; }
host_tcp() {
  local line
  exec 3<>"/dev/tcp/127.0.0.1/$1" || return 1
  read -r -t 3 line <&3 || true
  exec 3<&- 3>&-
  [[ ${line:-} == x ]]
}
s_network() {
  local ip hp i
  dk network create "${LBL[@]}" "$NET" >/dev/null
  dk run -d --name "$NET_A" "${LBL[@]}" --network "$NET" -p 127.0.0.1::9001/tcp --entrypoint sh "$IMAGE" -c "$LISTEN" >/dev/null
  ip="$(dk inspect --format "{{(index .NetworkSettings.Networks \"$NET\").IPAddress}}" "$NET_A")"
  [[ -n $ip ]]
  wait_until 40 net_probe "$ip" 9001 || { echo "control: the 0.0.0.0-bound port is NOT reachable from a second container (probe invalid)"; return 1; }
  echo "control: 0.0.0.0:9001 reachable from a second container/network namespace"
  wait_until 20 net_loop || { echo "the loopback-only listener does not answer on loopback inside its own namespace"; return 1; }
  echo "127.0.0.1:9000 answers inside its own namespace"
  for i in 1 2 3; do
    if net_probe "$ip" 9000; then echo "loopback-only service reachable from a second network namespace"; return 1; fi
  done
  echo "loopback-only 127.0.0.1:9000 is NOT reachable from a second container (3 attempts)"
  hp="$(dk port "$NET_A" 9001/tcp | awk -F: 'NR==1 {print $NF}')"
  [[ $hp =~ ^[0-9]+$ ]] || { echo "no published host port"; return 1; }
  dk port "$NET_A" | grep -q '127\.0\.0\.1:' || { echo "published port is not bound to 127.0.0.1"; return 1; }
  wait_until 20 host_tcp "$hp" || { echo "published TCP port not reachable on host loopback"; return 1; }
  echo "published TCP port reachable on host loopback only"
  if dk port "$NET_A" | grep -qi udp; then echo "a UDP mapping exists"; return 1; fi
  if ss -H -uln | awk '{print $4}' | grep -qE "[:.]${hp}\$"; then echo "a UDP socket is bound on the published host port: UDP is proxied"; return 1; fi
  echo "UDP: no mapping and no UDP listener on host port $hp (a TCP publish does not proxy UDP)"
  finding "Network checks are platform-semantics checks with synthetic nc listeners; DSO's refusal of rolling rotation on directly published ports is covered by TestTraffic_UnsupportedDirectPublishing."
}

# ---- compose -----------------------------------------------------------------------
cmp_yaml() { # MODE: base|fail
  local entry='["sleep","300"]'
  [[ $1 == fail ]] && entry='["sh","-c","exit 3"]'
  cat <<EOF
services:
  web:
    image: \${IT_IMAGE}
    pull_policy: never
    entrypoint: $entry
    environment:
      IT_PROBE: \${IT_VALUE}
    volumes:
      - ./data:/data:ro
    labels:
      dso.test.environment: \${IT_ENV}
      dso.test.run: \${IT_RUN}
  extra:
    profiles: ["extra"]
    image: \${IT_IMAGE}
    pull_policy: never
    entrypoint: ["sleep","300"]
    labels:
      dso.test.environment: \${IT_ENV}
      dso.test.run: \${IT_RUN}
networks:
  default:
    labels:
      dso.test.environment: \${IT_ENV}
      dso.test.run: \${IT_RUN}
EOF
}
cmp_prepare() { # NAME -> prints project dir
  local d="$M0_WORK/compose/$1"
  mkdir -p "$d/data" "$M0_WORK/drvtmp"
  (
    umask 077
    printf 'IT_IMAGE=%s\nIT_VALUE=%s\nIT_ENV=%s\nIT_RUN=%s\n' "$IMAGE" "$M0_SENTINEL" "$M0_ENV_ID" "$RUN" >"$d/.env"
  )
  echo "$d"
}
cmp_drv() { # PROJECT DIR MODE [driver args...]   (the YAML goes to the driver's stdin; nothing is written to disk)
  local p=$1 d=$2 mode=$3
  shift 3
  cmp_yaml "$mode" | TMPDIR="$M0_WORK/drvtmp" DOCKER_HOST="$M0_ENDPOINT" "$M0_WORK/composedrv" -project "$p" -dir "$d" "$@"
}
cmp_names() { dk ps -a --filter "label=com.docker.compose.project=$1" --format '{{.Label "com.docker.compose.service"}}' | sort | tr '\n' ' '; }
s_compose_deploy() {
  local p="${RUN}-cmp" d id
  d="$(cmp_prepare cmp)"
  cmp_drv "$p" "$d" base -arg -d -arg --wait
  [[ "$(cmp_names "$p")" == "web " ]] || { echo "unexpected services (profiles not honoured by default): '$(cmp_names "$p")'"; return 1; }
  id="$(dk ps -q --filter "label=com.docker.compose.project=$p")"
  [[ "$(dk inspect --format '{{index .Config.Labels "com.docker.compose.project"}}' "$id")" == "$p" ]] || { echo "project name not applied"; return 1; }
  [[ "$(dk inspect --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$id")" == "$d" ]] || { echo "project directory not applied"; return 1; }
  [[ "$(dk inspect --format '{{range .Mounts}}{{.Source}}{{end}}' "$id")" == "$d/data" ]] || { echo "relative bind path not resolved against the project directory"; return 1; }
  [[ "$(dk exec "$id" printenv IT_PROBE)" == "$M0_SENTINEL" ]] || { echo ".env interpolation did not reach the container"; return 1; }
  echo "stdin compose: project name, project dir, relative ./data bind, and .env interpolation verified"
  if find "$M0_WORK/drvtmp" /tmp /var/tmp -maxdepth 1 -name 'docker-compose-dso-*' 2>/dev/null | grep -q .; then echo "temporary compose file left behind"; return 1; fi
  if grep -rlF -- "$M0_SENTINEL" "$M0_WORK/drvtmp" 2>/dev/null | grep -q .; then echo "resolved secret written under the process TMPDIR"; return 1; fi
  echo "no temp compose file and no secret under the process TMPDIR"
}
s_compose_profiles() {
  local p="${RUN}-cmp" d="$M0_WORK/compose/cmp"
  [[ -d $d ]] || { echo "compose-stdin-deploy did not prepare the project"; return 1; }
  COMPOSE_PROFILES=extra cmp_drv "$p" "$d" base -arg -d -arg --wait
  [[ "$(cmp_names "$p")" == "extra web " ]] || { echo "profile 'extra' not applied: '$(cmp_names "$p")'"; return 1; }
  echo "COMPOSE_PROFILES=extra enabled the profiled service"
}
s_compose_failure() {
  local p="${RUN}-cmpfail" d rc=0
  d="$(cmp_prepare cmpfail)"
  cmp_drv "$p" "$d" fail -arg -d -arg --wait >"$M0_WORK/cmpfail.out" 2>&1 || rc=$?
  [[ $rc -ne 0 ]] || { echo "compose reported success for a service that exits 3"; return 1; }
  echo "failing service => non-zero exit ($rc)"
  if grep -qF -- "$M0_SENTINEL" "$M0_WORK/cmpfail.out"; then echo "failure output leaks the resolved secret"; return 1; fi
  echo "failure output contains no secret"
}
s_compose_cancel() {
  local p="${RUN}-cmpcancel" d pid i y="$M0_WORK/cancel.yaml"
  d="$(cmp_prepare cmpcancel)"
  cmp_yaml base >"$y" # contains ${VAR} placeholders only; the value comes from the project's .env
  TMPDIR="$M0_WORK/drvtmp" DOCKER_HOST="$M0_ENDPOINT" "$M0_WORK/composedrv" -project "$p" -dir "$d" <"$y" >"$M0_WORK/cmpcancel.out" 2>&1 &
  pid=$!
  wait_until 90 test -n "$(dk ps -q --filter "label=com.docker.compose.project=$p" --filter status=running)" || { kill "$pid" 2>/dev/null || true; echo "service never started"; return 1; }
  kill -TERM "$pid"
  for i in $(seq 1 60); do kill -0 "$pid" 2>/dev/null || break; sleep 1; done
  if kill -0 "$pid" 2>/dev/null; then kill -KILL "$pid"; echo "compose did not terminate within 60s of SIGTERM"; return 1; fi
  wait "$pid" || true
  [[ -z "$(dk ps -q --filter "label=com.docker.compose.project=$p" --filter status=running)" ]] || { echo "containers still running after cancellation"; return 1; }
  echo "SIGTERM cancelled compose up and its containers were stopped"
}

# ---- docker scenarios -----------------------------------------------------------------
parse_traffic_to_state() {
  m0_parse_traffic "$1" >"$M0_STATE/traffic.json"
  m0_output_lines "$1" | grep -E 'TRAFFIC REPORT|^\[(pre|during|post)\]|^rotation phase' >"$M0_ART/evidence/traffic-report.txt" || true
}
post_traffic() {
  parse_traffic_to_state "$1"
  jq -e 'length >= 5 and all(.[]; (.phases | length) == 3 and ([.phases[] | .requests] | all(type=="number")) and ([.phases[] | .requests] | add) > 0)' "$M0_STATE/traffic.json" >/dev/null ||
    { echo "TRAFFIC REPORT lines missing/incomplete (need >=5 reports, 3 phases each, >0 requests)"; return 1; }
}
post_crash() {
  m0_parse_recovery "$1" "$M0_LOGS/scenario-recovery-realdaemon.json" >"$M0_STATE/recovery.json" || echo '[]' >"$M0_STATE/recovery.json"
  if [[ "$(jq length "$M0_STATE/recovery.json")" -eq 0 ]]; then
    finding "No recovery-measurement log lines (t.Logf containing 'recover') were found in the crash/recovery output; recovery timings are therefore not in summary.json."
  fi
}
# n19_collect GO_JSON -- extract the N19-EVIDENCE lines to evidence/n19-evidence.jsonl, validate them, compute the
# n19_verdict FINDING and publish it (state/n19_verdict, state/n19.json, findings). Returns 1 when the evidence is
# missing or malformed (the verdict is then `inconclusive`, never a pass).
N19_EVIDENCE="$M0_ART/evidence/n19-evidence.jsonl"
n19_collect() {
  local json=$1 verdict note valid=true rc=0
  m0_n19_extract "$json" >"$N19_EVIDENCE" || true
  note="$(m0_n19_validate "$N19_EVIDENCE")" || { valid=false rc=1; }
  verdict="$(m0_n19_verdict "$N19_EVIDENCE")"
  [[ $valid == true ]] || verdict=inconclusive
  echo "$verdict" >"$M0_STATE/n19_verdict"
  m0_n19_summary "$N19_EVIDENCE" "$verdict" "$valid" "$note" >"$M0_STATE/n19.json"
  finding "n19_verdict: $verdict"
  case "$verdict" in
    confirmed) finding "N19 CONFIRMED by runtime evidence (see evidence/n19-evidence.jsonl): file-mode managed delivery by archive injection into a created-not-started container leaves the secret in the writable layer and/or hides it behind the tmpfs. release_readiness stays INCOMPLETE until this is fixed and re-verified." ;;
    not_confirmed) finding "N19 not confirmed by this diagnostic run (canary present in the running tmpfs and absent from the layer); this is evidence for one Engine/storage driver only." ;;
    *) finding "N19 INCONCLUSIVE ($note): release_readiness stays INCOMPLETE." ;;
  esac
  echo "$note"
  echo "n19_verdict: $verdict"
  return $rc
}
post_n19() { n19_collect "$1"; }
s_listeners() {
  local allowed=" ${DSO_IT_ALLOWED_PORTS:-22} " bad=0 addr port
  while read -r addr; do
    port="${addr##*:}"
    case "$addr" in 127.* | "[::1]"* | "::1"*) continue ;; esac
    [[ $allowed == *" $port "* ]] && continue
    echo "unexpected non-loopback TCP listener: $addr"
    bad=1
  done < <(ss -H -tln | awk '{print $4}')
  return $bad
}
s_coverage() {
  local list="$M0_WORK/test-list.txt" covered=() t
  env -u DSO_INTEGRATION_TESTS go test -tags "$CRASH_TAGS" -list '^Test' ./test/docker >"$list"
  for t in "${SIGNAL_TESTS[@]}" "${RESTART_TESTS[@]}" "${ROLLING_TESTS[@]}" "${OWNERSHIP_TESTS[@]}" "${INJECT_TESTS[@]}" "$S3_TEST"; do covered+=("^${t}\$"); done
  covered+=('^TestTraffic_' '^TestRecovery_RealDaemon_' '^TestCrash' '^TestN19Diag_')
  echo "tests listed in ./test/docker: $(grep -c '^Test' "$list")"
  t="$(m0_uncovered "$list" "${covered[@]}")"
  if [[ -n $t ]]; then echo "tests not executed by any step:"; echo "$t"; return 1; fi
  echo "every test in ./test/docker is executed by a step"
}

trap finalize EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

gate() { # NAME FUNC -- abort the run (finalize still runs) when a precondition fails
  m0_step "$@"
  [[ $M0_LAST_STATUS == passed ]] || { echo "gate '$1' failed: aborting the run" >&2; exit 1; }
}

# ---------------------------------------------------------------------------
# The run.
# ---------------------------------------------------------------------------
gate environment-identity s_environment_identity
gate docker-endpoint s_docker_endpoint
gate daemon-identity s_daemon_identity
gate foreign-resources-pre s_foreign_pre
gate run-identity s_run_identity
gate source-revision s_source_revision
gate cleanup-tool-build s_cleanup_tool_build

m0_step go-toolchain s_go_toolchain
m0_step go-build go build ./...
m0_step go-build-dsofault s_go_build_dsofault
m0_step go-vet go vet ./...
m0_step go-vet-dockerint go vet -tags dockerint ./...
m0_step go-vet-dsofault go vet -tags 'dockerint dsofault' ./...
gotest_step go-test-short 1 1 "" -short ./...
gotest_step go-test-race-short 1 1 "" -race -short ./...
# installer tests need cosign; a missing prerequisite is a failure, never a silent skip.
if command -v cosign >/dev/null 2>&1; then
  gotest_step installer-tests 0 1 "" ./test/installer
else
  m0_record installer-tests failed 127 "$(m0_now)" "$(m0_now)" step "cosign is not installed (installer tests require it)"
fi

m0_step image-build s_image_build
m0_step image-content-inspection s_image_contents
m0_step secret-tmpfs-injection s_secret_tmpfs
m0_step secret-nonroot-access s_secret_nonroot
m0_step health-probe-behaviour s_health
m0_step network-bind-protocol s_network
m0_step compose-stdin-deploy s_compose_deploy
m0_step compose-profiles s_compose_profiles
m0_step compose-failure s_compose_failure
m0_step compose-cancellation s_compose_cancel

# Guarded scenarios (test/dockerguard re-verifies the daemon identity in TestMain; it FAILS, not skips, on a wrong daemon).
export DSO_INTEGRATION_TESTS=1
SIG_REQ="$(printf '%s ' "${SIGNAL_TESTS[@]}")${S3_TEST}/signal-rejects-env-secret"
gotest_step scenario-signal 0 3 "$SIG_REQ" -tags dockerint -run "$(alt "${SIGNAL_TESTS[@]}" "$S3_TEST")/^signal-rejects-env-secret\$" ./test/docker
RST_REQ="$(printf '%s ' "${RESTART_TESTS[@]}")${S3_TEST}/restart"
gotest_step scenario-restart 0 2 "$RST_REQ" -tags dockerint -run "$(alt "${RESTART_TESTS[@]}" "$S3_TEST")/^restart\$" ./test/docker
RLL_REQ="$(printf '%s ' "${ROLLING_TESTS[@]}")${S3_TEST}/rolling"
gotest_step scenario-rolling 0 7 "$RLL_REQ" -tags dockerint -run "$(alt "${ROLLING_TESTS[@]}" "$S3_TEST")/^rolling\$" ./test/docker
GOTEST_POST=post_traffic gotest_step scenario-traffic 0 5 "${TRAFFIC_TESTS[*]}" -tags dockerint -run '^TestTraffic_' ./test/docker
gotest_step scenario-recovery-realdaemon 0 3 "${RECOVERY_TESTS[*]}" -tags dockerint -run '^TestRecovery_RealDaemon_' ./test/docker
# Real-process termination + recovery (harness under test/docker; fault hook behind the dsofault tag).
# Zero matched tests, or all skipped, FAILS/INCOMPLETEs this step: it can never pass vacuously.
GOTEST_POST=post_crash gotest_step scenario-crash-recovery 0 "$CRASH_MIN_TESTS" "" -tags "$CRASH_TAGS" -run "$CRASH_PATTERN" ./test/docker
gotest_step scenario-ownership-concurrency 0 "${#OWNERSHIP_TESTS[@]}" "${OWNERSHIP_TESTS[*]}" -tags dockerint -run "$(alt "${OWNERSHIP_TESTS[@]}")" ./test/docker
gotest_step scenario-inject 0 "${#INJECT_TESTS[@]}" "${INJECT_TESTS[*]}" -tags dockerint -run "$(alt "${INJECT_TESTS[@]}")" ./test/docker
# N19 diagnostic: its own step; all named tests must RUN and the evidence lines must be captured. A demonstrated N19
# failure does NOT fail this step (the verdict is a finding and keeps release_readiness INCOMPLETE).
GOTEST_POST=post_n19 gotest_step scenario-n19-diagnostic 0 "${#N19_DIAG_TESTS[@]}" "${N19_DIAG_TESTS[*]}" -tags dockerint -timeout "${DSO_IT_N19_GO_TIMEOUT:-40m}" -run '^TestN19Diag_' ./test/docker
[[ -s $M0_STATE/n19_verdict ]] || n19_collect "$M0_LOGS/scenario-n19-diagnostic.json" >/dev/null 2>&1 || true
unset DSO_INTEGRATION_TESTS
m0_step docker-suite-coverage s_coverage
m0_step host-listeners s_listeners

exit 0
