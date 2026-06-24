// Package paths provides platform-appropriate default paths for DSO runtime files.
package paths

import "runtime"

// DefaultSocketPath returns the DSO IPC socket path for the current OS.
//
// Linux uses /run (a tmpfs RAM disk managed by systemd).
// macOS does not mount /run; /var/run is the conventional equivalent
// (resolves to /private/var/run and is writable by root).
func DefaultSocketPath() string {
	if runtime.GOOS == "darwin" {
		return "/var/run/dso/dso.sock"
	}
	return "/run/dso/dso.sock"
}

// DefaultDriverSocketPath returns the Docker Secret Driver plugin socket path
// for the current OS.
func DefaultDriverSocketPath() string {
	if runtime.GOOS == "darwin" {
		return "/var/run/docker/plugins/dso.sock"
	}
	return "/run/docker/plugins/dso.sock"
}
