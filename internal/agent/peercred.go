package agent

import (
	"errors"
	"os/user"
	"strconv"
)

// errPeerCredUnsupported is returned by readPeerIdentity on platforms that cannot
// read peer credentials from a Unix domain socket (anything other than Linux).
// On such platforms the IPC socket falls back to filesystem-permission gating
// only. Production deployments run on Linux, where full SO_PEERCRED-based
// authorization applies (see peercred_linux.go).
var errPeerCredUnsupported = errors.New("peer credential check not supported on this platform")

// peerIdentity captures the authenticated credentials of a Unix-socket peer,
// read from SO_PEERCRED on Linux. It is used both for least-privilege
// authorization and for the per-connection audit trail (SEC-C2).
type peerIdentity struct {
	pid int32
	uid uint32
	gid uint32
}

// peerAuthorized implements least-privilege authorization for the IPC socket.
//
// Exact policy (deny by default; allow only if one of the following holds):
//  1. The peer's UID is 0 (root).
//  2. The peer's UID equals the agent's own UID (the socket owner).
//  3. The dso group exists (dsoGID >= 0) AND the peer's primary GID == dsoGID.
//  4. The dso group exists AND the peer's user is a supplementary member of it.
//
// When the dso group does not exist (dsoGID < 0), only cases 1 and 2 apply,
// matching the 0600 root-only socket fallback. The check is conjunction-free in
// the deny direction: any peer not matching a rule is rejected. There is no path
// by which a non-root, non-owner, non-dso user is admitted, so the socket grants
// no privilege beyond "root or dso group" — the same set the socket's
// 0660 root:dso filesystem permissions intend, validated at the protocol layer.
//
// Note: with CGO disabled (the default for the release binaries), os/user reads
// /etc/passwd and /etc/group directly and does not consult NSS sources such as
// LDAP/SSSD. Users defined only in those sources will fail to resolve here and
// be denied — a fail-closed outcome, never an escalation.
func peerAuthorized(p peerIdentity, selfUID int, dsoGID int) bool {
	if p.uid == 0 || int(p.uid) == selfUID {
		return true
	}
	if dsoGID < 0 {
		return false
	}
	if int(p.gid) == dsoGID {
		return true
	}
	// Supplementary group membership: resolve the peer's user and enumerate the
	// groups it belongs to. (os/user.Group has no member list, so we ask the user
	// for its group IDs — this captures both primary and supplementary membership.)
	u, err := user.LookupId(strconv.FormatUint(uint64(p.uid), 10))
	if err != nil {
		return false
	}
	gids, err := u.GroupIds()
	if err != nil {
		return false
	}
	target := strconv.Itoa(dsoGID)
	for _, g := range gids {
		if g == target {
			return true
		}
	}
	return false
}
