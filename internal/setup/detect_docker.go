package setup

import (
	"context"
	"os/exec"
	"strings"
)

// detectDocker probes for a Docker installation and a reachable daemon.
// It never returns an error — a missing Docker binary or unreachable daemon
// is recorded as false in DockerInfo.
func detectDocker(ctx context.Context, cfg DetectorConfig) DockerInfo {
	info := DockerInfo{}

	path, err := cfg.LookPath("docker")
	if err != nil {
		return info
	}
	info.BinaryFound = true
	info.BinaryPath = path

	for _, sp := range cfg.DockerSocketPaths {
		if _, err := cfg.Stat(sp); err == nil {
			info.SocketPath = sp
			break
		}
	}

	// Use the resolved binary path so the mocked path is honoured in tests.
	vCtx, cancel := context.WithTimeout(ctx, cfg.DockerTimeout)
	defer cancel()

	out, err := exec.CommandContext(vCtx, info.BinaryPath, "version", "--format", "{{.Server.Version}}").Output()
	if err == nil {
		info.Version = strings.TrimSpace(string(out))
		info.DaemonAvailable = true
	}

	return info
}
