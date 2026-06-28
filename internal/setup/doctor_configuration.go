package setup

import (
	"context"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ConfigurationChecks covers DSO-DOCTOR-007 through DSO-DOCTOR-009.
type ConfigurationChecks struct {
	configPath string
	stat       func(string) (os.FileInfo, error)
	readFile   func(string) ([]byte, error)
}

func newConfigurationChecks(configPath string) *ConfigurationChecks {
	return &ConfigurationChecks{
		configPath: configPath,
		stat:       os.Stat,
		readFile:   os.ReadFile,
	}
}

func (cc *ConfigurationChecks) run(_ context.Context) []DoctorCheck {
	return []DoctorCheck{
		cc.checkConfigExists(),
		cc.checkConfigSyntax(),
		cc.checkConfigMode(),
	}
}

// DSO-DOCTOR-007: DSO configuration file present on disk.
func (cc *ConfigurationChecks) checkConfigExists() DoctorCheck {
	const id = "DSO-DOCTOR-007"
	const name = "Config file exists"
	desc := "DSO config file must exist at " + cc.configPath

	if _, err := cc.stat(cc.configPath); err != nil {
		if os.IsNotExist(err) {
			return failCheck(id, name, desc,
				"file not found at "+cc.configPath,
				"DSO has not been configured on this system",
				DoctorCritical, DoctorCatConfiguration,
				"Run docker dso setup to initialise DSO",
				"Or copy an existing config to "+cc.configPath,
			)
		}
		return failCheck(id, name, desc,
			"stat failed: "+err.Error(),
			"File system error preventing config access",
			DoctorHigh, DoctorCatConfiguration,
			"Check file system health and permissions on the parent directory",
		)
	}
	return passCheck(id, name, desc, "config file found at "+cc.configPath, DoctorCatConfiguration)
}

// DSO-DOCTOR-008: DSO configuration file is valid YAML.
func (cc *ConfigurationChecks) checkConfigSyntax() DoctorCheck {
	const id = "DSO-DOCTOR-008"
	const name = "Config syntax"
	desc := "DSO config at " + cc.configPath + " must be valid YAML"

	content, err := cc.readFile(cc.configPath)
	if err != nil {
		return infoCheck(id, name, desc,
			"file not readable — skipping syntax check",
			DoctorCatConfiguration,
		)
	}
	if len(content) == 0 {
		return warnCheck(id, name, desc,
			"config file is empty",
			"An empty config file will cause DSO to use defaults, which may be incorrect",
			DoctorCatConfiguration,
			"Run docker dso setup to generate a valid configuration",
		)
	}

	var node yaml.Node
	if err := yaml.Unmarshal(content, &node); err != nil {
		return failCheck(id, name, desc,
			fmt.Sprintf("YAML parse error: %s", err.Error()),
			"Config file contains invalid YAML",
			DoctorHigh, DoctorCatConfiguration,
			"Validate YAML syntax: cat "+cc.configPath+" | python3 -c 'import sys,yaml; yaml.safe_load(sys.stdin)'",
			"Or re-run docker dso setup to regenerate the config",
		)
	}
	return passCheck(id, name, desc, "config file is valid YAML", DoctorCatConfiguration)
}

// DSO-DOCTOR-009: DSO config file permissions are not overly permissive.
func (cc *ConfigurationChecks) checkConfigMode() DoctorCheck {
	const id = "DSO-DOCTOR-009"
	const name = "Config file mode"
	desc := "DSO config at " + cc.configPath + " should have mode 0600 or 0640"

	info, err := cc.stat(cc.configPath)
	if err != nil {
		return infoCheck(id, name, desc,
			"file not found — skipping mode check",
			DoctorCatConfiguration,
		)
	}

	mode := info.Mode().Perm()
	if mode > 0640 {
		return warnCheck(id, name, desc,
			fmt.Sprintf("config mode is %04o — more permissive than recommended 0600", mode),
			"Overly permissive config may expose secret provider credentials to other users",
			DoctorCatConfiguration,
			fmt.Sprintf("Tighten permissions: chmod 600 %s", cc.configPath),
		)
	}
	return passCheck(id, name, desc,
		fmt.Sprintf("config mode %04o is within recommended bounds", mode),
		DoctorCatConfiguration,
	)
}
