package setup

import (
	"context"
	"os/exec"
	"strings"
)

// detectDocker probes for a Docker installation and a reachable daemon.
// It never errors — absence or unreachability is recorded in DockerInfo.
func detectDocker(ctx context.Context, cfg DetectorConfig) (DockerInfo, []DetectionWarning) {
	info := DockerInfo{}

	path, err := cfg.LookPath("docker")
	if err != nil {
		return info, nil
	}
	info.BinaryFound = true
	info.BinaryPath = path

	for _, sp := range cfg.DockerSocketPaths {
		if _, err := cfg.Stat(sp); err == nil {
			info.SocketFound = true
			info.SocketPath = sp
			break
		}
	}

	// Use the resolved binary path so tests with fake paths get predictable behaviour.
	vCtx, cancel := context.WithTimeout(ctx, cfg.DockerTimeout)
	defer cancel()

	out, err := exec.CommandContext(vCtx, info.BinaryPath, "version", "--format", "{{.Server.Version}}").Output()
	if err == nil {
		info.Version = strings.TrimSpace(string(out))
		info.DaemonReachable = true
		return info, nil
	}

	// Binary found but daemon is not reachable — emit a soft warning.
	return info, []DetectionWarning{{
		Code:    "docker_daemon_unreachable",
		Message: "docker binary found but daemon did not respond: " + err.Error(),
	}}
}
