//go:build linux

package runtimedir

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

// Dir is a pinned, validated directory. It owns one file descriptor
// (O_CLOEXEC); Close releases it. All methods are safe for concurrent use.
type Dir struct {
	mu   sync.Mutex
	fd   int
	path string
}

// Path returns the path the directory was opened by (for messages only; it
// is never used to reach the directory again).
func (d *Dir) Path() string { return d.path }

// Close releases the directory descriptor. It is idempotent.
func (d *Dir) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.fd < 0 {
		return nil
	}
	fd := d.fd
	d.fd = -1
	return unix.Close(fd)
}

// descriptor returns the pinned descriptor or an error if Close was called.
func (d *Dir) descriptor() (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.fd < 0 {
		return -1, fmt.Errorf("runtimedir: %s: directory is closed", d.path)
	}
	return d.fd, nil
}

func openDirNoFollow(path string) (int, error) {
	for {
		fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		return fd, err
	}
}

// Ensure opens path as a directory without following a symlink at its final
// component, validates the opened object with fstat, and (for Policy.Enforce)
// corrects its group and mode through the descriptor.
//
// It refuses, without modifying them, a symlink, a non-directory, and a
// directory owned by anyone other than Policy.OwnerUID. Ownership is never
// changed: only the group and the mode of an owner-matching directory are.
func Ensure(path string, p Policy) (*Dir, error) {
	if path == "" || !filepath.IsAbs(path) {
		return nil, fmt.Errorf("runtimedir: path %q must be absolute", path)
	}
	if p.Enforce && (p.Mode == 0 || p.Mode&^0o777 != 0) {
		return nil, fmt.Errorf("runtimedir: enforcing %s needs a plain permission Mode, got %#o", path, uint32(p.Mode))
	}

	fd, err := openDirNoFollow(path)
	if errors.Is(err, unix.ENOENT) && p.Create {
		// One level only, private until the policy is applied below. EEXIST
		// means another process created it first; validate what is there.
		if merr := unix.Mkdir(path, 0o700); merr != nil && !errors.Is(merr, unix.EEXIST) {
			return nil, fmt.Errorf("runtimedir: create %s: %w", path, merr)
		}
		fd, err = openDirNoFollow(path)
	}
	if err != nil {
		if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
			return nil, &UnsafeError{Path: path, Reason: "not a directory, or a symbolic link", Err: err}
		}
		return nil, fmt.Errorf("runtimedir: open %s: %w", path, err)
	}

	d := &Dir{fd: fd, path: path}
	if err := d.validate(p); err != nil {
		_ = d.Close()
		return nil, err
	}
	return d, nil
}

func (d *Dir) validate(p Policy) error {
	var st unix.Stat_t
	if err := unix.Fstat(d.fd, &st); err != nil {
		return fmt.Errorf("runtimedir: fstat %s: %w", d.path, err)
	}
	if st.Uid != p.OwnerUID {
		return &UnsafeError{Path: d.path, Reason: fmt.Sprintf("owned by uid %d, expected uid %d (ownership is never changed)", st.Uid, p.OwnerUID)}
	}

	if p.Enforce {
		wantGID := p.resolveGID()
		if int(st.Gid) != wantGID {
			if err := unix.Fchown(d.fd, -1, wantGID); err != nil {
				return fmt.Errorf("runtimedir: set group of %s to gid %d: %w", d.path, wantGID, err)
			}
		}
		want := uint32(p.Mode.Perm())
		if st.Mode&0o7777 != want {
			if err := unix.Fchmod(d.fd, want); err != nil {
				return fmt.Errorf("runtimedir: set mode of %s to %04o: %w", d.path, want, err)
			}
		}
		if err := unix.Fstat(d.fd, &st); err != nil {
			return fmt.Errorf("runtimedir: re-fstat %s: %w", d.path, err)
		}
		if st.Uid != p.OwnerUID || int(st.Gid) != wantGID || st.Mode&0o7777 != want {
			return &UnsafeError{Path: d.path, Reason: fmt.Sprintf("could not establish uid %d gid %d mode %04o (found uid %d gid %d mode %04o)",
				p.OwnerUID, wantGID, want, st.Uid, st.Gid, st.Mode&0o7777)}
		}
		return nil
	}

	if p.RequireNoGroupOtherWrite && st.Mode&0o022 != 0 {
		return &UnsafeError{Path: d.path, Reason: fmt.Sprintf("writable by group or others (mode %04o)", st.Mode&0o7777)}
	}
	return nil
}

// OpenRegular opens name relative to the pinned directory and validates the
// opened object, not a path. flags are os.O_* flags (O_RDONLY, O_WRONLY,
// O_APPEND, O_CREATE...); perm applies only when the file is created.
//
// The open uses O_NOFOLLOW (a symlink is refused), O_NONBLOCK (a FIFO can
// never hang the caller) and O_CLOEXEC. After fstat confirms a regular file
// with the expected owner, link count 1 and no mode bits beyond fp.MaxMode,
// O_NONBLOCK is cleared. Anything else is closed untouched and reported as an
// *UnsafeError. A missing file is reported as an error satisfying
// errors.Is(err, os.ErrNotExist).
func (d *Dir) OpenRegular(name string, flags int, perm os.FileMode, fp FilePolicy) (*os.File, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsRune(name, '/') {
		return nil, fmt.Errorf("runtimedir: %q is not a plain file name", name)
	}
	dfd, err := d.descriptor()
	if err != nil {
		return nil, err
	}
	full := filepath.Join(d.path, name)

	oflags := flags | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC | unix.O_NOCTTY
	var fd int
	for {
		fd, err = unix.Openat(dfd, name, oflags, uint32(perm.Perm()))
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if err != nil {
		switch {
		case errors.Is(err, unix.ELOOP):
			return nil, &UnsafeError{Path: full, Reason: "is a symbolic link", Err: err}
		case errors.Is(err, unix.ENXIO):
			return nil, &UnsafeError{Path: full, Reason: "is a FIFO", Err: err}
		case errors.Is(err, unix.EISDIR):
			return nil, &UnsafeError{Path: full, Reason: "is a directory", Err: err}
		}
		return nil, fmt.Errorf("runtimedir: open %s: %w", full, err)
	}

	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("runtimedir: fstat %s: %w", full, err)
	}
	if reason := fileProblem(&st, fp); reason != "" {
		_ = unix.Close(fd)
		return nil, &UnsafeError{Path: full, Reason: reason}
	}

	// A validated regular file: restore blocking semantics, keeping O_APPEND.
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFL, flags&unix.O_APPEND); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("runtimedir: clear O_NONBLOCK on %s: %w", full, err)
	}
	return os.NewFile(uintptr(fd), full), nil
}

// fileProblem returns why st describes an untrustworthy file, or "".
func fileProblem(st *unix.Stat_t, fp FilePolicy) string {
	switch st.Mode & unix.S_IFMT {
	case unix.S_IFREG:
	case unix.S_IFIFO:
		return "is a FIFO"
	case unix.S_IFDIR:
		return "is a directory"
	case unix.S_IFCHR, unix.S_IFBLK:
		return "is a device file"
	case unix.S_IFLNK:
		return "is a symbolic link"
	default:
		return fmt.Sprintf("is not a regular file (type %#o)", st.Mode&unix.S_IFMT)
	}
	if st.Uid != fp.OwnerUID {
		return fmt.Sprintf("owned by uid %d, expected uid %d", st.Uid, fp.OwnerUID)
	}
	if uint64(st.Nlink) != 1 {
		return fmt.Sprintf("has %d hard links, expected 1", uint64(st.Nlink))
	}
	// setuid, setgid and sticky have no business on an event log, and
	// FilePolicy.MaxMode only describes the rwx permission bits, so they are
	// rejected outright (they would otherwise pass an 0o777 mask).
	if special := st.Mode & 0o7000; special != 0 {
		return fmt.Sprintf("has special permission bits (mode %04o: setuid, setgid and sticky are not allowed)", st.Mode&0o7777)
	}
	if extra := os.FileMode(st.Mode&0o777) &^ fp.MaxMode.Perm(); extra != 0 {
		return fmt.Sprintf("mode %04o exceeds the allowed %04o", st.Mode&0o777, uint32(fp.MaxMode.Perm()))
	}
	return ""
}
