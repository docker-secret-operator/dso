//go:build !unix

package ledger

// readLedgerFile fails closed: ownership and mode cannot be verified here.
func readLedgerFile(string, int64) ([]byte, error) { return nil, ErrUnsupportedPlatform }
