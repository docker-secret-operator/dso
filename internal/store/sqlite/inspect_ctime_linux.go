//go:build linux

package sqlite

import (
	"os"
	"syscall"
)

// ctimeAvailable reports whether platformStat supplies device, inode and change
// time. On Linux they come from the kernel's stat structure.
const ctimeAvailable = true

// platformStat returns the device number, inode number and inode change time
// (ctime, in nanoseconds) of fi. ctime cannot be set from user space (unlike
// mtime, which utimes(2) and tools such as touch can reset), so it also moves
// when a writer restores the old modification time. It does NOT make same-tick
// writes on a coarse-resolution filesystem detectable: the kernel stamps ctime
// and mtime from the same clock, so both can be equal after a write that lands
// in the same timestamp tick as the earlier stat.
func platformStat(fi os.FileInfo) (dev, ino uint64, ctimeNs int64, ok bool) {
	st, isStat := fi.Sys().(*syscall.Stat_t)
	if !isStat {
		return 0, 0, 0, false
	}
	return uint64(st.Dev), uint64(st.Ino), st.Ctim.Nano(), true // #nosec G115 -- device/inode numbers are reinterpreted, never used as sizes
}
