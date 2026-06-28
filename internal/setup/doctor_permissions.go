package setup

import (
	"context"
	"fmt"
	"os"
)

// PermissionChecks covers DSO-DOCTOR-004 through DSO-DOCTOR-006.
// Checks permissions on the Docker socket and DSO config file, and verifies
// that the Docker socket is not world-readable (security check).
type PermissionChecks struct {
	socketPath string
	configPath string
	statSocket func(string) (os.FileInfo, error)
	statConfig func(string) (os.FileInfo, error)
	currentUID func() int
}

func newPermissionChecks(socketPath, configPath string) *PermissionChecks {
	return &PermissionChecks{
		socketPath: socketPath,
		configPath: configPath,
		statSocket: os.Stat,
		statConfig: os.Stat,
		currentUID: os.Getuid,
	}
}

func (pc *PermissionChecks) run(_ context.Context) []DoctorCheck {
	return []DoctorCheck{
		pc.checkSocketPermissions(),
		pc.checkConfigPermissions(),
		pc.checkRunningAsRoot(),
	}
}

// DSO-DOCTOR-004: Docker socket is not world-readable.
func (pc *PermissionChecks) checkSocketPermissions() DoctorCheck {
	const id = "DSO-DOCTOR-004"
	const name = "Docker socket permissions"
	desc := "Docker socket at " + pc.socketPath + " must not be world-readable"

	info, err := pc.statSocket(pc.socketPath)
	if err != nil {
		return infoCheck(id, name, desc,
			"socket not found — skipping permissions check",
			DoctorCatSecurity,
		)
	}

	mode := info.Mode().Perm()
	if mode&0002 != 0 {
		return warnCheck(id, name, desc,
			fmt.Sprintf("socket permissions are %04o — world-readable", mode),
			"World-readable Docker socket allows any user to control Docker",
			DoctorCatSecurity,
			fmt.Sprintf("Restrict permissions: sudo chmod 660 %s", pc.socketPath),
			"Ensure socket is owned by root:docker group",
		)
	}
	return passCheck(id, name, desc,
		fmt.Sprintf("socket permissions %04o — not world-readable", mode),
		DoctorCatSecurity,
	)
}

// DSO-DOCTOR-005: DSO config file is not world-readable.
func (pc *PermissionChecks) checkConfigPermissions() DoctorCheck {
	const id = "DSO-DOCTOR-005"
	const name = "Config file permissions"
	desc := "DSO config at " + pc.configPath + " must not be world-readable"

	info, err := pc.statConfig(pc.configPath)
	if err != nil {
		return infoCheck(id, name, desc,
			"config file not found — skipping permissions check",
			DoctorCatSecurity,
		)
	}

	mode := info.Mode().Perm()
	if mode&0004 != 0 {
		return failCheck(id, name, desc,
			fmt.Sprintf("config permissions are %04o — world-readable", mode),
			"World-readable config exposes secret provider credentials",
			DoctorHigh, DoctorCatSecurity,
			fmt.Sprintf("Restrict permissions: chmod 600 %s", pc.configPath),
		)
	}
	return passCheck(id, name, desc,
		fmt.Sprintf("config permissions %04o — not world-readable", mode),
		DoctorCatSecurity,
	)
}

// DSO-DOCTOR-006: Current user is root (required for agent mode operations).
func (pc *PermissionChecks) checkRunningAsRoot() DoctorCheck {
	const id = "DSO-DOCTOR-006"
	const name = "Root access"
	const desc = "Agent mode requires root privileges for service installation"

	uid := pc.currentUID()
	if uid != 0 {
		return infoCheck(id, name, desc,
			fmt.Sprintf("running as UID %d (non-root) — agent mode operations require sudo", uid),
			DoctorCatPermissions,
		)
	}
	return passCheck(id, name, desc, "running as root (UID 0)", DoctorCatPermissions)
}
