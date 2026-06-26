package setup

import "runtime"

// detectOS returns facts about the host operating system. Platform-specific
// details (distribution, version) are resolved by parsePlatformOSInfo, which
// is implemented in detect_os_linux.go, detect_os_darwin.go, etc.
func detectOS(cfg DetectorConfig) (OSInfo, []DetectionWarning) {
	info := OSInfo{
		GOOS:         runtime.GOOS,
		Architecture: runtime.GOARCH,
	}

	distro, version, warn := parsePlatformOSInfo(cfg.ReadFile)
	info.Distribution = distro
	info.Version = version

	if warn != nil {
		return info, []DetectionWarning{*warn}
	}
	return info, nil
}
