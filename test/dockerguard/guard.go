// Package dockerguard is the single, fail-closed gate every Docker integration
// suite must pass before it touches a Docker daemon.
//
// Why it exists: the integration suites used client.FromEnv, which silently
// falls back to the developer's default daemon (/var/run/docker.sock). A test
// that creates, renames or removes containers there can destroy real
// workloads. This package makes that impossible by construction:
//
//   - tests are opt-in (DSO_INTEGRATION_TESTS=1); otherwise they skip and never
//     construct a Docker client;
//   - the endpoint must be configured explicitly (DSO_TEST_DOCKER_HOST); the
//     default socket/context is never consulted;
//   - the daemon must prove its identity: its ID must equal DSO_TEST_DAEMON_ID
//     and it must carry the label dso.test.environment=<DSO_TEST_ENVIRONMENT_ID>.
//     Both are provisioned by the disposable-VM bootstrap (daemon.json), not by
//     the test suite, so the suite cannot forge them;
//   - the daemon must not already host any container that lacks the test label,
//     and its ID must not be on an explicit prohibited list;
//   - all resources a run creates carry dso.test.run=<run id> and cleanup only
//     ever removes resources carrying BOTH the environment and the run label.
//
// Verification is read-only: no container is created to discover whether a
// daemon is safe. Pathnames, hostnames and context names are never accepted as
// proof of isolation.
package dockerguard

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Environment variable names (all read through an injectable getenv so the
// guard can be tested without touching the process environment).
const (
	EnvOptIn       = "DSO_INTEGRATION_TESTS"
	EnvHost        = "DSO_TEST_DOCKER_HOST"
	EnvDaemonID    = "DSO_TEST_DAEMON_ID"
	EnvEnvironment = "DSO_TEST_ENVIRONMENT_ID"
	EnvProhibited  = "DSO_TEST_PROHIBITED_DAEMON_IDS"
	EnvRunID       = "DSO_TEST_RUN_ID"

	LabelEnvironment = "dso.test.environment"
	LabelRun         = "dso.test.run"
)

var (
	// ErrNotOptedIn means integration tests were not requested. Callers skip;
	// no Docker client is ever built in this state.
	ErrNotOptedIn = errors.New("integration tests not requested (set " + EnvOptIn + "=1)")

	envIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{7,63}$`)
	runIDRe = regexp.MustCompile(`^dso-it-[0-9a-f]{8,16}$`)
)

// DaemonInfo is the read-only identity the guard needs from a daemon.
type DaemonInfo struct {
	ID     string
	Labels []string // "key=value", as reported by `docker info`
}

// Resource is a container, network or volume with its labels.
type Resource struct {
	ID     string
	Name   string
	Labels map[string]string
}

// API is the minimal daemon surface used by the guard and by run cleanup.
// Real clients are adapted in client.go; tests inject fakes.
type API interface {
	DaemonInfo(ctx context.Context) (DaemonInfo, error)
	ListContainers(ctx context.Context) ([]Resource, error) // all, including stopped
	RemoveContainer(ctx context.Context, id string) error
	ListNetworks(ctx context.Context) ([]Resource, error)
	RemoveNetwork(ctx context.Context, id string) error
	ListVolumes(ctx context.Context) ([]Resource, error)
	RemoveVolume(ctx context.Context, name string) error
}

// Config is the validated, explicit test-environment configuration.
type Config struct {
	Host          string
	DaemonID      string
	EnvironmentID string
	ProhibitedIDs []string
	RunID         string
}

// Labels returns the labels every test-created resource must carry.
func (c Config) Labels() map[string]string {
	return map[string]string{LabelEnvironment: c.EnvironmentID, LabelRun: c.RunID}
}

// NewRunID returns a fresh, valid run identifier.
func NewRunID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		panic("dockerguard: crypto/rand failed: " + err.Error())
	}
	return "dso-it-" + hex.EncodeToString(b)
}

// ValidRunID reports whether id has the required shape.
func ValidRunID(id string) bool { return runIDRe.MatchString(id) }

// LoadConfig validates the process configuration without contacting any
// daemon. It returns ErrNotOptedIn when tests were not requested and a
// descriptive error for every ambiguous or missing setting.
func LoadConfig(getenv func(string) string) (Config, error) {
	if getenv(EnvOptIn) != "1" {
		return Config{}, ErrNotOptedIn
	}
	host := strings.TrimSpace(getenv(EnvHost))
	if host == "" {
		return Config{}, fmt.Errorf("%s is required: refusing to fall back to the default Docker endpoint", EnvHost)
	}
	u, err := url.Parse(host)
	if err != nil || (u.Scheme != "unix" && u.Scheme != "tcp") {
		return Config{}, fmt.Errorf("%s must be a unix:// or tcp:// endpoint, got %q", EnvHost, host)
	}
	if u.Scheme == "unix" && u.Path == "" {
		return Config{}, fmt.Errorf("%s has no socket path", EnvHost)
	}
	// Ambient Docker configuration must not disagree with, or silently add to,
	// the explicit endpoint.
	if h := strings.TrimSpace(getenv("DOCKER_HOST")); h != "" && h != host {
		return Config{}, fmt.Errorf("DOCKER_HOST is set and differs from %s: ambiguous endpoint configuration", EnvHost)
	}
	if c := strings.TrimSpace(getenv("DOCKER_CONTEXT")); c != "" {
		return Config{}, errors.New("DOCKER_CONTEXT is set: contexts are not accepted, use " + EnvHost)
	}
	env := strings.TrimSpace(getenv(EnvEnvironment))
	if env == "" {
		return Config{}, fmt.Errorf("%s is required (provisioned by the test-VM bootstrap)", EnvEnvironment)
	}
	if !envIDRe.MatchString(env) {
		return Config{}, fmt.Errorf("%s is malformed", EnvEnvironment)
	}
	daemon := strings.TrimSpace(getenv(EnvDaemonID))
	if daemon == "" {
		return Config{}, fmt.Errorf("%s is required (the expected Docker daemon ID)", EnvDaemonID)
	}
	run := strings.TrimSpace(getenv(EnvRunID))
	if run == "" {
		run = NewRunID()
	} else if !ValidRunID(run) {
		return Config{}, fmt.Errorf("%s must match %s", EnvRunID, runIDRe.String())
	}
	var prohibited []string
	for _, p := range strings.Split(getenv(EnvProhibited), ",") {
		if p = strings.TrimSpace(p); p != "" {
			prohibited = append(prohibited, p)
		}
	}
	return Config{Host: host, DaemonID: daemon, EnvironmentID: env, ProhibitedIDs: prohibited, RunID: run}, nil
}

// Verify proves, read-only, that api talks to the designated test daemon.
// It must be called (and succeed) before any mutating call.
func Verify(ctx context.Context, cfg Config, api API) error {
	info, err := api.DaemonInfo(ctx)
	if err != nil {
		return fmt.Errorf("cannot identify Docker daemon: %w", err)
	}
	if info.ID == "" {
		return errors.New("Docker daemon reported no ID: cannot establish identity")
	}
	for _, p := range cfg.ProhibitedIDs {
		if p == info.ID {
			return errors.New("daemon is on the prohibited list: refusing to run integration tests against it")
		}
	}
	if info.ID != cfg.DaemonID {
		return errors.New("unexpected Docker daemon identity: the endpoint is not the designated test daemon")
	}
	want := LabelEnvironment + "=" + cfg.EnvironmentID
	found := false
	for _, l := range info.Labels {
		if l == want {
			found = true
			break
		}
	}
	if !found {
		return errors.New("daemon does not carry the provisioned " + LabelEnvironment + " label: not an approved test environment")
	}
	cs, err := api.ListContainers(ctx)
	if err != nil {
		return fmt.Errorf("cannot list containers to confirm the daemon is dedicated: %w", err)
	}
	for _, c := range cs {
		if c.Labels[LabelEnvironment] != cfg.EnvironmentID {
			return fmt.Errorf("daemon hosts a container (%s) that is not a test resource: refusing, this is not a dedicated test daemon", shortID(c.ID))
		}
	}
	return nil
}

// CleanupRun removes only the containers, networks and volumes that carry
// both this environment's label and this run's label. Anything else,
// including another run's resources, is left untouched. It never prunes.
func CleanupRun(ctx context.Context, cfg Config, api API) error {
	if !ValidRunID(cfg.RunID) || cfg.EnvironmentID == "" {
		return errors.New("refusing cleanup: invalid run or environment identity")
	}
	own := func(r Resource) bool {
		return r.Labels[LabelEnvironment] == cfg.EnvironmentID && r.Labels[LabelRun] == cfg.RunID
	}
	var errs []error
	cs, err := api.ListContainers(ctx)
	if err != nil {
		return err
	}
	for _, r := range cs {
		if own(r) {
			if e := api.RemoveContainer(ctx, r.ID); e != nil {
				errs = append(errs, e)
			}
		}
	}
	ns, err := api.ListNetworks(ctx)
	if err == nil {
		for _, r := range ns {
			if own(r) {
				if e := api.RemoveNetwork(ctx, r.ID); e != nil {
					errs = append(errs, e)
				}
			}
		}
	} else {
		errs = append(errs, err)
	}
	vs, err := api.ListVolumes(ctx)
	if err == nil {
		for _, r := range vs {
			if own(r) {
				if e := api.RemoveVolume(ctx, r.Name); e != nil {
					errs = append(errs, e)
				}
			}
		}
	} else {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
