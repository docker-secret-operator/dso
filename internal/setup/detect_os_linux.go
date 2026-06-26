//go:build linux

package setup

import "strings"

// parsePlatformOSInfo reads /etc/os-release to extract distribution and version.
// readFile is injectable so tests can supply arbitrary content.
func parsePlatformOSInfo(readFile func(string) ([]byte, error)) (distro, version string, warn *DetectionWarning) {
	data, err := readFile("/etc/os-release")
	if err != nil {
		return "", "", &DetectionWarning{
			Code:    "os_release_read_failed",
			Message: "cannot read /etc/os-release: " + err.Error(),
		}
	}
	distro, version = parseOSRelease(data)
	return distro, version, nil
}

// parseOSRelease extracts ID and VERSION_ID from os-release file content.
func parseOSRelease(data []byte) (distro, version string) {
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
