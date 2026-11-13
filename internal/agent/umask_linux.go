//go:build linux

package agent

import (
	"net"
	"sync"

	"golang.org/x/sys/unix"
)

// umaskMu serialises every temporary umask change made by this package.
//
// Umask is PROCESS-WIDE state, not goroutine-local. Without this lock two
// goroutines creating sockets at once interleave as
//
//	A: old=022, set 077 | B: old=077, set 077 | A: restore 022 | B: Listen (umask 022)
//
// and B's socket is created world-accessible (0755) until its os.Chmod runs. The
// lock is held for the whole scope, so the umask a Listen sees is always 077 and
// the value restored is always the true original.
//
// Residual, accepted effect: while the scope is active, an UNRELATED goroutine
// that creates a file or directory in this process gets the 077 mask. 077 is more
// restrictive than any normal process mask, so such a file can only end up with
// fewer permission bits than requested (never more) for the microseconds that
// net.Listen takes. Files whose mode matters are chmod'ed explicitly by their
// creators. This is a correctness (not an exposure) trade-off and is why the scope
// must stay as small as one system call.
var umaskMu sync.Mutex

// withUmask runs fn with the process umask set to mask and restores the previous
// umask and releases the lock on every exit path, including a panic in fn. It is
// the ONLY way this package changes the umask; callers cannot forget to restore.
// fn must be short and must not call withUmask (the lock is not reentrant).
func withUmask(mask int, fn func()) {
	umaskMu.Lock()
	defer umaskMu.Unlock()
	old := unix.Umask(mask)
	defer unix.Umask(old)
	fn()
}

// listenUnixRestricted creates a Unix socket whose file is born with no group or
// other permission bits (umask 077), closing the window between net.Listen
// creating the file and the caller's explicit os.Chmod.
func listenUnixRestricted(path string) (net.Listener, error) {
	var (
		l   net.Listener
		err error
	)
	withUmask(0o077, func() { l, err = net.Listen("unix", path) })
	return l, err
}
