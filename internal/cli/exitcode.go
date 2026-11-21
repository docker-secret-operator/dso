package cli

import "errors"

// Exit statuses of `dso central audit verify`, in addition to the repository's
// established 0 (success) and 1 (usage error or any other runtime failure).
const (
	exitAuditIntegrityFailure = 10 // the audit chain failed verification
	exitAuditUnavailable      = 11 // verification could not be completed
)

// Exit statuses of `dso repair permissions`, in addition to 0 (every contract
// path conforms, or was corrected, and nothing needs manual action) and 1
// (refused because not root, a path could not be repaired, or any other failure).
const (
	exitRepairChangesRequired = 12 // dry run: directory-level corrections are required (nothing was changed)
	exitRepairManualAction    = 13 // a path needs manual action that this command never performs
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
