package setup

import (
	"fmt"

	"github.com/docker-secret-operator/dso/internal/bootstrap"
)

// RepairState handles DSO-DOCTOR-025 (state directory not root:dso 0750):
// REPAIR-STATE-001, Moderate, requires confirmation.
//
// It sets the owner (root:dso, or root:root when the group does not exist) and
// the exact mode of the ONE directory, through the same descriptor-based,
// symlink-refusing, non-recursive primitive bootstrap uses
// (bootstrap.ApplyDirectoryNoFollow). It never visits files or subdirectories
// (plugins, databases, keys, user content), never creates a directory and never
// regenerates or touches the ledger key.
type RepairState struct {
	dir       string
	apply     bootstrap.ApplyDirFunc
	lookupGID func() (int, bool)
}

func newRepairState(dir string) *RepairState {
	if dir == "" {
		dir = bootstrap.StateDirPath
	}
	return &RepairState{dir: dir, apply: bootstrap.ApplyDirectoryNoFollow, lookupGID: lookupDSOGroup}
}

func (rs *RepairState) planForCheck(check DoctorCheck) *RepairAction {
	if check.ID != "DSO-DOCTOR-025" {
		return nil
	}
	return &RepairAction{
		ID:                   "REPAIR-STATE-001",
		IssueID:              check.ID,
		Category:             DoctorCatPermissions,
		Description:          fmt.Sprintf("Set %s to root:dso mode %04o (this directory only; nothing inside is changed)", rs.dir, bootstrap.StateDirMode),
		RiskLevel:            RepairRiskModerate,
		RequiresConfirmation: true,
		Status:               RepairStatusPending,
	}
}

// repairStateDir applies owner and mode to the state directory only.
func (rs *RepairState) repairStateDir() error {
	gid, found := rs.lookupGID()
	if !found {
		gid = 0
	}
	if _, err := rs.apply(rs.dir, bootstrap.StateDirMode, 0, gid); err != nil {
		return fmt.Errorf("repair state directory %s: %w", rs.dir, err)
	}
	return nil
}
