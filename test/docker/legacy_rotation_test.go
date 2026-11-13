//go:build dockerint

package docker

// Guarded migration of test/integration/rotation_test.go (M0-11). Every
// container here is created with `docker run` against the verified test daemon,
// carries the run's guard labels plus a per-test dso.it.run label, has a unique
// name, and is removed by label at test end. Rotation clones copy the original's
// labels, so rotation candidates/backups are covered by the same cleanup.
//
// Secret values are synthetic (syntheticValue) and are never printed.
//
// Status: compiled with -tags dockerint; NOT executed (no isolated daemon yet).

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"go.uber.org/zap/zaptest"

	"github.com/docker-secret-operator/dso/internal/rotation"
	"github.com/docker-secret-operator/dso/test/dockerguard"
)

// lgIdleCmd is an idle workload that stops promptly on SIGTERM.
const lgIdleCmd = `trap 'exit 0' TERM; while true; do sleep 1 & wait $!; done`

// lgRun starts one labelled idle container and returns its id and unique name.
// extra are additional `docker run` flags (e.g. health flags, -e).
func lgRun(t *testing.T, prefix, runLabel string, extra ...string) (id, name string) {
	t.Helper()
	name = uniqueName(t, "dso-it-"+prefix)
	args := []string{"run", "-d", "--name", name}
	args = append(args, dockerguard.Current(t).LabelArgs()...)
	args = append(args, "--label", "dso.it.run="+runLabel)
	args = append(args, extra...)
	args = append(args, testImage, "sh", "-c", lgIdleCmd)
	return dockerCmd(t, args...), name
}

// lgNewRun returns a fresh per-test run label with label-scoped cleanup armed.
func lgNewRun(t *testing.T, cli *client.Client) string {
	t.Helper()
	runLabel := uniqueName(t, "run")
	cleanupByLabel(t, cli, "dso.it.run", runLabel)
	return runLabel
}

// lgRotator returns a RollingStrategy for ONE rotation (the production code
// also builds one per rotation: a strategy keeps per-Execute state).
func lgRotator(t *testing.T, cli *client.Client) *rotation.RollingStrategy {
	t.Helper()
	return rotation.NewRollingStrategyWithLogger(cli, zaptest.NewLogger(t))
}

// lgRunningByName returns the inspect of the running container now holding name.
func lgRunningByName(t *testing.T, cli *client.Client, name string) container.InspectResponse {
	t.Helper()
	in, err := cli.ContainerInspect(context.Background(), name)
	if err != nil {
		t.Fatalf("no container named %s: %v", name, err)
	}
	if !in.State.Running {
		t.Fatalf("container %s is not running (status %s)", name, in.State.Status)
	}
	return in
}

// lgEnv returns the container's environment as a map (never printed).
func lgEnv(in container.InspectResponse) map[string]string {
	out := map[string]string{}
	for _, kv := range in.Config.Env {
		k, v, _ := strings.Cut(kv, "=")
		out[k] = v
	}
	return out
}

// lgRunningOwned returns the ids of running containers carrying runLabel.
func lgRunningOwned(t *testing.T, cli *client.Client, runLabel string) []string {
	t.Helper()
	f := filters.NewArgs()
	f.Add("label", "dso.it.run="+runLabel)
	list, err := cli.ContainerList(context.Background(), container.ListOptions{Filters: f})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	ids := make([]string, 0, len(list))
	for _, c := range list {
		ids = append(ids, c.ID)
	}
	return ids
}

// Original: TestRotation_BlueGreenSwap_Success. Original assertion: Execute
// returns nil. Expanded: the name is now held by a NEW running container, the
// original id is gone, and the rotated environment variable reached the new one.
func TestRotation_BlueGreenSwap_Success(t *testing.T) {
	cli := dockerClient(t)
	runLabel := lgNewRun(t, cli)
	id, name := lgRun(t, "bg", runLabel)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := lgRotator(t, cli).Execute(ctx, id, map[string]string{"NEW_ENV_VAR": "test-value"}, nil, nil, 5*time.Second); err != nil {
		t.Fatalf("Rotation failed: %v", err)
	}

	in := lgRunningByName(t, cli, name)
	if in.ID == id {
		t.Fatal("rotation kept the original container")
	}
	assertGone(t, cli, id)
	if got := lgEnv(in)["NEW_ENV_VAR"]; got != "test-value" {
		t.Errorf("rotated environment variable not present on the replacement (got %q)", got)
	}
}

// Original: TestRotation_FileModeSecret_NeverInDockerInspect (P0 secret-exposure
// regression). Sentinel must be absent from the full raw inspect JSON, Config.Env,
// Config.Cmd and Config.Labels, AND present in the container filesystem. Expanded:
// Config.Entrypoint is checked too (the original doc comment listed it but the code
// did not), the original container's inspect is also checked before it is replaced,
// and failure messages never contain the value.
func TestRotation_FileModeSecret_NeverInDockerInspect(t *testing.T) {
	cli := dockerClient(t)
	runLabel := lgNewRun(t, cli)
	id, name := lgRun(t, "filemode", runLabel)
	secret := syntheticValue("inspect-probe")
	has := func(s string) bool { return strings.Contains(s, secret) }

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pre, err := cli.ContainerInspect(ctx, id)
	if err != nil {
		t.Fatalf("inspect before rotation: %v", err)
	}
	if rawPre, _ := json.Marshal(pre); has(string(rawPre)) {
		t.Fatal("SECURITY REGRESSION: secret value present in inspect output BEFORE rotation (test setup bug)")
	}

	fileSecrets := []rotation.FileSecretSpec{
		{Path: "/run/secrets/dso", Data: map[string]string{"db-password": secret}, UID: 0, GID: 0},
	}
	newEnvs := map[string]string{"UNRELATED_ENV": "fine-to-be-visible"}
	if err := lgRotator(t, cli).Execute(ctx, id, newEnvs, fileSecrets, nil, 5*time.Second); err != nil {
		t.Fatalf("Rotation with file-mode secret failed: %v", err)
	}

	// Rotation is an atomic name swap: the original id stops existing and the new
	// container takes the NAME, so look up by name.
	inspect, err := cli.ContainerInspect(ctx, name)
	if err != nil {
		t.Fatalf("inspect rotated container by name: %v", err)
	}
	raw, err := json.Marshal(inspect)
	if err != nil {
		t.Fatalf("marshal inspect: %v", err)
	}
	if has(string(raw)) {
		t.Fatal("SECURITY REGRESSION: secret value found in `docker inspect` output")
	}
	for _, e := range inspect.Config.Env {
		if has(e) {
			k, _, _ := strings.Cut(e, "=")
			t.Fatalf("SECURITY REGRESSION: secret value found in Config.Env entry %q", k)
		}
	}
	for i, c := range inspect.Config.Cmd {
		if has(c) {
			t.Fatalf("SECURITY REGRESSION: secret value found in Config.Cmd[%d]", i)
		}
	}
	for i, c := range inspect.Config.Entrypoint {
		if has(c) {
			t.Fatalf("SECURITY REGRESSION: secret value found in Config.Entrypoint[%d]", i)
		}
	}
	for k, v := range inspect.Config.Labels {
		if has(v) {
			t.Fatalf("SECURITY REGRESSION: secret value found in label %q", k)
		}
	}
	if got := lgEnv(inspect)["UNRELATED_ENV"]; got != "fine-to-be-visible" {
		t.Errorf("non-secret environment variable was not applied (got %q)", got)
	}

	// Functional check: the secret must actually have been delivered.
	out, err := execOut(t, inspect.ID, "cat", "/run/secrets/dso/db-password")
	if err != nil {
		t.Fatalf("reading the delivered secret file: %v", err)
	}
	if !has(out) {
		t.Fatal("file-mode secret was not delivered to the container filesystem (value mismatch; value not printed)")
	}
}

// Original: TestRotation_Concurrent_MultipleContainers. Original assertion: zero
// failures across 3 concurrent rotations. Deviation: each goroutine builds its own
// RollingStrategy (the original shared one, which keeps per-Execute state and is
// not how production uses it). Expanded: each name resolves to a new running
// container carrying its own ROTATION_ID.
func TestRotation_Concurrent_MultipleContainers(t *testing.T) {
	cli := dockerClient(t)
	runLabel := lgNewRun(t, cli)
	const n = 3
	ids, names := make([]string, n), make([]string, n)
	for i := 0; i < n; i++ {
		ids[i], names[i] = lgRun(t, fmt.Sprintf("conc%d", i), runLabel)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	errCh := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			rotCtx, c := context.WithTimeout(ctx, 90*time.Second)
			defer c()
			errCh <- lgRotator(t, cli).Execute(rotCtx, ids[idx], map[string]string{
				"ROTATION_ID": fmt.Sprintf("rotation-%d", idx),
			}, nil, nil, 5*time.Second)
		}(i)
	}
	wg.Wait()
	close(errCh)

	failures := 0
	for err := range errCh {
		if err != nil {
			t.Logf("rotation failed: %v", err)
			failures++
		}
	}
	if failures > 0 {
		t.Fatalf("Expected 0 failures, got %d", failures)
	}
	for i := 0; i < n; i++ {
		in := lgRunningByName(t, cli, names[i])
		if in.ID == ids[i] {
			t.Errorf("container %d was not replaced", i)
		}
		if got := lgEnv(in)["ROTATION_ID"]; got != fmt.Sprintf("rotation-%d", i) {
			t.Errorf("container %d carries the wrong ROTATION_ID %q", i, got)
		}
	}
}

// Original: TestRotation_HealthCheck_Timeout. The original only logged either
// outcome (it asserted nothing); its comment says rotation "should succeed since
// container has no health check". Preserved as a strict assertion of that stated
// expectation (this is the same outcome TestRotation_BlueGreenSwap_Success pins;
// the process-stability path takes ~2 polls).
func TestRotation_HealthCheck_Timeout(t *testing.T) {
	cli := dockerClient(t)
	runLabel := lgNewRun(t, cli)
	id, name := lgRun(t, "nohc", runLabel)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := lgRotator(t, cli).Execute(ctx, id, map[string]string{"NEW_VAR": "value"}, nil, nil, 5*time.Second); err != nil {
		t.Fatalf("rotation of a container without a HEALTHCHECK must succeed on process stability: %v", err)
	}
	if in := lgRunningByName(t, cli, name); in.ID == id {
		t.Error("container without a HEALTHCHECK was not replaced")
	}
}

// NEW (expanded coverage for the same test's stated subject): a candidate that
// never becomes healthy must hit the health timeout, the rotation must fail, the
// original must keep its id, name and running state, and no candidate may remain.
func TestRotation_HealthCheck_Timeout_FailingHealthcheckRollsBack(t *testing.T) {
	cli := dockerClient(t)
	runLabel := lgNewRun(t, cli)
	id, name := lgRun(t, "badhc", runLabel,
		"--health-cmd", "false", "--health-interval", "1s", "--health-retries", "1")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	start := time.Now()
	err := lgRotator(t, cli).Execute(ctx, id, map[string]string{"NEW_VAR": "value"}, nil, nil, 5*time.Second)
	if err == nil {
		t.Fatal("rotation succeeded although the candidate can never become healthy")
	}
	if d := time.Since(start); d > 45*time.Second {
		t.Errorf("health timeout of 5s was not honoured: rotation took %v", d)
	}
	in := lgRunningByName(t, cli, name)
	if in.ID != id {
		t.Errorf("original was replaced by a failed rotation")
	}
	if running := lgRunningOwned(t, cli, runLabel); len(running) != 1 {
		t.Errorf("expected only the original to run after rollback, found %d running containers", len(running))
	}
}

// Original: TestRotation_EventDriven_Detection. Original assertions: container is
// running; ContainerStop works (error only warned); afterwards it is not running.
// Despite its name the original never observed an event. Expanded: a daemon event
// subscription opened BEFORE the stop must deliver the container's die event.
func TestRotation_EventDriven_Detection(t *testing.T) {
	cli := dockerClient(t)
	runLabel := lgNewRun(t, cli)
	id, _ := lgRun(t, "evt", runLabel)

	opCtx, opCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer opCancel()
	in, err := cli.ContainerInspect(opCtx, id)
	if err != nil {
		t.Fatalf("Failed to inspect container: %v", err)
	}
	if !in.State.Running {
		t.Errorf("Container should be running, but state is: %s", in.State.Status)
	}

	evCtx, evCancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer evCancel()
	f := filters.NewArgs()
	f.Add("container", id)
	f.Add("type", string(events.ContainerEventType))
	msgs, errs := cli.Events(evCtx, events.ListOptions{Filters: f})

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 15*time.Second)
	stopTimeoutSec := 10
	if err := cli.ContainerStop(stopCtx, id, container.StopOptions{Timeout: &stopTimeoutSec}); err != nil {
		t.Logf("Warning: ContainerStop returned an error (state is verified below): %v", err)
	}
	stopCancel()

	sawDie := false
	for !sawDie {
		select {
		case m := <-msgs:
			if string(m.Action) == "die" && m.Actor.ID == id {
				sawDie = true
			}
		case err := <-errs:
			t.Fatalf("event stream failed before a die event was seen: %v", err)
		case <-evCtx.Done():
			t.Fatal("no die event observed for the stopped container")
		}
	}

	verifyCtx, verifyCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer verifyCancel()
	in, err = cli.ContainerInspect(verifyCtx, id)
	if err != nil {
		t.Fatalf("Failed to inspect stopped container: %v", err)
	}
	if in.State.Running {
		t.Errorf("Container should be stopped, but is still running")
	}
}
