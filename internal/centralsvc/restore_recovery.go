package centralsvc

// Phase D: thin orchestration for the read-only recovery-artifact scanner
// (internal/store/sqlite/recovery_scan.go). This file resolves the active
// database path, attempts the shared central lock (sqlite.SharedLock), and --
// ONLY if that lock is held -- scans for recovery artifacts. It never opens
// the active database, never calls InspectDatabase or CheckInitialized, and
// never touches Restore's own displacement/rollback code paths.
//
// A failure to acquire the shared lock is reported as an explicit,
// scan-not-performed result (RecoveryScanResult.Scanned == false), never as
// a silent unlocked scan: if a concurrent exclusive Restore holds the lock,
// or the lock otherwise cannot be taken within its timeout, this function
// says so and returns without reading the filesystem at all. An earlier
// version of this file scanned regardless of the lock outcome and only
// flagged the failure in a side note; that let every per-artifact
// classification look identical whether or not coordination had actually
// happened, which is exactly the defect this version closes.

import (
	"path/filepath"
	"time"

	"github.com/docker-secret-operator/dso/internal/store/sqlite"
)

// scanRecoveryArtifactsFn is sqlite.ScanRecoveryArtifacts, held behind a
// package-level variable purely as an unexported test seam: a test in this
// package can swap it for a spy to prove the scan path is never reached when
// the shared lock could not be acquired (see restore_recovery_test.go). It is
// not an exported hook and carries no public API of its own; production code
// always leaves it at this default.
var scanRecoveryArtifactsFn = sqlite.ScanRecoveryArtifacts

// RecoveryScanResult is what ScanActiveRecoveryArtifacts reports.
//
// Scanned is the field every consumer MUST check before reading Artifacts:
// it is true only when the shared central lock was actually held for the
// whole filesystem scan that produced them. When the lock could not be
// acquired, ScanActiveRecoveryArtifacts never calls the scanner at all --
// Scanned is false, Artifacts is always nil, and no artifact anywhere in
// this result was ever given a classification. There is deliberately no
// field on this type that looks like a classification result but was
// produced without the lock: a caller cannot mistake an uncoordinated
// attempt for a successfully coordinated scan by reading the wrong field.
type RecoveryScanResult struct {
	DBPath string
	// LockAcquired is true only when the shared central lock was held.
	LockAcquired bool
	// LockNote explains, in the caller's own words, exactly what
	// LockAcquired does and does not establish -- never that no process
	// anywhere is using the database (see sqlite.SharedLock's doc comment:
	// the lock is advisory, local-only, and coordinates only with other dso
	// central processes).
	LockNote string
	// Scanned is true only when the filesystem was actually examined and
	// Artifacts reflects real classifications. False means no artifact was
	// read, hashed, or classified for this call -- the only safe assumption
	// a caller may make about recovery artifacts in that case is "unknown".
	Scanned bool
	// Artifacts is always nil when Scanned is false. When Scanned is true,
	// it is exactly what sqlite.ScanRecoveryArtifacts reported.
	Artifacts []sqlite.Artifact
}

// ScanActiveRecoveryArtifacts attempts the shared central lock for dbPath
// and, ONLY if it is acquired, scans for recovery artifacts beside it.
// lockTimeout <= 0 uses the package default
// (sqlite.AcquireSharedCentralLock's own default).
//
// If the lock cannot be acquired, this function returns immediately with
// Scanned=false and never calls the scanner: per the approved Phase D.1
// contract, an uncoordinated filesystem read must never be classified as
// though it had been coordinated, so there is no "scan anyway and note the
// lock failure" path here at all -- the H-2 defect this replaces was exactly
// that path.
func ScanActiveRecoveryArtifacts(dbPath string, lockTimeout time.Duration) (RecoveryScanResult, error) {
	abs, err := filepath.Abs(dbPath)
	if err != nil {
		return RecoveryScanResult{}, err
	}
	res := RecoveryScanResult{DBPath: abs}

	lock, lockErr := sqlite.AcquireSharedCentralLock(abs, lockTimeout)
	if lockErr != nil {
		res.LockAcquired = false
		res.Scanned = false
		res.LockNote = "the shared central lock could not be acquired (" + lockErr.Error() + "); " +
			"artifact scanning and classification were NOT performed, because this result would otherwise be indistinguishable from a successfully coordinated scan -- retry once the conflicting dso central process (most likely an in-progress Restore) has finished"
		return res, nil
	}
	defer func() { _ = lock.Close() }()
	res.LockAcquired = true
	res.LockNote = "the shared central lock was held for this scan: no other dso central process held a conflicting (exclusive Restore) lock on this path while it ran. " +
		"This does NOT mean no process anywhere is using the database: the lock is advisory, local to this host, and coordinates only with other dso central processes -- " +
		"a process that opens the database file, its sidecars, or the lock sidecar itself directly (for example the sqlite3 CLI or a script) is not seen by it at all."

	sr, err := scanRecoveryArtifactsFn(abs)
	if err != nil {
		return res, err
	}
	res.Scanned = true
	res.Artifacts = sr.Artifacts
	return res, nil
}
