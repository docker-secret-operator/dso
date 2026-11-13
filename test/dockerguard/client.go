package dockerguard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
)

// Session is a verified connection to the designated test daemon.
type Session struct {
	Client *client.Client
	Config Config
	api    API
}

// Labels returns the labels every resource created by this run must carry.
func (s *Session) Labels() map[string]string { return s.Config.Labels() }

// LabelArgs returns `--label k=v` CLI arguments for docker run/create.
func (s *Session) LabelArgs() []string {
	var out []string
	for k, v := range s.Labels() {
		out = append(out, "--label", k+"="+v)
	}
	return out
}

// Docker runs the docker CLI against the verified endpoint only. All ambient
// DOCKER_* variables are removed so no default socket or context can leak in.
func (s *Session) Docker(t testing.TB, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("docker", append([]string{"--host", s.Config.Host}, args...)...) // #nosec G204 -- fixed binary, endpoint verified by Verify
	cmd.Env = append(sanitizedEnv(os.Environ()), "DOCKER_HOST="+s.Config.Host)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func sanitizedEnv(in []string) []string {
	out := make([]string, 0, len(in))
	for _, kv := range in {
		if strings.HasPrefix(kv, "DOCKER_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

var (
	mu      sync.Mutex
	current *Session
)

// Main is the TestMain helper every Docker integration package must use:
//
//	func TestMain(m *testing.M) { dockerguard.Main(m) }
//
// Not opted in: tests run (and skip via Current) without any Docker client.
// Opted in: the guard verifies the daemon ONCE before any test runs; if it
// cannot establish isolation the whole package FAILS (exit 1) rather than
// skipping. After the tests, only this run's own resources are removed.
func Main(m *testing.M) {
	cfg, err := LoadConfig(os.Getenv)
	if err != nil {
		if errors.Is(err, ErrNotOptedIn) {
			os.Exit(m.Run())
		}
		fmt.Fprintln(os.Stderr, "integration isolation guard:", err)
		os.Exit(1)
	}
	cli, err := client.NewClientWithOpts(client.WithHost(cfg.Host), client.WithAPIVersionNegotiation())
	if err != nil {
		fmt.Fprintln(os.Stderr, "integration isolation guard: docker client:", err)
		os.Exit(1)
	}
	api := &realAPI{cli: cli}
	vctx, vcancel := context.WithTimeout(context.Background(), 20*time.Second)
	err = Verify(vctx, cfg, api)
	vcancel()
	if err != nil {
		_ = cli.Close()
		fmt.Fprintln(os.Stderr, "integration isolation guard:", err)
		os.Exit(1)
	}
	mu.Lock()
	current = &Session{Client: cli, Config: cfg, api: api}
	mu.Unlock()
	code := m.Run()
	cctx, ccancel := context.WithTimeout(context.Background(), 2*time.Minute)
	if err := CleanupRun(cctx, cfg, api); err != nil {
		fmt.Fprintln(os.Stderr, "integration run cleanup:", err)
		if code == 0 {
			code = 1
		}
	}
	ccancel()
	_ = cli.Close()
	os.Exit(code)
}

// Current returns the verified session established by Main. When integration
// tests were not requested the calling test is skipped; if Main was never
// installed in this package while tests are opted in, the test FAILS (a suite
// that bypasses the guard must not run).
func Current(t testing.TB) *Session {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	if current != nil {
		return current
	}
	if os.Getenv(EnvOptIn) != "1" {
		t.Skip(ErrNotOptedIn.Error())
	}
	t.Fatalf("integration isolation guard: package does not install dockerguard.Main in TestMain")
	return nil
}

type realAPI struct{ cli *client.Client }

func (r *realAPI) DaemonInfo(ctx context.Context) (DaemonInfo, error) {
	i, err := r.cli.Info(ctx)
	if err != nil {
		return DaemonInfo{}, err
	}
	return DaemonInfo{ID: i.ID, Labels: i.Labels}, nil
}

func (r *realAPI) ListContainers(ctx context.Context) ([]Resource, error) {
	cs, err := r.cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, err
	}
	out := make([]Resource, 0, len(cs))
	for _, c := range cs {
		name := ""
		if len(c.Names) > 0 {
			name = c.Names[0]
		}
		out = append(out, Resource{ID: c.ID, Name: name, Labels: c.Labels})
	}
	return out, nil
}

func (r *realAPI) RemoveContainer(ctx context.Context, id string) error {
	return r.cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: true, RemoveVolumes: true})
}

func (r *realAPI) ListNetworks(ctx context.Context) ([]Resource, error) {
	ns, err := r.cli.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]Resource, 0, len(ns))
	for _, n := range ns {
		out = append(out, Resource{ID: n.ID, Name: n.Name, Labels: n.Labels})
	}
	return out, nil
}

func (r *realAPI) RemoveNetwork(ctx context.Context, id string) error {
	return r.cli.NetworkRemove(ctx, id)
}

func (r *realAPI) ListVolumes(ctx context.Context) ([]Resource, error) {
	vs, err := r.cli.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]Resource, 0, len(vs.Volumes))
	for _, v := range vs.Volumes {
		out = append(out, Resource{ID: v.Name, Name: v.Name, Labels: v.Labels})
	}
	return out, nil
}

func (r *realAPI) RemoveVolume(ctx context.Context, name string) error {
	return r.cli.VolumeRemove(ctx, name, true)
}

// CleanupFromEnv verifies the designated test daemon and removes only the
// resources owned by DSO_TEST_RUN_ID (which must be set and valid here: a
// cleanup must never guess which run to target). It is the engine behind
// scripts/integration/cleanup-integration.sh.
func CleanupFromEnv(ctx context.Context) error {
	if os.Getenv(EnvRunID) == "" {
		return errors.New(EnvRunID + " is required for cleanup")
	}
	cfg, err := LoadConfig(os.Getenv)
	if err != nil {
		return err
	}
	cli, err := client.NewClientWithOpts(client.WithHost(cfg.Host), client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}
	defer cli.Close()
	api := &realAPI{cli: cli}
	if err := Verify(ctx, cfg, api); err != nil {
		return err
	}
	return CleanupRun(ctx, cfg, api)
}
