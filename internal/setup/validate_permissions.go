package setup

// validatePermissions checks that the current user has the access required for
// the requested setup mode. It reads pre-computed Environment facts only —
// no os/user, os.Stat, or LookPath calls.
func validatePermissions(env Environment, opts SetupOptions) []ValidationIssue {
	var issues []ValidationIssue

	mode := effectiveMode(env, opts)

	if mode == ModeAgent && !env.User.IsRoot && !opts.NonRoot {
		issues = append(issues, ValidationIssue{
			Severity: SeverityError,
			Category: CategoryPermissions,
			Code:     CodeAgentModeRequiresRoot,
			Message:  "agent mode requires root privileges to install system services",
			Recovery: []string{
				"Run the setup command with sudo: sudo dso setup",
				"Or use local mode: dso setup --mode local",
				"Or configure rootless access: dso setup --non-root",
			},
		})
	}

	if mode == ModeAgent && !env.Capabilities.SupportsSystemd {
		issues = append(issues, ValidationIssue{
			Severity: SeverityError,
			Category: CategoryPermissions,
			Code:     CodeAgentModeRequiresSystemd,
			Message:  "agent mode requires systemd to manage the DSO daemon",
			Recovery: []string{
				"Use local mode on systems without systemd: dso setup --mode local",
			},
		})
	}

	if env.Docker.BinaryFound && !env.Docker.SocketFound && !env.User.IsRoot {
		issues = append(issues, ValidationIssue{
			Severity: SeverityWarning,
			Category: CategoryPermissions,
			Code:     CodeDockerSocketInaccessible,
			Message:  "Docker binary found but the socket could not be accessed; ensure the current user is in the docker group",
		})
	}

	return issues
}

// effectiveMode resolves the setup mode that will actually be used.
// It mirrors the planner's fallback logic without producing any plan artefacts.
func effectiveMode(env Environment, opts SetupOptions) SetupMode {
	if opts.Mode != "" {
		return opts.Mode
	}
	if env.Capabilities.SupportsAgentMode {
		return ModeAgent
	}
	return ModeLocal
}
