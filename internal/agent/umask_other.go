//go:build !linux

package agent

import "net"

// listenUnixRestricted on non-Linux platforms (macOS, Windows) relies on the
// caller's explicit os.Chmod after Listen; the production target is Linux, where
// the file is additionally created under a 077 umask.
func listenUnixRestricted(path string) (net.Listener, error) {
	return net.Listen("unix", path)
}
