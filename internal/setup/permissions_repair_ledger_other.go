//go:build !linux

package setup

// inspectLedgerKey is not supported off Linux: the key is reported as needing
// manual inspection and is never touched.
func (r *PermissionsRepairer) inspectLedgerKey() PermissionEntry {
	return PermissionEntry{
		Path:     r.layout.LedgerDir + "/ledger.key",
		Kind:     "ledger key",
		Current:  "not inspected",
		Expected: "root 0400 (0600 accepted)",
		Status:   PermStatusManual,
		Detail:   "ledger key inspection is only supported on Linux",
	}
}
