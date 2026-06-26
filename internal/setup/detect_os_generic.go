//go:build !linux && !darwin

package setup

// parsePlatformOSInfo is a stub for platforms other than Linux and macOS.
func parsePlatformOSInfo(_ func(string) ([]byte, error)) (distro, version string, warn *DetectionWarning) {
	return "", "", nil
}
