//go:build linux

package agent

import "golang.org/x/sys/unix"

// tightenUmask sets the process umask to 0077 (block group+other bits) before
// creating the Unix socket file, closing the TOCTOU window where the kernel
// creates the file with loose permissions before os.Chmod can tighten them.
// Returns the previous umask so the caller can restore it.
func tightenUmask() int { return unix.Umask(0o077) }

// restoreUmask restores the umask saved by tightenUmask.
func restoreUmask(old int) { unix.Umask(old) }
