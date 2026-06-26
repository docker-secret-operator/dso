package setup

import "path/filepath"

const (
	dsoSystemConfig  = "/etc/dso/dso.yaml"
	dsoSystemService = "/etc/systemd/system/dso-agent.service"
)

// detectExistingDSO checks for a prior DSO installation. It inspects
// well-known config and service paths without executing any binaries.
func detectExistingDSO(cfg DetectorConfig) (ExistingDSOInfo, []DetectionWarning) {
	info := ExistingDSOInfo{}

	// System config (preferred location).
	if _, err := cfg.Stat(dsoSystemConfig); err == nil {
		info.Installed = true
		info.ConfigPath = dsoSystemConfig
	} else {
		// User-local fallback (~/.dso/dso.yaml).
		if home := homeDir(cfg); home != "" {
			userConfig := filepath.Join(home, ".dso", "dso.yaml")
			if _, err := cfg.Stat(userConfig); err == nil {
				info.Installed = true
				info.ConfigPath = userConfig
			}
		}
	}

	// Systemd service unit.
	if _, err := cfg.Stat(dsoSystemService); err == nil {
		info.Installed = true
		info.ServiceInstalled = true
	}

	// DSO agent binary.
	if _, err := cfg.LookPath("dso"); err == nil {
		info.Installed = true
		info.AgentInstalled = true
	}

	return info, nil
}
