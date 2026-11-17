package setup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/docker-secret-operator/dso/internal/audit"
)

// DefaultAuditFilePath is where the agent keeps its durable audit file. It must
// equal agent.DurableAuditPath() (pinned by a test in internal/cli).
const DefaultAuditFilePath = "/var/lib/dso/state/audit.jsonl"

// AuditFileChecks is DSO-DOCTOR-032: the agent's durable audit file. It is
// read-only (Lstat and a verifying read), never creates or repairs anything,
// and never reports more than the unkeyed, best-effort chain can support.
type AuditFileChecks struct {
	mode  SetupMode
	path  string
	lstat func(string) (os.FileInfo, error)
	read  func(string) ([]audit.Record, error)
}

func newAuditFileChecks(mode SetupMode, path string) *AuditFileChecks {
	if path == "" {
		path = DefaultAuditFilePath
	}
	return &AuditFileChecks{mode: mode, path: path, lstat: os.Lstat, read: audit.ReadVerified}
}

func (ac *AuditFileChecks) run(_ context.Context) []DoctorCheck {
	return []DoctorCheck{ac.checkAuditFile()}
}

func (ac *AuditFileChecks) checkAuditFile() DoctorCheck {
	const id = "DSO-DOCTOR-032"
	const name = "Durable audit file"
	const desc = "The agent's durable audit file should exist with restrictive permissions and a verifiable hash chain"
	cat := DoctorCatSecurity

	if ac.mode != ModeAgent {
		return infoCheck(id, name, desc, "not applicable outside agent mode", cat)
	}
	st, err := ac.lstat(ac.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return infoCheck(id, name, desc, ac.path+": not present (created when the agent starts and can open it; audit events are then logged only)", cat)
		}
		return infoCheck(id, name, desc, fmt.Sprintf("%s: cannot be inspected as this user (%v)", ac.path, err), cat)
	}
	if !st.Mode().IsRegular() {
		return warnCheck(id, name, desc, ac.path+": not a regular file",
			"The agent only writes a regular file; a symlink or special file is refused",
			cat, "Remove or move the unexpected path aside, then restart dso-agent")
	}
	if st.Mode().Perm()&0o077 != 0 {
		return warnCheck(id, name, desc, fmt.Sprintf("%s: mode %04o is accessible to group or other", ac.path, st.Mode().Perm()),
			"The agent refuses to open an audit file accessible to group or other, so audit events are not being persisted",
			cat, fmt.Sprintf("chmod 600 %s, then restart dso-agent", ac.path))
	}
	recs, err := ac.read(ac.path)
	switch {
	case errors.Is(err, audit.ErrChain):
		return failCheck(id, name, desc, ac.path+": hash chain verification FAILED ("+err.Error()+")",
			"The file was modified, reordered, truncated mid-file or corrupted; the agent refuses to append to it, so audit events are not being persisted",
			DoctorHigh, cat,
			"Run `dso audit verify` for the exit-coded result",
			"Preserve the file as evidence, move it aside, then restart dso-agent to start a new chain")
	case errors.Is(err, fs.ErrPermission):
		return infoCheck(id, name, desc, ac.path+": exists but cannot be read as this user; run doctor as root or the agent user to verify the chain", cat)
	case err != nil:
		return infoCheck(id, name, desc, fmt.Sprintf("%s: could not be read (%v)", ac.path, err), cat)
	}
	return passCheck(id, name, desc,
		fmt.Sprintf("%s: %d records, chain consistent (unkeyed and best effort: not proof of authenticity or completeness)", ac.path, len(recs)), cat)
}
