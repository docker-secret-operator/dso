package setup

import (
	"path/filepath"
)

const (
	dsoSystemConfig  = "/etc/dso/dso.yaml"
	dsoSystemService = "/etc/systemd/system/dso-agent.service"
)

// detectExistingDSO checks for a prior DSO installation. It inspects
// well-known config and service paths without executing any binaries.
func detectExistingDSO(cfg DetectorConfig) ExistingDSOInfo {
	info := ExistingDSOInfo{}

	// System config (preferred).
	if _, err := cfg.Stat(dsoSystemConfig); err == nil {
		info.Found = true
		info.ConfigPath = dsoSystemConfig
	} else {
		// User-local fallback (~/.dso/dso.yaml).
		home := homeDir(cfg)
		if home != "" {
			userConfig := filepath.Join(home, ".dso", "dso.yaml")
			if _, err := cfg.Stat(userConfig); err == nil {
				info.Found = true
				info.ConfigPath = userConfig
			}
		}
	}

	// Systemd service unit.
	if _, err := cfg.Stat(dsoSystemService); err == nil {
		info.Found = true
		info.ServicePath = dsoSystemService
	}

	return info
}
