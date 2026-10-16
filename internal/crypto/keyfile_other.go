//go:build !unix

package crypto

import "os"

func openNoFollow(path string) (*os.File, error) { return os.Open(path) }

// File-ownership and mode semantics differ on this platform; the unix build is
// the supported deployment target for the central store.
func ownedByCurrentUser(os.FileInfo) bool { return true }
