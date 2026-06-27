package setup

import "fmt"

// validateExisting inspects the prior DSO installation and surfaces what action
// setup will take: fresh install, upgrade, repair, or service registration.
// It never reads the filesystem — all facts come from env.ExistingDSO.
func validateExisting(env Environment, opts SetupOptions) []ValidationIssue {
	existing := env.ExistingDSO
	if !existing.Installed {
		return nil
	}

	var issues []ValidationIssue

	if existing.ConfigPath != "" {
		action := "upgrade the existing installation"
		if existing.Version != "" {
			action = fmt.Sprintf("upgrade from v%s", existing.Version)
		}
		issues = append(issues, ValidationIssue{
			Severity: SeverityInfo,
			Category: CategoryConfiguration,
			Code:     CodeExistingInstallationFound,
			Message:  fmt.Sprintf("DSO configuration found at %s — setup will %s", existing.ConfigPath, action),
		})
	}

	if existing.ServiceInstalled && !existing.AgentInstalled {
		issues = append(issues, ValidationIssue{
			Severity: SeverityWarning,
			Category: CategoryConfiguration,
			Code:     CodeServiceWithoutAgent,
			Message:  "systemd service file exists but the dso binary is not in PATH — the prior installation may be incomplete",
		})
	}

	mode := effectiveMode(env, opts)
	if existing.AgentInstalled && !existing.ServiceInstalled && mode == ModeAgent {
		issues = append(issues, ValidationIssue{
			Severity: SeverityInfo,
			Category: CategoryConfiguration,
			Code:     CodeAgentWithoutService,
			Message:  "DSO agent binary found but no systemd service — setup will register the service",
		})
	}

	return issues
}
