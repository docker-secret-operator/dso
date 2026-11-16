package setup

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/docker-secret-operator/dso/internal/bootstrap"
)

// PermissionsRepairer backs `dso repair permissions`: it inspects the known DSO
// permission-contract paths, reports actual versus expected owner and mode, and
// (when asked, and only as root) corrects the necessary DIRECTORY-level
// deviations.
//
// Guarantees, each pinned by a test:
//   - only the contract paths are inspected; nothing is discovered or walked;
//   - nothing inside a directory is visited, so no recursive chmod/chown exists;
//   - every change goes through bootstrap.ApplyDirectoryNoFollow
//     (O_NOFOLLOW|O_DIRECTORY, fchown/fchmod on the descriptor): a symlink or a
//     regular file at a contract path is refused, never followed;
//   - configuration files are never read or rewritten (dso.yaml is not touched);
//   - the ledger key is never created, regenerated, renamed or removed. Its
//     contents are never read beyond the fixed public header line. Only its
//     mode may be tightened, and only when it is verifiably a ledger key owned
//     by root; anything else is reported as a diagnostic;
//   - the agent, its authentication state and unrelated files are never touched;
//   - it is idempotent: a conforming path is not touched.
type PermissionsRepairer struct {
	layout    PermissionsLayout
	ownerUID  int
	lookupGID func() (gid int, found bool)
	euid      func() int
	apply     bootstrap.ApplyDirFunc
	chmodKey  func(f *os.File, mode os.FileMode) error
}

// PermissionsLayout is the set of contract paths. Production uses
// DefaultPermissionsLayout; tests point it at temporary directories.
type PermissionsLayout struct {
	EtcDir    string
	StateDir  string
	RunDir    string
	LedgerDir string
	PluginDir string
}

// permissionsRepairScope names the bootstrap contract directories this command
// repairs. /var/log/dso is deliberately outside it: its 0770 group-writable mode
// is part of the contract and not a security deviation.
var permissionsRepairScope = []string{"/etc/dso", bootstrap.StateDirPath, "/var/run/dso"}

// DefaultPermissionsLayout returns the production contract paths.
func DefaultPermissionsLayout() PermissionsLayout {
	return PermissionsLayout{
		EtcDir:    permissionsScopePath("/etc/dso"),
		StateDir:  permissionsScopePath(bootstrap.StateDirPath),
		RunDir:    permissionsScopePath("/var/run/dso"),
		LedgerDir: bootstrap.LedgerDirPath,
		PluginDir: bootstrap.PluginDirPath,
	}
}

func permissionsScopePath(p string) string {
	for _, e := range bootstrap.DirectoryContract() {
		if e.Path == p {
			return e.Path
		}
	}
	return p
}

// NewPermissionsRepairer returns a repairer wired to the real system.
func NewPermissionsRepairer() *PermissionsRepairer {
	return &PermissionsRepairer{
		layout:    DefaultPermissionsLayout(),
		ownerUID:  0,
		lookupGID: lookupDSOGroup,
		euid:      os.Geteuid,
		apply:     bootstrap.ApplyDirectoryNoFollow,
		chmodKey:  func(f *os.File, m os.FileMode) error { return f.Chmod(m) },
	}
}

// PermissionsRepairerOptions overrides the production wiring. It exists so the
// command can be exercised against temporary directories without root; every
// zero field keeps the production value.
type PermissionsRepairerOptions struct {
	Layout    *PermissionsLayout
	OwnerUID  *int // the uid directories must belong to (0 in production)
	LookupGID func() (gid int, found bool)
	EUID      func() int
	Apply     bootstrap.ApplyDirFunc
}

// NewPermissionsRepairerWith returns a repairer with the given overrides.
func NewPermissionsRepairerWith(o PermissionsRepairerOptions) *PermissionsRepairer {
	r := NewPermissionsRepairer()
	if o.Layout != nil {
		r.layout = *o.Layout
	}
	if o.OwnerUID != nil {
		r.ownerUID = *o.OwnerUID
	}
	if o.LookupGID != nil {
		r.lookupGID = o.LookupGID
	}
	if o.EUID != nil {
		r.euid = o.EUID
	}
	if o.Apply != nil {
		r.apply = o.Apply
	}
	return r
}

// ErrPermissionsNeedRoot is returned when an apply is requested by a non-root user.
var ErrPermissionsNeedRoot = errors.New("dso repair permissions needs root to change owners and modes: re-run with sudo (for example `sudo docker dso repair permissions`), or add --dry-run to inspect without changing anything")

// Entry statuses.
const (
	PermStatusOK     = "ok"
	PermStatusChange = "change" // a directory-level correction is required
	PermStatusManual = "manual" // not repaired automatically; see Detail
	PermStatusAbsent = "absent" // the path does not exist (nothing to repair)
)

// Entry results after an apply (empty in a dry run or when nothing was needed).
const (
	PermResultChanged = "changed"
	PermResultFailed  = "failed"
)

// PermissionEntry is one inspected contract path. It never carries file contents.
type PermissionEntry struct {
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	Current  string `json:"current"`
	Expected string `json:"expected"`
	Status   string `json:"status"`
	Result   string `json:"result,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Error    string `json:"error,omitempty"`

	fix func() error
}

// PermissionsReport is the outcome of an inspection or repair run.
type PermissionsReport struct {
	DryRun          bool              `json:"dry_run"`
	Entries         []PermissionEntry `json:"entries"`
	ChangesRequired int               `json:"changes_required"`
	Changed         int               `json:"changed"`
	Failed          int               `json:"failed"`
	ManualRequired  int               `json:"manual_required"`
	NoDSOGroup      bool              `json:"no_dso_group,omitempty"`
}

// Inspect reads the contract paths and reports deviations. It never modifies anything.
func (r *PermissionsRepairer) Inspect() *PermissionsReport {
	rep := &PermissionsReport{DryRun: true}
	wantGID, found := r.lookupGID()
	if !found {
		wantGID = 0
		rep.NoDSOGroup = true
	}
	group := "dso"
	if !found {
		group = "root"
	}

	byPath := map[string]bootstrap.DirectoryContractEntry{}
	for _, e := range bootstrap.DirectoryContract() {
		byPath[e.Path] = e
	}
	dirPaths := map[string]string{
		"/etc/dso":             r.layout.EtcDir,
		bootstrap.StateDirPath: r.layout.StateDir,
		"/var/run/dso":         r.layout.RunDir,
	}
	for _, contractPath := range permissionsRepairScope {
		spec, ok := byPath[contractPath]
		if !ok {
			rep.Entries = append(rep.Entries, PermissionEntry{Path: contractPath, Kind: "directory", Status: PermStatusManual,
				Detail: "internal error: the path is not in the bootstrap directory contract"})
			continue
		}
		rep.Entries = append(rep.Entries, r.inspectContractDir(dirPaths[contractPath], spec, group, wantGID))
	}
	rep.Entries = append(rep.Entries, r.inspectLedgerDir(), r.inspectLedgerKey(), r.inspectPluginDir())

	for i := range rep.Entries {
		switch rep.Entries[i].Status {
		case PermStatusChange:
			rep.ChangesRequired++
		case PermStatusManual:
			rep.ManualRequired++
		}
	}
	return rep
}

// Repair inspects and, unless dryRun, applies every required directory-level
// correction. A failure on one path never stops the others; each path's outcome
// is in the report. The error is non-nil only when the run was refused before
// anything changed (not root).
func (r *PermissionsRepairer) Repair(dryRun bool) (*PermissionsReport, error) {
	if !dryRun && r.euid() != 0 {
		return nil, ErrPermissionsNeedRoot
	}
	rep := r.Inspect()
	rep.DryRun = dryRun
	if dryRun {
		return rep, nil
	}
	for i := range rep.Entries {
		e := &rep.Entries[i]
		if e.Status != PermStatusChange || e.fix == nil {
			continue
		}
		if err := e.fix(); err != nil {
			e.Result = PermResultFailed
			e.Error = err.Error()
			rep.Failed++
			continue
		}
		e.Result = PermResultChanged
		rep.Changed++
	}
	return rep, nil
}

// ─── Directories ──────────────────────────────────────────────────────────────

const permModeBits = os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky

func (r *PermissionsRepairer) inspectContractDir(path string, spec bootstrap.DirectoryContractEntry, group string, wantGID int) PermissionEntry {
	e := PermissionEntry{Path: path, Kind: "directory", Expected: fmt.Sprintf("root:%s %04o", group, spec.Mode)}
	fi, uid, gid, ok := r.lstatDir(&e)
	if !ok {
		return e
	}
	mode := fi.Mode() & permModeBits
	e.Current = fmt.Sprintf("%s %04o", ownerLabel(uid, gid), mode)

	var problems []string
	if int(uid) != r.ownerUID {
		problems = append(problems, fmt.Sprintf("owner uid %d, expected %d", uid, r.ownerUID))
	}
	if int(gid) != wantGID {
		problems = append(problems, fmt.Sprintf("group gid %d, expected %d", gid, wantGID))
	}
	target := spec.Mode
	switch {
	case mode == spec.Mode:
	case !spec.Strict && mode&^spec.Mode == 0:
		// Stricter than the contract on a non-strict directory: never loosen it
		// automatically. An owner or group deviation is still corrected, and the
		// existing (stricter) mode is kept.
		target = mode
		e.Detail = fmt.Sprintf("mode %04o is stricter than the contract %04o and is left as is", mode, spec.Mode)
	default:
		problems = append(problems, fmt.Sprintf("mode %04o, expected %04o", mode, spec.Mode))
	}
	if len(problems) == 0 {
		e.Status = PermStatusOK
		return e
	}
	e.Status = PermStatusChange
	e.Detail = strings.Join(problems, "; ")
	apply, owner := r.apply, r.ownerUID
	e.fix = func() error {
		_, err := apply(path, target, owner, wantGID)
		return err
	}
	return e
}

func (r *PermissionsRepairer) inspectLedgerDir() PermissionEntry {
	path := r.layout.LedgerDir
	e := PermissionEntry{Path: path, Kind: "private directory", Expected: fmt.Sprintf("root %04o", bootstrap.LedgerDirMode)}
	fi, uid, gid, ok := r.lstatDir(&e)
	if !ok {
		return e
	}
	mode := fi.Mode() & permModeBits
	e.Current = fmt.Sprintf("%s %04o", ownerLabel(uid, gid), mode)
	if int(uid) != r.ownerUID {
		e.Status = PermStatusManual
		e.Detail = fmt.Sprintf("owned by uid %d, not root: a private directory holding the ledger key is not re-owned automatically; "+
			"establish who created it and fix it by hand (never with chown -R)", uid)
		return e
	}
	if mode == bootstrap.LedgerDirMode {
		e.Status = PermStatusOK
		return e
	}
	e.Status = PermStatusChange
	e.Detail = fmt.Sprintf("mode %04o, expected %04o", mode, bootstrap.LedgerDirMode)
	apply := r.apply
	e.fix = func() error {
		_, err := apply(path, bootstrap.LedgerDirMode, -1, -1)
		return err
	}
	return e
}

func (r *PermissionsRepairer) inspectPluginDir() PermissionEntry {
	path := r.layout.PluginDir
	e := PermissionEntry{Path: path, Kind: "plugin directory", Expected: "root-owned, no group/world write (0755)"}
	fi, uid, gid, ok := r.lstatDir(&e)
	if !ok {
		return e
	}
	mode := fi.Mode() & permModeBits
	e.Current = fmt.Sprintf("%s %04o", ownerLabel(uid, gid), mode)
	if int(uid) != r.ownerUID {
		e.Status = PermStatusManual
		e.Detail = fmt.Sprintf("owned by uid %d, not root: plugin binaries are executed by the root agent, so an attacker-owned directory is "+
			"not repaired by changing its mode; inspect it and reinstall the plugins deliberately", uid)
		return e
	}
	if mode&0o022 == 0 {
		e.Status = PermStatusOK
		return e
	}
	target := mode &^ 0o022
	e.Status = PermStatusChange
	e.Detail = fmt.Sprintf("writable by group or others (mode %04o); only write bits are removed: %04o", mode, target)
	apply := r.apply
	e.fix = func() error {
		_, err := apply(path, target, -1, -1)
		return err
	}
	return e
}

// lstatDir classifies path with Lstat (a symlink is never followed) and
// returns ok=false, with e already filled in, when the path needs no further
// inspection.
func (r *PermissionsRepairer) lstatDir(e *PermissionEntry) (os.FileInfo, uint32, uint32, bool) {
	fi, err := os.Lstat(e.Path)
	switch {
	case err == nil:
	case os.IsNotExist(err):
		e.Status = PermStatusAbsent
		e.Current = "absent"
		e.Detail = "not present; nothing to repair (it is created by bootstrap or the agent)"
		return nil, 0, 0, false
	default:
		e.Status = PermStatusManual
		e.Current = "unreadable"
		e.Detail = "cannot inspect: " + err.Error()
		return nil, 0, 0, false
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		e.Status = PermStatusManual
		e.Current = "symbolic link"
		e.Detail = "a symbolic link is never followed or repaired: replace it with a real directory by hand"
		return nil, 0, 0, false
	}
	if !fi.IsDir() {
		e.Status = PermStatusManual
		e.Current = "not a directory"
		e.Detail = "a file exists at this path: move it away by hand; it is not touched"
		return nil, 0, 0, false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		e.Status = PermStatusManual
		e.Current = "unknown owner"
		e.Detail = "ownership cannot be determined on this platform"
		return nil, 0, 0, false
	}
	return fi, st.Uid, st.Gid, true
}

// ownerLabel renders uid:gid with names where they resolve ("root:dso").
func ownerLabel(uid, gid uint32) string {
	u, g := strconv.FormatUint(uint64(uid), 10), strconv.FormatUint(uint64(gid), 10)
	if usr, err := user.LookupId(u); err == nil {
		u = usr.Username
	}
	if grp, err := user.LookupGroupId(g); err == nil {
		g = grp.Name
	}
	return u + ":" + g
}

// ─── Report rendering ─────────────────────────────────────────────────────────

// WriteText renders the report for a terminal. It contains paths, owners and
// modes and fixed diagnostics only, never file contents.
func (rep *PermissionsReport) WriteText(w io.Writer) {
	if rep.DryRun {
		_, _ = fmt.Fprintln(w, "DSO permission contract (dry run: nothing is modified)")
	} else {
		_, _ = fmt.Fprintln(w, "DSO permission contract")
	}
	_, _ = fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "PATH\tCURRENT\tEXPECTED\tSTATUS")
	for _, e := range rep.Entries {
		status := e.Status
		if e.Result != "" {
			status = e.Result
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", printableLine(e.Path), printableLine(e.Current), printableLine(e.Expected), status)
	}
	_ = tw.Flush()
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintf(w, "Changes required: %d\n", rep.ChangesRequired)
	if !rep.DryRun {
		_, _ = fmt.Fprintf(w, "Changed: %d\nFailed: %d\n", rep.Changed, rep.Failed)
	}
	if rep.ManualRequired > 0 {
		_, _ = fmt.Fprintf(w, "Manual action required: %d\n", rep.ManualRequired)
	}
	if rep.NoDSOGroup {
		_, _ = fmt.Fprintln(w, "Note: no 'dso' group exists on this host; the expected group is root")
	}
	details := false
	for _, e := range rep.Entries {
		if e.Detail == "" && e.Error == "" {
			continue
		}
		if !details {
			_, _ = fmt.Fprintln(w, "\nDetails:")
			details = true
		}
		if e.Status == PermStatusOK || e.Status == PermStatusAbsent {
			if e.Detail != "" {
				_, _ = fmt.Fprintf(w, "  %s: %s\n", printableLine(e.Path), printableLine(e.Detail))
			}
			continue
		}
		if e.Detail != "" {
			_, _ = fmt.Fprintf(w, "  %s: %s\n", printableLine(e.Path), printableLine(e.Detail))
		}
		if e.Error != "" {
			_, _ = fmt.Fprintf(w, "  %s: FAILED: %s\n", printableLine(e.Path), printableLine(e.Error))
		}
	}
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Configuration files will not be rewritten")
	_, _ = fmt.Fprintln(w, "Ledger keys will not be regenerated")
	_, _ = fmt.Fprintln(w, "No recursive permission changes will be performed")
}

// printableLine escapes control characters so a hostile path or error text
// cannot start a new output line or inject terminal sequences.
func printableLine(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strconv.IsPrint(r) {
			b.WriteRune(r)
			continue
		}
		q := strconv.QuoteRuneToASCII(r)
		b.WriteString(q[1 : len(q)-1])
	}
	return b.String()
}
