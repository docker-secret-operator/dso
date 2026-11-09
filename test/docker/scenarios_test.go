//go:build dockerint

// Package docker holds the Docker integration scenarios from
// docs/product-evolution/m0-docker-ci-plan.md (S1-S3 baseline). They need a
// real Docker Engine with Compose v2 and run only with -tags dockerint, so
// `go test ./...` in ci.yml/coverage.yml never compiles or runs them.
//
// Scope note: these scenarios drive DSO's real rotation engine
// (watcher.ReloaderController: label discovery, ownership gate, restart /
// rolling / signal strategies, health gate) in-process against the real
// daemon. The secret value is supplied through the controller's cache
// interface instead of a Vault dev container polled by a root `docker-dso
// agent` process; that end-to-end harness (plan section 3) is a later step.
package docker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/docker-secret-operator/dso/internal/watcher"
	dsoConfig "github.com/docker-secret-operator/dso/pkg/config"
)

const (
	// testImage is pinned by tag; pin by digest once the lane is green
	// (plan section 3, BL-25).
	testImage    = "busybox:1.36.1"
	secretName   = "app_token"
	envName      = "APP_TOKEN"
	providerName = "vault"
	ownerID      = "dso-it-owner"
	rotateWait   = 3 * time.Minute

	// The workload traps SIGTERM (fast, clean stop) and SIGHUP (writes a
	// marker file so S3/signal can observe delivery).
	workloadScript = `trap 'exit 0' TERM; trap 'echo hup > /tmp/dso-hup' HUP; while true; do sleep 1 & wait $!; done`
)

// sentinel prefixes every synthetic secret value. CI sets DSO_IT_SENTINEL so
// the workflow's leak scan knows what to grep for; locally it is random.
var sentinel = func() string {
	if s := os.Getenv("DSO_IT_SENTINEL"); s != "" {
		return s
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "dso-it-" + hex.EncodeToString(b)
}()

func syntheticValue(version string) string { return sentinel + "-" + version }

// fakeCache stands in for the agent's provider-populated secret cache.
type fakeCache struct {
	mu   sync.Mutex
	data map[string]map[string]string
}

func (c *fakeCache) Get(key string) (map[string]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.data[key]
	return v, ok
}

func (c *fakeCache) set(value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[providerName+":"+secretName] = map[string]string{"value": value}
}

func dockerClient(t *testing.T) *client.Client {
	t.Helper()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		t.Fatalf("Docker daemon not reachable (these scenarios require one): %v", err)
	}
	return cli
}

func runCmd(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		// Redact synthetic secret values (e.g. `-e APP_TOKEN=...`) so the
		// workflow's leak scan only ever trips on a real product leak.
		redact := func(v string) string { return strings.ReplaceAll(v, sentinel, "<redacted>") }
		t.Fatalf("%s %s: %v\n%s", name, redact(strings.Join(args, " ")), err, redact(string(out)))
	}
	return strings.TrimSpace(string(out))
}

// uniqueName returns a per-test name so runs never collide.
func uniqueName(t *testing.T, prefix string) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return prefix + "-" + hex.EncodeToString(b)
}

// cleanupByLabel removes every container carrying label=value when the test ends.
func cleanupByLabel(t *testing.T, cli *client.Client, label, value string) {
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		f := filters.NewArgs()
		f.Add("label", label+"="+value)
		list, err := cli.ContainerList(ctx, container.ListOptions{All: true, Filters: f})
		if err != nil {
			t.Logf("cleanup list: %v", err)
			return
		}
		for _, c := range list {
			_ = cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true})
		}
	})
}

func newConfig() *dsoConfig.Config {
	cfg := &dsoConfig.Config{
		Providers: map[string]dsoConfig.ProviderConfig{providerName: {Type: "vault"}},
		Secrets: []dsoConfig.SecretMapping{{
			Name:     secretName,
			Provider: providerName,
			Inject:   dsoConfig.InjectionConfig{Type: "env"},
			Mappings: map[string]string{"value": envName},
		}},
	}
	cfg.Agent.OwnerID = ownerID
	cfg.Agent.Rotation.HealthCheckTimeout = "60s"
	return cfg
}

// startController starts a real ReloaderController (label discovery + event
// loop) wired to cache, and stops it at test end.
func startController(t *testing.T, cache *fakeCache) *watcher.ReloaderController {
	t.Helper()
	r, err := watcher.NewReloaderController(zaptest.NewLogger(t, zaptest.Level(zap.InfoLevel)))
	if err != nil {
		t.Fatalf("NewReloaderController: %v", err)
	}
	r.Config = newConfig()
	r.Cache = cache
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r.StartEventLoop(ctx)
	return r
}

func isTarget(r *watcher.ReloaderController, id string) bool {
	_, ok := r.Targets.Load(id)
	return ok
}

// waitTarget waits (with a deadline) until the controller has registered id.
func waitTarget(t *testing.T, r *watcher.ReloaderController, id string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if isTarget(r, id) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("controller never registered container %s", id[:12])
}

// rotate runs TriggerReload and waits for its real async outcome.
func rotate(t *testing.T, r *watcher.ReloaderController) {
	t.Helper()
	done := make(chan error, 1)
	if err := r.TriggerReload(context.Background(), secretName, func(err error) { done <- err }); err != nil {
		t.Fatalf("TriggerReload: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("rotation outcome: %v", err)
		}
	case <-time.After(rotateWait):
		t.Fatal("rotation did not complete before the deadline")
	}
}

// runWorkload starts a labelled container with APP_TOKEN=v1 via `docker run`.
func runWorkload(t *testing.T, strategy, runLabel string) (id, name string) {
	t.Helper()
	name = uniqueName(t, "dso-it-"+strategy)
	id = runCmd(t, "docker", "run", "-d", "--name", name,
		"--label", "dso.reloader=true",
		"--label", "dso.owner="+ownerID,
		"--label", "dso.secrets="+secretName,
		"--label", "dso.update.strategy="+strategy,
		"--label", "dso.it.run="+runLabel,
		"-e", envName+"="+syntheticValue("v1"),
		testImage, "sh", "-c", workloadScript)
	return id, name
}

func envOf(t *testing.T, cli *client.Client, id string) map[string]string {
	t.Helper()
	in, err := cli.ContainerInspect(context.Background(), id)
	if err != nil {
		t.Fatalf("inspect %s: %v", id[:12], err)
	}
	env := map[string]string{}
	for _, kv := range in.Config.Env {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	return env
}

// currentIDByName resolves the container currently holding name.
func currentIDByName(t *testing.T, cli *client.Client, name string) string {
	t.Helper()
	in, err := cli.ContainerInspect(context.Background(), name)
	if err != nil {
		t.Fatalf("no container named %s after rotation: %v", name, err)
	}
	if !in.State.Running {
		t.Fatalf("container %s is not running after rotation (status %s)", name, in.State.Status)
	}
	return in.ID
}

func assertGone(t *testing.T, cli *client.Client, id string) {
	t.Helper()
	if _, err := cli.ContainerInspect(context.Background(), id); err == nil {
		t.Errorf("original container %s still exists after rotation", id[:12])
	} else if !client.IsErrNotFound(err) {
		t.Errorf("inspect original %s: %v", id[:12], err)
	}
}

// assertEnvValue checks the env value without ever printing it.
func assertEnvValue(t *testing.T, cli *client.Client, id, want string) {
	t.Helper()
	if got := envOf(t, cli, id)[envName]; got != want {
		t.Errorf("%s in %s does not hold the expected rotated value (match=%v)", envName, id[:12], got == want)
	}
}

// writeCompose writes a one-service Compose project for S1/S2.
func writeCompose(t *testing.T, project string, ownerLabel bool) string {
	t.Helper()
	dir := t.TempDir()
	owner := ""
	if ownerLabel {
		owner = "      dso.owner: \"" + ownerID + "\"\n"
	}
	yaml := fmt.Sprintf(`services:
  web:
    image: %s
    command: ["sh", "-c", %q]
    environment:
      %s: %q
    labels:
      dso.reloader: "true"
      dso.secrets: %q
      dso.update.strategy: "restart"
%s`, testImage, workloadScript, envName, syntheticValue("v1"), secretName, owner)
	path := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = exec.Command("docker", "compose", "-p", project, "-f", path, "down", "-v", "--remove-orphans", "-t", "2").Run()
	})
	return path
}

// composeService resolves the single `web` container of a Compose project.
func composeService(t *testing.T, cli *client.Client, project, path string) (id, name string) {
	t.Helper()
	ids := strings.Fields(runCmd(t, "docker", "compose", "-p", project, "-f", path, "ps", "-q", "web"))
	if len(ids) != 1 {
		t.Fatalf("expected exactly one web container in %s, got %v", project, ids)
	}
	in, err := cli.ContainerInspect(context.Background(), ids[0])
	if err != nil {
		t.Fatalf("inspect %s: %v", ids[0], err)
	}
	return in.ID, strings.TrimPrefix(in.Name, "/")
}

// S1 -- Compose service creation: a Compose-created, owner-labelled service
// is adopted by label discovery, an unowned one is not (SEC-P0 default
// deny), and adoption alone recreates nothing.
func TestS1_ComposeServiceAdoption(t *testing.T) {
	cli := dockerClient(t)
	cache := &fakeCache{data: map[string]map[string]string{}}
	cache.set(syntheticValue("v1"))

	owned := uniqueName(t, "dso-it-s1-owned")
	unowned := uniqueName(t, "dso-it-s1-unowned")
	ownedPath := writeCompose(t, owned, true)
	unownedPath := writeCompose(t, unowned, false)
	runCmd(t, "docker", "compose", "-p", owned, "-f", ownedPath, "up", "-d", "--wait")
	runCmd(t, "docker", "compose", "-p", unowned, "-f", unownedPath, "up", "-d", "--wait")
	ownedID, _ := composeService(t, cli, owned, ownedPath)
	unownedID, _ := composeService(t, cli, unowned, unownedPath)

	r := startController(t, cache)
	waitTarget(t, r, ownedID)
	if isTarget(r, unownedID) {
		t.Errorf("container without a dso.owner label must not be adopted")
	}

	// Adoption must not rotate: the original container is still the one
	// running, and the project still has exactly one web container.
	if got, _ := composeService(t, cli, owned, ownedPath); got != ownedID {
		t.Errorf("adoption recreated the service container: %s -> %s", ownedID[:12], got[:12])
	}
}

// S2 -- Secret injection (env): rotating v1 -> v2 recreates the Compose
// service container with the new value and removes the original.
func TestS2_EnvSecretInjection(t *testing.T) {
	cli := dockerClient(t)
	cache := &fakeCache{data: map[string]map[string]string{}}
	cache.set(syntheticValue("v1"))

	project := uniqueName(t, "dso-it-s2")
	path := writeCompose(t, project, true)
	runCmd(t, "docker", "compose", "-p", project, "-f", path, "up", "-d", "--wait")
	oldID, oldName := composeService(t, cli, project, path)

	r := startController(t, cache)
	waitTarget(t, r, oldID)

	cache.set(syntheticValue("v2"))
	rotate(t, r)

	newID := currentIDByName(t, cli, oldName)
	if newID == oldID {
		t.Fatal("restart rotation did not recreate the container")
	}
	assertEnvValue(t, cli, newID, syntheticValue("v2"))
	assertGone(t, cli, oldID)
}

// S3 -- Container rotation per strategy: rolling and restart recreate the
// container under its original name with the new value; signal delivers
// SIGHUP to the running container without recreating it.
func TestS3_RotationStrategies(t *testing.T) {
	for _, strategy := range []string{"rolling", "restart"} {
		t.Run(strategy, func(t *testing.T) {
			cli := dockerClient(t)
			runLabel := uniqueName(t, "run")
			cleanupByLabel(t, cli, "dso.it.run", runLabel)
			cache := &fakeCache{data: map[string]map[string]string{}}
			cache.set(syntheticValue("v1"))

			oldID, name := runWorkload(t, strategy, runLabel)
			r := startController(t, cache)
			waitTarget(t, r, oldID)

			cache.set(syntheticValue("v2"))
			rotate(t, r)

			newID := currentIDByName(t, cli, name)
			if newID == oldID {
				t.Fatalf("%s rotation kept the original container", strategy)
			}
			assertEnvValue(t, cli, newID, syntheticValue("v2"))
			assertGone(t, cli, oldID)
		})
	}

	t.Run("signal", func(t *testing.T) {
		cli := dockerClient(t)
		runLabel := uniqueName(t, "run")
		cleanupByLabel(t, cli, "dso.it.run", runLabel)
		cache := &fakeCache{data: map[string]map[string]string{}}
		cache.set(syntheticValue("v1"))

		id, name := runWorkload(t, "signal", runLabel)
		r := startController(t, cache)
		waitTarget(t, r, id)

		cache.set(syntheticValue("v2"))
		rotate(t, r)

		if got := currentIDByName(t, cli, name); got != id {
			t.Fatalf("signal strategy must not recreate the container (%s -> %s)", id[:12], got[:12])
		}
		// SIGHUP delivery is asynchronous inside the container; wait for the
		// trap's marker with a deadline.
		deadline := time.Now().Add(15 * time.Second)
		for {
			out, err := exec.Command("docker", "exec", id, "cat", "/tmp/dso-hup").CombinedOutput()
			if err == nil && strings.TrimSpace(string(out)) == "hup" {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("SIGHUP marker never appeared: %v %s", err, out)
			}
			time.Sleep(250 * time.Millisecond)
		}
	})
}
