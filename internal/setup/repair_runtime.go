package setup

import (
	"fmt"
	"os"
	"path/filepath"
)

// RepairRuntime handles repairs for runtime-related doctor checks:
//   DSO-DOCTOR-012 (runtime directory missing) — Safe, auto-applied
//   DSO-DOCTOR-013 (stale lock files) — Moderate, requires confirmation
type RepairRuntime struct {
	runtimeDir string
	mkdir      func(string, os.FileMode) error
	glob       func(string) ([]string, error)
	removeFile func(string) error
}

func newRepairRuntime(runtimeDir string) *RepairRuntime {
	return &RepairRuntime{
		runtimeDir: runtimeDir,
		mkdir:      os.MkdirAll,
		glob:       filepath.Glob,
		removeFile: os.Remove,
	}
}

func (rr *RepairRuntime) planForCheck(check DoctorCheck) *RepairAction {
	switch check.ID {
	case "DSO-DOCTOR-012":
		return &RepairAction{
			ID:                   "REPAIR-RUNTIME-001",
			IssueID:              check.ID,
			Category:             DoctorCatRuntime,
			Description:          fmt.Sprintf("Create DSO runtime directory at %s", rr.runtimeDir),
			RiskLevel:            RepairRiskSafe,
			RequiresConfirmation: false,
			Status:               RepairStatusPending,
		}
	case "DSO-DOCTOR-013":
		return &RepairAction{
			ID:                   "REPAIR-RUNTIME-002",
			IssueID:              check.ID,
			Category:             DoctorCatRuntime,
			Description:          fmt.Sprintf("Remove stale lock files from %s", rr.runtimeDir),
			RiskLevel:            RepairRiskModerate,
			RequiresConfirmation: true,
			Status:               RepairStatusPending,
		}
	}
	return nil
}

// createRuntimeDir creates the DSO runtime directory.
// os.MkdirAll is idempotent — safe to call even if the directory already exists.
func (rr *RepairRuntime) createRuntimeDir() error {
	if err := rr.mkdir(rr.runtimeDir, 0750); err != nil {
		return fmt.Errorf("create runtime dir %s: %w", rr.runtimeDir, err)
	}
	return nil
}

// removeStaleLocks removes all *.lock files from the runtime directory.
// ErrNotExist is tolerated — a lock removed by another process is not an error.
func (rr *RepairRuntime) removeStaleLocks() error {
	pattern := filepath.Join(rr.runtimeDir, "*.lock")
	locks, err := rr.glob(pattern)
	if err != nil {
		return fmt.Errorf("glob lock files in %s: %w", rr.runtimeDir, err)
	}
	var lastErr error
	for _, f := range locks {
		if err := rr.removeFile(f); err != nil && !os.IsNotExist(err) {
			lastErr = fmt.Errorf("remove lock file %s: %w", f, err)
		}
	}
	return lastErr
}
