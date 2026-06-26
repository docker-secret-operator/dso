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
	ReadFile          func(string) ([]byte, error)
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
			ReadFile: os.ReadFile,
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
// Non-fatal problems are recorded in env.DetectionWarnings.
func (d *Detector) Detect(ctx context.Context) (*Environment, error) {
	env := &Environment{Timestamp: time.Now()}

	var warnings []DetectionWarning

	osInfo, osWarns := detectOS(d.cfg)
	env.OS = osInfo
	warnings = append(warnings, osWarns...)

	userInfo, userWarns := detectUser()
	env.User = userInfo
	warnings = append(warnings, userWarns...)

	dockerInfo, dockerWarns := detectDocker(ctx, d.cfg)
	env.Docker = dockerInfo
	warnings = append(warnings, dockerWarns...)

	systemdInfo, systemdWarns := detectSystemd(ctx, d.cfg)
	env.Systemd = systemdInfo
	warnings = append(warnings, systemdWarns...)

	providers, providerWarns := detectProviders(d.cfg)
	env.Providers = providers
	warnings = append(warnings, providerWarns...)

	existingDSO, existingWarns := detectExistingDSO(d.cfg)
	env.ExistingDSO = existingDSO
	warnings = append(warnings, existingWarns...)

	env.DetectionWarnings = warnings
	env.Capabilities = computeCapabilities(env)

	return env, nil
}

// computeCapabilities derives what this environment can support from raw facts.
// All later phases use Capabilities rather than re-examining raw fields.
func computeCapabilities(env *Environment) Capabilities {
	return Capabilities{
		SupportsSystemd:   env.Systemd.Available,
		SupportsDocker:    env.Docker.DaemonReachable,
		SupportsAgentMode: env.Systemd.Available && env.User.IsRoot,
		SupportsLocalMode: true, // always available
	}
}
