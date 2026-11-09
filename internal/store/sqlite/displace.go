package sqlite

// Displacement: replacing the active database without destroying the original.
//
// A restore used to remove the active database's -wal, -shm and -journal and then
// rename the replacement over the main file. Both steps destroy bytes: the removal
// is irreversible, and rename(2) over an existing file unlinks the old inode. If
// anything outside this program wrote to the database after the last check, those
// bytes were gone.
//
// ReplaceActiveDatabase instead MOVES the original files aside, into a sibling
// directory, and installs the replacement with an atomic no-replace rename. Nothing
// is deleted and nothing is overwritten:
//
//	0. the replacement is prepared first (PrepareReplacement): copied, fsynced, and
//	   still beside the database under a private name. A failure here changes nothing.
//	1. the baseline of the original files (taken earlier, see Baseline) is checked at
//	   the original paths: a cheap tripwire, not a barrier;
//	2. every source is examined; anything that is not a regular file is refused;
//	3. the aside directory <db>.replaced-<UTC>-<hex> is created (mode 0700) and the
//	   parent directory is synced;
//	4. the MAIN file is moved first, then -wal, -journal and -shm, each by an atomic
//	   no-replace rename. A companion that vanished between step 2 and its move is
//	   reported as vanished;
//	5. the displaced files are checked against the baseline (size, mtime, inode; NOT
//	   ctime, which the rename itself changes);
//	6. the aside directory and then the parent are synced;
//	7. the prepared replacement is re-checked (same file, same stat signature:
//	   size, mtime, inode, device, ctime) and installed with a no-replace rename. A
//	   changed, substituted or missing replacement is refused and everything is
//	   rolled back. A gap remains between that check and the rename that no check
//	   can close, and it is not a defence against a process that ignores the lock
//	   or against another process of the same user;
//	8. the parent is synced again. If that last sync fails the replacement IS
//	   installed: the error is an *InstallDurabilityError, never an install failure.
//
// Why the main file moves first: a crash then leaves "no main file, companions
// still beside it", a state a restore refuses (orphan companions). Moving the
// companions first would leave an old main file without its -wal or -journal,
// which looks like a valid, older database and is not detectable.
//
// Any failure before the installation rolls back (see rollback) in reverse order,
// with no-replace renames only. The rollback is all-or-nothing in intent: if the
// original main path has been taken by another file, NOTHING is moved back,
// because an old -wal or -journal beside a different database can be replayed onto
// it. If a rollback step fails, the companions already moved back are displaced
// again. Whatever remains in the aside directory is reported; it is never deleted.
//
// What this does NOT do. It does not stop a process that ignores the DSO lock
// from writing to the database while this runs, or from keeping the old files open
// (a "ghost" connection). Such a writer's later commits go to the displaced files,
// which are retained, but they are not in the restored database. The lock-typed
// methods prove only that THIS process holds the DSO lock for this path.
//
// A crash can leave the aside directory, a "<db>.restoring-<hex>" replacement
// file, or both. Nothing here removes them.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker-secret-operator/dso/internal/securefs"
)

var (
	// ErrDisplaceNoReplaceUnsupported: the platform, kernel or filesystem cannot
	// rename without replacing. Nothing falls back to an overwriting rename.
	ErrDisplaceNoReplaceUnsupported = errors.New("sqlite: atomic no-replace rename is not supported here, so the active database cannot be displaced or replaced safely")
	// ErrDisplaceBlocked: something already exists at a destination. It was not
	// overwritten.
	ErrDisplaceBlocked = errors.New("sqlite: something already exists at the destination; it was not overwritten")
	// ErrDisplaceBaselineChanged: the active database files changed after their
	// baseline was taken.
	ErrDisplaceBaselineChanged = errors.New("sqlite: the active database files changed after their baseline was taken")
	// ErrDisplaceOutcomeUnknown: a rename was interrupted and its outcome could
	// not be established.
	ErrDisplaceOutcomeUnknown = errors.New("sqlite: the outcome of a rename could not be established")
	// ErrRollbackIncomplete: the displacement could not be fully rolled back; the
	// original files are retained where the error reports.
	ErrRollbackIncomplete = errors.New("sqlite: the displacement could not be fully rolled back; the original files are retained where reported")
	// ErrDisplaceReplacementChanged: the prepared replacement file is no longer
	// the file that was prepared (modified, substituted or removed), so it was not
	// installed.
	ErrDisplaceReplacementChanged = errors.New("sqlite: the prepared replacement file changed before it could be installed")
	// ErrDisplaceCompanionAppeared: a -wal or -journal appeared at the original path
	// after the original files were moved aside, so installing the replacement
	// beside it could corrupt the restored database. Nothing was installed.
	ErrDisplaceCompanionAppeared = errors.New("sqlite: a -wal or -journal appeared at the original path before the replacement could be installed")
	// ErrDisplaceInstallOutcomeUnknown: the install rename returned an error and
	// the filesystem does not show whether the replacement was installed. Nothing
	// was rolled back (rolling back could displace a database that is in fact
	// installed); the original files stay where DisplacementError reports.
	ErrDisplaceInstallOutcomeUnknown = errors.New("sqlite: the outcome of installing the replacement could not be established")
)

// DisplacedFile is one original file that was moved aside.
type DisplacedFile struct {
	Name     string // "main", "wal", "journal" or "shm"
	Original string // where it was
	Retained string // where it is now
}

// DisplaceResult reports what ReplaceActiveDatabase did. It is never nil.
type DisplaceResult struct {
	// Dir is the aside directory ("" when nothing is retained there).
	Dir string
	// Files are the original files retained in Dir.
	Files []DisplacedFile
	// Vanished names companions that disappeared between the examination and
	// their move (nothing was moved for them).
	Vanished []string
	// Installed is true once the replacement is in place.
	Installed bool
	// Verified is true only when the installed file was checked after the rename:
	// it is the very file that was prepared, its content matches the hash recorded
	// while it was written, and it did not change while it was checked. It is false
	// whenever Installed is false, and when verification failed or could not run
	// (the error is then an *InstalledFileChangedError).
	//
	// Verified means the CONTENT of the installed file was verified. It does not
	// mean the directory entry is durable: Verified can be true together with an
	// *InstallDurabilityError, when only the parent directory sync failed.
	Verified bool
}

// InstallState says what is known about the install rename after a failed
// displacement.
type InstallState string

const (
	// InstallNotInstalled: the replacement was definitely not installed.
	InstallNotInstalled InstallState = "not-installed"
	// InstallUnknown: the install rename failed and the filesystem does not show
	// whether the replacement was installed. Nothing was rolled back.
	InstallUnknown InstallState = "unknown"
)

// MainState says where the original main database file is after a failed
// displacement, as established by looking at the filesystem (never inferred from
// whether the rollback reported an error).
type MainState string

const (
	// MainUntouched: this operation moved nothing, so the main file was not touched.
	MainUntouched MainState = "untouched"
	// MainAtOriginal: the main file was moved and is confirmed back at its original path.
	MainAtOriginal MainState = "at-original"
	// MainNone: there was no main file to displace (a missing active database).
	MainNone MainState = "none"
	// MainRetained: the main file is in the aside directory.
	MainRetained MainState = "retained"
	// MainUnknown: the main file could not be found at either path.
	MainUnknown MainState = "unknown"
	// MainVanished: the main file disappeared before it could be moved.
	MainVanished MainState = "vanished"
)

// DisplacementError is returned when the replacement was NOT installed.
type DisplacementError struct {
	Stage string
	Err   error
	Dir   string
	// Main is where the original main database file is. The zero value means the
	// error was not produced by a displacement and is treated as "not in place".
	Main MainState
	// Install is InstallNotInstalled for every failure in which the replacement was
	// definitely not installed, and InstallUnknown when the install rename failed and
	// its outcome could not be established. With InstallUnknown the active path may
	// hold the restored database or something else; no rollback was attempted and
	// RolledBack is false.
	Install InstallState
	// RolledBack is true only when every involved original file has been
	// CONFIRMED at its original path (or nothing had been moved), and nothing
	// vanished. A file whose location is unknown, retained, vanished, or
	// restored-but-stuck makes it false.
	RolledBack bool
	// Retained are the original files that are in Dir after the operation ended.
	Retained []DisplacedFile
	// Unknown lists the original paths of files whose location could not be
	// established. UnknownDetail has one line per entry, in the same order, saying
	// what was found at each candidate path.
	Unknown       []string
	UnknownDetail []string
	// Vanished lists files (original paths) that disappeared between the
	// examination and their move. They are neither retained nor restored: nothing
	// in this operation put them anywhere.
	Vanished []string
	// NotMoved lists original files that were never moved and are still at their
	// original path although others were displaced.
	NotMoved []string
	// RestoredInPlace lists companions that the rollback put back at their original
	// path and that the compensation then could not displace again: they sit at
	// their original path beside a missing or foreign main file.
	RestoredInPlace []string
	// Notes are facts about the end state that do not change where any original
	// file is, but that an operator must know: a directory sync after the rollback
	// failed (so the rollback is not confirmed crash-durable), or the empty aside
	// directory could not be removed.
	Notes []string
}

// OriginalInPlace reports whether the original main database file is confirmed at
// its original path (or never moved, or never existed) and no other original file
// is retained, of unknown location, or restored-but-stuck. A vanished companion
// does not prevent it: it is gone, not misplaced, and is reported separately.
func (e *DisplacementError) OriginalInPlace() bool {
	if e.Install == InstallUnknown {
		// The install rename may have taken effect: the active path may hold the
		// replacement, so nothing can be concluded about the original being in place,
		// whatever Main says (with no active database Main is MainNone).
		return false
	}
	switch e.Main {
	case MainUntouched, MainAtOriginal, MainNone:
	default:
		return false
	}
	return len(e.Retained) == 0 && len(e.Unknown) == 0 && len(e.RestoredInPlace) == 0
}

// Error describes the state from the structured fields only. It never says that
// files are retained unless Retained lists them, never says an original is back
// unless the main file is confirmed back, and says plainly what vanished or
// could not be found.
func (e *DisplacementError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "replacing the active database failed at %s: %v", e.Stage, e.Err)
	if e.Install == InstallUnknown {
		b.WriteString(" (NOT rolled back, because the replacement may be installed: the file at the original path may be the restored database; ")
		if len(e.Retained) > 0 {
			var list []string
			for _, f := range e.Retained {
				list = append(list, fmt.Sprintf("%s: %s -> %s", f.Name, f.Original, f.Retained))
			}
			fmt.Fprintf(&b, "the original files are retained in %s (%s)", e.Dir, strings.Join(list, "; "))
		} else {
			b.WriteString("no original file was moved")
		}
		for _, n := range e.Notes {
			b.WriteString("; note: " + n)
		}
		b.WriteString("; nothing was deleted or overwritten)")
		return b.String()
	}
	if e.RolledBack {
		b.WriteString(" (rolled back: every original file is back at its original path")
		for _, n := range e.Notes {
			b.WriteString("; however, " + n)
		}
		b.WriteString(")")
		return b.String()
	}
	var parts []string
	switch e.Main {
	case MainUntouched:
		parts = append(parts, "no file was moved")
	case MainAtOriginal:
		parts = append(parts, "the original main database file is back at its original path")
	case MainNone:
		parts = append(parts, "there was no main database file to displace")
	case MainRetained:
		parts = append(parts, "the original main database file is NOT at its original path: it is retained, as listed below")
	case MainUnknown:
		parts = append(parts, "the location of the original main database file could not be established")
	case MainVanished:
		parts = append(parts, "the original main database file vanished before it could be moved and is not at its original path")
	}
	if len(e.Retained) > 0 {
		var list []string
		for _, f := range e.Retained {
			list = append(list, fmt.Sprintf("%s: %s -> %s", f.Name, f.Original, f.Retained))
		}
		where := ""
		if e.Dir != "" {
			where = " in " + e.Dir
		}
		parts = append(parts, "retained"+where+" ("+strings.Join(list, "; ")+")")
	}
	unknown := e.UnknownDetail
	if len(unknown) != len(e.Unknown) {
		unknown = e.Unknown
	}
	for _, u := range unknown {
		parts = append(parts, "location could not be established: "+u)
	}
	for _, v := range e.Vanished {
		parts = append(parts, "vanished before it could be moved, so it is neither retained nor restored: "+v)
	}
	for _, n := range e.NotMoved {
		parts = append(parts, "never moved, still at "+n)
	}
	for _, r := range e.RestoredInPlace {
		parts = append(parts, "restored by the rollback and could not be displaced again, still at its original path "+r)
	}
	for _, n := range e.Notes {
		parts = append(parts, "note: "+n)
	}
	if len(parts) == 0 {
		// Never print an empty clause: say plainly that the state is not known.
		parts = append(parts, "the state of the original files is not known")
	}
	fmt.Fprintf(&b, " (NOT rolled back: %s; nothing was deleted or overwritten)", strings.Join(parts, "; "))
	return b.String()
}

func (e *DisplacementError) Unwrap() error { return e.Err }

// InstallDurabilityError is returned together with a DisplaceResult whose
// Installed is true: the replacement IS in place, but syncing the parent
// directory after the rename failed, so the rename is not yet crash-durable.
type InstallDurabilityError struct{ err error }

func (e *InstallDurabilityError) Error() string { return e.err.Error() }
func (e *InstallDurabilityError) Unwrap() error { return e.err }

// InstalledFileKind classifies why an installed file failed verification.
type InstalledFileKind string

const (
	// InstalledGone: nothing is at the active path any more.
	InstalledGone InstalledFileKind = "gone"
	// InstalledNotRegular: the active path is not a regular file.
	InstalledNotRegular InstalledFileKind = "not-regular"
	// InstalledReplaced: the active path holds a different file than the one installed.
	InstalledReplaced InstalledFileKind = "replaced"
	// InstalledContentChanged: the file's size or SHA-256 differs from what was written.
	InstalledContentChanged InstalledFileKind = "content-changed"
	// InstalledUnstable: the file changed while it was being verified.
	InstalledUnstable InstalledFileKind = "unstable"
	// InstalledUnreadable: the file could not be opened or read, so it is unverified.
	InstalledUnreadable InstalledFileKind = "unreadable"
)

// InstalledFileChangedError is returned together with a DisplaceResult whose
// Installed is true and Verified is false: the replacement WAS installed (renamed
// into place), but the file now at the active path could not be confirmed to be it.
// The original files were not rolled back (the installed path is occupied) and
// stay in the aside directory the result reports.
type InstalledFileChangedError struct {
	Path   string
	Kind   InstalledFileKind
	Detail string
	// SyncErr is the failure of the parent directory sync, if it also failed.
	SyncErr error
}

// Mismatch reports positive evidence that the file at the active path is not the
// installed replacement (as opposed to being merely unverified).
func (e *InstalledFileChangedError) Mismatch() bool { return e.Kind != InstalledUnreadable }

func (e *InstalledFileChangedError) Error() string {
	msg := fmt.Sprintf("the replacement was installed at %s but the file there could not be verified (%s): %s", e.Path, e.Kind, e.Detail)
	if e.SyncErr != nil {
		msg += fmt.Sprintf("; syncing the parent directory also failed: %v", e.SyncErr)
	}
	return msg
}

// ---- no-replace moves ------------------------------------------------------------------

// moveWith renames oldpath to newpath through rename, which must refuse to
// replace, and maps the result to this file's errors. EINTR is resolved from file
// identity (publishDirWith), never blindly retried.
func moveWith(rename func(oldpath, newpath string) error, oldpath, newpath string) error {
	err := publishDirWith(rename, oldpath, newpath)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrPreserveNoReplaceUnsupported):
		return fmt.Errorf("%w: %w", ErrDisplaceNoReplaceUnsupported, err)
	case errors.Is(err, ErrPreserveDestinationExists):
		return fmt.Errorf("%w: %s: %w", ErrDisplaceBlocked, newpath, err)
	case errors.Is(err, ErrPreserveOutcomeUnknown):
		return fmt.Errorf("%w: %w", ErrDisplaceOutcomeUnknown, err)
	}
	return err
}

func moveNoReplace(oldpath, newpath string) error { return moveWith(renameNoReplace, oldpath, newpath) }

// ---- the displacer (every tunable and test seam; production uses newDisplacer) -----------

type displacer struct {
	now        func() time.Time
	random     func([]byte) (int, error)
	mkdir      func(path string, perm os.FileMode) error
	rename     func(oldpath, newpath string) error
	syncDir    func(dir string) error
	removeDir  func(path string) error // os.Remove: only ever an empty directory
	createFile func(path string, mode os.FileMode) (*os.File, error)
	syncFile   func(f *os.File) error
	closeFile  func(f *os.File) error
	wrapWriter func(w io.Writer) io.Writer
	// wrapReader wraps the reader the installed file is hashed through.
	wrapReader func(r io.Reader) io.Reader
	// statInstalled and openInstalled examine the active path after the install.
	statInstalled func(path string) (os.FileInfo, error)
	openInstalled func(path string) (*os.File, error)
	// openPrepared and wrapPreparedReader are the same for the pre-displacement
	// check of the prepared replacement.
	openPrepared       func(path string) (*os.File, error)
	wrapPreparedReader func(r io.Reader) io.Reader
	// hook is called at fixed points (see step names in replace); tests use it to
	// change a file at an exact moment or to kill the process there.
	hook func(step string)
	// beforeInstall runs after the displaced files were verified and synced and
	// immediately before the installation.
	beforeInstall func()
}

func newDisplacer() *displacer {
	return &displacer{
		now:        time.Now,
		random:     rand.Read,
		mkdir:      os.Mkdir,
		rename:     moveNoReplace,
		syncDir:    fsyncDir,
		removeDir:  os.Remove,
		createFile: securefs.CreateExclusive,
		syncFile:   (*os.File).Sync,
		closeFile:  (*os.File).Close,

		statInstalled: os.Lstat,
		openInstalled: securefs.OpenPrivateFile,
		openPrepared:  securefs.OpenPrivateFile,
	}
}

func (d *displacer) step(name string) {
	if d.hook != nil {
		d.hook(name)
	}
}

// DisplaceOption adjusts one call of ReplaceActiveDatabase. It exists for tests
// and fault injection; production passes none.
type DisplaceOption func(*displacer)

// WithBeforeInstall runs f after the displaced files are verified and synced and
// before the replacement is installed.
func WithBeforeInstall(f func()) DisplaceOption {
	return func(d *displacer) { d.beforeInstall = f }
}

// ---- the lock-typed API ------------------------------------------------------------------

// check refuses a nil or released lock and any path other than the one the lock
// was acquired for, and returns the absolute path to operate on. It proves only
// that THIS process holds the DSO lock for this path; it says nothing about a
// process that does not use the lock.
func (r *RestoreLock) check(dbPath string) (string, error) {
	if r == nil {
		return "", errors.New("sqlite: a nil RestoreLock cannot authorize a change to the active database")
	}
	if r.lock == nil || r.lock.closed {
		return "", errors.New("sqlite: the RestoreLock was already released and cannot authorize a change to the active database")
	}
	resolved, err := absPathForLock(dbPath)
	if err != nil {
		return "", err
	}
	if resolved != r.path {
		return "", fmt.Errorf("sqlite: the restore lock held for %s cannot authorize a change to %s", r.path, resolved)
	}
	return filepath.Abs(dbPath)
}

// PrepareReplacement copies candidatePath into a new private file beside dbPath,
// fsyncs it and returns it, WITHOUT touching the active database or its
// companions. Any failure removes only the file this call created.
func (r *RestoreLock) PrepareReplacement(dbPath, candidatePath string) (*Replacement, error) {
	abs, err := r.check(dbPath)
	if err != nil {
		return nil, err
	}
	return newDisplacer().prepare(abs, candidatePath)
}

// BaselineAfterClose records the state of an EXISTING active database whose
// connections have all been closed (so SQLite's own changes are finished): the
// main file, -wal and -journal, each as signature, hash, signature, which must
// match.
func (r *RestoreLock) BaselineAfterClose(dbPath string) (*Baseline, error) {
	abs, err := r.check(dbPath)
	if err != nil {
		return nil, err
	}
	return (&baseliner{}).capture(abs, true)
}

// BaselineNoActive records that there is no main file at dbPath, together with
// any (empty) companions beside it.
func (r *RestoreLock) BaselineNoActive(dbPath string) (*Baseline, error) {
	abs, err := r.check(dbPath)
	if err != nil {
		return nil, err
	}
	b, err := (&baseliner{}).capture(abs, false)
	if err != nil {
		return nil, err
	}
	if b.main.exists {
		return nil, fmt.Errorf("%w: a file appeared at %s", ErrDisplaceBaselineChanged, abs)
	}
	return b, nil
}

// ReplaceActiveDatabase displaces the original files and installs the prepared
// replacement; see the file comment for the exact sequence and its guarantees.
// The returned result is never nil. The error is a *DisplacementError when the
// replacement was not installed, and an *InstallDurabilityError when it was.
func (r *RestoreLock) ReplaceActiveDatabase(dbPath string, repl *Replacement, b *Baseline, opts ...DisplaceOption) (*DisplaceResult, error) {
	abs, err := r.check(dbPath)
	if err != nil {
		return &DisplaceResult{}, err
	}
	d := newDisplacer()
	for _, o := range opts {
		o(d)
	}
	return d.replace(abs, repl, b)
}

// ---- the replacement file ----------------------------------------------------------------

// Replacement is a prepared replacement database file.
type Replacement struct {
	path   string
	dbPath string
	info   os.FileInfo // identity of the file created (taken when it was created)
	// sig is the stat signature taken after the final close; verify compares it
	// immediately before the installation.
	sig statSig
	// hash and size are the SHA-256 and length of the bytes written into the file,
	// recorded while it was written; the post-install check compares against them.
	hash      string
	size      int64
	installed bool
	finished  bool
}

// Path is the temporary file's current path.
func (r *Replacement) Path() string { return r.path }

// verify checks, just before the installation, that the prepared path still
// refers to the very file that was prepared and that it is unchanged: a regular
// file, the same device and inode, and the same size, mtime and ctime. Nothing
// has renamed it yet, so its ctime is stable. It removes nothing and returns an
// error matching ErrDisplaceReplacementChanged otherwise.
//
// This narrows the window, it does not close it: the file can still change
// between this check and the rename, and the check does not stop a process that
// ignores the DSO lock or another process of the same user.
func (r *Replacement) verify() error {
	changed := func(format string, a ...any) error {
		return fmt.Errorf("%w: %s", ErrDisplaceReplacementChanged, fmt.Sprintf(format, a...))
	}
	fi, err := os.Lstat(r.path)
	if err != nil {
		return changed("%s cannot be examined: %v", r.path, err)
	}
	if !fi.Mode().IsRegular() {
		return changed("%s is no longer a regular file", r.path)
	}
	if !os.SameFile(fi, r.info) {
		return changed("%s is no longer the file that was prepared", r.path)
	}
	if statSigOf(fi) != r.sig {
		return changed("%s was modified after it was prepared (its stat signature differs)", r.path)
	}
	return nil
}

// verifyPrepared re-checks the prepared replacement before any original file is
// moved. It opens the file through the secure opener (no symlink follow; regular
// file; owned by the current user; no group/other access), then on that one
// descriptor: checks the identity against the file that was created, compares the
// stat signature recorded after the final close, hashes the content and compares
// the hash and length recorded while the file was written, and checks the
// signature again after the read. Finally the path must still refer to the same
// file. It removes and moves nothing. Every failure matches
// ErrDisplaceReplacementChanged.
//
// This reduces the chance of displacing the original database for a replacement
// that is already bad. It does not remove the window between this check and the
// installation (a process that ignores the lock can still change the file).
func (d *displacer) verifyPrepared(r *Replacement) error {
	bad := func(format string, a ...any) error {
		return fmt.Errorf("%w: %s (nothing was moved; the original database and its companions are untouched)",
			ErrDisplaceReplacementChanged, fmt.Sprintf(format, a...))
	}
	f, err := d.openPrepared(r.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return bad("%s no longer exists: %v", r.path, err)
		}
		if fi, lerr := os.Lstat(r.path); lerr == nil {
			if !fi.Mode().IsRegular() {
				return bad("%s is not a regular file (%v)", r.path, err)
			}
			if !os.SameFile(fi, r.info) {
				return bad("%s is not the file that was prepared (%v)", r.path, err)
			}
		}
		return bad("%s could not be opened securely, so it was not verified: %v", r.path, err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return bad("%s could not be examined: %v", r.path, err)
	}
	if !fi.Mode().IsRegular() {
		return bad("%s is not a regular file", r.path)
	}
	if !os.SameFile(fi, r.info) {
		return bad("%s is not the file that was prepared", r.path)
	}
	sig := statSigOf(fi)
	if sig != r.sig {
		return bad("%s was modified after it was prepared (its stat signature differs)", r.path)
	}
	var rd io.Reader = f
	if d.wrapPreparedReader != nil {
		rd = d.wrapPreparedReader(f)
	}
	hash, n, err := hashReader(rd)
	if err != nil {
		return bad("%s could not be read, so it was not verified: %v", r.path, err)
	}
	if n != r.size || hash != r.hash {
		return bad("the content of %s (%d bytes, sha256 %s) is not what was written when it was prepared (%d bytes, sha256 %s)",
			r.path, n, hash, r.size, r.hash)
	}
	fi2, err := f.Stat()
	if err != nil {
		return bad("%s could not be examined after reading: %v", r.path, err)
	}
	if statSigOf(fi2) != sig {
		return bad("%s changed while it was being verified", r.path)
	}
	at, err := os.Lstat(r.path)
	if err != nil {
		return bad("%s cannot be examined after verification: %v", r.path, err)
	}
	if !at.Mode().IsRegular() || !os.SameFile(at, fi2) || statSigOf(at) != sig {
		return bad("%s no longer refers to the file that was verified", r.path)
	}
	return nil
}

// Abandon removes the temporary file, but only if the path still refers to the
// very file this operation created, and never after the replacement was
// installed. It is safe to call more than once and on a nil value.
func (r *Replacement) Abandon() error {
	if r == nil || r.installed || r.finished {
		return nil
	}
	r.finished = true
	return removeIfSame(r.path, r.info)
}

// removeIfSame removes path only if it is still the file described by want.
func removeIfSame(path string, want os.FileInfo) error {
	fi, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if want == nil || !os.SameFile(fi, want) {
		return fmt.Errorf("%s is no longer the temporary file this operation created; it was left in place", path)
	}
	return os.Remove(path)
}

func (d *displacer) prepare(dbPath, candidatePath string) (*Replacement, error) {
	dir := filepath.Dir(dbPath)
	if err := securefs.EnsureDir(dir); err != nil {
		return nil, fmt.Errorf("active database directory is not safe: %w", err)
	}
	var rnd [6]byte
	if _, err := d.random(rnd[:]); err != nil {
		return nil, err
	}
	tmp := fmt.Sprintf("%s.restoring-%s", dbPath, hex.EncodeToString(rnd[:]))

	src, err := os.Open(candidatePath) // #nosec G304 -- the staged candidate: opened read-only and never written to
	if err != nil {
		return nil, fmt.Errorf("opening restore candidate: %w", err)
	}
	defer src.Close()

	dst, err := d.createFile(tmp, 0o600)
	if err != nil {
		return nil, fmt.Errorf("creating temporary restore file: %w", err)
	}
	info, err := dst.Stat()
	if err != nil {
		_ = d.closeFile(dst)
		_ = os.Remove(tmp) // created exclusively a moment ago by this call
		return nil, fmt.Errorf("examining temporary restore file: %w", err)
	}
	fail := func(msg string, cause error, closed bool) (*Replacement, error) {
		if !closed {
			_ = d.closeFile(dst)
		}
		_ = removeIfSame(tmp, info)
		return nil, fmt.Errorf("%s: %w", msg, cause)
	}
	var w io.Writer = dst
	if d.wrapWriter != nil {
		w = d.wrapWriter(dst)
	}
	// The hash is of the bytes read from the candidate, not of what reached the
	// file, so a write that was altered on the way is caught after the install.
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, h), src)
	if err != nil {
		return fail("copying restore candidate", err, false)
	}
	if err := d.syncFile(dst); err != nil {
		return fail("syncing restored database", err, false)
	}
	if err := d.closeFile(dst); err != nil {
		return fail("closing restored database", err, true)
	}
	// The signature that verify compares just before the installation, taken after
	// the final close so it describes the finished file.
	fi, err := os.Lstat(tmp)
	if err != nil {
		_ = removeIfSame(tmp, info)
		return nil, fmt.Errorf("examining the prepared replacement: %w", err)
	}
	return &Replacement{path: tmp, dbPath: dbPath, info: info, sig: statSigOf(fi), hash: hex.EncodeToString(h.Sum(nil)), size: n}, nil
}

// ---- baselines ---------------------------------------------------------------------------

type baselineFile struct {
	exists bool
	info   os.FileInfo
	sig    statSig
	hash   string
	size   int64
}

// Baseline is what the original files looked like at a moment the caller trusts:
// the main file, -wal and -journal (the -shm is advisory and not recorded), each
// as a stat signature plus a hash. Take it as late as possible.
type Baseline struct {
	dbPath             string
	main, wal, journal baselineFile
}

// baseliner captures baselines. afterHash is a test seam run between the hash and
// the second signature.
type baseliner struct{ afterHash func() }

// file takes one file's baseline as signature, hash, signature: the two
// signatures, and the path's identity, must agree, or the file changed while it
// was read and the capture fails. A file that does not exist is recorded as such.
func (bl *baseliner) file(path string, private bool) (baselineFile, error) {
	var f *os.File
	var err error
	if private {
		f, err = securefs.OpenPrivateFile(path)
	} else {
		f, err = securefs.OpenRegularNoFollow(path)
	}
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return baselineFile{}, nil
		}
		return baselineFile{}, err
	}
	defer f.Close()
	fi1, err := f.Stat()
	if err != nil {
		return baselineFile{}, err
	}
	sig1 := statSigOf(fi1)
	sum, n, err := hashReader(f)
	if err != nil {
		return baselineFile{}, err
	}
	if bl.afterHash != nil {
		bl.afterHash()
	}
	fi2, err := f.Stat()
	if err != nil {
		return baselineFile{}, err
	}
	pfi, perr := os.Lstat(path)
	if statSigOf(fi2) != sig1 || perr != nil || !os.SameFile(pfi, fi1) {
		return baselineFile{}, fmt.Errorf("%w: %s changed while its baseline was being taken", ErrDisplaceBaselineChanged, path)
	}
	return baselineFile{exists: true, info: fi1, sig: sig1, hash: sum, size: n}, nil
}

func (bl *baseliner) capture(dbPath string, requireMain bool) (*Baseline, error) {
	b := &Baseline{dbPath: dbPath}
	var err error
	if b.main, err = bl.file(dbPath, true); err != nil {
		return nil, err
	}
	if requireMain && !b.main.exists {
		return nil, fmt.Errorf("%w: %s no longer exists", ErrDisplaceBaselineChanged, dbPath)
	}
	if b.wal, err = bl.file(dbPath+"-wal", false); err != nil {
		return nil, err
	}
	if b.journal, err = bl.file(dbPath+"-journal", false); err != nil {
		return nil, err
	}
	return b, nil
}

// VerifiedBaseline returns a baseline of the original files after proving, in the
// same pass, that they are exactly what this preservation holds: the identified
// main file with the preserved content, and a -wal and -journal present with the
// preserved content exactly when they were preserved. It is for flows that never
// opened the original.
func (p *Preservation) VerifiedBaseline() (*Baseline, error) {
	changed := func(format string, a ...any) error {
		return fmt.Errorf("%w: %s", ErrPreserveSourceChanged, fmt.Sprintf(format, a...))
	}
	main, ok := p.record("main")
	if !ok {
		return nil, errors.New("sqlite: the preservation has no main file record")
	}
	b, err := (&baseliner{}).capture(p.dbPath, true)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPreserveSourceChanged, err)
	}
	if !p.expect.matches(b.main.info) {
		return nil, changed("%s is no longer the identified file", p.dbPath)
	}
	if b.main.hash != main.SHA256 || b.main.size != main.Size {
		return nil, changed("%s content differs", p.dbPath)
	}
	for _, c := range []struct {
		name string
		file baselineFile
	}{{"wal", b.wal}, {"journal", b.journal}} {
		rec, preserved := p.record(c.name)
		path := p.dbPath + sidecarSuffix(c.name)
		switch {
		case c.file.exists && !preserved:
			return nil, changed("%s appeared", path)
		case !c.file.exists && preserved:
			return nil, changed("%s disappeared", path)
		case c.file.exists && (c.file.hash != rec.SHA256 || c.file.size != rec.Size):
			return nil, changed("%s content differs", path)
		}
	}
	return b, nil
}

// verifyAtPaths is the pre-displacement tripwire: every recorded file is still
// the same file with the same signature (ctime included, nothing has been renamed
// yet), and nothing appeared. It is a detector, not a barrier: a write in the same
// timestamp tick, or through an already-dirty memory mapping, can evade it.
func (b *Baseline) verifyAtPaths() error {
	changed := func(format string, a ...any) error {
		return fmt.Errorf("%w: %s", ErrDisplaceBaselineChanged, fmt.Sprintf(format, a...))
	}
	for _, e := range []struct {
		name string
		path string
		want baselineFile
	}{{"main", b.dbPath, b.main}, {"wal", b.dbPath + "-wal", b.wal}, {"journal", b.dbPath + "-journal", b.journal}} {
		fi, err := os.Lstat(e.path)
		switch {
		case err == nil && !e.want.exists:
			return changed("%s appeared", e.path)
		case err == nil:
			if !fi.Mode().IsRegular() || !os.SameFile(fi, e.want.info) {
				return changed("%s is no longer the file whose baseline was taken", e.path)
			}
			if statSigOf(fi) != e.want.sig {
				return changed("%s was modified (its stat signature differs)", e.path)
			}
		case errors.Is(err, os.ErrNotExist):
			if e.want.exists {
				return changed("%s disappeared", e.path)
			}
		default:
			return fmt.Errorf("examining %s: %w", e.path, err)
		}
	}
	return nil
}

// unmovedState describes the original files when this operation moved nothing,
// from the filesystem: the main file is untouched only if it is still there; a
// recorded main file, -wal or -journal that is now absent has vanished. It never
// claims that a missing file is in place.
func (b *Baseline) unmovedState() (MainState, []string) {
	gone := func(path string, want baselineFile) bool {
		if !want.exists {
			return false
		}
		_, err := os.Lstat(path)
		return err != nil && errors.Is(err, os.ErrNotExist)
	}
	var vanished []string
	state := MainNone
	if b.main.exists {
		state = MainUntouched
		if gone(b.dbPath, b.main) {
			state = MainVanished
			vanished = append(vanished, b.dbPath)
		}
	}
	if gone(b.dbPath+"-wal", b.wal) {
		vanished = append(vanished, b.dbPath+"-wal")
	}
	if gone(b.dbPath+"-journal", b.journal) {
		vanished = append(vanished, b.dbPath+"-journal")
	}
	return state, vanished
}

// verifyDisplaced checks a moved file against the baseline using only attributes
// a rename leaves alone (size, mtime, device, inode). ctime is excluded: the
// rename itself updates it (observed on tmpfs; expected on ext4).
func (b *Baseline) verifyDisplaced(it *dItem) error {
	var want baselineFile
	switch it.name {
	case "main":
		want = b.main
	case "wal":
		want = b.wal
	case "journal":
		want = b.journal
	default:
		return nil // the -shm is advisory
	}
	fi, err := os.Lstat(it.retained)
	if err != nil {
		return fmt.Errorf("examining the displaced %s: %w", it.name, err)
	}
	s := statSigOf(fi)
	if !want.exists || !os.SameFile(fi, want.info) || s.size != want.sig.size || s.mtimeNs != want.sig.mtimeNs || s.ino != want.sig.ino || s.dev != want.sig.dev {
		return fmt.Errorf("%w: the displaced %s differs from its baseline (it was written to after the baseline was taken)", ErrDisplaceBaselineChanged, it.name)
	}
	return nil
}

// ---- the displacement ----------------------------------------------------------------------

type dItem struct {
	name     string
	orig     string
	retained string
	info     os.FileInfo // identity at the examination
	// pre is the identity of the file at the original path immediately before the
	// rename was issued (the examination may be older: a file can be swapped in
	// between the two).
	pre os.FileInfo
	// movedInfo is the identity of the file that was ACTUALLY moved: taken at the
	// retained path right after a successful rename, or, after a rename that reported
	// an error, from whatever is found there (see settle). It is the one to follow.
	movedInfo os.FileInfo
}

// identity is the file this item is currently believed to be: the one that was
// actually moved if known, else the one seen just before the rename, else the
// one examined.
func (it *dItem) identity() os.FileInfo {
	switch {
	case it.movedInfo != nil:
		return it.movedInfo
	case it.pre != nil:
		return it.pre
	}
	return it.info
}

func pathAbsent(path string) bool {
	_, err := os.Lstat(path)
	return err != nil && errors.Is(err, os.ErrNotExist)
}

// settle establishes, after a rename reported an error, which file is where. It
// never assumes that the file examined earlier is the file that moved:
//
//   - a file at the retained path that is the one seen just before the rename: it
//     moved;
//   - a file at the retained path while the original path is empty: this rename
//     moved whatever it found (a file swapped in after the last look); it is
//     followed from then on;
//   - the file seen just before the rename still at the original path: not moved;
//   - anything else is unknown, and stays fail-closed.
func settle(it *dItem) location {
	fiR, errR := os.Lstat(it.retained)
	fiO, errO := os.Lstat(it.orig)
	if errR == nil {
		if it.pre != nil && os.SameFile(fiR, it.pre) {
			it.movedInfo = fiR
			return atRetained
		}
		if errO != nil && errors.Is(errO, os.ErrNotExist) {
			it.movedInfo = fiR
			return atRetained
		}
	}
	if errO == nil && it.pre != nil && os.SameFile(fiO, it.pre) {
		return atOriginal
	}
	return atUnknown
}

// describeCandidates says what is at each path where an item of unknown location
// could be, without claiming more than was seen.
func describeCandidates(it *dItem) string {
	id := it.identity()
	state := func(path string) string {
		fi, err := os.Lstat(path)
		switch {
		case err != nil && errors.Is(err, os.ErrNotExist):
			return "nothing there"
		case err != nil:
			return "cannot be examined"
		case id != nil && os.SameFile(fi, id):
			return "the file that was moved or examined"
		}
		return "a different file than the one examined"
	}
	return fmt.Sprintf("%s (at its original path: %s; at %s: %s)", it.orig, state(it.orig), it.retained, state(it.retained))
}

// prescan examines the original files in displacement order: main first, then
// -wal, -journal, -shm. A file that exists but is not a regular file is refused.
func prescan(dbPath string) ([]*dItem, error) {
	var items []*dItem
	for _, c := range []struct{ name, sfx string }{{"main", ""}, {"wal", "-wal"}, {"journal", "-journal"}, {"shm", "-shm"}} {
		path := dbPath + c.sfx
		fi, err := os.Lstat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("examining %s: %w", path, err)
		}
		if !fi.Mode().IsRegular() {
			return nil, fmt.Errorf("%w: %s is a %s", ErrSidecarNotRegular, path, fileKind(fi.Mode()))
		}
		items = append(items, &dItem{name: c.name, orig: path, info: fi})
	}
	return items, nil
}

type location int

const (
	atUnknown location = iota
	atOriginal
	atRetained
)

func locate(it *dItem) location {
	id := it.identity()
	if fi, err := os.Lstat(it.orig); err == nil && os.SameFile(fi, id) {
		return atOriginal
	}
	if it.retained != "" {
		if fi, err := os.Lstat(it.retained); err == nil && os.SameFile(fi, id) {
			return atRetained
		}
	}
	return atUnknown
}

func (d *displacer) replace(dbPath string, repl *Replacement, b *Baseline) (*DisplaceResult, error) {
	res := &DisplaceResult{}
	if repl == nil || b == nil {
		return res, errors.New("sqlite: ReplaceActiveDatabase needs a prepared replacement and a baseline")
	}
	if repl.installed || repl.finished {
		return res, errors.New("sqlite: the replacement was already installed or abandoned")
	}
	if repl.dbPath != dbPath || b.dbPath != dbPath {
		return res, fmt.Errorf("sqlite: the replacement or baseline was prepared for a different database than %s", dbPath)
	}
	parent := filepath.Dir(dbPath)

	var (
		aside      string
		items      []*dItem
		prescanned bool
		moved      []*dItem
		unknown    []*dItem // a move reported an error and the file cannot be placed
		vanished   []*dItem // gone before its move: neither retained nor restored
	)
	// fail rolls back whatever was moved and builds the typed error from the VERIFIED
	// final location of every involved file, never from what the rollback returned.
	//
	// With noRollback nothing is moved back (the install outcome is unknown, so the
	// replacement may be in place) and the error says so.
	finish := func(stage string, cause error, noRollback bool) (*DisplaceResult, error) {
		var notes []string
		var rbErr error
		if !noRollback {
			notes, rbErr = d.rollback(dbPath, aside, moved)
		}
		de := &DisplacementError{Stage: stage, Err: cause, Notes: notes, Install: InstallNotInstalled}
		if noRollback {
			de.Install = InstallUnknown
		}
		if rbErr != nil {
			de.Err = errors.Join(cause, rbErr)
		}
		seen := map[*dItem]bool{}
		for _, it := range moved {
			seen[it] = true
			switch locate(it) {
			case atRetained:
				de.Retained = append(de.Retained, DisplacedFile{Name: it.name, Original: it.orig, Retained: it.retained})
			case atUnknown:
				de.Unknown = append(de.Unknown, it.orig)
			case atOriginal:
				// Back in place. After a rollback that did not complete this means a
				// companion was restored and could not be displaced again.
				if rbErr != nil {
					de.RestoredInPlace = append(de.RestoredInPlace, it.orig)
				}
			}
		}
		for _, it := range unknown {
			seen[it] = true
			de.Unknown = append(de.Unknown, it.orig)
		}
		for _, it := range moved {
			if locate(it) == atUnknown {
				de.UnknownDetail = append(de.UnknownDetail, describeCandidates(it))
			}
		}
		for _, it := range unknown {
			de.UnknownDetail = append(de.UnknownDetail, describeCandidates(it))
		}
		for _, it := range vanished {
			seen[it] = true
			de.Vanished = append(de.Vanished, it.orig)
		}
		if len(moved)+len(unknown)+len(vanished) > 0 {
			for _, it := range items {
				if !seen[it] && locate(it) == atOriginal {
					de.NotMoved = append(de.NotMoved, it.orig)
				}
			}
		}
		// Where the main file is, established from the filesystem.
		var mainItem *dItem
		for _, it := range items {
			if it.name == "main" {
				mainItem = it
			}
		}
		inList := func(list []*dItem, it *dItem) bool {
			for _, x := range list {
				if x == it {
					return true
				}
			}
			return false
		}
		switch {
		case !prescanned:
			// Nothing was moved, but "untouched" must be verified, not assumed: a file
			// recorded in the baseline may have disappeared since it was taken.
			var gone []string
			de.Main, gone = b.unmovedState()
			de.Vanished = append(de.Vanished, gone...)
		case mainItem == nil:
			de.Main = MainNone
		case inList(vanished, mainItem):
			de.Main = MainVanished
		case inList(unknown, mainItem):
			de.Main = MainUnknown
		default:
			switch locate(mainItem) {
			case atRetained:
				de.Main = MainRetained
			case atOriginal:
				de.Main = MainUntouched
				if inList(moved, mainItem) {
					de.Main = MainAtOriginal
				}
			default:
				de.Main = MainUnknown
			}
		}
		// Rolled back means every involved original is confirmed at its original path
		// and nothing vanished.
		de.RolledBack = !noRollback && rbErr == nil && de.OriginalInPlace() && len(de.Vanished) == 0
		// Report a directory only if it still exists (a successful rollback removes it).
		if (len(de.Retained) > 0 || len(de.Unknown) > 0) && aside != "" {
			if _, err := os.Lstat(aside); err == nil {
				de.Dir = aside
				res.Dir = aside
			}
		}
		res.Files = de.Retained
		return res, de
	}
	fail := func(stage string, cause error) (*DisplaceResult, error) {
		return finish(stage, cause, false)
	}

	if err := b.verifyAtPaths(); err != nil {
		return fail("baseline", err)
	}
	// The prepared replacement is verified (identity, permissions, content hash)
	// BEFORE anything is moved: a replacement that is already bad must not cause the
	// original database to be displaced. This narrows, and does not close, the window
	// before the installation; repl.verify and the post-install verification remain.
	if err := d.verifyPrepared(repl); err != nil {
		return fail("prepared-replacement", err)
	}
	d.step("prepared-verified")
	var err error
	if items, err = prescan(dbPath); err != nil {
		return fail("prescan", err)
	}
	prescanned = true
	d.step("verified-baseline")

	if len(items) > 0 {
		var rnd [4]byte
		if _, err := d.random(rnd[:]); err != nil {
			return fail("aside-directory", err)
		}
		name := fmt.Sprintf("%s.replaced-%s-%s", dbPath, d.now().UTC().Format("20060102T150405Z"), hex.EncodeToString(rnd[:]))
		if err := d.mkdir(name, 0o700); err != nil {
			return fail("aside-directory", fmt.Errorf("creating %s: %w", name, err)) // aside stays empty: nothing of ours to remove
		}
		aside = name
		if err := os.Chmod(aside, 0o700); err != nil {
			return fail("aside-directory", err)
		}
		d.step("aside-created")
		if err := d.syncDir(parent); err != nil {
			return fail("aside-directory-sync", fmt.Errorf("syncing %s: %w", parent, err))
		}
		d.step("aside-parent-synced")

		for _, it := range items {
			it.retained = filepath.Join(aside, it.name)
			if fi, lerr := os.Lstat(it.orig); lerr == nil {
				it.pre = fi // the file this rename will be issued for (may differ from the examined one)
			}
			if err := d.rename(it.orig, it.retained); err != nil {
				// Gone before it could be moved: neither retained nor restored.
				if errors.Is(err, os.ErrNotExist) && pathAbsent(it.orig) && pathAbsent(it.retained) {
					vanished = append(vanished, it)
					res.Vanished = append(res.Vanished, it.name)
					if it.name != "main" {
						continue // a companion that is gone: carry on with the others
					}
					return fail("move-"+it.name, err)
				}
				// The rename reported an error: look at both paths. It may have taken effect,
				// possibly on a different file than the one examined.
				switch settle(it) {
				case atRetained:
					moved = append(moved, it)
				case atUnknown: // never silently dropped from the report
					unknown = append(unknown, it)
				}
				return fail("move-"+it.name, err)
			}
			if fi, lerr := os.Lstat(it.retained); lerr == nil {
				it.movedInfo = fi // follow the file that was actually moved
			}
			moved = append(moved, it)
			d.step(it.name + "-moved")
		}
		for _, it := range moved {
			if err := b.verifyDisplaced(it); err != nil {
				return fail("verify-displaced", err)
			}
		}
		d.step("verified-displaced")
		if err := d.syncDir(aside); err != nil {
			return fail("sync-aside-before-install", fmt.Errorf("syncing %s: %w", aside, err))
		}
		if err := d.syncDir(parent); err != nil {
			return fail("sync-parent-before-install", fmt.Errorf("syncing %s: %w", parent, err))
		}
		d.step("synced-before-install")
	}

	if d.beforeInstall != nil {
		d.beforeInstall()
	}
	d.step("before-install")
	// The replacement must not be installed beside a -wal or -journal that appeared at
	// the original path since the displacement (for example created by a process that
	// ignores the lock): SQLite would replay or roll back that file onto the new
	// database. Refuse, and roll back; nothing is deleted. The -shm is advisory and is
	// rebuilt, so a stray one is not refused.
	for _, sfx := range []string{"-wal", "-journal"} {
		if !pathAbsent(dbPath + sfx) {
			return fail("companion-appeared", fmt.Errorf("%w: %s%s", ErrDisplaceCompanionAppeared, dbPath, sfx))
		}
	}
	// The prepared replacement must still be the file that was prepared. A changed,
	// substituted or missing one is refused (and nothing is deleted): everything
	// displaced is rolled back.
	if err := repl.verify(); err != nil {
		return fail("replacement-changed", err)
	}
	if err := d.rename(repl.path, dbPath); err != nil {
		// A rename that reported an error may still have installed the file. Look at
		// both paths: installed, definitely not installed, or not knowable.
		switch d.installOutcome(dbPath, repl) {
		case installDone:
			// carry on: the file is in place and is verified below
		case installNot:
			return fail("install", err)
		default:
			// Rolling back could displace a database that is in fact installed, and the
			// temporary file must not be removed (it may be the installed database).
			repl.finished = true
			return finish("install", fmt.Errorf("%w: %w", ErrDisplaceInstallOutcomeUnknown, err), true)
		}
	}
	repl.installed = true
	res.Installed = true
	if len(moved) > 0 {
		res.Dir = aside
		for _, it := range moved {
			res.Files = append(res.Files, DisplacedFile{Name: it.name, Original: it.orig, Retained: it.retained})
		}
	}
	d.step("installed")
	// Verify what is now at the active path: the very file that was prepared, with
	// the content that was written, stable while it was read.
	check, ierr := d.verifyInstalled(dbPath, repl)
	if ierr == nil {
		d.step("install-verified")
	}
	syncErr := d.syncDir(parent)
	if syncErr == nil {
		d.step("final-synced")
	}
	if ierr == nil {
		// Nothing may have been swapped in while the directory was synced.
		ierr = d.recheckInstalled(dbPath, check)
	}
	if ierr != nil {
		ierr.SyncErr = syncErr
		return res, ierr
	}
	res.Verified = true
	d.step("final-rechecked")
	if syncErr != nil {
		return res, &InstallDurabilityError{fmt.Errorf("the replacement is installed, but syncing %s afterwards failed, so the rename is not yet crash-durable: %w", parent, syncErr)}
	}
	return res, nil
}

type installResult int

const (
	installUnknown installResult = iota
	installDone
	installNot
)

// installOutcome establishes, after a failed install rename, whether the prepared
// file is now at the active path (installDone), is definitely still at its prepared
// path with nothing of ours at the active path (installNot), or neither can be
// established (installUnknown): the active path cannot be examined, or the prepared
// file is gone from its path without being at the active path.
func (d *displacer) installOutcome(dbPath string, repl *Replacement) installResult {
	at, aerr := d.statInstalled(dbPath)
	if aerr == nil && os.SameFile(at, repl.info) {
		return installDone
	}
	if aerr != nil && !errors.Is(aerr, os.ErrNotExist) {
		return installUnknown
	}
	if src, serr := os.Lstat(repl.path); serr == nil && os.SameFile(src, repl.info) {
		return installNot
	}
	return installUnknown
}

// installedCheck is what verifyInstalled saw, for the final recheck.
type installedCheck struct {
	fi  os.FileInfo
	sig statSig
}

// verifyInstalled checks the file at the active path after the install rename. The
// identity (regular file, same device and inode as the prepared file) and the
// stability are established on one open descriptor; the content is hashed through
// it and compared with the hash and length recorded while the file was written. A
// rename changes the file's ctime, so the pre-install signature is not compared
// here; the signature taken now is compared again after the read and after the
// directory sync instead.
func (d *displacer) verifyInstalled(dbPath string, repl *Replacement) (*installedCheck, *InstalledFileChangedError) {
	bad := func(k InstalledFileKind, format string, a ...any) (*installedCheck, *InstalledFileChangedError) {
		return nil, &InstalledFileChangedError{Path: dbPath, Kind: k, Detail: fmt.Sprintf(format, a...)}
	}
	f, err := d.openInstalled(dbPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return bad(InstalledGone, "nothing is at the active path: %v", err)
		}
		if fi, lerr := d.statInstalled(dbPath); lerr == nil {
			if !fi.Mode().IsRegular() {
				return bad(InstalledNotRegular, "the active path is not a regular file (%v)", err)
			}
			if !os.SameFile(fi, repl.info) {
				return bad(InstalledReplaced, "the active path is not the file that was installed (%v)", err)
			}
		}
		return bad(InstalledUnreadable, "the installed file could not be opened: %v", err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return bad(InstalledUnreadable, "the installed file could not be examined: %v", err)
	}
	if !fi.Mode().IsRegular() {
		return bad(InstalledNotRegular, "the active path is not a regular file")
	}
	if !os.SameFile(fi, repl.info) {
		return bad(InstalledReplaced, "the active path is not the file that was installed")
	}
	if fi.Size() != repl.size {
		return bad(InstalledContentChanged, "its size is %d, expected %d", fi.Size(), repl.size)
	}
	sig := statSigOf(fi)
	var r io.Reader = f
	if d.wrapReader != nil {
		r = d.wrapReader(f)
	}
	hash, n, err := hashReader(r)
	if err != nil {
		return bad(InstalledUnreadable, "the installed file could not be read: %v", err)
	}
	if n != repl.size || hash != repl.hash {
		return bad(InstalledContentChanged, "its content (%d bytes, sha256 %s) is not what was written (%d bytes, sha256 %s)", n, hash, repl.size, repl.hash)
	}
	fi2, err := f.Stat()
	if err != nil {
		return bad(InstalledUnreadable, "the installed file could not be examined after reading: %v", err)
	}
	if statSigOf(fi2) != sig {
		return bad(InstalledUnstable, "the installed file changed while it was being verified")
	}
	return &installedCheck{fi: fi2, sig: sig}, nil
}

// recheckInstalled confirms, by path, that the active path still holds the file
// verifyInstalled examined and that it has not changed since.
func (d *displacer) recheckInstalled(dbPath string, c *installedCheck) *InstalledFileChangedError {
	bad := func(k InstalledFileKind, format string, a ...any) *InstalledFileChangedError {
		return &InstalledFileChangedError{Path: dbPath, Kind: k, Detail: fmt.Sprintf(format, a...)}
	}
	fi, err := d.statInstalled(dbPath)
	switch {
	case err != nil && errors.Is(err, os.ErrNotExist):
		return bad(InstalledGone, "nothing is at the active path any more: %v", err)
	case err != nil:
		return bad(InstalledUnreadable, "the active path could not be examined again: %v", err)
	case !fi.Mode().IsRegular():
		return bad(InstalledNotRegular, "the active path is no longer a regular file")
	case !os.SameFile(fi, c.fi):
		return bad(InstalledReplaced, "the active path no longer holds the verified file")
	case statSigOf(fi) != c.sig:
		return bad(InstalledUnstable, "the installed file changed after it was verified")
	}
	return nil
}

// rollback undoes a displacement, all or nothing in intent, with no-replace
// renames only. moved is in displacement order (main first), so walking it
// backwards restores the companions first and the main file last.
//
//   - If the original main path is occupied, nothing at all is moved back: an old
//     -wal or -journal beside a different database could be replayed onto it.
//   - If a step fails partway, the companions already moved back are displaced
//     again, so the original state is never left half restored.
//   - On complete success the aside directory, which this operation created and
//     which is now empty, is removed (os.Remove: it never removes a directory that
//     still holds anything).
//
// The returned notes are non-fatal facts about the end state that must reach the
// operator and are never swallowed: a directory sync after the rollback that
// failed (so the rollback is not confirmed crash-durable), and an empty aside
// directory that could not be removed. They do not change where any file is.
func (d *displacer) rollback(dbPath, aside string, moved []*dItem) (notes []string, err error) {
	if len(moved) == 0 {
		return d.removeAside(aside), nil
	}
	if _, err := os.Lstat(dbPath); err == nil {
		return nil, fmt.Errorf("%w: %s is occupied by another file, so nothing was moved back", ErrRollbackIncomplete, dbPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: examining %s: %v", ErrRollbackIncomplete, dbPath, err)
	}
	var restored []*dItem
	compensate := func(cause error) error {
		errs := []error{fmt.Errorf("%w: %v", ErrRollbackIncomplete, cause)}
		for i := len(restored) - 1; i >= 0; i-- {
			it := restored[i]
			if err := d.rename(it.orig, it.retained); err != nil && locate(it) != atRetained {
				errs = append(errs, fmt.Errorf("could not displace %s again: %w", it.orig, err))
			}
		}
		return errors.Join(errs...)
	}
	for i := len(moved) - 1; i >= 0; i-- {
		it := moved[i]
		if err := d.rename(it.retained, it.orig); err != nil {
			if locate(it) != atOriginal {
				return nil, compensate(err)
			}
		}
		restored = append(restored, it)
	}
	parent := filepath.Dir(dbPath)
	syncNote := func() {
		if err := d.syncDir(parent); err != nil {
			notes = append(notes, fmt.Sprintf("syncing %s after the rollback failed, so the rollback is not confirmed to be crash-durable: %v", parent, err))
		}
	}
	syncNote()
	notes = append(notes, d.removeAside(aside)...)
	syncNote()
	return notes, nil
}

// removeAside removes the empty aside directory this operation created. os.Remove
// never removes a directory that still holds anything. A failure is returned as a
// note, never ignored: the directory is then still there.
func (d *displacer) removeAside(aside string) []string {
	if aside == "" {
		return nil
	}
	if err := d.removeDir(aside); err != nil && !pathAbsent(aside) {
		return []string{fmt.Sprintf("the empty aside directory %s could not be removed and is still there: %v", aside, err)}
	}
	return nil
}
