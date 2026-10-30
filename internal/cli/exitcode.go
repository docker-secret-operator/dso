package cli

import "errors"

// Exit statuses of `dso central audit verify`, in addition to the repository's
// established 0 (success) and 1 (usage error or any other runtime failure).
const (
	exitAuditIntegrityFailure = 10 // the audit chain failed verification
	exitAuditUnavailable      = 11 // verification could not be completed
)

// exitCodeError carries a specific process exit status. cli.Run uses it
// instead of the default 1; commands still only return errors and never call
// os.Exit.
type exitCodeError struct {
	code int
	err  error
}

func (e *exitCodeError) Error() string { return e.err.Error() }
func (e *exitCodeError) Unwrap() error { return e.err }
func (e *exitCodeError) ExitCode() int { return e.code }

// exitCodeFor returns the exit status for a command error.
func exitCodeFor(err error) int {
	var ec *exitCodeError
	if errors.As(err, &ec) {
		return ec.code
	}
	return 1
}
