package setup

// validatePermissions checks that the current user has the access required
// for the requested setup mode. It consumes pre-computed Environment facts;
// it never calls os/user, os.Stat, or LookPath itself.
func validatePermissions(env Environment, opts SetupOptions) ([]ValidationError, []ValidationWarning) {
	var errs []ValidationError
	var warns []ValidationWarning

	mode := effectiveMode(env, opts)

	// Agent mode requires root to install system services and write /etc/dso.
	if mode == ModeAgent && !env.User.IsRoot && !opts.NonRoot {
		errs = append(errs, ValidationError{
			Code:    "agent_mode_requires_root",
			Message: "agent mode requires root privileges to install system services",
			Recovery: []string{
				"Run the setup command with sudo: sudo dso setup",
				"Or use local mode: dso setup --mode local",
				"Or configure rootless access after a root install: dso setup --non-root",
			},
		})
	}

	// Agent mode requires systemd to manage the DSO daemon lifecycle.
	if mode == ModeAgent && !env.Capabilities.SupportsSystemd {
		errs = append(errs, ValidationError{
			Code:    "agent_mode_requires_systemd",
			Message: "agent mode requires systemd to manage the DSO daemon",
			Recovery: []string{
				"Use local mode on systems without systemd: dso setup --mode local",
			},
		})
	}

	// Warn when the docker binary is present but no socket was stat'd successfully.
	// This is a soft warning because it may be a transient permission issue.
	if env.Docker.BinaryFound && !env.Docker.SocketFound && !env.User.IsRoot {
		warns = append(warns, ValidationWarning{
			Code:    "docker_socket_inaccessible",
			Message: "Docker binary found but the socket could not be accessed; ensure the current user is in the docker group",
		})
	}

	return errs, warns
}

// effectiveMode resolves the mode that will actually be used, mirroring the
// logic in plan() without generating any plan artefacts.
func effectiveMode(env Environment, opts SetupOptions) SetupMode {
	if opts.Mode != "" {
		return opts.Mode
	}
	if env.Capabilities.SupportsAgentMode {
		return ModeAgent
	}
	return ModeLocal
}
