//go:build linux

package agent

import (
	"net"
	"os"
	"strconv"
	"strings"
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
// service-manager that runs the agent with a stricter umask keeps it.
//
// The previous value is read from /proc/self/status ("Umask:", Linux >= 4.7), which
// changes nothing. Do NOT read it by probing with Umask(0o777): that installs a
// process-wide mask of 000-permission for an instant and any unrelated goroutine
// that creates a directory in that instant gets a directory it cannot write to
// (observed as "bind: permission denied" in concurrent StartSocketServer calls).
// If /proc is unreadable, fall back to the single atomic Umask(mask) call, which
// returns the previous value; owner bits that the previous mask cleared are then
// re-applied immediately (a transient that is looser only in owner bits).
// Under umaskMu this is atomic with respect to every other change THIS package makes.
//
// Remaining process-wide limitation (not fixable from user space): umask is shared
// by every thread, so for the few microseconds fn runs, an UNRELATED goroutine that
// creates a file gets the (only ever more restrictive) temporary mask, and code
// outside this package that itself calls Umask concurrently is not serialised by
// umaskMu. Files whose mode matters are chmod'ed explicitly by their creators.
func withUmask(mask int, fn func()) {
	umaskMu.Lock()
	defer umaskMu.Unlock()
	var previous int
	if p, ok := procUmask(); ok {
		previous = p
		unix.Umask(previous | mask)
	} else {
		previous = unix.Umask(mask)
		if extra := previous &^ mask; extra != 0 {
			unix.Umask(previous | mask)
		}
	}
	defer unix.Umask(previous)
	fn()
}

// procUmask reads the process umask without changing it.
func procUmask() (int, bool) {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "Umask:"); ok {
			n, err := strconv.ParseUint(strings.TrimSpace(v), 8, 32)
			if err != nil || n > 0o777 {
				return 0, false
			}
			return int(n), true
		}
	}
	return 0, false
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
