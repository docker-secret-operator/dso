package bootstrap

import "os"

// Read-only view of the DSO directory contract for `dso repair permissions`
// (internal/setup). The values come from dsoDirectoryPlan, the single source of
// truth bootstrap itself applies, so the repair command, the setup planner and
// bootstrap cannot disagree about owners and modes.

const (
	// LedgerDirPath is the private ledger directory (root 0700). It is created by
	// the agent's ledger key provider, never by bootstrap.
	LedgerDirPath = "/var/lib/dso/ledger"
	// LedgerDirMode is the exact mode of LedgerDirPath.
	LedgerDirMode os.FileMode = 0700
	// LedgerKeyFileName is the host-local ledger HMAC key inside LedgerDirPath.
	LedgerKeyFileName = "ledger.key"
	// PluginDirPath is the provider-plugin directory: root-owned, never group or
	// world writable.
	PluginDirPath = "/usr/local/lib/dso/plugins"
)

// DirectoryContractEntry is one directory of the contract (always root:dso, or
// root:root when the dso group does not exist).
type DirectoryContractEntry struct {
	Path   string
	Mode   os.FileMode
	Strict bool
}

// DirectoryContract returns the contract directories exactly as bootstrap
// applies them.
func DirectoryContract() []DirectoryContractEntry {
	plan := dsoDirectoryPlan()
	out := make([]DirectoryContractEntry, 0, len(plan))
	for _, d := range plan {
		out = append(out, DirectoryContractEntry{Path: d.path, Mode: d.perm, Strict: d.strict})
	}
	return out
}
