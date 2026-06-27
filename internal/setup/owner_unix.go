//go:build linux || darwin

package setup

import (
	"os/user"
	"strconv"
	"syscall"
)

// ownerOfPath returns "username:groupname" for the given path.
// It is called during Apply to capture pre-operation ownership for rollback.
// Errors are silently discarded by callers — a missing owner just means
// rollback skips the chown step rather than failing Apply.
func ownerOfPath(path string) (string, error) {
	var s syscall.Stat_t
	if err := syscall.Stat(path, &s); err != nil {
		return "", err
	}
	uid := strconv.FormatUint(uint64(s.Uid), 10)
	gid := strconv.FormatUint(uint64(s.Gid), 10)

	u, err := user.LookupId(uid)
	if err != nil {
		// Fall back to numeric IDs so rollback has something usable.
		return uid + ":" + gid, nil
	}
	g, err := user.LookupGroupId(gid)
	if err != nil {
		return u.Username + ":", nil
	}
	return u.Username + ":" + g.Name, nil
}
