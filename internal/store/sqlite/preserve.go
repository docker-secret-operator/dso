package sqlite

// Raw preservation of an active database's files (Restore design, Phase 2).
//
// PreserveActiveFiles copies the main database file and its -wal, -shm and
// -journal companions, byte for byte, into a new directory beside the database,
// BEFORE anything opens the database through SQLite. It is the recoverable record
// of an unusual active database that a later step may replace.
//
// What it is, precisely:
//
//   - A VERIFIED-STABLE COPY, not an atomic snapshot. Nothing can freeze the files
//     of a database that a process outside this program (the sqlite3 CLI, a
//     script) is writing to; the central advisory lock does not reach such a
//     process, and neither do the file locks SQLite uses for its own readers. The
//     copy is accepted only if the files were observably unchanged for the whole
//     copy: every source is hashed while it is copied and hashed again afterwards,
//     its stat signature (including the change time where the platform has one) is
//     compared before and after, its path is checked to still name the same file,
//     and the set of companions is compared before and after. Any difference
//     discards the attempt. After a bounded number of attempts the function fails
//     closed with ErrPreserveUnstable rather than keep a copy it cannot vouch for.
//   - It cannot detect a change that is made and fully reverted outside the two
//     reads, nor a same-tick change that leaves every compared value identical;
//     in those cases the copy it kept is nevertheless one the files really held.
//   - The -shm file is ADVISORY. SQLite rebuilds it from the -wal, and any reader
//     (including a WAL-aware inspection) may rewrite it, so it is copied for the
//     record but excluded from every stability and recheck comparison.
//
// What it never does:
//
//   - Open the database through SQLite: the sources are read as plain byte
//     streams, so a zero-byte file, a damaged file or a non-SQLite file is
//     preserved exactly like a healthy one. Nothing is checkpointed, migrated or
//     created in the database's own files.
//   - Modify, truncate, rename, remove or overwrite the original database or any
//     of its companions. Sources are opened read-only.
//   - Delete a published preservation directory. It removes only the ".partial"
//     directory that this very call created, and only on failure.
//
// Publication is atomic from the caller's point of view: the directory is built
// as "<db>.pre-restore-raw-<UTC>-<hex>.partial", every file and the manifest are
// verified and fsynced, and only then is it renamed to its final name and the
// parent directory synced. A crash (SIGKILL, power loss) at any point before the
// rename leaves the originals untouched and at most this call's own ".partial"
// directory, never something that looks complete. The rename is the platform's
// atomic NO-REPLACE rename (renameat2 RENAME_NOREPLACE on Linux, renamex_np
// RENAME_EXCL on macOS), so an existing entry at the final name, even an empty
// directory, is refused by the kernel in the same operation; on any platform,
// kernel or filesystem without it publication fails closed, and nothing falls
// back to a check followed by an ordinary rename. If that rename is interrupted
// (EINTR) its outcome is established from file identity before anything is
// retried or cleaned up; if it cannot be established nothing is deleted or
// overwritten and the partial directory is kept. Nothing removes a stale
// ".partial" directory automatically (a later call never reuses one and never
// touches it); it is safe for an operator to delete once nothing is running.
//
// Free space: Go's standard library offers no portable free-space query, so the
// precheck is an injectable probe that is unset by default. Without it, running
// out of space shows up as ENOSPC from a write and is reported as
// ErrPreserveNoSpace after the partial directory is removed.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/docker-secret-operator/dso/internal/securefs"
)

var (
	// ErrPreserveUnstable: the database files kept changing during every
	// attempt, so no copy could be vouched for. Nothing was kept.
	ErrPreserveUnstable = errors.New("sqlite: the database files kept changing while they were being preserved")
	// ErrPreserveIdentityChanged: the file at the database path is not the one
	// the caller identified (it was replaced, removed or swapped for a link).
	ErrPreserveIdentityChanged = errors.New("sqlite: the database file is not the one that was identified (it was replaced)")
	// ErrPreserveNoSpace: there was not enough space to preserve the files.
	// Nothing was kept.
	ErrPreserveNoSpace = errors.New("sqlite: not enough space to preserve the database files")
	// ErrPreserveVerification: a written copy did not read back identical.
	// Nothing was kept.
	ErrPreserveVerification = errors.New("sqlite: a preserved copy did not verify")
	// ErrPreserveSourceChanged: (*Preservation).Unchanged found that the original
	// files no longer match what was preserved.
	ErrPreserveSourceChanged = errors.New("sqlite: the database files changed after they were preserved")
	// ErrPreserveDestinationExists: something already exists at the final name
	// (even an empty directory). The existing entry was not touched.
	ErrPreserveDestinationExists = errors.New("sqlite: the destination already exists (an existing preservation is never replaced)")
	// ErrPreserveNoReplaceUnsupported: this platform, kernel or filesystem cannot
	// rename without replacing, so the preservation cannot be published safely.
	// There is no fallback to a check followed by an ordinary rename.
	ErrPreserveNoReplaceUnsupported = errors.New("sqlite: atomic no-replace rename is not supported here, so the preservation cannot be published")
	// ErrPreserveOutcomeUnknown: the publishing rename was interrupted and what
	// happened could not be established. Nothing was deleted or overwritten, and
	// the partial directory was deliberately left in place.
	ErrPreserveOutcomeUnknown = errors.New("sqlite: the outcome of publishing the preservation could not be established")
)

// PreserveDurabilityError is returned together with a valid *Preservation when
// the copy is complete, verified and published but fsyncing the parent directory
// (which makes the rename crash-durable) failed. The caller decides whether that
// is acceptable; a conservative caller treats it as a failure.
type PreserveDurabilityError struct{ err error }

func (e *PreserveDurabilityError) Error() string { return e.err.Error() }
func (e *PreserveDurabilityError) Unwrap() error { return e.err }

// FileIdentity identifies one specific file (device and inode, via os.SameFile),
// not a path. It is taken from the path before the file is classified, and a
// preservation proves that the file it copied is that same file. The zero value
// is invalid.
type FileIdentity struct{ info os.FileInfo }

// StatIdentity returns the identity of the regular file at path. A symbolic
// link, a directory or any other non-regular file is refused: it is never
// followed.
func StatIdentity(path string) (FileIdentity, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return FileIdentity{}, err
	}
	fi, err := os.Lstat(abs)
	if err != nil {
		return FileIdentity{}, err
	}
	if !fi.Mode().IsRegular() {
		return FileIdentity{}, fmt.Errorf("sqlite: %s is not a regular file", abs)
	}
	return FileIdentity{info: fi}, nil
}

func (id FileIdentity) valid() bool { return id.info != nil }

func (id FileIdentity) matches(fi os.FileInfo) bool {
	return id.info != nil && fi != nil && os.SameFile(id.info, fi)
}

// PreservedFile records one preserved file.
type PreservedFile struct {
	Name     string // "main", "wal", "journal" or "shm"
	Source   string // the original path
	Size     int64
	SHA256   string
	Advisory bool // true for "shm": copied for the record, excluded from comparisons
}

// Preservation is a published, verified preservation directory. It holds no
// open files and is safe to keep.
type Preservation struct {
	dir    string
	dbPath string
	expect FileIdentity
	files  []PreservedFile
}

// Dir is the published directory. It contains one file per preserved source
// ("main", "wal", "journal", "shm") and manifest.json.
func (p *Preservation) Dir() string { return p.dir }

// Files returns a copy of the preservation record.
func (p *Preservation) Files() []PreservedFile { return append([]PreservedFile(nil), p.files...) }

func (p *Preservation) record(name string) (PreservedFile, bool) {
	for _, f := range p.files {
		if f.Name == name {
			return f, true
		}
	}
	return PreservedFile{}, false
}

// Unchanged reports whether the original files still match what was preserved:
// the main file is still the identified file with the preserved content, and the
// -wal and -journal are present with the preserved content exactly when they were
// preserved. The -shm is ignored. It returns an error matching
// ErrPreserveSourceChanged otherwise. It reads the files; it changes nothing.
func (p *Preservation) Unchanged() error {
	changed := func(format string, a ...any) error {
		return fmt.Errorf("%w: %s", ErrPreserveSourceChanged, fmt.Sprintf(format, a...))
	}
	main, ok := p.record("main")
	if !ok {
		return errors.New("sqlite: the preservation has no main file record")
	}
	f, err := securefs.OpenPrivateFile(p.dbPath)
	if err != nil {
		return changed("the database file can no longer be opened safely: %v", err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return changed("%v", err)
	}
	if !p.expect.matches(fi) {
		return changed("%s is no longer the identified file", p.dbPath)
	}
	sum, size, err := hashReader(f)
	if err != nil {
		return changed("reading %s: %v", p.dbPath, err)
	}
	if sum != main.SHA256 || size != main.Size {
		return changed("%s content differs", p.dbPath)
	}
	for _, name := range []string{"wal", "journal"} {
		path := p.dbPath + sidecarSuffix(name)
		rec, preserved := p.record(name)
		cf, err := securefs.OpenRegularNoFollow(path)
		switch {
		case err == nil:
			sum, size, herr := hashReader(cf)
			_ = cf.Close()
			switch {
			case !preserved:
				return changed("%s appeared", path)
			case herr != nil:
				return changed("reading %s: %v", path, herr)
			case sum != rec.SHA256 || size != rec.Size:
				return changed("%s content differs", path)
			}
		case os.IsNotExist(err):
			if preserved {
				return changed("%s disappeared", path)
			}
		default:
			return changed("%s: %v", path, err)
		}
	}
	return nil
}

func sidecarSuffix(name string) string {
	switch name {
	case "wal":
		return "-wal"
	case "shm":
		return "-shm"
	case "journal":
		return "-journal"
	}
	return ""
}

// PreserveActiveFiles preserves the files of the database at dbPath. expect must
// be the identity of the file at dbPath, taken with StatIdentity before it was
// classified; if the file at dbPath is no longer that file the call fails with
// ErrPreserveIdentityChanged. The main file must pass the same private-file rules
// as every other database access (regular file, owned by the caller, no group or
// other access, a safe directory chain); a companion must be a regular file.
// See the file comment for exactly what is and is not guaranteed.
func PreserveActiveFiles(dbPath string, expect FileIdentity) (*Preservation, error) {
	return newPreserver().preserve(dbPath, expect)
}

// preserver holds every tunable and test seam, so none of it is shared mutable
// state. Production always uses newPreserver().
type preserver struct {
	attempts   int           // total attempts before ErrPreserveUnstable
	retryDelay time.Duration // pause between attempts
	now        func() time.Time
	random     func([]byte) (int, error)
	// freeSpace reports the bytes available in dir. nil, or ok == false, means
	// "unknown" and no precheck is made.
	freeSpace  func(dir string) (avail uint64, ok bool, err error)
	mkdir      func(path string, perm os.FileMode) error
	createFile func(path string, mode os.FileMode) (*os.File, error)
	syncDir    func(dir string) error
	// syncFile flushes one file this call wrote (a copy, or the manifest) to
	// stable storage. A test seam: production always uses (*os.File).Sync.
	syncFile func(f *os.File) error
	// publish moves the finished partial directory to its final name, refusing
	// to replace anything already there. A test seam: production always uses
	// publishDir.
	publish func(partial, final string) error
	// wrapWriter wraps the destination writer of one file (fault injection).
	wrapWriter func(name string, w io.Writer) io.Writer
	// Hooks run at fixed points of an attempt so tests can make a "concurrent"
	// change at an exact moment.
	beforeOpen func(attempt int)
	afterCopy  func(attempt int)
}

func newPreserver() *preserver {
	return &preserver{
		attempts:   3,
		retryDelay: 50 * time.Millisecond,
		now:        time.Now,
		random:     rand.Read,
		mkdir:      os.Mkdir,
		createFile: securefs.CreateExclusive,
		syncDir:    fsyncDir,
		syncFile:   (*os.File).Sync,
		publish:    publishDir,
	}
}

func fsyncDir(dir string) error {
	d, err := os.Open(dir) // #nosec G304 -- a directory this package created or whose chain was checked
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// source is one open original file.
type source struct {
	name     string
	path     string
	f        *os.File
	sig      statSig
	advisory bool
	sum      string // SHA-256 of what was copied
	size     int64
}

// attemptError is a failure of one attempt; retry says whether another attempt
// can help (a concurrent change) or the failure is final.
type attemptError struct {
	err   error
	retry bool
}

func (e *attemptError) Error() string { return e.err.Error() }
func (e *attemptError) Unwrap() error { return e.err }

func retryable(format string, a ...any) *attemptError {
	return &attemptError{err: fmt.Errorf(format, a...), retry: true}
}

func final(err error) *attemptError { return &attemptError{err: err} }

func (pv *preserver) preserve(dbPath string, expect FileIdentity) (*Preservation, error) {
	if !expect.valid() {
		return nil, errors.New("sqlite: PreserveActiveFiles needs the identity of the database file (StatIdentity)")
	}
	abs, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, fmt.Errorf("resolving database path %s: %w", dbPath, err)
	}
	dir := filepath.Dir(abs)
	if err := securefs.CheckDirChain(dir); err != nil {
		return nil, fmt.Errorf("database directory is not safe: %w", err)
	}
	var last error
	for n := 1; n <= pv.attempts; n++ {
		if n > 1 {
			time.Sleep(pv.retryDelay)
		}
		p, err := pv.attempt(abs, dir, expect, n)
		if err == nil {
			return p, nil
		}
		var ae *attemptError
		if errors.As(err, &ae) {
			if !ae.retry {
				return nil, ae.err
			}
			last = ae.err
			continue
		}
		return p, err // a durability error: the preservation is valid and returned with it
	}
	return nil, fmt.Errorf("%w after %d attempts (last change: %v)", ErrPreserveUnstable, pv.attempts, last)
}

// companions in the order they are examined. "shm" is advisory.
var companionNames = []string{"wal", "journal", "shm"}

func (pv *preserver) attempt(abs, dir string, expect FileIdentity, n int) (*Preservation, error) {
	if pv.beforeOpen != nil {
		pv.beforeOpen(n)
	}
	srcs, err := pv.openSources(abs, expect)
	if err != nil {
		return nil, err
	}
	defer func() {
		for _, s := range srcs {
			_ = s.f.Close()
		}
	}()

	var total int64
	for _, s := range srcs {
		fi, err := s.f.Stat()
		if err != nil {
			return nil, final(err)
		}
		total += fi.Size()
	}
	const margin = 1 << 20 // manifest and filesystem overhead
	if pv.freeSpace != nil {
		avail, ok, err := pv.freeSpace(dir)
		if err != nil {
			return nil, final(fmt.Errorf("checking free space in %s: %w", dir, err))
		}
		if ok && avail < uint64(total)+margin { // #nosec G115 -- total is a sum of file sizes, never negative
			return nil, final(fmt.Errorf("%w: need about %d bytes in %s, %d available", ErrPreserveNoSpace, total+margin, dir, avail))
		}
	}

	var rnd [4]byte
	if _, err := pv.random(rnd[:]); err != nil {
		return nil, final(err)
	}
	base := fmt.Sprintf("%s.pre-restore-raw-%s-%s", abs, pv.now().UTC().Format("20060102T150405Z"), hex.EncodeToString(rnd[:]))
	partial := base + ".partial"
	if err := pv.mkdir(partial, 0o700); err != nil {
		return nil, final(spaceOr(err, fmt.Errorf("creating %s: %w", partial, err)))
	}
	published := false
	keepPartial := false // set when the outcome of the publishing rename is unknown
	defer func() {
		if !published && !keepPartial {
			removePartial(partial, dir)
		}
	}()
	if err := os.Chmod(partial, 0o700); err != nil {
		return nil, final(err)
	}

	// Pass 1: copy, hashing exactly what is read.
	for _, s := range srcs {
		if err := pv.copyOne(partial, s); err != nil {
			return nil, final(err)
		}
	}
	if pv.afterCopy != nil {
		pv.afterCopy(n)
	}

	// Pass 2: the sources must still be what was copied.
	if err := verifyStable(abs, srcs, expect); err != nil {
		return nil, err
	}

	// The copies must read back identical to the sources.
	for _, s := range srcs {
		cf, err := os.Open(filepath.Join(partial, s.name)) // #nosec G304 -- a file this call created inside its own partial directory
		if err != nil {
			return nil, final(err)
		}
		sum, size, herr := hashReader(cf)
		_ = cf.Close()
		if herr != nil {
			return nil, final(herr)
		}
		if sum != s.sum || size != s.size {
			return nil, final(fmt.Errorf("%w: %s", ErrPreserveVerification, s.name))
		}
	}

	files := make([]PreservedFile, 0, len(srcs))
	for _, s := range srcs {
		files = append(files, PreservedFile{Name: s.name, Source: s.path, Size: s.size, SHA256: s.sum, Advisory: s.advisory})
	}
	if err := pv.writeManifest(partial, abs, expect, files); err != nil {
		return nil, final(err)
	}
	if err := pv.syncDir(partial); err != nil {
		return nil, final(fmt.Errorf("syncing %s: %w", partial, err))
	}

	// The one publication step: an atomic rename that refuses to replace anything.
	// There is no existence check beforehand, because a check followed by a rename
	// would leave a window in which a competing directory could be replaced.
	if err := pv.publish(partial, base); err != nil {
		if errors.Is(err, ErrPreserveOutcomeUnknown) {
			keepPartial = true
		}
		return nil, final(err)
	}
	published = true
	p := &Preservation{dir: base, dbPath: abs, expect: expect, files: files}
	if err := pv.syncDir(dir); err != nil {
		return p, &PreserveDurabilityError{fmt.Errorf("the preservation at %s is complete and verified, but syncing %s failed: %w", base, dir, err)}
	}
	return p, nil
}

// openSources opens the main file (identity-checked, private-file rules) and the
// companions that exist. A companion that is not a regular file, or is a link,
// is refused; one that vanishes between the lookup and the open is a change.
func (pv *preserver) openSources(abs string, expect FileIdentity) ([]*source, error) {
	mf, err := securefs.OpenPrivateFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, final(fmt.Errorf("%w: %s no longer exists", ErrPreserveIdentityChanged, abs))
		}
		return nil, final(err)
	}
	fi, err := mf.Stat()
	if err != nil {
		_ = mf.Close()
		return nil, final(err)
	}
	if !expect.matches(fi) {
		_ = mf.Close()
		return nil, final(fmt.Errorf("%w: %s", ErrPreserveIdentityChanged, abs))
	}
	srcs := []*source{{name: "main", path: abs, f: mf, sig: statSigOf(fi)}}
	closeAll := func() {
		for _, s := range srcs {
			_ = s.f.Close()
		}
	}
	for _, name := range companionNames {
		path := abs + sidecarSuffix(name)
		if _, err := os.Lstat(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			closeAll()
			return nil, final(err)
		}
		cf, err := securefs.OpenRegularNoFollow(path)
		if err != nil {
			if os.IsNotExist(err) {
				closeAll()
				return nil, retryable("%s disappeared while it was being opened", path)
			}
			closeAll()
			return nil, final(err)
		}
		cfi, err := cf.Stat()
		if err != nil {
			_ = cf.Close()
			closeAll()
			return nil, final(err)
		}
		srcs = append(srcs, &source{name: name, path: path, f: cf, sig: statSigOf(cfi), advisory: name == "shm"})
	}
	return srcs, nil
}

func (pv *preserver) copyOne(partial string, s *source) error {
	dst, err := pv.createFile(filepath.Join(partial, s.name), 0o600)
	if err != nil {
		return spaceOr(err, fmt.Errorf("creating the copy of %s: %w", s.path, err))
	}
	h := sha256.New()
	var w io.Writer = dst
	if pv.wrapWriter != nil {
		w = pv.wrapWriter(s.name, dst)
	}
	n, err := io.Copy(io.MultiWriter(w, h), s.f)
	if err != nil {
		_ = dst.Close()
		return spaceOr(err, fmt.Errorf("copying %s: %w", s.path, err))
	}
	if err := pv.syncFile(dst); err != nil {
		_ = dst.Close()
		return spaceOr(err, fmt.Errorf("syncing the copy of %s: %w", s.path, err))
	}
	if err := dst.Close(); err != nil {
		return spaceOr(err, fmt.Errorf("closing the copy of %s: %w", s.path, err))
	}
	s.sum, s.size = hex.EncodeToString(h.Sum(nil)), n
	return nil
}

// verifyStable is pass 2. Advisory files (-shm) are excluded from every
// comparison. A returned *attemptError is retryable unless the main file is no
// longer the identified one.
func verifyStable(abs string, srcs []*source, expect FileIdentity) *attemptError {
	have := map[string]bool{}
	for _, s := range srcs {
		have[s.name] = true
		if s.advisory {
			continue
		}
		pfi, err := os.Lstat(s.path)
		if err != nil {
			return retryable("%s changed or disappeared: %v", s.path, err)
		}
		ffi, err := s.f.Stat()
		if err != nil {
			return retryable("%s: %v", s.path, err)
		}
		if !os.SameFile(pfi, ffi) {
			if s.name == "main" && !expect.matches(pfi) {
				return final(fmt.Errorf("%w: %s was replaced while it was being copied", ErrPreserveIdentityChanged, abs))
			}
			return retryable("%s was replaced while it was being copied", s.path)
		}
		if statSigOf(ffi) != s.sig {
			return retryable("%s was modified while it was being copied", s.path)
		}
		sum, size, err := hashReader(s.f)
		if err != nil {
			return retryable("re-reading %s: %v", s.path, err)
		}
		if sum != s.sum || size != s.size {
			return retryable("%s content changed while it was being copied", s.path)
		}
	}
	for _, name := range companionNames {
		if name == "shm" || have[name] {
			continue
		}
		if _, err := os.Lstat(abs + sidecarSuffix(name)); err == nil {
			return retryable("%s appeared while the files were being copied", abs+sidecarSuffix(name))
		}
	}
	return nil
}

// hashReader hashes r from the start (rewinding a file) and returns the digest
// and the number of bytes read.
func hashReader(r io.Reader) (string, int64, error) {
	if s, ok := r.(io.Seeker); ok {
		if _, err := s.Seek(0, io.SeekStart); err != nil {
			return "", 0, err
		}
	}
	h := sha256.New()
	n, err := io.Copy(h, r)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

type manifestFile struct {
	Name     string `json:"name"`
	Source   string `json:"source"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	Advisory bool   `json:"advisory,omitempty"`
}

type manifest struct {
	Version int            `json:"version"`
	Created string         `json:"created"`
	Source  string         `json:"source"`
	Note    string         `json:"note"`
	Device  uint64         `json:"device,omitempty"`
	Inode   uint64         `json:"inode,omitempty"`
	Files   []manifestFile `json:"files"`
}

const manifestNote = "verified-stable copy of the files as they were found, taken without opening the database; " +
	"not an atomic snapshot against writers outside DSO. The -shm file, if present, is advisory."

func (pv *preserver) writeManifest(partial, abs string, expect FileIdentity, files []PreservedFile) error {
	m := manifest{Version: 1, Created: pv.now().UTC().Format(time.RFC3339), Source: abs, Note: manifestNote}
	if dev, ino, _, ok := platformStat(expect.info); ok {
		m.Device, m.Inode = dev, ino
	}
	sorted := append([]PreservedFile(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	for _, f := range sorted {
		m.Files = append(m.Files, manifestFile{Name: f.Name, Source: f.Source, Size: f.Size, SHA256: f.SHA256, Advisory: f.Advisory})
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	mf, err := pv.createFile(filepath.Join(partial, "manifest.json"), 0o600)
	if err != nil {
		return spaceOr(err, fmt.Errorf("creating the manifest: %w", err))
	}
	if _, err := mf.Write(append(b, '\n')); err != nil {
		_ = mf.Close()
		return spaceOr(err, fmt.Errorf("writing the manifest: %w", err))
	}
	if err := pv.syncFile(mf); err != nil {
		_ = mf.Close()
		return spaceOr(err, fmt.Errorf("syncing the manifest: %w", err))
	}
	if err := mf.Close(); err != nil {
		return spaceOr(err, fmt.Errorf("closing the manifest: %w", err))
	}
	return nil
}

// spaceOr maps an out-of-space error to ErrPreserveNoSpace, keeping detail.
func spaceOr(cause, fallback error) error {
	if errors.Is(cause, syscall.ENOSPC) || errors.Is(cause, syscall.EDQUOT) {
		return fmt.Errorf("%w: %v", ErrPreserveNoSpace, cause)
	}
	return fallback
}

// publishTries bounds how often an interrupted rename is re-issued, and only
// after it has been verified not to have happened.
const publishTries = 3

// publishDir publishes the finished partial directory under its final name using
// the platform's atomic no-replace rename (renameNoReplace). See publishDirWith.
func publishDir(partial, final string) error { return publishDirWith(renameNoReplace, partial, final) }

// publishDirWith renames partial to final through rename, which must refuse to
// replace an existing destination atomically, and classifies the result:
//
//   - success: published;
//   - EEXIST/ENOTEMPTY: ErrPreserveDestinationExists; the existing entry is not
//     touched (the caller removes only its own partial directory);
//   - ENOSYS/EINVAL/ENOTSUP/EOPNOTSUPP: ErrPreserveNoReplaceUnsupported, which
//     fails closed; nothing falls back to a check followed by an ordinary rename;
//   - any other error: returned with context, partial directory cleaned by the caller;
//   - EINTR: the rename may or may not have happened, so it is NOT simply
//     retried. The identity of the partial directory (taken before the call) is
//     compared with what now exists at both names:
//     source gone and destination is that same directory  -> it was published;
//     source still that directory and destination absent   -> verified not
//     performed, and only then is it re-issued (at most publishTries times);
//     source still there and something else at destination -> the destination exists;
//     anything else                                         -> ErrPreserveOutcomeUnknown:
//     nothing is deleted or overwritten and the caller keeps the partial directory.
func publishDirWith(rename func(oldpath, newpath string) error, partial, final string) error {
	want, err := os.Lstat(partial)
	if err != nil {
		return fmt.Errorf("publishing %s: %w", final, err)
	}
	for try := 0; try < publishTries; try++ {
		err := rename(partial, final)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, syscall.EINTR):
			switch publishOutcome(want, partial, final) {
			case outcomePublished:
				return nil
			case outcomeNotPerformed:
				continue
			case outcomeDestinationExists:
				return fmt.Errorf("%w: %s (the rename was interrupted)", ErrPreserveDestinationExists, final)
			default:
				return fmt.Errorf("%w: renaming %s to %s was interrupted; nothing was deleted or overwritten", ErrPreserveOutcomeUnknown, partial, final)
			}
		default:
			return classifyRenameError(err, partial, final)
		}
	}
	return fmt.Errorf("publishing %s: the rename was interrupted %d times and verified not to have happened (%w)", final, publishTries, syscall.EINTR)
}

func classifyRenameError(err error, partial, final string) error {
	switch {
	case errors.Is(err, ErrPreserveNoReplaceUnsupported), errors.Is(err, ErrPreserveDestinationExists):
		return err
	case errors.Is(err, syscall.EEXIST), errors.Is(err, syscall.ENOTEMPTY):
		return fmt.Errorf("%w: %s", ErrPreserveDestinationExists, final)
	case errors.Is(err, syscall.ENOSYS), errors.Is(err, syscall.EINVAL), errors.Is(err, syscall.ENOTSUP), errors.Is(err, syscall.EOPNOTSUPP):
		return fmt.Errorf("%w (%v)", ErrPreserveNoReplaceUnsupported, err)
	}
	return fmt.Errorf("publishing %s as %s: %w", partial, final, err)
}

type publishResult int

const (
	outcomeUnknown publishResult = iota
	outcomePublished
	outcomeNotPerformed
	outcomeDestinationExists
)

// publishOutcome decides what an interrupted rename did, from file identity.
func publishOutcome(want os.FileInfo, partial, final string) publishResult {
	src, srcErr := os.Lstat(partial)
	dst, dstErr := os.Lstat(final)
	srcHere := srcErr == nil && os.SameFile(want, src)
	switch {
	case os.IsNotExist(srcErr) && dstErr == nil && os.SameFile(want, dst):
		return outcomePublished
	case srcHere && os.IsNotExist(dstErr):
		return outcomeNotPerformed
	case srcHere && dstErr == nil:
		return outcomeDestinationExists
	}
	return outcomeUnknown
}

// removePartial removes the ".partial" directory created by this call, and
// nothing else: the path must end in ".partial" and sit directly in dir.
func removePartial(partial, dir string) {
	if !strings.HasSuffix(partial, ".partial") || filepath.Dir(partial) != dir {
		return
	}
	_ = os.RemoveAll(partial)
}

// ---- reading the state of the companions (read-only) -------------------------------------------

// ErrSidecarNotRegular: a database or one of its -wal, -journal or -shm
// companions is a symbolic link, a directory or some other non-regular file. It
// is never followed or examined further.
var ErrSidecarNotRegular = errors.New("sqlite: a database file or companion is not a regular file")

// SidecarFile describes one companion file as found, without opening it.
type SidecarFile struct {
	Exists bool
	Size   int64
}

// NonEmpty reports a companion that exists and holds bytes. An empty -wal is not
// evidence of a pending WAL (a WAL-aware read can create one), and an empty
// -journal carries no rollback data.
func (s SidecarFile) NonEmpty() bool { return s.Exists && s.Size > 0 }

// ActiveSidecarsInfo is what ActiveSidecars found for one database path.
type ActiveSidecarsInfo struct {
	MainExists bool
	WAL        SidecarFile
	SHM        SidecarFile
	Journal    SidecarFile
}

// OrphanNonEmpty lists, in a fixed order ("-wal", "-journal", "-shm"), the
// non-empty companions that sit beside a MISSING main file. Their data belongs to
// no database that can be opened, so they must never be deleted or overwritten
// silently. It is empty whenever the main file exists.
func (a ActiveSidecarsInfo) OrphanNonEmpty() []string {
	if a.MainExists {
		return nil
	}
	var out []string
	if a.WAL.NonEmpty() {
		out = append(out, "-wal")
	}
	if a.Journal.NonEmpty() {
		out = append(out, "-journal")
	}
	if a.SHM.NonEmpty() {
		out = append(out, "-shm")
	}
	return out
}

// ActiveSidecars reports whether the main database file and each of its -wal,
// -journal and -shm companions exist, and how large they are. It only looks:
// it never opens, deletes, renames, truncates or modifies anything, and never
// follows a link. A main file or companion that exists but is not a regular file
// (a symlink, a directory, a FIFO, a device) is reported as an error wrapping
// ErrSidecarNotRegular, and any other failure to look (permissions, a path
// component that is not a directory, I/O) is returned as an error too; neither is
// ever treated as "missing". A genuinely absent file is not an error.
func ActiveSidecars(dbPath string) (ActiveSidecarsInfo, error) {
	abs, err := filepath.Abs(dbPath)
	if err != nil {
		return ActiveSidecarsInfo{}, fmt.Errorf("resolving database path %s: %w", dbPath, err)
	}
	look := func(path string) (SidecarFile, error) {
		fi, err := os.Lstat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) { // ENOENT only: ENOTDIR, EACCES and the rest are errors
				return SidecarFile{}, nil
			}
			return SidecarFile{}, fmt.Errorf("examining %s: %w", path, err)
		}
		if !fi.Mode().IsRegular() {
			return SidecarFile{}, fmt.Errorf("%w: %s is a %s", ErrSidecarNotRegular, path, fileKind(fi.Mode()))
		}
		return SidecarFile{Exists: true, Size: fi.Size()}, nil
	}
	var info ActiveSidecarsInfo
	main, err := look(abs)
	if err != nil {
		return ActiveSidecarsInfo{}, err
	}
	info.MainExists = main.Exists
	if info.WAL, err = look(abs + "-wal"); err != nil {
		return ActiveSidecarsInfo{}, err
	}
	if info.Journal, err = look(abs + "-journal"); err != nil {
		return ActiveSidecarsInfo{}, err
	}
	if info.SHM, err = look(abs + "-shm"); err != nil {
		return ActiveSidecarsInfo{}, err
	}
	return info, nil
}

func fileKind(m os.FileMode) string {
	switch {
	case m&os.ModeSymlink != 0:
		return "symbolic link"
	case m.IsDir():
		return "directory"
	case m&os.ModeNamedPipe != 0:
		return "named pipe"
	case m&os.ModeSocket != 0:
		return "socket"
	case m&os.ModeDevice != 0:
		return "device"
	}
	return "non-regular file"
}
