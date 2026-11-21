//go:build dockerint

package docker

import (
	"fmt"
	"strings"
	"testing"
	"time"

	dsoConfig "github.com/docker-secret-operator/dso/pkg/config"
	"github.com/docker-secret-operator/dso/test/dockerguard"
)

// Synthetic application for M0-03: waits for /run/secrets/dso/value, loads it on
// start and again on SIGHUP (reopening the file), and records the loaded value
// and a reload counter. With IGNORE_HUP=1 it ignores SIGHUP (models an
// application that does not support reload).
const reloadApp = `
f=/run/secrets/dso/value
load() { cat "$f" > /tmp/loaded.new && mv /tmp/loaded.new /tmp/loaded; }
trap 'exit 0' TERM
if [ "$IGNORE_HUP" = 1 ]; then trap '' HUP; else trap 'load; echo r >> /tmp/reloads' HUP; fi
until [ -s "$f" ]; do sleep 0.2; done
load
echo started > /tmp/started
while true; do sleep 1 & wait $!; done`

const appUID = 10001

func fileModeConfig() *dsoConfig.Config {
	cfg := &dsoConfig.Config{
		Providers: map[string]dsoConfig.ProviderConfig{providerName: {Type: "vault"}},
		Secrets: []dsoConfig.SecretMapping{{
			Name:     secretName,
			Provider: providerName,
			Inject:   dsoConfig.InjectionConfig{Type: "file", Path: "/run/secrets/dso", UID: appUID, GID: appUID},
			Rotation: dsoConfig.RotationConfigV2{Signal: "SIGHUP"},
		}},
	}
	cfg.Agent.OwnerID = ownerID
	cfg.Agent.Rotation.HealthCheckTimeout = "30s"
	return cfg
}

func startReloadApp(t *testing.T, runLabel string, ignoreHUP bool) string {
	t.Helper()
	sess := dockerguard.Current(t)
	name := uniqueName(t, "dso-it-sigfile")
	args := []string{"run", "-d", "--name", name}
	args = append(args, sess.LabelArgs()...)
	args = append(args,
		"--user", fmt.Sprintf("%d:%d", appUID, appUID),
		"--tmpfs", fmt.Sprintf("/run/secrets/dso:rw,uid=%d,gid=%d,mode=0750", appUID, appUID),
		"--label", "dso.reloader=true",
		"--label", "dso.owner="+ownerID,
		"--label", "dso.secrets="+secretName,
		"--label", "dso.update.strategy=signal",
		"--label", "dso.it.run="+runLabel)
	if ignoreHUP {
		args = append(args, "-e", "IGNORE_HUP=1")
	}
	args = append(args, testImage, "sh", "-c", reloadApp)
	return dockerCmd(t, args...)
}

func execOut(t *testing.T, id string, cmd ...string) (string, error) {
	t.Helper()
	return dockerguard.Current(t).Docker(t, append([]string{"exec", id}, cmd...)...)
}

func waitFor(t *testing.T, what string, d time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// seedFile writes v1 the way the initial agent delivery would: as the app user,
// into the tmpfs.
func seedFile(t *testing.T, id, value string) {
	t.Helper()
	dockerCmd(t, "exec", id, "sh", "-c", fmt.Sprintf("umask 077; printf %%s %q > /run/secrets/dso/value", value))
}

// File-mode signal rotation: the new value must be DELIVERED before the signal, the
// container must not be recreated, a non-root application must be able to read the
// delivered file (mode 0400, owner = app user), the application must actually load
// the new value, and the value must never appear in docker inspect.
func TestSignalRotation_FileMode_ApplicationLoadsNewValue(t *testing.T) {
	cli := dockerClient(t)
	runLabel := uniqueName(t, "run")
	cleanupByLabel(t, cli, "dso.it.run", runLabel)
	cache := &fakeCache{data: map[string]map[string]string{}}
	cache.data[providerName+":"+secretName] = map[string]string{"value": syntheticValue("v1")}

	id := startReloadApp(t, runLabel, false)
	seedFile(t, id, syntheticValue("v1"))
	waitFor(t, "app start", 20*time.Second, func() bool { _, err := execOut(t, id, "cat", "/tmp/started"); return err == nil })
	if got, _ := execOut(t, id, "cat", "/tmp/loaded"); got != syntheticValue("v1") {
		t.Fatal("app did not load the initial value")
	}

	r := startControllerCfg(t, cache, fileModeConfig())
	waitTarget(t, r, id)
	cache.data[providerName+":"+secretName] = map[string]string{"value": syntheticValue("v2")}
	rotate(t, r)

	// Same container (signal strategy never recreates it) and still running.
	if run := dockerCmd(t, "inspect", "-f", "{{.State.Running}}", id); run != "true" {
		t.Fatalf("container %s is no longer running", id[:12])
	}
	waitFor(t, "application to load v2", 20*time.Second, func() bool {
		got, err := execOut(t, id, "cat", "/tmp/loaded")
		return err == nil && got == syntheticValue("v2")
	})
	if n, _ := execOut(t, id, "sh", "-c", "wc -l < /tmp/reloads"); strings.TrimSpace(n) == "0" || n == "" {
		t.Fatal("the application never recorded a reload")
	}
	// Non-root readability, owner and mode of the delivered file.
	if st, err := execOut(t, id, "stat", "-c", "%a %u", "/run/secrets/dso/value"); err != nil || st != fmt.Sprintf("400 %d", appUID) {
		t.Fatalf("delivered file attributes %q (%v), want 400 %d", st, err, appUID)
	}
	if out, _ := dockerguard.Current(t).Docker(t, "inspect", id); strings.Contains(out, syntheticValue("v2")) {
		t.Fatal("secret value appears in docker inspect")
	}
	// No temp files left behind in the secret directory.
	if ls, _ := execOut(t, id, "ls", "-A", "/run/secrets/dso"); strings.TrimSpace(ls) != "value" {
		t.Fatalf("unexpected files in the secret directory: %q", ls)
	}
}

// A signal being delivered is NOT proof the application adopted the secret. An app
// that ignores SIGHUP keeps its old value; DSO still reports only "delivered and
// reload requested". This test pins that limitation so it cannot silently be turned
// into an adoption claim.
func TestSignalRotation_FileMode_IgnoredSignalIsNotAdoption(t *testing.T) {
	cli := dockerClient(t)
	runLabel := uniqueName(t, "run")
	cleanupByLabel(t, cli, "dso.it.run", runLabel)
	cache := &fakeCache{data: map[string]map[string]string{}}
	cache.data[providerName+":"+secretName] = map[string]string{"value": syntheticValue("v1")}

	id := startReloadApp(t, runLabel, true)
	seedFile(t, id, syntheticValue("v1"))
	waitFor(t, "app start", 20*time.Second, func() bool { _, err := execOut(t, id, "cat", "/tmp/started"); return err == nil })

	r := startControllerCfg(t, cache, fileModeConfig())
	waitTarget(t, r, id)
	cache.data[providerName+":"+secretName] = map[string]string{"value": syntheticValue("v2")}
	rotate(t, r) // delivery + signal succeed

	time.Sleep(3 * time.Second)
	if got, _ := execOut(t, id, "cat", "/tmp/loaded"); got != syntheticValue("v1") {
		t.Fatalf("the application ignores SIGHUP and must still hold v1, got %q", got)
	}
	// ...while the NEW value is on disk, which is what makes "delivered" != "adopted".
	if got, _ := execOut(t, id, "cat", "/run/secrets/dso/value"); got != syntheticValue("v2") {
		t.Fatalf("new value must have been delivered to the file, got %q", got)
	}
}
