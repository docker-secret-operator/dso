//go:build linux

package agent

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// readPeerIdentity reads SO_PEERCRED from a Unix domain socket connection (Linux).
func readPeerIdentity(conn net.Conn) (peerIdentity, error) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return peerIdentity{}, fmt.Errorf("connection is not a unix socket")
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return peerIdentity{}, fmt.Errorf("failed to access raw conn: %w", err)
	}
	var cred *unix.Ucred
	var credErr error
	ctrlErr := raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	})
	if ctrlErr != nil {
		return peerIdentity{}, fmt.Errorf("control failed: %w", ctrlErr)
	}
	if credErr != nil {
		return peerIdentity{}, fmt.Errorf("getsockopt SO_PEERCRED failed: %w", credErr)
	}
	return peerIdentity{pid: cred.Pid, uid: cred.Uid, gid: cred.Gid}, nil
}
