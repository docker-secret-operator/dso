package centralsvc

// Restore-level tests for the displacement flow (sqlite/displace.go): what a
// restore reports about the original files it moved aside, where the snapshot
// flow's baseline is taken, and what happens when the installation is blocked.
// The displacement itself is tested exhaustively, with fault injection and real
// crashes, in internal/store/sqlite/displace_test.go.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/internal/store"
	"github.com/docker-secret-operator/dso/internal/store/sqlite"
)

// replaceWithDisplacement is the displacement-based counterpart of the legacy
// replaceDatabaseFile, for tests of the replacement on its own: it prepares the
// replacement, takes the baseline of the existing database, and replaces it.
func replaceWithDisplacement(lock *sqlite.RestoreLock, dbPath, candidatePath string) error {
	repl, err := lock.PrepareReplacement(dbPath, candidatePath)
	if err != nil {
		return err
	}
	defer func() { _ = repl.Abandon() }()
	baseline, err := lock.BaselineAfterClose(dbPath)
	if err != nil {
		return err
	}
	_, err = lock.ReplaceActiveDatabase(dbPath, repl, baseline)
	return err
}

func TestRestoreDisplace_NoOpenFlow_ReportsEveryRetainedOriginal(t *testing.T) {
	t.Parallel()
	cand, kp, _ := riCandidate(t)
	db := riMismatch(t)
	mainHash := icFileHash(t, db)
	r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
		o.ConfirmAfterPreservation = func(RestorePlan, PreservationInfo) bool { return true }
	})
	if r.err != nil {
		t.Fatal(r.err)
	}
	riAssertRestored(t, db, kp)
	if r.res.DisplacedDir == "" || len(r.res.DisplacedFiles) == 0 {
		t.Fatalf("the retained originals must be reported: %+v", r.res)
	}
	var found bool
	for _, f := range r.res.DisplacedFiles {
		if f.Name == "main" {
			found = true
			if f.Original != db || filepath.Dir(f.Retained) != r.res.DisplacedDir {
				t.Fatalf("main: %+v (dir %s)", f, r.res.DisplacedDir)
			}
			if icFileHash(t, f.Retained) != mainHash {
				t.Fatal("the retained main file is not the original, byte for byte")
			}
		}
	}
	if !found {
		t.Fatalf("the main file is not reported: %+v", r.res.DisplacedFiles)
	}
	if len(riPreservations(t, filepath.Dir(db))) != 1 {
		t.Fatal("the raw preservation must remain next to the displaced originals")
	}
}

func TestRestoreDisplace_SnapshotFlow_BaselineIsTakenAfterTheSnapshotAndClose(t *testing.T) {
	t.Parallel()
	cand, kp, _ := riCandidate(t)
	db := riOlderSchema(t) // opening it migrates it, so SQLite changes the file
	var preOpen, postClose string
	r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
		real := o.testHook
		o.testHook = func(s string) {
			real(s)
			switch s {
			case stageOpenOriginal:
				preOpen = icFileHash(t, db)
			case stagePreJournalGate: // after the snapshot, the checkpoint and Close
				postClose = icFileHash(t, db)
			}
		}
	})
	if r.err != nil {
		t.Fatal(r.err)
	}
	if preOpen == "" || postClose == "" {
		t.Fatalf("the hooks did not run: stages %v", r.stages)
	}
	if preOpen == postClose {
		t.Fatalf("the fixture cannot show the difference: opening the older schema left the main file unchanged")
	}
	var retained string
	for _, f := range r.res.DisplacedFiles {
		if f.Name == "main" {
			retained = f.Retained
		}
	}
	if retained == "" {
		t.Fatalf("the main file is not reported: %+v", r.res.DisplacedFiles)
	}
	// The retained original is the state after SQLite's own changes, which is what the
	// baseline was taken from. Had the baseline been the pre-snapshot state, the
	// pre-install tripwire would have refused this restore.
	if got := icFileHash(t, retained); got != postClose || got == preOpen {
		t.Fatalf("retained main hash %s, after-Close %s, before-Open %s", got, postClose, preOpen)
	}
	riAssertRestored(t, db, kp)
}

func TestDisplacedNote(t *testing.T) {
	t.Parallel()
	if got := displacedNote(RestoreResult{}); got != "" {
		t.Fatalf("nothing was displaced, so there is nothing to say: %q", got)
	}
	got := displacedNote(RestoreResult{DisplacedDir: "/data/db.replaced-20260101T000000Z-aabbccdd"})
	for _, want := range []string{"/data/db.replaced-20260101T000000Z-aabbccdd", "moved aside by this restore", "remain", "nothing was deleted"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the note must contain %q: %q", want, got)
		}
	}
}

func TestRestoreDisplace_ReopenFailure_NamesTheDisplacedDirectory(t *testing.T) {
	t.Parallel()
	reopenFails := func(o *RestoreOptions) {
		o.testReopen = func(context.Context, string) (*sqlite.Store, error) {
			return nil, errors.New("reopen exploded")
		}
	}
	yes := func(o *RestoreOptions) {
		o.ConfirmAfterPreservation = func(RestorePlan, PreservationInfo) bool { return true }
	}
	t.Run("raw preservation only (an R1 flow)", func(t *testing.T) {
		t.Parallel()
		cand, kp, _ := riCandidate(t)
		db := riMismatch(t)
		r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) { yes(o); reopenFails(o) })
		if r.err == nil || r.res.DisplacedDir == "" || r.res.RawPreservation == "" {
			t.Fatalf("err %v res %+v", r.err, r.res)
		}
		for _, want := range []string{"WAS ACTIVATED", r.res.RawPreservation, r.res.DisplacedDir, "nothing was deleted"} {
			if !strings.Contains(r.err.Error(), want) {
				t.Fatalf("the message must contain %q: %v", want, r.err)
			}
		}
	})
	t.Run("safety snapshot (an R3 flow)", func(t *testing.T) {
		t.Parallel()
		cand, kp, _ := riCandidate(t)
		db := riOlderSchema(t)
		r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) { reopenFails(o) })
		if r.err == nil || r.res.DisplacedDir == "" || r.res.SafetySnapshot == "" {
			t.Fatalf("err %v res %+v", r.err, r.res)
		}
		for _, want := range []string{"WAS ACTIVATED", r.res.SafetySnapshot, r.res.DisplacedDir} {
			if !strings.Contains(r.err.Error(), want) {
				t.Fatalf("the message must contain %q: %v", want, r.err)
			}
		}
	})
	t.Run("neither (a missing database with an empty companion)", func(t *testing.T) {
		t.Parallel()
		cand, kp, _ := riCandidate(t)
		db := riMissing(t)
		if err := os.WriteFile(db+"-wal", nil, 0o600); err != nil {
			t.Fatal(err)
		}
		r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) { reopenFails(o) })
		if r.err == nil || r.res.DisplacedDir == "" || r.res.RawPreservation != "" || r.res.SafetySnapshot != "" {
			t.Fatalf("err %v res %+v", r.err, r.res)
		}
		for _, want := range []string{"reopening restored database", "reopen exploded", r.res.DisplacedDir} {
			if !strings.Contains(r.err.Error(), want) {
				t.Fatalf("the message must contain %q: %v", want, r.err)
			}
		}
		if _, err := os.Lstat(filepath.Join(r.res.DisplacedDir, "wal")); err != nil {
			t.Fatalf("the empty companion must be retained in the displaced directory: %v", err)
		}
	})
}

func TestDisplacedNote_DistinguishesTheFlows(t *testing.T) {
	t.Parallel()
	dir := "/data/db.replaced-20260101T000000Z-aabbccdd"
	plain := displacedNote(RestoreResult{DisplacedDir: dir})
	for _, want := range []string{"original database files", "the main file and any -wal, -shm and -journal", dir} {
		if !strings.Contains(plain, want) {
			t.Fatalf("the note must contain %q: %q", want, plain)
		}
	}
	for _, no := range []string{"opened and closed", "byte-for-byte"} {
		if strings.Contains(plain, no) {
			t.Fatalf("a flow with neither a snapshot nor a raw preservation must not say %q: %q", no, plain)
		}
	}
	snap := displacedNote(RestoreResult{DisplacedDir: dir, SafetySnapshot: "/data/db.pre-restore-x.bak"})
	if !strings.Contains(snap, "SQLite had opened and closed the database first") || !strings.Contains(snap, "its state after that close") {
		t.Fatalf("the snapshot flow must say the retained main file is its post-close state: %q", snap)
	}
	raw := displacedNote(RestoreResult{DisplacedDir: dir, RawPreservation: "/data/db.pre-restore-raw-x"})
	if !strings.Contains(raw, "byte-for-byte preservation from before the restore is separate, at /data/db.pre-restore-raw-x") {
		t.Fatalf("a raw preservation is a separate directory and must be named as such: %q", raw)
	}
	if strings.Contains(raw, "opened and closed") {
		t.Fatalf("no snapshot, so no snapshot-flow wording: %q", raw)
	}
}

func TestVanishedFiles_AreNamedByOriginalPath_AndAnAbsentFileIsNeverListed(t *testing.T) {
	t.Parallel()
	db := "/data/c.db"
	got := vanishedPaths(db, []string{"wal", "shm", "journal", "main", "unknown"})
	want := []string{db + "-wal", db + "-shm", db + "-journal", db}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if vanishedPaths(db, nil) != nil {
		t.Fatal("nothing vanished: nothing may be listed")
	}
	if vanishedNote(RestoreResult{}) != "" {
		t.Fatal("no note when nothing vanished")
	}
	one := vanishedNote(RestoreResult{VanishedFiles: []string{db + "-wal"}})
	for _, w := range []string{db + "-wal", "vanished during the replacement", "it is neither retained nor restored"} {
		if !strings.Contains(one, w) {
			t.Fatalf("the note must contain %q: %q", w, one)
		}
	}
	many := vanishedNote(RestoreResult{VanishedFiles: []string{db + "-wal", db + "-journal"}})
	if !strings.Contains(many, "they are neither retained nor restored") {
		t.Fatalf("plural wording: %q", many)
	}
}

// A restore of a database whose companions are simply absent must not list them as
// vanished: absent is not vanished.
func TestRestoreDisplace_AlreadyAbsentCompanions_AreNotReportedAsVanished(t *testing.T) {
	t.Parallel()
	cand, kp, _ := riCandidate(t)
	db := riMismatch(t) // a closed database with no companion files at all
	r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
		o.ConfirmAfterPreservation = func(RestorePlan, PreservationInfo) bool { return true }
	})
	if r.err != nil {
		t.Fatal(r.err)
	}
	if len(r.res.VanishedFiles) != 0 {
		t.Fatalf("files that were already absent must not be reported as vanished: %v", r.res.VanishedFiles)
	}
}

func TestSnapshotNote_NeverPrintsAnEmptySnapshotPath(t *testing.T) {
	t.Parallel()
	if got := snapshotNote(RestoreResult{}); got != "" {
		t.Fatalf("no snapshot was taken, so there is nothing to say: %q", got)
	}
	if got := snapshotNote(RestoreResult{SafetySnapshot: "/data/x.bak"}); got != " (safety snapshot at /data/x.bak)" {
		t.Fatalf("got %q", got)
	}
}

// TestRestoreDisplace_FailureMessages_AreDerivedFromTheStructuredResult drives
// Restore with displacement results of each shape and asserts the text the operator
// actually receives. The results are injected through the replacement seam, so the
// classification and wording inside Restore are what is under test.
func TestRestoreDisplace_FailureMessages_AreDerivedFromTheStructuredResult(t *testing.T) {
	t.Parallel()
	injected := errors.New("injected failure")
	run := func(t *testing.T, build func(db string) *sqlite.DisplacementError) (*riRun, string) {
		t.Helper()
		cand, kp, _ := riCandidate(t)
		db := riMismatch(t)
		r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
			o.ConfirmAfterPreservation = func(RestorePlan, PreservationInfo) bool { return true }
			o.testReplaceDatabaseFile = func(string, string) error { return build(db) }
		})
		if r.err == nil {
			t.Fatal("the failure must be returned")
		}
		return r, db
	}
	type check struct {
		name         string
		build        func(db string) *sqlite.DisplacementError
		want, wantNo []string
	}
	cases := []check{
		{
			name: "main database restored, companion unknown",
			build: func(db string) *sqlite.DisplacementError {
				return &sqlite.DisplacementError{Stage: "move-wal", Err: injected, Main: sqlite.MainAtOriginal,
					Unknown: []string{db + "-wal"}, UnknownDetail: []string{db + "-wal (at its original path: nothing there; at x: nothing there)"}}
			},
			want: []string{"could not all be put back", "the original main database file is back at ", "do not use that database until they are located",
				"1 file(s) could not be located", "nothing was deleted or overwritten"},
			wantNo: []string{"holds no active database", "retained", "is NOT at"},
		},
		{
			name: "main database unknown",
			build: func(db string) *sqlite.DisplacementError {
				return &sqlite.DisplacementError{Stage: "move-main", Err: injected, Main: sqlite.MainUnknown,
					Unknown: []string{db}, UnknownDetail: []string{db + " (at its original path: nothing there; at x: nothing there)"}}
			},
			want:   []string{"could not all be put back", "the original main database file could not be located", "is not known to hold it"},
			wantNo: []string{"holds no active database", "retained", "back at"},
		},
		{
			name: "files retained in the displaced directory",
			build: func(db string) *sqlite.DisplacementError {
				return &sqlite.DisplacementError{Stage: "install", Err: injected, Main: sqlite.MainRetained, Dir: "/x/aside",
					Retained: []sqlite.DisplacedFile{{Name: "main", Original: db, Retained: "/x/aside/main"}, {Name: "wal", Original: db + "-wal", Retained: "/x/aside/wal"}}}
			},
			want: []string{"retained in /x/aside", "main: ", "-> /x/aside/main", "the original main database file is retained at /x/aside/main and is NOT at ",
				"was not put there by this restore and was not touched"},
			wantNo: []string{"holds no active database"},
		},
		{
			name: "fully successful rollback",
			build: func(db string) *sqlite.DisplacementError {
				return &sqlite.DisplacementError{Stage: "install", Err: injected, Main: sqlite.MainAtOriginal, RolledBack: true}
			},
			want:   []string{"replacing active database failed before activation", "was not replaced", "rolled back: every original file is back at its original path"},
			wantNo: []string{"could not all be put back", "NOT rolled back"},
		},
		{
			name: "mixed outcomes",
			build: func(db string) *sqlite.DisplacementError {
				return &sqlite.DisplacementError{Stage: "move-shm", Err: injected, Main: sqlite.MainRetained, Dir: "/x/aside",
					Retained:        []sqlite.DisplacedFile{{Name: "main", Original: db, Retained: "/x/aside/main"}},
					RestoredInPlace: []string{db + "-wal"}, Unknown: []string{db + "-journal"}, Vanished: []string{db + "-shm"}}
			},
			want: []string{"could not all be put back", "1 file(s) vanished during the restore and are neither retained nor restored",
				"1 file(s) could not be located", "1 companion file(s) were restored and could not be displaced again", "retained in /x/aside"},
			wantNo: []string{"holds no active database"},
		},
		{
			name: "main database vanished before the call (a baseline-stage failure)",
			build: func(db string) *sqlite.DisplacementError {
				return &sqlite.DisplacementError{Stage: "baseline", Err: injected, Main: sqlite.MainVanished, Vanished: []string{db}}
			},
			want:   []string{"could not all be put back", "the original main database file vanished before it could be moved (see above) and is not at ", "1 file(s) vanished during the restore"},
			wantNo: []string{"holds no active database", "rolled back: every original", "was not replaced"},
		},
		{
			name: "a vanished companion with the main file in place",
			build: func(db string) *sqlite.DisplacementError {
				return &sqlite.DisplacementError{Stage: "move-shm", Err: injected, Main: sqlite.MainAtOriginal, Vanished: []string{db + "-wal"}}
			},
			want:   []string{"was not replaced", "vanished before it could be moved, so it is neither retained nor restored"},
			wantNo: []string{"could not all be put back", "holds no active database"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			r, _ := run(t, c.build)
			msg := r.err.Error()
			for _, w := range c.want {
				if !strings.Contains(msg, w) {
					t.Errorf("the operator-facing message must contain %q:\n%s", w, msg)
				}
			}
			for _, w := range c.wantNo {
				if strings.Contains(msg, w) {
					t.Errorf("the operator-facing message must NOT contain %q:\n%s", w, msg)
				}
			}
			if !errors.Is(r.err, injected) {
				t.Errorf("the cause must stay reachable through errors.Is: %v", r.err)
			}
			var got *sqlite.DisplacementError
			if !errors.As(r.err, &got) {
				t.Errorf("the structured error must survive wrapping through Restore: %v", r.err)
			}
		})
	}
	t.Run("vanished information survives wrapping through Restore", func(t *testing.T) {
		t.Parallel()
		r, db := run(t, func(db string) *sqlite.DisplacementError {
			return &sqlite.DisplacementError{Stage: "move-shm", Err: injected, Main: sqlite.MainAtOriginal, Vanished: []string{db + "-wal"}}
		})
		var got *sqlite.DisplacementError
		if !errors.As(r.err, &got) || len(got.Vanished) != 1 || got.Vanished[0] != db+"-wal" || got.RolledBack {
			t.Fatalf("the vanished file must be in the structured error that Restore returns, and RolledBack must be false: %+v", got)
		}
		if !strings.Contains(r.err.Error(), db+"-wal") {
			t.Fatalf("the vanished file must be named in the message: %v", r.err)
		}
	})
}

// With a vanished companion the main file is in place, but the safety snapshot must
// not be called "not needed": files that were part of the database are gone.
func TestRestoreDisplace_SnapshotFlow_VanishedCompanion_KeepsTheSnapshotGuidance(t *testing.T) {
	t.Parallel()
	injected := errors.New("injected failure")
	cand, kp, _ := riCandidate(t)
	db := riInitialized(t) // a clean, current-schema database: the snapshot flow with a safety snapshot and NO raw preservation
	r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
		o.testReplaceDatabaseFile = func(string, string) error {
			return &sqlite.DisplacementError{Stage: "move-shm", Err: injected, Main: sqlite.MainAtOriginal, Vanished: []string{db + "-wal"}}
		}
	})
	if r.err == nil || r.res.SafetySnapshot == "" || r.res.RawPreservation != "" {
		t.Fatalf("err %v res %+v", r.err, r.res)
	}
	msg := r.err.Error()
	for _, want := range []string{"was not replaced, but 1 of its files vanished during the restore", "the safety snapshot at " + r.res.SafetySnapshot + " was kept", db + "-wal"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the message must contain %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "is not needed") {
		t.Fatalf("the snapshot must not be called not needed when a file vanished:\n%s", msg)
	}
}

func TestRestoreDisplace_InstallBlocked_ReportsEverythingAndNeverOpensTheOriginalPath(t *testing.T) {
	t.Parallel()
	cand, kp, _ := riCandidate(t)
	db := riMismatch(t)
	mainHash := icFileHash(t, db)
	r := riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
		o.ConfirmAfterPreservation = func(RestorePlan, PreservationInfo) bool { return true }
		o.testBeforeRename = func() { // another process takes the original path
			if err := os.WriteFile(db, []byte("someone else's database"), 0o600); err != nil {
				t.Error(err)
			}
		}
	})
	if r.err == nil {
		t.Fatal("the installation must be refused")
	}
	if !errors.Is(r.err, sqlite.ErrDisplaceBlocked) || !errors.Is(r.err, sqlite.ErrRollbackIncomplete) {
		t.Fatalf("want ErrDisplaceBlocked and ErrRollbackIncomplete, got %v", r.err)
	}
	if got, _ := os.ReadFile(db); string(got) != "someone else's database" {
		t.Fatal("the file that appeared was overwritten")
	}
	for _, sfx := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(db + sfx); !os.IsNotExist(err) {
			t.Fatalf("opening the original path created or restored %s", sfx)
		}
	}
	if r.res.DisplacedDir == "" || len(r.res.DisplacedFiles) == 0 {
		t.Fatalf("the retained originals must be reported on failure: %+v", r.res)
	}
	if !strings.Contains(r.err.Error(), r.res.DisplacedDir) {
		t.Fatalf("the error must name the retained location: %v", r.err)
	}
	for _, f := range r.res.DisplacedFiles {
		if f.Name == "main" && icFileHash(t, f.Retained) != mainHash {
			t.Fatal("the retained main file is not the original")
		}
	}
	if r.res.RawPreservation == "" || len(riPreservations(t, filepath.Dir(db))) != 1 {
		t.Fatal("the raw preservation must be kept")
	}
	if matches, _ := filepath.Glob(db + ".restoring-*"); len(matches) != 0 {
		t.Fatalf("the prepared replacement file must be removed after a failure: %v", matches)
	}
}

// ---- Phase C: an installation that could not be verified, or whose outcome is unknown ------------------------

// riUnverified runs a Restore whose replacement step reports the error that mk builds
// from the database path (so the path is part of the error before Restore runs), and
// records whether Restore went on to reopen the database or write an audit event.
func riUnverified(t *testing.T, mk func(db string) error) (r *riRun, db string, reopens, events *int) {
	t.Helper()
	cand, kp, _ := riCandidate(t)
	db = riMismatch(t)
	err := mk(db)
	reopens, events = new(int), new(int)
	r = riDo(t, db, cand, kp, func(o *RestoreOptions, _ *riRun) {
		o.ConfirmAfterPreservation = func(RestorePlan, PreservationInfo) bool { return true }
		o.testReplaceDatabaseFile = func(string, string) error { return err }
		o.testReopen = func(context.Context, string) (*sqlite.Store, error) {
			*reopens++
			return nil, errors.New("must not be reopened")
		}
		o.testAppendRestoreEvent = func(_ context.Context, _ *sqlite.Store, ev store.AuditEvent) (store.AuditEvent, error) {
			*events++
			return ev, nil
		}
	})
	return r, db, reopens, events
}

func TestRestoreDisplace_InstalledButUnverified_IsNotACompletedRestore(t *testing.T) {
	t.Parallel()
	// observed is what the message must say was seen; conclusion what it may conclude.
	// notWant are claims the evidence for that kind does not support.
	cases := []struct {
		kind    sqlite.InstalledFileKind
		want    []string
		notWant []string
	}{
		{sqlite.InstalledGone, []string{"nothing is at that path any more", "the installed file is missing", "may NOT be the restored content"},
			[]string{"no longer has its content", "could not be opened"}},
		{sqlite.InstalledNotRegular, []string{"is not a regular file", "may NOT be the restored content"},
			[]string{"nothing is at that path", "missing"}},
		{sqlite.InstalledReplaced, []string{"a different file than the one installed", "may NOT be the restored content"},
			[]string{"missing", "not a regular file"}},
		{sqlite.InstalledContentChanged, []string{"size or content of the installed file differs from what was written", "may NOT be the restored content"},
			[]string{"missing", "different file"}},
		{sqlite.InstalledUnstable, []string{"still changing while it was verified", "its content was not stable"},
			[]string{"missing", "different file"}},
		{sqlite.InstalledUnreadable, []string{"could not be opened or read, so it was not verified", "has not been confirmed whether the active database is the restored content"},
			[]string{"may NOT be the restored content", "differs"}},
	}
	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			t.Parallel()
			r, db, reopens, events := riUnverified(t, func(db string) error {
				return &sqlite.InstalledFileChangedError{Path: db, Kind: tc.kind, Detail: "injected detail"}
			})
			if r.err == nil {
				t.Fatal("an unverified installation must not be reported as success")
			}
			var ice *sqlite.InstalledFileChangedError
			if !errors.As(r.err, &ice) || ice.Kind != tc.kind || ice.Path != db {
				t.Fatalf("the typed cause, with the active database path, must be preserved: %v", r.err)
			}
			msg := r.err.Error()
			// The error carries the active database path both from the cause and from Restore.
			if !strings.Contains(msg, "installed at "+db+" but") || !strings.Contains(msg, "replaced the active database at "+db+",") {
				t.Fatalf("the active database path must be reported by the cause and by Restore:\n%s", msg)
			}
			for _, want := range append(tc.want, "the database was not reopened, migrated or recorded", "inspect it before use") {
				if !strings.Contains(msg, want) {
					t.Fatalf("the message must contain %q:\n%s", want, msg)
				}
			}
			for _, bad := range append(tc.notWant, "failed before activation", "restore completed") {
				if strings.Contains(msg, bad) {
					t.Fatalf("the message must not claim %q:\n%s", bad, msg)
				}
			}
			if r.reached(stageReplaced) || *reopens != 0 || *events != 0 || r.res.RestoreEventID != "" {
				t.Fatalf("nothing may be reopened, migrated, checkpointed or recorded: stages %v reopens %d events %d id %q",
					r.stages, *reopens, *events, r.res.RestoreEventID)
			}
			if r.res.RawPreservation == "" || !strings.Contains(msg, r.res.RawPreservation) {
				t.Fatalf("the raw preservation must be kept and named:\n%s", msg)
			}
		})
	}
}

func TestRestoreDisplace_InstallOutcomeUnknown_IsNotARollbackAndNothingIsOpened(t *testing.T) {
	t.Parallel()
	injected := errors.New("injected failure")
	dir := t.TempDir()
	retained := filepath.Join(dir, "main")
	de := &sqlite.DisplacementError{
		Stage: "install", Err: fmt.Errorf("%w: %w", sqlite.ErrDisplaceInstallOutcomeUnknown, injected),
		Install: sqlite.InstallUnknown, Main: sqlite.MainRetained, Dir: dir,
		Retained: []sqlite.DisplacedFile{{Name: "main", Original: "/x/c.db", Retained: retained}},
	}
	r, db, reopens, events := riUnverified(t, func(string) error { return de })
	if r.err == nil || !errors.Is(r.err, sqlite.ErrDisplaceInstallOutcomeUnknown) || !errors.Is(r.err, injected) {
		t.Fatalf("err %v", r.err)
	}
	msg := r.err.Error()
	for _, want := range []string{"its outcome could not be established", "nothing was rolled back, reopened or recorded", "inspect it before use", db, dir} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the message must contain %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "failed before activation") || strings.Contains(msg, "could not all be put back") {
		t.Fatalf("an unknown outcome is neither a failed activation nor a partial rollback:\n%s", msg)
	}
	if r.reached(stageReplaced) || *reopens != 0 || *events != 0 || r.res.RestoreEventID != "" {
		t.Fatalf("nothing may be reopened or recorded: stages %v reopens %d events %d", r.stages, *reopens, *events)
	}
	if r.res.DisplacedDir != dir || len(r.res.DisplacedFiles) != 1 {
		t.Fatalf("the retained originals must be reported: %+v", r.res)
	}
	if r.res.RawPreservation == "" {
		t.Fatal("the raw preservation must be kept")
	}
}
