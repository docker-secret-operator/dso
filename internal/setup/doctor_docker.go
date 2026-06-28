package setup

import (
	"context"
	"os"
	"os/exec"
)

// DockerChecks covers DSO-DOCTOR-001 through DSO-DOCTOR-003.
// Every OS call is injected so tests never require a running Docker daemon.
type DockerChecks struct {
	socketPath   string
	lookupBinary func(string) (string, error)
	runVersion   func(context.Context) error
	statSocket   func(string) (os.FileInfo, error)
}

func newDockerChecks(socketPath string) *DockerChecks {
	return &DockerChecks{
		socketPath:   socketPath,
		lookupBinary: exec.LookPath,
		runVersion: func(ctx context.Context) error {
			return exec.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Version}}").Run()
		},
		statSocket: os.Stat,
	}
}

func (dc *DockerChecks) run(ctx context.Context) []DoctorCheck {
	return []DoctorCheck{
		dc.checkBinaryInstalled(),
		dc.checkDaemonReachable(ctx),
		dc.checkSocketAccessible(),
	}
}

// DSO-DOCTOR-001: Docker CLI binary present in PATH.
func (dc *DockerChecks) checkBinaryInstalled() DoctorCheck {
	const id = "DSO-DOCTOR-001"
	const name = "Docker binary"
	const desc = "Docker CLI binary must be present in PATH"

	if _, err := dc.lookupBinary("docker"); err != nil {
		return failCheck(id, name, desc,
			"docker not found in PATH",
			"Docker is not installed on this system",
			DoctorCritical, DoctorCatDocker,
			"Install Docker Engine: https://docs.docker.com/engine/install/",
			"Confirm with: which docker",
		)
	}
	return passCheck(id, name, desc, "docker binary found in PATH", DoctorCatDocker)
}

// DSO-DOCTOR-002: Docker daemon responds to API calls.
func (dc *DockerChecks) checkDaemonReachable(ctx context.Context) DoctorCheck {
	const id = "DSO-DOCTOR-002"
	const name = "Docker daemon"
	const desc = "Docker daemon must be running and reachable"

	if err := dc.runVersion(ctx); err != nil {
		return failCheck(id, name, desc,
			"docker version command failed: "+err.Error(),
			"Docker daemon is not running or the current user cannot reach the socket",
			DoctorCritical, DoctorCatDocker,
			"Start the daemon:  sudo systemctl start docker",
			"Add user to group: sudo usermod -aG docker $USER && newgrp docker",
			"Verify with:       docker version",
		)
	}
	return passCheck(id, name, desc, "Docker daemon is running and reachable", DoctorCatDocker)
}

// DSO-DOCTOR-003: Docker socket file present on disk.
func (dc *DockerChecks) checkSocketAccessible() DoctorCheck {
	const id = "DSO-DOCTOR-003"
	const name = "Docker socket"
	desc := "Docker socket must exist at " + dc.socketPath

	if _, err := dc.statSocket(dc.socketPath); err != nil {
		return failCheck(id, name, desc,
			"socket not found at "+dc.socketPath+": "+err.Error(),
			"Docker socket is absent — Docker may not be installed or may have failed to start",
			DoctorHigh, DoctorCatDocker,
			"Install or start Docker: https://docs.docker.com/engine/install/",
			"Check socket path: ls -la "+dc.socketPath,
		)
	}
	return passCheck(id, name, desc, "Docker socket present at "+dc.socketPath, DoctorCatDocker)
}
