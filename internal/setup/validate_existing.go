package setup

import "fmt"

// validateExisting inspects the prior DSO installation and surfaces what
// action setup will take: fresh install, upgrade, repair, or service
// registration. It never reads the filesystem — all facts come from env.
func validateExisting(env Environment, opts SetupOptions) ([]ValidationError, []ValidationWarning, []ValidationSuggestion) {
	existing := env.ExistingDSO

	if !existing.Installed {
		return nil, nil, nil // fresh install — nothing to report
	}

	var warns []ValidationWarning
	var suggestions []ValidationSuggestion

	if existing.ConfigPath != "" {
		action := "upgrade the existing installation"
		if existing.Version != "" {
			action = fmt.Sprintf("upgrade from v%s", existing.Version)
		}
		suggestions = append(suggestions, ValidationSuggestion{
			Code:    "existing_installation_found",
			Message: fmt.Sprintf("DSO configuration found at %s — setup will %s", existing.ConfigPath, action),
		})
	}

	// Service file present but binary missing: installation is incomplete.
	if existing.ServiceInstalled && !existing.AgentInstalled {
		warns = append(warns, ValidationWarning{
			Code:    "service_without_agent",
			Message: "systemd service file exists but the dso binary is not in PATH — the prior installation may be incomplete",
		})
	}

	// Binary present but no service file, and agent mode is requested:
	// setup will register the service for the first time.
	mode := effectiveMode(env, opts)
	if existing.AgentInstalled && !existing.ServiceInstalled && mode == ModeAgent {
		suggestions = append(suggestions, ValidationSuggestion{
			Code:    "agent_without_service",
			Message: "DSO agent binary found but no systemd service — setup will register the service",
		})
	}

	return nil, warns, suggestions
}
