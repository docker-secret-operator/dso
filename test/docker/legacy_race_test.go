//go:build dockerint

package docker

// Guarded migration of the Docker-dependent tests of test/integration/race_test.go
// (M0-11): TestRace_ContainerClone_ConcurrentMutations and
// TestRace_ContainerRename_AtomicSwap. The other two original tests
// (DebounceWindow_ConcurrentUpdates, SocketPermission_Concurrent) never touched
// Docker; see docs/testing/LEGACY-DOCKER-TESTS.md for where they went
// (SocketPermission_Concurrent was retired here and replaced by Docker-free
// regression tests in internal/agent/socket_permission_race_test.go).
//
// Fixed names ("test-race-base", "atomic-test", "atomic-test_new",
// "atomic-test_backup") and the pre-run `ContainerRemove` of those fixed names are
// gone: names are unique per test and cleanup is scoped by the run label.
//
// Status: compiled with -tags dockerint; NOT executed (no isolated daemon yet).

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"

	"github.com/docker-secret-operator/dso/internal/rotation"
	"github.com/docker-secret-operator/dso/test/dockerguard"
)

// lgCreate creates (does not start) a labelled container through the API with the
// session's guard labels, exactly as the pattern requires for ContainerCreate.
func lgCreate(t *testing.T, runLabel, name string, env []string, extraLabels map[string]string) string {
	t.Helper()
	cli := dockerClient(t)
	labels := dockerguard.Current(t).Labels()
	labels["dso.it.run"] = runLabel
	for k, v := range extraLabels {
		labels[k] = v
	}
	resp, err := cli.ContainerCreate(context.Background(), &container.Config{
		Image:  testImage,
		Cmd:    []string{"sh", "-c", lgIdleCmd},
		Env:    env,
		Labels: labels,
	}, &container.HostConfig{}, &network.NetworkingConfig{}, nil, name)
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	return resp.ID
}

// Original: TestRace_ContainerClone_ConcurrentMutations. The original inspected
// concurrently and appended to the returned Env slice (which exercises only the
// local slice). Preserved verbatim in spirit (10 goroutines x inspect+append, any
// inspect error fails). Expanded: the REAL ContainerCloner.PrepareShadowConfig is
// called concurrently with distinct env; every result must carry only its own
// variable and the original container's configuration must be unchanged. Run
// with -race to make the data-race part meaningful.
func TestRace_ContainerClone_ConcurrentMutations(t *testing.T) {
	cli := dockerClient(t)
	runLabel := lgNewRun(t, cli)
	name := uniqueName(t, "dso-it-race-base")
	id := lgCreate(t, runLabel, name, []string{"BASE_VAR=value", "CONFIG_VAR=test"}, map[string]string{"test": "label"})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := cli.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		t.Fatalf("start: %v", err)
	}

	before, err := cli.ContainerInspect(ctx, id)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	wantEnv := append([]string(nil), before.Config.Env...)

	const n = 10
	var wg sync.WaitGroup
	var mu sync.Mutex
	var mutationErrors []error
	cloner := rotation.NewContainerCloner(cli)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			record := func(err error) { mu.Lock(); mutationErrors = append(mutationErrors, err); mu.Unlock() }

			inspect, err := cli.ContainerInspect(ctx, id)
			if err != nil {
				record(err)
				return
			}
			originalEnv := inspect.Config.Env
			for j := 0; j < 10; j++ {
				_ = append(originalEnv, fmt.Sprintf("VAR_%d_%d=value", g, j))
			}

			own := fmt.Sprintf("CLONE_%d", g)
			cfg, _, _, _, err := cloner.PrepareShadowConfig(ctx, id, map[string]string{own: "v"}, nil)
			if err != nil {
				record(fmt.Errorf("PrepareShadowConfig %d: %w", g, err))
				return
			}
			for _, kv := range cfg.Env {
				k, _, _ := strings.Cut(kv, "=")
				if strings.HasPrefix(k, "CLONE_") && k != own {
					record(fmt.Errorf("clone %d leaked another goroutine's variable %s", g, k))
				}
			}
			if !strings.Contains(strings.Join(cfg.Env, "\n"), own+"=v") {
				record(fmt.Errorf("clone %d is missing its own variable", g))
			}
		}(i)
	}
	wg.Wait()

	if len(mutationErrors) > 0 {
		t.Errorf("Got %d errors during concurrent mutations", len(mutationErrors))
		for _, err := range mutationErrors {
			t.Logf("Error: %v", err)
		}
	}
	after, err := cli.ContainerInspect(ctx, id)
	if err != nil {
		t.Fatalf("inspect after: %v", err)
	}
	if !reflect.DeepEqual(wantEnv, after.Config.Env) {
		t.Error("the original container's configuration changed while clones were prepared")
	}
}

// Original: TestRace_ContainerRename_AtomicSwap. Original assertion: after
// rename(original -> _backup) and rename(new -> base), the "new" container is named
// /base. Expanded: the backup name is held by the original, the base name resolves
// to the new container, and both keep running. Names are unique per test.
func TestRace_ContainerRename_AtomicSwap(t *testing.T) {
	cli := dockerClient(t)
	runLabel := lgNewRun(t, cli)
	baseName := uniqueName(t, "dso-it-atomic")
	backupName := baseName + "_backup"
	newName := baseName + "_new"

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	origID := lgCreate(t, runLabel, baseName, nil, nil)
	if err := cli.ContainerStart(ctx, origID, container.StartOptions{}); err != nil {
		t.Fatalf("Failed to start container: %v", err)
	}
	newID := lgCreate(t, runLabel, newName, nil, nil)
	if err := cli.ContainerStart(ctx, newID, container.StartOptions{}); err != nil {
		t.Fatalf("Failed to start new container: %v", err)
	}

	if err := cli.ContainerRename(ctx, origID, backupName); err != nil {
		t.Fatalf("Failed to rename original to backup: %v", err)
	}
	if err := cli.ContainerRename(ctx, newID, baseName); err != nil {
		_ = cli.ContainerRename(ctx, backupName, baseName) // best-effort recovery as in the original
		t.Fatalf("Failed to rename new to original: %v", err)
	}

	inspect, err := cli.ContainerInspect(ctx, newID)
	if err != nil {
		t.Fatalf("Failed to inspect result: %v", err)
	}
	if inspect.Name != "/"+baseName {
		t.Errorf("Expected container name %s, got %s", baseName, inspect.Name)
	}
	if in, err := cli.ContainerInspect(ctx, baseName); err != nil || in.ID != newID {
		t.Errorf("base name does not resolve to the new container (err=%v)", err)
	}
	if in, err := cli.ContainerInspect(ctx, backupName); err != nil || in.ID != origID {
		t.Errorf("backup name does not resolve to the original container (err=%v)", err)
	}
	for _, id := range []string{origID, newID} {
		if in, err := cli.ContainerInspect(ctx, id); err != nil || !in.State.Running {
			t.Errorf("container %s is not running after the swap (err=%v)", id[:12], err)
		}
	}
}
