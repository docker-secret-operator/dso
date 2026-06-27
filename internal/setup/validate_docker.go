package setup

// validateDocker checks that Docker is installed and reachable.
// All DSO deployment modes require Docker; the error messages guide the user
// toward the specific failure rather than a generic "Docker not working".
func validateDocker(env Environment, _ SetupOptions) ([]ValidationError, []ValidationWarning) {
	var errs []ValidationError
	var warns []ValidationWarning

	if !env.Docker.BinaryFound {
		errs = append(errs, ValidationError{
			Code:    "docker_not_installed",
			Message: "Docker is not installed; DSO requires Docker to manage secrets",
			Recovery: []string{
				"Install Docker from https://docs.docker.com/get-docker/",
				"Verify with: docker --version",
			},
		})
		// Without a binary, socket and daemon checks are irrelevant.
		return errs, warns
	}

	if !env.Docker.DaemonReachable {
		if env.Docker.SocketFound {
			// Socket exists but the daemon did not respond — likely stopped.
			errs = append(errs, ValidationError{
				Code:    "docker_daemon_not_running",
				Message: "Docker socket found but the daemon is not responding",
				Recovery: []string{
					"Start Docker Desktop, or run: sudo systemctl start docker",
				},
			})
		} else {
			// Binary found, no socket reachable — likely a permissions gap.
			errs = append(errs, ValidationError{
				Code:    "docker_daemon_unreachable",
				Message: "Docker daemon is not reachable; no accessible socket found",
				Recovery: []string{
					"Ensure Docker is running",
					"Add the current user to the docker group: sudo usermod -aG docker $USER",
					"Or run setup as root",
				},
			})
		}
	}

	return errs, warns
}
