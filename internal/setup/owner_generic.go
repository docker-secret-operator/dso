//go:build !linux && !darwin

package setup

// ownerOfPath is not supported on this platform; rollback will skip chown.
func ownerOfPath(_ string) (string, error) { return "", nil }
