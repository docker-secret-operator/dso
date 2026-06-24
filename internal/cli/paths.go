package cli

import "github.com/docker-secret-operator/dso/internal/paths"

// DefaultSocketPath returns the platform-appropriate DSO IPC socket path.
// On Linux: /run/dso/dso.sock  — On macOS: /var/run/dso/dso.sock
func DefaultSocketPath() string {
	return paths.DefaultSocketPath()
}

// DefaultDriverSocketPath returns the platform-appropriate Docker Secret Driver
// plugin socket path.
// On Linux: /run/docker/plugins/dso.sock  — On macOS: /var/run/docker/plugins/dso.sock
func DefaultDriverSocketPath() string {
	return paths.DefaultDriverSocketPath()
}
