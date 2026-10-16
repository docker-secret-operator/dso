//go:build unix

package config

import (
	"fmt"
	"os"
	"strings"
)

// PermissionAdvisory inspects the mode of an existing configuration file and
// returns a human-readable warning, or "" when the mode is acceptable. It only
// ever READS: it never changes ownership or permissions, because the path may
// be user-supplied and chmod-ing it behind the operator's back could break
// their deployment. The remediation is printed for the operator to run.
//
// Acceptable is the same bound the doctor check (DSO-DOCTOR-009) and the
// bootstrap installer use: no world access and no group write (0640 or
// stricter). Installs created before that bound existed (bootstrap used to
// create /etc/dso/dso.yaml as 0664/0644) keep their old mode until the
// operator re-runs `dso bootstrap` or fixes it by hand, which is what this
// advisory is for.
func PermissionAdvisory(path string) string {
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return ""
	}
	perm := fi.Mode().Perm()
	if perm&0o027 == 0 {
		return ""
	}
	return fmt.Sprintf(
		"config file %s has mode %04o: it can be read%s by users other than its owner/group, and DSO configs hold credentials "+
			"(webhook hmac_secret/auth_token, WebUI password hash, provider settings). Restrict it: "+
			"`sudo chown root:dso %s && sudo chmod 0640 %s` (or `chmod 0600` if the agent runs as the file owner). "+
			"DSO does not change this for you.",
		path, perm, writeNote(perm), path, path)
}

func writeNote(perm os.FileMode) string {
	var parts []string
	if perm&0o020 != 0 {
		parts = append(parts, "written")
	}
	if perm&0o007 != 0 {
		parts = append(parts, "world-accessible")
	}
	if len(parts) == 0 {
		return ""
	}
	return " (and is " + strings.Join(parts, " and ") + ")"
}
