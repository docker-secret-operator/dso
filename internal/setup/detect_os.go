package setup

import (
	"os"
	"runtime"
	"strings"
)

// detectOS returns facts about the host operating system. It reads
// /etc/os-release on Linux to identify distribution and version.
func detectOS() OSInfo {
	info := OSInfo{
		GOOS:         runtime.GOOS,
		Architecture: runtime.GOARCH,
	}
	if runtime.GOOS == "linux" {
		info.Distribution, info.Version = parseOSRelease()
	}
	return info
}

// parseOSRelease reads /etc/os-release and extracts ID and VERSION_ID.
func parseOSRelease() (distro, version string) {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if after, ok := strings.CutPrefix(line, "ID="); ok && distro == "" {
			distro = strings.Trim(after, `"'`)
		}
		if after, ok := strings.CutPrefix(line, "VERSION_ID="); ok && version == "" {
			version = strings.Trim(after, `"'`)
		}
	}
	return distro, version
}
