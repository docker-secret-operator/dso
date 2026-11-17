package setup

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"strconv"

	"github.com/docker-secret-operator/dso/internal/bootstrap"
)

// StateDirChecks covers DSO-DOCTOR-025: the persistent-state directory
// (/var/lib/dso) must be root:dso 0750 in agent mode. The ledger key provider and
// the plugin trust checks refuse a group- or world-writable ancestor, so an
// installation upgraded from a build that created it 0770 has the applied-version
// ledger disabled until this directory is corrected. The check is read-only; its
// repair (REPAIR-STATE-001) changes this one directory and nothing inside it.
type StateDirChecks struct {
	mode       SetupMode
	dir        string
	lstat      func(string) (os.FileInfo, error)
	lookupGID  func() (gid int, found bool)
	statOwnerF func(os.FileInfo) (uid, gid uint32, ok bool)
}

func newStateDirChecks(mode SetupMode, dir string) *StateDirChecks {
	if dir == "" {
		dir = bootstrap.StateDirPath
	}
	return &StateDirChecks{mode: mode, dir: dir, lstat: os.Lstat, lookupGID: lookupDSOGroup, statOwnerF: statOwner}
}

// lookupDSOGroup returns the gid of group "dso" (found=false: root:root applies).
func lookupDSOGroup() (int, bool) {
	g, err := user.LookupGroup("dso")
	if err != nil {
		return 0, false
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, false
	}
	return gid, true
}

func (sc *StateDirChecks) run(_ context.Context) []DoctorCheck {
	return []DoctorCheck{sc.checkStateDir()}
}

func (sc *StateDirChecks) checkStateDir() DoctorCheck {
	const id = "DSO-DOCTOR-025"
	const name = "State directory permissions"
	desc := "The persistent-state directory " + sc.dir + " must be root:dso 0750 (the applied-version ledger refuses a group- or world-writable parent)"

	if sc.mode != ModeAgent {
		return infoCheck(id, name, desc, "not applicable outside agent mode", DoctorCatPermissions)
	}
	fi, err := sc.lstat(sc.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return infoCheck(id, name, desc, sc.dir+" not present yet (created by bootstrap)", DoctorCatPermissions)
		}
		return warnCheck(id, name, desc, "stat failed: "+err.Error(), "The state directory cannot be inspected",
			DoctorCatPermissions, "Check filesystem health and parent directory permissions")
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return failCheck(id, name, desc, sc.dir+" is a symbolic link",
			"A symlinked state directory is refused by the ledger, plugin and key checks and is never repaired automatically",
			DoctorHigh, DoctorCatPermissions,
			"Replace the symlink with a real root:dso 0750 directory by hand (move the data deliberately; nothing is changed recursively by DSO)")
	}
	if !fi.IsDir() {
		return failCheck(id, name, desc, sc.dir+" is not a directory", "A file exists at the state directory path",
			DoctorHigh, DoctorCatPermissions, "Move the conflicting file away and re-run: sudo docker dso bootstrap agent")
	}

	wantGID, found := sc.lookupGID()
	if !found {
		wantGID = 0
	}
	perm := fi.Mode().Perm()
	var problems string
	add := func(s string) {
		if problems != "" {
			problems += "; "
		}
		problems += s
	}
	uid, gid, known := sc.statOwnerF(fi)
	actual := fmt.Sprintf("mode %04o", perm)
	if known {
		actual = fmt.Sprintf("uid %d gid %d, mode %04o", uid, gid, perm)
		if uid != 0 {
			add(fmt.Sprintf("owned by uid %d, expected root", uid))
		}
		if int(gid) != wantGID {
			add(fmt.Sprintf("group gid %d, expected %d", gid, wantGID))
		}
	}
	if perm&0o022 != 0 {
		add("writable by group or others")
	}
	if perm&0o007 != 0 {
		add("accessible by others")
	}
	if problems == "" {
		return passCheck(id, name, desc, fmt.Sprintf("%s: %s (expected root:dso, mode %04o or stricter)", sc.dir, actual, bootstrap.StateDirMode), DoctorCatPermissions)
	}
	return warnCheck(id, name, desc,
		fmt.Sprintf("%s: %s -- %s; expected root:dso, mode %04o", sc.dir, actual, problems, bootstrap.StateDirMode),
		"The directory was created by an older build (0770) or changed by hand; while it is group- or world-writable the applied-version ledger opens disabled",
		DoctorCatPermissions,
		"Repair only this directory (non-recursive; nothing inside is changed): `sudo docker dso repair permissions` (add --dry-run to preview; REPAIR-STATE-001 is the same change), or `sudo docker dso bootstrap agent` (re-applies the DSO directory permissions and rewrites dso.yaml, so review it)",
		fmt.Sprintf("By hand: `sudo chown root:dso %s && sudo chmod 0750 %s` (never -R), then restart the dso-agent service", sc.dir, sc.dir),
	)
}
