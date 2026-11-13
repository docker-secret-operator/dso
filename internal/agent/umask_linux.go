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

// restrictiveBits are the permission bits the temporary mask always adds: nothing
// created under it grants any group or other access.
const restrictiveBits = 0o077

// withUmask runs fn with the process umask temporarily set to
//
//	tempMask = previousMask | mask
//
// and restores the previous umask and releases the lock on every exit path,
// including a panic in fn. It is the ONLY way this package changes the umask;
// callers cannot forget to restore. fn must be short and must not call withUmask
// (the lock is not reentrant).
//
// Supported umask contract: the temporary mask is MONOTONIC. It can only add
// restrictive bits to whatever the process already had (a previous 0o277 stays
// 0o277 under mask 0o077, it is never loosened to 0o077), so an operator or
// service-manager that runs the agent with a stricter umask keeps it. The previous
// value is read with the same Umask call that installs the temporary one: Umask has
// no read-only form, so the probe installs 0o777 (the most restrictive mask, so the
// instant before the real value is applied can never be LESS restrictive than the
// previous one) and the previous value OR-ed with `mask` is applied before fn runs.
// Under umaskMu this read-modify-
// write is atomic with respect to every other change THIS package makes.
//
// Remaining process-wide limitation (not fixable from user space): umask is shared
// by every thread, so for the few microseconds fn runs, an UNRELATED goroutine that
// creates a file gets the (only ever more restrictive) temporary mask, and code
// outside this package that itself calls Umask concurrently is not serialised by
// umaskMu. Files whose mode matters are chmod'ed explicitly by their creators.
func withUmask(mask int, fn func()) {
	umaskMu.Lock()
	defer umaskMu.Unlock()
	previous := unix.Umask(0o777)
	unix.Umask(previous | mask)
	defer unix.Umask(previous)
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
	withUmask(restrictiveBits, func() { l, err = net.Listen("unix", path) })
	return l, err
}
