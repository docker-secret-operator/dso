// Package runtimedir establishes and uses DSO's runtime directory (/run/dso)
// without following attacker-planted filesystem objects.
//
// Security invariant for the canonical directory (agent running as root):
// owner root, group "dso" when that group exists (otherwise root), mode
// exactly 0750, never a symlink, always a directory. The directory is opened
// once with O_NOFOLLOW|O_DIRECTORY and every later decision is made on that
// descriptor (fstat/fchown/fchmod/openat), never on a path, so there is no
// check-then-use gap an attacker could race.
//
// Files inside the directory are opened relative to the pinned descriptor
// with O_NOFOLLOW|O_NONBLOCK and then validated with fstat on the opened
// object (regular file, expected owner, link count 1, restricted mode).
//
// Only Linux is supported. Other platforms return ErrUnsupported from every
// operation, so callers can never mistake "not enforced" for "enforced".
package runtimedir

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"strconv"
)

const (
	// CanonicalDir is the agent's runtime directory. /var/run is a symlink to
	// /run on systemd hosts, so /var/run/dso resolves to the same directory.
	CanonicalDir = "/run/dso"

	// GroupName is the group whose members may traverse the directory and
	// connect to the agent socket inside it.
	GroupName = "dso"

	// DirMode is the exact mode of the canonical directory: owner rwx, group
	// r-x (traverse only), no access for others, and no write for the group.
	DirMode os.FileMode = 0o750

	// FileMode is the most permissive mode a file in the directory may have.
	FileMode os.FileMode = 0o600
)

// ErrUnsupported is returned by every operation on a platform where the
// descriptor-based enforcement is not implemented (everything but Linux).
var ErrUnsupported = errors.New("runtimedir: not supported on this platform")

// ErrUnsafe matches every *UnsafeError.
var ErrUnsafe = errors.New("runtimedir: unsafe filesystem object")

// UnsafeError reports a filesystem object that the policy refuses to trust.
// The object is never modified, removed or followed.
type UnsafeError struct {
	Path   string
	Reason string
	Err    error // underlying errno, if any
}

func (e *UnsafeError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("unsafe %s: %s (%v)", e.Path, e.Reason, e.Err)
	}
	return fmt.Sprintf("unsafe %s: %s", e.Path, e.Reason)
}

// Is makes errors.Is(err, ErrUnsafe) true for any *UnsafeError.
func (e *UnsafeError) Is(target error) bool { return target == ErrUnsafe }

// Unwrap exposes the underlying errno.
func (e *UnsafeError) Unwrap() error { return e.Err }

// Policy describes what Ensure requires of, or establishes on, a directory.
type Policy struct {
	// OwnerUID is the uid that must own the directory. Ensure never chowns
	// the owner: a directory owned by anyone else is refused.
	OwnerUID uint32

	// Enforce makes Ensure correct the group and mode of an owner-matching
	// directory. It requires Mode. When false, Ensure only verifies.
	Enforce bool

	// GroupName is the group set on the directory when Enforce is true. When
	// it is empty, or the group does not exist, the group becomes gid 0.
	GroupName string

	// Mode is the exact mode established when Enforce is true.
	Mode os.FileMode

	// Create makes Ensure create a missing directory (one level, mode 0700
	// until the policy is applied).
	Create bool

	// RequireNoGroupOtherWrite makes Ensure refuse a directory whose group or
	// other write bits are set (the verify-only counterpart of Mode).
	RequireNoGroupOtherWrite bool

	// LookupGroup resolves GroupName. Nil uses os/user. Tests inject it.
	LookupGroup func(name string) (gid int, ok bool)
}

// FilePolicy describes what OpenRegular requires of the opened file.
type FilePolicy struct {
	// OwnerUID is the uid that must own the file.
	OwnerUID uint32

	// MaxMode is the most permissive mode the file may have; any permission
	// bit outside it makes the file unsafe.
	MaxMode os.FileMode
}

// CanonicalPolicyFor returns the policy for the canonical runtime directory
// for a process with the given effective uid.
//
// A root agent establishes the invariant (create, chown group, chmod 0750).
// A non-root process never modifies the canonical directory: it only accepts
// an existing directory it owns that is not group- or other-writable, which
// in practice means persistence is unavailable to it.
func CanonicalPolicyFor(euid int) Policy {
	if euid == 0 {
		return Policy{
			OwnerUID:                 0,
			Enforce:                  true,
			GroupName:                GroupName,
			Mode:                     DirMode,
			Create:                   true,
			RequireNoGroupOtherWrite: true,
		}
	}
	return Policy{
		OwnerUID:                 uint32(euid), // #nosec G115 -- euid is a non-negative uid from os.Geteuid
		RequireNoGroupOtherWrite: true,
	}
}

// CanonicalPolicy is CanonicalPolicyFor(os.Geteuid()).
func CanonicalPolicy() Policy { return CanonicalPolicyFor(os.Geteuid()) }

// CanonicalFilePolicyFor returns the file policy for files in the canonical
// directory for a process with the given effective uid.
func CanonicalFilePolicyFor(euid int) FilePolicy {
	return FilePolicy{OwnerUID: uint32(euid), MaxMode: FileMode} // #nosec G115 -- euid is a non-negative uid from os.Geteuid
}

// resolveGID returns the gid Ensure establishes for the policy.
func (p Policy) resolveGID() int {
	if p.GroupName == "" {
		return 0
	}
	if p.LookupGroup != nil {
		if gid, ok := p.LookupGroup(p.GroupName); ok {
			return gid
		}
		return 0
	}
	g, err := user.LookupGroup(p.GroupName)
	if err != nil {
		return 0
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil || gid < 0 {
		return 0
	}
	return gid
}
