//go:build unix

package ledger

import (
	"os"
	"syscall"
)

// ownerUID returns the owning uid of fi, when the platform reports one.
func ownerUID(fi os.FileInfo) (uint32, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return st.Uid, true
}
