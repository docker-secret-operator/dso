//go:build !unix

package ledger

import "errors"

func mkfifo(string) error { return errors.New("FIFOs are not available on this platform") }
