package setup

import (
	"context"
	"os"
	"os/exec"
)

// ServiceChecks covers DSO-DOCTOR-014 through DSO-DOCTOR-017.
// systemctl calls are injected so tests never require systemd.
type ServiceChecks struct {
	serviceName  string
	unitFilePath string
	lookupBinary func(string) (string, error)
	statUnitFile func(string) (os.FileInfo, error)
	isEnabled    func(string) (bool, error)
	isActive     func(string) (bool, error)
}

func newServiceChecks() *ServiceChecks {
	return &ServiceChecks{
		serviceName:  "dso-agent.service",
		unitFilePath: "/etc/systemd/system/dso-agent.service",
		lookupBinary: exec.LookPath,
		statUnitFile: os.Stat,
		isEnabled:    systemctlIsEnabled,
		isActive:     systemctlIsActive,
	}
}

func (sc *ServiceChecks) run(_ context.Context) []DoctorCheck {
	return []DoctorCheck{
		sc.checkAgentBinary(),
		sc.checkUnitFile(),
		sc.checkServiceEnabled(),
		sc.checkServiceActive(),
	}
}

// DSO-DOCTOR-014: DSO agent binary present in PATH.
func (sc *ServiceChecks) checkAgentBinary() DoctorCheck {
	const id = "DSO-DOCTOR-014"
	const name = "DSO agent binary"
	const desc = "DSO agent binary (dso-agent) must be present in PATH"

	if _, err := sc.lookupBinary("dso-agent"); err != nil {
		return failCheck(id, name, desc,
			"dso-agent not found in PATH",
			"DSO agent binary has not been installed",
			DoctorCritical, DoctorCatService,
			"Install DSO agent: docker dso setup",
			"Verify with: which dso-agent",
		)
	}
	return passCheck(id, name, desc, "dso-agent binary found in PATH", DoctorCatService)
}

// DSO-DOCTOR-015: systemd unit file present on disk.
func (sc *ServiceChecks) checkUnitFile() DoctorCheck {
	const id = "DSO-DOCTOR-015"
	const name = "Service unit file"
	desc := "systemd unit file must exist at " + sc.unitFilePath

	if _, err := sc.statUnitFile(sc.unitFilePath); err != nil {
		if os.IsNotExist(err) {
			return failCheck(id, name, desc,
				"unit file not found at "+sc.unitFilePath,
				"DSO agent systemd service has not been installed",
				DoctorHigh, DoctorCatService,
				"Install the service: docker dso setup",
				"Or create the unit file manually and run: systemctl daemon-reload",
			)
		}
		return warnCheck(id, name, desc,
			"cannot stat unit file: "+err.Error(),
			"Unit file cannot be verified",
			DoctorCatService,
			"Check permissions on /etc/systemd/system/",
		)
	}
	return passCheck(id, name, desc, "unit file found at "+sc.unitFilePath, DoctorCatService)
}

// DSO-DOCTOR-016: DSO agent service is enabled (starts on boot).
func (sc *ServiceChecks) checkServiceEnabled() DoctorCheck {
	const id = "DSO-DOCTOR-016"
	const name = "Service enabled"
	desc := sc.serviceName + " must be enabled (auto-starts on boot)"

	enabled, err := sc.isEnabled(sc.serviceName)
	if err != nil {
		return infoCheck(id, name, desc,
			"systemctl not available — cannot verify service state",
			DoctorCatService,
		)
	}
	if !enabled {
		return warnCheck(id, name, desc,
			sc.serviceName+" is installed but not enabled",
			"Service will not start automatically after a reboot",
			DoctorCatService,
			"Enable the service: sudo systemctl enable "+sc.serviceName,
		)
	}
	return passCheck(id, name, desc, sc.serviceName+" is enabled", DoctorCatService)
}

// DSO-DOCTOR-017: DSO agent service is currently running.
func (sc *ServiceChecks) checkServiceActive() DoctorCheck {
	const id = "DSO-DOCTOR-017"
	const name = "Service active"
	desc := sc.serviceName + " must be running"

	active, err := sc.isActive(sc.serviceName)
	if err != nil {
		return infoCheck(id, name, desc,
			"systemctl not available — cannot verify active state",
			DoctorCatService,
		)
	}
	if !active {
		return failCheck(id, name, desc,
			sc.serviceName+" is not running",
			"DSO agent is installed but not currently active — secrets cannot be injected",
			DoctorHigh, DoctorCatService,
			"Start the service: sudo systemctl start "+sc.serviceName,
			"Check logs:        journalctl -u "+sc.serviceName+" -n 50",
		)
	}
	return passCheck(id, name, desc, sc.serviceName+" is running", DoctorCatService)
}
