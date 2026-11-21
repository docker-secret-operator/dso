//go:build unix

package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// trustedOwner reports whether uid may own security-relevant plugin files and
// directories: root, or the uid the daemon itself runs as (a process already
// trusts what it owns). It is a variable so tests can exercise rejection
// without needing root.
var trustedOwner = func(uid uint32) bool {
	return uid == 0 || uid == uint32(os.Geteuid())
}

// openNoFollow opens path read-only refusing a symlink as the final component
// (O_NOFOLLOW). O_NONBLOCK keeps a FIFO planted at the path from hanging the
// daemon; the regular-file check that follows rejects it.
func openNoFollow(path string) (*os.File, error) {
	// #nosec G304 G703 -- path is the validated plugin or manifest path; the descriptor is fstat-checked afterwards
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
}

// checkFileTrust validates an already-open file's metadata (fstat, so it
// describes the object that will be read, not whatever the path names later):
// regular file, trusted owner, not group/other-writable, and (singleLink) no
// extra hard links that would let another path keep write access to the inode.
func checkFileTrust(f *os.File, what string, singleLink bool) (os.FileInfo, error) {
	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("%s: cannot stat: %w", what, err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s must be a regular file", what)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, fmt.Errorf("%s: cannot determine ownership", what)
	}
	if !trustedOwner(st.Uid) {
		return nil, fmt.Errorf("%s is owned by uid %d (must be root or the daemon user)", what, st.Uid)
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return nil, fmt.Errorf("%s is group- or world-writable (mode %04o)", what, fi.Mode().Perm())
	}
	if singleLink && uint64(st.Nlink) != 1 {
		return nil, fmt.Errorf("%s has %d hard links (must be exactly 1)", what, st.Nlink)
	}
	return fi, nil
}

// checkDirChainTrust walks dir (symlinks resolved) up to "/" and rejects any
// component owned by an untrusted uid or writable by group/other. Anyone who
// can write a directory in this chain can rename entries and swap the plugin
// or manifest. A world-writable ancestor is tolerated only when sticky (e.g.
// /tmp: other users cannot rename entries they do not own); the plugin
// directory itself must never be group/other-writable.
func checkDirChainTrust(dir string) error {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return fmt.Errorf("cannot resolve directory %s: %w", dir, err)
	}
	for cur, first := real, true; ; first = false {
		fi, err := os.Stat(cur) // #nosec G703 -- cur walks the symlink-resolved plugin directory chain; nothing is read or written
		if err != nil {
			return fmt.Errorf("cannot stat directory %s: %w", cur, err)
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("cannot determine ownership of %s", cur)
		}
		if !trustedOwner(st.Uid) {
			return fmt.Errorf("directory %s is owned by uid %d (must be root or the daemon user)", cur, st.Uid)
		}
		if fi.Mode().Perm()&0o022 != 0 {
			sticky := fi.Mode()&os.ModeSticky != 0
			if first || !sticky {
				return fmt.Errorf("directory %s is group- or world-writable (mode %04o)", cur, fi.Mode().Perm())
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return nil
		}
		cur = parent
	}
}
