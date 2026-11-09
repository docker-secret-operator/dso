package sqlite

// Phase D: a read-only recovery-artifact scanner.
//
// ScanRecoveryArtifacts looks, beside a database path, for the on-disk
// leftovers that PreserveActiveFiles (preserve.go) and ReplaceActiveDatabase
// (displace.go) can leave behind -- a raw preservation (".pre-restore-raw-*",
// published or still ".partial"), a displacement aside directory
// (".replaced-*"), or a prepared-replacement temporary file (".restoring-*")
// -- and reports what it found, without ever acting on it.
//
// This file implements DETECTION, OBSERVATION, CLASSIFICATION and REPORTING
// only (per the Phase D.1 design refinement report). It never performs
// RECOVERY or CLEANUP: it never deletes, renames, moves, repairs, or
// overwrites anything it finds, never opens the active database through
// SQLite, never triggers WAL recovery, and never creates or modifies a
// -wal/-shm/-journal sidecar. It calls no SQLite inspection path at all
// (sqlite.Open, InspectDatabase, CheckInitialized) against the active
// database -- only plain os.Lstat/os.ReadDir/byte-level reads of files this
// package itself already wrote the naming conventions and manifest format
// for.
//
// Classification is deliberately conservative, per the Phase D.1 crash-state
// matrix (§5) and stale-entry matrix (§6):
//
//   - It never claims an artifact is safe to delete or safe to restore.
//   - It never infers transaction completion, install outcome, or file
//     identity from a filename, a timestamp, or directory shape alone.
//   - It never calls os.SameFile against an identity that was never
//     persisted to disk -- no displacement or replacement-temp artifact
//     carries one (only a published preservation has a manifest, and that
//     manifest proves self-consistency with itself, never that the file it
//     describes is still the legitimate original).
//   - Age alone never raises a classification's confidence.
//   - A self-consistent, published preservation is never described as
//     disposable merely because it verifies or because it is old: whether it
//     is still needed depends on whether the restore it preceded ever
//     completed, which this directory does not record about itself.
//
// Without a persisted recovery marker (which this phase does not introduce),
// several crash states can never be resolved above ConfidenceManual: a
// displacement aside directory that holds files while no main database file
// exists at the active path (partial displacement, mixed companion
// placement, or mid-rollback), and every ".restoring-*" temporary file. See
// the Phase D.1 report, §5, rows 3/4/5/6/9.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/docker-secret-operator/dso/internal/securefs"
)

// Confidence is how strongly an Artifact's classification is supported by
// what the scanner actually observed. It is never a stand-in for "safe" or
// "unsafe to delete" -- those actions are out of scope for this phase
// entirely, and ConfidenceFact never means more than "this filesystem fact is
// true", not "this state is fine".
type Confidence string

const (
	// ConfidenceFact: the facts listed fully support the stated observation
	// and nothing about this artifact is ambiguous within the artifact's own
	// scope (an empty directory is empty; a manifest's recorded hashes match
	// the files beside it).
	ConfidenceFact Confidence = "fact"
	// ConfidenceInconclusive: the evidence does not support a confident
	// statement either way (an unreadable directory; a malformed manifest).
	ConfidenceInconclusive Confidence = "inconclusive"
	// ConfidenceManual: the artifact's shape is one the Phase D.1 matrices
	// name explicitly as requiring manual, reference-based operator
	// judgment -- a partial displacement, a mixed companion placement, a
	// replacement temp file, or an anomaly (symlink, non-regular file,
	// unexpected or missing manifest entry, ownership/permission mismatch).
	ConfidenceManual Confidence = "manual-intervention"
)

// ArtifactKind identifies which on-disk leftover an Artifact describes.
type ArtifactKind string

const (
	ArtifactPreservationPartial   ArtifactKind = "preservation-partial"   // "<db>.pre-restore-raw-*.partial"
	ArtifactPreservationPublished ArtifactKind = "preservation-published" // "<db>.pre-restore-raw-*" (no .partial)
	ArtifactDisplacementAside     ArtifactKind = "displacement-aside"     // "<db>.replaced-*"
	ArtifactReplacementTemp       ArtifactKind = "replacement-temp"       // "<db>.restoring-*"
)

// Artifact is one discovered leftover, reported as proven facts kept
// strictly separate from open questions, never blended into one prose
// message.
type Artifact struct {
	Kind ArtifactKind
	// Path is the artifact's own path (a directory for every kind except
	// ArtifactReplacementTemp, which is a single file).
	Path string
	// Confidence is the ceiling this report never exceeds; see its doc comment.
	Confidence Confidence
	// Facts are statements the scanner can support directly from what it
	// observed -- never an inference beyond the listed evidence.
	Facts []string
	// Unresolved are the specific questions the available evidence cannot
	// answer, stated so an operator knows exactly what is missing, not just
	// that something is uncertain.
	Unresolved []string
	// MainDatabaseUnsafe is true when this artifact's own state means the
	// active database at the scanned path must not be treated as safe to
	// open (a partial displacement or mid-rollback shape, or an unresolved
	// replacement temp file). It is never set to mean "safe" -- its absence
	// says nothing either way.
	MainDatabaseUnsafe bool
}

// ScanResult is everything ScanRecoveryArtifacts found for one database path.
// It is never nil from a successful scan.
type ScanResult struct {
	DBPath    string
	Artifacts []Artifact
}

// ScanRecoveryArtifacts enumerates, observes and classifies every recovery
// artifact beside dbPath. It takes no lock itself and opens nothing through
// SQLite; a caller that wants the shared-lock coordination described in
// SharedLock's doc comment acquires it separately (see
// internal/centralsvc/restore_recovery.go) and reports that outcome itself.
// This function never fails merely because no artifacts exist: an empty
// ScanResult.Artifacts is the normal, common case.
func ScanRecoveryArtifacts(dbPath string) (ScanResult, error) {
	abs, err := filepath.Abs(dbPath)
	if err != nil {
		return ScanResult{}, fmt.Errorf("resolving database path %s: %w", dbPath, err)
	}
	res := ScanResult{DBPath: abs}

	mainPresent := false
	if fi, err := os.Lstat(abs); err == nil && fi.Mode().IsRegular() {
		mainPresent = true
	}

	glob := func(pattern string) []string {
		m, _ := filepath.Glob(abs + pattern)
		sort.Strings(m)
		return m
	}

	for _, p := range glob(".pre-restore-raw-*.partial") {
		res.Artifacts = append(res.Artifacts, scanPreservationPartial(p))
	}
	for _, p := range glob(".pre-restore-raw-*") {
		if strings.HasSuffix(p, ".partial") {
			continue // already handled above
		}
		res.Artifacts = append(res.Artifacts, scanPreservationPublished(p))
	}
	for _, p := range glob(".replaced-*") {
		res.Artifacts = append(res.Artifacts, scanDisplacementAside(p, mainPresent))
	}
	for _, p := range glob(".restoring-*") {
		res.Artifacts = append(res.Artifacts, scanReplacementTemp(p, mainPresent))
	}
	return res, nil
}

// lstatKind describes a path's type without opening or following it, for
// anomaly reporting shared across every artifact kind.
func lstatKind(path string) (fi os.FileInfo, anomaly string, err error) {
	fi, err = os.Lstat(path)
	if err != nil {
		return nil, "", err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fi, fmt.Sprintf("%s is a symbolic link; it was not followed", path), nil
	}
	if !fi.Mode().IsRegular() && !fi.IsDir() {
		return fi, fmt.Sprintf("%s is a %s, not a regular file or directory", path, fileKind(fi.Mode())), nil
	}
	return fi, "", nil
}

// listEntries lists a directory's immediate children by Lstat only (never
// following a symlink to a subdirectory), reporting any entry that is itself
// a symlink or a non-regular, non-directory file as an anomaly.
func listEntries(dir string) (names []string, anomalies []string, err error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range ents {
		names = append(names, e.Name())
		_, anomaly, lerr := lstatKind(filepath.Join(dir, e.Name()))
		if lerr != nil {
			anomalies = append(anomalies, fmt.Sprintf("%s could not be examined: %v", filepath.Join(dir, e.Name()), lerr))
			continue
		}
		if anomaly != "" {
			anomalies = append(anomalies, anomaly)
		}
	}
	sort.Strings(names)
	return names, anomalies, nil
}

// ownershipNote reports a non-private mode as an observation; it never by
// itself changes an artifact's classification beyond ConfidenceManual.
func ownershipNote(path string, fi os.FileInfo) string {
	if fi.Mode().Perm()&0o077 != 0 {
		return fmt.Sprintf("%s has group or other access bits set (%04o)", path, fi.Mode().Perm())
	}
	return ""
}

func scanPreservationPartial(dir string) Artifact {
	a := Artifact{Kind: ArtifactPreservationPartial, Path: dir, Confidence: ConfidenceInconclusive}
	fi, anomaly, err := lstatKind(dir)
	if err != nil {
		a.Facts = append(a.Facts, fmt.Sprintf("%s could not be examined: %v", dir, err))
		return a
	}
	if anomaly != "" || !fi.IsDir() {
		a.Facts = append(a.Facts, fmt.Sprintf("%s is not a directory", dir))
		a.Confidence = ConfidenceManual
		return a
	}
	a.Facts = append(a.Facts, fmt.Sprintf("%s exists and was never published (its name ends in \".partial\")", dir))
	names, anomalies, err := listEntries(dir)
	if err != nil {
		a.Facts = append(a.Facts, fmt.Sprintf("its contents could not be listed: %v", err))
	} else {
		a.Facts = append(a.Facts, fmt.Sprintf("it contains: %s", joinOrNone(names)))
	}
	a.Unresolved = append(a.Unresolved,
		"whether the process that was writing this preservation is still running cannot be determined by this scanner alone (the shared lock, if held, only excludes another dso central process; it does not exclude an unrelated process, nor prove this one has exited)",
		"this directory is never published, so it is never self-consistent with a manifest by construction; completeness cannot be assessed")
	a.Unresolved = append(a.Unresolved, anomalies...)
	if len(anomalies) > 0 {
		a.Confidence = ConfidenceManual
	}
	return a
}

func scanPreservationPublished(dir string) Artifact {
	a := Artifact{Kind: ArtifactPreservationPublished, Path: dir, Confidence: ConfidenceInconclusive}
	fi, anomaly, err := lstatKind(dir)
	if err != nil {
		a.Facts = append(a.Facts, fmt.Sprintf("%s could not be examined: %v", dir, err))
		return a
	}
	if anomaly != "" || !fi.IsDir() {
		a.Facts = append(a.Facts, fmt.Sprintf("%s is not a directory", dir))
		a.Confidence = ConfidenceManual
		return a
	}

	names, anomalies, err := listEntries(dir)
	if err != nil {
		a.Facts = append(a.Facts, fmt.Sprintf("its contents could not be listed: %v", err))
		return a
	}
	a.Facts = append(a.Facts, fmt.Sprintf("%s is published (its name does not end in \".partial\")", dir))

	mpath := filepath.Join(dir, "manifest.json")
	// securefs.OpenRegularNoFollow, not a separate Lstat followed by os.Open:
	// the no-follow refusal is enforced by the kernel at this single open
	// call, so there is no window between an inspection and a later open for
	// the path to be replaced in.
	mf, err := securefs.OpenRegularNoFollow(mpath)
	if err != nil {
		a.Facts = append(a.Facts, fmt.Sprintf("its manifest could not be opened: %v", err))
		a.Confidence = ConfidenceManual
		return a
	}
	defer mf.Close()
	raw, err := io.ReadAll(io.LimitReader(mf, 1<<20))
	if err != nil {
		a.Facts = append(a.Facts, fmt.Sprintf("its manifest could not be read: %v", err))
		a.Confidence = ConfidenceManual
		return a
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		a.Facts = append(a.Facts, fmt.Sprintf("its manifest is not valid JSON: %v", err))
		a.Confidence = ConfidenceManual
		return a
	}

	expected := map[string]bool{"manifest.json": true}
	seen := map[string]bool{} // M-1: tracks every manifest filename already processed once
	allMatch := true
	for _, mfile := range m.Files {
		// M-1 duplicate detection: this check is the first operation on each
		// entry, before the name-safety check, before filepath.Join, and
		// before any hashFileNoFollow call. The first occurrence of a name is
		// recorded in seen and falls through to the existing validation and
		// hashing logic below. Every subsequent occurrence is reported as a
		// manifest anomaly and skipped entirely -- the referenced file is
		// never opened or read a second time.
		if seen[mfile.Name] {
			a.Facts = append(a.Facts, fmt.Sprintf("manifest entry %q appears more than once; this duplicate occurrence was skipped", mfile.Name))
			allMatch = false
			continue
		}
		seen[mfile.Name] = true

		if !isSafeManifestEntryName(mfile.Name) {
			// Never join, open, stat or hash anything for an unsafe name: the
			// check below is a positive allow-list (bare filename, no
			// separator, no "." or "..", not absolute), not a sanitize-then-
			// accept pass over an already-built path, so there is no
			// filepath.Join call to reach in this branch at all.
			a.Facts = append(a.Facts, fmt.Sprintf("manifest entry %q is not a safe filename and was not opened or read", mfile.Name))
			allMatch = false
			continue
		}
		expected[mfile.Name] = true
		fpath := filepath.Join(dir, mfile.Name)
		sum, size, herr := hashFileNoFollow(fpath)
		switch {
		case herr != nil:
			a.Facts = append(a.Facts, fmt.Sprintf("manifest entry %q (%s) is missing or unreadable: %v", mfile.Name, mfile.Source, herr))
			allMatch = false
		case sum != mfile.SHA256 || size != mfile.Size:
			a.Facts = append(a.Facts, fmt.Sprintf("manifest entry %q (%s) does not match its recorded hash/size", mfile.Name, mfile.Source))
			allMatch = false
		default:
			a.Facts = append(a.Facts, fmt.Sprintf("manifest entry %q matches its recorded hash and size", mfile.Name))
		}
	}
	var unexpected []string
	for _, n := range names {
		if !expected[n] {
			unexpected = append(unexpected, n)
		}
	}
	if len(unexpected) > 0 {
		a.Facts = append(a.Facts, fmt.Sprintf("unexpected entries not named in the manifest: %s", strings.Join(unexpected, ", ")))
		allMatch = false
	}
	if len(anomalies) > 0 {
		a.Facts = append(a.Facts, anomalies...)
		allMatch = false
	}

	if allMatch {
		a.Confidence = ConfidenceFact
		a.Facts = append(a.Facts, "this preservation is internally self-consistent with its own manifest")
		a.Unresolved = append(a.Unresolved,
			"self-consistency proves the copy is intact; it does NOT prove this preservation is no longer needed -- that depends on whether the restore it preceded ever completed, which this directory does not record about itself")
	} else {
		a.Confidence = ConfidenceManual
	}
	return a
}

func scanDisplacementAside(dir string, mainPresent bool) Artifact {
	a := Artifact{Kind: ArtifactDisplacementAside, Path: dir}
	fi, anomaly, err := lstatKind(dir)
	if err != nil {
		a.Facts = append(a.Facts, fmt.Sprintf("%s could not be examined: %v", dir, err))
		a.Confidence = ConfidenceInconclusive
		return a
	}
	if anomaly != "" || !fi.IsDir() {
		a.Facts = append(a.Facts, fmt.Sprintf("%s is not a directory", dir))
		a.Confidence = ConfidenceManual
		return a
	}
	names, anomalies, err := listEntries(dir)
	if err != nil {
		a.Facts = append(a.Facts, fmt.Sprintf("its contents could not be listed: %v", err))
		a.Confidence = ConfidenceInconclusive
		return a
	}
	if len(names) == 0 {
		a.Facts = append(a.Facts, fmt.Sprintf("%s is empty: nothing is retained in it", dir))
		a.Confidence = ConfidenceFact
		return a
	}
	a.Facts = append(a.Facts, fmt.Sprintf("%s contains: %s", dir, joinOrNone(names)))
	a.Facts = append(a.Facts, anomalies...)
	hasMain := false
	for _, n := range names {
		if n == "main" {
			hasMain = true
		}
	}
	switch {
	case mainPresent:
		a.Facts = append(a.Facts, "a file currently exists at the active database path")
		a.Confidence = ConfidenceFact
		a.Unresolved = append(a.Unresolved,
			"this scanner does not verify that the file at the active path is the legitimate installed replacement, only that a file is present there; it never claims the database is safe to open on that basis alone")
	case hasMain:
		a.Facts = append(a.Facts, "no file currently exists at the active database path, but a file named \"main\" is retained here")
		a.Confidence = ConfidenceManual
		a.MainDatabaseUnsafe = true
		a.Unresolved = append(a.Unresolved,
			"whether this retained \"main\" file is the pre-restore original, a partially-rolled-back file, or something else cannot be established without an identity that was persisted before the process that moved it ended -- no such identity exists for this artifact",
			"a companion (-wal/-journal) beside a missing main file is a hazardous shape this system treats as unsafe; do not treat any file here as the active database")
	default:
		a.Facts = append(a.Facts, "no file currently exists at the active database path, and no \"main\" file is retained here either")
		a.Confidence = ConfidenceManual
		a.MainDatabaseUnsafe = true
		a.Unresolved = append(a.Unresolved,
			"this shape (companions present, no main file anywhere) matches a crash during displacement or rollback; which files belong to which stage cannot be established from directory contents alone")
	}
	return a
}

func scanReplacementTemp(path string, mainPresent bool) Artifact {
	a := Artifact{Kind: ArtifactReplacementTemp, Path: path, Confidence: ConfidenceManual, MainDatabaseUnsafe: !mainPresent}
	fi, anomaly, err := lstatKind(path)
	if err != nil {
		a.Facts = append(a.Facts, fmt.Sprintf("%s could not be examined: %v", path, err))
		a.Confidence = ConfidenceInconclusive
		return a
	}
	if anomaly != "" || !fi.Mode().IsRegular() {
		a.Facts = append(a.Facts, fmt.Sprintf("%s is not a regular file", path))
		return a
	}
	a.Facts = append(a.Facts, fmt.Sprintf("%s exists (%d bytes)", path, fi.Size()))
	if note := ownershipNote(path, fi); note != "" {
		a.Facts = append(a.Facts, note)
	}
	if sum, _, herr := hashFileNoFollow(path); herr == nil {
		a.Facts = append(a.Facts, fmt.Sprintf("its content sha256 is %s", sum))
	}
	if mainPresent {
		a.Facts = append(a.Facts, "a file currently exists at the active database path")
	} else {
		a.Facts = append(a.Facts, "no file currently exists at the active database path")
	}
	a.Unresolved = append(a.Unresolved,
		"whether this file is a still-valid prepared replacement awaiting installation, an orphan left after the replacement's own cleanup failed, or (if an install rename's outcome was never resolved) the very file now installed at the active path cannot be determined by this scanner; compare its content hash above against a reference the operator independently holds (the original restore candidate, or a backup) before taking any action",
		"this scanner never installs, removes, or otherwise acts on this file")
	return a
}

// hashFileNoFollow opens path with the kernel's own no-follow refusal as the
// single point of truth for "is this a regular file at this exact path",
// then inspects and reads only the resulting descriptor -- never a second,
// separate lookup of the path string. There is deliberately no preceding
// Lstat here: a stat-then-open pair would leave a window between the two
// calls in which the path could be replaced (for example with a symlink),
// and the open would then follow it; securefs.OpenRegularNoFollow instead
// performs the refusal as part of the one open(2) call itself (O_NOFOLLOW),
// so there is no second resolution of the path for a replacement to race.
//
// This closes unsafe PATH RESOLUTION during opening. It does not, and is not
// intended to, detect every possible in-place mutation of an already-open
// regular file by a concurrent writer -- that is a different, deferred
// concern (no before/after descriptor comparison is performed here), not
// something this function claims to guard against.
func hashFileNoFollow(path string) (sum string, size int64, err error) {
	f, err := securefs.OpenRegularNoFollow(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// isSafeManifestEntryName is a positive allow-list, never a sanitize-then-
// accept pass: a manifest-provided Name is usable in filepath.Join only if it
// is exactly a bare filename with no way to leave the directory it is joined
// into. This rejects, independent of host OS, so the same manifest is judged
// identically on every platform this package compiles for:
//
//   - the empty string;
//   - "." and ".." (and any name equal to them after no transformation --
//     this function does not clean or normalize the input first, since that
//     would itself be "normalize, then accept", which this is deliberately
//     not);
//   - any path separator this package or any of its target platforms could
//     treat as one: '/' (unix, and recognized by Go's path/filepath on every
//     platform) and '\' (Windows, rejected here even though this package
//     never builds for Windows -- see securefs's own fail-closed stance on
//     non-unix platforms -- so a manifest crafted for one platform cannot be
//     replayed against a scan on another);
//   - a ':' (a Windows drive letter or an NTFS alternate data stream
//     qualifier, neither of which is a bare filename on any platform);
//   - anything filepath.IsAbs considers absolute on this build's platform.
func isSafeManifestEntryName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsAny(name, "/\\:") {
		return false
	}
	if filepath.IsAbs(name) {
		return false
	}
	return true
}

func joinOrNone(names []string) string {
	if len(names) == 0 {
		return "(nothing)"
	}
	return strings.Join(names, ", ")
}
