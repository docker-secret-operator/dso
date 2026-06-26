package setup

import (
	"context"
	"os"
	"os/exec"
	"time"
)

// DetectorConfig holds injectable dependencies for environment detection.
// Override these fields in tests to avoid real system calls.
type DetectorConfig struct {
	Getenv            func(string) string
	LookPath          func(string) (string, error)
	Stat              func(string) (os.FileInfo, error)
	DockerSocketPaths []string
	DockerTimeout     time.Duration
	SystemdTimeout    time.Duration
}

// Detector gathers environmental facts without side effects. It never writes
// files, connects to remote endpoints, or validates credentials — it only reads
// what is already present on the system.
type Detector struct {
	cfg DetectorConfig
}

// newDetector returns a Detector wired to real OS dependencies.
func newDetector() *Detector {
	return &Detector{
		cfg: DetectorConfig{
			Getenv:   os.Getenv,
			LookPath: exec.LookPath,
			Stat:     os.Stat,
			DockerSocketPaths: []string{
				"/var/run/docker.sock",
				"/run/docker.sock",
				"/var/run/docker/docker.sock",
			},
			DockerTimeout:  5 * time.Second,
			SystemdTimeout: 3 * time.Second,
		},
	}
}

// Detect collects all environmental facts. It never returns a non-nil error
// for missing optional components — absence is a fact, not a failure.
func (d *Detector) Detect(ctx context.Context) (*Environment, error) {
	env := &Environment{Timestamp: time.Now()}
	env.OS = detectOS()
	env.User = detectUser()
	env.Docker = detectDocker(ctx, d.cfg)
	env.Systemd = detectSystemd(ctx, d.cfg)
	env.Providers = detectProviders(d.cfg)
	env.ExistingDSO = detectExistingDSO(d.cfg)
	env.RecommendedMode, env.RecommendedProvider = computeRecommendation(env)
	return env, nil
}

// computeRecommendation derives the best mode and provider from detected facts.
// Provider priority: aws > azure > vault > local.
func computeRecommendation(env *Environment) (SetupMode, string) {
	mode := ModeLocal
	if env.Systemd.Available && env.User.IsRoot {
		mode = ModeAgent
	}

	provider := "local"
	if env.Providers.Vault.Detected {
		provider = "vault"
	}
	if env.Providers.Azure.Detected {
		provider = "azure"
	}
	if env.Providers.AWS.Detected {
		provider = "aws"
	}

	return mode, provider
}
