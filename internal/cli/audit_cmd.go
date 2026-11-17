package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/docker-secret-operator/dso/internal/agent"
	"github.com/docker-secret-operator/dso/internal/audit"
)

// auditFileVerifyJSON is the stable --json document of `dso audit verify`.
type auditFileVerifyJSON struct {
	Status      string `json:"status"` // verified | integrity_failure | unavailable
	RecordCount *int   `json:"record_count,omitempty"`
	TipSeq      *int   `json:"tip_seq,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

// NewAuditCmd is `dso audit`: read-only inspection of the agent's durable audit file.
func NewAuditCmd() *cobra.Command {
	parent := &cobra.Command{Use: "audit", Short: "Inspect the agent's durable audit file"}
	var (
		file   string
		asJSON bool
	)
	verify := &cobra.Command{
		Use:   "verify",
		Short: "Verify the agent's durable audit file hash chain (read-only)",
		Args:  cobra.NoArgs,
		Long: `Recomputes the hash chain of the agent's durable audit file
(default ` + "/var/lib/dso/state/audit.jsonl" + `) and reports whether it is internally
consistent. It never modifies, creates or repairs the file. This is NOT the
central store's audit log; see ` + "`dso central audit verify`" + ` for that.

An unterminated final line (a write that was never acknowledged, for example
interrupted by a crash) is ignored, exactly as the agent ignores it on restart.

Exit status:
  0   verified (an empty file is verified)
  1   usage error, or any other command failure
  10  integrity failure: a record, link or sequence number does not match
  11  verification unavailable: the file is missing, unreadable, or not a
      regular file; this is not evidence of tampering
Codes 10 and 11 are shared with ` + "`dso central audit verify`" + `.

--json prints exactly one document on stdout, also on failure.

Limitations. The chain is unkeyed: anyone who can write the file can recompute
it, so "verified" does NOT prove authenticity. It cannot detect deletion of the
newest records, replacement by an older valid copy, or events that happened
while the agent could not persist them (see dso_audit_durable_write_failures_total).
The audit file is best effort, not a guaranteed or fail-closed audit trail.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := file
			if path == "" {
				path = agent.DurableAuditPath()
			}
			doc, failure := verifyAuditFile(path)
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				if err := enc.Encode(doc); err != nil {
					return err
				}
				return failure
			}
			switch doc.Status {
			case "verified":
				fmt.Fprintf(out, "Audit file: VERIFIED (%d records)\n", *doc.RecordCount)
				fmt.Fprintln(out, "Note: the chain is unkeyed. This does not prove authenticity, and cannot detect deleted newest records, rollback to an older valid copy, or events lost while persistence was unavailable.")
			case "integrity_failure":
				fmt.Fprintln(out, "Audit file: INTEGRITY FAILURE")
				fmt.Fprintf(out, "Reason: %s\n", printableUntrusted(doc.Reason))
			default:
				fmt.Fprintln(out, "Audit file: VERIFICATION UNAVAILABLE (this is not evidence of tampering)")
				fmt.Fprintf(out, "Reason: %s\n", printableUntrusted(doc.Reason))
			}
			return failure
		},
	}
	verify.Flags().StringVar(&file, "file", "", "audit file to verify (default: the agent's durable audit file)")
	verify.Flags().BoolVar(&asJSON, "json", false, "print the result as one JSON document")
	parent.AddCommand(verify)
	return parent
}

// verifyAuditFile never modifies the file and returns the stable document plus
// the exit-coded error for a non-verified outcome.
func verifyAuditFile(path string) (auditFileVerifyJSON, error) {
	unavailable := func(reason string, err error) (auditFileVerifyJSON, error) {
		return auditFileVerifyJSON{Status: "unavailable", Reason: reason},
			&exitCodeError{exitAuditUnavailable, fmt.Errorf("audit file verification unavailable: %s (detail: %s)", reason, printableUntrusted(err.Error()))}
	}
	st, err := os.Lstat(path)
	if err != nil {
		return unavailable("the audit file could not be read", err)
	}
	if !st.Mode().IsRegular() {
		return unavailable("the audit path is not a regular file", errors.New("not a regular file"))
	}
	recs, err := audit.ReadVerified(path)
	if errors.Is(err, audit.ErrChain) {
		return auditFileVerifyJSON{Status: "integrity_failure", Reason: err.Error()},
			&exitCodeError{exitAuditIntegrityFailure, fmt.Errorf("audit file integrity failure: %s", printableUntrusted(err.Error()))}
	}
	if err != nil {
		return unavailable("the audit file could not be read", err)
	}
	n, tip := len(recs), 0
	if n > 0 {
		tip = int(recs[n-1].Seq)
	}
	return auditFileVerifyJSON{Status: "verified", RecordCount: &n, TipSeq: &tip}, nil
}
