//go:build !linux

package agent

import "net"

// readPeerIdentity is a no-op on non-Linux platforms, where SO_PEERCRED is not
// available. It reports errPeerCredUnsupported so the caller falls back to the
// socket's filesystem permissions. The agent's production target is Linux, where
// the full peer-credential authorization in peercred_linux.go applies.
func readPeerIdentity(_ net.Conn) (peerIdentity, error) {
	return peerIdentity{}, errPeerCredUnsupported
}
