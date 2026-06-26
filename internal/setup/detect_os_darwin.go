//go:build darwin

package setup

// parsePlatformOSInfo is a no-op on macOS. Distribution and version are not
// needed for DSO setup on Darwin — it only runs in local mode.
func parsePlatformOSInfo(_ func(string) ([]byte, error)) (distro, version string, warn *DetectionWarning) {
	return "", "", nil
}
