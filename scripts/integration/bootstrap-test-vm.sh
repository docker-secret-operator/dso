#!/usr/bin/env bash
# bootstrap-test-vm.sh -- provision a DISPOSABLE Linux VM as the designated DSO
# Docker integration-test environment.
#
# DO NOT RUN THIS ON A DEVELOPMENT MACHINE OR ANY HOST THAT RUNS REAL
# WORKLOADS. It reconfigures the Docker daemon (adds an identity label and
# restarts it). It therefore refuses to run unless you explicitly confirm the
# machine is disposable, and refuses if the daemon already hosts any container.
#
# What it establishes, independently of the test suite (the suite cannot forge
# any of it):
#   * the daemon label  dso.test.environment=<environment id>   (daemon.json)
#   * the daemon ID     (docker info) recorded together with the environment id
#     in /etc/dso-integration/environment, owned by root, mode 0644
# test/dockerguard verifies both before any test touches Docker.
#
# Usage (as root, on the VM):
#   DSO_BOOTSTRAP_DISPOSABLE_VM=yes ./bootstrap-test-vm.sh
#   DSO_BOOTSTRAP_DISPOSABLE_VM=yes ./bootstrap-test-vm.sh --install-docker   # Ubuntu 24.04 only
#       requires DOCKER_APT_VERSION (exact apt version string; no default, no "latest")
#   ... --ci-ephemeral   # GitHub-hosted runner only (GITHUB_ACTIONS=true, RUNNER_ENVIRONMENT=github-hosted)
set -euo pipefail

INSTALL_DOCKER=0
CI_EPHEMERAL=0
for a in "$@"; do
  case "$a" in
    --install-docker) INSTALL_DOCKER=1 ;;
    --ci-ephemeral)   CI_EPHEMERAL=1 ;;
    *) echo "unknown argument: $a" >&2; exit 2 ;;
  esac
done

die() { echo "bootstrap refused: $*" >&2; exit 1; }

[[ "${DSO_BOOTSTRAP_DISPOSABLE_VM:-}" == "yes" ]] || die "set DSO_BOOTSTRAP_DISPOSABLE_VM=yes to confirm this machine is a disposable test VM"
[[ $EUID -eq 0 ]] || die "must run as root on the disposable VM"
if [[ $CI_EPHEMERAL -eq 1 ]]; then
  [[ "${GITHUB_ACTIONS:-}" == "true" && "${RUNNER_ENVIRONMENT:-}" == "github-hosted" ]] \
    || die "--ci-ephemeral is only valid on a GitHub-hosted runner"
fi

if [[ $INSTALL_DOCKER -eq 1 ]]; then
  [[ -n "${DOCKER_APT_VERSION:-}" ]] || die "DOCKER_APT_VERSION must pin the exact docker-ce apt version"
  . /etc/os-release
  [[ "${ID}" == "ubuntu" && "${VERSION_ID}" == "24.04" ]] || die "--install-docker supports Ubuntu 24.04 only"
  # Docker's apt signing key; fingerprint must match before it is trusted.
  expected_fpr="9DC858229FC7DD38854AE2D88D81803C0EBFCD88"
  install -m 0755 -d /etc/apt/keyrings
  tmp="$(mktemp)"; trap 'rm -f "$tmp"' EXIT
  curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o "$tmp"
  actual_fpr="$(gpg --show-keys --with-colons "$tmp" | awk -F: '/^fpr:/ {print $10; exit}')"
  [[ "$actual_fpr" == "$expected_fpr" ]] || die "Docker apt key fingerprint mismatch"
  gpg --dearmor <"$tmp" >/etc/apt/keyrings/docker.gpg
  chmod a+r /etc/apt/keyrings/docker.gpg
  echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu ${VERSION_CODENAME} stable" >/etc/apt/sources.list.d/docker.list
  apt-get update -y
  apt-get install -y "docker-ce=${DOCKER_APT_VERSION}" "docker-ce-cli=${DOCKER_APT_VERSION}" containerd.io docker-compose-plugin
fi

command -v docker >/dev/null || die "docker is not installed (use --install-docker or install it first)"

# Fail closed: a dedicated test daemon has no pre-existing containers. If the daemon cannot
# be queried at all the host is NOT assumed empty (an unreachable daemon must not read as "no containers").
existing="$(docker ps -aq)" || die "cannot query the Docker daemon (is it running and reachable?)"
if [[ -n "$existing" ]]; then
  die "this Docker daemon already hosts containers: not a dedicated test VM"
fi
# Never replace an existing provisioning marker (a second bootstrap would orphan the first environment id).
if [[ -e /etc/dso-integration/environment ]]; then
  die "/etc/dso-integration/environment already exists: this VM is already provisioned (destroy and recreate it)"
fi
# Never merge into or overwrite an existing daemon configuration.
if [[ -s /etc/docker/daemon.json ]]; then
  die "/etc/docker/daemon.json already exists and is non-empty: refusing to overwrite"
fi

env_id="env-$(head -c 8 /dev/urandom | od -An -tx1 | tr -d ' \n')"
install -m 0755 -d /etc/docker
umask 022
cat >/etc/docker/daemon.json <<JSON
{
  "labels": ["dso.test.environment=${env_id}"]
}
JSON
systemctl restart docker

daemon_id="$(docker info --format '{{.ID}}')"
[[ -n "$daemon_id" ]] || die "daemon reported no ID after restart"
docker info --format '{{json .Labels}}' | grep -q "dso.test.environment=${env_id}" || die "daemon label not applied"

install -m 0755 -d /etc/dso-integration
umask 022
cat >/etc/dso-integration/environment <<EOF
DSO_TEST_ENVIRONMENT_ID=${env_id}
DSO_TEST_DAEMON_ID=${daemon_id}
EOF
chown root:root /etc/dso-integration/environment
chmod 0644 /etc/dso-integration/environment

echo "Test environment ${env_id} provisioned. Daemon ID recorded in /etc/dso-integration/environment."
echo "Add the test user to the docker group, then run scripts/integration/run-all.sh (the single entry point; see docs/testing/DISPOSABLE-VM-EXECUTION-PLAN.md) as that user."
