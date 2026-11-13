package dockerguard

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeAPI is an in-memory daemon. It records every mutating call so tests can
// prove the guard never mutates, and never contacts a real daemon.
type fakeAPI struct {
	info       DaemonInfo
	infoErr    error
	containers []Resource
	networks   []Resource
	volumes    []Resource
	calls      []string
}

func (f *fakeAPI) DaemonInfo(context.Context) (DaemonInfo, error) {
	f.calls = append(f.calls, "info")
	return f.info, f.infoErr
}
func (f *fakeAPI) ListContainers(context.Context) ([]Resource, error) { return f.containers, nil }
func (f *fakeAPI) ListNetworks(context.Context) ([]Resource, error)   { return f.networks, nil }
func (f *fakeAPI) ListVolumes(context.Context) ([]Resource, error)    { return f.volumes, nil }
func (f *fakeAPI) RemoveContainer(_ context.Context, id string) error {
	f.calls = append(f.calls, "rm-container:"+id)
	return nil
}
func (f *fakeAPI) RemoveNetwork(_ context.Context, id string) error {
	f.calls = append(f.calls, "rm-network:"+id)
	return nil
}
func (f *fakeAPI) RemoveVolume(_ context.Context, n string) error {
	f.calls = append(f.calls, "rm-volume:"+n)
	return nil
}

func env(kv map[string]string) func(string) string { return func(k string) string { return kv[k] } }

func goodEnv() map[string]string {
	return map[string]string{
		EnvOptIn:       "1",
		EnvHost:        "unix:///run/dso-test/docker.sock",
		EnvDaemonID:    "TEST-DAEMON-ID",
		EnvEnvironment: "env-0123456789ab",
		EnvRunID:       "dso-it-0123abcd",
	}
}

func goodAPI() *fakeAPI {
	return &fakeAPI{info: DaemonInfo{ID: "TEST-DAEMON-ID", Labels: []string{LabelEnvironment + "=env-0123456789ab"}}}
}

func TestLoadConfig_RejectsUnsafeConfiguration(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]string)
		want   string
		notOpt bool
	}{
		{"missing opt-in", func(m map[string]string) { delete(m, EnvOptIn) }, "", true},
		{"opt-in not exactly 1", func(m map[string]string) { m[EnvOptIn] = "true" }, "", true},
		{"missing endpoint", func(m map[string]string) { delete(m, EnvHost) }, "default Docker endpoint", false},
		{"default DOCKER_HOST fallback is not an endpoint", func(m map[string]string) { delete(m, EnvHost); m["DOCKER_HOST"] = "unix:///var/run/docker.sock" }, "default Docker endpoint", false},
		{"DOCKER_HOST disagrees", func(m map[string]string) { m["DOCKER_HOST"] = "unix:///var/run/docker.sock" }, "ambiguous", false},
		{"docker context set", func(m map[string]string) { m["DOCKER_CONTEXT"] = "default" }, "DOCKER_CONTEXT", false},
		{"non unix/tcp endpoint", func(m map[string]string) { m[EnvHost] = "ssh://prod" }, "unix:// or tcp://", false},
		{"unix without path", func(m map[string]string) { m[EnvHost] = "unix://" }, "no socket path", false},
		{"missing environment id", func(m map[string]string) { delete(m, EnvEnvironment) }, EnvEnvironment, false},
		{"malformed environment id", func(m map[string]string) { m[EnvEnvironment] = "x" }, "malformed", false},
		{"missing daemon id", func(m map[string]string) { delete(m, EnvDaemonID) }, EnvDaemonID, false},
		{"invalid run id", func(m map[string]string) { m[EnvRunID] = "../../etc" }, EnvRunID, false},
		{"uppercase run id", func(m map[string]string) { m[EnvRunID] = "dso-it-ABCDEF12" }, EnvRunID, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := goodEnv()
			c.mutate(m)
			_, err := LoadConfig(env(m))
			if err == nil {
				t.Fatal("expected an error")
			}
			if c.notOpt {
				if !errors.Is(err, ErrNotOptedIn) {
					t.Fatalf("expected ErrNotOptedIn, got %v", err)
				}
				return
			}
			if errors.Is(err, ErrNotOptedIn) {
				t.Fatalf("must be a hard error, not a skip: %v", err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not mention %q", err, c.want)
			}
		})
	}
}

func TestLoadConfig_AcceptsExplicitConfigAndGeneratesRunID(t *testing.T) {
	m := goodEnv()
	delete(m, EnvRunID)
	cfg, err := LoadConfig(env(m))
	if err != nil {
		t.Fatal(err)
	}
	if !ValidRunID(cfg.RunID) {
		t.Fatalf("generated run id %q invalid", cfg.RunID)
	}
	if cfg.Labels()[LabelRun] != cfg.RunID || cfg.Labels()[LabelEnvironment] != "env-0123456789ab" {
		t.Fatalf("labels wrong: %v", cfg.Labels())
	}
	if a, b := NewRunID(), NewRunID(); a == b {
		t.Fatal("run ids must be unique")
	}
}

func TestVerify_DaemonIdentity(t *testing.T) {
	cfg, _ := LoadConfig(env(goodEnv()))
	ctx := context.Background()

	t.Run("designated daemon is accepted and nothing is mutated", func(t *testing.T) {
		api := goodAPI()
		if err := Verify(ctx, cfg, api); err != nil {
			t.Fatal(err)
		}
		for _, c := range api.calls {
			if strings.HasPrefix(c, "rm-") {
				t.Fatalf("Verify mutated: %v", api.calls)
			}
		}
	})
	t.Run("unexpected daemon identity", func(t *testing.T) {
		api := goodAPI()
		api.info.ID = "SOMEONES-DEV-DAEMON"
		if err := Verify(ctx, cfg, api); err == nil || !strings.Contains(err.Error(), "unexpected Docker daemon identity") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("explicitly prohibited daemon", func(t *testing.T) {
		m := goodEnv()
		m[EnvProhibited] = "OTHER, TEST-DAEMON-ID"
		c, _ := LoadConfig(env(m))
		if err := Verify(ctx, c, goodAPI()); err == nil || !strings.Contains(err.Error(), "prohibited") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("matching id but missing provisioned environment label", func(t *testing.T) {
		api := goodAPI()
		api.info.Labels = nil
		if err := Verify(ctx, cfg, api); err == nil || !strings.Contains(err.Error(), "label") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("label for a different environment", func(t *testing.T) {
		api := goodAPI()
		api.info.Labels = []string{LabelEnvironment + "=another-environment"}
		if err := Verify(ctx, cfg, api); err == nil {
			t.Fatal("expected rejection")
		}
	})
	t.Run("daemon without an ID", func(t *testing.T) {
		api := goodAPI()
		api.info.ID = ""
		if err := Verify(ctx, cfg, api); err == nil {
			t.Fatal("expected rejection")
		}
	})
	t.Run("daemon unreachable", func(t *testing.T) {
		api := goodAPI()
		api.infoErr = errors.New("connection refused")
		if err := Verify(ctx, cfg, api); err == nil {
			t.Fatal("expected rejection")
		}
	})
	t.Run("daemon already hosts a non-test container", func(t *testing.T) {
		api := goodAPI()
		api.containers = []Resource{{ID: "abcdef1234567890", Labels: map[string]string{"app": "prod-db"}}}
		err := Verify(ctx, cfg, api)
		if err == nil || !strings.Contains(err.Error(), "not a dedicated test daemon") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("leftover container from an earlier test run is fine", func(t *testing.T) {
		api := goodAPI()
		api.containers = []Resource{{ID: "x", Labels: map[string]string{LabelEnvironment: "env-0123456789ab", LabelRun: "dso-it-ffffffff"}}}
		if err := Verify(ctx, cfg, api); err != nil {
			t.Fatal(err)
		}
	})
}

func TestCleanupRun_OnlyRemovesOwnResources(t *testing.T) {
	cfg, _ := LoadConfig(env(goodEnv()))
	own := map[string]string{LabelEnvironment: cfg.EnvironmentID, LabelRun: cfg.RunID}
	otherRun := map[string]string{LabelEnvironment: cfg.EnvironmentID, LabelRun: "dso-it-ffffffff"}
	otherEnv := map[string]string{LabelEnvironment: "different-environment", LabelRun: cfg.RunID}
	api := &fakeAPI{
		containers: []Resource{
			{ID: "own-c", Labels: own}, {ID: "other-run-c", Labels: otherRun},
			{ID: "other-env-c", Labels: otherEnv}, {ID: "unmanaged-c", Labels: map[string]string{"app": "x"}},
			{ID: "nolabel-c"},
		},
		networks: []Resource{{ID: "own-n", Labels: own}, {ID: "unmanaged-n"}, {ID: "other-run-n", Labels: otherRun}},
		volumes:  []Resource{{ID: "own-v", Name: "own-v", Labels: own}, {ID: "other-v", Name: "other-v", Labels: otherRun}},
	}
	if err := CleanupRun(context.Background(), cfg, api); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(api.calls, ",")
	for _, w := range []string{"rm-container:own-c", "rm-network:own-n", "rm-volume:own-v"} {
		if !strings.Contains(got, w) {
			t.Fatalf("missing %s in %s", w, got)
		}
	}
	for _, bad := range []string{"other-run", "other-env", "unmanaged", "nolabel", "other-v"} {
		if strings.Contains(got, bad) {
			t.Fatalf("cleanup touched %q: %s", bad, got)
		}
	}
}

func TestCleanupRun_RefusesInvalidIdentity(t *testing.T) {
	api := &fakeAPI{containers: []Resource{{ID: "c", Labels: map[string]string{}}}}
	for _, cfg := range []Config{
		{RunID: "", EnvironmentID: "env-0123456789ab"},
		{RunID: "dso-it-0123abcd", EnvironmentID: ""},
		{RunID: "not-valid", EnvironmentID: "env-0123456789ab"},
	} {
		if err := CleanupRun(context.Background(), cfg, api); err == nil {
			t.Fatalf("expected refusal for %+v", cfg)
		}
	}
	if len(api.calls) != 0 {
		t.Fatalf("no call may be made: %v", api.calls)
	}
}

func TestSanitizedEnvDropsAllDockerVariables(t *testing.T) {
	out := sanitizedEnv([]string{"PATH=/bin", "DOCKER_HOST=unix:///var/run/docker.sock", "DOCKER_CONTEXT=x", "DOCKER_CONFIG=/h", "HOME=/h"})
	for _, kv := range out {
		if strings.HasPrefix(kv, "DOCKER_") {
			t.Fatalf("leaked %s", kv)
		}
	}
	if len(out) != 2 {
		t.Fatalf("unexpected env %v", out)
	}
}
