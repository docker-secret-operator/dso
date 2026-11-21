//go:build !unix

package ledger

// lockDir fails closed: flock is not available, so the store cannot establish
// single-writer ownership.
func lockDir(string) (func() error, error) { return nil, ErrUnsupportedPlatform }
