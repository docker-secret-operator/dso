//go:build !unix

package ledger

import "os"

// ownerUID reports no owner: stale-temp cleanup then removes nothing.
func ownerUID(os.FileInfo) (uint32, bool) { return 0, false }
