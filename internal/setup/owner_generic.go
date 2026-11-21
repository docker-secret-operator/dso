//go:build !linux && !darwin

package setup

import "os"

// ownerOfPath is not supported on this platform; rollback will skip chown.
func ownerOfPath(_ string) (string, error) { return "", nil }

// statOwner is unknown on this platform.
func statOwner(_ os.FileInfo) (uid, gid uint32, ok bool) { return 0, 0, false }
