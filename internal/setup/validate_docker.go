package setup

// validateDocker checks that Docker is installed and reachable.
// All DSO deployment modes require Docker; the error codes guide Doctor/Repair.
func validateDocker(env Environment, _ SetupOptions) []ValidationIssue {
	if !env.Docker.BinaryFound {
		return []ValidationIssue{{
			Severity: SeverityError,
			Category: CategoryDocker,
			Code:     CodeDockerNotInstalled,
			Message:  "Docker is not installed; DSO requires Docker to manage secrets",
			Recovery: []string{
				"Install Docker from https://docs.docker.com/get-docker/",
				"Verify with: docker --version",
			},
		}}
	}

	if !env.Docker.DaemonReachable {
		if env.Docker.SocketFound {
			return []ValidationIssue{{
				Severity: SeverityError,
				Category: CategoryDocker,
				Code:     CodeDockerDaemonNotRunning,
				Message:  "Docker socket found but the daemon is not responding",
				Recovery: []string{
					"Start Docker Desktop, or run: sudo systemctl start docker",
				},
			}}
		}
		return []ValidationIssue{{
			Severity: SeverityError,
			Category: CategoryDocker,
			Code:     CodeDockerDaemonUnreachable,
			Message:  "Docker daemon is not reachable; no accessible socket found",
			Recovery: []string{
				"Ensure Docker is running",
				"Add the current user to the docker group: sudo usermod -aG docker $USER",
				"Or run setup as root",
			},
		}}
	}

	return nil
}
