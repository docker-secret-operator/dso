package setup

import (
	"fmt"
	"os"
)

// RepairPermissions handles repairs for permission-related doctor checks:
//   DSO-DOCTOR-004 (socket world-readable) — Moderate, requires confirmation
//   DSO-DOCTOR-005 (config world-readable) — Safe, auto-applied
//   DSO-DOCTOR-009 (config overly permissive) — Safe, auto-applied
type RepairPermissions struct {
	socketPath string
	configPath string
	chmod      func(string, os.FileMode) error
}

func newRepairPermissions(socketPath, configPath string) *RepairPermissions {
	return &RepairPermissions{
		socketPath: socketPath,
		configPath: configPath,
		chmod:      os.Chmod,
	}
}

func (rp *RepairPermissions) planForCheck(check DoctorCheck) *RepairAction {
	switch check.ID {
	case "DSO-DOCTOR-004":
		return &RepairAction{
			ID:                   "REPAIR-PERM-001",
			IssueID:              check.ID,
			Category:             DoctorCatPermissions,
			Description:          fmt.Sprintf("Set Docker socket permissions to 0660 at %s", rp.socketPath),
			RiskLevel:            RepairRiskModerate,
			RequiresConfirmation: true,
			Status:               RepairStatusPending,
		}
	case "DSO-DOCTOR-005":
		return &RepairAction{
			ID:                   "REPAIR-PERM-002",
			IssueID:              check.ID,
			Category:             DoctorCatPermissions,
			Description:          fmt.Sprintf("Restrict config file permissions to 0600 at %s", rp.configPath),
			RiskLevel:            RepairRiskSafe,
			RequiresConfirmation: false,
			Status:               RepairStatusPending,
		}
	case "DSO-DOCTOR-009":
		return &RepairAction{
			ID:                   "REPAIR-PERM-003",
			IssueID:              check.ID,
			Category:             DoctorCatPermissions,
			Description:          fmt.Sprintf("Tighten config file permissions to 0600 at %s", rp.configPath),
			RiskLevel:            RepairRiskSafe,
			RequiresConfirmation: false,
			Status:               RepairStatusPending,
		}
	}
	return nil
}

// repairSocketPerms sets the Docker socket to mode 0660.
func (rp *RepairPermissions) repairSocketPerms() error {
	if err := rp.chmod(rp.socketPath, 0660); err != nil {
		return fmt.Errorf("chmod socket %s to 0660: %w", rp.socketPath, err)
	}
	return nil
}

// repairConfigPerms sets the DSO config file to mode 0600.
func (rp *RepairPermissions) repairConfigPerms() error {
	if err := rp.chmod(rp.configPath, 0600); err != nil {
		return fmt.Errorf("chmod config %s to 0600: %w", rp.configPath, err)
	}
	return nil
}
