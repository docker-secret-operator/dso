package centralsvc

// Tests for planRestore and the assessment fingerprint (see restore_plan.go).
//
// The planner is pure, so most of these tests enumerate its whole input space and
// check it twice: against an independently written oracle, and against safety
// invariants that must hold for every input. The journal tests use genuine
// SQLite fixtures: a hot rollback journal left by a process that was SIGKILLed
// mid-transaction, and a cold non-empty journal left by journal_mode=PERSIST.

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/docker-secret-operator/dso/internal/securefs/securefstest"
	"github.com/docker-secret-operator/dso/internal/store/sqlite"
)

// ---- helper process: a genuine hot rollback journal ---------------------------------------------

// TestHelperProcessRestorePlan is not a real test: it runs only when
// DSO_TEST_RP_DB is set. It switches an existing DSO database to rollback-journal
// mode and is SIGKILLed in the middle of a transaction that has spilled pages to
// the database file, leaving a valid hot -journal behind.
func TestHelperProcessRestorePlan(t *testing.T) {
	path := os.Getenv("DSO_TEST_RP_DB")
	if path == "" {
		t.Skip("helper process")
	}
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	db.SetMaxOpenConns(1)
	for _, stmt := range []string{
		`PRAGMA journal_mode=DELETE`, `PRAGMA synchronous=OFF`, `PRAGMA cache_size=5`, `BEGIN`,
		`CREATE TABLE junk(x)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			fmt.Fprintln(os.Stderr, stmt, err)
			os.Exit(2)
		}
	}
	for i := 0; i < 6000; i++ {
		if _, err := db.Exec(`INSERT INTO junk VALUES (?)`, strings.Repeat("j", 500)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	if _, err := db.Exec(`UPDATE projects SET name = 'mutated-by-the-crashed-transaction'`); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
	select {}
}

// rpHotJournal returns a closed DSO database with one project and a genuine hot
// rollback journal beside it.
func rpHotJournal(t *testing.T) (dbPath string) {
	t.Helper()
	e := setup(t)
	e.project(t, "proj", "prod")
	if err := e.st.Close(); err != nil {
		t.Fatal(err)
	}
	e.st = nil
	c := exec.Command(os.Args[0], "-test.run=^TestHelperProcessRestorePlan$") // #nosec G204 -- this test binary
	c.Env = append(os.Environ(), "DSO_TEST_RP_DB="+e.dbPath)
	c.Stderr = os.Stderr
	err := c.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("the helper should have been killed: %v", err)
	}
	if ws, ok := ee.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
		t.Fatalf("expected SIGKILL, got %v", err)
	}
	fi, err := os.Stat(e.dbPath + "-journal")
	if err != nil || fi.Size() == 0 {
		t.Fatalf("the crash must leave a non-empty -journal (%v)", err)
	}
	for _, sfx := range []string{"-wal", "-shm"} {
		if _, err := os.Lstat(e.dbPath + sfx); !os.IsNotExist(err) {
			t.Fatalf("a rollback-mode database must have no %s (%v)", sfx, err)
		}
	}
	// Prove the journal is genuinely HOT: SQLite, given a copy, rolls the
	// crashed transaction back (the junk table disappears, the project returns).
	dir := securefstest.TempDir(t)
	for _, sfx := range []string{"", "-journal"} {
		b, err := os.ReadFile(e.dbPath + sfx)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "c.db"+sfx), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cp, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cp.Close()
	var name string
	if err := cp.QueryRow(`SELECT name FROM projects`).Scan(&name); err != nil || name == "mutated-by-the-crashed-transaction" {
		t.Fatalf("fixture is not a hot journal: SQLite did not roll the transaction back (%q, %v)", name, err)
	}
	var junk int
	if err := cp.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='junk'`).Scan(&junk); err != nil || junk != 0 {
		t.Fatalf("fixture is not a hot journal: the junk table survived (%d, %v)", junk, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "c.db-journal")); !os.IsNotExist(err) {
		t.Fatal("SQLite should have consumed the hot journal during recovery")
	}
	return e.dbPath
}

// ---- builders ---------------------------------------------------------------------------------------

const rpLatest = 3

// rpSC builds sidecar information; sizes < 0 mean the file is absent.
func rpSC(mainExists bool, wal, shm, journal int64) sqlite.ActiveSidecarsInfo {
	f := func(n int64) sqlite.SidecarFile {
		if n < 0 {
			return sqlite.SidecarFile{}
		}
		return sqlite.SidecarFile{Exists: true, Size: n}
	}
	return sqlite.ActiveSidecarsInfo{MainExists: mainExists, WAL: f(wal), SHM: f(shm), Journal: f(journal)}
}

func rpA(state activeState, structure structuralFinding, versionKnown bool, version int) activeAssessment {
	a := activeAssessment{State: state, Structure: structure, VersionKnown: versionKnown}
	if versionKnown {
		a.RecordedVersion = version
	}
	if state == stateInconclusive {
		a.Err = errors.New("inspection failed")
	}
	return a
}

var rpAllStates = []activeState{stateInconclusive, stateMissing, stateInitialized, stateEmpty, stateDSOUnbound,
	stateBindingCorrupt, stateForeign, stateTooNew, stateNonSQLite}

var rpAllStructures = []structuralFinding{structureUnknown, structureClean, structureMismatch}

// rpOracle is written independently of planRestore: it states the policy as a
// plain table.
type rpWant struct {
	refuse   error // the specific category, or nil
	flow     RestoreFlow
	preserve bool
	second   bool
	open     bool
}

func rpOracle(state activeState, structure structuralFinding, versionKnown bool, version int, walNE, journalNE, orphan bool) rpWant {
	switch state {
	case stateMissing:
		if orphan {
			return rpWant{refuse: ErrRestoreRefusedOrphans}
		}
		return rpWant{flow: RestoreFlowNoActive}
	case stateForeign:
		return rpWant{refuse: ErrRestoreRefusedForeign}
	case stateTooNew:
		return rpWant{refuse: ErrRestoreRefusedTooNew}
	case stateNonSQLite:
		return rpWant{refuse: ErrRestoreRefusedNotSQLite}
	case stateInconclusive:
		return rpWant{refuse: ErrRestoreRefusedInconclusive}
	case stateEmpty:
		// A fact about the file, not an inference: preserved (with any journal) and replaced.
		return rpWant{flow: RestoreFlowReplaceWithoutOpen, preserve: true}
	case stateBindingCorrupt, stateDSOUnbound:
		// R8: the classifier does not look at a rollback journal, so a negative or
		// partial finding beside a non-empty one cannot be trusted.
		if journalNE {
			return rpWant{refuse: ErrRestoreRefusedInconclusive}
		}
		return rpWant{flow: RestoreFlowReplaceWithoutOpen, preserve: true}
	case stateInitialized:
		if structure == structureClean && versionKnown {
			if version > rpLatest {
				return rpWant{refuse: ErrRestoreRefusedInvalid}
			}
			if version < rpLatest || walNE || journalNE {
				return rpWant{flow: RestoreFlowSnapshot, preserve: true, open: true}
			}
			return rpWant{flow: RestoreFlowSnapshot, open: true}
		}
		return rpWant{flow: RestoreFlowReplaceWithoutOpen, preserve: true, second: true}
	}
	return rpWant{refuse: ErrRestoreRefusedInconclusive}
}

// rpEach enumerates every combination of planner inputs that can occur.
func rpEach(t *testing.T, fn func(name string, in planInput, orphan bool, w rpWant)) {
	t.Helper()
	sizes := []int64{-1, 0, 5000} // absent, empty, non-empty
	for _, st := range rpAllStates {
		for _, structure := range rpAllStructures {
			for _, known := range []bool{false, true} {
				for _, ver := range []int{rpLatest, rpLatest - 1, rpLatest + 1} {
					for _, wal := range sizes {
						for _, jr := range sizes {
							for _, shm := range []int64{-1, 5000} {
								mainExists := st != stateMissing
								sc := rpSC(mainExists, wal, shm, jr)
								orphan := len(sc.OrphanNonEmpty()) > 0
								name := fmt.Sprintf("%v/%v/known=%v/v%d/wal=%d/journal=%d/shm=%d", st, structure, known, ver, wal, jr, shm)
								in := planInput{Assessment: rpA(st, structure, known, ver), Sidecars: sc, LatestVersion: rpLatest}
								fn(name, in, orphan, rpOracle(st, structure, known, ver, sc.WAL.NonEmpty(), sc.Journal.NonEmpty(), orphan))
							}
						}
					}
				}
			}
		}
	}
}

// ---- the planner: whole input space ---------------------------------------------------------------------

func TestPlanRestore_MatchesTheApprovedPolicyForEveryInput(t *testing.T) {
	t.Parallel()
	n := 0
	rpEach(t, func(name string, in planInput, _ bool, w rpWant) {
		n++
		p, err := planRestore(in)
		if w.refuse != nil {
			var re *RestoreRefusedError
			if !errors.As(err, &re) || !errors.Is(err, ErrRestoreRefused) || !errors.Is(err, w.refuse) {
				t.Fatalf("%s: want refusal %v, got plan %+v err %v", name, w.refuse, p, err)
			}
			if p.Flow != RestoreFlowRefuse || p.Preserve || p.SecondConfirmation || p.OpenOriginal {
				t.Fatalf("%s: a refusal must carry an empty plan: %+v", name, p)
			}
			return
		}
		if err != nil {
			t.Fatalf("%s: unexpected refusal: %v", name, err)
		}
		if p.Flow != w.flow || p.Preserve != w.preserve || p.SecondConfirmation != w.second || p.OpenOriginal != w.open {
			t.Fatalf("%s: got flow=%v preserve=%v second=%v open=%v, want flow=%v preserve=%v second=%v open=%v",
				name, p.Flow, p.Preserve, p.SecondConfirmation, p.OpenOriginal, w.flow, w.preserve, w.second, w.open)
		}
		if p.Preserve && len(p.PreserveReasons) == 0 {
			t.Fatalf("%s: a plan that preserves must say why", name)
		}
	})
	if n < 1500 {
		t.Fatalf("only %d inputs were enumerated", n)
	}
}

func TestPlanRestore_SafetyInvariantsHoldForEveryInput(t *testing.T) {
	t.Parallel()
	rpEach(t, func(name string, in planInput, _ bool, _ rpWant) {
		a, sc := in.Assessment, in.Sidecars
		p, err := planRestore(in)
		// R8: no plan may ever be made from a negative or partial finding that sits
		// beside a non-empty rollback journal.
		if (a.State == stateDSOUnbound || a.State == stateBindingCorrupt) && sc.Journal.NonEmpty() {
			if err == nil || !errors.Is(err, ErrRestoreRefusedInconclusive) || p.Flow != RestoreFlowRefuse || p.Preserve || p.OpenOriginal {
				t.Fatalf("%s: a %s finding beside a non-empty -journal must be refused as inconclusive, got %+v / %v", name, a.State, p, err)
			}
		}
		if err != nil {
			// Refusals: never any permission, never preservation, always the umbrella error.
			if p.Flow != RestoreFlowRefuse || p.Preserve || p.OpenOriginal || p.SecondConfirmation {
				t.Fatalf("%s: a refusal grants something: %+v", name, p)
			}
			if !errors.Is(err, ErrRestoreRefused) {
				t.Fatalf("%s: %v", name, err)
			}
			return
		}
		switch a.State {
		case stateForeign, stateTooNew, stateNonSQLite, stateInconclusive:
			t.Fatalf("%s: this state must always be refused, got %+v", name, p)
		}
		// Opening the original without preserving first is allowed only for the one
		// plain case: initialized, structurally Clean, the latest schema, and no
		// non-empty -wal or -journal.
		if p.OpenOriginal && !p.Preserve {
			if a.State != stateInitialized || a.Structure != structureClean || !a.VersionKnown ||
				a.RecordedVersion != rpLatest || sc.WAL.NonEmpty() || sc.Journal.NonEmpty() {
				t.Fatalf("%s: the original may be opened without preservation here: %+v", name, p)
			}
		}
		// Unknown and Mismatch are never Clean: no snapshot flow, no Open, preserve, second confirmation.
		if a.State == stateInitialized && (a.Structure != structureClean || !a.VersionKnown) {
			if p.Flow != RestoreFlowReplaceWithoutOpen || p.OpenOriginal || !p.Preserve || !p.SecondConfirmation {
				t.Fatalf("%s: a non-Clean initialized database must be preserved, confirmed twice and never opened: %+v", name, p)
			}
		}
		if p.SecondConfirmation && a.State != stateInitialized {
			t.Fatalf("%s: only an initialized, non-Clean database needs the second confirmation: %+v", name, p)
		}
		if p.OpenOriginal && p.Flow != RestoreFlowSnapshot {
			t.Fatalf("%s: only the snapshot flow opens the original: %+v", name, p)
		}
		if p.Preserve && p.Flow != RestoreFlowSnapshot && p.Flow != RestoreFlowReplaceWithoutOpen {
			t.Fatalf("%s: preservation belongs to flows that act on an existing file: %+v", name, p)
		}
		if a.State == stateMissing && (p.Preserve || p.OpenOriginal) {
			t.Fatalf("%s: there is nothing to preserve or open: %+v", name, p)
		}
		if p.State != a.State.String() || p.Structure != a.Structure.String() {
			t.Fatalf("%s: the plan must report the assessment it was made from: %+v", name, p)
		}
	})
}

func TestPlanRestore_ExplicitRows(t *testing.T) {
	t.Parallel()
	type row struct {
		name   string
		in     planInput
		flow   RestoreFlow
		pres   bool
		second bool
		open   bool
		reason string // substring of one preserve reason
	}
	rows := []row{
		{"plain initialized clean latest", planInput{rpA(stateInitialized, structureClean, true, rpLatest), rpSC(true, -1, -1, -1), rpLatest}, RestoreFlowSnapshot, false, false, true, ""},
		{"empty -wal and -journal are not triggers", planInput{rpA(stateInitialized, structureClean, true, rpLatest), rpSC(true, 0, 0, 0), rpLatest}, RestoreFlowSnapshot, false, false, true, ""},
		{"a non-empty -shm alone is not a trigger", planInput{rpA(stateInitialized, structureClean, true, rpLatest), rpSC(true, -1, 4096, -1), rpLatest}, RestoreFlowSnapshot, false, false, true, ""},
		{"older schema (R3)", planInput{rpA(stateInitialized, structureClean, true, rpLatest-1), rpSC(true, -1, -1, -1), rpLatest}, RestoreFlowSnapshot, true, false, true, "older than"},
		{"non-empty -wal (R4)", planInput{rpA(stateInitialized, structureClean, true, rpLatest), rpSC(true, 100, 4096, -1), rpLatest}, RestoreFlowSnapshot, true, false, true, "-wal"},
		{"non-empty cold -journal (R8)", planInput{rpA(stateInitialized, structureClean, true, rpLatest), rpSC(true, -1, -1, 100), rpLatest}, RestoreFlowSnapshot, true, false, true, "-journal"},
		{"older schema and wal and journal", planInput{rpA(stateInitialized, structureClean, true, 1), rpSC(true, 1, -1, 1), rpLatest}, RestoreFlowSnapshot, true, false, true, "older than"},
		{"initialized mismatch (R1)", planInput{rpA(stateInitialized, structureMismatch, true, rpLatest), rpSC(true, -1, -1, -1), rpLatest}, RestoreFlowReplaceWithoutOpen, true, true, false, "does not match"},
		{"initialized Unknown structure (R1)", planInput{rpA(stateInitialized, structureUnknown, false, 0), rpSC(true, -1, -1, -1), rpLatest}, RestoreFlowReplaceWithoutOpen, true, true, false, "could not be established"},
		{"initialized Clean but no known version is not trusted", planInput{rpA(stateInitialized, structureClean, false, 0), rpSC(true, -1, -1, -1), rpLatest}, RestoreFlowReplaceWithoutOpen, true, true, false, "could not be established"},
		{"binding corrupt (R2)", planInput{rpA(stateBindingCorrupt, structureClean, true, rpLatest), rpSC(true, -1, -1, -1), rpLatest}, RestoreFlowReplaceWithoutOpen, true, false, false, "binding-corrupt"},
		{"dso-unbound (R2)", planInput{rpA(stateDSOUnbound, structureUnknown, false, 0), rpSC(true, -1, -1, -1), rpLatest}, RestoreFlowReplaceWithoutOpen, true, false, false, "dso-unbound"},
		{"empty file (R2)", planInput{rpA(stateEmpty, structureUnknown, false, 0), rpSC(true, -1, -1, -1), rpLatest}, RestoreFlowReplaceWithoutOpen, true, false, false, "empty"},
		{"empty file with a non-empty journal carries it along", planInput{rpA(stateEmpty, structureUnknown, false, 0), rpSC(true, -1, -1, 4096), rpLatest}, RestoreFlowReplaceWithoutOpen, true, false, false, "-journal"},
		{"missing main, no companions", planInput{rpA(stateMissing, structureUnknown, false, 0), rpSC(false, -1, -1, -1), rpLatest}, RestoreFlowNoActive, false, false, false, ""},
		{"missing main, empty companions only", planInput{rpA(stateMissing, structureUnknown, false, 0), rpSC(false, 0, 0, 0), rpLatest}, RestoreFlowNoActive, false, false, false, ""},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			p, err := planRestore(r.in)
			if err != nil {
				t.Fatal(err)
			}
			if p.Flow != r.flow || p.Preserve != r.pres || p.SecondConfirmation != r.second || p.OpenOriginal != r.open {
				t.Fatalf("got %+v", p)
			}
			if r.reason != "" && !strings.Contains(strings.Join(p.PreserveReasons, "\n"), r.reason) {
				t.Fatalf("preserve reasons lack %q: %v", r.reason, p.PreserveReasons)
			}
		})
	}
}

func TestPlanRestore_UnknownIsNeverClean(t *testing.T) {
	t.Parallel()
	for _, structure := range []structuralFinding{structureUnknown, structureMismatch, structuralFinding(99)} {
		for _, known := range []bool{false, true} {
			for _, sc := range []sqlite.ActiveSidecarsInfo{rpSC(true, -1, -1, -1), rpSC(true, 1, 1, 1)} {
				p, err := planRestore(planInput{rpA(stateInitialized, structure, known, rpLatest), sc, rpLatest})
				if err != nil {
					t.Fatal(err)
				}
				if p.Flow == RestoreFlowSnapshot || p.OpenOriginal || !p.SecondConfirmation || !p.Preserve {
					t.Fatalf("structure %v known=%v was treated as Clean: %+v", structure, known, p)
				}
			}
		}
	}
	// The zero value of the structural finding is Unknown.
	var zero activeAssessment
	zero.State = stateInitialized
	p, err := planRestore(planInput{zero, rpSC(true, -1, -1, -1), rpLatest})
	if err != nil || p.Flow != RestoreFlowReplaceWithoutOpen || !p.SecondConfirmation {
		t.Fatalf("an unset structure must be handled as Unknown: %+v, %v", p, err)
	}
}

func TestPlanRestore_InconsistentInputsFailClosed(t *testing.T) {
	t.Parallel()
	cases := map[string]planInput{
		"missing state but the main file exists": {rpA(stateMissing, structureUnknown, false, 0), rpSC(true, -1, -1, -1), rpLatest},
		"existing state but no main file":        {rpA(stateInitialized, structureClean, true, rpLatest), rpSC(false, -1, -1, -1), rpLatest},
		"empty state but no main file":           {rpA(stateEmpty, structureUnknown, false, 0), rpSC(false, -1, -1, -1), rpLatest},
		"inconclusive but no main file":          {rpA(stateInconclusive, structureUnknown, false, 0), rpSC(false, -1, -1, -1), rpLatest},
		"initialized carrying an error": {func() activeAssessment {
			a := rpA(stateInitialized, structureClean, true, rpLatest)
			a.Err = errors.New("x")
			return a
		}(), rpSC(true, -1, -1, -1), rpLatest},
		"clean initialized, no supported version":                                         {rpA(stateInitialized, structureClean, true, rpLatest), rpSC(true, -1, -1, -1), 0},
		"clean initialized, version 0 and no supported version (nothing to compare with)": {rpA(stateInitialized, structureClean, true, 0), rpSC(true, -1, -1, -1), 0},
		"clean initialized, negative latest":                                              {rpA(stateInitialized, structureClean, true, rpLatest), rpSC(true, -1, -1, -1), -1},
		"clean initialized newer than supported":                                          {rpA(stateInitialized, structureClean, true, rpLatest+1), rpSC(true, -1, -1, -1), rpLatest},
		"unrecognised state value":                                                        {rpA(activeState(99), structureClean, true, rpLatest), rpSC(true, -1, -1, -1), rpLatest},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p, err := planRestore(in)
			if err == nil || !errors.Is(err, ErrRestoreRefused) {
				t.Fatalf("want a refusal, got %+v / %v", p, err)
			}
			if p.Flow != RestoreFlowRefuse || p.Preserve || p.OpenOriginal || p.SecondConfirmation {
				t.Fatalf("a refusal must carry an empty plan: %+v", p)
			}
		})
	}
}

func TestRestorePlan_ZeroValuePermitsNothing(t *testing.T) {
	t.Parallel()
	var p RestorePlan
	if p.Flow != RestoreFlowRefuse || p.Preserve || p.SecondConfirmation || p.OpenOriginal {
		t.Fatalf("the zero plan must refuse and permit nothing: %+v", p)
	}
	if RestoreFlowRefuse != 0 {
		t.Fatal("refuse must be the zero value of RestoreFlow")
	}
	names := map[string]bool{}
	for _, f := range []RestoreFlow{RestoreFlowRefuse, RestoreFlowNoActive, RestoreFlowSnapshot, RestoreFlowReplaceWithoutOpen} {
		if strings.HasPrefix(f.String(), "RestoreFlow(") || names[f.String()] {
			t.Fatalf("flow %d has no distinct name: %q", int(f), f.String())
		}
		names[f.String()] = true
	}
	if !strings.HasPrefix(RestoreFlow(99).String(), "RestoreFlow(") {
		t.Fatal("an unknown flow must be named as such")
	}
}

// ---- refusal errors and guidance -------------------------------------------------------------------------

func TestPlanRestore_RefusalErrorsAndGuidance(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		in       planInput
		kind     error
		contains []string
		absent   []string
	}{
		{"foreign", planInput{rpA(stateForeign, structureUnknown, false, 0), rpSC(true, -1, -1, -1), rpLatest}, ErrRestoreRefusedForeign,
			[]string{"not a DSO Central database", "no override", "move it"}, nil},
		{"too new", planInput{rpA(stateTooNew, structureUnknown, false, 0), rpSC(true, -1, -1, -1), rpLatest}, ErrRestoreRefusedTooNew,
			[]string{"newer version"}, nil},
		{"not sqlite", planInput{rpA(stateNonSQLite, structureUnknown, false, 0), rpSC(true, -1, -1, -1), rpLatest}, ErrRestoreRefusedNotSQLite,
			[]string{"not a SQLite database"}, nil},
		{"inconclusive without a journal", planInput{rpA(stateInconclusive, structureUnknown, false, 0), rpSC(true, -1, -1, -1), rpLatest}, ErrRestoreRefusedInconclusive,
			[]string{"inspection failed", "nothing was changed"}, []string{"rollback journal"}},
		{"inconclusive with a non-empty journal (hot journal guidance)", planInput{rpA(stateInconclusive, structureUnknown, false, 0), rpSC(true, -1, -1, 4096), rpLatest}, ErrRestoreRefusedInconclusive,
			[]string{"rollback journal", "hot journal", "SQLite itself", "move all of them aside"}, nil},
		{"inconclusive with an empty journal gets no journal guidance", planInput{rpA(stateInconclusive, structureUnknown, false, 0), rpSC(true, -1, -1, 0), rpLatest}, ErrRestoreRefusedInconclusive,
			nil, []string{"rollback journal"}},
		{"orphan companions", planInput{rpA(stateMissing, structureUnknown, false, 0), rpSC(false, 10, 10, 10), rpLatest}, ErrRestoreRefusedOrphans,
			[]string{"-wal, -journal, -shm", "will not delete or overwrite", "move them aside yourself"}, nil},
		{"orphan journal only", planInput{rpA(stateMissing, structureUnknown, false, 0), rpSC(false, -1, -1, 10), rpLatest}, ErrRestoreRefusedOrphans,
			[]string{"-journal"}, []string{"-wal"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := planRestore(tc.in)
			var re *RestoreRefusedError
			if !errors.As(err, &re) {
				t.Fatalf("want *RestoreRefusedError, got %v", err)
			}
			if !errors.Is(err, ErrRestoreRefused) || !errors.Is(err, tc.kind) {
				t.Fatalf("must match the umbrella and %v: %v", tc.kind, err)
			}
			for _, other := range []error{ErrRestoreRefusedForeign, ErrRestoreRefusedTooNew, ErrRestoreRefusedNotSQLite,
				ErrRestoreRefusedInconclusive, ErrRestoreRefusedOrphans, ErrRestoreRefusedInvalid} {
				if other != tc.kind && errors.Is(err, other) {
					t.Fatalf("also matches %v: %v", other, err)
				}
			}
			text := err.Error()
			for _, want := range tc.contains {
				if !strings.Contains(text, want) {
					t.Fatalf("message lacks %q:\n%s", want, text)
				}
			}
			for _, bad := range tc.absent {
				if strings.Contains(text, bad) {
					t.Fatalf("message must not contain %q:\n%s", bad, text)
				}
			}
			// Guidance never tells the operator or the program to delete anything.
			for _, g := range re.Guidance {
				low := strings.ToLower(g)
				if strings.HasPrefix(low, "delete") || strings.HasPrefix(low, "remove") || strings.Contains(low, " rm ") {
					t.Fatalf("guidance must not ask for deletion: %q", g)
				}
			}
		})
	}
}

func TestRestoreRefusedError_Unwrap(t *testing.T) {
	t.Parallel()
	cause := errors.New("underlying")
	e := &RestoreRefusedError{Kind: ErrRestoreRefusedInconclusive, Cause: cause}
	if !errors.Is(e, cause) || !errors.Is(e, ErrRestoreRefusedInconclusive) || !errors.Is(e, ErrRestoreRefused) {
		t.Fatal("the cause and the category must both be reachable")
	}
	var nilKind = &RestoreRefusedError{}
	if errors.Is(nilKind, ErrRestoreRefusedForeign) || !errors.Is(nilKind, ErrRestoreRefused) {
		t.Fatal("an error with no category matches only the umbrella")
	}
}

// ---- the planner is pure -----------------------------------------------------------------------------------

func TestRestorePlan_FileHasNoSideEffectCapability(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("restore_plan.go")
	if err != nil {
		t.Fatal(err)
	}
	f, err := parser.ParseFile(token.NewFileSet(), "restore_plan.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	allowedImports := map[string]bool{
		"errors": true, "fmt": true, "strings": true,
		"github.com/docker-secret-operator/dso/internal/store/sqlite": true,
	}
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if !allowedImports[path] {
			t.Errorf("the planner must stay free of I/O, but imports %q", path)
		}
	}
	allowedSQLite := map[string]bool{"ActiveSidecarsInfo": true, "SidecarFile": true} // plain data types only
	forbiddenIdents := map[string]bool{
		"assessActive": true, "assessActiveWith": true, "classifyActive": true, "CheckInitialized": true,
		"checkInitializedWith": true, "InspectDatabase": true, "PreserveActiveFiles": true, "ActiveSidecars": true,
		"StatIdentity": true, "Open": true, "Restore": true,
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok && id.Name == "sqlite" && !allowedSQLite[x.Sel.Name] {
				t.Errorf("the planner may use only the plain sqlite data types, not sqlite.%s", x.Sel.Name)
			}
		case *ast.Ident:
			if forbiddenIdents[x.Name] {
				t.Errorf("the planner must not reference %s", x.Name)
			}
		}
		return true
	})
}

// ---- real fixtures: journals, old schema, orphans -------------------------------------------------------------------

func TestPlanRestore_HotRollbackJournal_IsInconclusiveAndRefusedWithGuidance(t *testing.T) {
	t.Parallel()
	path := rpHotJournal(t)
	before := icHashes(t, filepath.Dir(path))

	a := assessActive(path)
	if a.State != stateInconclusive || !errors.Is(a.Err, sqlite.ErrInspectionFailed) {
		t.Fatalf("a database with a hot journal must classify as Inconclusive, got %v / %v", a.State, a.Err)
	}
	if errors.Is(a.Err, ErrNotInitialized) {
		t.Fatalf("an inconclusive classification must not look like 'not initialized': %v", a.Err)
	}
	sc, err := sqlite.ActiveSidecars(path)
	if err != nil {
		t.Fatal(err)
	}
	if !sc.MainExists || !sc.Journal.NonEmpty() || sc.WAL.Exists {
		t.Fatalf("unexpected sidecars: %+v", sc)
	}
	_, err = planRestore(planInput{Assessment: a, Sidecars: sc, LatestVersion: rpLatest})
	if !errors.Is(err, ErrRestoreRefusedInconclusive) {
		t.Fatalf("want a refusal, got %v", err)
	}
	for _, want := range []string{"rollback journal", "hot journal", "SQLite itself"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal lacks %q:\n%s", want, err.Error())
		}
	}
	if after := icHashes(t, filepath.Dir(path)); !icSameDir(before, after) {
		t.Fatalf("assessing and planning changed files:\nbefore %v\nafter  %v", before, after)
	}
}

func TestPlanRestore_EmptyMainWithAHotJournal_IsPreservedNotOpened(t *testing.T) {
	t.Parallel()
	// The path the classifier cannot protect: a zero-byte main file is classified
	// Empty before its companions are looked at, so a hot journal beside it must be
	// carried by the plan into the preservation.
	hot := rpHotJournal(t)
	journal, err := os.ReadFile(hot + "-journal")
	if err != nil {
		t.Fatal(err)
	}
	dir := securefstest.TempDir(t)
	path := filepath.Join(dir, "c.db")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+"-journal", journal, 0o600); err != nil {
		t.Fatal(err)
	}
	a := assessActive(path)
	if a.State != stateEmpty {
		t.Fatalf("state = %v (%v)", a.State, a.Err)
	}
	sc, err := sqlite.ActiveSidecars(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := planRestore(planInput{Assessment: a, Sidecars: sc, LatestVersion: rpLatest})
	if err != nil {
		t.Fatal(err)
	}
	if p.Flow != RestoreFlowReplaceWithoutOpen || !p.Preserve || p.OpenOriginal {
		t.Fatalf("got %+v", p)
	}
	if !strings.Contains(strings.Join(p.PreserveReasons, "\n"), "-journal") {
		t.Fatalf("the plan must name the journal it will preserve: %v", p.PreserveReasons)
	}
}

func TestPlanRestore_ColdNonEmptyJournal_IsPreservedThenSnapshot(t *testing.T) {
	t.Parallel()
	// journal_mode=PERSIST leaves the journal file behind with a zeroed header: a
	// genuine cold, non-empty journal on an otherwise ordinary DSO database.
	e := icBound(t, true)
	db, err := sql.Open("sqlite", "file:"+e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	for _, stmt := range []string{`PRAGMA journal_mode=PERSIST`, `UPDATE projects SET name = name`} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(stmt, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	head, err := os.ReadFile(e.dbPath + "-journal")
	if err != nil || len(head) < 8 {
		t.Fatalf("expected a non-empty -journal (%d bytes, %v)", len(head), err)
	}
	if !bytes.Equal(head[:8], make([]byte, 8)) {
		t.Fatal("a PERSIST journal after a completed transaction has a zeroed header (cold)")
	}
	a := assessActive(e.dbPath)
	if a.State != stateInitialized || a.Structure != structureClean {
		t.Fatalf("a cold journal does not change the classification: %v / %v (%v)", a.State, a.Structure, a.Err)
	}
	sc, err := sqlite.ActiveSidecars(e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	p, err := planRestore(planInput{Assessment: a, Sidecars: sc, LatestVersion: rpLatest})
	if err != nil {
		t.Fatal(err)
	}
	if p.Flow != RestoreFlowSnapshot || !p.Preserve || !p.OpenOriginal || p.SecondConfirmation {
		t.Fatalf("got %+v", p)
	}
	if !strings.Contains(strings.Join(p.PreserveReasons, "\n"), "-journal") {
		t.Fatalf("reasons: %v", p.PreserveReasons)
	}
}

func TestPlanRestore_OlderSchemaDatabase_IsPreservedFirst(t *testing.T) {
	t.Parallel()
	// A genuine schema-2 DSO database: bound, with a project, and exactly the
	// tables of schema 2.
	e := icBound(t, true)
	icEdit(t, e.dbPath, `DROP TRIGGER audit_events_no_update`, `DROP TRIGGER audit_events_no_delete`,
		`DROP TABLE audit_events`, `DELETE FROM schema_migrations WHERE version = 3`)
	a := assessActive(e.dbPath)
	if a.State != stateInitialized || a.Structure != structureClean || !a.VersionKnown || a.RecordedVersion != 2 {
		t.Fatalf("got %v / %v / known=%v v%d (%v %v)", a.State, a.Structure, a.VersionKnown, a.RecordedVersion, a.StructureReasons, a.Err)
	}
	sc, err := sqlite.ActiveSidecars(e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	p, err := planRestore(planInput{Assessment: a, Sidecars: sc, LatestVersion: rpLatest})
	if err != nil {
		t.Fatal(err)
	}
	if p.Flow != RestoreFlowSnapshot || !p.Preserve || !p.OpenOriginal || !strings.Contains(strings.Join(p.PreserveReasons, "\n"), "older than") {
		t.Fatalf("got %+v", p)
	}
}

func TestPlanRestore_MissingMainWithOrphanCompanions(t *testing.T) {
	t.Parallel()
	for _, sfx := range []string{"-wal", "-journal", "-shm"} {
		t.Run(sfx, func(t *testing.T) {
			t.Parallel()
			dir := securefstest.TempDir(t)
			path := filepath.Join(dir, "c.db")
			if err := os.WriteFile(path+sfx, []byte("orphaned data"), 0o600); err != nil {
				t.Fatal(err)
			}
			before := icHashes(t, dir)
			a := assessActive(path)
			sc, err := sqlite.ActiveSidecars(path)
			if err != nil {
				t.Fatal(err)
			}
			if a.State != stateMissing || sc.MainExists {
				t.Fatalf("state %v, sidecars %+v", a.State, sc)
			}
			_, err = planRestore(planInput{Assessment: a, Sidecars: sc, LatestVersion: rpLatest})
			if !errors.Is(err, ErrRestoreRefusedOrphans) || !strings.Contains(err.Error(), sfx) {
				t.Fatalf("got %v", err)
			}
			if after := icHashes(t, dir); !icSameDir(before, after) {
				t.Fatalf("the orphan was touched:\nbefore %v\nafter  %v", before, after)
			}
		})
	}
	t.Run("empty-companions-are-not-orphans", func(t *testing.T) {
		t.Parallel()
		dir := securefstest.TempDir(t)
		path := filepath.Join(dir, "c.db")
		for _, sfx := range []string{"-wal", "-journal", "-shm"} {
			if err := os.WriteFile(path+sfx, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		a := assessActive(path)
		sc, _ := sqlite.ActiveSidecars(path)
		p, err := planRestore(planInput{Assessment: a, Sidecars: sc, LatestVersion: rpLatest})
		if err != nil || p.Flow != RestoreFlowNoActive {
			t.Fatalf("got %+v / %v", p, err)
		}
	})
}

// ---- the assessment fingerprint -------------------------------------------------------------------------------

func TestFingerprint_EqualityAndEveryDifference(t *testing.T) {
	t.Parallel()
	base := func() (activeAssessment, sqlite.ActiveSidecarsInfo) {
		return rpA(stateInitialized, structureClean, true, rpLatest), rpSC(true, 100, 4096, -1)
	}
	a0, s0 := base()
	f0 := fingerprintOf(a0, s0)
	if ok, diffs := fingerprintsEquivalent(f0, fingerprintOf(a0, s0)); !ok || len(diffs) != 0 {
		t.Fatalf("identical assessments must be equivalent: %v", diffs)
	}
	mutations := map[string]struct {
		mutate func(a *activeAssessment, s *sqlite.ActiveSidecarsInfo)
		want   string
	}{
		"state":                   {func(a *activeAssessment, _ *sqlite.ActiveSidecarsInfo) { a.State = stateBindingCorrupt }, "state changed"},
		"structure":               {func(a *activeAssessment, _ *sqlite.ActiveSidecarsInfo) { a.Structure = structureMismatch }, "structure changed"},
		"structure to unknown":    {func(a *activeAssessment, _ *sqlite.ActiveSidecarsInfo) { a.Structure = structureUnknown }, "structure changed"},
		"version number":          {func(a *activeAssessment, _ *sqlite.ActiveSidecarsInfo) { a.RecordedVersion = rpLatest - 1 }, "recorded schema version"},
		"version becomes unknown": {func(a *activeAssessment, _ *sqlite.ActiveSidecarsInfo) { a.VersionKnown = false }, "recorded schema version"},
		"wal becomes empty":       {func(_ *activeAssessment, s *sqlite.ActiveSidecarsInfo) { s.WAL.Size = 0 }, "-wal non-empty"},
		"wal disappears":          {func(_ *activeAssessment, s *sqlite.ActiveSidecarsInfo) { s.WAL = sqlite.SidecarFile{} }, "-wal non-empty"},
		"journal appears": {func(_ *activeAssessment, s *sqlite.ActiveSidecarsInfo) {
			s.Journal = sqlite.SidecarFile{Exists: true, Size: 1}
		}, "-journal non-empty"},
	}
	for name, m := range mutations {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			a, s := base()
			m.mutate(&a, &s)
			ok, diffs := fingerprintsEquivalent(f0, fingerprintOf(a, s))
			if ok || !strings.Contains(strings.Join(diffs, "\n"), m.want) {
				t.Fatalf("want a difference containing %q, got ok=%v %v", m.want, ok, diffs)
			}
		})
	}
	t.Run("things-that-must-not-matter", func(t *testing.T) {
		t.Parallel()
		a, s := base()
		s.SHM = sqlite.SidecarFile{Exists: true, Size: 1} // -shm is advisory
		s.WAL.Size = 99999                                // only emptiness counts
		a.StructureReasons = []string{"different wording"}
		if ok, diffs := fingerprintsEquivalent(f0, fingerprintOf(a, s)); !ok {
			t.Fatalf("the -shm, sizes and reason text must not matter: %v", diffs)
		}
		// An unknown version is never a number: garbage in the number is ignored.
		u1 := fingerprintOf(rpA(stateDSOUnbound, structureUnknown, false, 0), rpSC(true, -1, -1, -1))
		ua := rpA(stateDSOUnbound, structureUnknown, false, 0)
		ua.RecordedVersion = 77
		if ok, diffs := fingerprintsEquivalent(u1, fingerprintOf(ua, rpSC(true, -1, -1, -1))); !ok {
			t.Fatalf("an unknown version must compare equal whatever the stored number: %v", diffs)
		}
	})
	t.Run("a known version zero is not an unknown version", func(t *testing.T) {
		t.Parallel()
		sc := rpSC(true, -1, -1, -1)
		known := fingerprintOf(rpA(stateInitialized, structureClean, true, 0), sc)
		unknown := fingerprintOf(rpA(stateInitialized, structureClean, false, 0), sc)
		if ok, diffs := fingerprintsEquivalent(known, unknown); ok || !strings.Contains(strings.Join(diffs, "\n"), "recorded schema version") {
			t.Fatalf("known-0 and unknown must differ: %v %v", ok, diffs)
		}
	})
	t.Run("inconclusive-is-never-equivalent", func(t *testing.T) {
		t.Parallel()
		inc := fingerprintOf(rpA(stateInconclusive, structureUnknown, false, 0), rpSC(true, -1, -1, -1))
		if ok, _ := fingerprintsEquivalent(inc, inc); ok {
			t.Fatal("an inconclusive assessment establishes nothing, even against itself")
		}
		if ok, diffs := fingerprintsEquivalent(f0, inc); ok || !strings.Contains(strings.Join(diffs, "\n"), "new assessment is inconclusive") {
			t.Fatalf("got %v %v", ok, diffs)
		}
		if ok, diffs := fingerprintsEquivalent(inc, f0); ok || !strings.Contains(strings.Join(diffs, "\n"), "original assessment was inconclusive") {
			t.Fatalf("got %v %v", ok, diffs)
		}
	})
}

func TestRestorePlan_MatchesReassessment(t *testing.T) {
	t.Parallel()
	a, sc := rpA(stateInitialized, structureMismatch, true, rpLatest), rpSC(true, -1, -1, 5)
	p, err := planRestore(planInput{Assessment: a, Sidecars: sc, LatestVersion: rpLatest})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.matchesReassessment(a, sc); err != nil {
		t.Fatalf("the same assessment must match: %v", err)
	}
	shm := sc
	shm.SHM = sqlite.SidecarFile{Exists: true, Size: 7}
	if err := p.matchesReassessment(a, shm); err != nil {
		t.Fatalf("a changed -shm must not matter: %v", err)
	}
	changed := a
	changed.Structure = structureClean
	err = p.matchesReassessment(changed, sc)
	var mm *ReassessmentMismatchError
	if !errors.As(err, &mm) || !errors.Is(err, ErrRestoreReassessmentMismatch) || len(mm.Differences) == 0 {
		t.Fatalf("got %v", err)
	}
	inc := rpA(stateInconclusive, structureUnknown, false, 0)
	if err := p.matchesReassessment(inc, sc); !errors.Is(err, ErrRestoreReassessmentMismatch) {
		t.Fatalf("an inconclusive reassessment must not match: %v", err)
	}
	gone := sc
	gone.Journal = sqlite.SidecarFile{}
	if err := p.matchesReassessment(a, gone); !errors.Is(err, ErrRestoreReassessmentMismatch) {
		t.Fatalf("a changed journal state must not match: %v", err)
	}
}

// ---- A1: R8 for negative and partial findings -------------------------------------------------------------------

func TestPlanRestore_R8_NonEmptyJournalTable(t *testing.T) {
	t.Parallel()
	type j int
	const (
		absent j = iota
		empty
		nonEmpty
	)
	size := map[j]int64{absent: -1, empty: 0, nonEmpty: 4096}
	rows := []struct {
		name   string
		a      activeAssessment
		jr     j
		refuse error // nil: a plan is expected
		flow   RestoreFlow
		pres   bool
		second bool
		open   bool
	}{
		{"dso-unbound, no journal", rpA(stateDSOUnbound, structureClean, true, rpLatest), absent, nil, RestoreFlowReplaceWithoutOpen, true, false, false},
		{"dso-unbound, empty journal", rpA(stateDSOUnbound, structureClean, true, rpLatest), empty, nil, RestoreFlowReplaceWithoutOpen, true, false, false},
		{"dso-unbound, NON-EMPTY journal", rpA(stateDSOUnbound, structureClean, true, rpLatest), nonEmpty, ErrRestoreRefusedInconclusive, 0, false, false, false},
		{"binding-corrupt, no journal", rpA(stateBindingCorrupt, structureClean, true, rpLatest), absent, nil, RestoreFlowReplaceWithoutOpen, true, false, false},
		{"binding-corrupt, empty journal", rpA(stateBindingCorrupt, structureClean, true, rpLatest), empty, nil, RestoreFlowReplaceWithoutOpen, true, false, false},
		{"binding-corrupt, NON-EMPTY journal", rpA(stateBindingCorrupt, structureClean, true, rpLatest), nonEmpty, ErrRestoreRefusedInconclusive, 0, false, false, false},
		{"binding-corrupt with mismatch, NON-EMPTY journal", rpA(stateBindingCorrupt, structureMismatch, true, rpLatest), nonEmpty, ErrRestoreRefusedInconclusive, 0, false, false, false},
		// Unchanged by A1:
		{"initialized clean, NON-EMPTY journal: preserve, then snapshot", rpA(stateInitialized, structureClean, true, rpLatest), nonEmpty, nil, RestoreFlowSnapshot, true, false, true},
		{"initialized mismatch, NON-EMPTY journal: preserve, second confirmation", rpA(stateInitialized, structureMismatch, true, rpLatest), nonEmpty, nil, RestoreFlowReplaceWithoutOpen, true, true, false},
		{"empty file, NON-EMPTY journal: preserve, replace without Open", rpA(stateEmpty, structureUnknown, false, 0), nonEmpty, nil, RestoreFlowReplaceWithoutOpen, true, false, false},
		{"foreign, NON-EMPTY journal: refused as foreign", rpA(stateForeign, structureUnknown, false, 0), nonEmpty, ErrRestoreRefusedForeign, 0, false, false, false},
		{"too-new, NON-EMPTY journal", rpA(stateTooNew, structureUnknown, false, 0), nonEmpty, ErrRestoreRefusedTooNew, 0, false, false, false},
		{"non-sqlite, NON-EMPTY journal", rpA(stateNonSQLite, structureUnknown, false, 0), nonEmpty, ErrRestoreRefusedNotSQLite, 0, false, false, false},
		{"inconclusive, NON-EMPTY journal", rpA(stateInconclusive, structureUnknown, false, 0), nonEmpty, ErrRestoreRefusedInconclusive, 0, false, false, false},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			p, err := planRestore(planInput{Assessment: r.a, Sidecars: rpSC(true, -1, -1, size[r.jr]), LatestVersion: rpLatest})
			if r.refuse != nil {
				if !errors.Is(err, ErrRestoreRefused) || !errors.Is(err, r.refuse) {
					t.Fatalf("want a %v refusal, got %+v / %v", r.refuse, p, err)
				}
				if p.Flow != RestoreFlowRefuse || p.Preserve || p.OpenOriginal || p.SecondConfirmation {
					t.Fatalf("a refusal must carry an empty plan: %+v", p)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if p.Flow != r.flow || p.Preserve != r.pres || p.SecondConfirmation != r.second || p.OpenOriginal != r.open {
				t.Fatalf("got %+v", p)
			}
		})
	}
}

func TestPlanRestore_R8_RefusalExplainsAndGuides(t *testing.T) {
	t.Parallel()
	for _, st := range []activeState{stateDSOUnbound, stateBindingCorrupt} {
		a := rpA(st, structureClean, true, rpLatest)
		a.Err = fmt.Errorf("the underlying finding for %s", st) // what the classifier reported
		_, err := planRestore(planInput{Assessment: a, Sidecars: rpSC(true, -1, -1, 4096), LatestVersion: rpLatest})
		var re *RestoreRefusedError
		if !errors.As(err, &re) || !errors.Is(err, ErrRestoreRefusedInconclusive) {
			t.Fatalf("%v: %v", st, err)
		}
		// The assessed state stays visible, the finding itself is not wrapped, and the
		// guidance is the hot-journal guidance.
		if re.State != st.String() || re.Cause != nil || errors.Is(err, ErrNotInitialized) || errors.Is(err, ErrBindingCorrupt) {
			t.Fatalf("%v: state %q cause %v: %v", st, re.State, re.Cause, err)
		}
		text := err.Error()
		for _, want := range []string{"does not take a rollback journal into account", "hot journal", "SQLite itself", "move all of them aside", "nothing was changed"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%v: message lacks %q:\n%s", st, want, text)
			}
		}
		for _, g := range re.Guidance {
			if low := strings.ToLower(g); strings.HasPrefix(low, "delete") || strings.HasPrefix(low, "remove") || strings.Contains(low, " rm ") {
				t.Fatalf("guidance must not ask for deletion: %q", g)
			}
		}
	}
}

// rpFileState is everything about a file that a refusal must leave alone.
type rpFileState struct {
	hash  string
	mode  os.FileMode
	size  int64
	mtime int64
	ino   uint64
}

func rpSnapshotDir(t *testing.T, dir string) map[string]rpFileState {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]rpFileState{}
	for _, e := range ents {
		fi, err := os.Lstat(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		st := rpFileState{mode: fi.Mode(), size: fi.Size(), mtime: fi.ModTime().UnixNano()}
		if sys, ok := fi.Sys().(*syscall.Stat_t); ok {
			st.ino = uint64(sys.Ino)
		}
		if fi.Mode().IsRegular() {
			st.hash = icFileHash(t, filepath.Join(dir, e.Name()))
		}
		out[e.Name()] = st
	}
	return out
}

func rpSameDir(t *testing.T, before, after map[string]rpFileState) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("the directory changed:\nbefore %v\nafter  %v", before, after)
	}
	for name, b := range before {
		if a, ok := after[name]; !ok || a != b {
			t.Fatalf("%s changed: before %+v, after %+v", name, b, a)
		}
	}
}

// rpJournalIsHotFor proves a journal is genuinely hot for a database: on a COPY,
// SQLite recovers from it, which consumes it.
func rpJournalIsHotFor(t *testing.T, path string) bool {
	t.Helper()
	dir := securefstest.TempDir(t)
	for _, sfx := range []string{"", "-journal"} {
		b, err := os.ReadFile(path + sfx)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "c.db"+sfx), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var n int
	_ = db.QueryRow(`SELECT count(*) FROM sqlite_master`).Scan(&n) // forces recovery; the result is irrelevant
	_, err = os.Lstat(filepath.Join(dir, "c.db-journal"))
	return os.IsNotExist(err)
}

func TestPlanRestore_R8_GenuineHotJournal_BesideUnboundAndBindingCorruptIsRefusedUntouched(t *testing.T) {
	t.Parallel()
	hot := rpHotJournal(t)
	journal, err := os.ReadFile(hot + "-journal")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		build func(t *testing.T) string
		state activeState
	}{
		{"dso-unbound", func(t *testing.T) string { return icUnbound(t) }, stateDSOUnbound},
		{"binding-corrupt", func(t *testing.T) string {
			e := icBound(t, true)
			icEdit(t, e.dbPath, icDropMetaTriggers1, icDropMetaTriggers2, `DELETE FROM store_meta WHERE key = 'binding.canary'`)
			return e.dbPath
		}, stateBindingCorrupt},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := tc.build(t)

			// Control: without a journal this is the ordinary R2 plan.
			a := assessActive(path)
			sc, err := sqlite.ActiveSidecars(path)
			if err != nil {
				t.Fatal(err)
			}
			if a.State != tc.state {
				t.Fatalf("fixture: state = %v (%v), want %v", a.State, a.Err, tc.state)
			}
			p, err := planRestore(planInput{Assessment: a, Sidecars: sc, LatestVersion: rpLatest})
			if err != nil || p.Flow != RestoreFlowReplaceWithoutOpen || !p.Preserve || p.SecondConfirmation || p.OpenOriginal {
				t.Fatalf("control without a journal: %+v / %v", p, err)
			}

			// Now a genuine hot journal appears beside it.
			if err := os.WriteFile(path+"-journal", journal, 0o600); err != nil {
				t.Fatal(err)
			}
			if !rpJournalIsHotFor(t, path) {
				t.Fatal("the fixture's journal must be hot for this database: SQLite should consume it during recovery")
			}
			dir := filepath.Dir(path)
			before := rpSnapshotDir(t, dir)

			// The classifier does not look at the journal, so it still reports the same
			// finding -- which is exactly why the planner must not trust it.
			a2 := assessActive(path)
			if a2.State != tc.state {
				t.Fatalf("the classifier ignores a journal, so its finding should be unchanged: %v (%v)", a2.State, a2.Err)
			}
			sc2, err := sqlite.ActiveSidecars(path)
			if err != nil || !sc2.Journal.NonEmpty() {
				t.Fatalf("%+v, %v", sc2, err)
			}
			p2, err := planRestore(planInput{Assessment: a2, Sidecars: sc2, LatestVersion: rpLatest})
			if !errors.Is(err, ErrRestoreRefusedInconclusive) || !errors.Is(err, ErrRestoreRefused) {
				t.Fatalf("want an inconclusive refusal, got %+v / %v", p2, err)
			}
			if p2.Flow != RestoreFlowRefuse || p2.Preserve || p2.OpenOriginal || p2.SecondConfirmation {
				t.Fatalf("a refusal must carry an empty plan: %+v", p2)
			}
			if errors.Is(err, ErrNotInitialized) || errors.Is(err, ErrBindingCorrupt) || errors.Is(err, ErrRestoreRefusedForeign) {
				t.Fatalf("the refusal must not look like the finding it distrusts: %v", err)
			}
			if !strings.Contains(err.Error(), "hot journal") || !strings.Contains(err.Error(), tc.state.String()) {
				t.Fatalf("message: %v", err)
			}

			// Nothing was opened, modified, deleted, replaced or created: the main file,
			// the journal and the directory are byte-, mtime- and inode-identical, and
			// no preservation, partial directory or sidecar appeared.
			rpSameDir(t, before, rpSnapshotDir(t, dir))
			if got, _ := os.ReadFile(path + "-journal"); !bytes.Equal(got, journal) {
				t.Fatal("the journal changed")
			}
			for name := range rpSnapshotDir(t, dir) {
				if strings.Contains(name, ".pre-restore-") || strings.HasSuffix(name, "-wal") || strings.HasSuffix(name, "-shm") || strings.HasSuffix(name, ".partial") {
					t.Fatalf("unexpected entry %s", name)
				}
			}
		})
	}
}

func TestPlanRestore_R8_EmptyJournalBesideUnboundIsNotATrigger(t *testing.T) {
	t.Parallel()
	u := icUnbound(t)
	if err := os.WriteFile(u+"-journal", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	a := assessActive(u)
	sc, err := sqlite.ActiveSidecars(u)
	if err != nil || !sc.Journal.Exists || sc.Journal.NonEmpty() {
		t.Fatalf("%+v, %v", sc, err)
	}
	p, err := planRestore(planInput{Assessment: a, Sidecars: sc, LatestVersion: rpLatest})
	if err != nil || p.Flow != RestoreFlowReplaceWithoutOpen || !p.Preserve {
		t.Fatalf("an empty journal carries no rollback data: %+v / %v", p, err)
	}
}

func TestPlanRestore_R8_ColdJournalBesideInitializedAndHotJournalBesideEmptyAreUnchanged(t *testing.T) {
	t.Parallel()
	hot := rpHotJournal(t)
	journal, err := os.ReadFile(hot + "-journal")
	if err != nil {
		t.Fatal(err)
	}
	// Initialized + a (hot) journal: still preserved, then the snapshot flow.
	b := icBound(t, false)
	if err := os.WriteFile(b.dbPath+"-journal", journal, 0o600); err != nil {
		t.Fatal(err)
	}
	a := assessActive(b.dbPath)
	sc, _ := sqlite.ActiveSidecars(b.dbPath)
	p, err := planRestore(planInput{Assessment: a, Sidecars: sc, LatestVersion: rpLatest})
	if err != nil || a.State != stateInitialized || p.Flow != RestoreFlowSnapshot || !p.Preserve || !p.OpenOriginal || p.SecondConfirmation {
		t.Fatalf("initialized + journal: %v %+v / %v", a.State, p, err)
	}
	// Foreign + a journal: still refused as foreign.
	f := icForeign(t, `CREATE TABLE customer(id)`)
	if err := os.WriteFile(f+"-journal", journal, 0o600); err != nil {
		t.Fatal(err)
	}
	af := assessActive(f)
	scf, _ := sqlite.ActiveSidecars(f)
	if _, err := planRestore(planInput{Assessment: af, Sidecars: scf, LatestVersion: rpLatest}); !errors.Is(err, ErrRestoreRefusedForeign) {
		t.Fatalf("foreign + journal: %v / %v", af.State, err)
	}
}
