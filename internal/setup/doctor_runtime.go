package setup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// RuntimeChecks covers DSO-DOCTOR-012 through DSO-DOCTOR-013.
type RuntimeChecks struct {
	runtimeDir string
	stat       func(string) (os.FileInfo, error)
	glob       func(string) ([]string, error)
}

func newRuntimeChecks(runtimeDir string) *RuntimeChecks {
	return &RuntimeChecks{
		runtimeDir: runtimeDir,
		stat:       os.Stat,
		glob:       filepath.Glob,
	}
}

func (rc *RuntimeChecks) run(_ context.Context) []DoctorCheck {
	return []DoctorCheck{
		rc.checkRuntimeDir(),
		rc.checkNoStaleLocks(),
	}
}

// DSO-DOCTOR-012: DSO runtime directory exists and is accessible.
func (rc *RuntimeChecks) checkRuntimeDir() DoctorCheck {
	const id = "DSO-DOCTOR-012"
	const name = "Runtime directory"
	desc := "DSO runtime directory must exist at " + rc.runtimeDir

	info, err := rc.stat(rc.runtimeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return infoCheck(id, name, desc,
				"runtime directory not present — DSO may not have run on this system yet",
				DoctorCatRuntime,
			)
		}
		return warnCheck(id, name, desc,
			"stat failed: "+err.Error(),
			"Runtime directory cannot be inspected",
			DoctorCatRuntime,
			"Check filesystem health and parent directory permissions",
		)
	}
	if !info.IsDir() {
		return failCheck(id, name, desc,
			rc.runtimeDir+" exists but is not a directory",
			"A file exists at the expected runtime directory path",
			DoctorHigh, DoctorCatRuntime,
			"Remove the conflicting file: sudo rm "+rc.runtimeDir,
			"Re-run docker dso setup to recreate the directory",
		)
	}
	return passCheck(id, name, desc,
		fmt.Sprintf("runtime directory found at %s", rc.runtimeDir),
		DoctorCatRuntime,
	)
}

// DSO-DOCTOR-013: No stale lock files in the runtime directory.
func (rc *RuntimeChecks) checkNoStaleLocks() DoctorCheck {
	const id = "DSO-DOCTOR-013"
	const name = "Stale lock files"
	desc := "No stale lock files should exist in " + rc.runtimeDir

	pattern := filepath.Join(rc.runtimeDir, "*.lock")
	locks, err := rc.glob(pattern)
	if err != nil {
		return infoCheck(id, name, desc,
			"unable to check for lock files: "+err.Error(),
			DoctorCatRuntime,
		)
	}
	if len(locks) == 0 {
		return passCheck(id, name, desc, "no stale lock files found", DoctorCatRuntime)
	}
	return warnCheck(id, name, desc,
		fmt.Sprintf("%d lock file(s) found: may indicate an unclean shutdown", len(locks)),
		"A previous DSO process may have exited without releasing its lock",
		DoctorCatRuntime,
		"If DSO is not running, remove lock files: sudo rm "+pattern,
		"Verify DSO is not running before removing locks",
	)
}
