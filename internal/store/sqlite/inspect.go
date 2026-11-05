package sqlite

// Non-mutating inspection of a database file that is NOT meant to be used as
// the live store (A2: "is this path an initialized DSO Central database?").
//
// InspectDatabase runs a caller-supplied probe against a read-only view of the
// file and tells the caller which kind of view produced the result. This
// package deliberately knows nothing about binding metadata, root keys or
// project ownership: the probe decides what the contents mean.
//
// Why a callback and not a handle: an immutable view (below) is not a snapshot.
// SQLite assumes an immutable file never changes, so pages it has cached stay
// stale while pages it reads later may already be new. A handle returned to the
// caller would keep exposing such a mixed view for as long as the caller used
// it, and no check made at open time could protect those later queries. The
// verification therefore has to wrap the probe itself, which is why the probe
// runs inside this package.
//
// How a view is chosen, per attempt:
//
//  1. The path is opened with the repository's private-file rules
//     (securefs.OpenPrivateFile: directory chain checked, final symlink
//     refused, regular file owned by the caller with no group/other access).
//     The first 16 bytes are read from that descriptor: a zero-byte file is
//     ErrEmptyDatabase, anything without the SQLite magic is ErrNotSQLite.
//  2. If neither <path>-wal nor <path>-shm exists, no connection can be live
//     and no WAL can be pending. The probe runs in a read-only transaction on a
//     mode=ro&immutable=1 view, which creates and modifies nothing. The result
//     is accepted (ViewImmutable) only if, after the probe, the sidecars are
//     still absent, the file's signature (device, inode, size, modification
//     time and change time; see statSig and inspect_ctime_*.go) is identical to
//     the one captured before the probe, and the path still names the file whose
//     header was checked. Otherwise the result is discarded, together with the
//     sidecar observations made during that attempt, and the whole attempt
//     starts again. If the immutable read itself fails (for example a torn read
//     reported as "malformed"), the WAL-aware view below is used instead.
//  3. If any sidecar exists (or the immutable attempt could not produce a
//     result), the probe runs in ONE read transaction on a mode=ro view with a
//     bounded busy timeout (ViewWALAware). A read transaction is a consistent
//     snapshot, and the view sees WAL-resident data. Transient
//     SQLITE_BUSY/LOCKED errors are retried a bounded number of times. SQLite
//     may rewrite -shm (the shared wal-index) and, when only one of -wal/-shm
//     exists, may create the other; the main database file and an existing -wal
//     are never written.
//  4. Attempts that observe a change are restarted, from the file gate, a
//     bounded number of times. Persistent change is ErrInspectionFailed.
//
// Contract for callers:
//
//   - The probe must be read-only and idempotent: it can run more than once.
//     Only the result of the LAST invocation, and only when InspectDatabase
//     returns a nil error, is valid; results of earlier invocations were
//     discarded because the file changed or the view was unverified.
//   - Every error from the probe is returned wrapped together with
//     ErrInspectionFailed (errors.Is matches both). A probe should report
//     "the schema I expected is absent" through its own result, not as an error.
//   - A positive finding (rows exist) is trustworthy from either view when
//     InspectDatabase returns nil.
//   - A negative finding is definitive only for ViewWALAware with
//     WALPresent == false (a snapshot with no WAL pending). For ViewImmutable it
//     is "verified stable", which is strong but not proof: see the limits.
//   - A negative finding with WALPresent == true is inconclusive: SQLite ignores
//     WAL frames that fail their checksums, so a truncated, damaged or
//     unreplayed -wal looks exactly like missing data. It must never be reported
//     as "not initialized". A zero-byte main file is ErrEmptyDatabase even if a
//     -wal exists.
//   - InspectionResult describes ONLY the attempt whose probe result is
//     returned. WALPresent is true if a non-empty -wal was observed when the
//     attempt began, just before the database was opened for the probe, or just
//     after the probe; presence seen at any of those points is kept even if the
//     file is gone by the end. An empty -wal does not count (SQLite creates one
//     itself when a read-only connection opens a WAL database that has none).
//     SHMPresent is true if an -shm existed when the attempt began or just
//     before the database was opened. It is deliberately not sampled after the
//     probe, because the WAL-aware read creates an -shm itself and that is not
//     evidence of a pre-existing one. The caller must not stat the sidecars
//     itself after InspectDatabase returns: that is a separate, racy check and
//     is polluted by the sidecars the inspection may have created.
//
// Limits (this guards against accidental path mistakes and incomplete
// initialization, NOT against a hostile or concurrent replacement of the file,
// and it is NOT race-free):
//
//   - SQLite opens the path itself, separately from the descriptor used for the
//     header check. Identity (os.SameFile) is compared after the probe, so a
//     replacement is detected and the attempt restarted, but a replacement that
//     happens after that comparison, or is swapped back, is not.
//   - The immutable view is verified, not guaranteed. ctime cannot guarantee
//     detection of a write that lands in the same timestamp tick as the earlier
//     stat on a coarse-resolution filesystem (the kernel stamps mtime and ctime
//     from the same clock), nor of a write that leaves size, mtime and ctime
//     all unchanged. ctime only adds detection when mtime was explicitly reset.
//     Off Linux the signature is size and modification time only (see
//     inspect_ctime_other.go). No content fingerprint is taken; this residual
//     risk is accepted for this phase, and its only effect is a stale
//     "verified stable" negative, never an unsafe acceptance.
//   - The device and inode in the signature come from the SAME descriptor before
//     and after, so they cannot differ; replacement of the path is detected by
//     comparing the descriptor with the path (os.SameFile) after the probe.
//   - On a read-only filesystem the immutable view works, mode=ro works only
//     when both -wal and -shm already exist, and every other combination fails
//     closed with ErrInspectionFailed.
//   - The mode=ro fallback is not free of side effects: see step 3.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	msqlite "modernc.org/sqlite"

	"github.com/docker-secret-operator/dso/internal/securefs"
)

// ErrEmptyDatabase is returned for a zero-byte file.
var ErrEmptyDatabase = errors.New("sqlite: database file is empty")

// ErrNotSQLite is returned for a non-empty file that does not start with the
// SQLite file header.
var ErrNotSQLite = errors.New("sqlite: file is not a SQLite database")

// ErrInspectionFailed means the file could not be inspected safely: a locked
// database after the bounded retries, a read or a probe that failed, damage
// reported by SQLite, non-regular sidecars, or a file that kept changing. It
// says nothing about whether the database is initialized.
var ErrInspectionFailed = errors.New("sqlite: database could not be inspected safely")

// InspectionView says how the probe's result was obtained.
type InspectionView int

const (
	// ViewImmutable: the probe ran on an immutable view (no sidecars existed)
	// and the file was verified unchanged across the probe.
	ViewImmutable InspectionView = iota + 1
	// ViewWALAware: the probe ran inside one read transaction (a snapshot) on a
	// mode=ro view that sees WAL-resident data.
	ViewWALAware
)

func (v InspectionView) String() string {
	switch v {
	case ViewImmutable:
		return "immutable"
	case ViewWALAware:
		return "wal-aware"
	}
	return "unknown"
}

// InspectionResult describes how, and under which sidecar conditions, a probe's
// result was obtained. It describes only the single attempt whose probe result
// is returned: attempts that were discarded and restarted contribute nothing.
// See the file comment for the exact meaning of the flags and how a caller must
// use them.
type InspectionResult struct {
	View InspectionView
	// WALPresent is true if a non-empty -wal was observed during the attempt.
	WALPresent bool
	// SHMPresent is true if an -shm existed before the probe database was opened.
	SHMPresent bool
}

// Querier is the read-only query surface a probe receives. *sql.Tx satisfies
// it; the transaction is always read-only (the connection is query_only).
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

const sqliteHeaderMagic = "SQLite format 3\x00"

// inspector holds every tunable and test seam, so none of it is shared mutable
// state. Production always uses newInspector(); tests build their own value and
// override fields. openPrivate is the security gate and is never replaced in
// production.
type inspector struct {
	openPrivate func(string) (*os.File, error)
	busyTimeout time.Duration
	attempts    int           // transient-lock tries for the WAL-aware view
	restarts    int           // whole-sequence restarts when stability fails
	retryDelay  time.Duration // pause between tries and restarts

	// Test hooks; nil in production.
	afterHeader         func()
	afterSidecarCheck   func()
	afterImmutableProbe func()
}

func newInspector() *inspector {
	return &inspector{
		openPrivate: securefs.OpenPrivateFile,
		busyTimeout: 1500 * time.Millisecond,
		attempts:    3,
		restarts:    3,
		retryDelay:  50 * time.Millisecond,
	}
}

// InspectDatabase runs probe against a read-only view of the database at path;
// see the file comment for the algorithm, the caller contract and the limits.
// It never creates, migrates or otherwise modifies the database file and never
// takes the central lock.
func InspectDatabase(path string, probe func(q Querier) error) (InspectionResult, error) {
	return newInspector().inspect(path, probe)
}

func (in *inspector) inspect(path string, probe func(Querier) error) (InspectionResult, error) {
	if probe == nil {
		return InspectionResult{}, errors.New("sqlite: InspectDatabase requires a probe")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return InspectionResult{}, fmt.Errorf("resolving database path %s: %w", path, err)
	}
	for i := 0; i < in.restarts; i++ {
		res, restart, err := in.attempt(abs, probe)
		if !restart {
			return res, err
		}
		if i < in.restarts-1 {
			time.Sleep(in.retryDelay)
		}
	}
	return InspectionResult{}, inspectionFailed(fmt.Errorf("%s kept changing during inspection (%d attempts); retry when no process is writing to it", abs, in.restarts))
}

// attempt makes one pass. restart=true means the file, its sidecars or its
// identity changed while the probe ran and nothing from the pass, including its
// sidecar observations, may be used.
func (in *inspector) attempt(abs string, probe func(Querier) error) (res InspectionResult, restart bool, err error) {
	f, err := in.openPrivate(abs)
	if err != nil {
		return InspectionResult{}, false, fmt.Errorf("database file is not safe to use: %w", err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return InspectionResult{}, false, inspectionFailed(err)
	}
	if fi.Size() == 0 {
		return InspectionResult{}, false, fmt.Errorf("%w: %s", ErrEmptyDatabase, abs)
	}
	magic := make([]byte, len(sqliteHeaderMagic))
	n, rerr := f.ReadAt(magic, 0)
	if rerr != nil && !errors.Is(rerr, io.EOF) {
		return InspectionResult{}, false, inspectionFailed(rerr)
	}
	if n != len(magic) || string(magic) != sqliteHeaderMagic {
		return InspectionResult{}, false, fmt.Errorf("%w: %s", ErrNotSQLite, abs)
	}
	if in.afterHeader != nil {
		in.afterHeader()
	}

	start, err := sampleSidecars(abs)
	if err != nil {
		return InspectionResult{}, false, inspectionFailed(err)
	}
	if in.afterSidecarCheck != nil {
		in.afterSidecarCheck()
	}

	if !start.exists() {
		ok, restart, post := in.immutableAttempt(abs, f, probe)
		if ok {
			return InspectionResult{
				View:       ViewImmutable,
				WALPresent: start.walBytes > 0 || post.walBytes > 0,
				SHMPresent: start.shm,
			}, false, nil
		}
		if restart {
			return InspectionResult{}, true, nil
		}
		// The immutable read failed without any observed change: use the
		// WAL-aware snapshot instead.
	}
	return in.walAwareAttempt(abs, f, probe, start)
}

// immutableAttempt returns ok=true only for a verified-stable result. post is
// the sidecar sample taken right after the probe.
func (in *inspector) immutableAttempt(abs string, f *os.File, probe func(Querier) error) (ok, restart bool, post sidecars) {
	fi, err := f.Stat()
	if err != nil {
		return false, false, sidecars{}
	}
	before := statSigOf(fi) // the signature captured BEFORE the probe
	perr := in.runProbe(inspectionDSN(abs, true, 0), probe)
	if in.afterImmutableProbe != nil {
		in.afterImmutableProbe()
	}
	// Verification wraps the probe: any sign of change discards the result.
	post, e2 := sampleSidecars(abs)
	afterFi, err := f.Stat()
	if e2 != nil || post.exists() || err != nil ||
		statSigOf(afterFi) != before ||
		!samePath(abs, f) {
		return false, true, post
	}
	return perr == nil, false, post
}

// walAwareAttempt runs the probe in one read transaction, retrying only
// transient lock errors. Sidecars are sampled before the database is opened
// (so sidecars SQLite creates for this read are not mistaken for earlier
// evidence) and again after the probe (only a non-empty -wal counts).
func (in *inspector) walAwareAttempt(abs string, f *os.File, probe func(Querier) error, start sidecars) (InspectionResult, bool, error) {
	pre, err := sampleSidecars(abs)
	if err != nil {
		return InspectionResult{}, false, inspectionFailed(err)
	}
	dsn := inspectionDSN(abs, false, in.busyTimeout)
	var last error
	for i := 0; i < in.attempts; i++ {
		err := in.runProbe(dsn, probe)
		if err == nil {
			post, serr := sampleSidecars(abs)
			if serr != nil {
				return InspectionResult{}, false, inspectionFailed(serr)
			}
			if !samePath(abs, f) {
				return InspectionResult{}, true, nil
			}
			return InspectionResult{
				View:       ViewWALAware,
				WALPresent: start.walBytes > 0 || pre.walBytes > 0 || post.walBytes > 0,
				SHMPresent: start.shm || pre.shm,
			}, false, nil
		}
		last = err
		if !isTransientInspectError(err) {
			break
		}
		if i < in.attempts-1 {
			time.Sleep(in.retryDelay)
		}
	}
	return InspectionResult{}, false, inspectionFailed(last)
}

// runProbe opens dsn on a single connection, runs probe in one read
// transaction and rolls it back. Any error (open, begin or probe) is returned.
func (in *inspector) runProbe(dsn string, probe func(Querier) error) error {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	return probe(tx)
}

// samePath reports whether abs still names the file f was opened on.
func samePath(abs string, f *os.File) bool {
	want, err := f.Stat()
	if err != nil {
		return false
	}
	got, err := os.Lstat(abs)
	if err != nil {
		return false
	}
	return os.SameFile(want, got)
}

func inspectionFailed(err error) error {
	return fmt.Errorf("%w: %w", ErrInspectionFailed, err)
}

// inspectionDSN builds a read-only DSN. Immutable views take no busy timeout.
func inspectionDSN(abs string, immutable bool, busy time.Duration) string {
	q := url.Values{}
	q.Set("mode", "ro")
	if immutable {
		q.Set("immutable", "1")
	} else {
		q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busy.Milliseconds()))
	}
	q.Add("_pragma", "query_only(1)")
	return (&url.URL{Scheme: "file", Opaque: escapePath(abs), RawQuery: q.Encode()}).String()
}

// sidecars is one sample of the -wal and -shm companions of a database file.
type sidecars struct {
	wal      bool  // a -wal file exists
	walBytes int64 // its size; an empty -wal carries no frames and is not evidence of a pending WAL
	shm      bool  // an -shm file exists
}

func (s sidecars) exists() bool { return s.wal || s.shm }

// sampleSidecars samples <path>-wal and <path>-shm. A sidecar that is not a
// regular file, or cannot be examined, is an error.
func sampleSidecars(path string) (sidecars, error) {
	var s sidecars
	for _, c := range []string{"-wal", "-shm"} {
		fi, err := os.Lstat(path + c)
		switch {
		case err == nil:
			if !fi.Mode().IsRegular() {
				return sidecars{}, fmt.Errorf("%s%s is not a regular file", path, c)
			}
			if c == "-wal" {
				s.wal, s.walBytes = true, fi.Size()
			} else {
				s.shm = true
			}
		case errors.Is(err, os.ErrNotExist):
		default:
			return sidecars{}, err
		}
	}
	return s, nil
}

// statSig is the immutable-verification signature of the checked file. The
// device, inode and change time come from the platform (inspect_ctime_*.go);
// platform is false where they are unavailable, in which case only size and
// modification time are compared.
type statSig struct {
	size     int64
	mtimeNs  int64
	dev, ino uint64
	ctimeNs  int64
	platform bool
}

func statSigOf(fi os.FileInfo) statSig {
	s := statSig{size: fi.Size(), mtimeNs: fi.ModTime().UnixNano()}
	s.dev, s.ino, s.ctimeNs, s.platform = platformStat(fi)
	return s
}

// isTransientInspectError reports SQLITE_BUSY / SQLITE_LOCKED (including their
// extended codes).
func isTransientInspectError(err error) bool {
	var se *msqlite.Error
	if errors.As(err, &se) {
		switch se.Code() & 0xff {
		case 5, 6: // SQLITE_BUSY, SQLITE_LOCKED
			return true
		}
		return false
	}
	return strings.Contains(err.Error(), "database is locked")
}
