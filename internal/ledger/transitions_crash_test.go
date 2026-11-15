package ledger

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Crash windows W1-W15 of design section 4.3, Docker-free. A crash is Close
// plus reopen of the same directory; write faults are injected at the store's
// write seam before the rename ("create", "rename": the old document stays) or
// after it ("syncdir": the complete candidate is on disk but unacknowledged).
// The journal is modelled by plain JournalRecord values, as the S4b bridge will
// supply them. Every reconcile is run twice: the second run changes nothing.

const (
	ridD1 = "rot-0000000000000d01"
	ridD2 = "rot-0000000000000d02"
)

// started returns a baseline rig (D1 applied) that observed D2 and, when
// correlate is true, correlated rotation ridD2 for it (steps 2 to 4).
func started(t *testing.T, correlate bool) (*rig, Version, Version) {
	t.Helper()
	r, d1 := baseline(t)
	d2 := r.ver("D2")
	r.mustObserve(d2, s4Live(2))
	if correlate {
		if err := r.v.Correlate(s4K, ridD2, 2, d2); err != nil {
			t.Fatal(err)
		}
	}
	return r, d1, d2
}

func (r *rig) reconcileTwice(snap JournalSnapshot) ReconcileReport {
	r.t.Helper()
	rep, err := r.v.ReconcileFromJournal(snap)
	if err != nil {
		r.t.Fatalf("reconcile: %v", err)
	}
	gen := r.s.Health().Generation
	again, err := r.v.ReconcileFromJournal(snap)
	if err != nil || len(again.Changes) != 0 || r.s.Health().Generation != gen {
		r.t.Fatalf("a second reconcile must change nothing: %+v %v", again, err)
	}
	requireVocabulary(r.t, r.s.Snapshot().Ledger)
	return rep
}

func journal(recs ...JournalRecord) JournalSnapshot {
	return JournalSnapshot{Readable: true, Records: recs, Horizon: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func rules(rep ReconcileReport) string {
	var s []string
	for _, c := range rep.Changes {
		s = append(s, c.Rule)
	}
	return strings.Join(s, ",")
}

func requireApplied(t *testing.T, e Entry, want Version) {
	t.Helper()
	if e.Applied == nil || Compare(*e.Applied, want) != Same {
		t.Fatalf("applied must be %v: %+v", want.ID, e.Applied)
	}
}

func TestW1_CrashAfterObserve_BeforeJournal_OneRotation(t *testing.T) {
	r, d1, _ := started(t, false)
	r.crash()
	rep := r.reconcileTwice(journal(jrec(ridD1, "restart", JournalCompleted, 5, true, &d1)))
	if len(rep.Changes) != 0 {
		t.Fatalf("%+v", rep)
	}
	e := r.entry()
	requireApplied(t, e, d1)
	if e.DesiredSeq != 2 || e.State != EntryPending {
		t.Fatalf("desired D2 must be durable: %+v", e)
	}
	if d := r.decide(); !d.Rotate() {
		t.Fatal("exactly one rotation is owed")
	}
}

func TestW2_JournalStartedNoCorrelation_LedgerUnchanged(t *testing.T) {
	r, d1, _ := started(t, false)
	r.crash()
	r.reconcileTwice(journal(jrec(ridD1, "restart", JournalCompleted, 5, true, &d1), jrec(ridD2, "restart", JournalRecovered, 0, false, nil)))
	requireApplied(t, r.entry(), d1)
	if d := r.decide(); !d.Rotate() {
		t.Fatal("re-dispatch is owed (guard permitting)")
	}
}

func TestW3_CorrelatedBeforeFirstDockerAction_HintCleared(t *testing.T) {
	r, d1, _ := started(t, true)
	r.crash()
	rep := r.reconcileTwice(journal(jrec(ridD2, "restart", JournalRecovered, 0, false, nil)))
	if rules(rep) != "W3" {
		t.Fatalf("%s", rules(rep))
	}
	e := r.entry()
	requireApplied(t, e, d1)
	for _, rp := range e.Replicas {
		if rp.Delivery != DeliveryNone || rp.Outcome != ReplicaPending || rp.Reason != string(WhyDeferred) {
			t.Fatalf("%+v", rp)
		}
	}
	if d := r.decide(); !d.Rotate() {
		t.Fatal("one re-dispatch")
	}
}

func TestW4_BeforeCandidateReady_UnknownNeverD2(t *testing.T) {
	r, d1, d2 := started(t, true)
	r.crash()
	rep := r.reconcileTwice(journal(jrec(ridD2, "restart", JournalCriticalError, 2, false, &d2)))
	if rules(rep) != "W4" {
		t.Fatalf("%s", rules(rep))
	}
	e := r.entry()
	requireApplied(t, e, d1)
	if e.State != EntryFailed {
		t.Fatalf("%+v", e)
	}
	for _, rp := range e.Replicas {
		if rp.Adoption != AdoptionUnknown || rp.Delivery != DeliveryNone || rp.Reason != string(WhyInterrupted) || Compare(*rp.Applied, d2) == Same {
			t.Fatalf("never D2, never trusted: %+v", rp)
		}
	}
}

func TestW5_AfterReadyWithoutCommit_NoAdvance_CommitAdvances(t *testing.T) {
	r, d1, d2 := started(t, true)
	r.crash()
	r.reconcileTwice(journal(jrec(ridD2, "rolling", JournalRecovered, 4, true, &d2)))
	requireApplied(t, r.entry(), d1)
	if d := r.decide(); !d.Rotate() {
		t.Fatal("one extra rotation allowed, none skipped")
	}
	// Variant: M0 recovery recorded Committed + recovered: evidence holds.
	r2, _, d2b := started(t, true)
	r2.crash()
	rec := jrec(ridD2, "rolling", JournalRecovered, 4, true, &d2b)
	rec.Committed = true
	if rep := r2.reconcileTwice(journal(rec)); rules(rep) != "W6" {
		t.Fatalf("%s", rules(rep))
	}
	requireApplied(t, r2.entry(), d2b)
}

// N21-dependent case 8: the journal commit is recorded, the ledger update was
// interrupted. Reconcile converges without rotating again.
func TestW6_JournalCommittedLedgerInterrupted_NoSecondRotation(t *testing.T) {
	r, _, d2 := started(t, true)
	r.crash()
	rep := r.reconcileTwice(journal(jrec(ridD2, "restart", JournalCompleted, 7, true, &d2)))
	if rules(rep) != "W6" {
		t.Fatalf("%s", rules(rep))
	}
	e := r.entry()
	requireApplied(t, e, d2)
	if e.State != EntryIdle {
		t.Fatalf("%+v", e)
	}
	if d := r.decide(); d.Rotate() {
		t.Fatalf("zero dispatches: %+v", d)
	}
	// Without a journal-carried version (O-2 not in place) nothing advances.
	r2, d1, _ := started(t, true)
	r2.crash()
	if rep := r2.reconcileTwice(journal(jrec(ridD2, "restart", JournalCompleted, 7, true, nil))); rules(rep) != "no_version" {
		t.Fatalf("%s", rules(rep))
	}
	requireApplied(t, r2.entry(), d1)
}

func TestW7_VerifyAndApplyAreOneUpdate(t *testing.T) {
	for _, fault := range []string{"create", "rename"} {
		r, d1, d2 := started(t, true)
		before := readLedgerBytes(t, r.dir)
		r.fault.Store(fault)
		if _, err := r.v.RecordDeliveryVerified(s4K, evOK(ridD2, d2)); err == nil {
			t.Fatal("injected fault must fail the update")
		}
		r.fault.Store("")
		if string(readLedgerBytes(t, r.dir)) != string(before) {
			t.Fatalf("%s: the file must hold neither the verified nor the applied write", fault)
		}
		e := r.entry()
		requireApplied(t, e, d1)
		for _, rp := range e.Replicas {
			if rp.Delivery != DeliveryRequested {
				t.Fatalf("no verified-but-not-applied state is observable: %+v", rp)
			}
		}
	}
	// A split record (verified for R, entry not applied) is repaired as W6.
	r, _, d2 := started(t, true)
	if err := r.s.update(func(l *Ledger) error {
		e := findEntry(l, s4K)
		for i := range e.Replicas {
			rp := &e.Replicas[i]
			rp.Applied, rp.Delivery, rp.Adoption, rp.Outcome = versionCopy(d2), DeliveryVerified, AdoptionVerified, ReplicaSucceeded
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if rep := r.reconcileTwice(journal(jrec(ridD2, "restart", JournalCompleted, 7, true, &d2))); rules(rep) != "W7" {
		t.Fatalf("%s", rules(rep))
	}
	requireApplied(t, r.entry(), d2)
}

func TestW8_LedgerPersistenceFails_RestartConvergesFromJournal(t *testing.T) {
	r, d1, d2 := started(t, true)
	r.fault.Store("create")
	if _, err := r.v.RecordDeliveryVerified(s4K, evOK(ridD2, d2)); err == nil {
		t.Fatal("expected a persistence failure")
	}
	requireApplied(t, r.entry(), d1) // published state unchanged; the engine keeps secretHashes
	r.fault.Store("")
	r.crash()
	r.reconcileTwice(journal(jrec(ridD2, "restart", JournalCompleted, 7, true, &d2)))
	requireApplied(t, r.entry(), d2)
	if d := r.decide(); d.Rotate() {
		t.Fatal("no re-dispatch after convergence")
	}
}

func TestW9_SignalDone_AdoptionUnverified_NoLoop(t *testing.T) {
	r, _, d2 := started(t, true)
	r.crash()
	if rep := r.reconcileTwice(journal(jrec(ridD2, "signal", JournalCompleted, 3, true, &d2))); rules(rep) != "W9" {
		t.Fatalf("%s", rules(rep))
	}
	e := r.entry()
	requireApplied(t, e, d2)
	for _, rp := range e.Replicas {
		if rp.Delivery != DeliveryVerified || rp.Adoption != AdoptionUnverified {
			t.Fatalf("signal: delivery verified, adoption unverified: %+v", rp)
		}
	}
	if d := r.decide(); d.Rotate() {
		t.Fatal("no loop of signal rotations")
	}
}

func TestW10_JournalRecordDeleted_HintCleared(t *testing.T) {
	r, d1, _ := started(t, true)
	r.crash()
	if rep := r.reconcileTwice(journal()); rules(rep) != "W10" {
		t.Fatalf("%s", rules(rep))
	}
	requireApplied(t, r.entry(), d1)
	for _, rp := range r.entry().Replicas {
		if rp.Delivery != DeliveryNone {
			t.Fatalf("%+v", rp)
		}
	}
}

func TestW11_FreshLedger_IgnoresOlderJournalHistory(t *testing.T) {
	r := newRig(t)
	d1 := r.ver("D1")
	r.mustObserve(d1, s4Live(2))
	old := jrec(ridD1, "restart", JournalCompleted, 7, true, &d1)
	old.UpdatedAt = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if rep := r.reconcileTwice(journal(old)); len(rep.Changes) != 0 {
		t.Fatalf("%+v", rep)
	}
	if e := r.entry(); e.State != EntryAdopted || e.Applied != nil {
		t.Fatalf("history is not replayed into a fresh ledger: %+v", e)
	}
}

// W12: the ledger claims more than the journal. The journal wins.
func TestW12_LedgerClaimWithoutJournalEvidence_Downgraded(t *testing.T) {
	type variant struct {
		name    string
		snap    func(d2 Version) JournalSnapshot
		rule    string
		applied bool // claim accepted
	}
	variants := []variant{
		{"R absent and recent", func(Version) JournalSnapshot { return journal() }, "R-B", false},
		{"R absent, horizon zero", func(Version) JournalSnapshot { return JournalSnapshot{Readable: true} }, "R-B", false},
		{"R absent and older than the horizon", func(Version) JournalSnapshot {
			s := journal()
			s.Horizon = time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
			return s
		}, "", true},
		{"R present without evidence", func(d2 Version) JournalSnapshot {
			return journal(jrec(ridD2, "restart", JournalInProgress, 2, false, &d2))
		}, "R-A", false},
		{"R recovered only", func(d2 Version) JournalSnapshot {
			return journal(jrec(ridD2, "restart", JournalRecovered, 5, true, &d2))
		}, "R-A", false},
		{"R completed with another version", func(Version) JournalSnapshot {
			other := Version{Kind: KindHMAC, ID: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", KeyID: s2KeyA}
			return journal(jrec(ridD2, "restart", JournalCompleted, 5, true, &other))
		}, "R-A", false},
	}
	for _, vt := range variants {
		t.Run(vt.name, func(t *testing.T) {
			r, _, d2 := started(t, true)
			// A restored backup or a bug: the ledger claims D2 verified and applied.
			if _, err := r.v.RecordDeliveryVerified(s4K, evOK(ridD2, d2)); err != nil {
				t.Fatal(err)
			}
			r.crash()
			rep := r.reconcileTwice(vt.snap(d2))
			if rules(rep) != vt.rule {
				t.Fatalf("rules %q, want %q", rules(rep), vt.rule)
			}
			e := r.entry()
			if vt.applied {
				requireApplied(t, e, d2)
				return
			}
			if e.Applied != nil || e.State != EntryFailed {
				t.Fatalf("the unsupported claim is corrected to unknown: %+v", e)
			}
			for _, rp := range e.Replicas {
				if rp.Applied != nil || rp.Delivery != DeliveryNone || rp.Adoption != AdoptionUnknown {
					t.Fatalf("%+v", rp)
				}
			}
			if d := r.decide(); !d.Rotate() {
				t.Fatal("a corrected claim costs at most one extra rotation, never none")
			}
		})
	}
	// Evidence downgrade: the ledger claims adoption verified for a signal
	// rotation; the journal shows signal_done, so adoption is lowered.
	r, _, d2 := started(t, true)
	if _, err := r.v.RecordDeliveryVerified(s4K, evOK(ridD2, d2)); err != nil {
		t.Fatal(err)
	}
	r.crash()
	if rep := r.reconcileTwice(journal(jrec(ridD2, "signal", JournalCompleted, 3, true, &d2))); rules(rep) != "W12-adoption" {
		t.Fatalf("%s", rules(rep))
	}
	for _, rp := range r.entry().Replicas {
		if rp.Adoption != AdoptionUnverified || rp.Delivery != DeliveryVerified {
			t.Fatalf("signal evidence never supports adoption verified: %+v", rp)
		}
	}
}

// W13: a torn write leaves either complete document; reconcile converges both
// outcomes to the same final state.
func TestW13_TornWrite_BothOutcomesConverge(t *testing.T) {
	final := map[string]Entry{}
	for _, fault := range []string{"rename", "syncdir"} {
		r, _, d2 := started(t, true)
		r.fault.Store(fault)
		if _, err := r.v.RecordDeliveryVerified(s4K, evOK(ridD2, d2)); err == nil {
			t.Fatal("injected fault must fail the update")
		}
		r.fault.Store("")
		r.crash()
		if _, err := Unmarshal(readLedgerBytes(t, r.dir)); err != nil {
			t.Fatalf("%s: restart must read a complete document: %v", fault, err)
		}
		r.reconcileTwice(journal(jrec(ridD2, "restart", JournalCompleted, 7, true, &d2)))
		e := r.entry()
		requireApplied(t, e, d2)
		e.LastOutcome, e.DesiredObservedAt = nil, time.Time{}
		for i := range e.Replicas {
			e.Replicas[i].At = time.Time{}
		}
		final[fault] = e
	}
	if !reflect.DeepEqual(final["rename"], final["syncdir"]) {
		t.Fatalf("the two outcomes did not converge:\n%+v\n%+v", final["rename"], final["syncdir"])
	}
}

// W14: the journal start failed, so the engine never correlates; the ledger
// holds no citation and still wants the rotation.
func TestW14_JournalStartFailed_NoCitation(t *testing.T) {
	r, d1, _ := started(t, false)
	for _, rp := range r.entry().Replicas {
		if rp.Delivery == DeliveryRequested || rp.RotationID == ridD2 {
			t.Fatalf("%+v", rp)
		}
	}
	if rep := r.reconcileTwice(journal(jrec(ridD1, "restart", JournalCompleted, 5, true, &d1))); len(rep.Changes) != 0 {
		t.Fatalf("%+v", rep)
	}
	if d := r.decide(); !d.Rotate() {
		t.Fatal("still wanted")
	}
}

func TestW15_FailedRotation_AppliedUnchanged_Retried(t *testing.T) {
	r, d1, d2 := started(t, true)
	r.crash()
	if rep := r.reconcileTwice(journal(jrec(ridD2, "restart", JournalRollbackRequired, 4, true, &d2))); rules(rep) != "W15" {
		t.Fatalf("%s", rules(rep))
	}
	e := r.entry()
	requireApplied(t, e, d1)
	if e.State != EntryFailed {
		t.Fatalf("%+v", e)
	}
	if d := r.decide(); !d.Rotate() {
		t.Fatal("retried next poll")
	}
}

func TestReconcile_UnreadableJournal_ChangesNothing(t *testing.T) {
	r, _, d2 := started(t, true)
	before := r.s.Snapshot().Ledger
	rep, err := r.v.ReconcileFromJournal(JournalSnapshot{Readable: false, Records: []JournalRecord{jrec(ridD2, "restart", JournalCompleted, 7, true, &d2)}})
	if err != nil || !rep.Skipped || rep.SkipReason != WhyJournalUnreadable {
		t.Fatalf("%+v %v", rep, err)
	}
	if !reflect.DeepEqual(before, r.s.Snapshot().Ledger) {
		t.Fatal("an unreadable journal must change nothing")
	}
}

func TestReconcile_IdentityDrift_LiveSet(t *testing.T) {
	r, _ := baseline(t)
	live := s4Live(2)
	live[1].ContainerID = "c2-new"
	snap := journal(jrec(ridD1, "restart", JournalCompleted, 5, true, nil))
	snap.Live = map[Key][]LiveReplica{s4K: live}
	if rep := r.reconcileTwice(snap); rules(rep) != "identity_changed" {
		t.Fatalf("%s", rules(rep))
	}
	if d := r.decide(); d.Rotate() {
		t.Fatal("identity drift never rotates")
	}
}

// Scenario 10 (partial replica delivery, v1 all-or-nothing): a failed fan-out
// leaves applied unchanged for every targeted replica.
func TestScenario10_PartialFanOut_AllOrNothing(t *testing.T) {
	r, d1, _ := started(t, true)
	if err := r.v.HandleInterrupted(s4K, ridD2, CauseFailed); err != nil {
		t.Fatal(err)
	}
	e := r.entry()
	requireApplied(t, e, d1)
	if e.State != EntryFailed || e.State == EntryPartial {
		t.Fatalf("%+v", e)
	}
}

// X1 / T-GUARD (ledger side): the package depends on nothing that could touch
// Docker, the journal or the operation guard.
func TestX1_PackageImportsNoDockerJournalOrGuard(t *testing.T) {
	ents, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, e := range ents {
		n := e.Name()
		if !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Clean(n), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if p == "log" {
				t.Errorf("%s imports log: the ledger reports through events only", n)
			}
			for _, bad := range []string{"docker", "internal/agent", "internal/rotation", "internal/watcher", "internal/server", "os/exec", "net/http"} {
				if p == bad || strings.Contains(p, bad+"/") || strings.HasSuffix(p, "/"+bad) || (bad == "docker" && strings.Contains(p, "docker/")) {
					t.Errorf("%s imports %s", n, p)
				}
			}
		}
	}
}
