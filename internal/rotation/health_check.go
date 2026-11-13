package rotation

import (
	"context"
	"errors"
	"fmt"
	"path"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// ErrUnhealthy classifies a readiness rejection of a container: it restarted,
// exited, reported unhealthy, never became healthy/stable within the timeout,
// or (ExecProbe) a file-mode secret was still missing after every retry.
// WaitHealthy and ExecProbe wrap it on exactly those paths; Docker API errors
// and context cancellation are NOT wrapped, so errors.Is(err, ErrUnhealthy)
// means "the container ran and was rejected", not "something went wrong".
var ErrUnhealthy = errors.New("rotation: container failed health verification")

// Readiness is the strongest evidence a readiness gate obtained.
type Readiness int

const (
	// ReadinessNone: the gate did not succeed.
	ReadinessNone Readiness = iota
	// ReadinessProcessStable: the image defines NO HEALTHCHECK, so all that is
	// known is that the process stayed running for two consecutive polls. This is
	// NOT proof the application accepts traffic or uses the new secret; callers
	// must not describe the result as application readiness (or zero downtime).
	ReadinessProcessStable
	// ReadinessHealthcheck: Docker's own HEALTHCHECK reported "healthy".
	ReadinessHealthcheck
)

func (r Readiness) String() string {
	switch r {
	case ReadinessProcessStable:
		return "process-running-only (no HEALTHCHECK defined)"
	case ReadinessHealthcheck:
		return "docker-healthcheck-healthy"
	default:
		return "none"
	}
}

// WaitHealthy is WaitReady without the evidence level.
func WaitHealthy(ctx context.Context, cli *client.Client, containerID string, timeout time.Duration) error {
	_, err := WaitReady(ctx, cli, containerID, timeout)
	return err
}

// WaitReady monitors the shadow instance before cutover and reports WHICH kind
// of evidence it obtained. It does not return success until the container is
// healthy per Docker's HEALTHCHECK, or -- when no HEALTHCHECK exists -- has stayed
// running for two consecutive polls (ReadinessProcessStable, a weaker claim).
func WaitReady(ctx context.Context, cli *client.Client, containerID string, timeout time.Duration) (Readiness, error) {
	deadline := time.Now().Add(timeout)
	hasHealthCheck := false
	// Tracks consecutive "running" polls when there is no health check defined.
	// We wait for two consecutive clean polls (~4s apart) before declaring healthy,
	// so we don't declare success on a container that immediately crashes.
	stablePolls := 0

	for time.Now().Before(deadline) {
		inspect, err := cli.ContainerInspect(ctx, containerID)
		if err != nil {
			return ReadinessNone, fmt.Errorf("failed to inspect container health: %w", err)
		}

		// CRITICAL: If container is restarting, fail immediately
		if inspect.State.Restarting {
			return ReadinessNone, fmt.Errorf("container is restarting - indicates startup failure: %w", ErrUnhealthy)
		}

		// CRITICAL: If container exited, fail immediately
		if !inspect.State.Running {
			return ReadinessNone, fmt.Errorf("container exited or stopped before becoming healthy: %w", ErrUnhealthy)
		}

		// Check Docker native health check status if present
		if inspect.State.Health != nil {
			hasHealthCheck = true
			switch inspect.State.Health.Status {
			case "healthy":
				return ReadinessHealthcheck, nil
			case "unhealthy":
				return ReadinessNone, fmt.Errorf("container became unhealthy during rotation: %w", ErrUnhealthy)
			case "starting":
				// still waiting...
			}
		} else {
			// No HEALTHCHECK defined: rely on consecutive stable running polls.
			// Two consecutive 2s-apart polls with the container still running is
			// good enough signal that it has not crashed on startup.
			stablePolls++
			if stablePolls >= 2 {
				return ReadinessProcessStable, nil
			}
		}

		select {
		case <-ctx.Done():
			return ReadinessNone, ctx.Err()
		case <-time.After(healthPollInterval):
		}
	}

	// CRITICAL: If we have a health check defined, only accept "healthy" status.
	// (If it had reached "healthy", we'd have returned nil already inside the
	// loop above, so reaching this line at all means it never did.)
	if hasHealthCheck {
		return ReadinessNone, fmt.Errorf("rotation timed out after %v - container has health check but never reached healthy state: %w", timeout, ErrUnhealthy)
	}

	// No health check defined and we never got two consecutive stable polls within timeout
	return ReadinessNone, fmt.Errorf("rotation timed out after %v - container did not reach stable running state: %w", timeout, ErrUnhealthy)
}

// probeScript succeeds only for a regular, non-empty file, or for a directory that
// contains at least one entry where EVERY regular file is non-empty and no entry is
// a leftover temporary (.inject.*) file. A missing path, an empty file, an empty
// directory or a directory holding only subdirectories fails. (The old probe ran
// `test -s <dir>`, which is true for any directory because directories have a
// non-zero size.)
const probeScript = `p=$1
if [ -f "$p" ]; then [ -s "$p" ]; exit $?; fi
[ -d "$p" ] || exit 2
n=0
for f in "$p"/* "$p"/.[!.]*; do
  [ -e "$f" ] || continue
  case "$f" in */.inject.*) exit 3;; esac
  if [ -f "$f" ]; then [ -s "$f" ] || exit 4; n=$((n+1)); fi
done
[ "$n" -gt 0 ] || exit 5
`

// ExecProbe verifies, by running a command inside the container and WAITING for
// it to finish, that path holds delivered secret material (see probeScript). It
// fails closed: success requires the exec to have completed with exit code 0.
// Each attempt is bounded by timeout/retries; a probe that does not finish in time
// counts as a failed attempt. A Docker API error or context cancellation is
// returned as-is (not ErrUnhealthy); a probe rejection after all retries wraps
// ErrUnhealthy. The probe needs /bin/sh in the image; without one it fails.
// Probe output is not attached, so no file content can reach logs.
func ExecProbe(ctx context.Context, cli *client.Client, containerID string, path string, timeout time.Duration, retries int) error {
	if retries <= 0 {
		retries = 3
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	interval := timeout / time.Duration(retries)
	if interval < probeMinAttempt {
		interval = probeMinAttempt
	}
	var last string
	for i := 0; i < retries; i++ {
		done, code, err := runProbeOnce(ctx, cli, containerID, path, interval)
		if err != nil {
			return err
		}
		switch {
		case !done:
			last = "probe did not finish within the attempt timeout"
		case code == 0:
			return nil
		default:
			last = fmt.Sprintf("probe exited %d", code)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(probeRetryPause):
		}
	}
	return fmt.Errorf("exec probe failed for %s after %d retries (%s): %w", path, retries, last, ErrUnhealthy)
}

// ExecProbeFiles probes dir/<name> for every delivered secret file name.
func ExecProbeFiles(ctx context.Context, cli *client.Client, containerID, dir string, names []string, timeout time.Duration, retries int) error {
	if len(names) == 0 {
		return ExecProbe(ctx, cli, containerID, dir, timeout, retries)
	}
	for _, n := range names {
		if err := ExecProbe(ctx, cli, containerID, path.Join(dir, path.Base(n)), timeout, retries); err != nil {
			return err
		}
	}
	return nil
}

var probeRetryPause = 500 * time.Millisecond

// runProbeOnce creates a detached exec, then polls until it is no longer running
// or attemptTimeout elapses.
func runProbeOnce(ctx context.Context, cli *client.Client, containerID, path string, attemptTimeout time.Duration) (done bool, exitCode int, err error) {
	resp, err := cli.ContainerExecCreate(ctx, containerID, container.ExecOptions{
		Cmd:          []string{"/bin/sh", "-c", probeScript, "dso-probe", path},
		AttachStdout: false,
		AttachStderr: false,
	})
	if err != nil {
		return false, 0, err
	}
	if err := cli.ContainerExecStart(ctx, resp.ID, container.ExecStartOptions{Detach: true}); err != nil {
		return false, 0, err
	}
	deadline := time.Now().Add(attemptTimeout)
	for {
		info, err := cli.ContainerExecInspect(ctx, resp.ID)
		if err != nil {
			return false, 0, err
		}
		if !info.Running {
			return true, info.ExitCode, nil
		}
		if !time.Now().Before(deadline) {
			return false, 0, nil
		}
		select {
		case <-ctx.Done():
			return false, 0, ctx.Err()
		case <-time.After(probePollInterval):
		}
	}
}

var (
	probePollInterval  = 50 * time.Millisecond
	healthPollInterval = 2 * time.Second
	probeMinAttempt    = time.Second
)
