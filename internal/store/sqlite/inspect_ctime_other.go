//go:build !linux

package sqlite

import "os"

// ctimeAvailable is false off Linux: device, inode and change time are not read
// from the platform stat structure here, so the immutable verification
// signature falls back to size and modification time. Path identity is still
// checked separately with os.SameFile (which compares device and inode where
// the platform supports it), and sidecar absence is still re-checked. The
// weaker signature means a same-size write that leaves the modification time
// unchanged (an explicit mtime reset, or a coarse-resolution filesystem) is not
// detected on these platforms.
const ctimeAvailable = false

func platformStat(os.FileInfo) (dev, ino uint64, ctimeNs int64, ok bool) {
	return 0, 0, 0, false
}
