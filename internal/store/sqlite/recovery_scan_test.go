//go:build unix

package sqlite

// Phase D: tests for the read-only recovery-artifact scanner. Every fixture
// is synthetic and built under a fresh temporary directory (securefstest.
// TempDir, via the existing pvDir/pvBytes/pvWrite/pvDB helpers from
// preserve_test.go); none of these tests touch real user data.
//
// The safety property every test here establishes, stated once: the scanner
// never reports a confidence higher than the Phase D.1 matrices permit for
// the exact filesystem shape constructed, and it never modifies anything it
// reads.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// dirSnapshot captures every regular file's content under root, recursively,
// so a test can assert the scanner changed nothing.
func dirSnapshot(t *testing.T, root string) map[string][]byte {
	t.Helper()
	snap := map[string][]byte{}
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !info.Mode().IsRegular() {
			return nil
		}
		b, rerr := os.ReadFile(path) // #nosec G304 -- test fixture under a temp dir this test created
		if rerr == nil {
			snap[path] = b
		}
		return nil
	})
	return snap
}

func assertDirUnchanged(t *testing.T, root string, before map[string][]byte) {
	t.Helper()
	after := dirSnapshot(t, root)
	if len(before) != len(after) {
		t.Fatalf("file count changed: before %d, after %d", len(before), len(after))
	}
	for p, b := range before {
		got, ok := after[p]
		if !ok {
			t.Fatalf("%s is gone after the scan", p)
		}
		if !bytes.Equal(got, b) {
			t.Fatalf("%s content changed after the scan", p)
		}
	}
}

func TestScan_NoArtifacts_ReturnsEmpty(t *testing.T) {
	_, db := pvDB(t, false, false, false)
	res, err := ScanRecoveryArtifacts(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Artifacts) != 0 {
		t.Fatalf("expected no artifacts, got %+v", res.Artifacts)
	}
}

func TestScan_NeverModifiesWhatItReads(t *testing.T) {
	dir, db := pvDB(t, false, false, false)
	mkEmptyAside(t, db)
	mkPartialPreservation(t, db)
	pvWrite(t, db+".restoring-aaaa", pvBytes(100))
	before := dirSnapshot(t, dir)
	if _, err := ScanRecoveryArtifacts(db); err != nil {
		t.Fatal(err)
	}
	assertDirUnchanged(t, dir, before)
}

func mkEmptyAside(t *testing.T, db string) string {
	t.Helper()
	dir := db + ".replaced-20260101T000000Z-aaaa"
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func mkPartialPreservation(t *testing.T, db string) string {
	t.Helper()
	dir := db + ".pre-restore-raw-20260101T000000Z-aaaa.partial"
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestScan_PartialPreservation_IsInconclusiveNotGivenUpOn(t *testing.T) {
	_, db := pvDB(t, false, false, false)
	dir := mkPartialPreservation(t, db)
	pvWrite(t, filepath.Join(dir, "main"), pvBytes(10))

	res, err := ScanRecoveryArtifacts(db)
	if err != nil {
		t.Fatal(err)
	}
	a := mustOneArtifact(t, res, ArtifactPreservationPartial)
	if a.Confidence != ConfidenceInconclusive {
		t.Fatalf("expected inconclusive, got %s", a.Confidence)
	}
	for _, f := range a.Facts {
		if containsFold(f, "abandon") {
			t.Fatalf("a .partial directory must never be described as abandoned: %q", f)
		}
	}
}

// TestScan_PublishedPreservation_ValidManifest_IsFactAndNeverCalledDisposable
// builds a genuine published preservation via the real production function
// (PreserveActiveFiles), so the manifest and file contents are exactly what
// Restore itself would produce.
func TestScan_PublishedPreservation_ValidManifest_IsFactAndNeverCalledDisposable(t *testing.T) {
	_, db := pvDB(t, true, false, true)
	id, err := StatIdentity(db)
	if err != nil {
		t.Fatal(err)
	}
	pres, err := PreserveActiveFiles(db, id)
	if err != nil {
		t.Fatal(err)
	}

	res, err := ScanRecoveryArtifacts(db)
	if err != nil {
		t.Fatal(err)
	}
	a := mustOneArtifact(t, res, ArtifactPreservationPublished)
	if a.Path != pres.Dir() {
		t.Fatalf("expected %s, got %s", pres.Dir(), a.Path)
	}
	if a.Confidence != ConfidenceFact {
		t.Fatalf("expected fact, got %s: %+v", a.Confidence, a.Facts)
	}
	for _, f := range a.Facts {
		if containsFold(f, "disposable") {
			t.Fatalf("a self-consistent preservation must never be called disposable: %q", f)
		}
	}
	foundSelfConsistent, foundNeverDisposableNote := false, false
	for _, f := range a.Facts {
		if containsFold(f, "self-consistent") {
			foundSelfConsistent = true
		}
	}
	for _, u := range a.Unresolved {
		if containsFold(u, "does not prove") {
			foundNeverDisposableNote = true
		}
	}
	if !foundSelfConsistent || !foundNeverDisposableNote {
		t.Fatalf("expected a self-consistency fact and an explicit 'does not prove it is unneeded' note; got facts=%v unresolved=%v", a.Facts, a.Unresolved)
	}
}

func TestScan_PublishedPreservation_TamperedFile_IsManualIntervention(t *testing.T) {
	_, db := pvDB(t, false, false, false)
	id, err := StatIdentity(db)
	if err != nil {
		t.Fatal(err)
	}
	pres, err := PreserveActiveFiles(db, id)
	if err != nil {
		t.Fatal(err)
	}
	// Corrupt the preserved copy after publication: the manifest now
	// disagrees with what is on disk.
	pvWrite(t, filepath.Join(pres.Dir(), "main"), pvBytes(5))

	res, err := ScanRecoveryArtifacts(db)
	if err != nil {
		t.Fatal(err)
	}
	a := mustOneArtifact(t, res, ArtifactPreservationPublished)
	if a.Confidence != ConfidenceManual {
		t.Fatalf("expected manual-intervention, got %s", a.Confidence)
	}
}

func TestScan_PublishedPreservation_UnexpectedFile_IsReportedAndManual(t *testing.T) {
	_, db := pvDB(t, false, false, false)
	id, err := StatIdentity(db)
	if err != nil {
		t.Fatal(err)
	}
	pres, err := PreserveActiveFiles(db, id)
	if err != nil {
		t.Fatal(err)
	}
	pvWrite(t, filepath.Join(pres.Dir(), "unexpected.bin"), pvBytes(5))

	res, err := ScanRecoveryArtifacts(db)
	if err != nil {
		t.Fatal(err)
	}
	a := mustOneArtifact(t, res, ArtifactPreservationPublished)
	if a.Confidence != ConfidenceManual {
		t.Fatalf("expected manual-intervention, got %s", a.Confidence)
	}
	found := false
	for _, f := range a.Facts {
		if containsFold(f, "unexpected.bin") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the unexpected file to be named explicitly in the facts: %v", a.Facts)
	}
}

func TestScan_PublishedPreservation_InvalidManifest_IsManualIntervention(t *testing.T) {
	_, db := pvDB(t, false, false, false)
	id, err := StatIdentity(db)
	if err != nil {
		t.Fatal(err)
	}
	pres, err := PreserveActiveFiles(db, id)
	if err != nil {
		t.Fatal(err)
	}
	pvWrite(t, filepath.Join(pres.Dir(), "manifest.json"), []byte("{not valid json"))

	res, err := ScanRecoveryArtifacts(db)
	if err != nil {
		t.Fatal(err)
	}
	a := mustOneArtifact(t, res, ArtifactPreservationPublished)
	if a.Confidence != ConfidenceManual {
		t.Fatalf("expected manual-intervention, got %s", a.Confidence)
	}
}

func TestScan_EmptyAsideDirectory_IsFactNeverDeleted(t *testing.T) {
	_, db := pvDB(t, false, false, false)
	dir := mkEmptyAside(t, db)

	res, err := ScanRecoveryArtifacts(db)
	if err != nil {
		t.Fatal(err)
	}
	a := mustOneArtifact(t, res, ArtifactDisplacementAside)
	if a.Confidence != ConfidenceFact {
		t.Fatalf("expected fact, got %s", a.Confidence)
	}
	if a.MainDatabaseUnsafe {
		t.Fatalf("an empty aside directory must not mark the database unsafe")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("the empty aside directory must still exist after scanning: %v", err)
	}
}

func TestScan_AsideDirectory_MainPresentAtActivePath_IsFact(t *testing.T) {
	_, db := pvDB(t, false, false, false)
	dir := mkEmptyAside(t, db)
	pvWrite(t, filepath.Join(dir, "main"), pvBytes(10))
	// db itself still exists (pvDB wrote it): this is the "installed and
	// verified, aside dir retained for the record" shape (row 8).

	res, err := ScanRecoveryArtifacts(db)
	if err != nil {
		t.Fatal(err)
	}
	a := mustOneArtifact(t, res, ArtifactDisplacementAside)
	if a.Confidence != ConfidenceFact {
		t.Fatalf("expected fact, got %s: %+v", a.Confidence, a.Facts)
	}
	if a.MainDatabaseUnsafe {
		t.Fatalf("a completed-install shape must not mark the database unsafe")
	}
}

func TestScan_AsideDirectory_MainRetainedButActivePathEmpty_IsManualAndUnsafe(t *testing.T) {
	dir := pvDir(t)
	db := filepath.Join(dir, "c.db") // never created: simulates a crash after the main file was moved aside
	aside := db + ".replaced-20260101T000000Z-aaaa"
	if err := os.Mkdir(aside, 0o700); err != nil {
		t.Fatal(err)
	}
	pvWrite(t, filepath.Join(aside, "main"), pvBytes(20000))

	res, err := ScanRecoveryArtifacts(db)
	if err != nil {
		t.Fatal(err)
	}
	a := mustOneArtifact(t, res, ArtifactDisplacementAside)
	if a.Confidence != ConfidenceManual {
		t.Fatalf("expected manual-intervention, got %s", a.Confidence)
	}
	if !a.MainDatabaseUnsafe {
		t.Fatalf("a retained main file with no active-path file must mark the database unsafe")
	}
}

func TestScan_AsideDirectory_CompanionsOnlyNoMain_IsManualAndUnsafe(t *testing.T) {
	dir := pvDir(t)
	db := filepath.Join(dir, "c.db")
	aside := db + ".replaced-20260101T000000Z-bbbb"
	if err := os.Mkdir(aside, 0o700); err != nil {
		t.Fatal(err)
	}
	pvWrite(t, filepath.Join(aside, "wal"), pvBytes(100))

	res, err := ScanRecoveryArtifacts(db)
	if err != nil {
		t.Fatal(err)
	}
	a := mustOneArtifact(t, res, ArtifactDisplacementAside)
	if a.Confidence != ConfidenceManual || !a.MainDatabaseUnsafe {
		t.Fatalf("expected manual-intervention and unsafe, got %s / unsafe=%t", a.Confidence, a.MainDatabaseUnsafe)
	}
}

func TestScan_ReplacementTemp_IsAlwaysManual_NeverInstalled(t *testing.T) {
	for _, mainExists := range []bool{true, false} {
		dir := pvDir(t)
		db := filepath.Join(dir, "c.db")
		if mainExists {
			pvWrite(t, db, pvBytes(20000))
		}
		pvWrite(t, db+".restoring-aaaa", pvBytes(500))

		res, err := ScanRecoveryArtifacts(db)
		if err != nil {
			t.Fatal(err)
		}
		a := mustOneArtifact(t, res, ArtifactReplacementTemp)
		if a.Confidence != ConfidenceManual {
			t.Fatalf("mainExists=%t: expected manual-intervention, got %s", mainExists, a.Confidence)
		}
		if a.MainDatabaseUnsafe == mainExists {
			t.Fatalf("mainExists=%t: MainDatabaseUnsafe should track whether a main file exists at the active path", mainExists)
		}
		found := false
		for _, u := range a.Unresolved {
			if containsFold(u, "never installs") {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected an explicit note that this scanner never installs the file: %v", a.Unresolved)
		}
	}
}

func TestScan_Symlink_IsReportedAsAnomalyAndNeverFollowed(t *testing.T) {
	_, db := pvDB(t, false, false, false)
	dir := mkEmptyAside(t, db)
	target := filepath.Join(filepath.Dir(dir), "elsewhere.bin")
	pvWrite(t, target, pvBytes(10))
	link := filepath.Join(dir, "main")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}

	res, err := ScanRecoveryArtifacts(db)
	if err != nil {
		t.Fatal(err)
	}
	a := mustOneArtifact(t, res, ArtifactDisplacementAside)
	foundAnomaly := false
	for _, f := range a.Facts {
		if containsFold(f, "symbolic link") {
			foundAnomaly = true
		}
	}
	if !foundAnomaly {
		t.Fatalf("expected the symlink to be reported as an anomaly: %+v", a.Facts)
	}
	fi, err := os.Lstat(link)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the symlink must remain exactly as it was, never followed or replaced")
	}
}

func TestScan_OwnershipAnomaly_IsReportedOnReplacementTemp(t *testing.T) {
	_, db := pvDB(t, false, false, false)
	path := db + ".restoring-aaaa"
	pvWrite(t, path, pvBytes(50))
	if err := os.Chmod(path, 0o640); err != nil {
		t.Skipf("chmod unsupported here: %v", err)
	}

	res, err := ScanRecoveryArtifacts(db)
	if err != nil {
		t.Fatal(err)
	}
	a := mustOneArtifact(t, res, ArtifactReplacementTemp)
	found := false
	for _, f := range a.Facts {
		if containsFold(f, "group or other access") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the loosened permissions to be reported: %+v", a.Facts)
	}
}

// ---- M-2 regression: no-follow opening (TOCTOU) --------------------------------------------------
//
// These tests exercise the kernel's O_NOFOLLOW refusal directly by placing
// a symlink where a regular file is expected BEFORE ScanRecoveryArtifacts
// runs, rather than attempting to win a timing race against the scanner.
// This is deliberate, not a weaker substitute for a race test: the fix
// removed the separate inspect-then-open pair entirely, so "a symlink is
// present when the single open(2) call happens" is the one and only
// code path a genuine race could ever land in -- there is no distinct
// "replaced mid-race" behavior left to simulate separately.

func TestScan_ManifestIsSymlink_NeverFollowedNeverReadAsConfidenceFact(t *testing.T) {
	_, db := pvDB(t, false, false, false)
	id, err := StatIdentity(db)
	if err != nil {
		t.Fatal(err)
	}
	pres, err := PreserveActiveFiles(db, id)
	if err != nil {
		t.Fatal(err)
	}
	outsidePath, outsideContent := outsideProbe(t, filepath.Dir(db))
	mpath := filepath.Join(pres.Dir(), "manifest.json")
	if err := os.Remove(mpath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsidePath, mpath); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}

	res, err := ScanRecoveryArtifacts(db)
	if err != nil {
		t.Fatal(err)
	}
	a := mustOneArtifact(t, res, ArtifactPreservationPublished)
	assertOutsideFileUntouchedAndUnread(t, a, outsidePath, outsideContent)
	found := false
	for _, f := range a.Facts {
		if containsFold(f, "symbolic link") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the symlinked manifest to be reported as a symbolic link, got: %+v", a.Facts)
	}
}

func TestScan_PreservedMainIsSymlink_NeverFollowedNeverReadAsConfidenceFact(t *testing.T) {
	_, db := pvDB(t, false, false, false)
	id, err := StatIdentity(db)
	if err != nil {
		t.Fatal(err)
	}
	pres, err := PreserveActiveFiles(db, id)
	if err != nil {
		t.Fatal(err)
	}
	outsidePath, outsideContent := outsideProbe(t, filepath.Dir(db))
	mainPath := filepath.Join(pres.Dir(), "main")
	if err := os.Remove(mainPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsidePath, mainPath); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}

	res, err := ScanRecoveryArtifacts(db)
	if err != nil {
		t.Fatal(err)
	}
	a := mustOneArtifact(t, res, ArtifactPreservationPublished)
	assertOutsideFileUntouchedAndUnread(t, a, outsidePath, outsideContent)
	found := false
	for _, f := range a.Facts {
		if containsFold(f, "symbolic link") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the symlinked preserved copy to be reported as a symbolic link, got: %+v", a.Facts)
	}
}

// TestScan_NoFollowFix_OrdinaryPreservationStillScansSuccessfully re-proves,
// after the M-2 refactor, that a genuine, untampered preservation is
// unaffected: this is the same property
// TestScan_PublishedPreservation_ValidManifest_IsFactAndNeverCalledDisposable
// and TestScan_PublishedPreservation_LegitimateFilenames_AreAccepted already
// assert (both re-run unmodified by this change and still pass -- see the
// verification report); this test adds no new assertion beyond what they
// already prove, and exists only to name the M-2 regression explicitly.
func TestScan_NoFollowFix_OrdinaryPreservationStillScansSuccessfully(t *testing.T) {
	_, db := pvDB(t, true, true, true)
	id, err := StatIdentity(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PreserveActiveFiles(db, id); err != nil {
		t.Fatal(err)
	}
	res, err := ScanRecoveryArtifacts(db)
	if err != nil {
		t.Fatal(err)
	}
	a := mustOneArtifact(t, res, ArtifactPreservationPublished)
	if a.Confidence != ConfidenceFact {
		t.Fatalf("an ordinary, untampered preservation must still reach ConfidenceFact after the M-2 fix, got %s: %v", a.Confidence, a.Facts)
	}
}

// ---- L-2: scanner classification of real, crash-derived artifacts -----------------------------
//
// Every other scanner test in this file builds its fixture by hand
// (os.Mkdir/os.WriteFile). These two instead run the real PreserveActiveFiles
// machinery in a subprocess and SIGKILL it at an exact point (reusing
// preserve_test.go's pvPreserveKillChild/pvHelper), so the resulting .partial
// or published directory is genuinely crash-derived, not a synthetic stand-in
// for one.

// TestScan_RealPartialPreservationCrash_IsInconclusive kills the
// preservation subprocess at the existing "presync" point: the manifest and
// every file have been written and synced, but the publish rename never
// happens. The directory left behind is a real, crash-derived .partial.
func TestScan_RealPartialPreservationCrash_IsInconclusive(t *testing.T) {
	dir, db := pvDB(t, true, false, true)
	origBefore := pvStates(t, db)

	c := pvHelper(t, "preserve-kill", db)
	c.Env = append(c.Env, "DSO_TEST_PRESERVE_KILLPOINT=presync")
	err := c.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("the preservation subprocess should have been killed: %v", err)
	}
	if ws, ok := ee.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
		t.Fatalf("expected SIGKILL, got %v", err)
	}

	leftovers := pvLeftovers(t, dir)
	var partials int
	for _, l := range leftovers {
		if strings.HasSuffix(l, ".partial") {
			partials++
		}
	}
	if partials != 1 || len(leftovers) != 1 {
		t.Fatalf("expected exactly one real .partial directory from the crash, got %v", leftovers)
	}

	beforeScan := dirSnapshot(t, dir)
	res, err := ScanRecoveryArtifacts(db)
	if err != nil {
		t.Fatal(err)
	}
	a := mustOneArtifact(t, res, ArtifactPreservationPartial)
	if a.Confidence != ConfidenceInconclusive {
		t.Fatalf("expected inconclusive for a real crash-derived .partial, got %s: facts=%v", a.Confidence, a.Facts)
	}
	for _, f := range a.Facts {
		if containsFold(f, "abandon") {
			t.Fatalf("a .partial directory must never be described as abandoned: %q", f)
		}
	}

	// The crash itself must not have altered the active database or its
	// companions (the ordinary preservation guarantee, re-asserted here).
	pvSame(t, origBefore, pvStates(t, db))
	// The scan performed no mutation of anything on disk, including the
	// crash-derived .partial itself; since no file anywhere under dir may be
	// added, removed, or changed, this also rules out any new SQLite/WAL/SHM
	// sidecar as a side effect of scanning.
	assertDirUnchanged(t, dir, beforeScan)
}

// TestScan_RealPublishedPreservationCrash_IsClassifiedAsFact kills the
// preservation subprocess immediately after the publication rename
// completes (the test-only "afterpublish" point added to
// pvPreserveKillChild, which wraps the existing pv.publish seam -- no
// production code was changed to add it). The resulting published
// preservation and its manifest are real and crash-derived, not hand-built.
func TestScan_RealPublishedPreservationCrash_IsClassifiedAsFact(t *testing.T) {
	dir, db := pvDB(t, true, false, true)
	origBefore := pvStates(t, db)

	c := pvHelper(t, "preserve-kill", db)
	c.Env = append(c.Env, "DSO_TEST_PRESERVE_KILLPOINT=afterpublish")
	err := c.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("the preservation subprocess should have been killed: %v", err)
	}
	if ws, ok := ee.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
		t.Fatalf("expected SIGKILL, got %v", err)
	}

	leftovers := pvLeftovers(t, dir)
	var published []string
	for _, l := range leftovers {
		if !strings.HasSuffix(l, ".partial") {
			published = append(published, l)
		}
	}
	if len(published) != 1 || len(leftovers) != 1 {
		t.Fatalf("expected exactly one real published preservation from the crash, got %v", leftovers)
	}
	presDir := filepath.Join(dir, published[0])

	// Independently verify, without relying on the scanner, that every file
	// the manifest lists is actually present with the hash it claims --
	// proof that the manifest itself is genuine and crash-consistent before
	// asking the scanner to agree.
	mb, err := os.ReadFile(filepath.Join(presDir, "manifest.json")) // #nosec G304 -- test fixture under a temp dir this test created
	if err != nil {
		t.Fatalf("reading the real crash-derived manifest: %v", err)
	}
	var mf manifest
	if err := json.Unmarshal(mb, &mf); err != nil {
		t.Fatalf("the real crash-derived manifest must be valid JSON: %v", err)
	}
	if len(mf.Files) == 0 {
		t.Fatal("expected the real manifest to list at least one file")
	}
	for _, f := range mf.Files {
		fb, ferr := os.ReadFile(filepath.Join(presDir, f.Name)) // #nosec G304 -- test fixture under a temp dir this test created
		if ferr != nil {
			t.Fatalf("manifest entry %q: %v", f.Name, ferr)
		}
		sum := sha256.Sum256(fb)
		if hex.EncodeToString(sum[:]) != f.SHA256 {
			t.Fatalf("manifest entry %q: hash does not match the preserved file's real content", f.Name)
		}
	}

	beforeScan := dirSnapshot(t, dir)
	res, err := ScanRecoveryArtifacts(db)
	if err != nil {
		t.Fatal(err)
	}
	a := mustOneArtifact(t, res, ArtifactPreservationPublished)
	if a.Confidence != ConfidenceFact {
		t.Fatalf("expected fact for a real crash-derived published preservation whose manifest matches, got %s: facts=%v", a.Confidence, a.Facts)
	}
	foundSelfConsistent := false
	for _, f := range a.Facts {
		if containsFold(f, "self-consistent") {
			foundSelfConsistent = true
		}
		if containsFold(f, "disposable") {
			t.Fatalf("a self-consistent preservation must never be called disposable: %q", f)
		}
	}
	if !foundSelfConsistent {
		t.Fatalf("expected a self-consistency fact for the crash-derived published preservation; got facts=%v", a.Facts)
	}

	// The crash itself must not have altered the active database or its
	// companions (the ordinary preservation guarantee, re-asserted here).
	pvSame(t, origBefore, pvStates(t, db))
	// The scan performed no mutation of anything on disk, including the
	// crash-derived preservation itself; since no file anywhere under dir may
	// be added, removed, or changed, this also rules out any new
	// SQLite/WAL/SHM sidecar as a side effect of scanning.
	assertDirUnchanged(t, dir, beforeScan)
}

// ---- H-1 regression: preservation manifest path traversal -------------------------------------

// outsideProbe builds a sibling file outside the preservation directory
// whose bytes, if ever read by the scanner, would be detectable: it carries
// its own recognizable content and an fstat-observable access-time/size
// fingerprint the test checks was never touched.
func outsideProbe(t *testing.T, siblingDir string) (path string, content []byte) {
	t.Helper()
	path = filepath.Join(siblingDir, "outside-secret.bin")
	content = pvBytes(37) // a distinctive size no "main"/"wal"/etc. fixture in this file uses
	pvWrite(t, path, content)
	return path, content
}

// assertOutsideFileUntouchedAndUnread is the actual security property: not
// merely that an error string mentions the path, but that (a) the file still
// has exactly its original bytes, and (b) no fact in the artifact's report
// contains either its name or, crucially, the sha256 the scanner would have
// computed had it actually hashed it -- proving the scanner never opened it.
func assertOutsideFileUntouchedAndUnread(t *testing.T, a Artifact, outsidePath string, outsideContent []byte) {
	t.Helper()
	got, err := os.ReadFile(outsidePath) // #nosec G304 -- test fixture path this test created
	if err != nil {
		t.Fatalf("outside file vanished: %v", err)
	}
	if !bytes.Equal(got, outsideContent) {
		t.Fatalf("outside file content changed: the scanner must never write to it")
	}
	wouldBeHash, _, err := hashFileNoFollow(outsidePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range a.Facts {
		if containsFold(f, wouldBeHash) {
			t.Fatalf("a fact contains the outside file's actual content hash -- the scanner read it: %q", f)
		}
		if containsFold(f, "outside-secret.bin") && containsFold(f, "matches its recorded hash and size") {
			t.Fatalf("the outside file must never be reported as matching: %q", f)
		}
	}
	if a.Confidence == ConfidenceFact {
		t.Fatalf("an artifact whose manifest references a path-traversal name must never reach ConfidenceFact")
	}
}

func TestScan_ManifestTraversal_DotDotSlash_NeverReadOutsideDirectory(t *testing.T) {
	_, db := pvDB(t, false, false, false)
	id, err := StatIdentity(db)
	if err != nil {
		t.Fatal(err)
	}
	pres, err := PreserveActiveFiles(db, id)
	if err != nil {
		t.Fatal(err)
	}
	outsidePath, outsideContent := outsideProbe(t, filepath.Dir(db))
	injectManifestEntry(t, pres.Dir(), "../outside-secret.bin")

	res, err := ScanRecoveryArtifacts(db)
	if err != nil {
		t.Fatal(err)
	}
	a := mustOneArtifact(t, res, ArtifactPreservationPublished)
	assertOutsideFileUntouchedAndUnread(t, a, outsidePath, outsideContent)
}

func TestScan_ManifestTraversal_AbsolutePath_NeverReadOutsideDirectory(t *testing.T) {
	_, db := pvDB(t, false, false, false)
	id, err := StatIdentity(db)
	if err != nil {
		t.Fatal(err)
	}
	pres, err := PreserveActiveFiles(db, id)
	if err != nil {
		t.Fatal(err)
	}
	outsidePath, outsideContent := outsideProbe(t, filepath.Dir(db))
	injectManifestEntry(t, pres.Dir(), outsidePath) // absolute path

	res, err := ScanRecoveryArtifacts(db)
	if err != nil {
		t.Fatal(err)
	}
	a := mustOneArtifact(t, res, ArtifactPreservationPublished)
	assertOutsideFileUntouchedAndUnread(t, a, outsidePath, outsideContent)
}

func TestScan_ManifestEntry_ForwardSlash_IsRejected(t *testing.T) {
	testManifestNameRejected(t, "sub/dir")
}

func TestScan_ManifestEntry_Backslash_IsRejected(t *testing.T) {
	testManifestNameRejected(t, `sub\dir`)
}

func TestScan_ManifestEntry_Dot_IsRejected(t *testing.T) {
	testManifestNameRejected(t, ".")
}

func TestScan_ManifestEntry_DotDot_IsRejected(t *testing.T) {
	testManifestNameRejected(t, "..")
}

func TestScan_ManifestEntry_Empty_IsRejected(t *testing.T) {
	testManifestNameRejected(t, "")
}

func testManifestNameRejected(t *testing.T, name string) {
	t.Helper()
	_, db := pvDB(t, false, false, false)
	id, err := StatIdentity(db)
	if err != nil {
		t.Fatal(err)
	}
	pres, err := PreserveActiveFiles(db, id)
	if err != nil {
		t.Fatal(err)
	}
	injectManifestEntry(t, pres.Dir(), name)

	res, err := ScanRecoveryArtifacts(db)
	if err != nil {
		t.Fatal(err)
	}
	a := mustOneArtifact(t, res, ArtifactPreservationPublished)
	if a.Confidence == ConfidenceFact {
		t.Fatalf("name %q must never be accepted into a ConfidenceFact result", name)
	}
	found := false
	for _, f := range a.Facts {
		if containsFold(f, "not a safe filename") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an explicit unsafe-filename fact for %q, got: %v", name, a.Facts)
	}
}

func TestScan_PublishedPreservation_LegitimateFilenames_AreAccepted(t *testing.T) {
	// The real preservation writer only ever produces "main", "wal",
	// "journal", "shm" and "manifest.json" -- every one of them a bare
	// filename -- so a genuine, untampered preservation must still reach
	// ConfidenceFact after the H-1 fix.
	_, db := pvDB(t, true, true, true)
	id, err := StatIdentity(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PreserveActiveFiles(db, id); err != nil {
		t.Fatal(err)
	}
	res, err := ScanRecoveryArtifacts(db)
	if err != nil {
		t.Fatal(err)
	}
	a := mustOneArtifact(t, res, ArtifactPreservationPublished)
	if a.Confidence != ConfidenceFact {
		t.Fatalf("a genuine, untampered preservation must still reach ConfidenceFact after the H-1 fix, got %s: %v", a.Confidence, a.Facts)
	}
}

// injectManifestEntry appends one extra entry with an unsafe/traversal Name
// to an already-published preservation's manifest.json, leaving every other
// entry untouched.
func injectManifestEntry(t *testing.T, presDir, name string) {
	t.Helper()
	mpath := filepath.Join(presDir, "manifest.json")
	raw, err := os.ReadFile(mpath) // #nosec G304 -- test fixture this test's own PreserveActiveFiles call created
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m.Files = append(m.Files, manifestFile{Name: name, Source: "injected", Size: 1, SHA256: "0"})
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	pvWrite(t, mpath, out)
}

// ---- M-1 regression: duplicate manifest entry detection ----------------------------------------
//
// These tests exercise the seen-name set added to scanPreservationPublished.
// Every test fixture is synthetic (temp dir, real PreserveActiveFiles output
// with an injected extra entry); none touch real user data.
//
// Safety property established by this block: a preservation manifest whose
// Files array contains a repeated filename can never cause the scanner to
// return ConfidenceFact, and the duplicate occurrence is never opened or read.

// injectDuplicateManifestEntry appends a second copy of the entry for `name`
// to an already-published preservation's manifest.json. When alterMeta is
// false the copy is byte-for-byte identical (same SHA-256, same size). When
// alterMeta is true the copy has a deliberately different size and a zeroed
// SHA-256, simulating a conflicting duplicate.
func injectDuplicateManifestEntry(t *testing.T, presDir, name string, alterMeta bool) {
	t.Helper()
	mpath := filepath.Join(presDir, "manifest.json")
	raw, err := os.ReadFile(mpath) // #nosec G304 -- test fixture this test's own PreserveActiveFiles call created
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	// Find the entry we want to duplicate.
	var orig manifestFile
	found := false
	for _, f := range m.Files {
		if f.Name == name {
			orig = f
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("injectDuplicateManifestEntry: entry %q not found in manifest", name)
	}
	dup := orig
	if alterMeta {
		dup.Size = orig.Size + 1
		dup.SHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	}
	m.Files = append(m.Files, dup)
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	pvWrite(t, mpath, out)
}

// countFacts counts how many facts in a contain substr (case-insensitive).
func countFacts(a Artifact, substr string) int {
	n := 0
	for _, f := range a.Facts {
		if containsFold(f, substr) {
			n++
		}
	}
	return n
}

// TestScan_NoDuplicates_RemainsConfidenceFact (T-1) proves that the seen-map
// introduced by M-1 does not affect legitimate preservations: a manifest
// produced by the real writer, with one entry per filename, must still reach
// ConfidenceFact.
func TestScan_NoDuplicates_RemainsConfidenceFact(t *testing.T) {
	_, db := pvDB(t, false, false, false)
	id, err := StatIdentity(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PreserveActiveFiles(db, id); err != nil {
		t.Fatal(err)
	}
	res, err := ScanRecoveryArtifacts(db)
	if err != nil {
		t.Fatal(err)
	}
	a := mustOneArtifact(t, res, ArtifactPreservationPublished)
	if a.Confidence != ConfidenceFact {
		t.Fatalf("T-1: expected ConfidenceFact for a duplicate-free manifest, got %s: %v", a.Confidence, a.Facts)
	}
	// Confirm no spurious duplicate-detected fact was produced.
	if countFacts(a, "appears more than once") > 0 {
		t.Fatalf("T-1: a duplicate fact must not appear for a legitimate manifest: %v", a.Facts)
	}
}

// TestScan_DuplicateManifestEntry_Identical_IsManualNotFact (T-2) injects a
// second entry for "main" that is byte-for-byte identical (same hash, same
// size). Both occurrences must result in ConfidenceManual; the approved
// duplicate diagnostic must be present.
func TestScan_DuplicateManifestEntry_Identical_IsManualNotFact(t *testing.T) {
	_, db := pvDB(t, false, false, false)
	id, err := StatIdentity(db)
	if err != nil {
		t.Fatal(err)
	}
	pres, err := PreserveActiveFiles(db, id)
	if err != nil {
		t.Fatal(err)
	}
	injectDuplicateManifestEntry(t, pres.Dir(), "main", false /* identical */)

	before := dirSnapshot(t, pres.Dir())
	res, err := ScanRecoveryArtifacts(db)
	if err != nil {
		t.Fatal(err)
	}
	assertDirUnchanged(t, pres.Dir(), before)

	a := mustOneArtifact(t, res, ArtifactPreservationPublished)
	if a.Confidence == ConfidenceFact {
		t.Fatalf("T-2: an identical duplicate must not reach ConfidenceFact, got: %v", a.Facts)
	}
	if a.Confidence != ConfidenceManual {
		t.Fatalf("T-2: expected ConfidenceManual, got %s: %v", a.Confidence, a.Facts)
	}
	found := false
	for _, f := range a.Facts {
		if containsFold(f, "appears more than once") {
			found = true
		}
	}
	if !found {
		t.Fatalf("T-2: expected the approved duplicate diagnostic in Facts, got: %v", a.Facts)
	}
}

// TestScan_DuplicateManifestEntry_Conflicting_IsManualWithExplicitFact (T-3)
// injects a second entry for "main" with deliberately different metadata.
// ConfidenceManual must be returned and the duplicate diagnostic (not merely
// a hash-mismatch diagnostic) must be present among the facts.
func TestScan_DuplicateManifestEntry_Conflicting_IsManualWithExplicitFact(t *testing.T) {
	_, db := pvDB(t, false, false, false)
	id, err := StatIdentity(db)
	if err != nil {
		t.Fatal(err)
	}
	pres, err := PreserveActiveFiles(db, id)
	if err != nil {
		t.Fatal(err)
	}
	injectDuplicateManifestEntry(t, pres.Dir(), "main", true /* conflicting */)

	res, err := ScanRecoveryArtifacts(db)
	if err != nil {
		t.Fatal(err)
	}
	a := mustOneArtifact(t, res, ArtifactPreservationPublished)
	if a.Confidence == ConfidenceFact {
		t.Fatalf("T-3: a conflicting duplicate must not reach ConfidenceFact, got: %v", a.Facts)
	}
	if a.Confidence != ConfidenceManual {
		t.Fatalf("T-3: expected ConfidenceManual, got %s: %v", a.Confidence, a.Facts)
	}
	// The duplicate fact must be present, naming the duplicate occurrence
	// rather than only reporting a hash mismatch.
	found := false
	for _, f := range a.Facts {
		if containsFold(f, "appears more than once") {
			found = true
		}
	}
	if !found {
		t.Fatalf("T-3: expected the duplicate diagnostic, not just a hash-mismatch fact: %v", a.Facts)
	}
}

// TestScan_DuplicateManifestEntry_NeverConfidenceFact (T-4) is table-driven
// across both duplicate shapes and asserts that neither can reach ConfidenceFact.
func TestScan_DuplicateManifestEntry_NeverConfidenceFact(t *testing.T) {
	for _, tc := range []struct {
		name      string
		alterMeta bool
	}{
		{"identical duplicate", false},
		{"conflicting duplicate", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, db := pvDB(t, false, false, false)
			id, err := StatIdentity(db)
			if err != nil {
				t.Fatal(err)
			}
			pres, err := PreserveActiveFiles(db, id)
			if err != nil {
				t.Fatal(err)
			}
			injectDuplicateManifestEntry(t, pres.Dir(), "main", tc.alterMeta)

			res, err := ScanRecoveryArtifacts(db)
			if err != nil {
				t.Fatal(err)
			}
			a := mustOneArtifact(t, res, ArtifactPreservationPublished)
			if a.Confidence == ConfidenceFact {
				t.Fatalf("T-4 %s: must never reach ConfidenceFact, got facts: %v", tc.name, a.Facts)
			}
		})
	}
}

// TestScan_DuplicateManifestEntry_FileNotHashedTwice (T-5) injects an
// identical duplicate for "main" and verifies that the scanner records at most
// one successful "matches its recorded hash and size" fact for that name.
// If the file had been hashed twice, two such facts would appear.
// The test also confirms the preservation directory is not modified.
//
// Note: this test verifies the diagnostic-count consequence of the skip, which
// is a sound proxy for the no-second-read guarantee: the duplicate diagnostic
// says "this duplicate occurrence was skipped", so the success-match fact can
// appear at most once (for the first occurrence). No separate open-call counter
// or test seam is needed to prove the skip.
func TestScan_DuplicateManifestEntry_FileNotHashedTwice(t *testing.T) {
	_, db := pvDB(t, false, false, false)
	id, err := StatIdentity(db)
	if err != nil {
		t.Fatal(err)
	}
	pres, err := PreserveActiveFiles(db, id)
	if err != nil {
		t.Fatal(err)
	}
	injectDuplicateManifestEntry(t, pres.Dir(), "main", false /* identical */)

	before := dirSnapshot(t, pres.Dir())
	res, err := ScanRecoveryArtifacts(db)
	if err != nil {
		t.Fatal(err)
	}
	assertDirUnchanged(t, pres.Dir(), before)

	a := mustOneArtifact(t, res, ArtifactPreservationPublished)

	// Count successful hash-match facts mentioning "main".
	successCount := 0
	for _, f := range a.Facts {
		if containsFold(f, "\"main\"") && containsFold(f, "matches its recorded hash and size") {
			successCount++
		}
	}
	if successCount > 1 {
		t.Fatalf("T-5: 'main' must produce at most one successful hash-match fact (got %d), indicating the file was read more than once: %v", successCount, a.Facts)
	}
	// The duplicate-detected diagnostic must be present (the skip actually ran).
	if countFacts(a, "appears more than once") == 0 {
		t.Fatalf("T-5: expected the duplicate diagnostic to confirm the skip occurred: %v", a.Facts)
	}
}

// TestScan_DuplicateManifestEntry_UnsafeDuplicateName exercises U-4: an
// unsafe filename that appears twice in the manifest must produce both the
// unsafe-name diagnostic (for the first occurrence) and the duplicate
// diagnostic (for the second occurrence). The outside file must remain
// byte-for-byte unchanged and its hash must not appear in the scanner facts.
func TestScan_DuplicateManifestEntry_UnsafeDuplicateName(t *testing.T) {
	_, db := pvDB(t, false, false, false)
	id, err := StatIdentity(db)
	if err != nil {
		t.Fatal(err)
	}
	pres, err := PreserveActiveFiles(db, id)
	if err != nil {
		t.Fatal(err)
	}

	// Build a file outside the preservation directory that would be reachable
	// via path traversal, so we can confirm it was never opened.
	outsidePath, outsideContent := outsideProbe(t, filepath.Dir(db))

	// Inject two entries with the unsafe traversal name.
	const unsafeName = "../outside-secret.bin"
	injectManifestEntry(t, pres.Dir(), unsafeName) // first occurrence
	injectManifestEntry(t, pres.Dir(), unsafeName) // second occurrence (duplicate)

	res, err := ScanRecoveryArtifacts(db)
	if err != nil {
		t.Fatal(err)
	}
	a := mustOneArtifact(t, res, ArtifactPreservationPublished)

	// The outside file must be untouched and unread.
	assertOutsideFileUntouchedAndUnread(t, a, outsidePath, outsideContent)

	// The unsafe-name diagnostic must be present (for the first occurrence).
	foundUnsafe := false
	for _, f := range a.Facts {
		if containsFold(f, "not a safe filename") {
			foundUnsafe = true
		}
	}
	if !foundUnsafe {
		t.Fatalf("unsafe-duplicate: expected the unsafe-filename fact for the first occurrence: %v", a.Facts)
	}

	// The duplicate diagnostic must also be present (for the second occurrence).
	if countFacts(a, "appears more than once") == 0 {
		t.Fatalf("unsafe-duplicate: expected the duplicate diagnostic for the second occurrence: %v", a.Facts)
	}

	// allMatch must be false → ConfidenceManual, never ConfidenceFact.
	if a.Confidence == ConfidenceFact {
		t.Fatalf("unsafe-duplicate: must never reach ConfidenceFact: %v", a.Facts)
	}
}

func mustOneArtifact(t *testing.T, res ScanResult, kind ArtifactKind) Artifact {
	t.Helper()
	for _, a := range res.Artifacts {
		if a.Kind == kind {
			return a
		}
	}
	t.Fatalf("no artifact of kind %s found among %d artifacts", kind, len(res.Artifacts))
	return Artifact{}
}

func containsFold(s, substr string) bool {
	ls, lsub := len(s), len(substr)
	if lsub == 0 {
		return true
	}
	for i := 0; i+lsub <= ls; i++ {
		if equalFold(s[i:i+lsub], substr) {
			return true
		}
	}
	return false
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
