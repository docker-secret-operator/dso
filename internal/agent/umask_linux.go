//go:build linux

package agent

import (
	"sync"

	"golang.org/x/sys/unix"
)

// umaskMu serialises every tighten/restore PAIR. The umask is process-global state:
// without it two goroutines creating sockets at the same time interleave as
//
//	A: old=022, set 077 | B: old=077, set 077 | A: restore 022 | B: Listen (umask 022)
//
// and B's socket is created world-accessible (0755) until its os.Chmod runs. The
// lock is held from tightenUmask until the matching restoreUmask, so the umask a
// Listen sees is always 077 and the value restored is always the true original.
var umaskMu sync.Mutex

// tightenUmask sets the process umask to 0077 (block group+other bits) before
// creating the Unix socket file, closing the TOCTOU window where the kernel
// creates the file with loose permissions before os.Chmod can tighten them.
// Returns the previous umask so the caller can restore it. The caller MUST call
// restoreUmask exactly once with the returned value: the umask lock is held until
// then.
func tightenUmask() int {
	umaskMu.Lock()
	return unix.Umask(0o077)
}

// restoreUmask restores the umask saved by tightenUmask and releases the lock.
func restoreUmask(old int) {
	unix.Umask(old)
	umaskMu.Unlock()
}
